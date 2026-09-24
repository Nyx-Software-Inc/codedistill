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
	"strings"
	"testing"

	"codedistill/internal/domain"
)

// Built-ins are ordinary rows. If they were not, user-defined workflows would
// be a second mechanism rather than the same one.
func TestBuiltinWorkflowsAreSeededAsData(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	got, err := s.ListWorkflows(ctx)
	if err != nil {
		t.Fatal(err)
	}
	byID := map[string]*domain.Workflow{}
	for _, w := range got {
		byID[w.ID] = w
	}
	d := byID["decompose"]
	if d == nil {
		t.Fatal("decompose workflow not seeded")
	}
	if !d.Builtin || !d.Resumable {
		t.Errorf("decompose: builtin=%v resumable=%v, want both true", d.Builtin, d.Resumable)
	}
	if len(d.Steps) != 1 || d.Steps[0].WorkerType != domain.WorkerDecomposer {
		t.Fatalf("steps = %+v", d.Steps)
	}
	// Measured: a reading pass carries the whole document.
	if d.Steps[0].NeedsContext < 16384 {
		t.Errorf("decomposer needs %d tokens; a reading pass carries the whole document",
			d.Steps[0].NeedsContext)
	}
	if byID["arch_draft"] == nil {
		t.Error("arch_draft not seeded")
	}
}

// A definition without an implementation is a row that fails only when someone
// tries to run it.
func TestBuiltinWorkflowCannotBeDeleted(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	err := s.DeleteWorkflow(ctx, "decompose")
	if err == nil || !strings.Contains(err.Error(), "built in") {
		t.Fatalf("err = %v; a built-in must refuse deletion", err)
	}
	if w, _ := s.GetWorkflow(ctx, "decompose"); w == nil {
		t.Fatal("decompose was deleted anyway")
	}
}

// Three levels, most specific first. The last — nothing — is what keeps an
// install with no configuration working at all.
func TestStepProviderResolutionCascades(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	now := fixedTime(t)
	global := seedProvider(t, s, "p-global", "local ollama", true)
	onStep := seedProvider(t, s, "p-step", "claude", false)

	// Nothing configured: the caller falls back to the app's model.
	got, err := s.ResolveStepProvider(ctx, "decompose", 0, "")
	if err != nil || got != nil {
		t.Fatalf("unconfigured step = %+v, %v; want nil, nil", got, err)
	}

	// A global worker binding applies.
	if err := s.SetWorkerModel(ctx, domain.WorkerDecomposer, "", global.ID, now); err != nil {
		t.Fatal(err)
	}
	got, _ = s.ResolveStepProvider(ctx, "decompose", 0, "")
	if got == nil || got.ID != global.ID {
		t.Fatalf("global binding did not apply: %+v", got)
	}

	// The step's own provider beats it.
	if err := s.SetStepProvider(ctx, "decompose", 0, onStep.ID, now); err != nil {
		t.Fatal(err)
	}
	got, _ = s.ResolveStepProvider(ctx, "decompose", 0, "")
	if got == nil || got.ID != onStep.ID {
		t.Fatalf("step provider lost to the global binding: %+v", got)
	}

	// Clearing falls back rather than leaving the step pointed at nothing,
	// which would fail at run time instead of at configuration time.
	if err := s.SetStepProvider(ctx, "decompose", 0, "", now); err != nil {
		t.Fatal(err)
	}
	got, _ = s.ResolveStepProvider(ctx, "decompose", 0, "")
	if got == nil || got.ID != global.ID {
		t.Fatalf("cleared step did not fall back: %+v", got)
	}
}

// A model too small for the step truncates silently. Catching it at
// configuration time is the whole point of recording what a step needs.
func TestStepReportsContextShortfall(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	now := fixedTime(t)

	small := &domain.ModelProvider{
		ID: "p-small", Name: "tiny", Protocol: "ollama", Endpoint: "http://x",
		Model: "m", ContextTokens: 4096, IsLocal: true, Enabled: true,
		CreatedAt: now, UpdatedAt: now,
	}
	if err := s.CreateModelProvider(ctx, small); err != nil {
		t.Fatal(err)
	}
	if err := s.SetStepProvider(ctx, "decompose", 0, small.ID, now); err != nil {
		t.Fatal(err)
	}

	w, _ := s.GetWorkflow(ctx, "decompose")
	step := w.Steps[0]
	if step.ContextTokens != 4096 {
		t.Fatalf("provider window not resolved onto the step: %+v", step)
	}
	if got := step.ContextShortfall(); got != 16384-4096 {
		t.Errorf("shortfall = %d, want %d", got, 16384-4096)
	}
}

// User-defined workflows are the direction, so creating one must work through
// the same tables as a built-in.
func TestUserDefinedWorkflowRoundTrips(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	now := fixedTime(t)

	w := &domain.Workflow{
		ID: "my-review", Name: "My review", Description: "two opinions",
		Resumable: false, Enabled: true, CreatedAt: now, UpdatedAt: now,
		Steps: []domain.WorkflowStep{
			{WorkerType: domain.WorkerSolutioner, Label: "proposes", NeedsContext: 8192},
			{WorkerType: domain.WorkerChallenger, Label: "attacks it", NeedsContext: 8192},
		},
	}
	if err := s.CreateWorkflow(ctx, w); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetWorkflow(ctx, "my-review")
	if err != nil || got == nil {
		t.Fatalf("not stored: %v", err)
	}
	if got.Builtin {
		t.Error("a user-defined workflow was marked built-in")
	}
	if len(got.Steps) != 2 || got.Steps[1].WorkerType != domain.WorkerChallenger {
		t.Fatalf("steps = %+v", got.Steps)
	}
	// And it IS deletable, unlike a built-in.
	if err := s.DeleteWorkflow(ctx, "my-review"); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.GetWorkflow(ctx, "my-review"); got != nil {
		t.Error("delete did not remove it")
	}
}
