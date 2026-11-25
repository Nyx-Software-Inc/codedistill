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
	"codedistill/internal/storage"
)

const jobColumns = `id, project_id, type, scope_kind, scope_id, scope_label,
	status, phase, done, total, model, tokens_in, tokens_out, error,
	created_by, started_at, updated_at, finished_at, branch, base_sha,
	priority, queued_at, paused_by`

// CreateJob records the start of a run, or fills in one the queue has just
// started.
//
// A queued job's row exists before its runner does (docs/design/job-queue.md):
// the queue marks it running when a slot frees, then calls the runner, which
// records the start exactly as it always has. So an existing row with the same
// id, still running, takes the runner's fields; its priority, its place in the
// line and its start time stay as the queue set them.
func (s *Store) CreateJob(ctx context.Context, j *domain.Job) error {
	if !domain.ValidJobStatus(j.Status) {
		return fmt.Errorf("create job: invalid status %q", j.Status)
	}
	priority := j.Priority
	if priority == "" {
		priority = domain.PriorityMedium
	}
	if !domain.ValidJobPriority(priority) {
		return fmt.Errorf("create job: invalid priority %q", priority)
	}
	var finished, queued any
	if j.FinishedAt != nil {
		finished = *j.FinishedAt
	}
	if j.QueuedAt != nil {
		queued = *j.QueuedAt
	}
	res, err := s.DB.ExecContext(ctx,
		`INSERT INTO jobs (`+jobColumns+`)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT (id) DO UPDATE SET
		   scope_kind = excluded.scope_kind, scope_id = excluded.scope_id,
		   scope_label = excluded.scope_label, phase = excluded.phase,
		   done = excluded.done, total = excluded.total, model = excluded.model,
		   created_by = COALESCE(jobs.created_by, excluded.created_by),
		   updated_at = excluded.updated_at,
		   branch = excluded.branch, base_sha = excluded.base_sha
		 WHERE jobs.status = 'running' AND excluded.status = 'running'`,
		j.ID, j.ProjectID, j.Type, nullable(j.ScopeKind), nullable(j.ScopeID),
		nullable(j.ScopeLabel), j.Status, nullable(j.Phase), j.Done, j.Total,
		nullable(j.Model), j.TokensIn, j.TokensOut, nullable(j.Error),
		nullable(j.CreatedBy), j.StartedAt, j.UpdatedAt, finished,
		nullable(j.Branch), nullable(j.BaseSHA), priority, queued, nullable(j.PausedBy))
	if err != nil {
		return fmt.Errorf("create job: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return storage.ErrJobNotStarting
	}
	return nil
}

// ListQueuedJobs returns every job waiting for a slot, in every project: the
// slots are shared by the whole installation. Ordering by priority is the
// queue's business (it ages priorities); this returns them by arrival.
func (s *Store) ListQueuedJobs(ctx context.Context) ([]*domain.Job, error) {
	rows, err := s.DB.QueryContext(ctx,
		`SELECT `+jobColumns+` FROM jobs WHERE status = ? ORDER BY queued_at, started_at, id`,
		domain.JobQueued)
	if err != nil {
		return nil, fmt.Errorf("list queued jobs: %w", err)
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

// ListJobsSince returns every job, in every project, still open or finished
// at or after since: the jobs a timeline from since onwards can see.
func (s *Store) ListJobsSince(ctx context.Context, since time.Time) ([]*domain.Job, error) {
	rows, err := s.DB.QueryContext(ctx,
		`SELECT `+jobColumns+` FROM jobs WHERE finished_at IS NULL OR finished_at >= ? ORDER BY started_at, id`, since)
	if err != nil {
		return nil, fmt.Errorf("list jobs since: %w", err)
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

// CountRunningJobs counts the jobs holding a slot, in every project. A job
// started outside the queue (by the command line, in its own process) holds
// one too, because it is a running row.
func (s *Store) CountRunningJobs(ctx context.Context) (int, error) {
	var n int
	if err := s.DB.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM jobs WHERE status = ?`, domain.JobRunning).Scan(&n); err != nil {
		return 0, fmt.Errorf("count running jobs: %w", err)
	}
	return n, nil
}

// StartQueuedJob takes a job out of the line: queued becomes running, and its
// start time becomes now. False when it is no longer queued (cancelled, or
// started by another process), so two starters can never both run it.
func (s *Store) StartQueuedJob(ctx context.Context, id string, at time.Time) (bool, error) {
	res, err := s.DB.ExecContext(ctx,
		`UPDATE jobs SET status = ?, started_at = ?, updated_at = ?, paused_by = NULL WHERE id = ? AND status = ?`,
		domain.JobRunning, at, at, id, domain.JobQueued)
	if err != nil {
		return false, fmt.Errorf("start queued job: %w", err)
	}
	n, _ := res.RowsAffected()
	return n == 1, nil
}

// ListRunningJobs returns every running job, in every project: the ones
// holding the slots, among which the queue picks one to pause for an
// Immediate job.
func (s *Store) ListRunningJobs(ctx context.Context) ([]*domain.Job, error) {
	rows, err := s.DB.QueryContext(ctx,
		`SELECT `+jobColumns+` FROM jobs WHERE status = ? ORDER BY started_at, id`, domain.JobRunning)
	if err != nil {
		return nil, fmt.Errorf("list running jobs: %w", err)
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

// PauseJob marks a running job paused, saying who paused it. False when it is
// not running. Stopping the work is the caller's: the job stops at its next
// checkpoint.
func (s *Store) PauseJob(ctx context.Context, id, by string, at time.Time) (bool, error) {
	res, err := s.DB.ExecContext(ctx,
		`UPDATE jobs SET status = ?, paused_by = ?, updated_at = ?, finished_at = ? WHERE id = ? AND status = ?`,
		domain.JobPaused, nullable(by), at, at, id, domain.JobRunning)
	if err != nil {
		return false, fmt.Errorf("pause job: %w", err)
	}
	n, _ := res.RowsAffected()
	return n == 1, nil
}

// RequeueJob puts a paused or interrupted job back in line to resume. It keeps
// its place: when it first joined the line, or when it first started for a job
// that never waited. by is kept as the reason it is waiting again ("" for a
// person resuming it).
func (s *Store) RequeueJob(ctx context.Context, id, by string, at time.Time) (bool, error) {
	res, err := s.DB.ExecContext(ctx,
		`UPDATE jobs SET status = ?, paused_by = ?, updated_at = ?, finished_at = NULL, error = NULL,
		        queued_at = COALESCE(queued_at, started_at)
		 WHERE id = ? AND status IN (?, ?)`,
		domain.JobQueued, nullable(by), at, id, domain.JobPaused, domain.JobInterrupted)
	if err != nil {
		return false, fmt.Errorf("requeue job: %w", err)
	}
	n, _ := res.RowsAffected()
	return n == 1, nil
}

// RequeueRunningJobs is InterruptStaleJobs for jobs that can resume: run at
// startup, it puts the given types' jobs left running back in line, marked as
// stopped by a restart. The rest become interrupted as before.
func (s *Store) RequeueRunningJobs(ctx context.Context, types []string, at time.Time) (int, error) {
	if len(types) == 0 {
		return 0, nil
	}
	q := `UPDATE jobs SET status = ?, paused_by = ?, updated_at = ?, queued_at = COALESCE(queued_at, started_at)
	      WHERE status = ? AND type IN (` + strings.TrimSuffix(strings.Repeat("?,", len(types)), ",") + `)`
	args := []any{domain.JobQueued, domain.PausedByRestart, at, domain.JobRunning}
	for _, t := range types {
		args = append(args, t)
	}
	res, err := s.DB.ExecContext(ctx, q, args...)
	if err != nil {
		return 0, fmt.Errorf("requeue running jobs: %w", err)
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}

// SetJobPriority changes a waiting job's priority. False when it is no longer
// waiting: a running job's priority no longer orders anything.
func (s *Store) SetJobPriority(ctx context.Context, id, priority string, at time.Time) (bool, error) {
	if !domain.ValidJobPriority(priority) {
		return false, fmt.Errorf("set job priority: invalid priority %q", priority)
	}
	res, err := s.DB.ExecContext(ctx,
		`UPDATE jobs SET priority = ?, updated_at = ? WHERE id = ? AND status = ?`,
		priority, at, id, domain.JobQueued)
	if err != nil {
		return false, fmt.Errorf("set job priority: %w", err)
	}
	n, _ := res.RowsAffected()
	return n == 1, nil
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
	if !domain.ValidJobStatus(status) || status == domain.JobRunning || status == domain.JobQueued {
		return fmt.Errorf("finish job: invalid terminal status %q", status)
	}
	if status != domain.JobFailed {
		errMsg = ""
	}
	_, err := s.DB.ExecContext(ctx,
		`UPDATE jobs SET status = ?, error = ?, updated_at = ?, finished_at = ?
		 WHERE id = ? AND status IN (?, ?)`,
		status, nullable(errMsg), at, at, id, domain.JobRunning, domain.JobQueued)
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

// ActiveJobFor answers "is something already working on this, or waiting to",
// which is the
// singleton check architecture drafting does today with an in-memory map. A
// nil result means the caller may start one.
func (s *Store) ActiveJobFor(ctx context.Context, jobType, scopeKind, scopeID string) (*domain.Job, error) {
	row := s.DB.QueryRowContext(ctx,
		`SELECT `+jobColumns+` FROM jobs
		 WHERE type = ? AND scope_kind = ? AND scope_id = ? AND status IN (?, ?)
		 ORDER BY started_at DESC LIMIT 1`,
		jobType, nullable(scopeKind), nullable(scopeID), domain.JobRunning, domain.JobQueued)
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
	var scopeKind, scopeID, scopeLabel, phase, model, errMsg, createdBy, branch, baseSHA sql.NullString
	var finished, queued sql.NullTime
	var pausedBy sql.NullString
	err := r.Scan(&j.ID, &j.ProjectID, &j.Type, &scopeKind, &scopeID, &scopeLabel,
		&j.Status, &phase, &j.Done, &j.Total, &model, &j.TokensIn, &j.TokensOut,
		&errMsg, &createdBy, &j.StartedAt, &j.UpdatedAt, &finished, &branch, &baseSHA,
		&j.Priority, &queued, &pausedBy)
	j.PausedBy = pausedBy.String
	if err != nil {
		return nil, err
	}
	if queued.Valid {
		t := queued.Time
		j.QueuedAt = &t
	}
	j.Branch, j.BaseSHA = branch.String, baseSHA.String
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
