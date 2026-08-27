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

func TestDrift_UnbuiltAndDiverged(t *testing.T) {
	srv, _, store := setupWithStore(t)
	ctx := context.Background()
	now := time.Now().UTC()

	if err := store.CreateProject(ctx, &domain.Project{ID: "p1", Name: "P", CreatedAt: now}); err != nil {
		t.Fatalf("project: %v", err)
	}
	if err := store.CreateScratchpad(ctx, &domain.Scratchpad{
		ID: "sp1", ProjectID: "p1", Name: "main", ClassificationMode: "full", CreatedAt: now,
	}); err != nil {
		t.Fatalf("scratchpad: %v", err)
	}
	if err := store.CreateScratchpadItem(ctx, &domain.ScratchpadItem{
		ID: "si1", ScratchpadID: "sp1", ContentType: "text", Content: "seed",
		ClassificationState: "classified", CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatalf("item: %v", err)
	}
	mkTodo := func(id, status string) {
		if err := store.CreateTodoItem(ctx, &domain.TodoItem{
			ID: id, ProjectID: "p1", SourceItemID: "si1", Subject: id,
			Priority: "none", Status: status, Origin: "agent-derived", CreatedAt: now,
		}); err != nil {
			t.Fatalf("todo %s: %v", id, err)
		}
	}
	// done, no code → unbuilt.
	mkTodo("t1", "complete")
	// open, no code → normal backlog, NOT drift.
	mkTodo("t2", "incomplete")
	// done, has code but a failed criterion → diverged (not unbuilt).
	mkTodo("t3", "complete")
	if err := store.CreateCodeAnchor(ctx, &domain.CodeAnchor{
		ID: "a1", OwnerType: "todo_item", OwnerID: "t3", Kind: "commit",
		Revision: "abc1234", Provenance: "agent-suggested", CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatalf("anchor: %v", err)
	}
	if err := store.CreateAcceptanceCriterion(ctx, &domain.AcceptanceCriterion{
		ID: "c1", OwnerType: "todo_item", OwnerID: "t3", Text: "must hold",
		State: "failed", VerificationKind: "test", Provenance: "user-authored",
		CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatalf("criterion: %v", err)
	}

	var resp driftResp
	doJSON(t, srv, "GET", "/api/v1/projects/p1/drift", nil, 200, &resp)

	if len(resp.Unbuilt) != 1 || resp.Unbuilt[0].ID != "t1" {
		t.Errorf("unbuilt = %+v, want [t1]", resp.Unbuilt)
	}
	if len(resp.Diverged) != 1 || resp.Diverged[0].ID != "t3" {
		t.Errorf("diverged = %+v, want [t3]", resp.Diverged)
	}
	// No repo configured → no untraced detection.
	if len(resp.Untraced) != 0 || resp.RepoConfigured {
		t.Errorf("untraced should be empty without a repo: %+v", resp)
	}
}
