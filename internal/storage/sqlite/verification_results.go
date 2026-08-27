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

const verificationResultColumns = `
	id, owner_type, owner_id, layer, kind, check_name,
	verdict, commit_sha, exit_code, summary, output,
	duration_ms, produced_by, created_at, updated_at`

func scanVerificationResult(scan func(...any) error) (*domain.VerificationResult, error) {
	v := &domain.VerificationResult{}
	var commitSHA sql.NullString
	var exitCode sql.NullInt64
	err := scan(
		&v.ID, &v.OwnerType, &v.OwnerID, &v.Layer, &v.Kind, &v.CheckName,
		&v.Verdict, &commitSHA, &exitCode, &v.Summary, &v.Output,
		&v.DurationMS, &v.ProducedBy, &v.CreatedAt, &v.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	v.CommitSHA = stringOrEmpty(commitSHA)
	if exitCode.Valid {
		c := int(exitCode.Int64)
		v.ExitCode = &c
	}
	return v, nil
}

func (s *Store) ListVerificationResults(ctx context.Context, ownerType, ownerID string) ([]*domain.VerificationResult, error) {
	rows, err := s.DB.QueryContext(ctx,
		`SELECT `+verificationResultColumns+`
		 FROM verification_results
		 WHERE owner_type = ? AND owner_id = ?
		 ORDER BY created_at DESC`,
		ownerType, ownerID,
	)
	if err != nil {
		return nil, fmt.Errorf("list verification results: %w", err)
	}
	defer rows.Close()
	out := []*domain.VerificationResult{}
	for rows.Next() {
		v, err := scanVerificationResult(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

func (s *Store) GetVerificationResult(ctx context.Context, id string) (*domain.VerificationResult, error) {
	v, err := scanVerificationResult(s.DB.QueryRowContext(ctx,
		`SELECT `+verificationResultColumns+` FROM verification_results WHERE id = ?`, id,
	).Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("verification result %s: %w", id, storage.ErrNotFound)
	}
	if err != nil {
		return nil, err
	}
	return v, nil
}

func (s *Store) CreateVerificationResult(ctx context.Context, v *domain.VerificationResult) error {
	_, err := s.DB.ExecContext(ctx,
		`INSERT INTO verification_results (`+verificationResultColumns+`)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		v.ID, v.OwnerType, v.OwnerID, v.Layer, v.Kind, v.CheckName,
		v.Verdict, nullString(v.CommitSHA), nullIntPtr(v.ExitCode), v.Summary, v.Output,
		v.DurationMS, v.ProducedBy, v.CreatedAt, v.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("create verification result: %w", err)
	}
	return nil
}

// UpdateVerificationResult writes the mutable fields — verdict, commit_sha,
// exit_code, summary, output, duration_ms + updated_at — so an async run can be
// created as "running" and resolved to its terminal verdict in place. Owner,
// layer, kind, and producer are immutable.
func (s *Store) UpdateVerificationResult(ctx context.Context, v *domain.VerificationResult) error {
	res, err := s.DB.ExecContext(ctx,
		`UPDATE verification_results
		 SET verdict = ?, commit_sha = ?, exit_code = ?, summary = ?,
		     output = ?, duration_ms = ?, updated_at = ?
		 WHERE id = ?`,
		v.Verdict, nullString(v.CommitSHA), nullIntPtr(v.ExitCode), v.Summary,
		v.Output, v.DurationMS, v.UpdatedAt, v.ID,
	)
	if err != nil {
		return fmt.Errorf("update verification result: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("verification result %s: %w", v.ID, storage.ErrNotFound)
	}
	return nil
}

// FailStaleRunningVerifications flips any result still at "running" to error —
// used once at startup to clear rows orphaned by a crash/restart mid-run
// (audit H9). Returns how many were finalized.
func (s *Store) FailStaleRunningVerifications(ctx context.Context, now time.Time) (int, error) {
	res, err := s.DB.ExecContext(ctx,
		`UPDATE verification_results
		 SET verdict = 'error', summary = 'interrupted — the service restarted mid-run', updated_at = ?
		 WHERE verdict = 'running'`, now)
	if err != nil {
		return 0, fmt.Errorf("fail stale running verifications: %w", err)
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}
