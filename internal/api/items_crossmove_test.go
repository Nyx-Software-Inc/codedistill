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

// UC-1: moving a scratchpad item to a scratchpad in ANOTHER project is
// allowed; it re-homes the item's derived work item to the destination
// project, and is BLOCKED when the item or its derived item carries code
// anchors (which reference the source project's codebase).
func TestMoveItem_CrossProject(t *testing.T) {
	srv, _, store := setupWithStore(t)
	ctx := context.Background()
	now := time.Now().UTC()

	pa := &domain.Project{ID: "pa", Name: "Alpha", CreatedAt: now}
	pb := &domain.Project{ID: "pb", Name: "Beta", CreatedAt: now}
	for _, p := range []*domain.Project{pa, pb} {
		if err := store.CreateProject(ctx, p); err != nil {
			t.Fatalf("project %s: %v", p.ID, err)
		}
	}
	padA := &domain.Scratchpad{ID: "spa", ProjectID: pa.ID, Name: "A", ClassificationMode: "full", Visibility: "project", CreatedAt: now}
	padB := &domain.Scratchpad{ID: "spb", ProjectID: pb.ID, Name: "B", ClassificationMode: "full", Visibility: "project", CreatedAt: now}
	for _, sp := range []*domain.Scratchpad{padA, padB} {
		if err := store.CreateScratchpad(ctx, sp); err != nil {
			t.Fatalf("scratchpad %s: %v", sp.ID, err)
		}
	}
	todo := &domain.TodoItem{
		ID: "t1", ProjectID: pa.ID, Subject: "do it",
		Priority: "none", Status: "incomplete", Origin: "agent-derived", CreatedAt: now,
	}
	if err := store.CreateTodoItem(ctx, todo); err != nil {
		t.Fatalf("todo: %v", err)
	}
	// Destination project ALREADY has a todo — it gets number 1 there, so a
	// naive re-home that kept t1's number (also 1 in pa) would violate
	// UNIQUE(project_id, number). The fix renumbers on move.
	existing := &domain.TodoItem{
		ID: "t-pb", ProjectID: pb.ID, Subject: "pre-existing in B",
		Priority: "none", Status: "incomplete", Origin: "manual", CreatedAt: now,
	}
	if err := store.CreateTodoItem(ctx, existing); err != nil {
		t.Fatalf("existing todo: %v", err)
	}
	item := &domain.ScratchpadItem{
		ID: "i1", ScratchpadID: padA.ID, Content: "do it", ContentType: "text",
		ClassificationState: "classified", ProposedCategory: "todo",
		DerivedItemID: todo.ID, CreatedAt: now,
	}
	if err := store.CreateScratchpadItem(ctx, item); err != nil {
		t.Fatalf("item: %v", err)
	}

	move := func(dstPad string, wantStatus int) {
		t.Helper()
		doJSON(t, srv, "POST", "/api/v1/items/"+item.ID+"/move",
			map[string]string{"scratchpad_id": dstPad}, wantStatus, nil)
	}

	// 1) Clean cross-project move INTO a project that already has a todo:
	// the item lands in padB and the derived todo is re-homed to project B
	// with a FRESH number (not colliding with the pre-existing #1).
	move(padB.ID, 200)
	if got, _ := store.GetScratchpadItem(ctx, item.ID); got.ScratchpadID != padB.ID {
		t.Fatalf("item scratchpad = %s, want %s", got.ScratchpadID, padB.ID)
	}
	if got, err := store.GetTodoItem(ctx, todo.ID); err != nil || got.ProjectID != pb.ID {
		t.Fatalf("derived todo not re-homed: project=%v err=%v", got, err)
	} else if got.Number == existing.Number {
		t.Fatalf("re-homed todo kept colliding number %d; want a fresh one", got.Number)
	}
	// The pre-existing destination todo is untouched.
	if got, err := store.GetTodoItem(ctx, existing.ID); err != nil || got.Number != existing.Number {
		t.Fatalf("pre-existing todo number changed: %v (want %d)", got, existing.Number)
	}

	// 2a) An anchor on the ITEM blocks the cross-project move (409).
	itemAnchor := &domain.CodeAnchor{
		ID: "a-item", OwnerType: "scratchpad_item", OwnerID: item.ID,
		Kind: "file", Path: "main.go", Provenance: "user-set", CreatedAt: now, UpdatedAt: now,
	}
	if err := store.CreateCodeAnchor(ctx, itemAnchor); err != nil {
		t.Fatalf("item anchor: %v", err)
	}
	move(padA.ID, 409)
	if got, _ := store.GetScratchpadItem(ctx, item.ID); got.ScratchpadID != padB.ID {
		t.Errorf("item moved despite item anchor: %s", got.ScratchpadID)
	}
	if err := store.DeleteCodeAnchor(ctx, itemAnchor.ID); err != nil {
		t.Fatalf("del item anchor: %v", err)
	}

	// 2b) An anchor on the DERIVED todo also blocks (409).
	todoAnchor := &domain.CodeAnchor{
		ID: "a-todo", OwnerType: "todo_item", OwnerID: todo.ID,
		Kind: "file", Path: "todo.go", Provenance: "user-set", CreatedAt: now, UpdatedAt: now,
	}
	if err := store.CreateCodeAnchor(ctx, todoAnchor); err != nil {
		t.Fatalf("todo anchor: %v", err)
	}
	move(padA.ID, 409)
	if err := store.DeleteCodeAnchor(ctx, todoAnchor.ID); err != nil {
		t.Fatalf("del todo anchor: %v", err)
	}

	// 2c) With anchors cleared, the move back to project A succeeds and the
	// derived todo follows.
	move(padA.ID, 200)
	if got, err := store.GetTodoItem(ctx, todo.ID); err != nil || got.ProjectID != pa.ID {
		t.Fatalf("derived todo not re-homed back: project=%v err=%v", got, err)
	}
}

// Same-project move must remain unaffected by the cross-project logic:
// the derived item keeps its project and no anchor check applies.
func TestMoveItem_SameProjectUnaffected(t *testing.T) {
	srv, _, store := setupWithStore(t)
	ctx := context.Background()
	now := time.Now().UTC()

	p := &domain.Project{ID: "p1", Name: "Solo", CreatedAt: now}
	if err := store.CreateProject(ctx, p); err != nil {
		t.Fatalf("project: %v", err)
	}
	pad1 := &domain.Scratchpad{ID: "s1", ProjectID: p.ID, Name: "one", ClassificationMode: "full", Visibility: "project", CreatedAt: now}
	pad2 := &domain.Scratchpad{ID: "s2", ProjectID: p.ID, Name: "two", ClassificationMode: "full", Visibility: "project", CreatedAt: now}
	for _, sp := range []*domain.Scratchpad{pad1, pad2} {
		if err := store.CreateScratchpad(ctx, sp); err != nil {
			t.Fatalf("scratchpad: %v", err)
		}
	}
	item := &domain.ScratchpadItem{
		ID: "i1", ScratchpadID: pad1.ID, Content: "x", ContentType: "text",
		ClassificationState: "unprocessed", CreatedAt: now,
	}
	if err := store.CreateScratchpadItem(ctx, item); err != nil {
		t.Fatalf("item: %v", err)
	}
	// Even with an anchor, a same-project move is fine (no codebase change).
	anc := &domain.CodeAnchor{
		ID: "a1", OwnerType: "scratchpad_item", OwnerID: item.ID,
		Kind: "file", Path: "main.go", Provenance: "user-set", CreatedAt: now, UpdatedAt: now,
	}
	if err := store.CreateCodeAnchor(ctx, anc); err != nil {
		t.Fatalf("anchor: %v", err)
	}
	doJSON(t, srv, "POST", "/api/v1/items/"+item.ID+"/move",
		map[string]string{"scratchpad_id": pad2.ID}, 200, nil)
	if got, _ := store.GetScratchpadItem(ctx, item.ID); got.ScratchpadID != pad2.ID {
		t.Errorf("same-project move failed: %s", got.ScratchpadID)
	}
}
