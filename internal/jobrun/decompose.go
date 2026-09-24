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
	"os"
	"strings"
	"time"

	"codedistill/internal/decompose"
	"codedistill/internal/domain"
)

// DecomposeInput is everything the run needs that it cannot work out itself.
type DecomposeInput struct {
	ProjectID    string
	ScratchpadID string

	// SourceItemID names an EXISTING scratchpad item holding the document.
	//
	// When set, nothing new is created. That is the normal case from the UI: a
	// dropped or uploaded document is already an item, and decompose creating a
	// second copy of it — which it did — left two identical items with only one
	// of them cited by the proposals.
	//
	// Empty means the caller has only bytes (the CLI reading a path), and the
	// item is created here.
	SourceItemID string
	Label        string   // what to call the document
	Lines        []string // already read and normalised
	SourceHash   string
	Format       string
	FileInfo     os.FileInfo // may be nil: a document need not have come from a file
	Again        bool        // re-read everything rather than resuming

	// Gen is the model. Chosen by the caller, because "which model reads the
	// document" is a configuration question and this package should not have an
	// opinion about where the answer lives.
	Gen decompose.Generator

	// JobID, when set, is used rather than minted. The submit endpoint needs
	// the id BEFORE the run starts so it can answer the request with something
	// the monitor can follow; a submission that returns nothing to watch is
	// indistinguishable from one that did nothing.
	JobID string

	// OnJobCreated fires once the row exists, before any model is called.
	// Anything keyed on the job — the parameters it was submitted with — has to
	// wait for the row, and waiting for the RUN to finish would mean a job in
	// flight could not say what it was asked for.
	OnJobCreated func(*domain.Job)

	// NewID mints ids. Injected so a test can make a run reproducible.
	NewID func() string
}

// DecomposeResult is what the caller needs to report afterwards.
type DecomposeResult struct {
	Job      *domain.Job
	DocItem  *domain.ScratchpadItem
	Run      *domain.DecomposeRun
	Detail   *decompose.Result
	Proposal []*domain.DecomposeProposal
	Resumed  int
}

// RunDecompose reads a document and records what it implies.
//
// The job row is created here rather than by the caller, so that a run started
// from a dialog and one started from the command line produce the same shape of
// history — including the scratchpad item holding the document, which has to
// exist before anything can cite it.
//
// Nothing is created as a work item. The run produces PROPOSALS; turning them
// into items is a separate, human act.
func RunDecompose(ctx context.Context, store Store, in DecomposeInput, progress Progress) (*DecomposeResult, error) {
	now := time.Now().UTC()

	// The document must exist as a scratchpad item before anything else: it is
	// what every derived proposal cites. Usually it already does — it was
	// dropped or uploaded — and then this creates nothing.
	docItem := &domain.ScratchpadItem{ID: in.SourceItemID}
	if in.SourceItemID == "" {
		docItem = &domain.ScratchpadItem{
			ID: in.NewID(), ScratchpadID: in.ScratchpadID, Name: in.Label,
			ContentType: "text", Content: strings.Join(in.Lines, "\n"),
			// Not classified: it is the source, not a candidate. Running the
			// classifier over a 150-sentence specification would produce one
			// nonsense item and hide the real ones.
			ClassificationState: "skipped", SkippedReason: "override_skip",
			CreatedAt: now, UpdatedAt: now,
		}
		if err := store.CreateScratchpadItem(ctx, docItem); err != nil {
			return nil, err
		}
	}

	jobID := in.JobID
	if jobID == "" {
		jobID = in.NewID()
	}
	job := &domain.Job{
		ID: jobID, ProjectID: in.ProjectID, Type: domain.JobDecompose,
		ScopeKind: "scratchpad_item", ScopeID: docItem.ID, ScopeLabel: in.Label,
		Status: domain.JobRunning, Model: in.Gen.Model(), StartedAt: now, UpdatedAt: now,
	}
	if err := store.CreateJob(ctx, job); err != nil {
		return nil, err
	}

	if in.OnJobCreated != nil {
		in.OnJobCreated(job)
	}

	res := &DecomposeResult{Job: job, DocItem: docItem}

	passes := &passStore{store: store, projectID: in.ProjectID, hash: in.SourceHash, newID: in.NewID}
	if in.Again {
		// An explicit re-read must actually re-read.
		_ = store.ForgetDecomposePasses(ctx, in.ProjectID, in.SourceHash)
	} else if done, err := passes.Completed(ctx); err == nil {
		res.Resumed = len(done)
	}

	rep := &reporter{store: store, jobID: job.ID,
		workerType: domain.WorkerDecomposer, extra: progress, newID: in.NewID}

	out, runErr := decompose.Run(ctx, in.Gen, in.Lines,
		decompose.Options{Passes: passes},
		func(phase string, done, total int) { rep.report(ctx, phase, done, total) })
	if runErr != nil {
		// Closed with the reason attached. A failed run that simply stops
		// updating is indistinguishable from a hung one.
		fin := finishCtx(ctx)
		_ = store.FinishJobPhases(fin, job.ID, runErr.Error(), time.Now().UTC())
		_ = store.FinishJob(fin, job.ID, statusFor(ctx), runErr.Error(), time.Now().UTC())
		return res, runErr
	}
	res.Detail = out

	props := make([]*domain.DecomposeProposal, 0, len(out.Proposals))
	for _, p := range out.Proposals {
		props = append(props, &domain.DecomposeProposal{
			ID: in.NewID(), JobID: job.ID, ProjectID: in.ProjectID,
			Kind: p.Kind, Subject: p.Subject, Body: p.Body,
			Origin: p.Origin(), Corroborated: p.Corroborated,
			ExternalRef: p.ExternalRef, Priority: p.Priority,
			Lines: p.Lines, Status: domain.ProposalPending, CreatedAt: now,
		})
	}
	res.Proposal = props

	var modified *time.Time
	var size int64
	if in.FileInfo != nil {
		m := in.FileInfo.ModTime().UTC()
		modified, size = &m, in.FileInfo.Size()
	}
	run := &domain.DecomposeRun{
		JobID: job.ID, ProjectID: in.ProjectID, SourceItemID: docItem.ID, SourceLabel: in.Label,
		SourceHash: out.SourceHash, SourceBytes: size, ModifiedAt: modified,
		Sentences: len(out.Document.Sentences), TablesRead: out.TablesRead,
		TablesDeclined: out.TablesDeclined,
		Corroborated:   len(out.Recon.Corroborated), TableOnly: len(out.Recon.TableOnly),
		ProseOnly: len(out.Recon.ProseOnly), DeclinedNodes: len(out.Skipped),
		RejectedMoves: len(out.Rejected), InferredLinks: out.Graph.InferredLinks,
		Model: out.Model, CreatedAt: now,
	}
	for _, e := range out.Document.Excluded {
		run.ExcludedLines += e.Line2 - e.Line1 + 1
	}
	res.Run = run

	if err := store.SaveDecomposeRun(ctx, run, props); err != nil {
		fin := finishCtx(ctx)
		_ = store.FinishJobPhases(fin, job.ID, err.Error(), time.Now().UTC())
		_ = store.FinishJob(fin, job.ID, domain.JobFailed, err.Error(), time.Now().UTC())
		return res, err
	}
	_ = store.FinishJobPhases(ctx, job.ID, "", time.Now().UTC())
	_ = store.FinishJob(ctx, job.ID, domain.JobSucceeded, "", time.Now().UTC())
	return res, nil
}

// statusFor distinguishes a cancelled run from a failed one.
//
// They look identical from inside — both surface as a context error — but they
// mean opposite things in the history: one is a person pressing Cancel, the
// other is something to investigate.
func statusFor(ctx context.Context) string {
	if ctx.Err() != nil {
		return domain.JobCancelled
	}
	return domain.JobFailed
}

// finishCtx gives the closing writes a context that is not already dead.
//
// A cancelled run's ctx is done, so using it to record the cancellation would
// fail and leave the row stuck at "running" forever — the exact state that makes
// a job monitor untrustworthy.
func finishCtx(ctx context.Context) context.Context {
	if ctx.Err() == nil {
		return ctx
	}
	c, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	// Deliberately leaked to the timeout: the caller returns immediately after
	// the writes, and cancelling here would race them.
	_ = cancel
	return c
}
