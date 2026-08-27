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

func TestExtractHashtags(t *testing.T) {
	got := normalizeTags(extractHashtags("fix the #Backend bug; see #api-v2, a #heading\n# not-a-tag and #1 issue"))
	want := []string{"backend", "api-v2", "heading"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("got %v, want %v", got, want)
		}
	}
}

// TestHashtagExtractionEndToEnd: create an item with inline #tags → tags
// populated, content untouched; PATCH content with a new #tag → tag
// added (additive), existing tags kept.
func TestHashtagExtractionEndToEnd(t *testing.T) {
	srv, _, store := setupWithStore(t)
	ctx := context.Background()
	if err := store.CreateProject(ctx, &domain.Project{ID: "p1", Name: "P", CreatedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	if err := store.CreateScratchpad(ctx, &domain.Scratchpad{
		ID: "sp1", ProjectID: "p1", Name: "main", ClassificationMode: "off", CreatedAt: time.Now(),
	}); err != nil {
		t.Fatal(err)
	}

	var created domain.ScratchpadItem
	doJSON(t, srv, "POST", "/api/v1/scratchpads/sp1/items",
		map[string]string{"content": "ship the #login fix and #api-v2 work"}, 201, &created)
	if created.Content != "ship the #login fix and #api-v2 work" {
		t.Errorf("content was modified: %q", created.Content)
	}
	if !hasTag(created.Tags, "login") || !hasTag(created.Tags, "api-v2") {
		t.Errorf("tags not extracted on create: %v", created.Tags)
	}

	var updated domain.ScratchpadItem
	doJSON(t, srv, "PATCH", "/api/v1/items/"+created.ID,
		map[string]any{"content": "ship the #login fix, now also #urgent"}, 200, &updated)
	if !hasTag(updated.Tags, "login") || !hasTag(updated.Tags, "urgent") {
		t.Errorf("content edit didn't add new hashtag additively: %v", updated.Tags)
	}
}

func hasTag(tags []string, t string) bool {
	for _, x := range tags {
		if x == t {
			return true
		}
	}
	return false
}
