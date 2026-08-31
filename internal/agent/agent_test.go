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

package agent

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"codedistill/internal/domain"
	"codedistill/internal/storage"
	"codedistill/internal/storage/sqlite"
)

// fakeClassifier lets tests stub the classifier's output.
type fakeClassifier struct {
	category       string
	reasoning      string
	role           string
	want           string
	why            string
	suggestedFiles []string
	err            error
	calls          int
	lastInput      ClassifyInput // for assertions on what we received
}

func (f *fakeClassifier) Classify(_ context.Context, in ClassifyInput) (Classification, error) {
	f.lastInput = in
	f.calls++
	return Classification{
		Category:       f.category,
		Reasoning:      f.reasoning,
		Role:           f.role,
		Want:           f.want,
		Why:            f.why,
		SuggestedFiles: f.suggestedFiles,
	}, f.err
}

type fixture struct {
	store storage.Storage
	agent *Agent
	clf   *fakeClassifier
	nowT  time.Time
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	s, err := sqlite.Open(":memory:")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	if err := s.Migrate(context.Background()); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	clf := &fakeClassifier{}
	now := time.Date(2026, 4, 22, 12, 0, 0, 0, time.UTC)

	// deterministic ID generator
	counter := 0
	idGen := func() string {
		counter++
		return fmt.Sprintf("derived-%d", counter)
	}

	a := New(s, clf,
		WithClock(func() time.Time { return now }),
		WithIDGen(idGen),
	)
	return &fixture{store: s, agent: a, clf: clf, nowT: now}
}

func (f *fixture) seed(t *testing.T, scratchpadMode string, itemOverride string) *domain.ScratchpadItem {
	t.Helper()
	ctx := context.Background()
	if err := f.store.CreateProject(ctx, &domain.Project{ID: "p1", Name: "P", CreatedAt: f.nowT}); err != nil {
		t.Fatalf("seed project: %v", err)
	}
	if err := f.store.CreateScratchpad(ctx, &domain.Scratchpad{
		ID: "sp1", ProjectID: "p1", Name: "main",
		ClassificationMode: scratchpadMode, CreatedAt: f.nowT,
	}); err != nil {
		t.Fatalf("seed scratchpad: %v", err)
	}
	item := &domain.ScratchpadItem{
		ID: "si1", ScratchpadID: "sp1",
		ContentType: "text", Content: "clicking login throws 500",
		ClassificationState: "unprocessed",
		CreatedAt:           f.nowT, UpdatedAt: f.nowT,
		ClassificationOverride: itemOverride,
	}
	if err := f.store.CreateScratchpadItem(ctx, item); err != nil {
		t.Fatalf("seed item: %v", err)
	}
	return item
}

// --- Process: no override, Mode=full → pending-review (Inbox) ---

func TestProcessRoutesToInbox(t *testing.T) {
	fx := newFixture(t)
	fx.clf.category = "BUG"
	fx.clf.reasoning = "stack trace + keyword 'crash'"
	fx.seed(t, "full", "")

	if err := fx.agent.Process(context.Background(), "si1"); err != nil {
		t.Fatalf("process: %v", err)
	}

	got, _ := fx.store.GetScratchpadItem(context.Background(), "si1")
	if got.ClassificationState != "pending-review" {
		t.Errorf("state = %q, want pending-review", got.ClassificationState)
	}
	if got.ProposedCategory != "bug" {
		t.Errorf("proposed_category = %q, want bug", got.ProposedCategory)
	}
	if got.ClassificationReasoning == "" {
		t.Errorf("reasoning should be populated")
	}
	if got.DerivedItemID != "" {
		t.Errorf("no derived item should be created for pending-review; got %q", got.DerivedItemID)
	}
	if fx.clf.calls != 1 {
		t.Errorf("classifier called %d times, want 1", fx.clf.calls)
	}
}

// --- Override skip: model is never called, state = skipped ---

func TestOverrideSkipBypassesClassifier(t *testing.T) {
	fx := newFixture(t)
	fx.seed(t, "full", "skip")

	if err := fx.agent.Process(context.Background(), "si1"); err != nil {
		t.Fatalf("process: %v", err)
	}
	if fx.clf.calls != 0 {
		t.Errorf("classifier should not be called; was called %d times", fx.clf.calls)
	}
	got, _ := fx.store.GetScratchpadItem(context.Background(), "si1")
	if got.ClassificationState != "skipped" {
		t.Errorf("state = %q, want skipped", got.ClassificationState)
	}
	if got.SkippedReason != "override_skip" {
		t.Errorf("skipped_reason = %q, want override_skip", got.SkippedReason)
	}
}

// --- Override bug: model skipped, derived Bug_Item created, state = classified ---

func TestOverrideBugCreatesBugImmediately(t *testing.T) {
	fx := newFixture(t)
	fx.seed(t, "full", "bug")

	if err := fx.agent.Process(context.Background(), "si1"); err != nil {
		t.Fatalf("process: %v", err)
	}
	if fx.clf.calls != 0 {
		t.Errorf("classifier should not be called for forced category")
	}
	got, _ := fx.store.GetScratchpadItem(context.Background(), "si1")
	if got.ClassificationState != "classified" {
		t.Errorf("state = %q, want classified", got.ClassificationState)
	}
	if got.DerivedItemID == "" {
		t.Errorf("expected derived_item_id to be set")
	}
	bug, err := fx.store.GetBugItem(context.Background(), got.DerivedItemID)
	if err != nil {
		t.Fatalf("get bug: %v", err)
	}
	if bug.Subject == "" || bug.Origin != "agent-derived" || bug.Status != "open" {
		t.Errorf("unexpected bug: %+v", bug)
	}
}

// A classifier-derived item is pushed to the MCP-export hook (it bypasses the
// API's notifyExport, so without this it would never sync).
func TestDerivedItemFiresExportHook(t *testing.T) {
	fx := newFixture(t)
	fx.seed(t, "full", "bug")

	type call struct{ ownerType, ownerID, op string }
	var calls []call
	ag := New(fx.store, fx.clf,
		WithClock(func() time.Time { return fx.nowT }),
		WithIDGen(func() string { return "derived-x" }),
		WithExportNotify(func(_ context.Context, ownerType, ownerID, op string, payload any) {
			calls = append(calls, call{ownerType, ownerID, op})
			if payload == nil {
				t.Error("export payload should be the derived item, got nil")
			}
		}),
	)
	if err := ag.Process(context.Background(), "si1"); err != nil {
		t.Fatalf("process: %v", err)
	}
	if len(calls) != 1 {
		t.Fatalf("expected 1 export call, got %d: %+v", len(calls), calls)
	}
	if calls[0].ownerType != "bug_item" || calls[0].op != "create" {
		t.Errorf("export call = %+v, want {bug_item, <id>, create}", calls[0])
	}
}

// --- Classification_Mode=off: untouched unless override set ---

func TestModeOffSkipsProcessingWithoutOverride(t *testing.T) {
	fx := newFixture(t)
	fx.clf.category = "BUG"
	fx.seed(t, "off", "")

	if err := fx.agent.Process(context.Background(), "si1"); err != nil {
		t.Fatalf("process: %v", err)
	}
	if fx.clf.calls != 0 {
		t.Errorf("classifier should not be called when mode=off")
	}
	got, _ := fx.store.GetScratchpadItem(context.Background(), "si1")
	if got.ClassificationState != "unprocessed" {
		t.Errorf("state = %q, want unprocessed", got.ClassificationState)
	}
}

func TestModeOffHonorsOverride(t *testing.T) {
	fx := newFixture(t)
	fx.seed(t, "off", "kb")

	if err := fx.agent.Process(context.Background(), "si1"); err != nil {
		t.Fatalf("process: %v", err)
	}
	got, _ := fx.store.GetScratchpadItem(context.Background(), "si1")
	if got.ClassificationState != "classified" {
		t.Errorf("state = %q, want classified (override beats mode)", got.ClassificationState)
	}
	if _, err := fx.store.GetKnowledgeEntry(context.Background(), got.DerivedItemID); err != nil {
		t.Errorf("expected KB entry to exist: %v", err)
	}
}

// --- Classifier error: state = failed ---

func TestClassifierErrorMarksFailed(t *testing.T) {
	fx := newFixture(t)
	fx.clf.err = errors.New("ollama unreachable")
	fx.seed(t, "full", "")

	err := fx.agent.Process(context.Background(), "si1")
	if err == nil {
		t.Fatalf("expected error")
	}
	got, _ := fx.store.GetScratchpadItem(context.Background(), "si1")
	if got.ClassificationState != "failed" {
		t.Errorf("state = %q, want failed", got.ClassificationState)
	}
}

// --- Inbox actions: Accept, Reclassify, Reject ---

func TestAcceptPendingCreatesDerivedItem(t *testing.T) {
	fx := newFixture(t)
	fx.clf.category = "BUG"
	fx.clf.reasoning = "stack trace"
	fx.seed(t, "full", "")
	ctx := context.Background()

	if err := fx.agent.Process(ctx, "si1"); err != nil {
		t.Fatalf("process: %v", err)
	}
	if err := fx.agent.AcceptPending(ctx, "si1", ""); err != nil {
		t.Fatalf("accept: %v", err)
	}

	got, _ := fx.store.GetScratchpadItem(ctx, "si1")
	if got.ClassificationState != "classified" {
		t.Errorf("state = %q, want classified", got.ClassificationState)
	}
	if got.DerivedItemID == "" {
		t.Fatal("derived_item_id empty after Accept")
	}
	if _, err := fx.store.GetBugItem(ctx, got.DerivedItemID); err != nil {
		t.Errorf("expected bug to exist: %v", err)
	}
}

func TestAcceptPendingReclassifyChangesCategory(t *testing.T) {
	fx := newFixture(t)
	fx.clf.category = "BUG"
	fx.seed(t, "full", "")
	ctx := context.Background()

	fx.agent.Process(ctx, "si1")
	if err := fx.agent.AcceptPending(ctx, "si1", "todo"); err != nil {
		t.Fatalf("reclassify: %v", err)
	}

	got, _ := fx.store.GetScratchpadItem(ctx, "si1")
	if got.ClassificationOverride != "todo" {
		t.Errorf("override = %q, want todo (reclassify should set override)", got.ClassificationOverride)
	}
	if _, err := fx.store.GetTodoItem(ctx, got.DerivedItemID); err != nil {
		t.Errorf("expected todo to exist: %v", err)
	}
}

func TestRejectPendingMarksSkipped(t *testing.T) {
	fx := newFixture(t)
	fx.clf.category = "KB"
	fx.seed(t, "full", "")
	ctx := context.Background()

	fx.agent.Process(ctx, "si1")
	if err := fx.agent.RejectPending(ctx, "si1"); err != nil {
		t.Fatalf("reject: %v", err)
	}

	got, _ := fx.store.GetScratchpadItem(ctx, "si1")
	if got.ClassificationState != "skipped" {
		t.Errorf("state = %q, want skipped", got.ClassificationState)
	}
	if got.SkippedReason != "user_rejected_from_inbox" {
		t.Errorf("skipped_reason = %q, want user_rejected_from_inbox", got.SkippedReason)
	}
}

// --- Queue: Start/Enqueue/Stop processes items end-to-end ---

func TestAgentQueueProcessesEnqueuedItem(t *testing.T) {
	fx := newFixture(t)
	fx.clf.category = "TODO"
	fx.seed(t, "full", "")

	fx.agent.Start(context.Background())
	fx.agent.Enqueue("si1")
	fx.agent.Stop() // drains the queue

	got, _ := fx.store.GetScratchpadItem(context.Background(), "si1")
	if got.ClassificationState != "pending-review" {
		t.Errorf("state = %q, want pending-review", got.ClassificationState)
	}
}

// --- OllamaClassifier: JSON parsing happy path + malformed handling ---

func TestOllamaClassifierParses(t *testing.T) {
	c := &OllamaClassifier{
		Generate: func(_ context.Context, _ string) (string, error) {
			return `{"category":"BUG","reasoning":"stack trace"}`, nil
		},
	}
	got, err := c.Classify(context.Background(), ClassifyInput{Content: "content"})
	if err != nil {
		t.Fatalf("classify: %v", err)
	}
	if got.Category != "BUG" || got.Reasoning != "stack trace" {
		t.Errorf("got (%q, %q), want (BUG, stack trace)", got.Category, got.Reasoning)
	}
}

func TestOllamaClassifierRejectsUnknownCategory(t *testing.T) {
	c := &OllamaClassifier{
		Generate: func(_ context.Context, _ string) (string, error) {
			return `{"category":"OTHER","reasoning":"x"}`, nil
		},
	}
	_, err := c.Classify(context.Background(), ClassifyInput{Content: "content"})
	if err == nil {
		t.Error("expected error on unknown category")
	}
}

func TestOllamaClassifierRejectsMalformedJSON(t *testing.T) {
	c := &OllamaClassifier{
		Generate: func(_ context.Context, _ string) (string, error) {
			return `not json at all`, nil
		},
	}
	_, err := c.Classify(context.Background(), ClassifyInput{Content: "content"})
	if err == nil {
		t.Error("expected error on malformed json")
	}
}

// --- OllamaClassifier: USE_CASE role/want/why round-trip ---

func TestOllamaClassifierExtractsUseCaseFields(t *testing.T) {
	c := &OllamaClassifier{
		Generate: func(_ context.Context, _ string) (string, error) {
			return `{"category":"USE_CASE","reasoning":"as-a structure","role":"developer","want":"export a project as a zip","why":"I can move it between machines"}`, nil
		},
	}
	got, err := c.Classify(context.Background(), ClassifyInput{Content: "content"})
	if err != nil {
		t.Fatalf("classify: %v", err)
	}
	if got.Role != "developer" || got.Want != "export a project as a zip" || got.Why != "I can move it between machines" {
		t.Errorf("role/want/why not parsed: %+v", got)
	}
}

// Hallucination resistance: when the model returns role/want/why under a
// non-USE_CASE category, the classifier must drop them. Protects derive
// from corrupt USE_CASE-shaped data leaking into TODO/BUG/KB rows if the
// model ever ignores the prompt's "only when category=USE_CASE" rule.
func TestOllamaClassifierDropsRoleWantWhyForNonUseCase(t *testing.T) {
	c := &OllamaClassifier{
		Generate: func(_ context.Context, _ string) (string, error) {
			return `{"category":"BUG","reasoning":"stack trace","role":"dev","want":"fix it","why":"users blocked"}`, nil
		},
	}
	got, err := c.Classify(context.Background(), ClassifyInput{Content: "content"})
	if err != nil {
		t.Fatalf("classify: %v", err)
	}
	if got.Role != "" || got.Want != "" || got.Why != "" {
		t.Errorf("expected empty role/want/why under non-USE_CASE category, got %+v", got)
	}
}

// --- USE_CASE classification creates a UseCaseItem on accept ---

func TestAcceptPendingUseCaseCreatesUseCase(t *testing.T) {
	fx := newFixture(t)
	fx.clf.category = "USE_CASE"
	fx.clf.reasoning = "users can…"
	fx.seed(t, "full", "")
	ctx := context.Background()

	if err := fx.agent.Process(ctx, "si1"); err != nil {
		t.Fatalf("process: %v", err)
	}
	if err := fx.agent.AcceptPending(ctx, "si1", ""); err != nil {
		t.Fatalf("accept: %v", err)
	}

	got, _ := fx.store.GetScratchpadItem(ctx, "si1")
	if got.ClassificationState != "classified" {
		t.Errorf("state = %q, want classified", got.ClassificationState)
	}
	if got.ProposedCategory != "use_case" {
		t.Errorf("proposed_category = %q, want use_case", got.ProposedCategory)
	}
	if got.DerivedItemID == "" {
		t.Fatal("derived_item_id empty after Accept")
	}
	uc, err := fx.store.GetUseCaseItem(ctx, got.DerivedItemID)
	if err != nil {
		t.Fatalf("get use_case: %v", err)
	}
	if uc.Subject == "" || uc.Origin != "agent-derived" || uc.Status != "open" {
		t.Errorf("unexpected use_case: %+v", uc)
	}
}

// --- Override use_case: model skipped, derived UseCaseItem created ---

func TestOverrideUseCaseCreatesUseCaseImmediately(t *testing.T) {
	fx := newFixture(t)
	fx.seed(t, "full", "use_case")

	if err := fx.agent.Process(context.Background(), "si1"); err != nil {
		t.Fatalf("process: %v", err)
	}
	if fx.clf.calls != 0 {
		t.Errorf("classifier should not be called for forced category")
	}
	got, _ := fx.store.GetScratchpadItem(context.Background(), "si1")
	if got.ClassificationState != "classified" {
		t.Errorf("state = %q, want classified", got.ClassificationState)
	}
	if got.DerivedItemID == "" {
		t.Fatal("expected derived_item_id to be set")
	}
	uc, err := fx.store.GetUseCaseItem(context.Background(), got.DerivedItemID)
	if err != nil {
		t.Fatalf("get use_case: %v", err)
	}
	if uc.Subject == "" || uc.Origin != "agent-derived" || uc.Status != "open" {
		t.Errorf("unexpected use_case: %+v", uc)
	}
}

// --- Lenient category parser strips leading non-alpha (e.g. ".TODO") ---

func TestOllamaClassifierStripsLeadingDot(t *testing.T) {
	c := &OllamaClassifier{
		Generate: func(_ context.Context, _ string) (string, error) {
			return `{"category":".TODO","reasoning":"x"}`, nil
		},
	}
	got, err := c.Classify(context.Background(), ClassifyInput{Content: "content"})
	if err != nil {
		t.Fatalf("classify: %v", err)
	}
	if got.Category != "TODO" {
		t.Errorf("got %q, want TODO (leading dot should be stripped)", got.Category)
	}
}

// --- Use-case extraction round-trips through pending-review and Accept ---

func TestUseCaseExtractionSurvivesInboxAccept(t *testing.T) {
	fx := newFixture(t)
	fx.clf.category = "USE_CASE"
	fx.clf.reasoning = "as-a-I-want-so-that"
	fx.clf.role = "developer"
	fx.clf.want = "export a project as a zip"
	fx.clf.why = "I can move it between machines"
	fx.seed(t, "full", "")
	ctx := context.Background()

	if err := fx.agent.Process(ctx, "si1"); err != nil {
		t.Fatalf("process: %v", err)
	}
	// Stashed on the scratchpad item while pending-review.
	mid, _ := fx.store.GetScratchpadItem(ctx, "si1")
	if mid.ProposedRole != "developer" || mid.ProposedWant != "export a project as a zip" || mid.ProposedWhy != "I can move it between machines" {
		t.Errorf("proposed_role/want/why not stashed: %+v", mid)
	}

	if err := fx.agent.AcceptPending(ctx, "si1", ""); err != nil {
		t.Fatalf("accept: %v", err)
	}
	got, _ := fx.store.GetScratchpadItem(ctx, "si1")
	uc, err := fx.store.GetUseCaseItem(ctx, got.DerivedItemID)
	if err != nil {
		t.Fatalf("get use_case: %v", err)
	}
	if uc.Role != "developer" || uc.Want != "export a project as a zip" || uc.Why != "I can move it between machines" {
		t.Errorf("role/want/why not propagated to use_case: %+v", uc)
	}
	// Subject derives from `want` when populated — capability label, not the
	// first line of the source paste.
	if uc.Subject != "export a project as a zip" {
		t.Errorf("subject = %q, want %q (from `want`)", uc.Subject, "export a project as a zip")
	}
	// Original paste is preserved in description.
	if uc.Description != mid.Content {
		t.Errorf("description should preserve original paste; got %q", uc.Description)
	}
}

// --- Per-project numbering: derived items get sequential per-project numbers ---

func TestDerivedItemsAssignSequentialNumbers(t *testing.T) {
	fx := newFixture(t)
	fx.seed(t, "full", "use_case")
	ctx := context.Background()
	if err := fx.agent.Process(ctx, "si1"); err != nil {
		t.Fatalf("process: %v", err)
	}
	got, _ := fx.store.GetScratchpadItem(ctx, "si1")
	uc, err := fx.store.GetUseCaseItem(ctx, got.DerivedItemID)
	if err != nil {
		t.Fatalf("get use_case: %v", err)
	}
	if uc.Number != 1 {
		t.Errorf("first use_case in fresh project should be UC-1, got %d", uc.Number)
	}
}

// --- Auto-anchor: file tree → suggested files become anchors ---

func TestAutoAnchorCreatesAnchorsForSuggestedFiles(t *testing.T) {
	fx := newFixture(t)
	// Re-build the agent with a file-tree func returning a known small tree.
	tree := []string{
		"internal/api/server.go",
		"internal/api/items.go",
		"web/src/App.svelte",
	}
	idCounter := 0
	fx.agent = New(fx.store, fx.clf,
		WithClock(func() time.Time { return fx.nowT }),
		WithIDGen(func() string {
			idCounter++
			return fmt.Sprintf("a-%d", idCounter)
		}),
		WithProjectFileTree(func(_ context.Context, _ string) ([]string, error) {
			return tree, nil
		}),
		// Auto-anchoring is paid and now fails closed (CE-review item 28).
		WithCodeAnchors(true),
	)
	fx.clf.category = "TODO"
	fx.clf.suggestedFiles = []string{"internal/api/server.go", "web/src/App.svelte"}
	fx.seed(t, "full", "")

	if err := fx.agent.Process(context.Background(), "si1"); err != nil {
		t.Fatalf("process: %v", err)
	}

	// The classifier should have received the file tree.
	if len(fx.clf.lastInput.FileTree) != 3 {
		t.Errorf("classifier saw %d tree entries, want 3", len(fx.clf.lastInput.FileTree))
	}

	// Two anchors should now exist on the source item.
	anchors, err := fx.store.ListCodeAnchors(context.Background(), "scratchpad_item", "si1")
	if err != nil {
		t.Fatalf("list anchors: %v", err)
	}
	if len(anchors) != 2 {
		t.Fatalf("got %d anchors, want 2", len(anchors))
	}
	for _, a := range anchors {
		if a.Provenance != "agent-suggested" {
			t.Errorf("anchor %s provenance = %q, want agent-suggested", a.Path, a.Provenance)
		}
		if a.Kind != "file" {
			t.Errorf("anchor %s kind = %q, want file", a.Path, a.Kind)
		}
		if a.LineStart != 0 || a.LineEnd != 0 {
			t.Errorf("anchor %s should have no line range; got %d-%d", a.Path, a.LineStart, a.LineEnd)
		}
	}
}

// --- Auto-anchor: hallucinated paths get filtered ---

func TestAutoAnchorDropsHallucinatedPaths(t *testing.T) {
	fx := newFixture(t)
	// Counter so each anchor gets a unique id; UNIQUE PK on code_anchors.
	idCounter := 0
	fx.agent = New(fx.store, fx.clf,
		WithClock(func() time.Time { return fx.nowT }),
		WithIDGen(func() string {
			idCounter++
			return fmt.Sprintf("a-%d", idCounter)
		}),
		WithProjectFileTree(func(_ context.Context, _ string) ([]string, error) {
			return []string{"internal/api/server.go"}, nil
		}),
		// Auto-anchoring is paid and now fails closed (CE-review item 28).
		WithCodeAnchors(true),
	)
	fx.clf.category = "BUG"
	// Model returns one valid path + one hallucination.
	fx.clf.suggestedFiles = []string{"internal/api/server.go", "internal/totally/made-up.go"}
	fx.seed(t, "full", "")

	if err := fx.agent.Process(context.Background(), "si1"); err != nil {
		t.Fatalf("process: %v", err)
	}
	anchors, _ := fx.store.ListCodeAnchors(context.Background(), "scratchpad_item", "si1")
	if len(anchors) != 1 {
		t.Fatalf("got %d anchors, want 1 (hallucinated path should drop)", len(anchors))
	}
	if anchors[0].Path != "internal/api/server.go" {
		t.Errorf("kept wrong path: %s", anchors[0].Path)
	}
}

// --- Auto-anchor: no file tree → no anchors, classification still works ---

func TestAutoAnchorNoFileTreeIsNoOp(t *testing.T) {
	fx := newFixture(t)
	// Licensed, but no fileTree wired — degrades cleanly. WithCodeAnchors(true)
	// matters: without it this test would pass because the paid gate is off
	// rather than because the tree is empty, quietly ceasing to test its own
	// claim (CE-review item 28).
	fx.agent = New(fx.store, fx.clf,
		WithClock(func() time.Time { return fx.nowT }),
		WithCodeAnchors(true),
	)
	fx.clf.category = "TODO"
	fx.clf.suggestedFiles = []string{"foo.go"} // model would return paths but we sent no tree
	fx.seed(t, "full", "")

	if err := fx.agent.Process(context.Background(), "si1"); err != nil {
		t.Fatalf("process: %v", err)
	}
	if len(fx.clf.lastInput.FileTree) != 0 {
		t.Errorf("classifier should have received empty FileTree; got %v", fx.clf.lastInput.FileTree)
	}
	anchors, _ := fx.store.ListCodeAnchors(context.Background(), "scratchpad_item", "si1")
	if len(anchors) != 0 {
		t.Errorf("got %d anchors, want 0 (no tree means no auto-anchor)", len(anchors))
	}
	got, _ := fx.store.GetScratchpadItem(context.Background(), "si1")
	if got.ClassificationState != "pending-review" {
		t.Errorf("classification still happens; state = %q want pending-review", got.ClassificationState)
	}
}

// --- Auto-anchor: KB classifications never get anchors (defended in Process) ---

func TestAutoAnchorSkipsKB(t *testing.T) {
	fx := newFixture(t)
	idCounter := 0
	fx.agent = New(fx.store, fx.clf,
		WithClock(func() time.Time { return fx.nowT }),
		WithIDGen(func() string {
			idCounter++
			return fmt.Sprintf("a-%d", idCounter)
		}),
		WithProjectFileTree(func(_ context.Context, _ string) ([]string, error) {
			return []string{"a.go", "b.go"}, nil
		}),
	)
	// Classifier returns suggestions even though category is KB; Process must drop them.
	fx.clf.category = "KB"
	fx.clf.suggestedFiles = []string{"a.go", "b.go"}
	fx.seed(t, "full", "")

	if err := fx.agent.Process(context.Background(), "si1"); err != nil {
		t.Fatalf("process: %v", err)
	}
	anchors, _ := fx.store.ListCodeAnchors(context.Background(), "scratchpad_item", "si1")
	if len(anchors) != 0 {
		t.Errorf("KB classification should not auto-anchor; got %d anchors", len(anchors))
	}
}

// --- Embedding: agent fires Embed alongside Classify ---

type fakeEmbedder struct {
	calls  int
	lastIn string
	vec    []float32
	err    error
}

func (f *fakeEmbedder) Embed(_ context.Context, in string) ([]float32, error) {
	f.calls++
	f.lastIn = in
	if f.err != nil {
		return nil, f.err
	}
	return f.vec, nil
}

func TestEmbedderRunsAfterClassify(t *testing.T) {
	fx := newFixture(t)
	emb := &fakeEmbedder{vec: []float32{0.1, 0.2, 0.3, 0.4}}
	fx.agent = New(fx.store, fx.clf,
		WithClock(func() time.Time { return fx.nowT }),
		WithIDGen(func() string { return "deriv" }),
		WithEmbedder(emb),
	)
	fx.clf.category = "BUG"
	fx.seed(t, "full", "")

	if err := fx.agent.Process(context.Background(), "si1"); err != nil {
		t.Fatalf("process: %v", err)
	}

	// Classifier ran once, embedder ran once with the source content.
	if fx.clf.calls != 1 {
		t.Errorf("classifier calls = %d, want 1", fx.clf.calls)
	}
	if emb.calls != 1 {
		t.Errorf("embedder calls = %d, want 1", emb.calls)
	}
	if emb.lastIn != "clicking login throws 500" {
		t.Errorf("embedder saw %q, want the source content", emb.lastIn)
	}
	// ListUnembedded should now be empty for the scratchpad — meaning the
	// row carries an embedding + embedded_at.
	pending, err := fx.store.ListUnembedded(context.Background(), "scratchpad_items", 10)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(pending) != 0 {
		t.Errorf("scratchpad still unembedded: %+v", pending)
	}
}

func TestEmbedderFailureIsNonFatal(t *testing.T) {
	fx := newFixture(t)
	emb := &fakeEmbedder{err: errors.New("ollama down")}
	fx.agent = New(fx.store, fx.clf,
		WithClock(func() time.Time { return fx.nowT }),
		WithIDGen(func() string { return "deriv" }),
		WithEmbedder(emb),
	)
	fx.clf.category = "TODO"
	fx.seed(t, "full", "")

	// Process must complete cleanly even when the embedder errors.
	if err := fx.agent.Process(context.Background(), "si1"); err != nil {
		t.Fatalf("process should not surface embed errors: %v", err)
	}
	got, _ := fx.store.GetScratchpadItem(context.Background(), "si1")
	if got.ClassificationState != "pending-review" {
		t.Errorf("classification still ran; state = %q", got.ClassificationState)
	}
	// Item remains unembedded — backfill will pick it up next time.
	pending, _ := fx.store.ListUnembedded(context.Background(), "scratchpad_items", 10)
	if len(pending) != 1 {
		t.Errorf("expected 1 unembedded row, got %d", len(pending))
	}
}

func TestCommitClassifiedEmbedsDerivedItem(t *testing.T) {
	fx := newFixture(t)
	emb := &fakeEmbedder{vec: []float32{1, 0, 0, 0}}
	idCounter := 0
	fx.agent = New(fx.store, fx.clf,
		WithClock(func() time.Time { return fx.nowT }),
		WithIDGen(func() string {
			idCounter++
			return fmt.Sprintf("d-%d", idCounter)
		}),
		WithEmbedder(emb),
	)
	// Use the override path so commitClassified runs synchronously
	// (creates the derived item and embeds it immediately).
	fx.seed(t, "full", "bug")

	if err := fx.agent.Process(context.Background(), "si1"); err != nil {
		t.Fatalf("process: %v", err)
	}

	// embedAndStore was called twice: once for source, once for derived bug.
	if emb.calls != 2 {
		t.Errorf("embedder calls = %d, want 2 (source + derived)", emb.calls)
	}
	// The derived bug should also be embedded.
	pending, _ := fx.store.ListUnembedded(context.Background(), "bug_items", 10)
	if len(pending) != 0 {
		t.Errorf("derived bug still unembedded: %+v", pending)
	}
}

// TestReclassifyDeletesPriorDerived: re-classifying an item (todo → bug)
// must delete the superseded todo, not leave it orphaned (which
// double-counted in the header — CodeDestill_imports bug #1).
func TestReclassifyDeletesPriorDerived(t *testing.T) {
	fx := newFixture(t)
	ctx := context.Background()

	// First classification via override → a todo.
	fx.seed(t, "full", "todo")
	if err := fx.agent.Process(ctx, "si1"); err != nil {
		t.Fatalf("process todo: %v", err)
	}
	todos, _ := fx.store.ListTodoItems(ctx, "p1")
	if len(todos) != 1 {
		t.Fatalf("after first classify: %d todos, want 1", len(todos))
	}
	si, _ := fx.store.GetScratchpadItem(ctx, "si1")
	if si.DerivedItemID != todos[0].ID || si.ProposedCategory != "todo" {
		t.Fatalf("derived link wrong: derived=%q cat=%q", si.DerivedItemID, si.ProposedCategory)
	}

	// Reclassify to bug.
	si.ClassificationOverride = "bug"
	if err := fx.store.UpdateScratchpadItem(ctx, si); err != nil {
		t.Fatal(err)
	}
	if err := fx.agent.Process(ctx, "si1"); err != nil {
		t.Fatalf("process bug: %v", err)
	}

	todos, _ = fx.store.ListTodoItems(ctx, "p1")
	bugs, _ := fx.store.ListBugItems(ctx, "p1")
	if len(todos) != 0 {
		t.Errorf("prior todo not deleted on reclassify: %d remain", len(todos))
	}
	if len(bugs) != 1 {
		t.Errorf("reclassify: %d bugs, want 1", len(bugs))
	}
	si, _ = fx.store.GetScratchpadItem(ctx, "si1")
	if si.DerivedItemID != bugs[0].ID || si.ProposedCategory != "bug" {
		t.Errorf("derived link not updated: derived=%q cat=%q", si.DerivedItemID, si.ProposedCategory)
	}
}

// Slice 4: reclassifying re-derives the work item, and its activity log (notes)
// must carry over to the new type record instead of being orphaned, plus a
// "reclassified" entry lands on the new item.
func TestReclassifyCarriesOverLog(t *testing.T) {
	ctx := context.Background()
	fx := newFixture(t)
	fx.seed(t, "full", "use_case")

	// First derivation → use_case (deterministic id derived-1).
	if err := fx.agent.Process(ctx, "si1"); err != nil {
		t.Fatalf("derive: %v", err)
	}
	si, _ := fx.store.GetScratchpadItem(ctx, "si1")
	ucID := si.DerivedItemID
	if ucID == "" {
		t.Fatal("no derived use_case")
	}

	// A note on the use_case's activity log.
	if err := fx.store.RecordItemEvent(ctx, &domain.ItemEvent{
		ID: "note-1", OwnerType: "use_case_item", OwnerID: ucID,
		Kind: "note", Body: "KEEPME", Summary: "KEEPME", Source: "ui", CreatedAt: fx.nowT,
	}); err != nil {
		t.Fatalf("note: %v", err)
	}

	// Reclassify → bug (mirrors the HTTP path: change override, re-process).
	si.ClassificationOverride = "bug"
	if err := fx.store.UpdateScratchpadItem(ctx, si); err != nil {
		t.Fatalf("set override: %v", err)
	}
	if err := fx.agent.Process(ctx, "si1"); err != nil {
		t.Fatalf("reclassify: %v", err)
	}

	si2, _ := fx.store.GetScratchpadItem(ctx, "si1")
	bugID := si2.DerivedItemID
	if bugID == "" || bugID == ucID {
		t.Fatalf("expected a new bug id, got %q (uc %q)", bugID, ucID)
	}

	// Note + a reclassified event now live on the new bug.
	be, _ := fx.store.ListItemEvents(ctx, "bug_item", bugID)
	var hasNote, hasReclassify bool
	for _, e := range be {
		if e.Kind == "note" && e.Body == "KEEPME" {
			hasNote = true
		}
		if e.Kind == "reclassified" {
			hasReclassify = true
		}
	}
	if !hasNote {
		t.Errorf("note did not carry over to the new bug (%d events on it)", len(be))
	}
	if !hasReclassify {
		t.Errorf("no reclassified event on the new bug")
	}

	// And nothing lingers on the retired use_case.
	ue, _ := fx.store.ListItemEvents(ctx, "use_case_item", ucID)
	if len(ue) != 0 {
		t.Errorf("retired use_case still has %d events (should be re-homed)", len(ue))
	}
}
