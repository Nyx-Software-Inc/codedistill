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

// LatestNotes returns the most recent note per owner (the canvas pulse) and
// ignores non-note events.
func TestLatestNotes(t *testing.T) {
	ctx := context.Background()
	s, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	base := time.Now().UTC()
	rec := func(id, ot, oid, kind, body string, at time.Time) {
		if err := s.RecordItemEvent(ctx, &domain.ItemEvent{
			ID: id, OwnerType: ot, OwnerID: oid, Kind: kind, Body: body, Summary: body, Source: "ui", CreatedAt: at,
		}); err != nil {
			t.Fatal(err)
		}
	}
	rec("e1", "todo_item", "t1", "note", "old", base)
	rec("e2", "todo_item", "t1", "note", "new", base.Add(time.Hour)) // newer wins
	rec("e3", "bug_item", "b1", "note", "bugnote", base)
	rec("e4", "todo_item", "t1", "status-changed", "ignore me", base.Add(2*time.Hour)) // not a note

	got, err := s.LatestNotes(ctx)
	if err != nil {
		t.Fatal(err)
	}
	m := map[string]string{}
	for _, e := range got {
		m[e.OwnerType+":"+e.OwnerID] = e.Summary
	}
	if len(got) != 2 {
		t.Fatalf("want 2 owners, got %d (%v)", len(got), m)
	}
	if m["todo_item:t1"] != "new" {
		t.Errorf("todo pulse should be the latest note 'new', got %q", m["todo_item:t1"])
	}
	if m["bug_item:b1"] != "bugnote" {
		t.Errorf("bug pulse missing")
	}
}
