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
	"strings"
	"time"

	"codedistill/internal/domain"
	"codedistill/internal/storage"
)

const useCaseColumns = `
	id, project_id, creator_id, source_item_id, number, subject, description,
	role, want, why,
	status, target_release, implementation_date, commit_sha, commit_tag, due_date,
	origin, visibility, created_at, updated_at, tags, priority`

const useCaseColumnsRead = useCaseColumns + `,
	remote_id, sync_status, last_sync_at, last_sync_error,
	claimed_by, claimed_at`

// useCaseColumnsReadAliased is useCaseColumnsRead with every column prefixed
// "u." for the JOIN in ListUseCaseItemsByScratchpad, which needs the alias to
// disambiguate `id` against scratchpad_items.
//
// DERIVED, not hand-written. The hand-written copy drifted the moment a column
// was added (priority, migration 0062): the SELECT still returned 27 columns
// while scanUseCase wanted 28, and the endpoint 500'd with
// "expected 27 destination arguments in Scan, not 28". A positional column list
// duplicated in two places is the same defect this codebase has now hit five
// times; the fix is to stop having two.
var useCaseColumnsReadAliased = aliasColumns(useCaseColumnsRead, "u")

// aliasColumns prefixes each comma-separated column with alias + ".".
func aliasColumns(list, alias string) string {
	parts := strings.Split(list, ",")
	for i, c := range parts {
		parts[i] = alias + "." + strings.TrimSpace(c)
	}
	return strings.Join(parts, ", ")
}

func scanUseCase(scan func(...any) error) (*domain.UseCaseItem, error) {
	u := &domain.UseCaseItem{}
	var (
		sourceID             sql.NullString
		implDate             flexTime
		dueDate              flexTime
		createdAt, updatedAt flexTime
		tagsRaw              string
		lastSyncAt           flexTime
		claimedAt            flexTime
	)
	err := scan(
		&u.ID, &u.ProjectID, &u.CreatorID, &sourceID, &u.Number, &u.Subject, &u.Description,
		&u.Role, &u.Want, &u.Why,
		&u.Status, &u.TargetRelease, &implDate, &u.CommitSHA, &u.CommitTag, &dueDate,
		&u.Origin, &u.Visibility, &createdAt, &updatedAt, &tagsRaw, &u.Priority,
		&u.RemoteID, &u.SyncStatus, &lastSyncAt, &u.LastSyncError,
		&u.ClaimedBy, &claimedAt,
	)
	if err != nil {
		return nil, err
	}
	u.SourceItemID = stringOrEmpty(sourceID)
	u.CreatedAt = createdAt.t
	u.UpdatedAt = updatedAt.t
	u.ImplementationDate = implDate.ptr()
	u.DueDate = dueDate.ptr()
	u.Tags = unmarshalTags(tagsRaw)
	u.LastSyncAt = lastSyncAt.ptr()
	u.ClaimedAt = claimedAt.ptr()
	return u, nil
}

func (s *Store) CreateUseCaseItem(ctx context.Context, u *domain.UseCaseItem) error {
	creator := u.CreatorID
	if creator == "" {
		creator = "local"
	}
	vis := u.Visibility
	if vis == "" {
		vis = "project"
	}
	status := u.Status
	if status == "" {
		status = "open"
	}
	// Empty means "unset" to callers; the column is NOT NULL with a 'none'
	// default and a CHECK, so normalise here rather than let the insert fail.
	priority := u.Priority
	if priority == "" {
		priority = "none"
	}
	if u.Number == 0 {
		if err := s.DB.QueryRowContext(ctx,
			`SELECT COALESCE(MAX(number)+1, 1) FROM use_case_items WHERE project_id = ?`,
			u.ProjectID,
		).Scan(&u.Number); err != nil {
			return fmt.Errorf("assign use_case number: %w", err)
		}
	}
	_, err := s.DB.ExecContext(ctx,
		`INSERT INTO use_case_items (`+useCaseColumns+`)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		u.ID, u.ProjectID, creator, nullString(u.SourceItemID), u.Number, u.Subject, u.Description,
		u.Role, u.Want, u.Why,
		status, u.TargetRelease, nullTimePtr(u.ImplementationDate), u.CommitSHA, u.CommitTag, nullTimePtr(u.DueDate),
		u.Origin, vis, u.CreatedAt, u.UpdatedAt, marshalTags(u.Tags), priority,
	)
	if err != nil {
		return err
	}
	u.CreatorID = creator
	u.Visibility = vis
	u.Status = status
	if u.SyncStatus == "" {
		u.SyncStatus = "local-only"
	}
	if u.Tags == nil {
		u.Tags = []string{}
	}
	return nil
}

func (s *Store) GetUseCaseItem(ctx context.Context, id string) (*domain.UseCaseItem, error) {
	row := s.DB.QueryRowContext(ctx,
		`SELECT `+useCaseColumnsRead+` FROM use_case_items WHERE id = ?`, id,
	)
	u, err := scanUseCase(row.Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("use_case %s: %w", id, storage.ErrNotFound)
	}
	if err != nil {
		return nil, err
	}
	return u, nil
}

func (s *Store) ListUseCaseItems(ctx context.Context, projectID string) ([]*domain.UseCaseItem, error) {
	rows, err := s.DB.QueryContext(ctx,
		`SELECT `+useCaseColumnsRead+` FROM use_case_items WHERE project_id = ? ORDER BY created_at`,
		projectID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*domain.UseCaseItem
	for rows.Next() {
		u, err := scanUseCase(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return s.enrichUseCaseSourceNames(ctx, out)
}

// enrichUseCaseSourceNames — see enrichTodoSourceNames.
func (s *Store) enrichUseCaseSourceNames(ctx context.Context, items []*domain.UseCaseItem) ([]*domain.UseCaseItem, error) {
	ids := collectSourceIDs(items, func(u *domain.UseCaseItem) string { return u.SourceItemID })
	if len(ids) == 0 {
		return items, nil
	}
	names, err := s.fetchSourceNames(ctx, ids)
	if err != nil {
		return items, err
	}
	for _, u := range items {
		if n, ok := names[u.SourceItemID]; ok {
			u.SourceName = n
		}
	}
	return items, nil
}

// ListUseCaseItemsByScratchpad mirrors the todo/bug/kb pattern: returns
// only use cases derived from items in the given scratchpad. Manually-
// created use cases (no source_item_id) and orphaned ones are excluded.
func (s *Store) ListUseCaseItemsByScratchpad(ctx context.Context, scratchpadID string) ([]*domain.UseCaseItem, error) {
	rows, err := s.DB.QueryContext(ctx,
		`SELECT `+useCaseColumnsReadAliased+`
		 FROM use_case_items u
		 JOIN scratchpad_items si ON u.source_item_id = si.id
		 WHERE si.scratchpad_id = ?
		 ORDER BY u.created_at`,
		scratchpadID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*domain.UseCaseItem
	for rows.Next() {
		u, err := scanUseCase(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return s.enrichUseCaseSourceNames(ctx, out)
}

func (s *Store) UpdateUseCaseItem(ctx context.Context, u *domain.UseCaseItem) error {
	// creator_id and number intentionally not updatable — they're set once at
	// creation. Number is the per-project sequence and must stay stable so
	// external references (UC-12 in a commit message, MCP push to Linear, etc.)
	// keep resolving.
	priority := u.Priority
	if priority == "" {
		priority = "none"
	}
	res, err := s.DB.ExecContext(ctx,
		`UPDATE use_case_items SET
			source_item_id = ?, subject = ?, description = ?,
			role = ?, want = ?, why = ?,
			status = ?, target_release = ?, implementation_date = ?,
			commit_sha = ?, commit_tag = ?, due_date = ?, origin = ?, visibility = ?, updated_at = ?, tags = ?,
			priority = ?,
			claimed_by = ?, claimed_at = ?
		 WHERE id = ?`,
		nullString(u.SourceItemID), u.Subject, u.Description,
		u.Role, u.Want, u.Why,
		u.Status, u.TargetRelease, nullTimePtr(u.ImplementationDate),
		u.CommitSHA, u.CommitTag, nullTimePtr(u.DueDate), u.Origin, u.Visibility, u.UpdatedAt, marshalTags(u.Tags),
		priority,
		u.ClaimedBy, nullTimePtr(u.ClaimedAt),
		u.ID,
	)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("use_case %s: %w", u.ID, storage.ErrNotFound)
	}
	return nil
}

// ClaimUseCaseItem atomically flips status to 'in_progress' + records
// the claimer. See ClaimTodoItem for the conflict semantics.
func (s *Store) ClaimUseCaseItem(ctx context.Context, id, claimedBy string) error {
	now := time.Now().UTC()
	res, err := s.DB.ExecContext(ctx,
		`UPDATE use_case_items SET
			status = 'in_progress',
			claimed_by = ?,
			claimed_at = ?,
			updated_at = ?
		 WHERE id = ? AND (claimed_by = '' OR claimed_by = ?)`,
		claimedBy, now, now, id, claimedBy,
	)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 1 {
		return nil
	}
	var existing string
	row := s.DB.QueryRowContext(ctx, `SELECT claimed_by FROM use_case_items WHERE id = ?`, id)
	if err := row.Scan(&existing); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("use_case %s: %w", id, storage.ErrNotFound)
		}
		return err
	}
	return fmt.Errorf("use_case %s claimed by %q: %w", id, existing, storage.ErrConflict)
}

func (s *Store) DeleteUseCaseItem(ctx context.Context, id string) error {
	// One transaction: the anchor cascade and the parent delete must stand or
	// fall together. Un-transacted, a losing concurrent delete committed the
	// anchor removal and THEN returned ErrNotFound — leaving a live use case whose
	// Throughline provenance was gone, behind an error that reads as "nothing
	// happened" (audit M24; same shape as DeleteScratchpadItem).
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("delete use_case: begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if err := deleteCodeAnchorsForOwnerTx(ctx, tx, "use_case_item", id); err != nil {
		return fmt.Errorf("delete use_case anchors: %w", err)
	}
	res, err := tx.ExecContext(ctx, `DELETE FROM use_case_items WHERE id = ?`, id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("use_case %s: %w", id, storage.ErrNotFound)
	}
	return tx.Commit()
}
