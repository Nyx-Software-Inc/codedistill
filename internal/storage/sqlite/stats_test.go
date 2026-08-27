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

// GlobalStats must count rows across projects and break todos/bugs/use-cases
// down by status (UC-64).
func TestGlobalStats(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	now := time.Now().UTC()
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}

	must(s.CreateProject(ctx, &domain.Project{ID: "p1", Name: "P", CreatedAt: now}))
	must(s.CreateScratchpad(ctx, &domain.Scratchpad{ID: "sp1", ProjectID: "p1", Name: "m", ClassificationMode: "full", CreatedAt: now}))
	must(s.CreateScratchpadItem(ctx, &domain.ScratchpadItem{ID: "si1", ScratchpadID: "sp1", Content: "x", ContentType: "text", ClassificationState: "classified", CreatedAt: now}))

	// Todos: 2 incomplete, 1 complete.
	for i, st := range []string{"incomplete", "incomplete", "complete"} {
		must(s.CreateTodoItem(ctx, &domain.TodoItem{ID: "t" + string(rune('a'+i)), ProjectID: "p1", SourceItemID: "si1",
			Subject: "t", Priority: "none", Status: st, Origin: "agent-derived", CreatedAt: now}))
	}
	// Bugs: 1 open, 1 fixed.
	for i, st := range []string{"open", "fixed"} {
		must(s.CreateBugItem(ctx, &domain.BugItem{ID: "b" + string(rune('a'+i)), ProjectID: "p1", SourceItemID: "si1",
			Subject: "b", Severity: "minor", Status: st, Origin: "agent-derived", CreatedAt: now}))
	}
	// Use cases: 1 completed.
	must(s.CreateUseCaseItem(ctx, &domain.UseCaseItem{ID: "u1", ProjectID: "p1", SourceItemID: "si1",
		Subject: "u", Description: "u", Status: "completed", Origin: "agent-derived", CreatedAt: now, UpdatedAt: now}))
	// KB: 1 entry.
	must(s.CreateKnowledgeEntry(ctx, &domain.KnowledgeEntry{ID: "k1", ProjectID: "p1", SourceItemID: "si1",
		Title: "k", Content: "k", CreatedAt: now}))

	st, err := s.GlobalStats(ctx)
	must(err)

	if st.Projects != 1 {
		t.Errorf("projects = %d, want 1", st.Projects)
	}
	if st.Scratchpads != 1 {
		t.Errorf("scratchpads = %d, want 1", st.Scratchpads)
	}
	if st.KB != 1 {
		t.Errorf("kb = %d, want 1", st.KB)
	}
	if st.Todos["incomplete"] != 2 || st.Todos["complete"] != 1 {
		t.Errorf("todos = %v, want incomplete:2 complete:1", st.Todos)
	}
	if st.Bugs["open"] != 1 || st.Bugs["fixed"] != 1 {
		t.Errorf("bugs = %v, want open:1 fixed:1", st.Bugs)
	}
	if st.UseCases["completed"] != 1 {
		t.Errorf("use_cases = %v, want completed:1", st.UseCases)
	}
}
