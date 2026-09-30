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
	"encoding/json"
	"fmt"

	"codedistill/internal/domain"
)

// CreateDependency records an edge between two pieces of work.
//
// Self-edges and duplicates are refused rather than stored: an item that
// depends on itself makes the whole graph unorderable, and the UNIQUE index
// would reject the duplicate anyway with a message nobody can act on.
func (s *Store) CreateDependency(ctx context.Context, d *domain.ItemDependency) error {
	if !domain.ValidItemKind(d.FromKind) || !domain.ValidItemKind(d.ToKind) {
		return fmt.Errorf("create dependency: unknown item kind")
	}
	if !domain.ValidDependencyKind(d.Kind) {
		return fmt.Errorf("create dependency: unknown kind %q", d.Kind)
	}
	if d.FromKind == d.ToKind && d.FromID == d.ToID {
		return fmt.Errorf("create dependency: an item cannot depend on itself")
	}
	lines, _ := json.Marshal(d.Lines)
	if d.Status == "" {
		d.Status = domain.EdgeAccepted
	}
	_, err := s.DB.ExecContext(ctx,
		`INSERT INTO item_dependencies
		   (id, project_id, from_kind, from_id, to_kind, to_id, kind,
		    rationale, lines, origin, created_at, created_by, status, job_id)
		 VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?)
		 ON CONFLICT(from_kind, from_id, to_kind, to_id) DO NOTHING`,
		d.ID, d.ProjectID, d.FromKind, d.FromID, d.ToKind, d.ToID, d.Kind,
		d.Rationale, string(lines), d.Origin, d.CreatedAt, nullable(d.CreatedBy),
		d.Status, nullable(d.JobID))
	if err != nil {
		return fmt.Errorf("create dependency: %w", err)
	}
	return nil
}

func (s *Store) DeleteDependency(ctx context.Context, id string) error {
	_, err := s.DB.ExecContext(ctx, `DELETE FROM item_dependencies WHERE id = ?`, id)
	return err
}

// ListDependencies returns every edge in a project.
//
// The whole graph rather than per-item neighbours: answering "what can I start"
// needs all of it, and a project's edge count is bounded by its item count.
func (s *Store) ListDependencies(ctx context.Context, projectID string) ([]*domain.ItemDependency, error) {
	rows, err := s.DB.QueryContext(ctx,
		`SELECT id, project_id, from_kind, from_id, to_kind, to_id, kind,
		        rationale, lines, origin, created_at, created_by, status,
		        COALESCE(job_id,'')
		   FROM item_dependencies WHERE project_id = ?`, projectID)
	if err != nil {
		return nil, fmt.Errorf("list dependencies: %w", err)
	}
	defer rows.Close()

	var out []*domain.ItemDependency
	for rows.Next() {
		d := &domain.ItemDependency{}
		var lines string
		var by sql.NullString
		if err := rows.Scan(&d.ID, &d.ProjectID, &d.FromKind, &d.FromID,
			&d.ToKind, &d.ToID, &d.Kind, &d.Rationale, &lines,
			&d.Origin, &d.CreatedAt, &by, &d.Status, &d.JobID); err != nil {
			return nil, err
		}
		d.CreatedBy = by.String
		if lines != "" {
			_ = json.Unmarshal([]byte(lines), &d.Lines)
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// SetDependencyStatus records a human's verdict on a proposed edge.
//
// A rejected edge is KEPT, not deleted: a later run over the same items would
// propose it again, and re-asking a question already answered is how a review
// screen teaches people to stop reading it.
func (s *Store) SetDependencyStatus(ctx context.Context, id, status string) error {
	if status != domain.EdgeAccepted && status != domain.EdgeDismissed {
		return fmt.Errorf("set dependency status: %q is not a verdict", status)
	}
	_, err := s.DB.ExecContext(ctx,
		`UPDATE item_dependencies SET status = ? WHERE id = ? AND status = 'proposed'`,
		status, id)
	return err
}

// KnownEdge reports whether this pair has already been ruled on, in either
// direction. Used before proposing, so a run does not re-offer an edge a human
// has already rejected.
func (s *Store) KnownEdge(ctx context.Context, projectID, fromID, toID string) (bool, error) {
	var n int
	err := s.DB.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM item_dependencies
		  WHERE project_id = ?
		    AND ((from_id = ? AND to_id = ?) OR (from_id = ? AND to_id = ?))`,
		projectID, fromID, toID, toID, fromID).Scan(&n)
	return n > 0, err
}
