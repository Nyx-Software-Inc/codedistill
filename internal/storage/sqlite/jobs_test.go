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
	"errors"
	"testing"
	"time"

	"codedistill/internal/domain"
	"codedistill/internal/storage"
)

func seedJob(t *testing.T, s *Store, id, jobType, scopeID, status string) *domain.Job {
	t.Helper()
	p := seedProject(t, s)
	return seedJobIn(t, s, p.ID, id, jobType, scopeID, status)
}

func seedJobIn(t *testing.T, s *Store, projectID, id, jobType, scopeID, status string) *domain.Job {
	t.Helper()
	now := fixedTime(t)
	j := &domain.Job{
		ID: id, ProjectID: projectID, Type: jobType,
		ScopeKind: "scratchpad_item", ScopeID: scopeID, ScopeLabel: "a document",
		Status: status, StartedAt: now, UpdatedAt: now,
	}
	if err := s.CreateJob(context.Background(), j); err != nil {
		t.Fatalf("create job: %v", err)
	}
	return j
}

func TestActiveJobForGatesSecondRun(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	j := seedJob(t, s, "j1", domain.JobDecompose, "item-1", domain.JobRunning)

	// The singleton check architecture drafting currently does with an
	// in-memory map, now surviving a restart.
	got, err := s.ActiveJobFor(ctx, domain.JobDecompose, "scratchpad_item", "item-1")
	if err != nil || got == nil {
		t.Fatalf("ActiveJobFor = %v, %v — want the running job", got, err)
	}
	if got.ID != j.ID {
		t.Fatalf("got job %s, want %s", got.ID, j.ID)
	}

	// A different scope is not blocked.
	if other, _ := s.ActiveJobFor(ctx, domain.JobDecompose, "scratchpad_item", "item-2"); other != nil {
		t.Errorf("a job on item-1 blocked item-2")
	}
	// Nor is a different type on the same scope.
	if other, _ := s.ActiveJobFor(ctx, domain.JobArchDraft, "scratchpad_item", "item-1"); other != nil {
		t.Errorf("a decompose job blocked an arch_draft job")
	}

	// Once finished, the scope is free.
	if err := s.FinishJob(ctx, "j1", domain.JobSucceeded, "", fixedTime(t)); err != nil {
		t.Fatalf("finish: %v", err)
	}
	if again, _ := s.ActiveJobFor(ctx, domain.JobDecompose, "scratchpad_item", "item-1"); again != nil {
		t.Errorf("finished job still blocks its scope: %+v", again)
	}
}

func TestProgressDoesNotResurrectAFinishedJob(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	seedJob(t, s, "j1", domain.JobDecompose, "item-1", domain.JobRunning)
	now := fixedTime(t)

	if err := s.FinishJob(ctx, "j1", domain.JobCancelled, "", now); err != nil {
		t.Fatalf("cancel: %v", err)
	}
	// A worker pass already in flight writes progress after the cancel. It must
	// not un-cancel the job, or a cancelled run keeps showing as active.
	if err := s.UpdateJobProgress(ctx, "j1", "reading", 12, 31, now); err != nil {
		t.Fatalf("progress after cancel returned an error: %v", err)
	}
	got, _ := s.GetJob(ctx, "j1")
	if got.Status != domain.JobCancelled {
		t.Fatalf("status = %q, want cancelled", got.Status)
	}
	if got.Done != 0 {
		t.Errorf("done = %d, want 0 — progress leaked into a cancelled job", got.Done)
	}
}

func TestFinishJobRejectsNonTerminalAndDropsSpuriousError(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	seedJob(t, s, "j1", domain.JobDecompose, "item-1", domain.JobRunning)
	now := fixedTime(t)

	if err := s.FinishJob(ctx, "j1", domain.JobRunning, "", now); err == nil {
		t.Errorf("finishing a job AS running was accepted")
	}
	if err := s.FinishJob(ctx, "j1", "banana", "", now); err == nil {
		t.Errorf("an unknown status was accepted")
	}
	// An error message on a success would make the dashboard lie.
	if err := s.FinishJob(ctx, "j1", domain.JobSucceeded, "it went fine actually", now); err != nil {
		t.Fatalf("finish: %v", err)
	}
	got, _ := s.GetJob(ctx, "j1")
	if got.Error != "" {
		t.Errorf("error = %q on a succeeded job, want empty", got.Error)
	}
	if got.FinishedAt == nil {
		t.Errorf("finished_at not set")
	}
}

func TestInterruptStaleJobsUnblocksTheScope(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	p := seedProject(t, s)
	seedJobIn(t, s, p.ID, "j1", domain.JobDecompose, "item-1", domain.JobRunning)
	seedJobIn(t, s, p.ID, "j2", domain.JobArchDraft, "item-2", domain.JobRunning)
	done := seedJobIn(t, s, p.ID, "j3", domain.JobDecompose, "item-3", domain.JobRunning)
	if err := s.FinishJob(ctx, done.ID, domain.JobSucceeded, "", fixedTime(t)); err != nil {
		t.Fatalf("finish: %v", err)
	}

	// This is what startup does. Without it a crashed decomposition holds its
	// document forever and the user can never retry it.
	n, err := s.InterruptStaleJobs(ctx, fixedTime(t))
	if err != nil {
		t.Fatalf("interrupt: %v", err)
	}
	if n != 2 {
		t.Fatalf("interrupted %d jobs, want 2 (the finished one must be left alone)", n)
	}
	got, _ := s.GetJob(ctx, "j1")
	if got.Status != domain.JobInterrupted {
		t.Fatalf("status = %q, want interrupted", got.Status)
	}
	if blocked, _ := s.ActiveJobFor(ctx, domain.JobDecompose, "scratchpad_item", "item-1"); blocked != nil {
		t.Errorf("an interrupted job still blocks its scope — the document can never be retried")
	}
	if fin, _ := s.GetJob(ctx, "j3"); fin.Status != domain.JobSucceeded {
		t.Errorf("a finished job was rewritten to %q", fin.Status)
	}
}

func TestJobTokensAccumulate(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	seedJob(t, s, "j1", domain.JobDecompose, "item-1", domain.JobRunning)
	now := fixedTime(t)

	for i := 0; i < 3; i++ {
		if err := s.AddJobTokens(ctx, "j1", 1000, 250, now); err != nil {
			t.Fatalf("add tokens: %v", err)
		}
	}
	got, _ := s.GetJob(ctx, "j1")
	if got.TokensIn != 3000 || got.TokensOut != 750 {
		t.Fatalf("tokens = %d/%d, want 3000/750", got.TokensIn, got.TokensOut)
	}
}

func TestListJobsFiltersAndOrders(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	p := seedProject(t, s)
	seedJobIn(t, s, p.ID, "j1", domain.JobDecompose, "item-1", domain.JobRunning)
	seedJobIn(t, s, p.ID, "j2", domain.JobArchDraft, "item-2", domain.JobRunning)
	if err := s.FinishJob(ctx, "j2", domain.JobFailed, "model unreachable", fixedTime(t)); err != nil {
		t.Fatalf("finish: %v", err)
	}

	all, err := s.ListJobs(ctx, p.ID, nil, 0)
	if err != nil || len(all) != 2 {
		t.Fatalf("ListJobs = %d jobs, %v — want 2", len(all), err)
	}
	running, _ := s.ListJobs(ctx, p.ID, []string{domain.JobRunning}, 0)
	if len(running) != 1 || running[0].ID != "j1" {
		t.Fatalf("running filter returned %+v", running)
	}
	failed, _ := s.ListJobs(ctx, p.ID, []string{domain.JobFailed}, 0)
	if len(failed) != 1 || failed[0].Error != "model unreachable" {
		t.Fatalf("failed job lost its error: %+v", failed)
	}
}

// The queue's life of a job: waits in line (holding its scope), leaves it once,
// and its runner then records the start without losing its place's details.
func TestQueuedJobLifecycle(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	p := seedProject(t, s)
	now := fixedTime(t)
	q := &domain.Job{ID: "q1", ProjectID: p.ID, Type: domain.JobFixFindings, ScopeKind: "scratchpad", ScopeID: "sp1",
		Status: domain.JobQueued, Priority: domain.PriorityHigh, QueuedAt: &now, StartedAt: now, UpdatedAt: now, CreatedBy: "rich"}
	if err := s.CreateJob(ctx, q); err != nil {
		t.Fatal(err)
	}
	seedJobIn(t, s, p.ID, "r1", domain.JobDecompose, "doc", domain.JobRunning)

	if got, _ := s.ActiveJobFor(ctx, domain.JobFixFindings, "scratchpad", "sp1"); got == nil || got.ID != "q1" {
		t.Fatalf("a queued job must hold its scope: %+v", got)
	}
	if n, _ := s.CountRunningJobs(ctx); n != 1 {
		t.Errorf("running = %d", n)
	}
	if ls, _ := s.ListQueuedJobs(ctx); len(ls) != 1 || ls[0].Priority != domain.PriorityHigh || ls[0].QueuedAt == nil {
		t.Fatalf("queued = %+v", ls)
	}
	if ok, err := s.SetJobPriority(ctx, "q1", domain.PriorityLow, now); !ok || err != nil {
		t.Fatalf("set priority: %v %v", ok, err)
	}
	if _, err := s.SetJobPriority(ctx, "q1", "urgent", now); err == nil {
		t.Error("an unknown priority was accepted")
	}

	later := now.Add(time.Hour)
	if ok, _ := s.StartQueuedJob(ctx, "q1", later); !ok {
		t.Fatal("could not start it")
	}
	if ok, _ := s.StartQueuedJob(ctx, "q1", later); ok {
		t.Fatal("started twice")
	}
	if ok, _ := s.SetJobPriority(ctx, "q1", domain.PriorityHigh, later); ok {
		t.Error("a running job's priority changed")
	}
	// The runner records its start as it always has.
	run := &domain.Job{ID: "q1", ProjectID: p.ID, Type: domain.JobFixFindings, ScopeKind: "scratchpad", ScopeID: "sp1",
		ScopeLabel: "Bugs", Status: domain.JobRunning, Phase: "preparing", Branch: "cd/fix/q1", BaseSHA: "abc",
		StartedAt: later.Add(time.Second), UpdatedAt: later.Add(time.Second)}
	if err := s.CreateJob(ctx, run); err != nil {
		t.Fatalf("runner's start: %v", err)
	}
	j, _ := s.GetJob(ctx, "q1")
	if j.Status != domain.JobRunning || j.Branch != "cd/fix/q1" || j.Phase != "preparing" || j.ScopeLabel != "Bugs" ||
		j.Priority != domain.PriorityLow || j.QueuedAt == nil || !j.StartedAt.Equal(later) || j.CreatedBy != "rich" {
		t.Errorf("after the runner's start: %+v", j)
	}
}

// A job cancelled while it waits never runs: its runner's start is refused,
// and a cancel needs no runner to have existed.
func TestCancelledQueuedJobIsNotRevived(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	now := fixedTime(t)
	seedJob(t, s, "q1", domain.JobSequence, "sp", domain.JobQueued)
	if err := s.FinishJob(ctx, "q1", domain.JobCancelled, "", now); err != nil {
		t.Fatal(err)
	}
	if j, _ := s.GetJob(ctx, "q1"); j.Status != domain.JobCancelled {
		t.Fatalf("status = %s", j.Status)
	}
	if ok, _ := s.StartQueuedJob(ctx, "q1", now); ok {
		t.Error("a cancelled job left the line")
	}
	j, _ := s.GetJob(ctx, "q1")
	j.Status = domain.JobRunning
	if err := s.CreateJob(ctx, j); !errors.Is(err, storage.ErrJobNotStarting) {
		t.Errorf("runner's start on a cancelled job = %v", err)
	}
	if j, _ := s.GetJob(ctx, "q1"); j.Status != domain.JobCancelled {
		t.Errorf("revived: %s", j.Status)
	}
	if err := s.FinishJob(ctx, "q1", domain.JobQueued, "", now); err == nil {
		t.Error("queued accepted as an ending")
	}
}

// Pausing and resuming: who paused is kept while the job waits and cleared
// when it runs; a restart puts resumable jobs back in line and leaves the
// rest to be interrupted; an item stopped part-way loses its attempt's claims.
func TestPauseRequeueAndReset(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	p := seedProject(t, s)
	now := fixedTime(t)
	seedJobIn(t, s, p.ID, "fix", domain.JobFixFindings, "sp1", domain.JobRunning)
	seedJobIn(t, s, p.ID, "doc", domain.JobDecompose, "d1", domain.JobRunning)

	if run, _ := s.ListRunningJobs(ctx); len(run) != 2 {
		t.Fatalf("running = %d", len(run))
	}
	if ok, err := s.PauseJob(ctx, "fix", domain.PausedByQueue, now); !ok || err != nil {
		t.Fatalf("pause: %v %v", ok, err)
	}
	if ok, _ := s.PauseJob(ctx, "fix", domain.PausedByQueue, now); ok {
		t.Error("paused twice")
	}
	if ok, _ := s.RequeueJob(ctx, "fix", domain.PausedByQueue, now); !ok {
		t.Fatal("requeue")
	}
	j, _ := s.GetJob(ctx, "fix")
	if j.Status != domain.JobQueued || j.PausedBy != domain.PausedByQueue || j.QueuedAt == nil || j.FinishedAt != nil {
		t.Fatalf("requeued = %+v", j)
	}
	if ok, _ := s.StartQueuedJob(ctx, "fix", now); !ok {
		t.Fatal("start")
	}
	if j, _ := s.GetJob(ctx, "fix"); j.PausedBy != "" {
		t.Errorf("still paused by %q once running", j.PausedBy)
	}

	// The restart: the fix job goes back in line, the decomposition does not.
	if n, err := s.RequeueRunningJobs(ctx, []string{domain.JobFixFindings}, now); n != 1 || err != nil {
		t.Fatalf("requeue at startup: %d %v", n, err)
	}
	if n, _ := s.InterruptStaleJobs(ctx, now); n != 1 {
		t.Errorf("interrupted = %d", n)
	}
	if j, _ := s.GetJob(ctx, "fix"); j.Status != domain.JobQueued || j.PausedBy != domain.PausedByRestart {
		t.Errorf("fix after restart = %+v", j)
	}

	// An item stopped part-way.
	it := &domain.JobItem{ID: "i1", JobID: "fix", OwnerType: "bug_item", OwnerID: "b1", Label: "md5", Status: domain.JobItemQueued}
	if err := s.CreateJobItem(ctx, it); err != nil {
		t.Fatal(err)
	}
	started := now
	it.Status, it.Rounds, it.Phase, it.StartedAt, it.Reason = domain.JobItemRunning, 2, domain.PhasePlan, &started, "x"
	if err := s.UpdateJobItem(ctx, it); err != nil {
		t.Fatal(err)
	}
	if err := s.CreateJobClaim(ctx, &domain.JobClaim{ID: "c1", JobID: "fix", JobItemID: "i1", Round: 1, Phase: domain.PhasePlan,
		AuthorRole: "solutioner", Kind: domain.ClaimDecision, Statement: "uses sha256", CreatedAt: now}); err != nil {
		t.Fatal(err)
	}
	if err := s.ResetJobItem(ctx, "i1"); err != nil {
		t.Fatal(err)
	}
	rows, _ := s.ListJobItems(ctx, "fix")
	if r := rows[0]; r.Status != domain.JobItemQueued || r.Rounds != 0 || r.Reason != "" || r.StartedAt != nil || r.Phase != domain.PhaseBuild {
		t.Errorf("reset item = %+v", r)
	}
	if cs, _ := s.ListJobClaims(ctx, "i1", 1, domain.PhasePlan); len(cs) != 0 {
		t.Errorf("claims survived the reset: %d", len(cs))
	}
}
