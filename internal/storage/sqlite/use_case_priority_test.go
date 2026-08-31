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

// Migration 0062 adds use_case_items.priority. This pins the round trip because
// the column had to be threaded into a POSITIONAL column list, insert
// placeholder run, and scan argument list — get one of those out of step and
// every field after it silently shifts by one.
func TestUseCasePriority_RoundTrip(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	now := time.Now().UTC()
	if err := s.CreateProject(ctx, &domain.Project{ID: "p1", Name: "P", CreatedAt: now}); err != nil {
		t.Fatal(err)
	}

	uc := &domain.UseCaseItem{
		ID: "uc1", ProjectID: "p1", Subject: "export a project as a zip",
		Description: "desc", Role: "developer", Want: "export", Why: "backup",
		Status: "open", Priority: "high", TargetRelease: "v1.1",
		Origin: "manual", CreatedAt: now, UpdatedAt: now,
	}
	if err := s.CreateUseCaseItem(ctx, uc); err != nil {
		t.Fatal(err)
	}

	got, err := s.GetUseCaseItem(ctx, "uc1")
	if err != nil {
		t.Fatal(err)
	}
	if got.Priority != "high" {
		t.Errorf("priority = %q, want high", got.Priority)
	}
	// Neighbouring fields prove the positional lists stayed in step.
	for _, f := range []struct{ name, got, want string }{
		{"subject", got.Subject, "export a project as a zip"},
		{"role", got.Role, "developer"},
		{"want", got.Want, "export"},
		{"why", got.Why, "backup"},
		{"status", got.Status, "open"},
		{"target_release", got.TargetRelease, "v1.1"},
		{"origin", got.Origin, "manual"},
	} {
		if f.got != f.want {
			t.Errorf("%s = %q, want %q — a column-order shift", f.name, f.got, f.want)
		}
	}

	got.Priority = "low"
	if err := s.UpdateUseCaseItem(ctx, got); err != nil {
		t.Fatal(err)
	}
	again, _ := s.GetUseCaseItem(ctx, "uc1")
	if again.Priority != "low" {
		t.Errorf("after update: priority = %q, want low", again.Priority)
	}
}

// Empty means "unset" to callers; the column is NOT NULL with a CHECK, so the
// store normalises rather than letting the insert fail.
func TestUseCasePriority_EmptyDefaultsToNone(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	now := time.Now().UTC()
	if err := s.CreateProject(ctx, &domain.Project{ID: "p1", Name: "P", CreatedAt: now}); err != nil {
		t.Fatal(err)
	}
	if err := s.CreateUseCaseItem(ctx, &domain.UseCaseItem{
		ID: "uc1", ProjectID: "p1", Subject: "s", Status: "open",
		Origin: "manual", CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatalf("create with empty priority: %v", err)
	}
	got, _ := s.GetUseCaseItem(ctx, "uc1")
	if got.Priority != "none" {
		t.Errorf("priority = %q, want none", got.Priority)
	}
}

// ListUseCaseItemsByScratchpad hand-wrote its own aliased column list, so adding
// `priority` to the shared one broke it at RUNTIME — the SELECT returned 27
// columns while scanUseCase wanted 28, and the endpoint 500'd with
// "expected 27 destination arguments in Scan, not 28".
//
// The round-trip test above did not catch it because it exercises Get and
// list-by-project, which both use the shared constant. This covers the third
// read path, which is the one the UI actually calls for a scratchpad.
func TestListUseCaseItemsByScratchpad_ScansEveryColumn(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	now := time.Now().UTC()
	if err := s.CreateProject(ctx, &domain.Project{ID: "p1", Name: "P", CreatedAt: now}); err != nil {
		t.Fatal(err)
	}
	if err := s.CreateScratchpad(ctx, &domain.Scratchpad{
		ID: "sp1", ProjectID: "p1", Name: "main",
		ClassificationMode: "full", CreatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.CreateScratchpadItem(ctx, &domain.ScratchpadItem{
		ID: "si1", ScratchpadID: "sp1", ContentType: "text", Content: "c",
		ClassificationState: "classified", CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.CreateUseCaseItem(ctx, &domain.UseCaseItem{
		ID: "uc1", ProjectID: "p1", SourceItemID: "si1", Subject: "s",
		Status: "open", Priority: "high", Origin: "manual",
		CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}

	got, err := s.ListUseCaseItemsByScratchpad(ctx, "sp1")
	if err != nil {
		t.Fatalf("list by scratchpad: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d use cases, want 1", len(got))
	}
	if got[0].Priority != "high" {
		t.Errorf("priority = %q, want high", got[0].Priority)
	}
	if got[0].Subject != "s" || got[0].Status != "open" {
		t.Errorf("column shift: subject=%q status=%q", got[0].Subject, got[0].Status)
	}
}
