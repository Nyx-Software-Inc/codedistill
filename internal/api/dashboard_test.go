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

package api

import (
	"context"
	"testing"
	"time"

	"codedistill/internal/domain"
)

// TestDashboardThroughputEndpoint covers the happy path with a mix of
// open/closed todos+bugs+use_cases, then checks the boundary cases
// (unknown project, bad days param).
func TestDashboardThroughputEndpoint(t *testing.T) {
	srv, _, store := setupWithStore(t)
	ctx := context.Background()

	// Seed project + scratchpad + source item so the create*Item
	// FK chain is satisfied.
	now := time.Now()
	yesterday := now.AddDate(0, 0, -1)
	twoDaysAgo := now.AddDate(0, 0, -2)

	if err := store.CreateProject(ctx, &domain.Project{ID: "p1", Name: "P", CreatedAt: now}); err != nil {
		t.Fatalf("project: %v", err)
	}
	if err := store.CreateScratchpad(ctx, &domain.Scratchpad{
		ID: "sp1", ProjectID: "p1", Name: "main",
		ClassificationMode: "full", CreatedAt: now,
	}); err != nil {
		t.Fatalf("scratchpad: %v", err)
	}
	if err := store.CreateScratchpadItem(ctx, &domain.ScratchpadItem{
		ID: "si1", ScratchpadID: "sp1", ContentType: "text", Content: "seed",
		ClassificationState: "classified", CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatalf("item: %v", err)
	}

	// One open todo created yesterday, one completed today.
	completedNow := now
	if err := store.CreateTodoItem(ctx, &domain.TodoItem{
		ID: "t1", ProjectID: "p1", SourceItemID: "si1",
		Subject: "open one", Priority: "none", Status: "incomplete",
		Origin: "agent-derived", CreatedAt: yesterday,
	}); err != nil {
		t.Fatalf("todo open: %v", err)
	}
	if err := store.CreateTodoItem(ctx, &domain.TodoItem{
		ID: "t2", ProjectID: "p1", SourceItemID: "si1",
		Subject: "closed one", Priority: "none", Status: "complete",
		Origin: "agent-derived", CreatedAt: twoDaysAgo, CompletedAt: &completedNow,
	}); err != nil {
		t.Fatalf("todo closed: %v", err)
	}

	type DayCount struct {
		Date  string `json:"date"`
		Count int    `json:"count"`
	}
	type Resp struct {
		CreatedByDay   map[string][]DayCount `json:"created_by_day"`
		CompletedByDay map[string][]DayCount `json:"completed_by_day"`
		Open           map[string]int        `json:"open"`
		Since          time.Time             `json:"since"`
	}
	var out Resp
	doJSON(t, srv, "GET", "/api/v1/projects/p1/dashboard/throughput", nil, 200, &out)

	for _, slug := range []string{"todos", "bugs", "use_cases"} {
		if _, ok := out.CreatedByDay[slug]; !ok {
			t.Errorf("created_by_day missing %q key", slug)
		}
		if _, ok := out.CompletedByDay[slug]; !ok {
			t.Errorf("completed_by_day missing %q key", slug)
		}
	}
	// Two created todos in window, one open snapshot.
	totalCreated := 0
	for _, dc := range out.CreatedByDay["todos"] {
		totalCreated += dc.Count
	}
	if totalCreated != 2 {
		t.Errorf("todos created sum = %d, want 2 (raw=%+v)", totalCreated, out.CreatedByDay["todos"])
	}
	totalCompleted := 0
	for _, dc := range out.CompletedByDay["todos"] {
		totalCompleted += dc.Count
	}
	if totalCompleted != 1 {
		t.Errorf("todos completed sum = %d, want 1", totalCompleted)
	}
	if out.Open["todos"] != 1 {
		t.Errorf("Open[todos] = %d, want 1", out.Open["todos"])
	}
	if out.Open["bugs"] != 0 || out.Open["use_cases"] != 0 {
		t.Errorf("non-todo open counts should be zero, got bugs=%d use_cases=%d", out.Open["bugs"], out.Open["use_cases"])
	}
	if out.Since.IsZero() {
		t.Errorf("since should be populated")
	}

	// 404 on unknown project.
	doJSON(t, srv, "GET", "/api/v1/projects/nope/dashboard/throughput", nil, 404, nil)

	// 400 on invalid days param.
	doJSON(t, srv, "GET", "/api/v1/projects/p1/dashboard/throughput?days=0", nil, 400, nil)
	doJSON(t, srv, "GET", "/api/v1/projects/p1/dashboard/throughput?days=notanumber", nil, 400, nil)
	doJSON(t, srv, "GET", "/api/v1/projects/p1/dashboard/throughput?days=999", nil, 400, nil)

	// Custom days param widens the window — should still succeed; we
	// don't assert shape since seeded data is recent.
	doJSON(t, srv, "GET", "/api/v1/projects/p1/dashboard/throughput?days=7", nil, 200, &out)
}

// TestDashboardIndexHealthEndpoint seeds a minimal corpus and checks
// the rollup shape comes through the HTTP surface, plus the 404 path.
func TestDashboardIndexHealthEndpoint(t *testing.T) {
	srv, _, store := setupWithStore(t)
	ctx := context.Background()
	now := time.Now()

	if err := store.CreateProject(ctx, &domain.Project{ID: "p1", Name: "P", CreatedAt: now}); err != nil {
		t.Fatalf("project: %v", err)
	}
	if err := store.CreateScratchpad(ctx, &domain.Scratchpad{
		ID: "sp1", ProjectID: "p1", Name: "main",
		ClassificationMode: "full", CreatedAt: now,
	}); err != nil {
		t.Fatalf("scratchpad: %v", err)
	}
	if err := store.CreateScratchpadItem(ctx, &domain.ScratchpadItem{
		ID: "si1", ScratchpadID: "sp1", ContentType: "text", Content: "seed",
		ClassificationState: "classified", CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatalf("item: %v", err)
	}
	if err := store.CreateCodeAnchor(ctx, &domain.CodeAnchor{
		ID: "a1", OwnerType: "scratchpad_item", OwnerID: "si1", Kind: "file",
		Path: "a.go", Provenance: "agent-suggested", CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatalf("anchor: %v", err)
	}

	type Resp struct {
		Coverage map[string]struct {
			Embedded int `json:"embedded"`
			Total    int `json:"total"`
		} `json:"coverage"`
		ChunkCount          int            `json:"chunk_count"`
		AnchorTotal         int            `json:"anchor_total"`
		AnchorsByProvenance map[string]int `json:"anchors_by_provenance"`
		DedupFlagged        int            `json:"dedup_flagged"`
	}
	var out Resp
	doJSON(t, srv, "GET", "/api/v1/projects/p1/dashboard/index-health", nil, 200, &out)

	if got := out.Coverage["items"]; got.Total != 1 || got.Embedded != 0 {
		t.Errorf("coverage.items = %+v, want total 1 / embedded 0", got)
	}
	for _, slug := range []string{"todos", "bugs", "kb", "use_cases"} {
		if _, ok := out.Coverage[slug]; !ok {
			t.Errorf("coverage missing %q key", slug)
		}
	}
	if out.AnchorTotal != 1 || out.AnchorsByProvenance["agent-suggested"] != 1 {
		t.Errorf("anchors = %d total %+v, want 1 agent-suggested", out.AnchorTotal, out.AnchorsByProvenance)
	}

	doJSON(t, srv, "GET", "/api/v1/projects/nope/dashboard/index-health", nil, 404, nil)
}

// TestDashboardLifecycleEndpoint drives both lifecycle panels: a real
// repo gives branch attribution + default branch; closed items cover
// the attributed / no-commit / unresolved buckets; durations feed the
// median/p90 stats.
func TestDashboardLifecycleEndpoint(t *testing.T) {
	srv, _, store := setupWithStore(t)
	ctx := context.Background()
	root, sha1, _ := seedRepo(t)

	now := time.Now()
	created := now.AddDate(0, 0, -2) // 48h before close
	if err := store.CreateProject(ctx, &domain.Project{
		ID: "p1", Name: "P", RepoRoot: root, CreatedAt: now,
	}); err != nil {
		t.Fatalf("project: %v", err)
	}
	if err := store.CreateScratchpad(ctx, &domain.Scratchpad{
		ID: "sp1", ProjectID: "p1", Name: "main",
		ClassificationMode: "full", CreatedAt: now,
	}); err != nil {
		t.Fatalf("scratchpad: %v", err)
	}
	if err := store.CreateScratchpadItem(ctx, &domain.ScratchpadItem{
		ID: "si1", ScratchpadID: "sp1", ContentType: "text", Content: "seed",
		ClassificationState: "classified", CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatalf("item: %v", err)
	}

	closedNow := now
	// Todo closed at sha1 (on the repo's default branch).
	if err := store.CreateTodoItem(ctx, &domain.TodoItem{
		ID: "t1", ProjectID: "p1", SourceItemID: "si1",
		Subject: "attributed", Priority: "none", Status: "complete",
		Origin: "agent-derived", CreatedAt: created, CompletedAt: &closedNow,
		CommitSHA: sha1,
	}); err != nil {
		t.Fatalf("todo attributed: %v", err)
	}
	// Bug closed with no commit reference.
	if err := store.CreateBugItem(ctx, &domain.BugItem{
		ID: "b1", ProjectID: "p1", SourceItemID: "si1",
		Subject: "no commit", Severity: "minor", Status: "fixed",
		Origin: "agent-derived", CreatedAt: created, CompletedAt: &closedNow,
	}); err != nil {
		t.Fatalf("bug no-commit: %v", err)
	}
	// Use case closed at a SHA the repo has never seen.
	if err := store.CreateUseCaseItem(ctx, &domain.UseCaseItem{
		ID: "uc1", ProjectID: "p1", SourceItemID: "si1",
		Subject: "ghost sha", Description: "x", Status: "completed",
		Origin: "agent-derived", CreatedAt: created, UpdatedAt: created,
		ImplementationDate: &closedNow,
		CommitSHA:          "0123456789abcdef0123456789abcdef01234567",
	}); err != nil {
		t.Fatalf("use_case unresolved: %v", err)
	}

	var out struct {
		TimeToClose map[string]struct {
			Count       int     `json:"count"`
			MedianHours float64 `json:"median_hours"`
			P90Hours    float64 `json:"p90_hours"`
		} `json:"time_to_close"`
		Branches []struct {
			Branch string         `json:"branch"`
			Counts map[string]int `json:"counts"`
			Total  int            `json:"total"`
		} `json:"branches"`
		NoCommit      map[string]int `json:"no_commit"`
		Unresolved    map[string]int `json:"unresolved"`
		RepoAvailable bool           `json:"repo_available"`
		DefaultBranch string         `json:"default_branch"`
	}
	doJSON(t, srv, "GET", "/api/v1/projects/p1/dashboard/lifecycle", nil, 200, &out)

	if !out.RepoAvailable {
		t.Fatalf("repo_available = false with a valid repo_root")
	}
	if out.DefaultBranch != "master" { // go-git PlainInit default
		t.Errorf("default_branch = %q, want master", out.DefaultBranch)
	}
	tt := out.TimeToClose["todos"]
	if tt.Count != 1 || tt.MedianHours < 47 || tt.MedianHours > 49 {
		t.Errorf("todos time_to_close = %+v, want count 1 / ~48h median", tt)
	}
	if len(out.Branches) != 1 || out.Branches[0].Branch != "master" ||
		out.Branches[0].Counts["todos"] != 1 || out.Branches[0].Total != 1 {
		t.Errorf("branches = %+v, want master with 1 todo", out.Branches)
	}
	if out.NoCommit["bugs"] != 1 {
		t.Errorf("no_commit = %+v, want bugs:1", out.NoCommit)
	}
	if out.Unresolved["use_cases"] != 1 {
		t.Errorf("unresolved = %+v, want use_cases:1", out.Unresolved)
	}

	doJSON(t, srv, "GET", "/api/v1/projects/nope/dashboard/lifecycle", nil, 404, nil)
}

// TestDashboardLifecycleNoRepo verifies graceful degradation: a project
// without repo_root still serves time-to-close, with branch data empty
// and items with SHAs landing in the unresolved bucket.
func TestDashboardLifecycleNoRepo(t *testing.T) {
	srv, _, store := setupWithStore(t)
	ctx := context.Background()
	now := time.Now()

	if err := store.CreateProject(ctx, &domain.Project{ID: "p1", Name: "P", CreatedAt: now}); err != nil {
		t.Fatalf("project: %v", err)
	}
	if err := store.CreateScratchpad(ctx, &domain.Scratchpad{
		ID: "sp1", ProjectID: "p1", Name: "main",
		ClassificationMode: "full", CreatedAt: now,
	}); err != nil {
		t.Fatalf("scratchpad: %v", err)
	}
	if err := store.CreateScratchpadItem(ctx, &domain.ScratchpadItem{
		ID: "si1", ScratchpadID: "sp1", ContentType: "text", Content: "seed",
		ClassificationState: "classified", CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatalf("item: %v", err)
	}
	closedNow := now
	if err := store.CreateTodoItem(ctx, &domain.TodoItem{
		ID: "t1", ProjectID: "p1", SourceItemID: "si1",
		Subject: "x", Priority: "none", Status: "complete",
		Origin: "agent-derived", CreatedAt: now.Add(-time.Hour), CompletedAt: &closedNow,
		CommitSHA: "0123456789abcdef0123456789abcdef01234567",
	}); err != nil {
		t.Fatalf("todo: %v", err)
	}

	var out struct {
		TimeToClose   map[string]struct{ Count int }`json:"time_to_close"`
		Branches      []any          `json:"branches"`
		Unresolved    map[string]int `json:"unresolved"`
		RepoAvailable bool           `json:"repo_available"`
	}
	doJSON(t, srv, "GET", "/api/v1/projects/p1/dashboard/lifecycle", nil, 200, &out)
	if out.RepoAvailable {
		t.Errorf("repo_available = true without repo_root")
	}
	if len(out.Branches) != 0 {
		t.Errorf("branches = %+v, want empty", out.Branches)
	}
	if out.TimeToClose["todos"].Count != 1 {
		t.Errorf("time_to_close.todos.count = %d, want 1", out.TimeToClose["todos"].Count)
	}
	if out.Unresolved["todos"] != 1 {
		t.Errorf("unresolved = %+v, want todos:1", out.Unresolved)
	}
}
