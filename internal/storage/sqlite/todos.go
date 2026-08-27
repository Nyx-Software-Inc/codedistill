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
	"time"

	"codedistill/internal/domain"
	"codedistill/internal/storage"
)

// todoColumns lists the writable columns; reused for INSERT.
// todoColumnsRead extends it with the v0.8.2 sync columns (migration
// 0013) plus the v0.8.3 claim columns (migration 0021) so SELECT path
// surfaces both to the API + MCP responses. INSERT keeps using
// todoColumns alone — sync + claim columns have schema defaults.
const todoColumns = `
	id, project_id, creator_id, source_item_id, number, subject,
	priority, status, origin, visibility, created_at, completed_at,
	commit_sha, commit_tag, due_date, tags`

const todoColumnsRead = todoColumns + `,
	remote_id, sync_status, last_sync_at, last_sync_error,
	claimed_by, claimed_at`

func scanTodo(scan func(...any) error) (*domain.TodoItem, error) {
	t := &domain.TodoItem{}
	var (
		sourceID             sql.NullString
		createdAt            flexTime
		completedAt          flexTime
		commitSHA, commitTag sql.NullString
		dueDate              flexTime
		tagsRaw              string
		lastSyncAt           flexTime
		claimedAt            flexTime
	)
	err := scan(
		&t.ID, &t.ProjectID, &t.CreatorID, &sourceID, &t.Number, &t.Subject,
		&t.Priority, &t.Status, &t.Origin, &t.Visibility, &createdAt, &completedAt,
		&commitSHA, &commitTag, &dueDate, &tagsRaw,
		&t.RemoteID, &t.SyncStatus, &lastSyncAt, &t.LastSyncError,
		&t.ClaimedBy, &claimedAt,
	)
	if err != nil {
		return nil, err
	}
	t.SourceItemID = stringOrEmpty(sourceID)
	t.CreatedAt = createdAt.t
	t.CompletedAt = completedAt.ptr()
	t.CommitSHA = stringOrEmpty(commitSHA)
	t.CommitTag = stringOrEmpty(commitTag)
	t.DueDate = dueDate.ptr()
	t.Tags = unmarshalTags(tagsRaw)
	t.LastSyncAt = lastSyncAt.ptr()
	t.ClaimedAt = claimedAt.ptr()
	return t, nil
}

func (s *Store) CreateTodoItem(ctx context.Context, t *domain.TodoItem) error {
	creator := t.CreatorID
	if creator == "" {
		creator = "local"
	}
	vis := t.Visibility
	if vis == "" {
		vis = "project"
	}
	if t.Number == 0 {
		if err := s.DB.QueryRowContext(ctx,
			`SELECT COALESCE(MAX(number)+1, 1) FROM todo_items WHERE project_id = ?`,
			t.ProjectID,
		).Scan(&t.Number); err != nil {
			return fmt.Errorf("assign todo number: %w", err)
		}
	}
	_, err := s.DB.ExecContext(ctx,
		`INSERT INTO todo_items (`+todoColumns+`)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		t.ID, t.ProjectID, creator, nullString(t.SourceItemID), t.Number, t.Subject,
		t.Priority, t.Status, t.Origin, vis, t.CreatedAt, nullTimePtr(t.CompletedAt),
		nullString(t.CommitSHA), nullString(t.CommitTag), nullTimePtr(t.DueDate), marshalTags(t.Tags),
	)
	if err != nil {
		return err
	}
	t.CreatorID = creator
	t.Visibility = vis
	if t.SyncStatus == "" {
		t.SyncStatus = "local-only" // matches schema default
	}
	if t.Tags == nil {
		t.Tags = []string{} // tags are never null (matches the round-tripped value)
	}
	return nil
}

func (s *Store) GetTodoItem(ctx context.Context, id string) (*domain.TodoItem, error) {
	row := s.DB.QueryRowContext(ctx,
		`SELECT `+todoColumnsRead+` FROM todo_items WHERE id = ?`, id,
	)
	t, err := scanTodo(row.Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("todo %s: %w", id, storage.ErrNotFound)
	}
	if err != nil {
		return nil, err
	}
	return t, nil
}

func (s *Store) ListTodoItems(ctx context.Context, projectID string) ([]*domain.TodoItem, error) {
	rows, err := s.DB.QueryContext(ctx,
		`SELECT `+todoColumnsRead+` FROM todo_items WHERE project_id = ? ORDER BY created_at`,
		projectID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*domain.TodoItem
	for rows.Next() {
		t, err := scanTodo(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return s.enrichTodoSourceNames(ctx, out)
}

// enrichTodoSourceNames populates each todo's SourceName from its
// originating scratchpad_item.name (when set). Cheap: one extra SELECT
// per list call, batched via IN. Skips when no items carry a
// source_item_id.
func (s *Store) enrichTodoSourceNames(ctx context.Context, items []*domain.TodoItem) ([]*domain.TodoItem, error) {
	ids := collectSourceIDs(items, func(t *domain.TodoItem) string { return t.SourceItemID })
	if len(ids) == 0 {
		return items, nil
	}
	names, err := s.fetchSourceNames(ctx, ids)
	if err != nil {
		return items, err
	}
	for _, t := range items {
		if n, ok := names[t.SourceItemID]; ok {
			t.SourceName = n
		}
	}
	return items, nil
}

// ListTodoItemsByScratchpad returns todos derived from items in a specific
// scratchpad. Manually-created todos (no source_item_id) and todos whose source
// was deleted (source_item_id nulled by ON DELETE SET NULL) are excluded — they
// have no scratchpad lineage to match.
func (s *Store) ListTodoItemsByScratchpad(ctx context.Context, scratchpadID string) ([]*domain.TodoItem, error) {
	rows, err := s.DB.QueryContext(ctx,
		`SELECT t.id, t.project_id, t.creator_id, t.source_item_id, t.number, t.subject,
		        t.priority, t.status, t.origin, t.visibility, t.created_at, t.completed_at,
		        t.commit_sha, t.commit_tag, t.due_date, t.tags,
		        t.remote_id, t.sync_status, t.last_sync_at, t.last_sync_error,
		        t.claimed_by, t.claimed_at
		 FROM todo_items t
		 JOIN scratchpad_items si ON t.source_item_id = si.id
		 WHERE si.scratchpad_id = ?
		 ORDER BY t.created_at`,
		scratchpadID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*domain.TodoItem
	for rows.Next() {
		t, err := scanTodo(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return s.enrichTodoSourceNames(ctx, out)
}

func (s *Store) UpdateTodoItem(ctx context.Context, t *domain.TodoItem) error {
	// creator_id is intentionally not updatable — derived items keep the
	// creator (or "local") they were tagged with at creation time.
	res, err := s.DB.ExecContext(ctx,
		`UPDATE todo_items SET
			source_item_id = ?, subject = ?,
			priority = ?, status = ?, origin = ?, visibility = ?, completed_at = ?,
			commit_sha = ?, commit_tag = ?, due_date = ?, tags = ?,
			claimed_by = ?, claimed_at = ?
		 WHERE id = ?`,
		nullString(t.SourceItemID), t.Subject,
		t.Priority, t.Status, t.Origin, t.Visibility, nullTimePtr(t.CompletedAt),
		nullString(t.CommitSHA), nullString(t.CommitTag), nullTimePtr(t.DueDate), marshalTags(t.Tags),
		t.ClaimedBy, nullTimePtr(t.ClaimedAt),
		t.ID,
	)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("todo %s: %w", t.ID, storage.ErrNotFound)
	}
	return nil
}

// ClaimTodoItem atomically flips status to in_progress + records the
// claimer. Allowed when the item is unclaimed (claimed_by = ”) OR
// already claimed by the same `claimedBy` (idempotent re-claim). On
// conflict (different existing claimer), returns storage.ErrConflict;
// the API handler should look up the existing claimer to compose the
// 409 response. Returns ErrNotFound if the id doesn't exist.
func (s *Store) ClaimTodoItem(ctx context.Context, id, claimedBy string) error {
	now := time.Now().UTC()
	res, err := s.DB.ExecContext(ctx,
		`UPDATE todo_items SET
			status = 'in_progress',
			claimed_by = ?,
			claimed_at = ?
		 WHERE id = ? AND (claimed_by = '' OR claimed_by = ?)`,
		claimedBy, now, id, claimedBy,
	)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 1 {
		return nil
	}
	// Disambiguate not-found vs already-claimed-by-someone-else.
	var existing string
	row := s.DB.QueryRowContext(ctx, `SELECT claimed_by FROM todo_items WHERE id = ?`, id)
	if err := row.Scan(&existing); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("todo %s: %w", id, storage.ErrNotFound)
		}
		return err
	}
	return fmt.Errorf("todo %s claimed by %q: %w", id, existing, storage.ErrConflict)
}

func (s *Store) DeleteTodoItem(ctx context.Context, id string) error {
	if err := s.DeleteCodeAnchorsForOwner(ctx, "todo_item", id); err != nil {
		return err
	}
	res, err := s.DB.ExecContext(ctx, `DELETE FROM todo_items WHERE id = ?`, id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("todo %s: %w", id, storage.ErrNotFound)
	}
	return nil
}
