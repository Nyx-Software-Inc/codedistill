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

const bugColumns = `
	id, project_id, creator_id, source_item_id, number, subject,
	severity, status, steps_to_reproduce, expected_behavior, actual_behavior,
	environment, affected_component, origin, visibility, created_at,
	completed_at, commit_sha, commit_tag, due_date, tags`

const bugColumnsRead = bugColumns + `,
	remote_id, sync_status, last_sync_at, last_sync_error,
	claimed_by, claimed_at`

func scanBug(scan func(...any) error) (*domain.BugItem, error) {
	b := &domain.BugItem{}
	var (
		sourceID                           sql.NullString
		steps, expected, actual, env, comp sql.NullString
		createdAt                                 flexTime
		completedAt                               flexTime
		commitSHA, commitTag                      sql.NullString
		dueDate                                   flexTime
		tagsRaw                                   string
		lastSyncAt                                flexTime
		claimedAt                                 flexTime
	)
	err := scan(
		&b.ID, &b.ProjectID, &b.CreatorID, &sourceID, &b.Number, &b.Subject,
		&b.Severity, &b.Status, &steps, &expected, &actual,
		&env, &comp, &b.Origin, &b.Visibility, &createdAt,
		&completedAt, &commitSHA, &commitTag, &dueDate, &tagsRaw,
		&b.RemoteID, &b.SyncStatus, &lastSyncAt, &b.LastSyncError,
		&b.ClaimedBy, &claimedAt,
	)
	if err != nil {
		return nil, err
	}
	b.SourceItemID = stringOrEmpty(sourceID)
	b.StepsToReproduce = stringOrEmpty(steps)
	b.ExpectedBehavior = stringOrEmpty(expected)
	b.ActualBehavior = stringOrEmpty(actual)
	b.Environment = stringOrEmpty(env)
	b.AffectedComponent = stringOrEmpty(comp)
	b.CreatedAt = createdAt.t
	b.CompletedAt = completedAt.ptr()
	b.CommitSHA = stringOrEmpty(commitSHA)
	b.CommitTag = stringOrEmpty(commitTag)
	b.DueDate = dueDate.ptr()
	b.Tags = unmarshalTags(tagsRaw)
	b.LastSyncAt = lastSyncAt.ptr()
	b.ClaimedAt = claimedAt.ptr()
	return b, nil
}

func (s *Store) CreateBugItem(ctx context.Context, b *domain.BugItem) error {
	creator := b.CreatorID
	if creator == "" {
		creator = "local"
	}
	vis := b.Visibility
	if vis == "" {
		vis = "project"
	}
	if b.Number == 0 {
		if err := s.DB.QueryRowContext(ctx,
			`SELECT COALESCE(MAX(number)+1, 1) FROM bug_items WHERE project_id = ?`,
			b.ProjectID,
		).Scan(&b.Number); err != nil {
			return fmt.Errorf("assign bug number: %w", err)
		}
	}
	_, err := s.DB.ExecContext(ctx,
		`INSERT INTO bug_items (`+bugColumns+`)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		b.ID, b.ProjectID, creator, nullString(b.SourceItemID), b.Number, b.Subject,
		b.Severity, b.Status, nullString(b.StepsToReproduce), nullString(b.ExpectedBehavior), nullString(b.ActualBehavior),
		nullString(b.Environment), nullString(b.AffectedComponent), b.Origin, vis, b.CreatedAt,
		nullTimePtr(b.CompletedAt), nullString(b.CommitSHA), nullString(b.CommitTag), nullTimePtr(b.DueDate), marshalTags(b.Tags),
	)
	if err != nil {
		return err
	}
	b.CreatorID = creator
	b.Visibility = vis
	if b.SyncStatus == "" {
		b.SyncStatus = "local-only"
	}
	if b.Tags == nil {
		b.Tags = []string{}
	}
	return nil
}

func (s *Store) GetBugItem(ctx context.Context, id string) (*domain.BugItem, error) {
	row := s.DB.QueryRowContext(ctx,
		`SELECT `+bugColumnsRead+` FROM bug_items WHERE id = ?`, id,
	)
	b, err := scanBug(row.Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("bug %s: %w", id, storage.ErrNotFound)
	}
	if err != nil {
		return nil, err
	}
	return b, nil
}

func (s *Store) ListBugItems(ctx context.Context, projectID string) ([]*domain.BugItem, error) {
	rows, err := s.DB.QueryContext(ctx,
		`SELECT `+bugColumnsRead+` FROM bug_items WHERE project_id = ? ORDER BY created_at`,
		projectID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*domain.BugItem
	for rows.Next() {
		b, err := scanBug(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return s.enrichBugSourceNames(ctx, out)
}

// enrichBugSourceNames — see enrichTodoSourceNames.
func (s *Store) enrichBugSourceNames(ctx context.Context, items []*domain.BugItem) ([]*domain.BugItem, error) {
	ids := collectSourceIDs(items, func(b *domain.BugItem) string { return b.SourceItemID })
	if len(ids) == 0 {
		return items, nil
	}
	names, err := s.fetchSourceNames(ctx, ids)
	if err != nil {
		return items, err
	}
	for _, b := range items {
		if n, ok := names[b.SourceItemID]; ok {
			b.SourceName = n
		}
	}
	return items, nil
}

// ListBugItemsByScratchpad returns bugs derived from items in a specific
// scratchpad. Manually-created bugs and bugs whose source was deleted are
// excluded — they have no scratchpad lineage to match.
func (s *Store) ListBugItemsByScratchpad(ctx context.Context, scratchpadID string) ([]*domain.BugItem, error) {
	rows, err := s.DB.QueryContext(ctx,
		`SELECT b.id, b.project_id, b.creator_id, b.source_item_id, b.number, b.subject,
		        b.severity, b.status, b.steps_to_reproduce, b.expected_behavior,
		        b.actual_behavior, b.environment, b.affected_component,
		        b.origin, b.visibility, b.created_at,
		        b.completed_at, b.commit_sha, b.commit_tag, b.due_date, b.tags,
		        b.remote_id, b.sync_status, b.last_sync_at, b.last_sync_error,
		        b.claimed_by, b.claimed_at
		 FROM bug_items b
		 JOIN scratchpad_items si ON b.source_item_id = si.id
		 WHERE si.scratchpad_id = ?
		 ORDER BY b.created_at`,
		scratchpadID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*domain.BugItem
	for rows.Next() {
		b, err := scanBug(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return s.enrichBugSourceNames(ctx, out)
}

func (s *Store) UpdateBugItem(ctx context.Context, b *domain.BugItem) error {
	res, err := s.DB.ExecContext(ctx,
		`UPDATE bug_items SET
			source_item_id = ?, subject = ?,
			severity = ?, status = ?, steps_to_reproduce = ?, expected_behavior = ?, actual_behavior = ?,
			environment = ?, affected_component = ?, origin = ?, visibility = ?,
			completed_at = ?, commit_sha = ?, commit_tag = ?, due_date = ?, tags = ?,
			claimed_by = ?, claimed_at = ?
		 WHERE id = ?`,
		nullString(b.SourceItemID), b.Subject,
		b.Severity, b.Status, nullString(b.StepsToReproduce), nullString(b.ExpectedBehavior), nullString(b.ActualBehavior),
		nullString(b.Environment), nullString(b.AffectedComponent), b.Origin, b.Visibility,
		nullTimePtr(b.CompletedAt), nullString(b.CommitSHA), nullString(b.CommitTag), nullTimePtr(b.DueDate), marshalTags(b.Tags),
		b.ClaimedBy, nullTimePtr(b.ClaimedAt),
		b.ID,
	)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("bug %s: %w", b.ID, storage.ErrNotFound)
	}
	return nil
}

// ClaimBugItem atomically flips status to 'in-progress' (note the
// hyphen — bug_items uses the legacy spelling from migration 0001) +
// records the claimer. See ClaimTodoItem for the conflict semantics.
func (s *Store) ClaimBugItem(ctx context.Context, id, claimedBy string) error {
	now := time.Now().UTC()
	res, err := s.DB.ExecContext(ctx,
		`UPDATE bug_items SET
			status = 'in-progress',
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
	var existing string
	row := s.DB.QueryRowContext(ctx, `SELECT claimed_by FROM bug_items WHERE id = ?`, id)
	if err := row.Scan(&existing); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("bug %s: %w", id, storage.ErrNotFound)
		}
		return err
	}
	return fmt.Errorf("bug %s claimed by %q: %w", id, existing, storage.ErrConflict)
}

func (s *Store) DeleteBugItem(ctx context.Context, id string) error {
	if err := s.DeleteCodeAnchorsForOwner(ctx, "bug_item", id); err != nil {
		return err
	}
	res, err := s.DB.ExecContext(ctx, `DELETE FROM bug_items WHERE id = ?`, id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("bug %s: %w", id, storage.ErrNotFound)
	}
	return nil
}
