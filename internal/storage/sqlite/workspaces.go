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

const workspaceColumns = `id, name, plan, seat_limit, created_at`

func scanWorkspace(scan func(...any) error) (*domain.Workspace, error) {
	w := &domain.Workspace{}
	var seat sql.NullInt64
	if err := scan(&w.ID, &w.Name, &w.Plan, &seat, &w.CreatedAt); err != nil {
		return nil, err
	}
	if seat.Valid {
		w.SeatLimit = int(seat.Int64)
	}
	return w, nil
}

func (s *Store) CreateWorkspace(ctx context.Context, w *domain.Workspace) error {
	_, err := s.DB.ExecContext(ctx,
		`INSERT INTO workspaces (`+workspaceColumns+`) VALUES (?, ?, ?, ?, ?)`,
		w.ID, w.Name, w.Plan, nullIntZero(w.SeatLimit), w.CreatedAt,
	)
	return wrapDupErr(err)
}

func (s *Store) GetWorkspace(ctx context.Context, id string) (*domain.Workspace, error) {
	row := s.DB.QueryRowContext(ctx,
		`SELECT `+workspaceColumns+` FROM workspaces WHERE id = ?`, id,
	)
	w, err := scanWorkspace(row.Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("workspace %s: %w", id, storage.ErrNotFound)
	}
	if err != nil {
		return nil, err
	}
	return w, nil
}

func (s *Store) GetWorkspaceByName(ctx context.Context, name string) (*domain.Workspace, error) {
	row := s.DB.QueryRowContext(ctx,
		`SELECT `+workspaceColumns+` FROM workspaces WHERE name = ?`, name,
	)
	w, err := scanWorkspace(row.Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("workspace named %q: %w", name, storage.ErrNotFound)
	}
	if err != nil {
		return nil, err
	}
	return w, nil
}

func (s *Store) ListWorkspaces(ctx context.Context) ([]*domain.Workspace, error) {
	rows, err := s.DB.QueryContext(ctx,
		`SELECT `+workspaceColumns+` FROM workspaces ORDER BY created_at`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*domain.Workspace
	for rows.Next() {
		w, err := scanWorkspace(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, w)
	}
	return out, rows.Err()
}

func (s *Store) UpdateWorkspace(ctx context.Context, w *domain.Workspace) error {
	res, err := s.DB.ExecContext(ctx,
		`UPDATE workspaces SET name = ?, plan = ?, seat_limit = ? WHERE id = ?`,
		w.Name, w.Plan, nullIntZero(w.SeatLimit), w.ID,
	)
	if err != nil {
		return wrapDupErr(err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("workspace %s: %w", w.ID, storage.ErrNotFound)
	}
	return nil
}

func (s *Store) DeleteWorkspace(ctx context.Context, id string) error {
	res, err := s.DB.ExecContext(ctx, `DELETE FROM workspaces WHERE id = ?`, id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("workspace %s: %w", id, storage.ErrNotFound)
	}
	return nil
}
