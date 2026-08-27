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

const reviewDecisionColumns = `
	id, owner_type, owner_id, decision, commit_sha, reviewer, note, created_at`

func scanReviewDecision(scan func(...any) error) (*domain.ReviewDecision, error) {
	d := &domain.ReviewDecision{}
	var commitSHA, reviewer, note sql.NullString
	err := scan(&d.ID, &d.OwnerType, &d.OwnerID, &d.Decision, &commitSHA, &reviewer, &note, &d.CreatedAt)
	if err != nil {
		return nil, err
	}
	d.CommitSHA = stringOrEmpty(commitSHA)
	d.Reviewer = stringOrEmpty(reviewer)
	d.Note = stringOrEmpty(note)
	return d, nil
}

func (s *Store) CreateReviewDecision(ctx context.Context, d *domain.ReviewDecision) error {
	_, err := s.DB.ExecContext(ctx,
		`INSERT INTO review_decisions (`+reviewDecisionColumns+`)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		d.ID, d.OwnerType, d.OwnerID, d.Decision, d.CommitSHA, d.Reviewer, d.Note, d.CreatedAt,
	)
	if err != nil {
		return fmt.Errorf("create review decision: %w", err)
	}
	return nil
}

// LatestReviewDecision returns the most recent decision for an owner, or
// ErrNotFound when none has been recorded.
func (s *Store) LatestReviewDecision(ctx context.Context, ownerType, ownerID string) (*domain.ReviewDecision, error) {
	d, err := scanReviewDecision(s.DB.QueryRowContext(ctx,
		`SELECT `+reviewDecisionColumns+`
		 FROM review_decisions
		 WHERE owner_type = ? AND owner_id = ?
		 ORDER BY created_at DESC LIMIT 1`,
		ownerType, ownerID,
	).Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("review decision for %s/%s: %w", ownerType, ownerID, storage.ErrNotFound)
	}
	if err != nil {
		return nil, err
	}
	return d, nil
}
