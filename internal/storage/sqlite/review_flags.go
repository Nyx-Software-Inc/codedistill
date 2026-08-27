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
	"fmt"
	"time"

	"codedistill/internal/domain"
)

func scanReviewFlag(scan func(...any) error) (*domain.ReviewFlag, error) {
	f := &domain.ReviewFlag{}
	var cleared sql.NullTime
	if err := scan(&f.ID, &f.OwnerType, &f.OwnerID, &f.Reason, &f.Source, &f.CreatedAt, &cleared); err != nil {
		return nil, err
	}
	if cleared.Valid {
		t := cleared.Time
		f.ClearedAt = &t
	}
	return f, nil
}

func (s *Store) CreateReviewFlag(ctx context.Context, f *domain.ReviewFlag) error {
	_, err := s.DB.ExecContext(ctx,
		`INSERT INTO review_flags (id, owner_type, owner_id, reason, source, created_at, cleared_at)
		 VALUES (?, ?, ?, ?, ?, ?, NULL)`,
		f.ID, f.OwnerType, f.OwnerID, f.Reason, f.Source, f.CreatedAt)
	if err != nil {
		return fmt.Errorf("create review flag: %w", err)
	}
	return nil
}

// ActiveReviewFlags returns an item's uncleared flags, newest first.
func (s *Store) ActiveReviewFlags(ctx context.Context, ownerType, ownerID string) ([]*domain.ReviewFlag, error) {
	rows, err := s.DB.QueryContext(ctx,
		`SELECT id, owner_type, owner_id, reason, source, created_at, cleared_at
		 FROM review_flags WHERE owner_type = ? AND owner_id = ? AND cleared_at IS NULL
		 ORDER BY created_at DESC`, ownerType, ownerID)
	if err != nil {
		return nil, fmt.Errorf("active review flags: %w", err)
	}
	defer rows.Close()
	out := []*domain.ReviewFlag{}
	for rows.Next() {
		f, err := scanReviewFlag(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

// ClearReviewFlags resolves all of an item's active flags as of `at` (e.g. when
// a human approves the change).
func (s *Store) ClearReviewFlags(ctx context.Context, ownerType, ownerID string, at time.Time) error {
	_, err := s.DB.ExecContext(ctx,
		`UPDATE review_flags SET cleared_at = ? WHERE owner_type = ? AND owner_id = ? AND cleared_at IS NULL`,
		at, ownerType, ownerID)
	if err != nil {
		return fmt.Errorf("clear review flags: %w", err)
	}
	return nil
}
