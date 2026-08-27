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

// UC-5: GET /items/{id}/lineage merges the scratchpad item's events with
// its derived item's, oldest-first, and preserves source attribution.
func TestItemLineage_MergesItemAndDerived(t *testing.T) {
	srv, _, store := setupWithStore(t)
	ctx := context.Background()
	base := time.Date(2026, 6, 12, 9, 0, 0, 0, time.UTC)

	p := &domain.Project{ID: "p1", Name: "P", CreatedAt: base}
	if err := store.CreateProject(ctx, p); err != nil {
		t.Fatalf("project: %v", err)
	}
	pad := &domain.Scratchpad{ID: "s1", ProjectID: p.ID, Name: "pad", ClassificationMode: "full", Visibility: "project", CreatedAt: base}
	if err := store.CreateScratchpad(ctx, pad); err != nil {
		t.Fatalf("scratchpad: %v", err)
	}
	todo := &domain.TodoItem{ID: "t1", ProjectID: p.ID, Subject: "do", Priority: "none", Status: "incomplete", Origin: "agent-derived", CreatedAt: base}
	if err := store.CreateTodoItem(ctx, todo); err != nil {
		t.Fatalf("todo: %v", err)
	}
	item := &domain.ScratchpadItem{
		ID: "i1", ScratchpadID: pad.ID, Content: "do", ContentType: "text",
		ClassificationState: "classified", ProposedCategory: "todo", DerivedItemID: todo.ID, CreatedAt: base,
	}
	if err := store.CreateScratchpadItem(ctx, item); err != nil {
		t.Fatalf("item: %v", err)
	}

	// Interleave events across the two owners with out-of-insertion-order
	// timestamps to prove the endpoint sorts by time, not insertion.
	seed := []*domain.ItemEvent{
		{ID: "e3", OwnerType: "todo_item", OwnerID: todo.ID, Kind: "status-changed", Summary: "Status → complete", Source: "mcp", CreatedAt: base.Add(2 * time.Hour)},
		{ID: "e1", OwnerType: "scratchpad_item", OwnerID: item.ID, Kind: "created", Summary: "Item created", Source: "ui", CreatedAt: base},
		{ID: "e2", OwnerType: "scratchpad_item", OwnerID: item.ID, Kind: "classified", Summary: "Classified as todo", Source: "agent", CreatedAt: base.Add(1 * time.Hour)},
	}
	for _, e := range seed {
		if err := store.RecordItemEvent(ctx, e); err != nil {
			t.Fatalf("seed event %s: %v", e.ID, err)
		}
	}

	var got []domain.ItemEvent
	doJSON(t, srv, "GET", "/api/v1/items/"+item.ID+"/lineage", nil, 200, &got)
	if len(got) != 3 {
		t.Fatalf("got %d events, want 3", len(got))
	}
	wantOrder := []struct{ kind, source string }{
		{"created", "ui"},
		{"classified", "agent"},
		{"status-changed", "mcp"},
	}
	for i, w := range wantOrder {
		if got[i].Kind != w.kind || got[i].Source != w.source {
			t.Errorf("event[%d] = (%s,%s), want (%s,%s)", i, got[i].Kind, got[i].Source, w.kind, w.source)
		}
	}
}

// Creating an item through the HTTP API records a 'created' (source=ui)
// lineage event end-to-end.
func TestItemLineage_CreateRecordsEvent(t *testing.T) {
	srv, _, _ := setupWithStore(t)
	var p domain.Project
	doJSON(t, srv, "POST", "/api/v1/projects", map[string]string{"name": "p"}, 201, &p)
	var pad domain.Scratchpad
	doJSON(t, srv, "POST", "/api/v1/projects/"+p.ID+"/scratchpads", map[string]string{"name": "pad"}, 201, &pad)
	var item domain.ScratchpadItem
	doJSON(t, srv, "POST", "/api/v1/scratchpads/"+pad.ID+"/items",
		map[string]string{"content": "hello", "classification_override": "skip"}, 201, &item)

	var got []domain.ItemEvent
	doJSON(t, srv, "GET", "/api/v1/items/"+item.ID+"/lineage", nil, 200, &got)
	found := false
	for _, e := range got {
		if e.Kind == "created" && e.Source == "ui" {
			found = true
		}
	}
	if !found {
		t.Fatalf("no created/ui event in lineage: %+v", got)
	}
}
