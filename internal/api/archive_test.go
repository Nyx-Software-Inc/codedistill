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
