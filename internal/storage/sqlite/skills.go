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
	"fmt"
	"strconv"

	"codedistill/internal/domain"
	"codedistill/internal/storage"
)

const skillColumns = `id, project_id, name, content, enabled, position, version, created_at, updated_at`

func scanSkill(scan func(...any) error) (*domain.Skill, error) {
	s := &domain.Skill{}
	if err := scan(&s.ID, &s.ProjectID, &s.Name, &s.Content, &s.Enabled, &s.Position, &s.Version, &s.CreatedAt, &s.UpdatedAt); err != nil {
		return nil, err
	}
	return s, nil
}

// skillVersionRowID is the deterministic id for a version snapshot, matching the
// backfill convention in migration 0058 (`<skill_id>:<version>`). One row per
// (skill, version), so this is naturally unique.
func skillVersionRowID(skillID string, version int) string {
	return skillID + ":" + strconv.Itoa(version)
}

// ListSkills returns a project's skills, ordered. enabledOnly filters to the
// active set (what the agent should retrieve).
func (s *Store) ListSkills(ctx context.Context, projectID string, enabledOnly bool) ([]*domain.Skill, error) {
	q := `SELECT ` + skillColumns + ` FROM skills WHERE project_id = ?`
	if enabledOnly {
		q += ` AND enabled = ` + s.DB.Dialect().BoolTrue()
	}
	q += ` ORDER BY position, created_at`
	rows, err := s.DB.QueryContext(ctx, q, projectID)
	if err != nil {
		return nil, fmt.Errorf("list skills: %w", err)
	}
	defer rows.Close()
	out := []*domain.Skill{}
	for rows.Next() {
		sk, err := scanSkill(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, sk)
	}
	return out, rows.Err()
}

func (s *Store) GetSkill(ctx context.Context, id string) (*domain.Skill, error) {
	sk, err := scanSkill(s.DB.QueryRowContext(ctx,
		`SELECT `+skillColumns+` FROM skills WHERE id = ?`, id).Scan)
	if err != nil {
		return nil, fmt.Errorf("skill %s: %w", id, storage.ErrNotFound)
	}
	return sk, nil
}

// CreateSkill writes the skill at version 1 and records its initial version
// snapshot so the history is complete from the first edit forward.
func (s *Store) CreateSkill(ctx context.Context, sk *domain.Skill) error {
	sk.Version = 1
	if _, err := s.DB.ExecContext(ctx,
		`INSERT INTO skills (`+skillColumns+`) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		sk.ID, sk.ProjectID, sk.Name, sk.Content, sk.Enabled, sk.Position, sk.Version, sk.CreatedAt, sk.UpdatedAt); err != nil {
		return fmt.Errorf("create skill: %w", err)
	}
	if err := s.insertSkillVersion(ctx, sk.ID, 1, sk.Name, sk.Content, "", sk.CreatedAt); err != nil {
		return err
	}
	return nil
}

// UpdateSkill writes the mutable fields (name/content/enabled/position) + updated_at.
// If the name or content changed, it cuts a new immutable version and bumps the
// head pointer; enabled/position-only edits do NOT create a version. On return,
// sk.Version reflects the (possibly bumped) head version.
func (s *Store) UpdateSkill(ctx context.Context, sk *domain.Skill) error {
	var (
		curName, curContent string
		curVersion          int
	)
	err := s.DB.QueryRowContext(ctx,
		`SELECT name, content, version FROM skills WHERE id = ?`, sk.ID).
		Scan(&curName, &curContent, &curVersion)
	if err != nil {
		return fmt.Errorf("skill %s: %w", sk.ID, storage.ErrNotFound)
	}

	newVersion := curVersion
	if sk.Name != curName || sk.Content != curContent {
		newVersion = curVersion + 1
		if err := s.insertSkillVersion(ctx, sk.ID, newVersion, sk.Name, sk.Content, "", sk.UpdatedAt); err != nil {
			return err
		}
	}

	res, err := s.DB.ExecContext(ctx,
		`UPDATE skills SET name = ?, content = ?, enabled = ?, position = ?, version = ?, updated_at = ? WHERE id = ?`,
		sk.Name, sk.Content, sk.Enabled, sk.Position, newVersion, sk.UpdatedAt, sk.ID)
	if err != nil {
		return fmt.Errorf("update skill: %w", err)
	}
	if c, _ := res.RowsAffected(); c == 0 {
		return fmt.Errorf("skill %s: %w", sk.ID, storage.ErrNotFound)
	}
	sk.Version = newVersion
	return nil
}

func (s *Store) DeleteSkill(ctx context.Context, id string) error {
	// The version history and any retrievals/applications are intentionally left
	// in place — they are provenance about changes that already happened, and a
	// deleted skill's past use should not silently disappear from the throughline.
	res, err := s.DB.ExecContext(ctx, `DELETE FROM skills WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete skill: %w", err)
	}
	// The other outlier alongside architecture nodes: without this a delete of a
	// nonexistent skill reported success and the API answered 204 (CE-review
	// item 31).
	if c, _ := res.RowsAffected(); c == 0 {
		return fmt.Errorf("skill %s: %w", id, storage.ErrNotFound)
	}
	return nil
}
