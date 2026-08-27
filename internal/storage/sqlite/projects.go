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

// projects.workspace_id has NOT NULL DEFAULT 'local' from migration 0006, so
// CreateProject can omit it for callers that don't set WorkspaceID and the
// implicit local workspace is used. Callers that target a specific workspace
// (multi-user mode) MUST set WorkspaceID explicitly.

const projectColumns = `id, workspace_id, name, repo_root, created_at`

func scanProject(scan func(...any) error) (*domain.Project, error) {
	p := &domain.Project{}
	var repoRoot sql.NullString
	if err := scan(&p.ID, &p.WorkspaceID, &p.Name, &repoRoot, &p.CreatedAt); err != nil {
		return nil, err
	}
	p.RepoRoot = stringOrEmpty(repoRoot)
	return p, nil
}

func (s *Store) CreateProject(ctx context.Context, p *domain.Project) error {
	wsID := p.WorkspaceID
	if wsID == "" {
		wsID = "local"
	}
	_, err := s.DB.ExecContext(ctx,
		`INSERT INTO projects (`+projectColumns+`) VALUES (?, ?, ?, ?, ?)`,
		p.ID, wsID, p.Name, nullString(p.RepoRoot), p.CreatedAt,
	)
	if err != nil {
		return wrapDupErr(err)
	}
	// Reflect the effective workspace_id back to the caller so the JSON
	// response includes it even when the caller omitted it.
	p.WorkspaceID = wsID
	return nil
}

func (s *Store) GetProject(ctx context.Context, id string) (*domain.Project, error) {
	row := s.DB.QueryRowContext(ctx,
		`SELECT `+projectColumns+` FROM projects WHERE id = ?`, id,
	)
	p, err := scanProject(row.Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("project %s: %w", id, storage.ErrNotFound)
	}
	if err != nil {
		return nil, err
	}
	return p, nil
}

func (s *Store) GetProjectByName(ctx context.Context, workspaceID, name string) (*domain.Project, error) {
	row := s.DB.QueryRowContext(ctx,
		`SELECT `+projectColumns+` FROM projects WHERE workspace_id = ? AND name = ?`,
		workspaceID, name,
	)
	p, err := scanProject(row.Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("project %q in workspace %q: %w", name, workspaceID, storage.ErrNotFound)
	}
	if err != nil {
		return nil, err
	}
	return p, nil
}

func (s *Store) ListProjects(ctx context.Context) ([]*domain.Project, error) {
	rows, err := s.DB.QueryContext(ctx,
		`SELECT `+projectColumns+` FROM projects ORDER BY created_at`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*domain.Project
	for rows.Next() {
		p, err := scanProject(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (s *Store) UpdateProject(ctx context.Context, p *domain.Project) error {
	// workspace_id is intentionally not updatable via this method — moving a
	// project between workspaces is a tenant transfer and warrants its own
	// surface (not yet built).
	res, err := s.DB.ExecContext(ctx,
		`UPDATE projects SET name = ?, repo_root = ? WHERE id = ?`,
		p.Name, nullString(p.RepoRoot), p.ID,
	)
	if err != nil {
		return wrapDupErr(err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("project %s: %w", p.ID, storage.ErrNotFound)
	}
	return nil
}

func (s *Store) DeleteProject(ctx context.Context, id string) error {
	res, err := s.DB.ExecContext(ctx, `DELETE FROM projects WHERE id = ?`, id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("project %s: %w", id, storage.ErrNotFound)
	}
	return nil
}
