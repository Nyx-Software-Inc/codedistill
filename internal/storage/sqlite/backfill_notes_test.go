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

// Migration 0060 retires the blob note columns: a catch-up pass converts any
// value edited since the one-shot 0055 backfill into an activity-log note —
// guarded by NOT EXISTS on an identical body so 0055's events don't duplicate —
// then drops the columns. The columns are already gone on a migrated store, so
// this test re-adds them, seeds legacy values, and replays the 0060 SQL.
func TestNoteBlobRetirementCatchUp(t *testing.T) {
	ctx := context.Background()
	s, err := Open(":memory:")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer s.Close()
	if err := s.Migrate(ctx); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	must := func(e error) {
		t.Helper()
		if e != nil {
			t.Fatal(e)
		}
	}
	now := time.Now().UTC()
	must(s.CreateProject(ctx, &domain.Project{ID: "p1", Name: "P", CreatedAt: now}))
	must(s.CreateScratchpad(ctx, &domain.Scratchpad{ID: "sp1", ProjectID: "p1", Name: "m", ClassificationMode: "full", CreatedAt: now}))
	must(s.CreateScratchpadItem(ctx, &domain.ScratchpadItem{
		ID: "si1", ScratchpadID: "sp1", ContentType: "text", Content: "x",
		ClassificationState: "classified", CreatedAt: now, UpdatedAt: now,
	}))
	must(s.CreateScratchpadItem(ctx, &domain.ScratchpadItem{
		ID: "g1", ScratchpadID: "sp1", ContentType: "group", Content: "",
		ClassificationState: "skipped", CreatedAt: now, UpdatedAt: now,
	}))
	must(s.CreateTodoItem(ctx, &domain.TodoItem{
		ID: "t1", ProjectID: "p1", Subject: "T", Priority: "none", Status: "incomplete",
		Origin: "agent-derived", CreatedAt: now,
	}))
	must(s.CreateBugItem(ctx, &domain.BugItem{
		ID: "b1", ProjectID: "p1", Subject: "B", Severity: "minor", Status: "open",
		Origin: "agent-derived", CreatedAt: now,
	}))

	// Recreate the pre-0060 state: the legacy columns holding values.
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := s.DB.ExecContext(ctx, q, args...); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	exec(`ALTER TABLE scratchpad_items ADD COLUMN annotations TEXT`)
	exec(`ALTER TABLE todo_items ADD COLUMN notes TEXT`)
	exec(`ALTER TABLE bug_items ADD COLUMN notes TEXT`)
	exec(`UPDATE scratchpad_items SET annotations = 'edited since 0055' WHERE id = 'si1'`)
	exec(`UPDATE scratchpad_items SET annotations = 'group note blob' WHERE id = 'g1'`)
	exec(`UPDATE todo_items SET notes = 'todo note' WHERE id = 't1'`)
	exec(`UPDATE bug_items SET notes = 'already logged' WHERE id = 'b1'`)
	// The bug's note already has its 0055 event — the catch-up must not duplicate it.
	must(s.RecordItemEvent(ctx, &domain.ItemEvent{
		ID: "e-prior", OwnerType: "bug_item", OwnerID: "b1", Kind: "note",
		Summary: "already logged", Body: "already logged", Source: "ui", CreatedAt: now,
	}))

	data, err := migrationsFS.ReadFile("migrations/0060_drop_note_blobs.sql")
	must(err)
	if _, err := s.DB.ExecContext(ctx, string(data)); err != nil {
		t.Fatalf("run 0060: %v", err)
	}

	notes := func(ownerType, ownerID string) []string {
		evs, _ := s.ListItemEvents(ctx, ownerType, ownerID)
		var out []string
		for _, e := range evs {
			if e.Kind == "note" {
				out = append(out, e.Body)
			}
		}
		return out
	}
	if got := notes("scratchpad_item", "si1"); len(got) != 1 || got[0] != "edited since 0055" {
		t.Errorf("si1 notes = %v, want the caught-up annotation", got)
	}
	if got := notes("todo_item", "t1"); len(got) != 1 || got[0] != "todo note" {
		t.Errorf("t1 notes = %v, want the caught-up note", got)
	}
	if got := notes("bug_item", "b1"); len(got) != 1 {
		t.Errorf("b1 notes = %v, want exactly the pre-existing event (no duplicate)", got)
	}
	if got := notes("scratchpad_item", "g1"); len(got) != 0 {
		t.Errorf("g1 notes = %v, group annotations must be skipped", got)
	}
	// And the columns are gone again.
	for _, q := range []string{
		`SELECT annotations FROM scratchpad_items LIMIT 1`,
		`SELECT notes FROM todo_items LIMIT 1`,
		`SELECT notes FROM bug_items LIMIT 1`,
	} {
		if _, err := s.DB.ExecContext(ctx, q); err == nil {
			t.Errorf("%s: column should be dropped", q)
		}
	}
}
