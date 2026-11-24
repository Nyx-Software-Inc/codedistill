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

const jobItemColumns = `id, job_id, position, owner_type, owner_id, finding_id, label,
	status, reason, commit_sha, attempt_ref, rounds, checks,
	progress_note, last_progress_at, started_at, finished_at, phase`

// CreateJobItem queues one item for a job.
func (s *Store) CreateJobItem(ctx context.Context, it *domain.JobItem) error {
	if !domain.ValidJobItemStatus(it.Status) {
		return fmt.Errorf("create job item: invalid status %q", it.Status)
	}
	_, err := s.DB.ExecContext(ctx,
		`INSERT INTO job_items (`+jobItemColumns+`)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		it.ID, it.JobID, it.Position, it.OwnerType, it.OwnerID, nullable(it.FindingID), it.Label,
		it.Status, it.Reason, nullable(it.CommitSHA), nullable(it.AttemptRef), it.Rounds, it.Checks,
		it.ProgressNote, nullTimePtr(it.LastProgressAt), nullTimePtr(it.StartedAt), nullTimePtr(it.FinishedAt),
		phaseOr(it.Phase))
	if err != nil {
		return fmt.Errorf("create job item: %w", err)
	}
	return nil
}

// UpdateJobItem writes an item's outcome fields. Progress is deliberately NOT
// written here: it arrives from the agent's own MCP process
// (TouchJobItemProgress), and an outcome write must not clobber the latest
// note with the stale copy the Conductor read earlier.
func (s *Store) UpdateJobItem(ctx context.Context, it *domain.JobItem) error {
	if !domain.ValidJobItemStatus(it.Status) {
		return fmt.Errorf("update job item: invalid status %q", it.Status)
	}
	res, err := s.DB.ExecContext(ctx,
		`UPDATE job_items SET status = ?, reason = ?, commit_sha = ?, attempt_ref = ?,
		        rounds = ?, checks = ?, started_at = ?, finished_at = ?, phase = ?
		 WHERE id = ?`,
		it.Status, it.Reason, nullable(it.CommitSHA), nullable(it.AttemptRef),
		it.Rounds, it.Checks, nullTimePtr(it.StartedAt), nullTimePtr(it.FinishedAt), phaseOr(it.Phase), it.ID)
	if err != nil {
		return fmt.Errorf("update job item: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("job item %s: %w", it.ID, storage.ErrNotFound)
	}
	return nil
}

// ResetJobItem puts an item back to queued with nothing of its unfinished
// attempt left that the next attempt would read: its claims, verdicts, notes
// and tests go, its rounds and checks are cleared. Its agent runs stay, as the
// record of what the attempt cost. For an item stopped part-way by a pause or a
// restart, which starts again from its first round.
func (s *Store) ResetJobItem(ctx context.Context, id string) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("reset job item: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	for _, t := range []string{"claim_verdicts", "job_claims", "job_points", "job_tests"} {
		if _, err := tx.ExecContext(ctx, `DELETE FROM `+t+` WHERE job_item_id = ?`, id); err != nil {
			return fmt.Errorf("reset job item: %s: %w", t, err)
		}
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE job_items SET status = ?, reason = '', commit_sha = NULL, attempt_ref = NULL, rounds = 0,
		        checks = '', progress_note = '', last_progress_at = NULL, started_at = NULL, finished_at = NULL,
		        phase = ?
		 WHERE id = ?`, domain.JobItemQueued, phaseOr(""), id); err != nil {
		return fmt.Errorf("reset job item: %w", err)
	}
	return tx.Commit()
}

// ListJobItems returns a job's items in work order.
func (s *Store) ListJobItems(ctx context.Context, jobID string) ([]*domain.JobItem, error) {
	rows, err := s.DB.QueryContext(ctx,
		`SELECT `+jobItemColumns+` FROM job_items WHERE job_id = ? ORDER BY position`, jobID)
	if err != nil {
		return nil, fmt.Errorf("list job items: %w", err)
	}
	defer rows.Close()
	var out []*domain.JobItem
	for rows.Next() {
		it, err := scanJobItem(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, it)
	}
	return out, rows.Err()
}

// TouchJobItemProgress records a progress report against the job's RUNNING
// item and the job row, and reports whether a running item took it.
//
// Called from the agent's MCP process, not the Conductor's, which is why it
// keys on the job alone: the agent knows its job id, and only one item of a
// job runs at a time. A report that arrives when nothing is running (between
// items, after a cancel) changes nothing rather than reviving a finished row.
func (s *Store) TouchJobItemProgress(ctx context.Context, jobID, note string, at time.Time) (bool, error) {
	res, err := s.DB.ExecContext(ctx,
		`UPDATE job_items SET progress_note = ?, last_progress_at = ?
		 WHERE job_id = ? AND status = ?`,
		note, at, jobID, domain.JobItemRunning)
	if err != nil {
		return false, fmt.Errorf("touch job item progress: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return false, nil
	}
	if _, err := s.DB.ExecContext(ctx,
		`UPDATE jobs SET updated_at = ? WHERE id = ? AND status = ?`,
		at, jobID, domain.JobRunning); err != nil {
		return true, fmt.Errorf("touch job: %w", err)
	}
	return true, nil
}

// RunningJobItem returns the item a job is working on now, or nil.
func (s *Store) RunningJobItem(ctx context.Context, jobID string) (*domain.JobItem, error) {
	row := s.DB.QueryRowContext(ctx,
		`SELECT `+jobItemColumns+` FROM job_items WHERE job_id = ? AND status = ?
		 ORDER BY position LIMIT 1`, jobID, domain.JobItemRunning)
	it, err := scanJobItem(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return it, err
}

func scanJobItem(r rowScanner) (*domain.JobItem, error) {
	it := &domain.JobItem{}
	var findingID, commit, attempt sql.NullString
	var lastProgress, started, finished sql.NullTime
	err := r.Scan(&it.ID, &it.JobID, &it.Position, &it.OwnerType, &it.OwnerID, &findingID, &it.Label,
		&it.Status, &it.Reason, &commit, &attempt, &it.Rounds, &it.Checks,
		&it.ProgressNote, &lastProgress, &started, &finished, &it.Phase)
	if err != nil {
		return nil, err
	}
	it.FindingID, it.CommitSHA, it.AttemptRef = findingID.String, commit.String, attempt.String
	it.LastProgressAt = timePtr(lastProgress)
	it.StartedAt = timePtr(started)
	it.FinishedAt = timePtr(finished)
	return it, nil
}

func phaseOr(p string) string {
	if p == "" {
		return domain.PhaseBuild
	}
	return p
}

func timePtr(t sql.NullTime) *time.Time {
	if !t.Valid {
		return nil
	}
	v := t.Time
	return &v
}
