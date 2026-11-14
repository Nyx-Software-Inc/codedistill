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
	"codedistill/internal/id"
	"codedistill/internal/storage"
)

// Changesets and review comments (docs/design/code-review.md, slice 4).

const changesetColumns = `id, project_id, kind, base_sha, head_sha, ref, created_at`

func scanChangeset(scan func(...any) error) (*domain.Changeset, error) {
	cs := &domain.Changeset{}
	if err := scan(&cs.ID, &cs.ProjectID, &cs.Kind, &cs.BaseSHA, &cs.HeadSHA, &cs.Ref, &cs.CreatedAt); err != nil {
		return nil, err
	}
	return cs, nil
}

// EnsureChangeset finds the row for (project, base, head) or inserts one.
// Read-then-insert rather than an upsert: the row's id must be stable, and
// a lost race on the UNIQUE constraint is re-read rather than failed.
func (s *Store) EnsureChangeset(ctx context.Context, cs *domain.Changeset) error {
	find := func() (*domain.Changeset, error) {
		got, err := scanChangeset(s.DB.QueryRowContext(ctx,
			`SELECT `+changesetColumns+` FROM changesets
			 WHERE project_id = ? AND base_sha = ? AND head_sha = ?`,
			cs.ProjectID, cs.BaseSHA, cs.HeadSHA,
		).Scan)
		if errors.Is(err, sql.ErrNoRows) {
			return nil, storage.ErrNotFound
		}
		return got, err
	}
	got, err := find()
	if err == nil {
		*cs = *got
		return nil
	}
	if !errors.Is(err, storage.ErrNotFound) {
		return fmt.Errorf("find changeset: %w", err)
	}
	if cs.ID == "" {
		cs.ID = id.New()
	}
	_, err = s.DB.ExecContext(ctx,
		`INSERT INTO changesets (`+changesetColumns+`) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		cs.ID, cs.ProjectID, cs.Kind, cs.BaseSHA, cs.HeadSHA, cs.Ref, cs.CreatedAt,
	)
	if err != nil {
		// Lost the race to a concurrent insert: the other row is the one.
		if got, ferr := find(); ferr == nil {
			*cs = *got
			return nil
		}
		return fmt.Errorf("create changeset: %w", err)
	}
	return nil
}

// FindChangeset is the read-only half of EnsureChangeset: the row for
// (project, base, head) or ErrNotFound. A read tool must not mint rows.
func (s *Store) FindChangeset(ctx context.Context, projectID, baseSHA, headSHA string) (*domain.Changeset, error) {
	cs, err := scanChangeset(s.DB.QueryRowContext(ctx,
		`SELECT `+changesetColumns+` FROM changesets
		 WHERE project_id = ? AND base_sha = ? AND head_sha = ?`,
		projectID, baseSHA, headSHA,
	).Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("changeset %s..%s: %w", baseSHA, headSHA, storage.ErrNotFound)
	}
	if err != nil {
		return nil, err
	}
	return cs, nil
}

func (s *Store) GetChangeset(ctx context.Context, id string) (*domain.Changeset, error) {
	cs, err := scanChangeset(s.DB.QueryRowContext(ctx,
		`SELECT `+changesetColumns+` FROM changesets WHERE id = ?`, id,
	).Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("changeset %s: %w", id, storage.ErrNotFound)
	}
	if err != nil {
		return nil, err
	}
	return cs, nil
}

const reviewCommentColumns = `
	id, changeset_id, path, side, line_start, line_end, body, author,
	created_at, updated_at, resolved_at, promoted_owner_type, promoted_owner_id`

func scanReviewComment(scan func(...any) error) (*domain.ReviewComment, error) {
	c := &domain.ReviewComment{}
	var resolved sql.NullTime
	err := scan(&c.ID, &c.ChangesetID, &c.Path, &c.Side, &c.LineStart, &c.LineEnd, &c.Body, &c.Author,
		&c.CreatedAt, &c.UpdatedAt, &resolved, &c.PromotedOwnerType, &c.PromotedOwnerID)
	if err != nil {
		return nil, err
	}
	if resolved.Valid {
		t := resolved.Time
		c.ResolvedAt = &t
	}
	return c, nil
}

func (s *Store) CreateReviewComment(ctx context.Context, c *domain.ReviewComment) error {
	var resolved any
	if c.ResolvedAt != nil {
		resolved = *c.ResolvedAt
	}
	_, err := s.DB.ExecContext(ctx,
		`INSERT INTO review_comments (`+reviewCommentColumns+`)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		c.ID, c.ChangesetID, c.Path, c.Side, c.LineStart, c.LineEnd, c.Body, c.Author,
		c.CreatedAt, c.UpdatedAt, resolved, c.PromotedOwnerType, c.PromotedOwnerID,
	)
	if err != nil {
		return fmt.Errorf("create review comment: %w", err)
	}
	return nil
}

func (s *Store) GetReviewComment(ctx context.Context, id string) (*domain.ReviewComment, error) {
	c, err := scanReviewComment(s.DB.QueryRowContext(ctx,
		`SELECT `+reviewCommentColumns+` FROM review_comments WHERE id = ?`, id,
	).Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("review comment %s: %w", id, storage.ErrNotFound)
	}
	if err != nil {
		return nil, err
	}
	return c, nil
}

func (s *Store) ListReviewComments(ctx context.Context, changesetID string) ([]domain.ReviewComment, error) {
	rows, err := s.DB.QueryContext(ctx,
		`SELECT `+reviewCommentColumns+` FROM review_comments
		 WHERE changeset_id = ?
		 ORDER BY path, line_end, side, created_at`, changesetID,
	)
	if err != nil {
		return nil, fmt.Errorf("list review comments: %w", err)
	}
	defer rows.Close()
	out := []domain.ReviewComment{}
	for rows.Next() {
		c, err := scanReviewComment(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, *c)
	}
	return out, rows.Err()
}

func (s *Store) UpdateReviewComment(ctx context.Context, c *domain.ReviewComment) error {
	var resolved any
	if c.ResolvedAt != nil {
		resolved = *c.ResolvedAt
	}
	res, err := s.DB.ExecContext(ctx,
		`UPDATE review_comments
		 SET body = ?, updated_at = ?, resolved_at = ?, promoted_owner_type = ?, promoted_owner_id = ?
		 WHERE id = ?`,
		c.Body, c.UpdatedAt, resolved, c.PromotedOwnerType, c.PromotedOwnerID, c.ID,
	)
	if err != nil {
		return fmt.Errorf("update review comment: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("review comment %s: %w", c.ID, storage.ErrNotFound)
	}
	return nil
}

func (s *Store) DeleteReviewComment(ctx context.Context, id string) error {
	res, err := s.DB.ExecContext(ctx, `DELETE FROM review_comments WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete review comment: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("review comment %s: %w", id, storage.ErrNotFound)
	}
	return nil
}
