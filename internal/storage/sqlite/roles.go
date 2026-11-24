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
	"sort"
	"strings"
	"time"

	"codedistill/internal/domain"
	"codedistill/internal/storage"
	"codedistill/internal/storage/dbx"
)

// Roles and permissions (docs/design/workflows-and-roles.md, Part 2).

// seatHolder is the SQL test for "this active member takes a seat": one of
// their roles lets them use the product. wm is the workspace_members alias.
const seatHolder = `EXISTS (SELECT 1 FROM member_roles mr
	JOIN role_permissions rp ON rp.role_id = mr.role_id
	WHERE mr.workspace_id = wm.workspace_id AND mr.user_id = wm.user_id AND rp.permission = 'use_product')`

// seatsTaken counts the active members taking a seat, the desktop's implicit
// 'local' user aside.
const seatsTaken = `(SELECT COUNT(*) FROM workspace_members wm
	WHERE wm.workspace_id = ? AND wm.status = 'active' AND wm.user_id != 'local' AND ` + seatHolder + `)`

// ListRoles returns every role with its permissions, built-ins first.
func (s *Store) ListRoles(ctx context.Context) ([]*domain.Role, error) {
	rows, err := s.DB.QueryContext(ctx,
		`SELECT id, name, description, builtin, created_at, updated_at FROM roles ORDER BY builtin DESC, name`)
	if err != nil {
		return nil, fmt.Errorf("list roles: %w", err)
	}
	var out []*domain.Role
	byID := map[string]*domain.Role{}
	for rows.Next() {
		r := &domain.Role{Permissions: []string{}}
		var builtin int
		if err := rows.Scan(&r.ID, &r.Name, &r.Description, &builtin, &r.CreatedAt, &r.UpdatedAt); err != nil {
			rows.Close()
			return nil, err
		}
		r.Builtin = builtin != 0
		out = append(out, r)
		byID[r.ID] = r
	}
	rows.Close()
	prows, err := s.DB.QueryContext(ctx, `SELECT role_id, permission FROM role_permissions`)
	if err != nil {
		return nil, fmt.Errorf("list role permissions: %w", err)
	}
	defer prows.Close()
	for prows.Next() {
		var id, p string
		if err := prows.Scan(&id, &p); err != nil {
			return nil, err
		}
		if r := byID[id]; r != nil {
			r.Permissions = append(r.Permissions, p)
		}
	}
	for _, r := range out {
		r.Permissions = inConsoleOrder(r.Permissions)
	}
	// The order the built-ins are explained in: owner, admin, lead, member.
	order := map[string]int{domain.RoleOwner: 0, domain.RoleAdmin: 1, domain.RoleLead: 2, domain.RoleMember: 3}
	sort.SliceStable(out, func(a, b int) bool {
		oa, aok := order[out[a].ID]
		ob, bok := order[out[b].ID]
		switch {
		case aok && bok:
			return oa < ob
		case aok != bok:
			return aok
		}
		return out[a].Name < out[b].Name
	})
	return out, prows.Err()
}

func inConsoleOrder(ps []string) []string {
	set := domain.PermSet{}
	for _, p := range ps {
		set[p] = true
	}
	return set.List()
}

// SaveRole creates or replaces a role's name, description and permissions.
// The owner role keeps every permission it must hold however it is edited, so
// an owner can never lock themselves out.
func (s *Store) SaveRole(ctx context.Context, r *domain.Role) error {
	if strings.TrimSpace(r.ID) == "" || strings.TrimSpace(r.Name) == "" {
		return fmt.Errorf("save role: id and name are required")
	}
	perms := map[string]bool{}
	for _, p := range r.Permissions {
		if !domain.ValidPermission(p) {
			return fmt.Errorf("save role: unknown permission %q", p)
		}
		perms[p] = true
	}
	if r.ID == domain.RoleOwner {
		for _, p := range domain.OwnerPermissions() {
			perms[p] = true
		}
	}
	now := time.Now().UTC()
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO roles (id, name, description, builtin, created_at, updated_at) VALUES (?, ?, ?, 0, ?, ?)
		 ON CONFLICT (id) DO UPDATE SET name = excluded.name, description = excluded.description, updated_at = excluded.updated_at`,
		r.ID, r.Name, r.Description, now, now); err != nil {
		return fmt.Errorf("save role: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM role_permissions WHERE role_id = ?`, r.ID); err != nil {
		return fmt.Errorf("save role: %w", err)
	}
	for p := range perms {
		if _, err := tx.ExecContext(ctx, `INSERT INTO role_permissions (role_id, permission) VALUES (?, ?)`, r.ID, p); err != nil {
			return fmt.Errorf("save role: %w", err)
		}
	}
	return tx.Commit()
}

// DeleteRole removes a role that is not built in, and with it every member's
// holding of it.
func (s *Store) DeleteRole(ctx context.Context, id string) error {
	res, err := s.DB.ExecContext(ctx, `DELETE FROM roles WHERE id = ? AND builtin = 0`, id)
	if err != nil {
		return fmt.Errorf("delete role: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("role %s (or built in): %w", id, storage.ErrNotFound)
	}
	return nil
}

// MemberRoles returns the roles a member holds.
func (s *Store) MemberRoles(ctx context.Context, workspaceID, userID string) ([]string, error) {
	rows, err := s.DB.QueryContext(ctx,
		`SELECT role_id FROM member_roles WHERE workspace_id = ? AND user_id = ? ORDER BY role_id`, workspaceID, userID)
	if err != nil {
		return nil, fmt.Errorf("member roles: %w", err)
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// MemberPermissions is what an active member may do: the union of their
// roles' permissions. Empty for anyone not an active member.
func (s *Store) MemberPermissions(ctx context.Context, workspaceID, userID string) (domain.PermSet, error) {
	rows, err := s.DB.QueryContext(ctx,
		`SELECT DISTINCT rp.permission
		   FROM member_roles mr
		   JOIN role_permissions rp ON rp.role_id = mr.role_id
		   JOIN workspace_members wm ON wm.workspace_id = mr.workspace_id AND wm.user_id = mr.user_id
		  WHERE mr.workspace_id = ? AND mr.user_id = ? AND wm.status = 'active'`, workspaceID, userID)
	if err != nil {
		return nil, fmt.Errorf("member permissions: %w", err)
	}
	defer rows.Close()
	set := domain.PermSet{}
	for rows.Next() {
		var p string
		if err := rows.Scan(&p); err != nil {
			return nil, err
		}
		set[p] = true
	}
	return set, rows.Err()
}

// SetMemberRoles replaces the roles a member holds. When the new roles take a
// seat the member did not take before, and the member is active, a seat must
// be free: seats > 0 is the license's count, checked inside the write so two
// changes at once cannot oversell it. Returns false when seats are full.
func (s *Store) SetMemberRoles(ctx context.Context, workspaceID, userID string, roles []string, seats int) (bool, error) {
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
	var active int
	if err := tx.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM workspace_members WHERE workspace_id = ? AND user_id = ? AND status = 'active'`,
		workspaceID, userID).Scan(&active); err != nil {
		return false, err
	}
	var hadSeat int
	if err := tx.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM workspace_members wm WHERE wm.workspace_id = ? AND wm.user_id = ? AND `+seatHolder,
		workspaceID, userID).Scan(&hadSeat); err != nil {
		return false, err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM member_roles WHERE workspace_id = ? AND user_id = ?`, workspaceID, userID); err != nil {
		return false, err
	}
	for _, r := range roles {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO member_roles (workspace_id, user_id, role_id) VALUES (?, ?, ?)`, workspaceID, userID, r); err != nil {
			return false, fmt.Errorf("set member roles: %s: %w", r, err)
		}
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE workspace_members SET role = ? WHERE workspace_id = ? AND user_id = ?`,
		legacyRole(roles), workspaceID, userID); err != nil {
		return false, err
	}
	if seats > 0 && active > 0 && hadSeat == 0 && userID != "local" {
		var nowSeat, taken int
		if err := tx.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM workspace_members wm WHERE wm.workspace_id = ? AND wm.user_id = ? AND `+seatHolder,
			workspaceID, userID).Scan(&nowSeat); err != nil {
			return false, err
		}
		if err := tx.QueryRowContext(ctx, `SELECT `+seatsTaken, workspaceID).Scan(&taken); err != nil {
			return false, err
		}
		if nowSeat > 0 && taken > seats {
			return false, nil
		}
	}
	return true, tx.Commit()
}

// legacyRole is the old single-role column, derived: owner, else admin, else
// member.
func legacyRole(roles []string) string {
	has := map[string]bool{}
	for _, r := range roles {
		has[r] = true
	}
	switch {
	case has[domain.RoleOwner]:
		return domain.RoleOwner
	case has[domain.RoleAdmin]:
		return domain.RoleAdmin
	}
	return domain.RoleMember
}

// CountSeatsTaken counts the active members taking a license seat: those
// whose roles let them use the product.
func (s *Store) CountSeatsTaken(ctx context.Context, workspaceID string) (int, error) {
	var n int
	err := s.DB.QueryRowContext(ctx, `SELECT `+seatsTaken, workspaceID).Scan(&n)
	return n, err
}

// CountActiveOwners counts the people holding the owner role and active. The
// last one cannot be deactivated or lose the role. The built-in local identity
// is the server's own (the desktop's user, and whoever holds the API token),
// not a person, and does not count: a new multi-user server has no owner until
// its configured one signs in.
func (s *Store) CountActiveOwners(ctx context.Context, workspaceID string) (int, error) {
	var n int
	err := s.DB.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM workspace_members wm
		   JOIN member_roles mr ON mr.workspace_id = wm.workspace_id AND mr.user_id = wm.user_id
		  WHERE wm.workspace_id = ? AND wm.status = 'active' AND mr.role_id = 'owner' AND wm.user_id != 'local'`, workspaceID).Scan(&n)
	return n, err
}
