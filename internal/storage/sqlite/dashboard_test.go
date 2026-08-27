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
	"codedistill/internal/storage"
)

// TestDashboardThroughput drives the metrics-panel query end-to-end:
// seeds todos / bugs / use_cases across multiple days with mixed
// statuses, then checks per-day buckets and open counts.
//
// All seeded timestamps are anchored in time.Local so SQLite's
// date(ts,'localtime') conversion is a no-op and day boundaries are
// stable regardless of where the test runs.
func TestDashboardThroughput(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	seedScratchpadItem(t, s)

	noon := func(year int, month time.Month, day int) time.Time {
		return time.Date(year, month, day, 12, 0, 0, 0, time.Local)
	}
	dayA := noon(2026, 4, 10)
	dayB := noon(2026, 4, 12)
	dayC := noon(2026, 4, 15)
	preCutoff := noon(2026, 3, 25)
	since := noon(2026, 4, 1)

	// Two todos on dayA (one completed dayB, one still open) + one on
	// dayC (open). Plus one created PRE-cutoff that should not appear
	// in the created series but should still count toward "open".
	completedB := dayB
	seedTodo := func(id, status string, created time.Time, completed *time.Time) {
		t.Helper()
		td := &domain.TodoItem{
			ID: id, ProjectID: "p1", SourceItemID: "si1",
			Subject: id, Priority: "none", Status: status,
			Origin: "agent-derived", CreatedAt: created, CompletedAt: completed,
		}
		if err := s.CreateTodoItem(ctx, td); err != nil {
			t.Fatalf("seed todo %s: %v", id, err)
		}
	}
	seedTodo("t1", "complete", dayA, &completedB)
	seedTodo("t2", "incomplete", dayA, nil)
	seedTodo("t3", "incomplete", dayC, nil)
	seedTodo("t-old", "incomplete", preCutoff, nil)

	// Bugs: one created dayB and fixed dayC, one open created dayC.
	completedC := dayC
	seedBug := func(id, status string, created time.Time, completed *time.Time) {
		t.Helper()
		b := &domain.BugItem{
			ID: id, ProjectID: "p1", SourceItemID: "si1",
			Subject: id, Severity: "minor", Status: status,
			Origin: "agent-derived", CreatedAt: created, CompletedAt: completed,
		}
		if err := s.CreateBugItem(ctx, b); err != nil {
			t.Fatalf("seed bug %s: %v", id, err)
		}
	}
	seedBug("b1", "fixed", dayB, &completedC)
	seedBug("b2", "open", dayC, nil)

	// Use_cases: one created dayA, implemented dayC; one open created
	// dayB. Note use_case completion uses implementation_date, not
	// completed_at.
	seedUC := func(id, status string, created time.Time, implDate *time.Time) {
		t.Helper()
		uc := &domain.UseCaseItem{
			ID: id, ProjectID: "p1", SourceItemID: "si1",
			Subject: id, Description: id, Status: status,
			Origin: "agent-derived", CreatedAt: created, UpdatedAt: created,
			ImplementationDate: implDate,
		}
		if err := s.CreateUseCaseItem(ctx, uc); err != nil {
			t.Fatalf("seed use_case %s: %v", id, err)
		}
	}
	implC := dayC
	seedUC("uc1", "completed", dayA, &implC)
	seedUC("uc2", "open", dayB, nil)

	out, err := s.DashboardThroughput(ctx, "p1", since)
	if err != nil {
		t.Fatalf("DashboardThroughput: %v", err)
	}

	wantSeries := func(name string, got []domain.DayCount, want map[string]int) {
		t.Helper()
		gotMap := map[string]int{}
		for _, dc := range got {
			gotMap[dc.Date] = dc.Count
		}
		for k, v := range want {
			if gotMap[k] != v {
				t.Errorf("%s[%s] = %d, want %d (full got: %+v)", name, k, gotMap[k], v, gotMap)
			}
		}
		for k := range gotMap {
			if _, ok := want[k]; !ok {
				t.Errorf("%s has unexpected day %s=%d", name, k, gotMap[k])
			}
		}
	}

	wantSeries("todos.created", out.CreatedByDay["todos"], map[string]int{
		"2026-04-10": 2, // t1 + t2 on dayA
		"2026-04-15": 1, // t3 on dayC
	})
	wantSeries("todos.completed", out.CompletedByDay["todos"], map[string]int{
		"2026-04-12": 1, // t1 completed dayB
	})
	wantSeries("bugs.created", out.CreatedByDay["bugs"], map[string]int{
		"2026-04-12": 1,
		"2026-04-15": 1,
	})
	wantSeries("bugs.completed", out.CompletedByDay["bugs"], map[string]int{
		"2026-04-15": 1,
	})
	wantSeries("use_cases.created", out.CreatedByDay["use_cases"], map[string]int{
		"2026-04-10": 1,
		"2026-04-12": 1,
	})
	wantSeries("use_cases.completed", out.CompletedByDay["use_cases"], map[string]int{
		"2026-04-15": 1, // implementation_date, not completed_at
	})

	// Open counts include the pre-cutoff todo (cutoff only filters the
	// time series; open is a snapshot of "right now").
	wantOpen := map[string]int{"todos": 3, "bugs": 1, "use_cases": 1}
	for k, v := range wantOpen {
		if out.Open[k] != v {
			t.Errorf("Open[%s] = %d, want %d", k, out.Open[k], v)
		}
	}

	if !out.Since.Equal(since) {
		t.Errorf("Since echoed back wrong: got %v want %v", out.Since, since)
	}
}

// TestDashboardThroughputEmptyProject verifies the shape when a fresh
// project has no items at all — the maps are non-nil, every type slug
// is present with an empty slice / zero count.
func TestDashboardThroughputEmptyProject(t *testing.T) {
	s := newTestStore(t)
	seedProject(t, s)

	out, err := s.DashboardThroughput(context.Background(), "p1", time.Date(2026, 4, 1, 0, 0, 0, 0, time.Local))
	if err != nil {
		t.Fatalf("DashboardThroughput: %v", err)
	}
	for _, slug := range []string{"todos", "bugs", "use_cases"} {
		if out.CreatedByDay[slug] == nil {
			t.Errorf("CreatedByDay[%s] is nil; want empty slice", slug)
		}
		if len(out.CreatedByDay[slug]) != 0 {
			t.Errorf("CreatedByDay[%s] non-empty on empty project: %+v", slug, out.CreatedByDay[slug])
		}
		if out.CompletedByDay[slug] == nil {
			t.Errorf("CompletedByDay[%s] is nil", slug)
		}
		if out.Open[slug] != 0 {
			t.Errorf("Open[%s] = %d, want 0", slug, out.Open[slug])
		}
	}
}

// TestDashboardIndexHealth seeds a small mixed corpus — embedded and
// unembedded items, code chunks (embedded / pending / failed), anchors
// across owner types, one dedup flag — and checks every rollup field.
func TestDashboardIndexHealth(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	seedScratchpadItem(t, s) // p1 / sp1 / si1
	now := fixedTime(t)
	vec := []byte{1, 2, 3, 4}

	// Second scratchpad item; embed only si1 and flag si2 as its dup.
	si2 := &domain.ScratchpadItem{
		ID: "si2", ScratchpadID: "sp1", ContentType: "text", Content: "hello again",
		ClassificationState: "unprocessed", CreatedAt: now, UpdatedAt: now,
	}
	if err := s.CreateScratchpadItem(ctx, si2); err != nil {
		t.Fatalf("seed si2: %v", err)
	}
	if err := s.UpdateEmbedding(ctx, storage.TableScratchpadItems, "si1", vec, now); err != nil {
		t.Fatalf("embed si1: %v", err)
	}
	if err := s.SetItemSimilarity(ctx, "si2", "si1", 0.97); err != nil {
		t.Fatalf("flag dup: %v", err)
	}

	// One todo (embedded) + one bug (not embedded).
	td := &domain.TodoItem{
		ID: "t1", ProjectID: "p1", SourceItemID: "si1", Subject: "t1",
		Priority: "none", Status: "incomplete", Origin: "agent-derived", CreatedAt: now,
	}
	if err := s.CreateTodoItem(ctx, td); err != nil {
		t.Fatalf("seed todo: %v", err)
	}
	if err := s.UpdateEmbedding(ctx, storage.TableTodoItems, "t1", vec, now); err != nil {
		t.Fatalf("embed todo: %v", err)
	}
	bg := &domain.BugItem{
		ID: "b1", ProjectID: "p1", SourceItemID: "si1", Subject: "b1",
		Severity: "minor", Status: "open", Origin: "agent-derived", CreatedAt: now,
	}
	if err := s.CreateBugItem(ctx, bg); err != nil {
		t.Fatalf("seed bug: %v", err)
	}

	// Chunks: one embedded, one pending, one failed.
	seedChunk := func(id string, lineStart int) {
		t.Helper()
		c := &domain.CodeChunk{
			ID: id, ProjectID: "p1", FilePath: "a.go",
			LineStart: lineStart, LineEnd: lineStart + 9,
			ContentHash: id, Content: "0123456789", // 10 bytes each
			CreatedAt: now, UpdatedAt: now,
		}
		if err := s.UpsertCodeChunk(ctx, c); err != nil {
			t.Fatalf("seed chunk %s: %v", id, err)
		}
	}
	seedChunk("c1", 1)
	seedChunk("c2", 11)
	seedChunk("c3", 21)
	if err := s.UpdateCodeChunkEmbedding(ctx, "c1", vec, now); err != nil {
		t.Fatalf("embed chunk: %v", err)
	}
	if err := s.MarkCodeChunkEmbedFailed(ctx, "c3", "boom", now); err != nil {
		t.Fatalf("fail chunk: %v", err)
	}

	// Anchors: user-set on the todo, two agent-suggested on item + bug.
	seedAnchor := func(id, ownerType, ownerID, prov string) {
		t.Helper()
		a := &domain.CodeAnchor{
			ID: id, OwnerType: ownerType, OwnerID: ownerID, Kind: "file",
			Path: "a.go", Provenance: prov, CreatedAt: now, UpdatedAt: now,
		}
		if err := s.CreateCodeAnchor(ctx, a); err != nil {
			t.Fatalf("seed anchor %s: %v", id, err)
		}
	}
	seedAnchor("a1", "todo_item", "t1", "user-set")
	seedAnchor("a2", "scratchpad_item", "si1", "agent-suggested")
	seedAnchor("a3", "bug_item", "b1", "agent-suggested")

	out, err := s.DashboardIndexHealth(ctx, "p1")
	if err != nil {
		t.Fatalf("DashboardIndexHealth: %v", err)
	}

	wantCov := map[string]domain.CoverageCount{
		"items":     {Embedded: 1, Total: 2},
		"todos":     {Embedded: 1, Total: 1},
		"bugs":      {Embedded: 0, Total: 1},
		"kb":        {Embedded: 0, Total: 0},
		"use_cases": {Embedded: 0, Total: 0},
	}
	for slug, want := range wantCov {
		if out.Coverage[slug] != want {
			t.Errorf("Coverage[%s] = %+v, want %+v", slug, out.Coverage[slug], want)
		}
	}
	if out.ChunkCount != 3 || out.ChunkBytes != 30 {
		t.Errorf("chunks = %d/%d bytes, want 3/30", out.ChunkCount, out.ChunkBytes)
	}
	if out.ChunkUnembedded != 1 || out.ChunkFailed != 1 {
		t.Errorf("chunk backlog = %d pending / %d failed, want 1/1", out.ChunkUnembedded, out.ChunkFailed)
	}
	if out.AnchorTotal != 3 {
		t.Errorf("AnchorTotal = %d, want 3", out.AnchorTotal)
	}
	if out.AnchorsByProvenance["agent-suggested"] != 2 || out.AnchorsByProvenance["user-set"] != 1 {
		t.Errorf("AnchorsByProvenance = %+v, want agent-suggested:2 user-set:1", out.AnchorsByProvenance)
	}
	if out.DedupFlagged != 1 {
		t.Errorf("DedupFlagged = %d, want 1", out.DedupFlagged)
	}
}

// TestDashboardClosedItems checks that only terminal items come back,
// with the right per-type closed timestamp and an empty-string SHA when
// the item closed without a commit reference.
func TestDashboardClosedItems(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	seedScratchpadItem(t, s)

	created := time.Date(2026, 4, 10, 12, 0, 0, 0, time.Local)
	closed := time.Date(2026, 4, 12, 12, 0, 0, 0, time.Local)
	sha := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

	td := &domain.TodoItem{
		ID: "t1", ProjectID: "p1", SourceItemID: "si1", Subject: "t1",
		Priority: "none", Status: "complete", Origin: "agent-derived",
		CreatedAt: created, CompletedAt: &closed, CommitSHA: sha,
	}
	if err := s.CreateTodoItem(ctx, td); err != nil {
		t.Fatalf("seed closed todo: %v", err)
	}
	open := &domain.TodoItem{
		ID: "t2", ProjectID: "p1", SourceItemID: "si1", Subject: "t2",
		Priority: "none", Status: "incomplete", Origin: "agent-derived", CreatedAt: created,
	}
	if err := s.CreateTodoItem(ctx, open); err != nil {
		t.Fatalf("seed open todo: %v", err)
	}
	uc := &domain.UseCaseItem{
		ID: "uc1", ProjectID: "p1", SourceItemID: "si1", Subject: "uc1",
		Description: "uc1", Status: "completed", Origin: "agent-derived",
		CreatedAt: created, UpdatedAt: created, ImplementationDate: &closed,
	}
	if err := s.CreateUseCaseItem(ctx, uc); err != nil {
		t.Fatalf("seed use_case: %v", err)
	}

	out, err := s.DashboardClosedItems(ctx, "p1")
	if err != nil {
		t.Fatalf("DashboardClosedItems: %v", err)
	}
	if len(out) != 2 {
		t.Fatalf("got %d closed items, want 2: %+v", len(out), out)
	}
	byType := map[string]domain.DashboardClosedItem{}
	for _, ci := range out {
		byType[ci.Type] = ci
	}
	gotTodo := byType["todos"]
	if gotTodo.CommitSHA != sha {
		t.Errorf("todo sha = %q, want %q", gotTodo.CommitSHA, sha)
	}
	if !gotTodo.ClosedAt.Equal(closed) || !gotTodo.CreatedAt.Equal(created) {
		t.Errorf("todo times = %v→%v, want %v→%v", gotTodo.CreatedAt, gotTodo.ClosedAt, created, closed)
	}
	gotUC := byType["use_cases"]
	if gotUC.CommitSHA != "" {
		t.Errorf("use_case sha = %q, want empty", gotUC.CommitSHA)
	}
	if !gotUC.ClosedAt.Equal(closed) {
		t.Errorf("use_case closed = %v, want %v (implementation_date)", gotUC.ClosedAt, closed)
	}
}
