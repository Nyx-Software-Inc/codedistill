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

// A parent delete that FAILS must not have already erased the item's code
// anchors. Before this was transactional the two statements ran on separate
// connections: the anchor DELETE committed, the parent DELETE then affected
// zero rows, and the caller got ErrNotFound — an error that reads as "nothing
// happened" while the item's Throughline provenance was already gone, with no
// way to notice.
//
// The trigger modelled here is the realistic one, not disk-full: two clients
// deleting the same item (the UI and an MCP complete_item, or two tabs). One
// wins the parent row; the loser's anchor deletion has already committed by the
// time it discovers that. Simulated by removing the parent row underneath the
// call, which is exactly the state the losing caller observes.
func TestDeleteOwner_FailedParentDeleteKeepsAnchors(t *testing.T) {
	ctx := context.Background()
	now := time.Now().UTC()

	cases := []struct {
		name      string
		ownerType string
		table     string
		seed      func(t *testing.T, s *Store)
		del       func(s *Store) error
	}{
		{"todo", "todo_item", "todo_items",
			func(t *testing.T, s *Store) {
				if err := s.CreateTodoItem(ctx, &domain.TodoItem{ID: "o1", ProjectID: "p1", SourceItemID: "si1",
					Subject: "t", Priority: "none", Status: "incomplete", Origin: "agent-derived", CreatedAt: now}); err != nil {
					t.Fatal(err)
				}
			},
			func(s *Store) error { return s.DeleteTodoItem(ctx, "o1") }},

		{"bug", "bug_item", "bug_items",
			func(t *testing.T, s *Store) {
				if err := s.CreateBugItem(ctx, &domain.BugItem{ID: "o1", ProjectID: "p1", SourceItemID: "si1",
					Subject: "b", Severity: "minor", Status: "open", Origin: "agent-derived", CreatedAt: now}); err != nil {
					t.Fatal(err)
				}
			},
			func(s *Store) error { return s.DeleteBugItem(ctx, "o1") }},

		{"knowledge", "knowledge_entry", "knowledge_entries",
			func(t *testing.T, s *Store) {
				if err := s.CreateKnowledgeEntry(ctx, &domain.KnowledgeEntry{ID: "o1", ProjectID: "p1", SourceItemID: "si1",
					Title: "k", Content: "k", CreatedAt: now}); err != nil {
					t.Fatal(err)
				}
			},
			func(s *Store) error { return s.DeleteKnowledgeEntry(ctx, "o1") }},

		{"use case", "use_case_item", "use_case_items",
			func(t *testing.T, s *Store) {
				if err := s.CreateUseCaseItem(ctx, &domain.UseCaseItem{ID: "o1", ProjectID: "p1", SourceItemID: "si1",
					Subject: "u", Description: "u", Status: "open", Origin: "agent-derived", CreatedAt: now, UpdatedAt: now}); err != nil {
					t.Fatal(err)
				}
			},
			func(s *Store) error { return s.DeleteUseCaseItem(ctx, "o1") }},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := newTestStore(t)
			must := func(err error) {
				t.Helper()
				if err != nil {
					t.Fatal(err)
				}
			}
			must(s.CreateProject(ctx, &domain.Project{ID: "p1", Name: "P", CreatedAt: now}))
			must(s.CreateScratchpad(ctx, &domain.Scratchpad{ID: "sp1", ProjectID: "p1", Name: "m",
				ClassificationMode: "full", CreatedAt: now}))
			must(s.CreateScratchpadItem(ctx, &domain.ScratchpadItem{ID: "si1", ScratchpadID: "sp1",
				Content: "src", ContentType: "text", ClassificationState: "classified", CreatedAt: now}))
			tc.seed(t, s)

			must(s.CreateCodeAnchor(ctx, &domain.CodeAnchor{
				ID: "anc1", OwnerType: tc.ownerType, OwnerID: "o1",
				Kind: "file", Path: "internal/api/server.go", LineStart: 1, LineEnd: 2,
				Provenance: "user-set", CreatedAt: now, UpdatedAt: now,
			}))

			// The concurrent winner removes the parent row.
			if _, err := s.DB.ExecContext(ctx, `DELETE FROM `+tc.table+` WHERE id = ?`, "o1"); err != nil {
				t.Fatalf("simulate concurrent delete: %v", err)
			}

			// The loser now runs the full delete and must fail...
			if err := tc.del(s); !errors.Is(err, storage.ErrNotFound) {
				t.Fatalf("delete err = %v, want ErrNotFound", err)
			}
			// ...without having destroyed the anchor on its way there.
			if _, err := s.GetCodeAnchor(ctx, "anc1"); err != nil {
				t.Errorf("anchor was deleted by a delete that FAILED: %v — "+
					"the two statements are not in one transaction", err)
			}
		})
	}
}
