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
	"time"

	"codedistill/internal/domain"
)

func phaseAt(t *testing.T, s *Store, jobID, phase string, at time.Time) {
	t.Helper()
	p := &domain.JobPhase{ID: phase + "-" + jobID + at.Format("150405"), JobID: jobID, Phase: phase}
	if err := s.StartJobPhase(context.Background(), p, at); err != nil {
		t.Fatalf("start phase: %v", err)
	}
}

// A worker that moves on without closing the previous phase would leave one
// that appears to have run forever. Closing it here means no caller has to
// remember.
func TestStartingAPhaseClosesThePreviousOne(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	p := seedProject(t, s)
	seedJobIn(t, s, p.ID, "j1", domain.JobDecompose, "doc-1", domain.JobRunning)
	base := fixedTime(t)

	phaseAt(t, s, "j1", "reading", base)
	phaseAt(t, s, "j1", "sorting", base.Add(24*time.Minute))
	phaseAt(t, s, "j1", "reconciling", base.Add(29*time.Minute))

	got, err := s.ListJobPhases(ctx, "j1")
	if err != nil || len(got) != 3 {
		t.Fatalf("phases = %d, %v", len(got), err)
	}
	// Order is by ordinal, not time: two phases can share a start instant at
	// second resolution.
	for i, want := range []string{"reading", "sorting", "reconciling"} {
		if got[i].Phase != want || got[i].Ordinal != i {
			t.Fatalf("phase %d = %q (ordinal %d), want %q", i, got[i].Phase, got[i].Ordinal, want)
		}
	}
	// The finished run can now say where its time went, which jobs.phase alone
	// could never do.
	if d := got[0].Seconds(base.Add(29 * time.Minute)); d != 24*60 {
		t.Errorf("reading took %ds, want 1440", d)
	}
	if !got[2].Running() {
		t.Error("the last phase should still be open")
	}
}

// A failed run must show WHERE it failed, not only that it did.
func TestFinishStampsTheErrorOnTheRunningPhase(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	p := seedProject(t, s)
	seedJobIn(t, s, p.ID, "j1", domain.JobDecompose, "doc-1", domain.JobRunning)
	base := fixedTime(t)

	phaseAt(t, s, "j1", "reading", base)
	phaseAt(t, s, "j1", "sorting", base.Add(10*time.Minute))
	if err := s.FinishJobPhases(ctx, "j1", "model unreachable", base.Add(12*time.Minute)); err != nil {
		t.Fatal(err)
	}

	got, _ := s.ListJobPhases(ctx, "j1")
	if got[0].Error != "" {
		t.Errorf("the completed reading phase was blamed: %q", got[0].Error)
	}
	if got[1].Error != "model unreachable" {
		t.Errorf("sorting error = %q, want the failure recorded where it happened", got[1].Error)
	}
	for _, ph := range got {
		if ph.Running() {
			t.Errorf("%s left open after the job finished", ph.Phase)
		}
	}
}

// "12 minutes elapsed" means nothing until you know the usual run is 34.
func TestPhaseNormsGiveElapsedTimeAVerdict(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	p := seedProject(t, s)
	base := fixedTime(t)

	// Three successful runs: reading 20, 24, 40 minutes.
	for i, mins := range []int{20, 24, 40} {
		id := "ok" + string(rune('a'+i))
		seedJobIn(t, s, p.ID, id, domain.JobDecompose, "doc", domain.JobRunning)
		phaseAt(t, s, id, "reading", base)
		if err := s.FinishJobPhases(ctx, id, "", base.Add(time.Duration(mins)*time.Minute)); err != nil {
			t.Fatal(err)
		}
		if err := s.FinishJob(ctx, id, domain.JobSucceeded, "", base); err != nil {
			t.Fatal(err)
		}
	}
	// A run that failed after 90 seconds must NOT drag the median toward a
	// figure no healthy run will ever match.
	seedJobIn(t, s, p.ID, "bad", domain.JobDecompose, "doc", domain.JobRunning)
	phaseAt(t, s, "bad", "reading", base)
	if err := s.FinishJobPhases(ctx, "bad", "died", base.Add(90*time.Second)); err != nil {
		t.Fatal(err)
	}
	if err := s.FinishJob(ctx, "bad", domain.JobFailed, "died", base); err != nil {
		t.Fatal(err)
	}

	norms, err := s.PhaseNorms(ctx, p.ID, domain.JobDecompose)
	if err != nil || len(norms) != 1 {
		t.Fatalf("norms = %+v, %v", norms, err)
	}
	// Median of 20/24/40 is 24. Mean would be 28, and a failed run would drag
	// it further — which is why this is a median over successes only.
	if norms[0].MedianSec != 24*60 {
		t.Errorf("median = %ds, want 1440 (the middle of 20/24/40, failures excluded)", norms[0].MedianSec)
	}
	if norms[0].Runs != 3 {
		t.Errorf("runs = %d, want 3 — the failed run must not count", norms[0].Runs)
	}
}

// Interacting agents take turns, and knowing which one spent the time is the
// useful shape.
func TestPhasesRecordWorkerAndRound(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	p := seedProject(t, s)
	seedJobIn(t, s, p.ID, "j1", domain.JobDecompose, "doc-1", domain.JobRunning)
	base := fixedTime(t)

	for i, w := range []struct {
		worker string
		round  int
	}{
		{domain.WorkerSolutioner, 1}, {domain.WorkerChallenger, 1},
		{domain.WorkerSolutioner, 2}, {domain.WorkerChallenger, 2},
	} {
		ph := &domain.JobPhase{
			ID: "p" + string(rune('a'+i)), JobID: "j1", Phase: "debate",
			WorkerType: w.worker, Round: w.round,
		}
		if err := s.StartJobPhase(ctx, ph, base.Add(time.Duration(i)*time.Minute)); err != nil {
			t.Fatal(err)
		}
	}
	got, _ := s.ListJobPhases(ctx, "j1")
	if len(got) != 4 {
		t.Fatalf("got %d phases", len(got))
	}
	if got[3].WorkerType != domain.WorkerChallenger || got[3].Round != 2 {
		t.Errorf("last exchange = %s round %d", got[3].WorkerType, got[3].Round)
	}
	// Interaction count is just the highest round — real, unlike a confidence
	// figure, which would have nothing behind it.
	if got[3].Round != 2 {
		t.Errorf("round count lost")
	}
}
