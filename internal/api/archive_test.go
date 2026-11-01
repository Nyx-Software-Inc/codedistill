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
	"testing"

	"codedistill/internal/domain"
)

func TestArchive_HidesFromCanvasButRetains(t *testing.T) {
	srv, _ := setup(t)

	var p domain.Project
	doJSON(t, srv, "POST", "/api/v1/projects", map[string]string{"name": "p"}, 201, &p)
	var sp domain.Scratchpad
	doJSON(t, srv, "POST", "/api/v1/projects/"+p.ID+"/scratchpads", map[string]string{"name": "x"}, 201, &sp)
	var item domain.ScratchpadItem
	doJSON(t, srv, "POST", "/api/v1/scratchpads/"+sp.ID+"/items", map[string]string{"content": "keep me"}, 201, &item)

	// Archive it.
	doJSON(t, srv, "POST", "/api/v1/items/"+item.ID+"/archive", nil, 204, nil)

	// Default list excludes it.
	var live []domain.ScratchpadItem
	doJSON(t, srv, "GET", "/api/v1/scratchpads/"+sp.ID+"/items", nil, 200, &live)
	if len(live) != 0 {
		t.Errorf("archived item should be off the default canvas, got %d", len(live))
	}

	// include_archived returns it, with archived_at set.
	var all []domain.ScratchpadItem
	doJSON(t, srv, "GET", "/api/v1/scratchpads/"+sp.ID+"/items?include_archived=true", nil, 200, &all)
	if len(all) != 1 || all[0].ArchivedAt == nil {
		t.Fatalf("include_archived should return the item with archived_at set, got %+v", all)
	}

	// Un-archive restores it to the default canvas.
	doJSON(t, srv, "POST", "/api/v1/items/"+item.ID+"/unarchive", nil, 204, nil)
	doJSON(t, srv, "GET", "/api/v1/scratchpads/"+sp.ID+"/items", nil, 200, &live)
	if len(live) != 1 || live[0].ArchivedAt != nil {
		t.Errorf("un-archive should restore the item, got %+v", live)
	}
}

// Hiding compacts the canvas, so a card captured after the hide takes the
// hidden card's spot. Unhiding must not put the card back on top of it — the
// same rule unarchive follows.
func TestUnhide_MovesToNextFreeRow(t *testing.T) {
	srv, _ := setup(t)

	var p domain.Project
	doJSON(t, srv, "POST", "/api/v1/projects", map[string]string{"name": "p"}, 201, &p)
	var sp domain.Scratchpad
	doJSON(t, srv, "POST", "/api/v1/projects/"+p.ID+"/scratchpads", map[string]string{"name": "x"}, 201, &sp)
	var a, b domain.ScratchpadItem
	doJSON(t, srv, "POST", "/api/v1/scratchpads/"+sp.ID+"/items", map[string]string{"content": "first"}, 201, &a)
	doJSON(t, srv, "PATCH", "/api/v1/items/"+a.ID, map[string]any{"hidden": true}, 200, nil)
	doJSON(t, srv, "POST", "/api/v1/scratchpads/"+sp.ID+"/items", map[string]string{"content": "second"}, 201, &b)
	if b.GridRow != a.GridRow || b.GridCol != a.GridCol {
		t.Fatalf("precondition: the new card should take the hidden card's spot (a %d,%d; b %d,%d)",
			a.GridCol, a.GridRow, b.GridCol, b.GridRow)
	}

	var got domain.ScratchpadItem
	doJSON(t, srv, "PATCH", "/api/v1/items/"+a.ID, map[string]any{"hidden": false}, 200, &got)
	if got.Hidden {
		t.Fatal("item still hidden")
	}
	if got.GridCol != 0 || got.GridRow != b.GridRow+b.GridH {
		t.Errorf("unhidden card at %d,%d; want 0,%d (the row below the card now in its old spot)",
			got.GridCol, got.GridRow, b.GridRow+b.GridH)
	}

	// An explicit position in the same request is honoured.
	doJSON(t, srv, "PATCH", "/api/v1/items/"+a.ID, map[string]any{"hidden": true}, 200, nil)
	doJSON(t, srv, "PATCH", "/api/v1/items/"+a.ID, map[string]any{"hidden": false, "grid_col": 3, "grid_row": 7}, 200, &got)
	if got.GridCol != 3 || got.GridRow != 7 {
		t.Errorf("explicit position ignored: got %d,%d, want 3,7", got.GridCol, got.GridRow)
	}
}
