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

const acceptanceCriterionColumns = `
	id, owner_type, owner_id, position, text,
	verification_kind, state, provenance, satisfied_by, test_command,
	created_at, updated_at`

func scanAcceptanceCriterion(scan func(...any) error) (*domain.AcceptanceCriterion, error) {
	c := &domain.AcceptanceCriterion{}
	var satisfiedBy sql.NullString
	err := scan(
		&c.ID, &c.OwnerType, &c.OwnerID, &c.Position, &c.Text,
		&c.VerificationKind, &c.State, &c.Provenance, &satisfiedBy, &c.TestCommand,
		&c.CreatedAt, &c.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	c.SatisfiedBy = stringOrEmpty(satisfiedBy)
	return c, nil
}

func (s *Store) ListAcceptanceCriteria(ctx context.Context, ownerType, ownerID string) ([]*domain.AcceptanceCriterion, error) {
	rows, err := s.DB.QueryContext(ctx,
		`SELECT `+acceptanceCriterionColumns+`
		 FROM acceptance_criteria
		 WHERE owner_type = ? AND owner_id = ?
		 ORDER BY position ASC, created_at ASC`,
		ownerType, ownerID,
	)
	if err != nil {
		return nil, fmt.Errorf("list acceptance criteria: %w", err)
	}
	defer rows.Close()
	out := []*domain.AcceptanceCriterion{}
	for rows.Next() {
		c, err := scanAcceptanceCriterion(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (s *Store) GetAcceptanceCriterion(ctx context.Context, id string) (*domain.AcceptanceCriterion, error) {
	c, err := scanAcceptanceCriterion(s.DB.QueryRowContext(ctx,
		`SELECT `+acceptanceCriterionColumns+` FROM acceptance_criteria WHERE id = ?`, id,
	).Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("acceptance criterion %s: %w", id, storage.ErrNotFound)
	}
	if err != nil {
		return nil, err
	}
	return c, nil
}

func (s *Store) CreateAcceptanceCriterion(ctx context.Context, c *domain.AcceptanceCriterion) error {
	_, err := s.DB.ExecContext(ctx,
		`INSERT INTO acceptance_criteria (`+acceptanceCriterionColumns+`)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		c.ID, c.OwnerType, c.OwnerID, c.Position, c.Text,
		c.VerificationKind, c.State, c.Provenance, nullString(c.SatisfiedBy), c.TestCommand,
		c.CreatedAt, c.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("create acceptance criterion: %w", err)
	}
	return nil
}

func (s *Store) UpdateAcceptanceCriterion(ctx context.Context, c *domain.AcceptanceCriterion) error {
	res, err := s.DB.ExecContext(ctx,
		`UPDATE acceptance_criteria
		 SET text = ?, verification_kind = ?, state = ?, position = ?,
		     satisfied_by = ?, test_command = ?, updated_at = ?
		 WHERE id = ?`,
		c.Text, c.VerificationKind, c.State, c.Position,
		nullString(c.SatisfiedBy), c.TestCommand, c.UpdatedAt, c.ID,
	)
	if err != nil {
		return fmt.Errorf("update acceptance criterion: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("acceptance criterion %s: %w", c.ID, storage.ErrNotFound)
	}
	return nil
}

func (s *Store) DeleteAcceptanceCriterion(ctx context.Context, id string) error {
	res, err := s.DB.ExecContext(ctx, `DELETE FROM acceptance_criteria WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete acceptance criterion: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("acceptance criterion %s: %w", id, storage.ErrNotFound)
	}
	return nil
}

func (s *Store) NextAcceptanceCriterionPosition(ctx context.Context, ownerType, ownerID string) (int, error) {
	var next sql.NullInt64
	err := s.DB.QueryRowContext(ctx,
		`SELECT MAX(position) + 1 FROM acceptance_criteria WHERE owner_type = ? AND owner_id = ?`,
		ownerType, ownerID,
	).Scan(&next)
	if err != nil {
		return 0, fmt.Errorf("next acceptance criterion position: %w", err)
	}
	if !next.Valid {
		return 0, nil
	}
	return int(next.Int64), nil
}
