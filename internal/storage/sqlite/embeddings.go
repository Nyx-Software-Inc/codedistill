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
	"fmt"
	"time"

	"codedistill/internal/storage"
)

// embeddableTextSQL maps each whitelisted table to the SELECT projection
// for its canonical embeddable text. Kept in one place so the schema for
// "what gets embedded per type" lives next to the storage layer.
//
// COALESCE handles tables where the secondary column is nullable
// (notes / description / content can legitimately be empty). The
// concatenation deliberately uses a blank line so the embedder sees a
// hard boundary between subject and body rather than a single space.
var embeddableTextSQL = map[storage.EmbeddableTable]string{
	storage.TableScratchpadItems:  `SELECT id, content FROM scratchpad_items`,
	storage.TableTodoItems:        `SELECT id, subject FROM todo_items`,
	storage.TableBugItems:         `SELECT id, subject || char(10) || char(10) || COALESCE(expected_behavior, '') || char(10) || COALESCE(actual_behavior, '') FROM bug_items`,
	storage.TableKnowledgeEntries: `SELECT id, title   || char(10) || char(10) || COALESCE(content, '') FROM knowledge_entries`,
	storage.TableUseCaseItems:     `SELECT id, subject || char(10) || char(10) || COALESCE(description, '') FROM use_case_items`,
}

// freshnessColumn names the column the unembedded predicate compares
// against. Only scratchpad_items + use_case_items carry an updated_at
// in the v1 schema; todo / bug / kb fall back to created_at (derived
// items were modeled as write-once + state-only changes, so embedded
// content can't go stale relative to a content edit today).
var freshnessColumn = map[storage.EmbeddableTable]string{
	storage.TableScratchpadItems:  "updated_at",
	storage.TableTodoItems:        "created_at",
	storage.TableBugItems:         "created_at",
	storage.TableKnowledgeEntries: "created_at",
	storage.TableUseCaseItems:     "updated_at",
}

func validEmbeddableTable(t storage.EmbeddableTable) bool {
	_, ok := embeddableTextSQL[t]
	return ok
}

// ListSearchCandidates returns every embedded row across the five item
// tables for the given project. UNION joins each table's per-type display
// shape into the SearchCandidate normalized shape; scratchpad_items
// contributes its scratchpad_id, derived items contribute the empty
// string. Title and Snippet are computed in SQL via substr() so the
// caller doesn't have to reload full rows.
//
// Embedding rows that haven't been embedded yet (NULL) are filtered out.
// Per-table size at solo-dev scale is small enough that we scan all rows
// in Go for cosine; if a project ever has tens of thousands of items,
// this query becomes the obvious thing to swap for sqlite-vec.
func (s *Store) ListSearchCandidates(
	ctx context.Context, projectID string,
) ([]storage.SearchCandidate, error) {
	const snippetChars = 200
	// For derived items (todo/bug/knowledge/use_case) the originating
	// scratchpad is best-effort: LEFT JOIN through source_item_id +
	// scratchpads. Returns ('', '') for derived items whose source was
	// deleted or that were created manually with no source. Scratchpad
	// kind rows always carry their own (id, name).
	q := fmt.Sprintf(`
		SELECT 'scratchpad_item' AS kind, si.id,
		       COALESCE(NULLIF(si.name, ''), substr(si.content, 1, 60)) AS title,
		       substr(si.content, 1, %d) AS snippet,
		       si.scratchpad_id AS scratchpad_id,
		       sp.name          AS scratchpad_name,
		       si.embedding
		  FROM scratchpad_items si
		  JOIN scratchpads sp ON si.scratchpad_id = sp.id
		 WHERE sp.project_id = ? AND si.embedding IS NOT NULL

		UNION ALL

		SELECT 'todo_item', t.id, t.subject,
		       substr(t.subject, 1, %d),
		       COALESCE(src.scratchpad_id, ''),
		       COALESCE(spt.name, ''),
		       t.embedding
		  FROM todo_items t
		  LEFT JOIN scratchpad_items src ON t.source_item_id = src.id
		  LEFT JOIN scratchpads      spt ON src.scratchpad_id = spt.id
		 WHERE t.project_id = ? AND t.embedding IS NOT NULL

		UNION ALL

		SELECT 'bug_item', b.id, b.subject,
		       substr(b.subject || char(10) || COALESCE(b.expected_behavior, '') || char(10) || COALESCE(b.actual_behavior, ''), 1, %d),
		       COALESCE(src.scratchpad_id, ''),
		       COALESCE(spt.name, ''),
		       b.embedding
		  FROM bug_items b
		  LEFT JOIN scratchpad_items src ON b.source_item_id = src.id
		  LEFT JOIN scratchpads      spt ON src.scratchpad_id = spt.id
		 WHERE b.project_id = ? AND b.embedding IS NOT NULL

		UNION ALL

		SELECT 'knowledge_entry', k.id, k.title,
		       substr(k.title || char(10) || COALESCE(k.content, ''), 1, %d),
		       COALESCE(src.scratchpad_id, ''),
		       COALESCE(spt.name, ''),
		       k.embedding
		  FROM knowledge_entries k
		  LEFT JOIN scratchpad_items src ON k.source_item_id = src.id
		  LEFT JOIN scratchpads      spt ON src.scratchpad_id = spt.id
		 WHERE k.project_id = ? AND k.embedding IS NOT NULL

		UNION ALL

		SELECT 'use_case_item', u.id,
		       COALESCE(NULLIF(u.want, ''), u.subject),
		       substr(u.subject || char(10) || COALESCE(u.description, ''), 1, %d),
		       COALESCE(src.scratchpad_id, ''),
		       COALESCE(spt.name, ''),
		       u.embedding
		  FROM use_case_items u
		  LEFT JOIN scratchpad_items src ON u.source_item_id = src.id
		  LEFT JOIN scratchpads      spt ON src.scratchpad_id = spt.id
		 WHERE u.project_id = ? AND u.embedding IS NOT NULL
	`, snippetChars, snippetChars, snippetChars, snippetChars, snippetChars)

	rows, err := s.DB.QueryContext(ctx, q,
		projectID, projectID, projectID, projectID, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []storage.SearchCandidate{}
	for rows.Next() {
		var c storage.SearchCandidate
		var kind, scratchpadID, scratchpadName string
		if err := rows.Scan(&kind, &c.ID, &c.Title, &c.Snippet, &scratchpadID, &scratchpadName, &c.Embedding); err != nil {
			return nil, err
		}
		c.Kind = storage.EmbeddableTable(kind)
		c.ScratchpadID = scratchpadID
		c.ScratchpadName = scratchpadName
		out = append(out, c)
	}
	return out, rows.Err()
}

// SetItemSimilarity stores the dedup candidate pointer + cosine score
// on the source scratchpad_item. Returns ErrNotFound if id is missing.
func (s *Store) SetItemSimilarity(ctx context.Context, id, similarToID string, score float64) error {
	res, err := s.DB.ExecContext(ctx,
		`UPDATE scratchpad_items SET similar_to_id = ?, similarity_score = ? WHERE id = ?`,
		similarToID, score, id,
	)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("scratchpad item %s: %w", id, storage.ErrNotFound)
	}
	return nil
}

// ClearItemSimilarity drops the candidate-match fields. Idempotent —
// no-op for items that already have NULL/0 values.
func (s *Store) ClearItemSimilarity(ctx context.Context, id string) error {
	res, err := s.DB.ExecContext(ctx,
		`UPDATE scratchpad_items SET similar_to_id = NULL, similarity_score = NULL WHERE id = ?`,
		id,
	)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("scratchpad item %s: %w", id, storage.ErrNotFound)
	}
	return nil
}

// GetEmbeddableText returns the canonical embeddable text for a single
// row. Mirrors the composition used by ListUnembedded so the agent (which
// embeds new + just-derived items) and the backfill (which sweeps stale
// rows) feed the embedder identical input for the same row.
func (s *Store) GetEmbeddableText(
	ctx context.Context, table storage.EmbeddableTable, id string,
) (string, error) {
	if !validEmbeddableTable(table) {
		return "", fmt.Errorf("embeddings: unknown table %q", table)
	}
	q := fmt.Sprintf(`%s WHERE id = ?`, embeddableTextSQL[table])
	var gotID, text string
	err := s.DB.QueryRowContext(ctx, q, id).Scan(&gotID, &text)
	if err != nil {
		return "", fmt.Errorf("%s %s: %w", table, id, err)
	}
	return text, nil
}

// UpdateEmbedding writes only the embedding + embedded_at columns for
// the row, leaving every other field untouched (and avoiding races with
// in-flight content updates). Empty vector is treated as "skip" — caller
// can decide to retry rather than persist a no-op.
func (s *Store) UpdateEmbedding(
	ctx context.Context, table storage.EmbeddableTable, id string,
	vector []byte, at time.Time,
) error {
	if !validEmbeddableTable(table) {
		return fmt.Errorf("embeddings: unknown table %q", table)
	}
	if len(vector) == 0 {
		return fmt.Errorf("embeddings: empty vector for %s/%s", table, id)
	}
	q := fmt.Sprintf(
		`UPDATE %s SET embedding = ?, embedded_at = ? WHERE id = ?`,
		string(table),
	)
	res, err := s.DB.ExecContext(ctx, q, vector, at, id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("%s %s: %w", table, id, storage.ErrNotFound)
	}
	return nil
}

// ListUnembedded returns rows whose embedded_at is null OR strictly
// older than the table's updated_at column. Capped at limit. Sorted by
// the updated_at column ascending so the oldest stale rows get embedded
// first (fair-share when backfilling a large queue).
func (s *Store) ListUnembedded(
	ctx context.Context, table storage.EmbeddableTable, limit int,
) ([]storage.EmbeddingTarget, error) {
	if !validEmbeddableTable(table) {
		return nil, fmt.Errorf("embeddings: unknown table %q", table)
	}
	if limit <= 0 {
		limit = 100
	}
	tsCol := freshnessColumn[table]
	q := fmt.Sprintf(
		`%s WHERE embedded_at IS NULL OR embedded_at < %s
		 ORDER BY %s ASC LIMIT ?`,
		embeddableTextSQL[table], tsCol, tsCol,
	)
	rows, err := s.DB.QueryContext(ctx, q, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]storage.EmbeddingTarget, 0, limit)
	for rows.Next() {
		var t storage.EmbeddingTarget
		if err := rows.Scan(&t.ID, &t.Text); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}
