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

package mcp

import (
	"context"
	"strings"
	"testing"

	"codedistill/internal/domain"
)

// delete_scratchpad_item is the one agent-callable tool that can destroy a
// capture, so each guard gets its own test. The shape of the whole thing is
// "archive unless told otherwise".

// plainItem adds an UNCLASSIFIED capture — nothing derived from it, so deletion
// strips no provenance.
func (fx *fixture) plainItem(t *testing.T, id string) {
	t.Helper()
	if err := fx.store.CreateScratchpadItem(context.Background(), &domain.ScratchpadItem{
		ID: id, ScratchpadID: fx.padID, Content: "a passing thought",
		ContentType: "text", ClassificationState: "pending-review", CreatedAt: fx.now,
	}); err != nil {
		t.Fatalf("seed %s: %v", id, err)
	}
}

func TestDeleteScratchpadItem_ArchivesByDefault(t *testing.T) {
	fx := newFixture(t)
	fx.plainItem(t, "gone1")

	// No `archive` argument at all: the default must be the reversible one.
	res := fx.callTool("delete_scratchpad_item", map[string]any{
		"item_id":         "gone1",
		"project_name":    fx.projectName,
		"scratchpad_name": fx.padName,
	})
	if res.IsError {
		t.Fatalf("archive failed: %s", firstText(t, res))
	}
	if txt := firstText(t, res); !strings.Contains(txt, "archived") {
		t.Errorf("result should say what happened, got: %s", txt)
	}

	// The row survives, off the canvas.
	it, err := fx.store.GetScratchpadItem(context.Background(), "gone1")
	if err != nil {
		t.Fatalf("archived item should still exist: %v", err)
	}
	if it.ArchivedAt == nil {
		t.Error("archived_at not set — the default did not archive")
	}
}

func TestDeleteScratchpadItem_DeletesWhenAsked(t *testing.T) {
	fx := newFixture(t)
	fx.plainItem(t, "gone2")

	res := fx.callTool("delete_scratchpad_item", map[string]any{
		"item_id":         "gone2",
		"project_name":    fx.projectName,
		"scratchpad_name": fx.padName,
		"archive":         false,
	})
	if res.IsError {
		t.Fatalf("delete failed: %s", firstText(t, res))
	}
	if _, err := fx.store.GetScratchpadItem(context.Background(), "gone2"); err == nil {
		t.Error("item still exists after an explicit delete")
	}
}

func TestDeleteScratchpadItem_RefusesToDeleteAClassifiedItem(t *testing.T) {
	fx := newFixture(t)
	// fx.itemID is the fixture's classified capture — it has a derived work item.
	res := fx.callTool("delete_scratchpad_item", map[string]any{
		"item_id":         fx.itemID,
		"project_name":    fx.projectName,
		"scratchpad_name": fx.padName,
		"archive":         false,
	})
	if !res.IsError {
		t.Fatal("deleting a classified item was allowed — its derived work item would lose its provenance silently (source_item_id is ON DELETE SET NULL)")
	}
	txt := firstText(t, res)
	// The refusal has to say what to do instead, or an agent will just retry.
	if !strings.Contains(strings.ToLower(txt), "archive") {
		t.Errorf("refusal should point at archiving, got: %s", txt)
	}
	if _, err := fx.store.GetScratchpadItem(context.Background(), fx.itemID); err != nil {
		t.Errorf("item should be untouched after a refusal: %v", err)
	}
}

func TestDeleteScratchpadItem_ArchivingAClassifiedItemIsFine(t *testing.T) {
	fx := newFixture(t)
	// Archiving severs nothing, so the provenance guard must NOT apply here —
	// otherwise the recommended alternative would itself be blocked.
	res := fx.callTool("delete_scratchpad_item", map[string]any{
		"item_id":         fx.itemID,
		"project_name":    fx.projectName,
		"scratchpad_name": fx.padName,
	})
	if res.IsError {
		t.Fatalf("archiving a classified item should be allowed: %s", firstText(t, res))
	}
	it, err := fx.store.GetScratchpadItem(context.Background(), fx.itemID)
	if err != nil {
		t.Fatal(err)
	}
	if it.ArchivedAt == nil {
		t.Error("not archived")
	}
	// The invariant that matters is the AUTHORITATIVE link — the work item still
	// naming this capture as its source. Asserting the item's own
	// derived_item_id would test the fixture rather than the behaviour: that
	// field is a denormalised pointer the classifier sets, and nothing here
	// writes it.
	todo, err := fx.store.GetTodoItem(context.Background(), fx.todoID)
	if err != nil {
		t.Fatal(err)
	}
	if todo.SourceItemID != fx.itemID {
		t.Errorf("archiving severed provenance: source_item_id = %q, want %q",
			todo.SourceItemID, fx.itemID)
	}
}

func TestDeleteScratchpadItem_RefusesAWrongScratchpad(t *testing.T) {
	fx := newFixture(t)
	fx.plainItem(t, "gone3")

	// Item ids are globally unique, so project/scratchpad are not needed to FIND
	// the item — they are a guard. A stale or hallucinated id paired with a
	// confidently-wrong location must fail rather than delete something real.
	if err := fx.store.CreateScratchpad(context.Background(), &domain.Scratchpad{
		ID: "sp2", ProjectID: fx.projectID, Name: "Elsewhere",
		ClassificationMode: "full", CreatedAt: fx.now,
	}); err != nil {
		t.Fatal(err)
	}
	res := fx.callTool("delete_scratchpad_item", map[string]any{
		"item_id":         "gone3",
		"project_name":    fx.projectName,
		"scratchpad_name": "Elsewhere",
		"archive":         false,
	})
	if !res.IsError {
		t.Fatal("acted on an item that was not in the named scratchpad")
	}
	if _, err := fx.store.GetScratchpadItem(context.Background(), "gone3"); err != nil {
		t.Errorf("item should be untouched: %v", err)
	}
}

func TestDeleteScratchpadItem_ArchivingTwiceIsNotAnError(t *testing.T) {
	fx := newFixture(t)
	fx.plainItem(t, "gone4")
	args := map[string]any{
		"item_id":         "gone4",
		"project_name":    fx.projectName,
		"scratchpad_name": fx.padName,
	}
	fx.callTool("delete_scratchpad_item", args)
	// Agents retry. A second archive reporting failure would read as "the first
	// one did not work" and invite an escalation to delete.
	res := fx.callTool("delete_scratchpad_item", args)
	if res.IsError {
		t.Fatalf("re-archiving should be idempotent, got: %s", firstText(t, res))
	}
}
