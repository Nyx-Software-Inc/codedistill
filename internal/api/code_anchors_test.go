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
	"strings"
	"testing"

	"codedistill/internal/domain"
)

// --- Manual anchor CRUD (kind=file) ---

// --- URL auto-detect ---

func TestCodeAnchorURLAutoDetect_GitHubBlobWithLineRange(t *testing.T) {
	srv, _ := setup(t)
	var p domain.Project
	doJSON(t, srv, "POST", "/api/v1/projects", map[string]string{"name": "p"}, 201, &p)
	var sp domain.Scratchpad
	doJSON(t, srv, "POST", "/api/v1/projects/"+p.ID+"/scratchpads",
		map[string]string{"name": "extra"}, 201, &sp)

	content := "See https://github.com/anthropics/claude-code/blob/a1b2c3d4/internal/api/foo.go#L42-L80 for context"
	var item domain.ScratchpadItem
	doJSON(t, srv, "POST", "/api/v1/scratchpads/"+sp.ID+"/items",
		map[string]string{"content": content}, 201, &item)

	var anchors []*domain.CodeAnchor
	doJSON(t, srv, "GET", "/api/v1/items/"+item.ID+"/code-anchors", nil, 200, &anchors)
	if len(anchors) != 1 {
		t.Fatalf("expected 1 auto-detected anchor, got %d: %+v", len(anchors), anchors)
	}
	a := anchors[0]
	if a.Kind != "file" || a.Path != "internal/api/foo.go" {
		t.Errorf("anchor fields: %+v", a)
	}
	if a.Revision != "a1b2c3d4" || a.LineStart != 42 || a.LineEnd != 80 {
		t.Errorf("anchor revision/lines: %+v", a)
	}
	if a.Provenance != "url-detected" {
		t.Errorf("provenance = %q, want url-detected", a.Provenance)
	}
}

func TestCodeAnchorURLAutoDetect_GitHubPR(t *testing.T) {
	srv, _ := setup(t)
	var p domain.Project
	doJSON(t, srv, "POST", "/api/v1/projects", map[string]string{"name": "p"}, 201, &p)
	var sp domain.Scratchpad
	doJSON(t, srv, "POST", "/api/v1/projects/"+p.ID+"/scratchpads",
		map[string]string{"name": "extra"}, 201, &sp)

	content := "discuss at https://github.com/foo/bar/pull/123 please"
	var item domain.ScratchpadItem
	doJSON(t, srv, "POST", "/api/v1/scratchpads/"+sp.ID+"/items",
		map[string]string{"content": content}, 201, &item)

	var anchors []*domain.CodeAnchor
	doJSON(t, srv, "GET", "/api/v1/items/"+item.ID+"/code-anchors", nil, 200, &anchors)
	if len(anchors) != 1 {
		t.Fatalf("expected 1 PR anchor, got %d: %+v", len(anchors), anchors)
	}
	if anchors[0].Kind != "pr" || !strings.Contains(anchors[0].URL, "/pull/123") {
		t.Errorf("anchor fields: %+v", anchors[0])
	}
}

func TestCodeAnchorURLAutoDetect_RefreshOnContentUpdate(t *testing.T) {
	srv, _, store := setupWithStore(t)
	var p domain.Project
	doJSON(t, srv, "POST", "/api/v1/projects", map[string]string{"name": "p"}, 201, &p)
	var sp domain.Scratchpad
	doJSON(t, srv, "POST", "/api/v1/projects/"+p.ID+"/scratchpads",
		map[string]string{"name": "extra"}, 201, &sp)

	var item domain.ScratchpadItem
	doJSON(t, srv, "POST", "/api/v1/scratchpads/"+sp.ID+"/items",
		map[string]string{"content": "first https://github.com/foo/bar/pull/1"}, 201, &item)

	// Add a user-set anchor that must survive the URL re-detection pass. Seeded
	// through the store, not the paid POST route: this test is about the FREE
	// url-detection pass, and the anchor is only fixture — so it stays in the
	// community suite instead of being tagged out with the paid coverage.
	seedCodeAnchor(t, store, "scratchpad_item", item.ID, domain.CodeAnchor{
		Kind: "commit", Revision: "abcd1234"})

	// Replace content with a different URL; the old url-detected PR anchor
	// should disappear and the new one should appear.
	var updated domain.ScratchpadItem
	doJSON(t, srv, "PATCH", "/api/v1/items/"+item.ID, map[string]any{
		"content": "now https://github.com/foo/bar/pull/2",
	}, 200, &updated)

	var anchors []*domain.CodeAnchor
	doJSON(t, srv, "GET", "/api/v1/items/"+item.ID+"/code-anchors", nil, 200, &anchors)

	kinds := map[string]int{}
	urls := map[string]bool{}
	for _, a := range anchors {
		kinds[a.Provenance]++
		if a.URL != "" {
			urls[a.URL] = true
		}
	}
	if kinds["user-set"] != 1 {
		t.Errorf("user-set anchor count = %d, want 1 (user anchor must survive)", kinds["user-set"])
	}
	if kinds["url-detected"] != 1 {
		t.Errorf("url-detected count = %d, want 1 (old URL removed, new URL added)", kinds["url-detected"])
	}
	if !urls["https://github.com/foo/bar/pull/2"] {
		t.Errorf("new URL not present in anchors: %+v", anchors)
	}
	if urls["https://github.com/foo/bar/pull/1"] {
		t.Errorf("old URL should have been removed: %+v", anchors)
	}
}

// --- Agent propagation ---

func TestCodeAnchor_PropagatesOnAgentDerive(t *testing.T) {
	srv, ag := setup(t)
	var p domain.Project
	doJSON(t, srv, "POST", "/api/v1/projects", map[string]string{"name": "p"}, 201, &p)
	var sp domain.Scratchpad
	doJSON(t, srv, "POST", "/api/v1/projects/"+p.ID+"/scratchpads",
		map[string]string{"name": "extra"}, 201, &sp)

	// Create item with an inline GitHub blob URL — auto-detected into a file anchor.
	content := "fix: https://github.com/foo/bar/blob/deadbee/src/main.go#L10-L20"
	var item domain.ScratchpadItem
	doJSON(t, srv, "POST", "/api/v1/scratchpads/"+sp.ID+"/items",
		map[string]string{"content": content, "classification_override": "bug"}, 201, &item)

	// Override=bug short-circuits classification; drain the agent to process it.
	ag.Stop()

	var got domain.ScratchpadItem
	doJSON(t, srv, "GET", "/api/v1/items/"+item.ID, nil, 200, &got)
	if got.DerivedItemID == "" {
		t.Fatalf("no derived bug created; state=%q", got.ClassificationState)
	}

	var bugAnchors []*domain.CodeAnchor
	doJSON(t, srv, "GET", "/api/v1/bugs/"+got.DerivedItemID+"/code-anchors", nil, 200, &bugAnchors)
	if len(bugAnchors) != 1 {
		t.Fatalf("expected 1 propagated anchor on bug, got %d: %+v", len(bugAnchors), bugAnchors)
	}
	ba := bugAnchors[0]
	if ba.Kind != "file" || ba.Path != "src/main.go" || ba.LineStart != 10 || ba.LineEnd != 20 {
		t.Errorf("propagated anchor fields: %+v", ba)
	}
	if ba.OwnerType != "bug_item" || ba.OwnerID != got.DerivedItemID {
		t.Errorf("propagated anchor owner wrong: %+v", ba)
	}
	// Copy should get a fresh ID — not shared with source.
	var srcAnchors []*domain.CodeAnchor
	doJSON(t, srv, "GET", "/api/v1/items/"+item.ID+"/code-anchors", nil, 200, &srcAnchors)
	if len(srcAnchors) != 1 {
		t.Fatalf("source item should still have its anchor: %+v", srcAnchors)
	}
	if srcAnchors[0].ID == ba.ID {
		t.Errorf("copied anchor shares source ID %q — expected fresh ID", ba.ID)
	}
}

// --- Project repo_root via API ---

func TestProjectAPI_UpdateRepoRoot(t *testing.T) {
	srv, _ := setup(t)
	var p domain.Project
	doJSON(t, srv, "POST", "/api/v1/projects", map[string]string{"name": "p"}, 201, &p)
	if p.RepoRoot != "" {
		t.Errorf("new project should have empty repo_root, got %q", p.RepoRoot)
	}

	var updated domain.Project
	doJSON(t, srv, "PATCH", "/api/v1/projects/"+p.ID, map[string]any{
		"repo_root": "/home/dev/projects/foo",
	}, 200, &updated)
	if updated.RepoRoot != "/home/dev/projects/foo" {
		t.Errorf("updated repo_root = %q", updated.RepoRoot)
	}
}
