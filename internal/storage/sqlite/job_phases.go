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
	"sort"
	"time"

	"codedistill/internal/domain"
)

const phaseColumns = `id, job_id, ordinal, phase, worker_type, round,
	done, total, tokens_in, tokens_out, error, started_at, finished_at`

// StartJobPhase opens a phase and closes whichever one was open.
//
// Closing the previous one here rather than asking callers to do it: a worker
// that moves on without closing leaves a phase that appears to have run
// forever, and every caller would have to remember. The ordinal comes from
// what is already recorded, so phases render in the order they happened even
// when two start within the same second.
func (s *Store) StartJobPhase(ctx context.Context, p *domain.JobPhase, at time.Time) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx,
		`UPDATE job_phases SET finished_at = ? WHERE job_id = ? AND finished_at IS NULL`,
		at, p.JobID); err != nil {
		return fmt.Errorf("close open phase: %w", err)
	}

	var next int
	if err := tx.QueryRowContext(ctx,
		`SELECT COALESCE(MAX(ordinal)+1, 0) FROM job_phases WHERE job_id = ?`,
		p.JobID).Scan(&next); err != nil {
		return fmt.Errorf("next phase ordinal: %w", err)
	}
	p.Ordinal, p.StartedAt = next, at

	if _, err := tx.ExecContext(ctx,
		`INSERT INTO job_phases (`+phaseColumns+`)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		p.ID, p.JobID, p.Ordinal, p.Phase, p.WorkerType, p.Round,
		p.Done, p.Total, p.TokensIn, p.TokensOut, nullable(p.Error),
		p.StartedAt, nil); err != nil {
		return fmt.Errorf("start phase: %w", err)
	}
	return tx.Commit()
}

// UpdateJobPhase advances the open phase's counters.
func (s *Store) UpdateJobPhase(ctx context.Context, jobID string, done, total, tokensIn, tokensOut int) error {
	_, err := s.DB.ExecContext(ctx,
		`UPDATE job_phases SET done = ?, total = ?,
		        tokens_in = tokens_in + ?, tokens_out = tokens_out + ?
		 WHERE job_id = ? AND finished_at IS NULL`,
		done, total, tokensIn, tokensOut, jobID)
	return err
}

// FinishJobPhases closes any phase still open, which is what a job reaching a
// terminal state means. errMsg is stamped on the phase that was running, so a
// failed run shows WHERE it failed rather than only that it did.
func (s *Store) FinishJobPhases(ctx context.Context, jobID, errMsg string, at time.Time) error {
	_, err := s.DB.ExecContext(ctx,
		`UPDATE job_phases SET finished_at = ?, error = ?
		 WHERE job_id = ? AND finished_at IS NULL`,
		at, nullable(errMsg), jobID)
	return err
}

// ListJobPhases returns a run's phases in order.
func (s *Store) ListJobPhases(ctx context.Context, jobID string) ([]*domain.JobPhase, error) {
	rows, err := s.DB.QueryContext(ctx,
		`SELECT `+phaseColumns+` FROM job_phases WHERE job_id = ? ORDER BY ordinal`, jobID)
	if err != nil {
		return nil, fmt.Errorf("list job phases: %w", err)
	}
	defer rows.Close()

	var out []*domain.JobPhase
	for rows.Next() {
		p := &domain.JobPhase{}
		var errMsg sql.NullString
		var fin sql.NullTime
		if err := rows.Scan(&p.ID, &p.JobID, &p.Ordinal, &p.Phase, &p.WorkerType,
			&p.Round, &p.Done, &p.Total, &p.TokensIn, &p.TokensOut, &errMsg,
			&p.StartedAt, &fin); err != nil {
			return nil, err
		}
		p.Error = errMsg.String
		if fin.Valid {
			t := fin.Time
			p.FinishedAt = &t
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// PhaseNorms returns what each phase of a workflow usually costs.
//
// This is the number that turns elapsed time into a verdict: "12 minutes"
// means nothing until you know the usual reading pass is 24. Computed from
// SUCCEEDED runs only — a run that failed after 90 seconds would drag the
// median toward a figure no healthy run will ever match.
//
// Median rather than mean, because one 46-minute outlier should not move the
// bar a human judges against.
func (s *Store) PhaseNorms(ctx context.Context, projectID, workflowID string) ([]domain.PhaseNorm, error) {
	rows, err := s.DB.QueryContext(ctx,
		`SELECT ph.phase, ph.started_at, ph.finished_at
		 FROM job_phases ph
		 JOIN jobs j ON j.id = ph.job_id
		 WHERE j.project_id = ? AND j.type = ? AND j.status = ?
		   AND ph.finished_at IS NOT NULL
		 ORDER BY ph.phase`,
		projectID, workflowID, domain.JobSucceeded)
	if err != nil {
		return nil, fmt.Errorf("phase norms: %w", err)
	}
	defer rows.Close()

	durations := map[string][]int{}
	for rows.Next() {
		var phase string
		var start, end time.Time
		if err := rows.Scan(&phase, &start, &end); err != nil {
			return nil, err
		}
		if d := int(end.Sub(start).Seconds()); d >= 0 {
			durations[phase] = append(durations[phase], d)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	var out []domain.PhaseNorm
	for phase, ds := range durations {
		sort.Ints(ds)
		out = append(out, domain.PhaseNorm{
			Phase: phase, Runs: len(ds), MedianSec: ds[len(ds)/2],
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Phase < out[j].Phase })
	return out, nil
}
