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
	"testing"

	"codedistill/internal/domain"
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
