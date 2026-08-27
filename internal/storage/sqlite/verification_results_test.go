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

func TestVerificationResultsCRUD(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	now := time.Now().UTC()

	// Empty owner → empty list (never nil).
	list, err := s.ListVerificationResults(ctx, "todo_item", "t1")
	if err != nil {
		t.Fatalf("list empty: %v", err)
	}
	if len(list) != 0 {
		t.Fatalf("expected 0 results, got %d", len(list))
	}

	// Create a "running" row (async run just started) — exit_code nil,
	// commit known.
	v := &domain.VerificationResult{
		ID: "vr1", OwnerType: "todo_item", OwnerID: "t1",
		Layer: domain.VerifyLayerDeterministic, Kind: domain.VerifyKindTest,
		CheckName: "go test ./...", Verdict: domain.VerifyVerdictRunning,
		CommitSHA: "abc1234", ProducedBy: domain.VerifyProducedByBox,
		CreatedAt: now, UpdatedAt: now,
	}
	if err := s.CreateVerificationResult(ctx, v); err != nil {
		t.Fatalf("create: %v", err)
	}

	got, err := s.GetVerificationResult(ctx, "vr1")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Verdict != domain.VerifyVerdictRunning || got.ExitCode != nil || got.CommitSHA != "abc1234" {
		t.Errorf("unexpected running row: %+v (exit=%v)", got, got.ExitCode)
	}

	// Resolve the run in place to a terminal verdict; exit code 0 must
	// round-trip as 0 (a meaningful value), not NULL.
	zero := 0
	got.Verdict = domain.VerifyVerdictPass
	got.ExitCode = &zero
	got.Summary = "12 passed, 0 failed"
	got.Output = "ok  codedistill/...  2.4s"
	got.DurationMS = 2400
	got.UpdatedAt = now.Add(time.Second)
	if err := s.UpdateVerificationResult(ctx, got); err != nil {
		t.Fatalf("update: %v", err)
	}
	got, _ = s.GetVerificationResult(ctx, "vr1")
	if got.Verdict != domain.VerifyVerdictPass || got.ExitCode == nil || *got.ExitCode != 0 {
		t.Errorf("terminal verdict not persisted: %+v (exit=%v)", got, got.ExitCode)
	}
	if got.DurationMS != 2400 || got.Summary != "12 passed, 0 failed" {
		t.Errorf("fields not persisted: %+v", got)
	}

	// A second, newer row sorts first (newest-first ordering).
	v2 := &domain.VerificationResult{
		ID: "vr2", OwnerType: "todo_item", OwnerID: "t1",
		Layer: domain.VerifyLayerDeterministic, Kind: domain.VerifyKindTest,
		CheckName: "go test ./...", Verdict: domain.VerifyVerdictFail,
		ProducedBy: domain.VerifyProducedByBox,
		CreatedAt:  now.Add(time.Minute), UpdatedAt: now.Add(time.Minute),
	}
	if err := s.CreateVerificationResult(ctx, v2); err != nil {
		t.Fatalf("create v2: %v", err)
	}
	list, _ = s.ListVerificationResults(ctx, "todo_item", "t1")
	if len(list) != 2 || list[0].ID != "vr2" {
		t.Fatalf("expected newest-first [vr2, vr1], got %d: %+v", len(list), list)
	}

	// Not-found semantics.
	if _, err := s.GetVerificationResult(ctx, "nope"); !errors.Is(err, storage.ErrNotFound) {
		t.Errorf("get missing: err = %v, want ErrNotFound", err)
	}
	missing := &domain.VerificationResult{ID: "nope", UpdatedAt: now}
	if err := s.UpdateVerificationResult(ctx, missing); !errors.Is(err, storage.ErrNotFound) {
		t.Errorf("update missing: err = %v, want ErrNotFound", err)
	}
}

func TestFailStaleRunningVerifications(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	now := time.Now().UTC()
	mk := func(id, verdict string) {
		if err := s.CreateVerificationResult(ctx, &domain.VerificationResult{
			ID: id, OwnerType: "todo_item", OwnerID: "t1", Layer: "deterministic",
			Kind: "test", CheckName: "go test", Verdict: verdict, Summary: verdict,
			ProducedBy: "box", CreatedAt: now, UpdatedAt: now,
		}); err != nil {
			t.Fatal(err)
		}
	}
	mk("r1", "running")
	mk("r2", "running")
	mk("r3", "pass")

	n, err := s.FailStaleRunningVerifications(ctx, now)
	if err != nil || n != 2 {
		t.Fatalf("failed %d (err %v), want 2", n, err)
	}
	got, _ := s.ListVerificationResults(ctx, "todo_item", "t1")
	for _, r := range got {
		if r.Verdict == "running" {
			t.Errorf("result %s still running after sweep", r.ID)
		}
		if r.ID == "r3" && r.Verdict != "pass" {
			t.Errorf("r3 (pass) should be untouched, got %s", r.Verdict)
		}
	}
}
