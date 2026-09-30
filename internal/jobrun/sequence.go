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

package jobrun

import (
	"context"
	"fmt"
	"time"

	"codedistill/internal/decompose"
	"codedistill/internal/domain"
)

// Working out what has to happen before what.
//
// Runs over ITEMS, whatever produced them — a decomposition, the classifier, a
// person typing all forty-nine in by hand. That is the whole point: ordering is
// a property of the work, not of the document some of it came from, and the
// sweep only ever needed a label, a kind, a subject and a sentence.

// SequenceStore is the slice of storage this run touches.
type SequenceStore interface {
	CreateJob(ctx context.Context, j *domain.Job) error
	UpdateJobProgress(ctx context.Context, id, phase string, done, total int, at time.Time) error
	StartJobPhase(ctx context.Context, p *domain.JobPhase, at time.Time) error
	UpdateJobPhase(ctx context.Context, jobID string, done, total, tokensIn, tokensOut int) error
	FinishJobPhases(ctx context.Context, jobID, errMsg string, at time.Time) error
	FinishJob(ctx context.Context, id, status, errMsg string, at time.Time) error

	CreateDependency(ctx context.Context, d *domain.ItemDependency) error
	KnownEdge(ctx context.Context, projectID, fromID, toID string) (bool, error)
}

// SequenceInput is what the run needs.
type SequenceInput struct {
	ProjectID string
	// ScopeLabel is what the job row shows: the scratchpad's name, usually.
	ScopeLabel string
	ScopeKind  string
	ScopeID    string

	Items []decompose.SeqItem
	// Kinds maps item id -> item kind, so an edge can be stored against the
	// right table without the sweep having to care.
	Kinds map[string]string

	Gen   decompose.Generator
	JobID string
	NewID func() string

	UserID string
}

// SequenceResult reports what came back, including what was thrown away.
type SequenceResult struct {
	Job *domain.Job

	Proposed int // edges stored and in force
	Rejected int // edges the sweep discarded as unusable
	Known    int // edges already ruled on before, not re-offered
}

// RunSequence asks for the ordering and records what comes back as ADVISORY.
//
// Not queued for review, which is the difference between this and a proposal.
// The cost of being wrong is asymmetric: accepting a bad proposal creates junk
// work carried for months, while a bad edge only misorders a queue — and it is
// noticed within seconds, because the person looks at the item and thinks "I can
// actually do this now."
//
// Forty-nine proposals to ratify and then forty-eight edges to ratify is more
// work than the ordering saves. So an edge stands until someone disagrees with
// it, and the moment to disagree is when it is actually stopping them — which
// is in the readiness view, with the item in front of them.
func RunSequence(ctx context.Context, store SequenceStore, in SequenceInput) (*SequenceResult, error) {
	now := time.Now().UTC()
	jobID := in.JobID
	if jobID == "" {
		jobID = in.NewID()
	}

	job := &domain.Job{
		ID: jobID, ProjectID: in.ProjectID, Type: domain.JobSequence,
		ScopeKind: in.ScopeKind, ScopeID: in.ScopeID, ScopeLabel: in.ScopeLabel,
		Status: domain.JobRunning, Model: in.Gen.Model(), StartedAt: now, UpdatedAt: now,
	}
	if err := store.CreateJob(ctx, job); err != nil {
		return nil, err
	}
	res := &SequenceResult{Job: job}

	rep := &reporter{store: store, jobID: job.ID,
		workerType: domain.WorkerDecomposer, newID: in.NewID}
	// One call, so one phase with a known total. An indeterminate bar for a
	// single step reads as broken.
	rep.report(ctx, "ordering", 0, 1)

	edges, rejected, err := decompose.Sequence(ctx, in.Gen, in.Items)
	if err != nil {
		fin := finishCtx(ctx)
		_ = store.FinishJobPhases(fin, job.ID, err.Error(), time.Now().UTC())
		_ = store.FinishJob(fin, job.ID, statusFor(ctx), err.Error(), time.Now().UTC())
		return res, err
	}
	res.Rejected = rejected
	rep.report(ctx, "ordering", 1, 1)

	for _, e := range edges {
		// Already ruled on, in either direction? Then it is not news. Re-asking
		// a question someone answered is how a review screen teaches people to
		// stop reading it.
		if known, err := store.KnownEdge(ctx, in.ProjectID, e.From, e.To); err == nil && known {
			res.Known++
			continue
		}
		d := &domain.ItemDependency{
			ID: in.NewID(), ProjectID: in.ProjectID,
			FromKind: in.Kinds[e.From], FromID: e.From,
			ToKind: in.Kinds[e.To], ToID: e.To,
			Kind: e.Kind, Rationale: e.Rationale,
			Origin: domain.DependencyFromDecompose,
			Status: domain.EdgeAdvisory, JobID: job.ID,
			CreatedAt: time.Now().UTC(), CreatedBy: in.UserID,
		}
		if err := store.CreateDependency(ctx, d); err != nil {
			// Per edge, not fatal: one unstorable edge must not cost the rest.
			continue
		}
		res.Proposed++
	}

	_ = store.FinishJobPhases(ctx, job.ID, "", time.Now().UTC())
	_ = store.FinishJob(ctx, job.ID, domain.JobSucceeded, "", time.Now().UTC())
	return res, nil
}

// SeqItemsFrom turns a project's work into something orderable.
//
// Deliberately takes the four kinds together rather than one at a time:
// dependencies cross kinds, and a bug blocking a use case is exactly the kind
// of edge a per-table sweep could never see.
func SeqItemsFrom(
	useCases []*domain.UseCaseItem,
	todos []*domain.TodoItem,
	bugs []*domain.BugItem,
) ([]decompose.SeqItem, map[string]string) {

	items := make([]decompose.SeqItem, 0, len(useCases)+len(todos)+len(bugs))
	kinds := make(map[string]string, cap(items))

	for _, u := range useCases {
		items = append(items, decompose.SeqItem{
			ID: u.ID, Ref: refOf(u.Number), Kind: "use_case",
			Subject: u.Subject, Body: u.Description,
		})
		kinds[u.ID] = "use_case"
	}
	for _, t := range todos {
		items = append(items, decompose.SeqItem{
			ID: t.ID, Kind: "todo", Subject: t.Subject,
		})
		kinds[t.ID] = "todo"
	}
	for _, b := range bugs {
		items = append(items, decompose.SeqItem{
			ID: b.ID, Kind: "bug", Subject: b.Subject,
		})
		kinds[b.ID] = "bug"
	}
	return items, kinds
}

// refOf renders a use case's number as the handle the model is shown. A
// meaningful label is echoed back far more reliably than an opaque id.
func refOf(number int) string {
	if number <= 0 {
		return ""
	}
	return fmt.Sprintf("UC-%d", number)
}
