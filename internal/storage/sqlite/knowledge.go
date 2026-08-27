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
	"database/sql"
	"errors"
	"fmt"

	"codedistill/internal/domain"
	"codedistill/internal/storage"
)

const kbColumns = `id, project_id, creator_id, source_item_id, number, title, content, visibility, created_at`

const kbColumnsRead = kbColumns + `, remote_id, sync_status, last_sync_at, last_sync_error,
	status, claimed_by, claimed_at, kind, tags`

func scanKB(scan func(...any) error) (*domain.KnowledgeEntry, error) {
	k := &domain.KnowledgeEntry{}
	var (
		sourceID   sql.NullString
		createdAt  flexTime
		lastSyncAt flexTime
		claimedAt  flexTime
		tagsRaw    string
	)
	err := scan(
		&k.ID, &k.ProjectID, &k.CreatorID, &sourceID, &k.Number, &k.Title, &k.Content, &k.Visibility, &createdAt,
		&k.RemoteID, &k.SyncStatus, &lastSyncAt, &k.LastSyncError,
		&k.Status, &k.ClaimedBy, &claimedAt, &k.Kind, &tagsRaw,
	)
	if err != nil {
		return nil, err
	}
	k.SourceItemID = stringOrEmpty(sourceID)
	k.CreatedAt = createdAt.t
	k.Tags = unmarshalTags(tagsRaw)
	k.LastSyncAt = lastSyncAt.ptr()
	k.ClaimedAt = claimedAt.ptr()
	return k, nil
}

func (s *Store) CreateKnowledgeEntry(ctx context.Context, k *domain.KnowledgeEntry) error {
	creator := k.CreatorID
	if creator == "" {
		creator = "local"
	}
	vis := k.Visibility
	if vis == "" {
		vis = "project"
	}
	if k.Number == 0 {
		if err := s.DB.QueryRowContext(ctx,
			`SELECT COALESCE(MAX(number)+1, 1) FROM knowledge_entries WHERE project_id = ?`,
			k.ProjectID,
		).Scan(&k.Number); err != nil {
			return fmt.Errorf("assign kb number: %w", err)
		}
	}
	kind := k.Kind
	if kind == "" {
		kind = "reference"
	}
	_, err := s.DB.ExecContext(ctx,
		`INSERT INTO knowledge_entries (`+kbColumns+`, kind, tags)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		k.ID, k.ProjectID, creator, nullString(k.SourceItemID), k.Number, k.Title, k.Content, vis, k.CreatedAt, kind, marshalTags(k.Tags),
	)
	if err != nil {
		return err
	}
	k.CreatorID = creator
	k.Visibility = vis
	k.Kind = kind
	if k.Status == "" {
		k.Status = "active" // matches schema default; mirrored on the input pointer for round-trip equality
	}
	if k.SyncStatus == "" {
		k.SyncStatus = "local-only"
	}
	if k.Tags == nil {
		k.Tags = []string{}
	}
	return nil
}

func (s *Store) GetKnowledgeEntry(ctx context.Context, id string) (*domain.KnowledgeEntry, error) {
	row := s.DB.QueryRowContext(ctx,
		`SELECT `+kbColumnsRead+` FROM knowledge_entries WHERE id = ?`, id,
	)
	k, err := scanKB(row.Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("kb entry %s: %w", id, storage.ErrNotFound)
	}
	if err != nil {
		return nil, err
	}
	return k, nil
}

func (s *Store) ListKnowledgeEntries(ctx context.Context, projectID string) ([]*domain.KnowledgeEntry, error) {
	rows, err := s.DB.QueryContext(ctx,
		`SELECT `+kbColumnsRead+` FROM knowledge_entries WHERE project_id = ? ORDER BY created_at`,
		projectID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*domain.KnowledgeEntry
	for rows.Next() {
		k, err := scanKB(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, k)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return s.enrichKBSourceNames(ctx, out)
}

// enrichKBSourceNames — see enrichTodoSourceNames.
func (s *Store) enrichKBSourceNames(ctx context.Context, items []*domain.KnowledgeEntry) ([]*domain.KnowledgeEntry, error) {
	ids := collectSourceIDs(items, func(k *domain.KnowledgeEntry) string { return k.SourceItemID })
	if len(ids) == 0 {
		return items, nil
	}
	names, err := s.fetchSourceNames(ctx, ids)
	if err != nil {
		return items, err
	}
	for _, k := range items {
		if n, ok := names[k.SourceItemID]; ok {
			k.SourceName = n
		}
	}
	return items, nil
}

// ListKnowledgeEntriesByScratchpad returns KB entries derived from items in a
// specific scratchpad. Manually-created entries and entries whose source was
// deleted are excluded — they have no scratchpad lineage to match.
func (s *Store) ListKnowledgeEntriesByScratchpad(ctx context.Context, scratchpadID string) ([]*domain.KnowledgeEntry, error) {
	rows, err := s.DB.QueryContext(ctx,
		`SELECT k.id, k.project_id, k.creator_id, k.source_item_id, k.number, k.title, k.content, k.visibility, k.created_at,
		        k.remote_id, k.sync_status, k.last_sync_at, k.last_sync_error,
		        k.status, k.claimed_by, k.claimed_at, k.kind, k.tags
		 FROM knowledge_entries k
		 JOIN scratchpad_items si ON k.source_item_id = si.id
		 WHERE si.scratchpad_id = ?
		 ORDER BY k.created_at`,
		scratchpadID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*domain.KnowledgeEntry
	for rows.Next() {
		k, err := scanKB(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, k)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return s.enrichKBSourceNames(ctx, out)
}

func (s *Store) UpdateKnowledgeEntry(ctx context.Context, k *domain.KnowledgeEntry) error {
	status := k.Status
	if status == "" {
		status = "active"
	}
	kind := k.Kind
	if kind == "" {
		kind = "reference"
	}
	res, err := s.DB.ExecContext(ctx,
		`UPDATE knowledge_entries SET
			source_item_id = ?, title = ?, content = ?, visibility = ?,
			status = ?, claimed_by = ?, claimed_at = ?, kind = ?, tags = ?
		 WHERE id = ?`,
		nullString(k.SourceItemID), k.Title, k.Content, k.Visibility,
		status, k.ClaimedBy, nullTimePtr(k.ClaimedAt), kind, marshalTags(k.Tags),
		k.ID,
	)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("kb entry %s: %w", k.ID, storage.ErrNotFound)
	}
	return nil
}

func (s *Store) DeleteKnowledgeEntry(ctx context.Context, id string) error {
	if err := s.DeleteCodeAnchorsForOwner(ctx, "knowledge_entry", id); err != nil {
		return err
	}
	res, err := s.DB.ExecContext(ctx, `DELETE FROM knowledge_entries WHERE id = ?`, id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("kb entry %s: %w", id, storage.ErrNotFound)
	}
	return nil
}
