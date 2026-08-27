// =============================================================================
//  Copyright (c) 2026 Nyx Software, Inc.  All rights reserved.
//
//  CodeDistill
//
//  Property of Nyx Software, Inc., provided under a dual license: the GNU Affero General
//  Public License v3.0 (see the LICENSE file) and, separately, a commercial
//  license available from Nyx Software, Inc. Use outside the terms of one of those
//  licenses is prohibited.
//
//  SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-Nyx-Commercial
// =============================================================================

package sqlite

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"codedistill/internal/domain"
	"codedistill/internal/embed"
	"codedistill/internal/storage"
)

// UpsertCodeChunk inserts or replaces a chunk by its natural key
// (project_id, file_path, line_start, line_end). The indexer feeds this
// freshly-chunked rows; existing rows with the same content_hash on
// re-index keep their embedding (handled at the caller — the indexer
// checks the hash before calling). Embedding is preserved across
// upserts of identical content because we read it back and re-write.
func (s *Store) UpsertCodeChunk(ctx context.Context, c *domain.CodeChunk) error {
	now := time.Now().UTC()
	if c.CreatedAt.IsZero() {
		c.CreatedAt = now
	}
	c.UpdatedAt = now
	_, err := s.DB.ExecContext(ctx,
		`INSERT INTO code_chunks
			(id, project_id, file_path, line_start, line_end,
			 content_hash, content, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT (project_id, file_path, line_start, line_end)
		 DO UPDATE SET
			content_hash = excluded.content_hash,
			content      = excluded.content,
			updated_at   = excluded.updated_at,
			-- Preserve embedding only when the hash didn't change.
			embedding   = CASE
				WHEN code_chunks.content_hash = excluded.content_hash THEN code_chunks.embedding
				ELSE NULL
			END,
			embedded_at = CASE
				WHEN code_chunks.content_hash = excluded.content_hash THEN code_chunks.embedded_at
				ELSE NULL
			END
		`,
		c.ID, c.ProjectID, c.FilePath, c.LineStart, c.LineEnd,
		c.ContentHash, c.Content, c.CreatedAt, c.UpdatedAt,
	)
	return err
}

func (s *Store) ListCodeChunksForFile(
	ctx context.Context, projectID, filePath string,
) ([]*domain.CodeChunk, error) {
	rows, err := s.DB.QueryContext(ctx,
		`SELECT id, project_id, file_path, line_start, line_end,
		        content_hash, content,
		        embedding IS NOT NULL,
		        created_at, updated_at
		   FROM code_chunks
		  WHERE project_id = ? AND file_path = ?
		  ORDER BY line_start`,
		projectID, filePath,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*domain.CodeChunk{}
	for rows.Next() {
		c := &domain.CodeChunk{}
		if err := rows.Scan(
			&c.ID, &c.ProjectID, &c.FilePath, &c.LineStart, &c.LineEnd,
			&c.ContentHash, &c.Content, &c.HasEmbedding,
			&c.CreatedAt, &c.UpdatedAt,
		); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (s *Store) DeleteCodeChunksByID(ctx context.Context, ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	// Build a parameterized IN clause; small batches keep this safe.
	placeholders := strings.Repeat("?,", len(ids))
	placeholders = placeholders[:len(placeholders)-1]
	args := make([]any, len(ids))
	for i, id := range ids {
		args[i] = id
	}
	_, err := s.DB.ExecContext(ctx,
		`DELETE FROM code_chunks WHERE id IN (`+placeholders+`)`, args...)
	return err
}

func (s *Store) DeleteCodeChunksForMissingFiles(
	ctx context.Context, projectID string, keep []string,
) (int, error) {
	if len(keep) == 0 {
		// No files claimed — wipe every chunk for the project.
		res, err := s.DB.ExecContext(ctx,
			`DELETE FROM code_chunks WHERE project_id = ?`, projectID)
		if err != nil {
			return 0, err
		}
		n, _ := res.RowsAffected()
		return int(n), nil
	}
	placeholders := strings.Repeat("?,", len(keep))
	placeholders = placeholders[:len(placeholders)-1]
	args := make([]any, 0, len(keep)+1)
	args = append(args, projectID)
	for _, p := range keep {
		args = append(args, p)
	}
	res, err := s.DB.ExecContext(ctx,
		`DELETE FROM code_chunks
		  WHERE project_id = ? AND file_path NOT IN (`+placeholders+`)`,
		args...)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}

func (s *Store) ListUnembeddedCodeChunks(
	ctx context.Context, projectID string, limit int,
) ([]*domain.CodeChunk, error) {
	if limit <= 0 {
		limit = 100
	}
	// Exclude chunks the embedder has already given up on (migration
	// 0019). Without that filter, perma-failing chunks (SVG, minified
	// bundles) keep getting tried first every tick and block healthy
	// chunks behind them.
	rows, err := s.DB.QueryContext(ctx,
		`SELECT id, project_id, file_path, line_start, line_end,
		        content_hash, content, false, created_at, updated_at
		   FROM code_chunks
		  WHERE project_id = ?
		    AND embedded_at IS NULL
		    AND embed_failed_at IS NULL
		  ORDER BY created_at LIMIT ?`,
		projectID, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*domain.CodeChunk{}
	for rows.Next() {
		c := &domain.CodeChunk{}
		if err := rows.Scan(
			&c.ID, &c.ProjectID, &c.FilePath, &c.LineStart, &c.LineEnd,
			&c.ContentHash, &c.Content, &c.HasEmbedding,
			&c.CreatedAt, &c.UpdatedAt,
		); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (s *Store) UpdateCodeChunkEmbedding(
	ctx context.Context, id string, vector []byte, at time.Time,
) error {
	if len(vector) == 0 {
		return errors.New("code chunk: empty vector")
	}
	res, err := s.DB.ExecContext(ctx,
		`UPDATE code_chunks SET embedding = ?, embedded_at = ?, updated_at = ?
		  WHERE id = ?`,
		vector, at, at, id,
	)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("code chunk %s: %w", id, storage.ErrNotFound)
	}
	return nil
}

// MarkCodeChunkEmbedFailed stamps a chunk as embed-failed so the indexer
// stops retrying it on every tick. errMsg is recorded for triage; the
// column can be cleared via SQL to retry (e.g. after swapping embedders).
func (s *Store) MarkCodeChunkEmbedFailed(
	ctx context.Context, id, errMsg string, at time.Time,
) error {
	res, err := s.DB.ExecContext(ctx,
		`UPDATE code_chunks SET embed_failed_at = ?, embed_last_error = ?, updated_at = ?
		  WHERE id = ?`,
		at, errMsg, at, id,
	)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("code chunk %s: %w", id, storage.ErrNotFound)
	}
	return nil
}

func (s *Store) SearchCodeChunks(
	ctx context.Context, projectID string, query []float32,
	minScore float32, limit int,
) ([]domain.CodeChunkSearchHit, error) {
	if limit <= 0 {
		limit = 20
	}
	if len(query) == 0 {
		return nil, nil
	}
	const snippetChars = 240
	rows, err := s.DB.QueryContext(ctx,
		`SELECT id, file_path, line_start, line_end,
		        substr(content, 1, ?), embedding
		   FROM code_chunks
		  WHERE project_id = ? AND embedding IS NOT NULL`,
		snippetChars, projectID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	hits := []domain.CodeChunkSearchHit{}
	for rows.Next() {
		var (
			h    domain.CodeChunkSearchHit
			blob []byte
		)
		if err := rows.Scan(&h.ID, &h.FilePath, &h.LineStart, &h.LineEnd, &h.Snippet, &blob); err != nil {
			return nil, err
		}
		vec, err := embed.DecodeFloat32(blob)
		if err != nil || len(vec) == 0 {
			continue
		}
		score := embed.Cosine(query, vec)
		if score < minScore {
			continue
		}
		h.Score = score
		hits = append(hits, h)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	sort.SliceStable(hits, func(i, j int) bool { return hits[i].Score > hits[j].Score })
	if len(hits) > limit {
		hits = hits[:limit]
	}
	return hits, nil
}

func (s *Store) CountCodeChunks(ctx context.Context, projectID string) (int, error) {
	var n int
	err := s.DB.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM code_chunks WHERE project_id = ?`, projectID,
	).Scan(&n)
	if err != nil {
		return 0, err
	}
	return n, nil
}
