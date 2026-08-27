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
	"reflect"
	"testing"

	"codedistill/internal/domain"
	"codedistill/internal/id"
	"codedistill/internal/storage"
)

// --- CodeAnchor round-trip (all three kinds) ---

func TestCodeAnchorRoundTrip_File(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	item := seedScratchpadItem(t, s)

	a := &domain.CodeAnchor{
		ID: "ca1", OwnerType: "scratchpad_item", OwnerID: item.ID,
		Kind: "file", Path: "internal/api/scratchpads.go",
		LineStart: 62, LineEnd: 80,
		Revision:   "a1b2c3d4e5f6",
		Label:      "scratchpad create",
		Provenance: "user-set",
		CreatedAt:  fixedTime(t),
		UpdatedAt:  fixedTime(t),
	}
	if err := s.CreateCodeAnchor(ctx, a); err != nil {
		t.Fatalf("create: %v", err)
	}

	got, err := s.GetCodeAnchor(ctx, "ca1")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if !reflect.DeepEqual(got, a) {
		t.Errorf("round-trip mismatch:\n got:  %+v\n want: %+v", got, a)
	}
}

func TestCodeAnchorRoundTrip_Commit(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	item := seedScratchpadItem(t, s)

	a := &domain.CodeAnchor{
		ID: "ca2", OwnerType: "scratchpad_item", OwnerID: item.ID,
		Kind: "commit", Revision: "a1b2c3d",
		URL:        "https://github.com/foo/bar/commit/a1b2c3d",
		Provenance: "user-set",
		CreatedAt:  fixedTime(t),
		UpdatedAt:  fixedTime(t),
	}
	if err := s.CreateCodeAnchor(ctx, a); err != nil {
		t.Fatalf("create: %v", err)
	}
	got, _ := s.GetCodeAnchor(ctx, "ca2")
	if !reflect.DeepEqual(got, a) {
		t.Errorf("round-trip mismatch:\n got:  %+v\n want: %+v", got, a)
	}
}

func TestCodeAnchorRoundTrip_PR(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	item := seedScratchpadItem(t, s)

	a := &domain.CodeAnchor{
		ID: "ca3", OwnerType: "scratchpad_item", OwnerID: item.ID,
		Kind: "pr", URL: "https://github.com/foo/bar/pull/42",
		Provenance: "url-detected",
		CreatedAt:  fixedTime(t),
		UpdatedAt:  fixedTime(t),
	}
	if err := s.CreateCodeAnchor(ctx, a); err != nil {
		t.Fatalf("create: %v", err)
	}
	got, _ := s.GetCodeAnchor(ctx, "ca3")
	if !reflect.DeepEqual(got, a) {
		t.Errorf("round-trip mismatch:\n got:  %+v\n want: %+v", got, a)
	}
}

func TestCodeAnchorList_OrderedByCreatedAt(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	item := seedScratchpadItem(t, s)

	first := fixedTime(t)
	second := first.Add(1)
	third := first.Add(2)

	for _, a := range []*domain.CodeAnchor{
		{ID: "c", OwnerType: "scratchpad_item", OwnerID: item.ID, Kind: "commit", Revision: "aaaaaaa", Provenance: "user-set", CreatedAt: third, UpdatedAt: third},
		{ID: "a", OwnerType: "scratchpad_item", OwnerID: item.ID, Kind: "commit", Revision: "bbbbbbb", Provenance: "user-set", CreatedAt: first, UpdatedAt: first},
		{ID: "b", OwnerType: "scratchpad_item", OwnerID: item.ID, Kind: "commit", Revision: "ccccccc", Provenance: "user-set", CreatedAt: second, UpdatedAt: second},
	} {
		if err := s.CreateCodeAnchor(ctx, a); err != nil {
			t.Fatalf("create %s: %v", a.ID, err)
		}
	}
	list, err := s.ListCodeAnchors(ctx, "scratchpad_item", item.ID)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	wantIDs := []string{"a", "b", "c"}
	gotIDs := make([]string, len(list))
	for i, a := range list {
		gotIDs[i] = a.ID
	}
	if !reflect.DeepEqual(gotIDs, wantIDs) {
		t.Errorf("order: got %v, want %v", gotIDs, wantIDs)
	}
}

func TestCodeAnchorUpdate(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	item := seedScratchpadItem(t, s)

	a := &domain.CodeAnchor{
		ID: "ca1", OwnerType: "scratchpad_item", OwnerID: item.ID,
		Kind: "file", Path: "old.go", LineStart: 1, LineEnd: 5,
		Provenance: "user-set", CreatedAt: fixedTime(t), UpdatedAt: fixedTime(t),
	}
	if err := s.CreateCodeAnchor(ctx, a); err != nil {
		t.Fatalf("create: %v", err)
	}
	a.Path = "new.go"
	a.LineStart = 10
	a.LineEnd = 20
	a.Label = "refactored"
	if err := s.UpdateCodeAnchor(ctx, a); err != nil {
		t.Fatalf("update: %v", err)
	}
	got, _ := s.GetCodeAnchor(ctx, "ca1")
	if got.Path != "new.go" || got.LineStart != 10 || got.LineEnd != 20 || got.Label != "refactored" {
		t.Errorf("update not applied: %+v", got)
	}
}

func TestCodeAnchorUpdateNotFound(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	a := &domain.CodeAnchor{ID: "nope", OwnerType: "scratchpad_item", OwnerID: "x", Kind: "commit", Revision: "abcd123", Provenance: "user-set"}
	if err := s.UpdateCodeAnchor(ctx, a); !errors.Is(err, storage.ErrNotFound) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

// --- Cascade on owner delete ---

func TestCodeAnchorCascadeOnScratchpadItemDelete(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	item := seedScratchpadItem(t, s)

	a := &domain.CodeAnchor{
		ID: "ca1", OwnerType: "scratchpad_item", OwnerID: item.ID,
		Kind: "commit", Revision: "aaaaaaa",
		Provenance: "user-set", CreatedAt: fixedTime(t), UpdatedAt: fixedTime(t),
	}
	if err := s.CreateCodeAnchor(ctx, a); err != nil {
		t.Fatalf("create anchor: %v", err)
	}

	if err := s.DeleteScratchpadItem(ctx, item.ID); err != nil {
		t.Fatalf("delete item: %v", err)
	}
	if _, err := s.GetCodeAnchor(ctx, "ca1"); !errors.Is(err, storage.ErrNotFound) {
		t.Errorf("expected anchor gone after item delete, got %v", err)
	}
}

func TestCodeAnchorCascadeOnTodoDelete(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	seedProject(t, s)

	todo := &domain.TodoItem{
		ID: "t1", ProjectID: "p1", Subject: "task", Priority: "none",
		Status: "incomplete", Origin: "manual", CreatedAt: fixedTime(t),
	}
	if err := s.CreateTodoItem(ctx, todo); err != nil {
		t.Fatalf("create todo: %v", err)
	}
	a := &domain.CodeAnchor{
		ID: "ca1", OwnerType: "todo_item", OwnerID: todo.ID,
		Kind: "commit", Revision: "aaaaaaa",
		Provenance: "user-set", CreatedAt: fixedTime(t), UpdatedAt: fixedTime(t),
	}
	if err := s.CreateCodeAnchor(ctx, a); err != nil {
		t.Fatalf("create anchor: %v", err)
	}
	if err := s.DeleteTodoItem(ctx, todo.ID); err != nil {
		t.Fatalf("delete todo: %v", err)
	}
	if _, err := s.GetCodeAnchor(ctx, "ca1"); !errors.Is(err, storage.ErrNotFound) {
		t.Errorf("expected anchor gone after todo delete, got %v", err)
	}
}

func TestCopyCodeAnchors(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	item := seedScratchpadItem(t, s)

	src := &domain.CodeAnchor{
		ID: "src1", OwnerType: "scratchpad_item", OwnerID: item.ID,
		Kind: "file", Path: "foo.go", LineStart: 10, LineEnd: 20,
		Provenance: "user-set", CreatedAt: fixedTime(t), UpdatedAt: fixedTime(t),
	}
	if err := s.CreateCodeAnchor(ctx, src); err != nil {
		t.Fatalf("seed src anchor: %v", err)
	}
	// Create a bug to copy onto.
	b := &domain.BugItem{
		ID: "b1", ProjectID: "p1", SourceItemID: item.ID,
		Subject: "bug", Severity: "minor", Status: "open",
		Origin: "agent-derived", CreatedAt: fixedTime(t),
	}
	if err := s.CreateBugItem(ctx, b); err != nil {
		t.Fatalf("create bug: %v", err)
	}

	now := fixedTime(t)
	if err := s.CopyCodeAnchors(ctx, "scratchpad_item", item.ID, "bug_item", b.ID, id.New, now); err != nil {
		t.Fatalf("copy: %v", err)
	}
	dst, err := s.ListCodeAnchors(ctx, "bug_item", b.ID)
	if err != nil {
		t.Fatalf("list dst: %v", err)
	}
	if len(dst) != 1 {
		t.Fatalf("expected 1 copied anchor, got %d", len(dst))
	}
	if dst[0].ID == src.ID {
		t.Errorf("copied anchor kept source ID %q — expected fresh ID", src.ID)
	}
	if dst[0].Path != src.Path || dst[0].LineStart != src.LineStart || dst[0].LineEnd != src.LineEnd {
		t.Errorf("copied fields mismatch: %+v", dst[0])
	}
	if dst[0].OwnerType != "bug_item" || dst[0].OwnerID != b.ID {
		t.Errorf("copied anchor has wrong owner: %+v", dst[0])
	}

	// Source anchor is preserved.
	if _, err := s.GetCodeAnchor(ctx, src.ID); err != nil {
		t.Errorf("src anchor should still exist: %v", err)
	}
}

// --- Project repo_root round-trip ---

func TestProjectRepoRootRoundTrip(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	p := &domain.Project{
		ID: "p1", Name: "repo-backed",
		RepoRoot: "/home/dev/projects/foo", CreatedAt: fixedTime(t),
	}
	if err := s.CreateProject(ctx, p); err != nil {
		t.Fatalf("create: %v", err)
	}
	got, err := s.GetProject(ctx, "p1")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.RepoRoot != p.RepoRoot {
		t.Errorf("repo_root mismatch: got %q, want %q", got.RepoRoot, p.RepoRoot)
	}
	p.RepoRoot = "/tmp/other"
	if err := s.UpdateProject(ctx, p); err != nil {
		t.Fatalf("update: %v", err)
	}
	got, _ = s.GetProject(ctx, "p1")
	if got.RepoRoot != "/tmp/other" {
		t.Errorf("updated repo_root: got %q, want /tmp/other", got.RepoRoot)
	}
	// Unset via empty string round-trips as "".
	p.RepoRoot = ""
	if err := s.UpdateProject(ctx, p); err != nil {
		t.Fatalf("unset: %v", err)
	}
	got, _ = s.GetProject(ctx, "p1")
	if got.RepoRoot != "" {
		t.Errorf("unset repo_root: got %q", got.RepoRoot)
	}
}

// TestListCodeAnchorsByPath verifies the cross-owner UNION query: all five
// owner types contribute anchors filtered by (project_id, path), and rows
// from a different project / different path / different kind are excluded.
func TestListCodeAnchorsByPath(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	// Seed: a project with one scratchpad + one item; one todo, one bug,
	// one KB entry, one use-case in that project; one extra todo in a
	// second project.
	item := seedScratchpadItem(t, s) // creates project p1, scratchpad sp1, item si1
	now := fixedTime(t)

	todo := &domain.TodoItem{
		ID: "td1", ProjectID: "p1", Subject: "Refactor router",
		Status: "incomplete", Priority: "none", Origin: "manual",
		CreatedAt: now,
	}
	if err := s.CreateTodoItem(ctx, todo); err != nil {
		t.Fatalf("seed todo: %v", err)
	}
	bug := &domain.BugItem{
		ID: "bg1", ProjectID: "p1", Subject: "Race in handler",
		Severity: "minor", Status: "open", Origin: "manual",
		CreatedAt: now,
	}
	if err := s.CreateBugItem(ctx, bug); err != nil {
		t.Fatalf("seed bug: %v", err)
	}
	kb := &domain.KnowledgeEntry{
		ID: "kb1", ProjectID: "p1", Title: "Auth notes", Content: "blob",
		CreatedAt: now,
	}
	if err := s.CreateKnowledgeEntry(ctx, kb); err != nil {
		t.Fatalf("seed kb: %v", err)
	}
	uc := &domain.UseCaseItem{
		ID: "uc1", ProjectID: "p1", Subject: "Login flow",
		Role: "user", Want: "log in", Status: "open", Origin: "manual",
		CreatedAt: now,
	}
	if err := s.CreateUseCaseItem(ctx, uc); err != nil {
		t.Fatalf("seed use case: %v", err)
	}

	// Other-project todo whose anchor must be excluded.
	other := &domain.Project{ID: "p2", Name: "Other", CreatedAt: now}
	if err := s.CreateProject(ctx, other); err != nil {
		t.Fatalf("seed other project: %v", err)
	}
	otherTodo := &domain.TodoItem{
		ID: "td-other", ProjectID: "p2", Subject: "Should not appear",
		Status: "incomplete", Priority: "none", Origin: "manual",
		CreatedAt: now,
	}
	if err := s.CreateTodoItem(ctx, otherTodo); err != nil {
		t.Fatalf("seed other todo: %v", err)
	}

	// Anchors. All five matching owners point at the same path; one
	// commit-kind anchor and one wrong-path anchor should be excluded.
	for _, a := range []*domain.CodeAnchor{
		{ID: id.New(), OwnerType: "scratchpad_item", OwnerID: item.ID,
			Kind: "file", Path: "internal/api/server.go", LineStart: 10, LineEnd: 20,
			Provenance: "user-set", CreatedAt: now, UpdatedAt: now},
		{ID: id.New(), OwnerType: "todo_item", OwnerID: todo.ID,
			Kind: "file", Path: "internal/api/server.go", LineStart: 30, LineEnd: 40,
			Provenance: "user-set", CreatedAt: now, UpdatedAt: now},
		{ID: id.New(), OwnerType: "bug_item", OwnerID: bug.ID,
			Kind: "file", Path: "internal/api/server.go", LineStart: 50, LineEnd: 55,
			Provenance: "agent-suggested", CreatedAt: now, UpdatedAt: now},
		{ID: id.New(), OwnerType: "knowledge_entry", OwnerID: kb.ID,
			Kind: "file", Path: "internal/api/server.go", LineStart: 70,
			Provenance: "user-set", CreatedAt: now, UpdatedAt: now},
		{ID: id.New(), OwnerType: "use_case_item", OwnerID: uc.ID,
			Kind: "file", Path: "internal/api/server.go", LineStart: 80, LineEnd: 90,
			Provenance: "user-set", CreatedAt: now, UpdatedAt: now},
		// Excluded: wrong path
		{ID: id.New(), OwnerType: "todo_item", OwnerID: todo.ID,
			Kind: "file", Path: "other.go", LineStart: 1,
			Provenance: "user-set", CreatedAt: now, UpdatedAt: now},
		// Excluded: wrong kind
		{ID: id.New(), OwnerType: "todo_item", OwnerID: todo.ID,
			Kind: "commit", Revision: "abcdef1",
			Provenance: "user-set", CreatedAt: now, UpdatedAt: now},
		// Excluded: other project
		{ID: id.New(), OwnerType: "todo_item", OwnerID: otherTodo.ID,
			Kind: "file", Path: "internal/api/server.go", LineStart: 99,
			Provenance: "user-set", CreatedAt: now, UpdatedAt: now},
	} {
		if err := s.CreateCodeAnchor(ctx, a); err != nil {
			t.Fatalf("create anchor %s: %v", a.OwnerType, err)
		}
	}

	got, err := s.ListCodeAnchorsByPath(ctx, "p1", "internal/api/server.go")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(got) != 5 {
		t.Fatalf("got %d anchors, want 5", len(got))
	}
	// Sorted by line_start ascending.
	wantLines := []int{10, 30, 50, 70, 80}
	for i, a := range got {
		if a.LineStart != wantLines[i] {
			t.Errorf("position %d: line_start = %d, want %d", i, a.LineStart, wantLines[i])
		}
	}
	// scratchpad_item owner gets owner_scratchpad_id; others must be empty.
	if got[0].OwnerType != "scratchpad_item" {
		t.Fatalf("first anchor owner_type = %q, want scratchpad_item", got[0].OwnerType)
	}
	if got[0].OwnerScratchpadID != "sp1" {
		t.Errorf("scratchpad_item anchor: owner_scratchpad_id = %q, want sp1", got[0].OwnerScratchpadID)
	}
	for i, a := range got[1:] {
		if a.OwnerScratchpadID != "" {
			t.Errorf("derived-item anchor %d (%s): owner_scratchpad_id = %q, want empty", i+1, a.OwnerType, a.OwnerScratchpadID)
		}
	}
	// Owner titles populated from the right column per owner type.
	titles := map[string]string{
		"scratchpad_item": "hello", // first 60 chars of content
		"todo_item":       "Refactor router",
		"bug_item":        "Race in handler",
		"knowledge_entry": "Auth notes",
		"use_case_item":   "log in", // want field
	}
	for _, a := range got {
		want := titles[a.OwnerType]
		if a.OwnerTitle != want {
			t.Errorf("owner %s: title = %q, want %q", a.OwnerType, a.OwnerTitle, want)
		}
	}
}

