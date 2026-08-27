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

// scratchpads.owner_id and scratchpads.visibility have NOT NULL DEFAULT
// from migration 0006 ('local' / 'project'). CreateScratchpad omits the
// columns when the caller hasn't set them so single-user code paths
// continue to work unchanged.

const scratchpadColumns = `id, project_id, owner_id, name, classification_mode, visibility, created_at`

func scanScratchpad(scan func(...any) error) (*domain.Scratchpad, error) {
	sp := &domain.Scratchpad{}
	if err := scan(&sp.ID, &sp.ProjectID, &sp.OwnerID, &sp.Name, &sp.ClassificationMode, &sp.Visibility, &sp.CreatedAt); err != nil {
		return nil, err
	}
	return sp, nil
}

func (s *Store) CreateScratchpad(ctx context.Context, sp *domain.Scratchpad) error {
	owner := sp.OwnerID
	if owner == "" {
		owner = "local"
	}
	vis := sp.Visibility
	if vis == "" {
		vis = "project"
	}
	_, err := s.DB.ExecContext(ctx,
		`INSERT INTO scratchpads (`+scratchpadColumns+`)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		sp.ID, sp.ProjectID, owner, sp.Name, sp.ClassificationMode, vis, sp.CreatedAt,
	)
	if err != nil {
		return wrapDupErr(err)
	}
	sp.OwnerID = owner
	sp.Visibility = vis
	return nil
}

func (s *Store) GetScratchpadByName(ctx context.Context, projectID, name string) (*domain.Scratchpad, error) {
	row := s.DB.QueryRowContext(ctx,
		`SELECT `+scratchpadColumns+` FROM scratchpads WHERE project_id = ? AND name = ?`,
		projectID, name,
	)
	sp, err := scanScratchpad(row.Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("scratchpad %q in project %q: %w", name, projectID, storage.ErrNotFound)
	}
	if err != nil {
		return nil, err
	}
	return sp, nil
}

func (s *Store) GetScratchpad(ctx context.Context, id string) (*domain.Scratchpad, error) {
	row := s.DB.QueryRowContext(ctx,
		`SELECT `+scratchpadColumns+` FROM scratchpads WHERE id = ?`, id,
	)
	sp, err := scanScratchpad(row.Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("scratchpad %s: %w", id, storage.ErrNotFound)
	}
	if err != nil {
		return nil, err
	}
	return sp, nil
}

func (s *Store) ListScratchpads(ctx context.Context, projectID string) ([]*domain.Scratchpad, error) {
	rows, err := s.DB.QueryContext(ctx,
		`SELECT `+scratchpadColumns+`
		 FROM scratchpads WHERE project_id = ? ORDER BY created_at`, projectID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*domain.Scratchpad
	for rows.Next() {
		sp, err := scanScratchpad(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, sp)
	}
	return out, rows.Err()
}

func (s *Store) UpdateScratchpad(ctx context.Context, sp *domain.Scratchpad) error {
	// owner_id is intentionally not updatable — transfer-of-ownership is a
	// separate concern and not yet exposed.
	res, err := s.DB.ExecContext(ctx,
		`UPDATE scratchpads SET name = ?, classification_mode = ?, visibility = ? WHERE id = ?`,
		sp.Name, sp.ClassificationMode, sp.Visibility, sp.ID,
	)
	if err != nil {
		return wrapDupErr(err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("scratchpad %s: %w", sp.ID, storage.ErrNotFound)
	}
	return nil
}

func (s *Store) DeleteScratchpad(ctx context.Context, id string) error {
	res, err := s.DB.ExecContext(ctx, `DELETE FROM scratchpads WHERE id = ?`, id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("scratchpad %s: %w", id, storage.ErrNotFound)
	}
	return nil
}
