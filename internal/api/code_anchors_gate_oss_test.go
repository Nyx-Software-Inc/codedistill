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

// The community build must refuse to MUTATE a code anchor. Creation was always
// gated on all ten POST routes, but the flat PATCH and DELETE shipped ungated,
// so two calls got a free user the paid authoring capability: GET an item's
// anchors (a free read, deliberately) to learn an id, then PATCH path, line
// range, label or provenance — only kind is immutable.
//
// Seeded through the store rather than the API because POST is itself 402 in
// this build; the point is to exercise the mutation routes on an anchor that
// legitimately exists, which is exactly the position a CE user is in after URL
// detection or the free baseline scan mints one for them.
//
// oss-tagged: under the commercial build baselineAllows() returns true for
// every feature, so requireFeature never refuses and there is nothing to assert.
func TestCodeAnchorMutationIsPaid_OSS(t *testing.T) {
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

	// Listing stays open — reads are never license-gated.
	doJSON(t, srv, "GET", "/api/v1/items/"+item.ID+"/code-anchors", nil, 200, nil)

	// Mutating is not.
	doJSON(t, srv, "PATCH", "/api/v1/code-anchors/"+anchor.ID,
		map[string]any{"path": "somewhere/else.go", "line_start": 10, "line_end": 20}, 402, nil)
	doJSON(t, srv, "DELETE", "/api/v1/code-anchors/"+anchor.ID, nil, 402, nil)

	// And the refusal must not have taken effect anyway.
	got, err := store.GetCodeAnchor(ctx, anchor.ID)
	if err != nil {
		t.Fatalf("anchor should still exist after a refused DELETE: %v", err)
	}
	if got.Path != "main.go" || got.LineStart != 1 {
		t.Errorf("refused PATCH still mutated the anchor: %+v", got)
	}
}
