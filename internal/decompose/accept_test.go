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

package decompose

import (
	"context"
	"testing"
	"time"

	"codedistill/internal/domain"
)

// A card is only a card because it has geometry. Accepting a proposal once
// produced items with none — forty-nine of them at zero-by-zero on the same
// square, invisible but for a resize handle in the corner.
func TestAcceptedItemsAreBornWithASizeAndAPlace(t *testing.T) {
	st := &sizingStore{}
	for i, subject := range []string{"Start timer", "Pause a running timer", "Reset it"} {
		p := &domain.DecomposeProposal{
			ID: "p", ProjectID: "proj", Kind: "todo", Subject: subject,
			Body:   "The document said so, at some length, which is what decides the height.",
			Status: domain.ProposalPending, Lines: [][2]int{{i + 1, i + 1}},
		}
		if _, err := Accept(context.Background(), st, p, "pad", "rich", func() string { return "id" }, time.Now()); err != nil {
			t.Fatalf("accept: %v", err)
		}
	}
	if len(st.created) != 3 {
		t.Fatalf("created %d base items, want 3", len(st.created))
	}
	seen := map[int]bool{}
	for _, it := range st.created {
		if it.GridW <= 0 || it.GridH <= 0 {
			t.Fatalf("%q was born %dx%d — an invisible card", it.Name, it.GridW, it.GridH)
		}
		if seen[it.GridRow] {
			t.Fatalf("%q landed on row %d, which is already taken", it.Name, it.GridRow)
		}
		seen[it.GridRow] = true
	}
}

// sizingStore records what Accept creates and hands out rows the way the real
// store does: each new item goes below the last.
type sizingStore struct {
	created []*domain.ScratchpadItem
}

func (s *sizingStore) CreateScratchpadItem(_ context.Context, i *domain.ScratchpadItem) error {
	c := *i
	s.created = append(s.created, &c)
	return nil
}
func (s *sizingStore) DeleteScratchpadItem(context.Context, string) error { return nil }
func (s *sizingStore) NextAvailableGridRow(context.Context, string) (int, error) {
	row := 0
	for _, it := range s.created {
		if end := it.GridRow + it.GridH; end > row {
			row = end
		}
	}
	return row, nil
}
func (s *sizingStore) CreateTodoItem(context.Context, *domain.TodoItem) error             { return nil }
func (s *sizingStore) CreateBugItem(context.Context, *domain.BugItem) error               { return nil }
func (s *sizingStore) CreateKnowledgeEntry(context.Context, *domain.KnowledgeEntry) error { return nil }
func (s *sizingStore) CreateUseCaseItem(context.Context, *domain.UseCaseItem) error       { return nil }
func (s *sizingStore) DecideProposal(context.Context, string, string, string, string, string, time.Time) error {
	return nil
}
