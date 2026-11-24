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
	"codedistill/internal/storage/dbx"
)

// Workspace members.

func (s *Store) AddWorkspaceMember(ctx context.Context, m *domain.WorkspaceMember) error {
	_, err := s.addMember(ctx, m, 0)
	return err
}

// TryAddWorkspaceMember adds a member only if, when their roles take a seat
// (they may use the product), a licensed seat is free. The count is made
// inside the same write, under a lock, so two first sign-ins cannot both pass
// it and oversell the license (audit M18). Returns whether the member landed;
// false means seats are full. Caller passes seats > 0 (an unlimited license
// skips the guard).
func (s *Store) TryAddWorkspaceMember(ctx context.Context, m *domain.WorkspaceMember, seats int) (bool, error) {
	return s.addMember(ctx, m, seats)
}

// memberRoles is what a member is given: their Roles, or their legacy Role
// for a caller that names only that.
func memberRoles(m *domain.WorkspaceMember) []string {
	if len(m.Roles) > 0 {
		return m.Roles
	}
	switch m.Role {
	case domain.RoleOwner:
		return []string{domain.RoleOwner, domain.RoleMember}
	case domain.RoleAdmin:
		return []string{domain.RoleAdmin, domain.RoleLead}
	}
	return []string{domain.RoleMember}
}

func (s *Store) addMember(ctx context.Context, m *domain.WorkspaceMember, seats int) (bool, error) {
	status := m.Status
	if status == "" {
		status = domain.MemberActive
	}
	roles := memberRoles(m)
	return s.seatGuarded(ctx, m.WorkspaceID, m.UserID, seats, func(tx *dbx.Tx) error {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO workspace_members (workspace_id, user_id, role, status, created_at)
			 VALUES (?, ?, ?, ?, ?)`,
			m.WorkspaceID, m.UserID, legacyRole(roles), status, m.CreatedAt); err != nil {
			return err
		}
		for _, r := range roles {
			if _, err := tx.ExecContext(ctx,
				`INSERT INTO member_roles (workspace_id, user_id, role_id) VALUES (?, ?, ?)`,
				m.WorkspaceID, m.UserID, r); err != nil {
				return fmt.Errorf("member role %s: %w", r, err)
			}
		}
		return nil
	})
}

// seatGuarded runs a write that may give a member a seat, and undoes it when
// that leaves more seats taken than the license has (seats > 0). On Postgres,
// READ COMMITTED lets concurrent writes each count the seats as they were
// before the others committed (measured: 8 members under a 2-seat license),
// so a per-workspace advisory lock, held until the transaction ends, makes
// them take turns; on SQLite one writer at a time already holds.
func (s *Store) seatGuarded(ctx context.Context, workspaceID, userID string, seats int, write func(tx *dbx.Tx) error) (bool, error) {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	if s.DB.Dialect() == dbx.Postgres {
		if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtext(?))`, "workspace_seats:"+workspaceID); err != nil {
			return false, err
		}
	}
	if err := write(tx); err != nil {
		return false, err
	}
	if seats > 0 && userID != "local" {
		var mine, taken int
		if err := tx.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM workspace_members wm
			  WHERE wm.workspace_id = ? AND wm.user_id = ? AND wm.status = 'active' AND `+seatHolder,
			workspaceID, userID).Scan(&mine); err != nil {
			return false, err
		}
		if err := tx.QueryRowContext(ctx, `SELECT `+seatsTaken, workspaceID).Scan(&taken); err != nil {
			return false, err
		}
		if mine > 0 && taken > seats {
			return false, nil
		}
	}
	return true, tx.Commit()
}

// TryActivateWorkspaceMember makes a non-active member active, if their roles
// take a seat only while one is free (the same guard as adding). Returns
// whether it activated; false means the member was already active OR seats
// are full: the caller re-reads to tell those apart. Caller passes seats > 0.
func (s *Store) TryActivateWorkspaceMember(ctx context.Context, workspaceID, userID string, seats int) (bool, error) {
	changed := false
	ok, err := s.seatGuarded(ctx, workspaceID, userID, seats, func(tx *dbx.Tx) error {
		res, err := tx.ExecContext(ctx,
			`UPDATE workspace_members SET status = ? WHERE workspace_id = ? AND user_id = ? AND status != ?`,
			domain.MemberActive, workspaceID, userID, domain.MemberActive)
		if err != nil {
			return err
		}
		n, _ := res.RowsAffected()
		changed = n > 0
		return nil
	})
	return ok && changed, err
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
	if m.Roles, err = s.MemberRoles(ctx, workspaceID, userID); err != nil {
		return nil, err
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

// SetWorkspaceMemberRole gives a member the roles a legacy single role stood
// for (owner: Owner and Member; admin: Admin and Lead; member: Member). Seats
// are not checked; SetMemberRoles is the guarded way.
func (s *Store) SetWorkspaceMemberRole(ctx context.Context, workspaceID, userID, role string) error {
	if _, err := s.GetWorkspaceMember(ctx, workspaceID, userID); err != nil {
		return err
	}
	_, err := s.SetMemberRoles(ctx, workspaceID, userID, memberRoles(&domain.WorkspaceMember{Role: role}), 0)
	return err
}

// CountActiveWorkspaceMembers counts the members taking a licensed seat:
// active, allowed to use the product, the implicit 'local' identity aside.
func (s *Store) CountActiveWorkspaceMembers(ctx context.Context, workspaceID string) (int, error) {
	return s.CountSeatsTaken(ctx, workspaceID)
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
	if err := rows.Err(); err != nil {
		return nil, err
	}
	rows.Close()
	for _, m := range out {
		var err error
		if m.Roles, err = s.MemberRoles(ctx, workspaceID, m.UserID); err != nil {
			return nil, err
		}
	}
	return out, nil
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
