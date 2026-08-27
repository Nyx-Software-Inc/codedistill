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

// A group frame's note is read through from group_notes on both the single-get
// and the list path. Non-group items have no stored annotations at all (the
// blob column is dropped; notes live on the activity log).
func TestGroupNoteReadThrough(t *testing.T) {
	ctx := context.Background()
	s, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if err := s.CreateProject(ctx, &domain.Project{ID: "p1", Name: "P", CreatedAt: now}); err != nil {
		t.Fatal(err)
	}
	if err := s.CreateScratchpad(ctx, &domain.Scratchpad{ID: "sp1", ProjectID: "p1", Name: "m", ClassificationMode: "full", CreatedAt: now}); err != nil {
		t.Fatal(err)
	}
	mk := func(id, contentType, annotations string) {
		if err := s.CreateScratchpadItem(ctx, &domain.ScratchpadItem{
			ID: id, ScratchpadID: "sp1", ContentType: contentType, Content: "x",
			ClassificationState: "skipped", Annotations: annotations,
			CreatedAt: now, UpdatedAt: now,
		}); err != nil {
			t.Fatal(err)
		}
	}
	mk("grp1", "group", "stale blob note")
	mk("txt1", "text", "plain item blob")
	if err := s.SetGroupNote(ctx, "grp1", "canonical group note", now); err != nil {
		t.Fatal(err)
	}

	got, err := s.GetScratchpadItem(ctx, "grp1")
	if err != nil {
		t.Fatal(err)
	}
	if got.Annotations != "canonical group note" {
		t.Errorf("get: annotations = %q, want group_notes value", got.Annotations)
	}

	items, err := s.ListScratchpadItems(ctx, "sp1")
	if err != nil {
		t.Fatal(err)
	}
	byID := map[string]*domain.ScratchpadItem{}
	for _, it := range items {
		byID[it.ID] = it
	}
	if byID["grp1"].Annotations != "canonical group note" {
		t.Errorf("list: group annotations = %q, want group_notes value", byID["grp1"].Annotations)
	}
	if byID["txt1"].Annotations != "" {
		t.Errorf("list: text annotations = %q, want empty (blob retired)", byID["txt1"].Annotations)
	}
}
