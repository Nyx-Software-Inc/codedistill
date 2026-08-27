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

const useCaseColumns = `
	id, project_id, creator_id, source_item_id, number, subject, description,
	role, want, why,
	status, target_release, implementation_date, commit_sha, commit_tag, due_date,
	origin, visibility, created_at, updated_at, tags`

const useCaseColumnsRead = useCaseColumns + `,
	remote_id, sync_status, last_sync_at, last_sync_error,
	claimed_by, claimed_at`

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
		&u.Origin, &u.Visibility, &createdAt, &updatedAt, &tagsRaw,
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
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		u.ID, u.ProjectID, creator, nullString(u.SourceItemID), u.Number, u.Subject, u.Description,
		u.Role, u.Want, u.Why,
		status, u.TargetRelease, nullTimePtr(u.ImplementationDate), u.CommitSHA, u.CommitTag, nullTimePtr(u.DueDate),
		u.Origin, vis, u.CreatedAt, u.UpdatedAt, marshalTags(u.Tags),
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
		`SELECT u.id, u.project_id, u.creator_id, u.source_item_id, u.number, u.subject, u.description,
		        u.role, u.want, u.why,
		        u.status, u.target_release, u.implementation_date, u.commit_sha, u.commit_tag, u.due_date,
		        u.origin, u.visibility, u.created_at, u.updated_at, u.tags,
		        u.remote_id, u.sync_status, u.last_sync_at, u.last_sync_error,
		        u.claimed_by, u.claimed_at
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
	res, err := s.DB.ExecContext(ctx,
		`UPDATE use_case_items SET
			source_item_id = ?, subject = ?, description = ?,
			role = ?, want = ?, why = ?,
			status = ?, target_release = ?, implementation_date = ?,
			commit_sha = ?, commit_tag = ?, due_date = ?, origin = ?, visibility = ?, updated_at = ?, tags = ?,
			claimed_by = ?, claimed_at = ?
		 WHERE id = ?`,
		nullString(u.SourceItemID), u.Subject, u.Description,
		u.Role, u.Want, u.Why,
		u.Status, u.TargetRelease, nullTimePtr(u.ImplementationDate),
		u.CommitSHA, u.CommitTag, nullTimePtr(u.DueDate), u.Origin, u.Visibility, u.UpdatedAt, marshalTags(u.Tags),
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
	if err := s.DeleteCodeAnchorsForOwner(ctx, "use_case_item", id); err != nil {
		return err
	}
	res, err := s.DB.ExecContext(ctx, `DELETE FROM use_case_items WHERE id = ?`, id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("use_case %s: %w", id, storage.ErrNotFound)
	}
	return nil
}
