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

// An implemented item that touches an always-review zone is escalated to High
// and flagged in the review queue.
func TestReviewQueue_ZoneEscalates(t *testing.T) {
	srv, ag, store := setupWithStore(t)

	var p domain.Project
	doJSON(t, srv, "POST", "/api/v1/projects", map[string]string{"name": "p"}, 201, &p)
	// Tag an always-review zone.
	doJSON(t, srv, "PUT", "/api/v1/projects/"+p.ID+"/settings/review.always_review_zones",
		map[string]any{"value": []string{"auth"}}, 200, nil)

	var sp domain.Scratchpad
	doJSON(t, srv, "POST", "/api/v1/projects/"+p.ID+"/scratchpads", map[string]string{"name": "x"}, 201, &sp)
	var item domain.ScratchpadItem
	doJSON(t, srv, "POST", "/api/v1/scratchpads/"+sp.ID+"/items",
		map[string]string{"content": "boom 500", "classification_override": "bug"}, 201, &item)
	ag.Stop()
	var got domain.ScratchpadItem
	doJSON(t, srv, "GET", "/api/v1/items/"+item.ID, nil, 200, &got)
	bugID := got.DerivedItemID
	if bugID == "" {
		t.Fatalf("no derived bug; state=%q", got.ClassificationState)
	}

	// Record an implementation: a commit + a file in the auth zone.
	seedCodeAnchor(t, store, "bug_item", bugID, domain.CodeAnchor{Kind: "commit", Revision: "abcd1234"})
	seedCodeAnchor(t, store, "bug_item", bugID, domain.CodeAnchor{
		Kind: "file", Path: "internal/auth/login.go", Revision: "abcd1234"})

	var resp reviewQueueResp
	doJSON(t, srv, "GET", "/api/v1/projects/"+p.ID+"/review-queue", nil, 200, &resp)
	if len(resp.Items) != 1 {
		t.Fatalf("expected 1 queued item, got %d: %+v", len(resp.Items), resp.Items)
	}
	e := resp.Items[0]
	if e.ID != bugID || e.OwnerType != "bug_item" {
		t.Errorf("wrong item: %+v", e)
	}
	if !e.InZone || e.Band != "High" || !e.NeedsReview {
		t.Errorf("zone change should escalate to High/needs_review: %+v", e)
	}
	if e.HasVerification {
		t.Errorf("no verification has run; has_verification should be false")
	}
}

// A new project (no verification track record) is untrusting: a Medium-risk
// unverified change escalates, and the response reports New trust.
func TestReviewQueue_NewTrustEscalates(t *testing.T) {
	srv, ag, store := setupWithStore(t)
	var p domain.Project
	doJSON(t, srv, "POST", "/api/v1/projects", map[string]string{"name": "p"}, 201, &p)
	var sp domain.Scratchpad
	doJSON(t, srv, "POST", "/api/v1/projects/"+p.ID+"/scratchpads", map[string]string{"name": "x"}, 201, &sp)
	var item domain.ScratchpadItem
	doJSON(t, srv, "POST", "/api/v1/scratchpads/"+sp.ID+"/items",
		map[string]string{"content": "boom", "classification_override": "bug"}, 201, &item)
	ag.Stop()
	var got domain.ScratchpadItem
	doJSON(t, srv, "GET", "/api/v1/items/"+item.ID, nil, 200, &got)

	// Implemented (commit), unverified, no zone → Medium risk.
	seedCodeAnchor(t, store, "bug_item", got.DerivedItemID, domain.CodeAnchor{Kind: "commit", Revision: "abcd1234"})

	var resp reviewQueueResp
	doJSON(t, srv, "GET", "/api/v1/projects/"+p.ID+"/review-queue", nil, 200, &resp)
	if resp.Trust.Tier != "New" || resp.EscalateAtOrAbove != "Medium" {
		t.Fatalf("new project should be New trust escalating Medium+, got %s/%s", resp.Trust.Tier, resp.EscalateAtOrAbove)
	}
	if len(resp.Items) != 1 || resp.Items[0].Band != "Medium" || !resp.Items[0].NeedsReview {
		t.Errorf("unverified Medium change should escalate at New trust: %+v", resp.Items)
	}
}

// A human approval clears an escalated item from the queue (closing the loop),
// and a rejection flags it for rework.
func TestReviewQueue_HumanDecision(t *testing.T) {
	srv, ag, store := setupWithStore(t)
	var p domain.Project
	doJSON(t, srv, "POST", "/api/v1/projects", map[string]string{"name": "p"}, 201, &p)
	doJSON(t, srv, "PUT", "/api/v1/projects/"+p.ID+"/settings/review.always_review_zones",
		map[string]any{"value": []string{"auth"}}, 200, nil)
	var sp domain.Scratchpad
	doJSON(t, srv, "POST", "/api/v1/projects/"+p.ID+"/scratchpads", map[string]string{"name": "x"}, 201, &sp)
	var item domain.ScratchpadItem
	doJSON(t, srv, "POST", "/api/v1/scratchpads/"+sp.ID+"/items",
		map[string]string{"content": "boom", "classification_override": "bug"}, 201, &item)
	ag.Stop()
	var got domain.ScratchpadItem
	doJSON(t, srv, "GET", "/api/v1/items/"+item.ID, nil, 200, &got)
	bugID := got.DerivedItemID
	seedCodeAnchor(t, store, "bug_item", bugID, domain.CodeAnchor{Kind: "commit", Revision: "abcd1234"})
	seedCodeAnchor(t, store, "bug_item", bugID, domain.CodeAnchor{
		Kind: "file", Path: "internal/auth/x.go", Revision: "abcd1234"})

	// Starts escalated (zone).
	var resp reviewQueueResp
	doJSON(t, srv, "GET", "/api/v1/projects/"+p.ID+"/review-queue", nil, 200, &resp)
	if !resp.Items[0].NeedsReview {
		t.Fatalf("zone item should start escalated")
	}

	// Approve → cleared.
	doJSON(t, srv, "POST", "/api/v1/bugs/"+bugID+"/review", map[string]any{"decision": "approved"}, 201, nil)
	doJSON(t, srv, "GET", "/api/v1/projects/"+p.ID+"/review-queue", nil, 200, &resp)
	if resp.Items[0].Decision != "approved" || resp.Items[0].NeedsReview {
		t.Errorf("approved item should be cleared: %+v", resp.Items[0])
	}

	// Reject → needs rework again.
	doJSON(t, srv, "POST", "/api/v1/bugs/"+bugID+"/review",
		map[string]any{"decision": "rejected", "note": "nope"}, 201, nil)
	doJSON(t, srv, "GET", "/api/v1/projects/"+p.ID+"/review-queue", nil, 200, &resp)
	if resp.Items[0].Decision != "rejected" || !resp.Items[0].NeedsReview {
		t.Errorf("rejected item should need rework: %+v", resp.Items[0])
	}

	// Bad decision value → 400.
	doJSON(t, srv, "POST", "/api/v1/bugs/"+bugID+"/review", map[string]any{"decision": "maybe"}, 400, nil)
}

// Items with no recorded implementation don't appear in the queue.
func TestReviewQueue_OnlyImplemented(t *testing.T) {
	srv, ag := setup(t)
	var p domain.Project
	doJSON(t, srv, "POST", "/api/v1/projects", map[string]string{"name": "p"}, 201, &p)
	var sp domain.Scratchpad
	doJSON(t, srv, "POST", "/api/v1/projects/"+p.ID+"/scratchpads", map[string]string{"name": "x"}, 201, &sp)
	var item domain.ScratchpadItem
	doJSON(t, srv, "POST", "/api/v1/scratchpads/"+sp.ID+"/items",
		map[string]string{"content": "boom", "classification_override": "bug"}, 201, &item)
	ag.Stop()

	var resp reviewQueueResp
	doJSON(t, srv, "GET", "/api/v1/projects/"+p.ID+"/review-queue", nil, 200, &resp)
	if len(resp.Items) != 0 {
		t.Errorf("no implemented items, queue should be empty: %+v", resp.Items)
	}
}

// TestReviewQueue_UnanchoredImplementedIsFlagged proves the throughline flag: a
// bug marked done with NO code anchor is surfaced (unanchored + needs_review),
// not silently dropped from the queue for lacking a location.
func TestReviewQueue_UnanchoredImplementedIsFlagged(t *testing.T) {
	srv, ag := setup(t)
	var p domain.Project
	doJSON(t, srv, "POST", "/api/v1/projects", map[string]string{"name": "p"}, 201, &p)
	var sp domain.Scratchpad
	doJSON(t, srv, "POST", "/api/v1/projects/"+p.ID+"/scratchpads", map[string]string{"name": "x"}, 201, &sp)
	var item domain.ScratchpadItem
	doJSON(t, srv, "POST", "/api/v1/scratchpads/"+sp.ID+"/items",
		map[string]string{"content": "boom", "classification_override": "bug"}, 201, &item)
	ag.Stop()
	var got domain.ScratchpadItem
	doJSON(t, srv, "GET", "/api/v1/items/"+item.ID, nil, 200, &got)
	bugID := got.DerivedItemID
	if bugID == "" {
		t.Fatalf("no derived bug; state=%q", got.ClassificationState)
	}

	// Mark it fixed — WITHOUT recording any code anchor (the broken throughline).
	doJSON(t, srv, "PATCH", "/api/v1/bugs/"+bugID, map[string]string{"status": "fixed"}, 200, nil)

	var resp reviewQueueResp
	doJSON(t, srv, "GET", "/api/v1/projects/"+p.ID+"/review-queue", nil, 200, &resp)
	if len(resp.Items) != 1 {
		t.Fatalf("unanchored implemented item must be surfaced, got %d: %+v", len(resp.Items), resp.Items)
	}
	e := resp.Items[0]
	if e.ID != bugID || !e.Unanchored || !e.NeedsReview {
		t.Fatalf("want unanchored + needs_review for %s, got %+v", bugID, e)
	}
	if len(e.Reasons) == 0 || !strings.Contains(strings.Join(e.Reasons, " "), "no code location") {
		t.Errorf("want a 'no code location' reason, got %v", e.Reasons)
	}
}
