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
	"time"

	"codedistill/internal/domain"
	"codedistill/internal/storage"
)

const codeAnchorColumns = `
	id, owner_type, owner_id, kind, codebase_id, path,
	line_start, line_end, revision, url, label,
	provenance, created_at, updated_at`

func scanCodeAnchor(scan func(...any) error) (*domain.CodeAnchor, error) {
	a := &domain.CodeAnchor{}
	var (
		codebaseID, path, revision, url, label sql.NullString
		lineStart, lineEnd                     sql.NullInt64
	)
	err := scan(
		&a.ID, &a.OwnerType, &a.OwnerID, &a.Kind, &codebaseID, &path,
		&lineStart, &lineEnd, &revision, &url, &label,
		&a.Provenance, &a.CreatedAt, &a.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	a.CodebaseID = stringOrEmpty(codebaseID)
	a.Path = stringOrEmpty(path)
	a.Revision = stringOrEmpty(revision)
	a.URL = stringOrEmpty(url)
	a.Label = stringOrEmpty(label)
	if lineStart.Valid {
		a.LineStart = int(lineStart.Int64)
	}
	if lineEnd.Valid {
		a.LineEnd = int(lineEnd.Int64)
	}
	return a, nil
}

func nullIntZero(n int) sql.NullInt64 {
	if n == 0 {
		return sql.NullInt64{}
	}
	return sql.NullInt64{Int64: int64(n), Valid: true}
}

func (s *Store) CreateCodeAnchor(ctx context.Context, a *domain.CodeAnchor) error {
	_, err := s.DB.ExecContext(ctx,
		`INSERT INTO code_anchors (`+codeAnchorColumns+`)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		a.ID, a.OwnerType, a.OwnerID, a.Kind, nullString(a.CodebaseID), nullString(a.Path),
		nullIntZero(a.LineStart), nullIntZero(a.LineEnd),
		nullString(a.Revision), nullString(a.URL), nullString(a.Label),
		a.Provenance, a.CreatedAt, a.UpdatedAt,
	)
	return err
}

func (s *Store) GetCodeAnchor(ctx context.Context, id string) (*domain.CodeAnchor, error) {
	row := s.DB.QueryRowContext(ctx,
		`SELECT `+codeAnchorColumns+` FROM code_anchors WHERE id = ?`, id,
	)
	a, err := scanCodeAnchor(row.Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("code anchor %s: %w", id, storage.ErrNotFound)
	}
	if err != nil {
		return nil, err
	}
	return a, nil
}

func (s *Store) ListCodeAnchors(ctx context.Context, ownerType, ownerID string) ([]*domain.CodeAnchor, error) {
	rows, err := s.DB.QueryContext(ctx,
		`SELECT `+codeAnchorColumns+`
		 FROM code_anchors WHERE owner_type = ? AND owner_id = ?
		 ORDER BY created_at, id`,
		ownerType, ownerID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*domain.CodeAnchor
	for rows.Next() {
		a, err := scanCodeAnchor(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func (s *Store) UpdateCodeAnchor(ctx context.Context, a *domain.CodeAnchor) error {
	res, err := s.DB.ExecContext(ctx,
		`UPDATE code_anchors SET
			kind = ?, codebase_id = ?, path = ?, line_start = ?, line_end = ?,
			revision = ?, url = ?, label = ?,
			provenance = ?, updated_at = ?
		 WHERE id = ?`,
		a.Kind, nullString(a.CodebaseID), nullString(a.Path), nullIntZero(a.LineStart), nullIntZero(a.LineEnd),
		nullString(a.Revision), nullString(a.URL), nullString(a.Label),
		a.Provenance, a.UpdatedAt,
		a.ID,
	)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("code anchor %s: %w", a.ID, storage.ErrNotFound)
	}
	return nil
}

func (s *Store) DeleteCodeAnchor(ctx context.Context, id string) error {
	res, err := s.DB.ExecContext(ctx, `DELETE FROM code_anchors WHERE id = ?`, id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("code anchor %s: %w", id, storage.ErrNotFound)
	}
	return nil
}

// DeleteCodeAnchorsForOwner removes every anchor bound to (ownerType, ownerID).
// Called from the four owner-specific Delete* methods so cascade is enforced
// without cross-type FKs. Not-found is not an error — an owner with no anchors
// is a normal case.
func (s *Store) DeleteCodeAnchorsForOwner(ctx context.Context, ownerType, ownerID string) error {
	_, err := s.DB.ExecContext(ctx,
		`DELETE FROM code_anchors WHERE owner_type = ? AND owner_id = ?`,
		ownerType, ownerID,
	)
	return err
}

// ListCodeAnchorsByPath returns every kind=file anchor in the project that
// points at the given path. Joins each owner type to derive its project_id
// (scratchpad_items go through scratchpads) and pulls a display title +
// scratchpad_id (the latter only for scratchpad_item owners). Sorted by
// line_start so the gutter render order is stable.
func (s *Store) ListCodeAnchorsByPath(ctx context.Context, projectID, path string) ([]*domain.CodeAnchorWithOwner, error) {
	// One UNION ALL per owner type. Each branch selects every CodeAnchor
	// column plus three extras: owner_title, owner_scratchpad_id, and a
	// constant for owner_type (already in the row, but echoed so we can
	// scan uniformly). We scan in the same order in each branch.
	q := `
		SELECT a.id, a.owner_type, a.owner_id, a.kind, a.codebase_id, a.path,
		       a.line_start, a.line_end, a.revision, a.url, a.label,
		       a.provenance, a.created_at, a.updated_at,
		       COALESCE(NULLIF(si.name, ''), substr(si.content, 1, 60)) AS owner_title,
		       si.scratchpad_id AS owner_scratchpad_id
		FROM code_anchors a
		JOIN scratchpad_items si ON a.owner_id = si.id
		JOIN scratchpads sp ON si.scratchpad_id = sp.id
		WHERE a.owner_type = 'scratchpad_item' AND a.kind = 'file' AND a.path = ?
		  AND sp.project_id = ?

		UNION ALL

		SELECT a.id, a.owner_type, a.owner_id, a.kind, a.codebase_id, a.path,
		       a.line_start, a.line_end, a.revision, a.url, a.label,
		       a.provenance, a.created_at, a.updated_at,
		       ti.subject AS owner_title,
		       '' AS owner_scratchpad_id
		FROM code_anchors a
		JOIN todo_items ti ON a.owner_id = ti.id
		WHERE a.owner_type = 'todo_item' AND a.kind = 'file' AND a.path = ?
		  AND ti.project_id = ?

		UNION ALL

		SELECT a.id, a.owner_type, a.owner_id, a.kind, a.codebase_id, a.path,
		       a.line_start, a.line_end, a.revision, a.url, a.label,
		       a.provenance, a.created_at, a.updated_at,
		       bi.subject AS owner_title,
		       '' AS owner_scratchpad_id
		FROM code_anchors a
		JOIN bug_items bi ON a.owner_id = bi.id
		WHERE a.owner_type = 'bug_item' AND a.kind = 'file' AND a.path = ?
		  AND bi.project_id = ?

		UNION ALL

		SELECT a.id, a.owner_type, a.owner_id, a.kind, a.codebase_id, a.path,
		       a.line_start, a.line_end, a.revision, a.url, a.label,
		       a.provenance, a.created_at, a.updated_at,
		       ke.title AS owner_title,
		       '' AS owner_scratchpad_id
		FROM code_anchors a
		JOIN knowledge_entries ke ON a.owner_id = ke.id
		WHERE a.owner_type = 'knowledge_entry' AND a.kind = 'file' AND a.path = ?
		  AND ke.project_id = ?

		UNION ALL

		SELECT a.id, a.owner_type, a.owner_id, a.kind, a.codebase_id, a.path,
		       a.line_start, a.line_end, a.revision, a.url, a.label,
		       a.provenance, a.created_at, a.updated_at,
		       COALESCE(NULLIF(uc.want, ''), uc.role) AS owner_title,
		       '' AS owner_scratchpad_id
		FROM code_anchors a
		JOIN use_case_items uc ON a.owner_id = uc.id
		WHERE a.owner_type = 'use_case_item' AND a.kind = 'file' AND a.path = ?
		  AND uc.project_id = ?

		ORDER BY line_start
	`
	rows, err := s.DB.QueryContext(ctx, q,
		path, projectID,
		path, projectID,
		path, projectID,
		path, projectID,
		path, projectID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []*domain.CodeAnchorWithOwner{}
	for rows.Next() {
		var (
			a                                              domain.CodeAnchorWithOwner
			codebaseID, anchorPath, revision, url, label  sql.NullString
			lineStart, lineEnd                             sql.NullInt64
			ownerScratchpad                                sql.NullString
			ownerTitle                                     sql.NullString
		)
		err := rows.Scan(
			&a.ID, &a.OwnerType, &a.OwnerID, &a.Kind, &codebaseID, &anchorPath,
			&lineStart, &lineEnd, &revision, &url, &label,
			&a.Provenance, &a.CreatedAt, &a.UpdatedAt,
			&ownerTitle, &ownerScratchpad,
		)
		if err != nil {
			return nil, err
		}
		a.CodebaseID = stringOrEmpty(codebaseID)
		a.Path = stringOrEmpty(anchorPath)
		a.Revision = stringOrEmpty(revision)
		a.URL = stringOrEmpty(url)
		a.Label = stringOrEmpty(label)
		if lineStart.Valid {
			a.LineStart = int(lineStart.Int64)
		}
		if lineEnd.Valid {
			a.LineEnd = int(lineEnd.Int64)
		}
		a.OwnerTitle = stringOrEmpty(ownerTitle)
		a.OwnerScratchpadID = stringOrEmpty(ownerScratchpad)
		out = append(out, &a)
	}
	return out, rows.Err()
}

// ListCodeAnchorsForScratchpad returns every anchor owned by a scratchpad
// item in the given scratchpad. Sorted by owner_id then line_start so the
// frontend can group cheaply. Cheaper than a per-card fetch when the user
// is viewing a scratchpad with many items.
func (s *Store) ListCodeAnchorsForScratchpad(ctx context.Context, scratchpadID string) ([]*domain.CodeAnchor, error) {
	rows, err := s.DB.QueryContext(ctx,
		`SELECT `+codeAnchorColumns+`
		 FROM code_anchors
		 WHERE owner_type = 'scratchpad_item'
		   AND owner_id IN (SELECT id FROM scratchpad_items WHERE scratchpad_id = ?)
		 ORDER BY owner_id, line_start`,
		scratchpadID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []*domain.CodeAnchor{}
	for rows.Next() {
		a, err := scanCodeAnchor(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// CopyCodeAnchors duplicates anchors from the source owner onto the destination
// owner with new IDs and timestamps. Existing destination anchors are preserved
// (no de-duping). Intended for agent-derive propagation where the destination
// is a freshly created derived item with no prior anchors.
func (s *Store) CopyCodeAnchors(
	ctx context.Context,
	srcOwnerType, srcOwnerID, dstOwnerType, dstOwnerID string,
	newID func() string,
	now time.Time,
) error {
	anchors, err := s.ListCodeAnchors(ctx, srcOwnerType, srcOwnerID)
	if err != nil {
		return err
	}
	for _, src := range anchors {
		dst := *src
		dst.ID = newID()
		dst.OwnerType = dstOwnerType
		dst.OwnerID = dstOwnerID
		dst.CreatedAt = now
		dst.UpdatedAt = now
		if err := s.CreateCodeAnchor(ctx, &dst); err != nil {
			return err
		}
	}
	return nil
}
