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

// Regression: ListKnowledgeEntriesByScratchpad must select the same columns
// scanKB reads (it once omitted `kind`, causing a 16-vs-17 Scan error on the
// /scratchpads/{sid}/kb route).
func TestListKnowledgeEntriesByScratchpad_ScansAllColumns(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	now := time.Now().UTC()

	if err := s.CreateProject(ctx, &domain.Project{ID: "p1", Name: "P", CreatedAt: now}); err != nil {
		t.Fatal(err)
	}
	if err := s.CreateScratchpad(ctx, &domain.Scratchpad{ID: "sp1", ProjectID: "p1", Name: "m", ClassificationMode: "full", CreatedAt: now}); err != nil {
		t.Fatal(err)
	}
	if err := s.CreateScratchpadItem(ctx, &domain.ScratchpadItem{ID: "si1", ScratchpadID: "sp1", ContentType: "text", Content: "x", ClassificationState: "classified", CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	if err := s.CreateKnowledgeEntry(ctx, &domain.KnowledgeEntry{
		ID: "kb1", ProjectID: "p1", SourceItemID: "si1", Title: "T", Content: "C",
		Kind: "decision", Status: "active", CreatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}

	got, err := s.ListKnowledgeEntriesByScratchpad(ctx, "sp1")
	if err != nil {
		t.Fatalf("ListKnowledgeEntriesByScratchpad: %v", err)
	}
	if len(got) != 1 || got[0].Kind != "decision" {
		t.Fatalf("expected 1 entry with kind=decision, got %+v", got)
	}
}
