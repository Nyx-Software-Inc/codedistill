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
)

// TestMigration0029BackfillSourceItemID seeds the pre-0029 data shape —
// a derived todo with NULL source_item_id whose scratchpad item still
// points at it via derived_item_id, plus a true orphan with no
// surviving pointer — then runs the remaining migrations and checks
// the backfill links the first and leaves the orphan NULL.
func TestMigration0029BackfillSourceItemID(t *testing.T) {
	s, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	ctx := context.Background()
	applyMigrationsThrough(t, s, "0028_match_commit_author.sql")

	mustExec := func(q string, args ...any) {
		t.Helper()
		if _, err := s.DB.ExecContext(ctx, q, args...); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	mustExec(`INSERT INTO projects (id, name) VALUES ('p1', 'P')`)
	mustExec(`INSERT INTO scratchpads (id, project_id, name, classification_mode) VALUES ('sp1', 'p1', 'main', 'full')`)
	// Scratchpad item with the forward pointer only (pre-fix epoch).
	mustExec(`INSERT INTO scratchpad_items (id, scratchpad_id, content_type, content, classification_state, derived_item_id)
	          VALUES ('si1', 'sp1', 'text', 'fix the thing', 'classified', 't-linked')`)
	mustExec(`INSERT INTO todo_items (id, project_id, number, subject, priority, status, origin)
	          VALUES ('t-linked', 'p1', 1, 'linked todo', 'none', 'incomplete', 'agent-derived')`)
	// Orphan: no scratchpad item points at it.
	mustExec(`INSERT INTO todo_items (id, project_id, number, subject, priority, status, origin)
	          VALUES ('t-orphan', 'p1', 2, 'orphan todo', 'none', 'incomplete', 'agent-derived')`)
	// Already-linked row must not be touched.
	mustExec(`INSERT INTO scratchpad_items (id, scratchpad_id, content_type, content, classification_state, derived_item_id)
	          VALUES ('si2', 'sp1', 'text', 'another', 'classified', 't-modern')`)
	mustExec(`INSERT INTO todo_items (id, project_id, source_item_id, number, subject, priority, status, origin)
	          VALUES ('t-modern', 'p1', 'si2', 3, 'modern todo', 'none', 'incomplete', 'agent-derived')`)

	if err := s.Migrate(ctx); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	want := map[string]any{"t-linked": "si1", "t-orphan": nil, "t-modern": "si2"}
	for id, wantSrc := range want {
		var got any
		if err := s.DB.QueryRowContext(ctx,
			`SELECT source_item_id FROM todo_items WHERE id = ?`, id).Scan(&got); err != nil {
			t.Fatalf("read %s: %v", id, err)
		}
		if got != wantSrc {
			t.Errorf("%s source_item_id = %v, want %v", id, got, wantSrc)
		}
	}

	// The scratchpad-scoped join (what the banner + drawer panes use)
	// now sees both linked todos.
	todos, err := s.ListTodoItemsByScratchpad(ctx, "sp1")
	if err != nil {
		t.Fatal(err)
	}
	if len(todos) != 2 {
		t.Errorf("ByScratchpad sees %d todos, want 2 (linked + modern)", len(todos))
	}
}
