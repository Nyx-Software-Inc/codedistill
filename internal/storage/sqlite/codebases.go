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

const codebaseColumns = `id, project_id, name, repo_root, created_at`

func scanCodebase(scan func(...any) error) (*domain.Codebase, error) {
	cb := &domain.Codebase{}
	if err := scan(&cb.ID, &cb.ProjectID, &cb.Name, &cb.RepoRoot, &cb.CreatedAt); err != nil {
		return nil, err
	}
	return cb, nil
}

func (s *Store) CreateCodebase(ctx context.Context, cb *domain.Codebase) error {
	_, err := s.DB.ExecContext(ctx,
		`INSERT INTO codebases (`+codebaseColumns+`) VALUES (?, ?, ?, ?, ?)`,
		cb.ID, cb.ProjectID, cb.Name, cb.RepoRoot, cb.CreatedAt,
	)
	return wrapDupErr(err)
}

func (s *Store) GetCodebase(ctx context.Context, id string) (*domain.Codebase, error) {
	row := s.DB.QueryRowContext(ctx,
		`SELECT `+codebaseColumns+` FROM codebases WHERE id = ?`, id,
	)
	cb, err := scanCodebase(row.Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("codebase %s: %w", id, storage.ErrNotFound)
	}
	if err != nil {
		return nil, err
	}
	return cb, nil
}

func (s *Store) ListCodebases(ctx context.Context, projectID string) ([]*domain.Codebase, error) {
	rows, err := s.DB.QueryContext(ctx,
		`SELECT `+codebaseColumns+` FROM codebases WHERE project_id = ? ORDER BY created_at`,
		projectID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*domain.Codebase
	for rows.Next() {
		cb, err := scanCodebase(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, cb)
	}
	return out, rows.Err()
}

func (s *Store) UpdateCodebase(ctx context.Context, cb *domain.Codebase) error {
	res, err := s.DB.ExecContext(ctx,
		`UPDATE codebases SET name = ?, repo_root = ? WHERE id = ?`,
		cb.Name, cb.RepoRoot, cb.ID,
	)
	if err != nil {
		return wrapDupErr(err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("codebase %s: %w", cb.ID, storage.ErrNotFound)
	}
	return nil
}

func (s *Store) DeleteCodebase(ctx context.Context, id string) error {
	res, err := s.DB.ExecContext(ctx, `DELETE FROM codebases WHERE id = ?`, id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("codebase %s: %w", id, storage.ErrNotFound)
	}
	return nil
}
