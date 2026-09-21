//go:build oss

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

// Anchoring is FREE, and the community build must be able to do it end to end.
//
// This test previously asserted the opposite — that PATCH and DELETE returned
// 402 in the CE (item 6 closed a hole where creation was gated but the flat
// PATCH/DELETE were not). That gate was removed deliberately: creating, editing
// and deleting an anchor is Community, and what stays paid is having anchors
// made FOR you — agent auto-anchoring and the semantic code index behind it.
//
// The inversion is the point of keeping this test rather than deleting it: it
// now guards the decision in the direction it actually runs. Charging for the
// record was also incoherent, since url-detected anchors already appeared in
// the CE with no way to author one deliberately.
//
// Seeded through the store rather than the API because POST is itself 402 in
// this build; the point is to exercise the mutation routes on an anchor that
// legitimately exists, which is exactly the position a CE user is in after URL
// detection or the free baseline scan mints one for them.
//
// oss-tagged: under the commercial build baselineAllows() returns true for
// every feature, so requireFeature never refuses and there is nothing to assert.
func TestCodeAnchorAuthoringIsFree_OSS(t *testing.T) {
	srv, _, store := setupWithStore(t)
	ctx := context.Background()
	now := time.Now().UTC()

	var p domain.Project
	doJSON(t, srv, "POST", "/api/v1/projects", map[string]string{"name": "p"}, 201, &p)
	var sp domain.Scratchpad
	doJSON(t, srv, "POST", "/api/v1/projects/"+p.ID+"/scratchpads",
		map[string]string{"name": "extra"}, 201, &sp)
	var item domain.ScratchpadItem
	doJSON(t, srv, "POST", "/api/v1/scratchpads/"+sp.ID+"/items",
		map[string]string{"content": "placeholder"}, 201, &item)

	anchor := &domain.CodeAnchor{
		ID: "a-gate", OwnerType: "scratchpad_item", OwnerID: item.ID,
		Kind: "file", Path: "main.go", LineStart: 1, LineEnd: 2,
		Provenance: "url-detected", CreatedAt: now, UpdatedAt: now,
	}
	if err := store.CreateCodeAnchor(ctx, anchor); err != nil {
		t.Fatalf("seed anchor: %v", err)
	}

	// Listing was always open — reads are never license-gated.
	doJSON(t, srv, "GET", "/api/v1/items/"+item.ID+"/code-anchors", nil, 200, nil)

	// Creating one is now open too.
	doJSON(t, srv, "POST", "/api/v1/items/"+item.ID+"/code-anchors",
		map[string]any{"kind": "file", "path": "internal/new.go", "line_start": 3, "line_end": 9}, 201, nil)

	// So is editing.
	doJSON(t, srv, "PATCH", "/api/v1/code-anchors/"+anchor.ID,
		map[string]any{"path": "somewhere/else.go", "line_start": 10, "line_end": 20}, 200, nil)

	// And the edit must actually have taken effect.
	got, err := store.GetCodeAnchor(ctx, anchor.ID)
	if err != nil {
		t.Fatalf("anchor should still exist after an accepted PATCH: %v", err)
	}
	if got.Path != "somewhere/else.go" || got.LineStart != 10 {
		t.Errorf("PATCH was accepted but the anchor did not change: %+v", got)
	}
}
