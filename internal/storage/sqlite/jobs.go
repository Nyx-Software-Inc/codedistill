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
	"strings"
	"time"

	"codedistill/internal/domain"
)

const jobColumns = `id, project_id, type, scope_kind, scope_id, scope_label,
	status, phase, done, total, model, tokens_in, tokens_out, error,
	created_by, started_at, updated_at, finished_at`

// CreateJob records the start of a run.
func (s *Store) CreateJob(ctx context.Context, j *domain.Job) error {
	if !domain.ValidJobStatus(j.Status) {
		return fmt.Errorf("create job: invalid status %q", j.Status)
	}
	var finished any
	if j.FinishedAt != nil {
		finished = *j.FinishedAt
	}
	_, err := s.DB.ExecContext(ctx,
		`INSERT INTO jobs (`+jobColumns+`)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		j.ID, j.ProjectID, j.Type, nullable(j.ScopeKind), nullable(j.ScopeID),
		nullable(j.ScopeLabel), j.Status, nullable(j.Phase), j.Done, j.Total,
		nullable(j.Model), j.TokensIn, j.TokensOut, nullable(j.Error),
		nullable(j.CreatedBy), j.StartedAt, j.UpdatedAt, finished)
	if err != nil {
		return fmt.Errorf("create job: %w", err)
	}
	return nil
}

// UpdateJobProgress advances a running job. Total is written too because a
// phased job revises it at the phase boundary — decomposition does not know
// how many classification passes it needs until reading has finished.
//
// It only touches rows still marked running, so a progress write that races a
// cancel cannot resurrect the job.
func (s *Store) UpdateJobProgress(ctx context.Context, id, phase string, done, total int, at time.Time) error {
	res, err := s.DB.ExecContext(ctx,
		`UPDATE jobs SET phase = ?, done = ?, total = ?, updated_at = ?
		 WHERE id = ? AND status = ?`,
		nullable(phase), done, total, at, id, domain.JobRunning)
	if err != nil {
		return fmt.Errorf("update job progress: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		// Not an error: the job was cancelled or finished under us, and the
		// worker will notice on its next cancellation check.
		return nil
	}
	return nil
}

// AddJobTokens accumulates model usage. Separate from progress because a job
// may make several calls per unit of progress, and because a job dashboard
// wants cost even for runs that never advanced.
func (s *Store) AddJobTokens(ctx context.Context, id string, in, out int, at time.Time) error {
	_, err := s.DB.ExecContext(ctx,
		`UPDATE jobs SET tokens_in = tokens_in + ?, tokens_out = tokens_out + ?,
		        updated_at = ? WHERE id = ?`,
		in, out, at, id)
	if err != nil {
		return fmt.Errorf("add job tokens: %w", err)
	}
	return nil
}

// FinishJob closes a run. errMsg is stored only for the failed status; passing
// one with success would make the dashboard lie.
func (s *Store) FinishJob(ctx context.Context, id, status, errMsg string, at time.Time) error {
	if !domain.ValidJobStatus(status) || status == domain.JobRunning {
		return fmt.Errorf("finish job: invalid terminal status %q", status)
	}
	if status != domain.JobFailed {
		errMsg = ""
	}
	_, err := s.DB.ExecContext(ctx,
		`UPDATE jobs SET status = ?, error = ?, updated_at = ?, finished_at = ?
		 WHERE id = ? AND status = ?`,
		status, nullable(errMsg), at, at, id, domain.JobRunning)
	if err != nil {
		return fmt.Errorf("finish job: %w", err)
	}
	return nil
}

// GetJob reads one run.
func (s *Store) GetJob(ctx context.Context, id string) (*domain.Job, error) {
	row := s.DB.QueryRowContext(ctx, `SELECT `+jobColumns+` FROM jobs WHERE id = ?`, id)
	j, err := scanJob(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return j, err
}

// ActiveJobFor answers "is something already working on this", which is the
// singleton check architecture drafting does today with an in-memory map. A
// nil result means the caller may start one.
func (s *Store) ActiveJobFor(ctx context.Context, jobType, scopeKind, scopeID string) (*domain.Job, error) {
	row := s.DB.QueryRowContext(ctx,
		`SELECT `+jobColumns+` FROM jobs
		 WHERE type = ? AND scope_kind = ? AND scope_id = ? AND status = ?
		 ORDER BY started_at DESC LIMIT 1`,
		jobType, nullable(scopeKind), nullable(scopeID), domain.JobRunning)
	j, err := scanJob(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return j, err
}

// LatestJobFor returns the most recent run for a scope whatever its status.
// The architecture panel needs this rather than ActiveJobFor: after a run ends
// it still shows the final tally ("24 of 24 characterised"), which the old
// in-memory map provided by keeping completed entries around.
func (s *Store) LatestJobFor(ctx context.Context, jobType, scopeKind, scopeID string) (*domain.Job, error) {
	row := s.DB.QueryRowContext(ctx,
		`SELECT `+jobColumns+` FROM jobs
		 WHERE type = ? AND scope_kind = ? AND scope_id = ?
		 ORDER BY started_at DESC LIMIT 1`,
		jobType, nullable(scopeKind), nullable(scopeID))
	j, err := scanJob(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return j, err
}

// ListJobs returns a project's runs, newest first. statuses filters; empty
// means all.
func (s *Store) ListJobs(ctx context.Context, projectID string, statuses []string, limit int) ([]*domain.Job, error) {
	q := `SELECT ` + jobColumns + ` FROM jobs WHERE project_id = ?`
	args := []any{projectID}
	if len(statuses) > 0 {
		q += ` AND status IN (` + strings.TrimSuffix(strings.Repeat("?,", len(statuses)), ",") + `)`
		for _, st := range statuses {
			args = append(args, st)
		}
	}
	q += ` ORDER BY started_at DESC`
	if limit > 0 {
		q += ` LIMIT ?`
		args = append(args, limit)
	}

	rows, err := s.DB.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("list jobs: %w", err)
	}
	defer rows.Close()

	var out []*domain.Job
	for rows.Next() {
		j, err := scanJob(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, j)
	}
	return out, rows.Err()
}

// InterruptStaleJobs marks runs that were still "running" when the process
// died. Called once at startup, BEFORE anything is allowed to start a new job.
//
// Without this a crashed decomposition holds its scope forever: ActiveJobFor
// keeps returning it, and the user can never retry the document. Distinguishing
// interrupted from failed matters because nothing went wrong with the work.
func (s *Store) InterruptStaleJobs(ctx context.Context, at time.Time) (int, error) {
	res, err := s.DB.ExecContext(ctx,
		`UPDATE jobs SET status = ?, updated_at = ?, finished_at = ?
		 WHERE status = ?`,
		domain.JobInterrupted, at, at, domain.JobRunning)
	if err != nil {
		return 0, fmt.Errorf("interrupt stale jobs: %w", err)
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}

// rowScanner is satisfied by both *sql.Row and *sql.Rows.
type rowScanner interface{ Scan(dest ...any) error }

func scanJob(r rowScanner) (*domain.Job, error) {
	j := &domain.Job{}
	var scopeKind, scopeID, scopeLabel, phase, model, errMsg, createdBy sql.NullString
	var finished sql.NullTime
	err := r.Scan(&j.ID, &j.ProjectID, &j.Type, &scopeKind, &scopeID, &scopeLabel,
		&j.Status, &phase, &j.Done, &j.Total, &model, &j.TokensIn, &j.TokensOut,
		&errMsg, &createdBy, &j.StartedAt, &j.UpdatedAt, &finished)
	if err != nil {
		return nil, err
	}
	j.ScopeKind, j.ScopeID, j.ScopeLabel = scopeKind.String, scopeID.String, scopeLabel.String
	j.Phase, j.Model, j.Error, j.CreatedBy = phase.String, model.String, errMsg.String, createdBy.String
	if finished.Valid {
		t := finished.Time
		j.FinishedAt = &t
	}
	return j, nil
}

// nullable stores "" as NULL so the scope columns compare correctly and the
// dashboard does not have to distinguish empty from absent.
func nullable(s string) any {
	if s == "" {
		return nil
	}
	return s
}
