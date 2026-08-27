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

// A PATCH that fails validation must NOT leave its `notes` in the append-only
// activity log — otherwise the corrected retry duplicates the note (audit M19).
func TestUpdateTodo_NoteNotLoggedOn400(t *testing.T) {
	srv, _, store := setupWithStore(t)
	ctx := context.Background()
	if err := store.CreateProject(ctx, &domain.Project{ID: "p1", Name: "P", CreatedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	var td domain.TodoItem
	doJSON(t, srv, "POST", "/api/v1/projects/p1/todos", map[string]string{"subject": "x"}, 201, &td)

	// Invalid status + a note → 400; the note must not be recorded.
	doJSON(t, srv, "PATCH", "/api/v1/todos/"+td.ID,
		map[string]string{"status": "bogus", "notes": "first attempt"}, 400, nil)

	var log1 []domain.ItemEvent
	doJSON(t, srv, "GET", "/api/v1/todos/"+td.ID+"/log", nil, 200, &log1)
	if n := countNotes(log1); n != 0 {
		t.Fatalf("after 400: %d note(s) in log, want 0 (the failed PATCH must not log)", n)
	}

	// Corrected retry → 200; the note is recorded exactly once.
	doJSON(t, srv, "PATCH", "/api/v1/todos/"+td.ID,
		map[string]string{"status": "in_progress", "notes": "first attempt"}, 200, nil)

	var log2 []domain.ItemEvent
	doJSON(t, srv, "GET", "/api/v1/todos/"+td.ID+"/log", nil, 200, &log2)
	if n := countNotes(log2); n != 1 {
		t.Fatalf("after successful retry: %d note(s), want exactly 1 (no duplication)", n)
	}
}

// POSTing a note to a nonexistent owner must 404, not silently 201 an orphaned
// log entry (audit M19).
func TestAppendNote_NonexistentOwner404(t *testing.T) {
	srv, _, store := setupWithStore(t)
	ctx := context.Background()
	if err := store.CreateProject(ctx, &domain.Project{ID: "p1", Name: "P", CreatedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	// Bogus todo id → 404.
	doJSON(t, srv, "POST", "/api/v1/todos/does-not-exist/notes",
		map[string]string{"text": "orphan"}, 404, nil)

	// A real one → 201.
	var td domain.TodoItem
	doJSON(t, srv, "POST", "/api/v1/projects/p1/todos", map[string]string{"subject": "x"}, 201, &td)
	doJSON(t, srv, "POST", "/api/v1/todos/"+td.ID+"/notes",
		map[string]string{"text": "real note"}, 201, nil)
}

func countNotes(events []domain.ItemEvent) int {
	n := 0
	for _, e := range events {
		if e.Kind == "note" {
			n++
		}
	}
	return n
}
