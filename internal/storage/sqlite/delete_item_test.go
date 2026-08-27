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
	"testing"
	"time"

	"codedistill/internal/domain"
	"codedistill/internal/storage"
)

// Deleting a group must remove its group_note (no FK covers it) and un-group its
// children via the ON DELETE SET NULL FK — all in one transaction (audit M24).
func TestDeleteScratchpadItem_GroupCleanup(t *testing.T) {
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
	must(s.CreateScratchpadItem(ctx, &domain.ScratchpadItem{ID: "g1", ScratchpadID: "sp1", Content: "grp", ContentType: "group", ClassificationState: "classified", CreatedAt: now}))
	must(s.CreateScratchpadItem(ctx, &domain.ScratchpadItem{ID: "c1", ScratchpadID: "sp1", Content: "child", ContentType: "text", GroupID: "g1", ClassificationState: "classified", CreatedAt: now}))
	must(s.SetGroupNote(ctx, "g1", "the group note", now))

	must(s.DeleteScratchpadItem(ctx, "g1"))

	// Group row gone.
	if _, err := s.GetScratchpadItem(ctx, "g1"); !errors.Is(err, storage.ErrNotFound) {
		t.Errorf("group still present: err = %v", err)
	}
	// group_note orphan removed.
	if note, _ := s.GetGroupNote(ctx, "g1"); note != "" {
		t.Errorf("orphaned group_note left behind: %q", note)
	}
	// Child survives, un-grouped by the FK.
	child, err := s.GetScratchpadItem(ctx, "c1")
	must(err)
	if child.GroupID != "" {
		t.Errorf("child GroupID = %q, want empty (FK ON DELETE SET NULL)", child.GroupID)
	}

	// Deleting a nonexistent item is ErrNotFound (transaction rolls back cleanly).
	if err := s.DeleteScratchpadItem(ctx, "nope"); !errors.Is(err, storage.ErrNotFound) {
		t.Errorf("delete missing: err = %v, want ErrNotFound", err)
	}
}
