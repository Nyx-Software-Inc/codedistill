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
	"strings"

	"codedistill/internal/domain"
	"codedistill/internal/storage"
)

// Reviews (docs/design/code-review.md, slice 5): one per changeset, its
// decisions in review_decisions under owner_type "review", and the commits a
// changeset covers so the governance gate is a join.

const reviewColumns = `id, changeset_id, project_id, status, required_approvals, author_email, created_at, updated_at`

func scanReview(scan func(...any) error) (*domain.Review, error) {
	rv := &domain.Review{}
	if err := scan(&rv.ID, &rv.ChangesetID, &rv.ProjectID, &rv.Status, &rv.RequiredApprovals, &rv.AuthorEmail, &rv.CreatedAt, &rv.UpdatedAt); err != nil {
		return nil, err
	}
	return rv, nil
}

func (s *Store) CreateReview(ctx context.Context, rv *domain.Review) error {
	_, err := s.DB.ExecContext(ctx,
		`INSERT INTO reviews (`+reviewColumns+`) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		rv.ID, rv.ChangesetID, rv.ProjectID, rv.Status, rv.RequiredApprovals, rv.AuthorEmail, rv.CreatedAt, rv.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("create review: %w", err)
	}
	return nil
}

func (s *Store) getReviewWhere(ctx context.Context, where string, arg any) (*domain.Review, error) {
	rv, err := scanReview(s.DB.QueryRowContext(ctx, `SELECT `+reviewColumns+` FROM reviews WHERE `+where, arg).Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("review (%s = %v): %w", where, arg, storage.ErrNotFound)
	}
	if err != nil {
		return nil, err
	}
	return rv, nil
}

func (s *Store) GetReview(ctx context.Context, id string) (*domain.Review, error) {
	return s.getReviewWhere(ctx, "id = ?", id)
}

func (s *Store) GetReviewByChangeset(ctx context.Context, changesetID string) (*domain.Review, error) {
	return s.getReviewWhere(ctx, "changeset_id = ?", changesetID)
}

func (s *Store) UpdateReview(ctx context.Context, rv *domain.Review) error {
	res, err := s.DB.ExecContext(ctx,
		`UPDATE reviews SET status = ?, updated_at = ? WHERE id = ?`, rv.Status, rv.UpdatedAt, rv.ID)
	if err != nil {
		return fmt.Errorf("update review: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("review %s: %w", rv.ID, storage.ErrNotFound)
	}
	return nil
}

func (s *Store) ListReviews(ctx context.Context, projectID string, statuses []string) ([]domain.Review, error) {
	q := `SELECT ` + reviewColumns + ` FROM reviews WHERE project_id = ?`
	args := []any{projectID}
	if len(statuses) > 0 {
		q += ` AND status IN (` + placeholders(len(statuses)) + `)`
		for _, st := range statuses {
			args = append(args, st)
		}
	}
	q += ` ORDER BY created_at DESC`
	rows, err := s.DB.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("list reviews: %w", err)
	}
	defer rows.Close()
	out := []domain.Review{}
	for rows.Next() {
		rv, err := scanReview(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, *rv)
	}
	return out, rows.Err()
}

func (s *Store) ListReviewDecisions(ctx context.Context, ownerType, ownerID string) ([]domain.ReviewDecision, error) {
	rows, err := s.DB.QueryContext(ctx,
		`SELECT `+reviewDecisionColumns+` FROM review_decisions
		 WHERE owner_type = ? AND owner_id = ? ORDER BY created_at DESC`, ownerType, ownerID)
	if err != nil {
		return nil, fmt.Errorf("list review decisions: %w", err)
	}
	defer rows.Close()
	out := []domain.ReviewDecision{}
	for rows.Next() {
		d, err := scanReviewDecision(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, *d)
	}
	return out, rows.Err()
}

func (s *Store) SetChangesetCommits(ctx context.Context, changesetID string, shas []string) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck
	if _, err := tx.ExecContext(ctx, `DELETE FROM changeset_commits WHERE changeset_id = ?`, changesetID); err != nil {
		return fmt.Errorf("clear changeset commits: %w", err)
	}
	for _, sha := range shas {
		if _, err := tx.ExecContext(ctx, `INSERT INTO changeset_commits (changeset_id, sha) VALUES (?, ?)`, changesetID, sha); err != nil {
			return fmt.Errorf("add changeset commit: %w", err)
		}
	}
	return tx.Commit()
}

func (s *Store) ListChangesetCommits(ctx context.Context, changesetID string) ([]string, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT sha FROM changeset_commits WHERE changeset_id = ?`, changesetID)
	if err != nil {
		return nil, fmt.Errorf("list changeset commits: %w", err)
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var sha string
		if err := rows.Scan(&sha); err != nil {
			return nil, err
		}
		out = append(out, sha)
	}
	return out, rows.Err()
}

func (s *Store) OpenReviewForCommits(ctx context.Context, projectID string, shas []string) (*domain.Review, error) {
	if len(shas) == 0 {
		return nil, storage.ErrNotFound
	}
	args := []any{projectID}
	for _, sha := range shas {
		args = append(args, sha)
	}
	cols := prefixColumns("r", reviewColumns)
	rv, err := scanReview(s.DB.QueryRowContext(ctx,
		`SELECT `+cols+` FROM reviews r
		 JOIN changeset_commits cc ON cc.changeset_id = r.changeset_id
		 WHERE r.project_id = ? AND r.status IN ('open', 'changes_requested')
		   AND cc.sha IN (`+placeholders(len(shas))+`)
		 ORDER BY r.created_at DESC LIMIT 1`, args...).Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, storage.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return rv, nil
}

// ItemsByCommits finds the todo, bug and use-case items of a project whose
// commit anchors name any of the commits.
func (s *Store) ItemsByCommits(ctx context.Context, projectID string, shas []string) ([]domain.OwnerRef, error) {
	if len(shas) == 0 {
		return []domain.OwnerRef{}, nil
	}
	in := placeholders(len(shas))
	shaArgs := make([]any, 0, len(shas))
	for _, sha := range shas {
		shaArgs = append(shaArgs, sha)
	}
	q := `
		SELECT a.owner_type, a.owner_id FROM code_anchors a JOIN todo_items t ON t.id = a.owner_id
		WHERE a.owner_type = 'todo_item' AND a.kind = 'commit' AND t.project_id = ? AND a.revision IN (` + in + `)
		UNION
		SELECT a.owner_type, a.owner_id FROM code_anchors a JOIN bug_items b ON b.id = a.owner_id
		WHERE a.owner_type = 'bug_item' AND a.kind = 'commit' AND b.project_id = ? AND a.revision IN (` + in + `)
		UNION
		SELECT a.owner_type, a.owner_id FROM code_anchors a JOIN use_case_items u ON u.id = a.owner_id
		WHERE a.owner_type = 'use_case_item' AND a.kind = 'commit' AND u.project_id = ? AND a.revision IN (` + in + `)`
	args := []any{}
	for i := 0; i < 3; i++ {
		args = append(args, projectID)
		args = append(args, shaArgs...)
	}
	rows, err := s.DB.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("items by commits: %w", err)
	}
	defer rows.Close()
	out := []domain.OwnerRef{}
	for rows.Next() {
		var ref domain.OwnerRef
		if err := rows.Scan(&ref.OwnerType, &ref.OwnerID); err != nil {
			return nil, err
		}
		out = append(out, ref)
	}
	return out, rows.Err()
}

func placeholders(n int) string {
	return strings.TrimSuffix(strings.Repeat("?, ", n), ", ")
}

// prefixColumns rewrites "a, b, c" as "p.a, p.b, p.c".
func prefixColumns(p, cols string) string {
	parts := strings.Split(cols, ",")
	for i, c := range parts {
		parts[i] = p + "." + strings.TrimSpace(c)
	}
	return strings.Join(parts, ", ")
}
