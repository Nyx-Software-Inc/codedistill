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

	"codedistill/internal/domain"
	"codedistill/internal/storage"
)

// Workspace members.

func (s *Store) AddWorkspaceMember(ctx context.Context, m *domain.WorkspaceMember) error {
	status := m.Status
	if status == "" {
		status = domain.MemberActive
	}
	_, err := s.DB.ExecContext(ctx,
		`INSERT INTO workspace_members (workspace_id, user_id, role, status, created_at)
		 VALUES (?, ?, ?, ?, ?)`,
		m.WorkspaceID, m.UserID, m.Role, status, m.CreatedAt,
	)
	return err
}

// TryAddWorkspaceMember inserts an active member only if a licensed seat is
// free — the seat count is a subquery INSIDE the INSERT, so the check-then-act
// race (two first-logins both passing a separate COUNT and both inserting,
// overselling the license — audit M18) can't open. Returns whether the row
// landed; false means seats are full. The guard mirrors
// CountActiveWorkspaceMembers exactly (active, excluding the implicit 'local'
// identity). Caller must pass seats > 0 (an unlimited license skips the guard).
func (s *Store) TryAddWorkspaceMember(ctx context.Context, m *domain.WorkspaceMember, seats int) (bool, error) {
	status := m.Status
	if status == "" {
		status = domain.MemberActive
	}
	res, err := s.DB.ExecContext(ctx, `
		INSERT INTO workspace_members (workspace_id, user_id, role, status, created_at)
		SELECT ?, ?, ?, ?, ?
		 WHERE (SELECT COUNT(*) FROM workspace_members
		          WHERE workspace_id = ? AND status = ? AND user_id != 'local') < ?`,
		m.WorkspaceID, m.UserID, m.Role, status, m.CreatedAt,
		m.WorkspaceID, domain.MemberActive, seats)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n > 0, err
}

// TryActivateWorkspaceMember flips a non-active member to active only if a seat
// is free, atomically (same guard as TryAddWorkspaceMember). Returns whether it
// activated; false means the member was already active OR seats are full — the
// caller re-reads to tell those apart. Caller must pass seats > 0.
func (s *Store) TryActivateWorkspaceMember(ctx context.Context, workspaceID, userID string, seats int) (bool, error) {
	res, err := s.DB.ExecContext(ctx, `
		UPDATE workspace_members SET status = ?
		 WHERE workspace_id = ? AND user_id = ? AND status != ?
		   AND (SELECT COUNT(*) FROM workspace_members
		          WHERE workspace_id = ? AND status = ? AND user_id != 'local') < ?`,
		domain.MemberActive, workspaceID, userID, domain.MemberActive,
		workspaceID, domain.MemberActive, seats)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n > 0, err
}

// GetWorkspaceMember returns one member's role + status.
func (s *Store) GetWorkspaceMember(ctx context.Context, workspaceID, userID string) (*domain.WorkspaceMember, error) {
	m := &domain.WorkspaceMember{}
	err := s.DB.QueryRowContext(ctx,
		`SELECT workspace_id, user_id, role, status, created_at
		 FROM workspace_members WHERE workspace_id = ? AND user_id = ?`, workspaceID, userID).
		Scan(&m.WorkspaceID, &m.UserID, &m.Role, &m.Status, &m.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("workspace member (%s, %s): %w", workspaceID, userID, storage.ErrNotFound)
	}
	return m, nil
}

// SetWorkspaceMemberStatus changes a member's lifecycle status (activate /
// deactivate / re-activate).
func (s *Store) SetWorkspaceMemberStatus(ctx context.Context, workspaceID, userID, status string) error {
	res, err := s.DB.ExecContext(ctx,
		`UPDATE workspace_members SET status = ? WHERE workspace_id = ? AND user_id = ?`,
		status, workspaceID, userID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("workspace member (%s, %s): %w", workspaceID, userID, storage.ErrNotFound)
	}
	return nil
}

// SetWorkspaceMemberRole changes a member's role (promote/demote).
func (s *Store) SetWorkspaceMemberRole(ctx context.Context, workspaceID, userID, role string) error {
	res, err := s.DB.ExecContext(ctx,
		`UPDATE workspace_members SET role = ? WHERE workspace_id = ? AND user_id = ?`,
		role, workspaceID, userID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("workspace member (%s, %s): %w", workspaceID, userID, storage.ErrNotFound)
	}
	return nil
}

// CountActiveWorkspaceMembers counts members consuming a licensed seat (active),
// excluding the implicit 'local' bootstrap identity (a system user, not a real
// seat — see migration 0006).
func (s *Store) CountActiveWorkspaceMembers(ctx context.Context, workspaceID string) (int, error) {
	var n int
	err := s.DB.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM workspace_members
		  WHERE workspace_id = ? AND status = ? AND user_id != 'local'`,
		workspaceID, domain.MemberActive).Scan(&n)
	return n, err
}

func (s *Store) RemoveWorkspaceMember(ctx context.Context, workspaceID, userID string) error {
	res, err := s.DB.ExecContext(ctx,
		`DELETE FROM workspace_members WHERE workspace_id = ? AND user_id = ?`,
		workspaceID, userID,
	)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("workspace member (%s, %s): %w", workspaceID, userID, storage.ErrNotFound)
	}
	return nil
}

func (s *Store) ListWorkspaceMembers(ctx context.Context, workspaceID string) ([]*domain.WorkspaceMember, error) {
	rows, err := s.DB.QueryContext(ctx,
		`SELECT workspace_id, user_id, role, status, created_at
		 FROM workspace_members WHERE workspace_id = ? ORDER BY created_at`,
		workspaceID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*domain.WorkspaceMember
	for rows.Next() {
		m := &domain.WorkspaceMember{}
		if err := rows.Scan(&m.WorkspaceID, &m.UserID, &m.Role, &m.Status, &m.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// Project members.

func (s *Store) AddProjectMember(ctx context.Context, m *domain.ProjectMember) error {
	_, err := s.DB.ExecContext(ctx,
		`INSERT INTO project_members (project_id, user_id, role, created_at)
		 VALUES (?, ?, ?, ?)`,
		m.ProjectID, m.UserID, m.Role, m.CreatedAt,
	)
	return err
}

func (s *Store) RemoveProjectMember(ctx context.Context, projectID, userID string) error {
	res, err := s.DB.ExecContext(ctx,
		`DELETE FROM project_members WHERE project_id = ? AND user_id = ?`,
		projectID, userID,
	)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("project member (%s, %s): %w", projectID, userID, storage.ErrNotFound)
	}
	return nil
}

func (s *Store) ListProjectMembers(ctx context.Context, projectID string) ([]*domain.ProjectMember, error) {
	rows, err := s.DB.QueryContext(ctx,
		`SELECT project_id, user_id, role, created_at
		 FROM project_members WHERE project_id = ? ORDER BY created_at`,
		projectID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*domain.ProjectMember
	for rows.Next() {
		m := &domain.ProjectMember{}
		if err := rows.Scan(&m.ProjectID, &m.UserID, &m.Role, &m.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}
