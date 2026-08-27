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
	"time"

	"codedistill/internal/domain"
	"codedistill/internal/storage"
)

// insertSkillVersion appends one immutable version snapshot. The row id is
// deterministic (`<skill_id>:<version>`), so a duplicate (skill, version) is
// rejected by the primary key rather than silently double-written.
func (s *Store) insertSkillVersion(ctx context.Context, skillID string, version int, name, content, author string, at time.Time) error {
	_, err := s.DB.ExecContext(ctx,
		`INSERT INTO skill_versions (id, skill_id, version, name, content, author, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		skillVersionRowID(skillID, version), skillID, version, name, content, author, at)
	if err != nil {
		return fmt.Errorf("insert skill version: %w", err)
	}
	return nil
}

func scanSkillVersion(scan func(...any) error) (*domain.SkillVersion, error) {
	v := &domain.SkillVersion{}
	if err := scan(&v.ID, &v.SkillID, &v.Version, &v.Name, &v.Content, &v.Author, &v.CreatedAt); err != nil {
		return nil, err
	}
	return v, nil
}

const skillVersionColumns = `id, skill_id, version, name, content, author, created_at`

// ListSkillVersions returns a skill's version history, newest first.
func (s *Store) ListSkillVersions(ctx context.Context, skillID string) ([]*domain.SkillVersion, error) {
	rows, err := s.DB.QueryContext(ctx,
		`SELECT `+skillVersionColumns+` FROM skill_versions WHERE skill_id = ? ORDER BY version DESC`, skillID)
	if err != nil {
		return nil, fmt.Errorf("list skill versions: %w", err)
	}
	defer rows.Close()
	out := []*domain.SkillVersion{}
	for rows.Next() {
		v, err := scanSkillVersion(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// GetSkillVersion returns one snapshot by (skill, version).
func (s *Store) GetSkillVersion(ctx context.Context, skillID string, version int) (*domain.SkillVersion, error) {
	v, err := scanSkillVersion(s.DB.QueryRowContext(ctx,
		`SELECT `+skillVersionColumns+` FROM skill_versions WHERE skill_id = ? AND version = ?`,
		skillID, version).Scan)
	if err != nil {
		return nil, fmt.Errorf("skill %s v%d: %w", skillID, version, storage.ErrNotFound)
	}
	return v, nil
}

// PurgeSkillVersion hard-deletes one version snapshot. This is the deliberate
// compliance escape hatch (e.g. a secret pasted into a version) — the normal
// path never removes history. It does not renumber other versions.
func (s *Store) PurgeSkillVersion(ctx context.Context, skillID string, version int) error {
	if _, err := s.DB.ExecContext(ctx,
		`DELETE FROM skill_versions WHERE skill_id = ? AND version = ?`, skillID, version); err != nil {
		return fmt.Errorf("purge skill version: %w", err)
	}
	return nil
}

// RecordSkillRetrieval logs a get_skill fetch (audit fact, not proof of use).
func (s *Store) RecordSkillRetrieval(ctx context.Context, r *domain.SkillRetrieval) error {
	_, err := s.DB.ExecContext(ctx,
		`INSERT INTO skill_retrievals (id, skill_id, skill_version, project_id, actor, created_at)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		r.ID, r.SkillID, r.SkillVersion, r.ProjectID, r.Actor, r.CreatedAt)
	if err != nil {
		return fmt.Errorf("record skill retrieval: %w", err)
	}
	return nil
}

// RecordSkillApplication binds a skill version to a change. It requires a
// non-empty evidence class — the tie is never recorded without evidence of use.
func (s *Store) RecordSkillApplication(ctx context.Context, a *domain.SkillApplication) error {
	if a.Evidence == "" {
		return fmt.Errorf("skill application requires an evidence class")
	}
	_, err := s.DB.ExecContext(ctx,
		`INSERT INTO skill_applications (id, skill_id, skill_version, owner_type, owner_id, commit_sha, evidence, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		a.ID, a.SkillID, a.SkillVersion, a.OwnerType, a.OwnerID, a.CommitSHA, a.Evidence, a.CreatedAt)
	if err != nil {
		return fmt.Errorf("record skill application: %w", err)
	}
	return nil
}

func scanSkillApplication(scan func(...any) error) (*domain.SkillApplication, error) {
	a := &domain.SkillApplication{}
	if err := scan(&a.ID, &a.SkillID, &a.SkillVersion, &a.OwnerType, &a.OwnerID, &a.CommitSHA, &a.Evidence, &a.CreatedAt); err != nil {
		return nil, err
	}
	return a, nil
}

const skillApplicationColumns = `id, skill_id, skill_version, owner_type, owner_id, commit_sha, evidence, created_at`

// ListSkillApplications is the reverse-lookup: every change bound to a skill,
// optionally narrowed to one version (0 = any) and/or one evidence class
// ("" = any). Newest first. This answers "what did skill vN touch?".
func (s *Store) ListSkillApplications(ctx context.Context, skillID string, version int, evidence string) ([]*domain.SkillApplication, error) {
	q := `SELECT ` + skillApplicationColumns + ` FROM skill_applications WHERE skill_id = ?`
	args := []any{skillID}
	if version > 0 {
		q += ` AND skill_version = ?`
		args = append(args, version)
	}
	if evidence != "" {
		q += ` AND evidence = ?`
		args = append(args, evidence)
	}
	q += ` ORDER BY created_at DESC`
	rows, err := s.DB.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("list skill applications: %w", err)
	}
	defer rows.Close()
	out := []*domain.SkillApplication{}
	for rows.Next() {
		a, err := scanSkillApplication(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func scanSkillRetrieval(scan func(...any) error) (*domain.SkillRetrieval, error) {
	r := &domain.SkillRetrieval{}
	if err := scan(&r.ID, &r.SkillID, &r.SkillVersion, &r.ProjectID, &r.Actor, &r.CreatedAt); err != nil {
		return nil, err
	}
	return r, nil
}

// ListSkillRetrievals returns the fetch log for a skill, optionally narrowed to
// one version (0 = any). Newest first — the "wider suspect net" alongside the
// attested applications.
func (s *Store) ListSkillRetrievals(ctx context.Context, skillID string, version int) ([]*domain.SkillRetrieval, error) {
	q := `SELECT id, skill_id, skill_version, project_id, actor, created_at FROM skill_retrievals WHERE skill_id = ?`
	args := []any{skillID}
	if version > 0 {
		q += ` AND skill_version = ?`
		args = append(args, version)
	}
	q += ` ORDER BY created_at DESC`
	rows, err := s.DB.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("list skill retrievals: %w", err)
	}
	defer rows.Close()
	out := []*domain.SkillRetrieval{}
	for rows.Next() {
		r, err := scanSkillRetrieval(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
