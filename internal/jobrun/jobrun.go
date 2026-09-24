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

// Package jobrun runs a workflow, once, wherever it was asked for.
//
// It exists because submission arrived from two directions — a command line and
// a dialog — and the second must not be a reimplementation of the first. A
// submit button that starts a subtly different run than `distill` does is how
// two code paths drift until one of them is wrong and nobody knows which.
//
// What differs between the two callers is REPORTING, and only that: a terminal
// rewrites a line in place, a browser polls a row. So progress is a callback and
// everything else is shared.
package jobrun

import (
	"context"
	"time"

	"codedistill/internal/decompose"
	"codedistill/internal/domain"
)

// Store is the slice of storage a run touches.
//
// Narrow on purpose: this package is the join between the CLI and the server,
// and a fat dependency on the whole store would drag one into the other.
type Store interface {
	CreateJob(ctx context.Context, j *domain.Job) error
	UpdateJobProgress(ctx context.Context, id, phase string, done, total int, at time.Time) error
	StartJobPhase(ctx context.Context, p *domain.JobPhase, at time.Time) error
	UpdateJobPhase(ctx context.Context, jobID string, done, total, tokensIn, tokensOut int) error
	FinishJobPhases(ctx context.Context, jobID, errMsg string, at time.Time) error
	FinishJob(ctx context.Context, id, status, errMsg string, at time.Time) error

	CreateScratchpadItem(ctx context.Context, it *domain.ScratchpadItem) error

	SaveDecomposeRun(ctx context.Context, run *domain.DecomposeRun, props []*domain.DecomposeProposal) error
	SaveDecomposePass(ctx context.Context, p *domain.DecomposePass, moves []*domain.DecomposeMove) error
	CompletedPasses(ctx context.Context, projectID, sourceHash string) (map[int][]*domain.DecomposeMove, error)
	ForgetDecomposePasses(ctx context.Context, projectID, sourceHash string) error
}

// Progress reports where a run is. Called often; must not block.
type Progress func(phase string, done, total int)

// reporter turns raw progress into both the live row and the per-phase record.
//
// Two different things are written because they answer different questions:
// jobs.phase says what is happening NOW and is overwritten, while job_phases
// accumulates so a finished run can say it spent 24 minutes reading and 5
// sorting. Neither can be derived from the other.
type reporter struct {
	store      Store
	jobID      string
	workerType string
	last       string
	extra      Progress
	newID      func() string
}

func (r *reporter) report(ctx context.Context, phase string, done, total int) {
	now := time.Now().UTC()
	changed := phase != r.last
	if changed {
		r.last = phase
	}
	// Errors are dropped deliberately: progress reporting must never be the
	// reason a forty-minute run dies. A missing tick costs a stale display.
	_ = r.store.UpdateJobProgress(ctx, r.jobID, phase, done, total, now)
	if changed {
		// Starting a phase closes the previous one, so no caller has to
		// remember to.
		_ = r.store.StartJobPhase(ctx, &domain.JobPhase{
			ID: r.newID(), JobID: r.jobID, Phase: phase, WorkerType: r.workerType,
		}, now)
	}
	_ = r.store.UpdateJobPhase(ctx, r.jobID, done, total, 0, 0)
	if r.extra != nil {
		r.extra(phase, done, total)
	}
}

// passStore checkpoints reading passes, keyed on the DOCUMENT's content hash
// rather than the job.
//
// A retry after an interruption is a new job that must inherit the earlier run's
// completed passes. Keying by job would start over from zero, which is the thing
// checkpointing exists to prevent.
type passStore struct {
	store     Store
	projectID string
	hash      string
	newID     func() string
	Resumed   int
}

func (p *passStore) Completed(ctx context.Context) (map[int][]decompose.Move, error) {
	rows, err := p.store.CompletedPasses(ctx, p.projectID, p.hash)
	if err != nil {
		return nil, err
	}
	out := make(map[int][]decompose.Move, len(rows))
	for start, ms := range rows {
		moves := make([]decompose.Move, 0, len(ms))
		for _, m := range ms {
			moves = append(moves, decompose.Move{
				Sentence: m.Sentence, Role: m.Role, Label: m.Label, Target: m.Target,
			})
		}
		out[start] = moves
		p.Resumed++
	}
	return out, nil
}

func (p *passStore) Save(ctx context.Context, start, end int, moves []decompose.Move, rejected int) error {
	rows := make([]*domain.DecomposeMove, 0, len(moves))
	for _, m := range moves {
		rows = append(rows, &domain.DecomposeMove{
			ID: p.newID(), Sentence: m.Sentence, Role: m.Role,
			Label: m.Label, Target: m.Target,
		})
	}
	return p.store.SaveDecomposePass(ctx, &domain.DecomposePass{
		ProjectID: p.projectID, SourceHash: p.hash,
		WindowStart: start, WindowEnd: end, Rejected: rejected,
		CreatedAt: time.Now().UTC(),
	}, rows)
}
