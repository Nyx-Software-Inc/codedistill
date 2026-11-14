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
	"strings"
	"testing"
	"time"

	"codedistill/internal/domain"
)

func TestReviewsAPI(t *testing.T) {
	srv, _, st := setupWithStore(t)
	root, sha1, sha2 := seedRepo(t)
	ctx := context.Background()
	var p domain.Project
	doJSON(t, srv, "POST", "/api/v1/projects", map[string]string{"name": "p"}, 201, &p)
	base := "/api/v1/projects/" + p.ID
	doJSON(t, srv, "PATCH", base, map[string]any{"repo_root": root}, 200, &p)

	var cs domain.Changeset
	doJSON(t, srv, "POST", base+"/changesets", map[string]string{"head": sha2}, 201, &cs)
	doJSON(t, srv, "GET", base+"/changesets/"+cs.ID+"/review", nil, 404, nil)

	// An item implemented at sha2, with the review policy at gate.
	var todo domain.TodoItem
	doJSON(t, srv, "POST", base+"/todos", map[string]any{"subject": "the work"}, 201, &todo)
	doJSON(t, srv, "POST", "/api/v1/todos/"+todo.ID+"/code-anchors", map[string]any{"kind": "commit", "revision": sha2}, 201, nil)
	doJSON(t, srv, "PUT", base+"/settings/governance.review", map[string]any{"value": "gate"}, 200, nil)
	if r := gateReason(ctx, st, "todo_item", todo.ID); r != "" {
		t.Fatalf("blocked before any review: %s", r)
	}

	// Request a review: single-user needs one approval; it covers sha2 only.
	var v reviewView
	doJSON(t, srv, "POST", base+"/changesets/"+cs.ID+"/review", nil, 201, &v)
	if v.Review.Status != domain.ReviewOpen || v.Review.RequiredApprovals != 1 || v.Approvals != 0 || v.Label == "" {
		t.Fatalf("opened = %+v", v)
	}
	var again reviewView
	doJSON(t, srv, "POST", base+"/changesets/"+cs.ID+"/review", nil, 200, &again)
	if again.Review.ID != v.Review.ID {
		t.Fatalf("second request opened another review")
	}
	if shas, _ := st.ListChangesetCommits(ctx, cs.ID); len(shas) != 1 || shas[0] != sha2 {
		t.Fatalf("covered commits = %v (sha1 %s must not be covered)", shas, sha1)
	}
	if r := gateReason(ctx, st, "todo_item", todo.ID); hasGate && !strings.Contains(r, "unfinished code review") {
		t.Fatalf("open review did not gate completion: %q", r)
	}
	// It is in the live list and the Needs review count.
	var list reviewListResponse
	doJSON(t, srv, "GET", base+"/reviews?live=1", nil, 200, &list)
	if len(list.Reviews) != 1 || list.Reviews[0].Review.ID != v.Review.ID || list.Reviews[0].Changeset == nil {
		t.Fatalf("live list = %+v", list)
	}

	// Request changes, then approve: the latest decision per reviewer wins.
	doJSON(t, srv, "POST", base+"/reviews/"+v.Review.ID+"/decide", map[string]any{"decision": "meh"}, 400, nil)
	doJSON(t, srv, "POST", base+"/reviews/"+v.Review.ID+"/decide", map[string]any{"decision": "rejected", "note": "nope"}, 201, &v)
	if v.Review.Status != domain.ReviewChangesRequested || v.Mine != "rejected" || len(v.Decisions) != 1 || !v.Decisions[0].Counts {
		t.Fatalf("after reject = %+v", v)
	}
	if r := gateReason(ctx, st, "todo_item", todo.ID); hasGate && r == "" {
		t.Fatal("changes requested did not gate completion")
	}
	doJSON(t, srv, "POST", base+"/reviews/"+v.Review.ID+"/decide", map[string]any{"decision": "approved"}, 201, &v)
	if v.Review.Status != domain.ReviewApproved || v.Approvals != 1 || v.Mine != "approved" || len(v.Decisions) != 2 {
		t.Fatalf("after approve = %+v", v)
	}
	// Approval propagated to the item: the gate opens, the trust dial sees a
	// human sign-off, and the item's log says where it came from.
	if r := gateReason(ctx, st, "todo_item", todo.ID); r != "" {
		t.Fatalf("approved review still blocks: %s", r)
	}
	d, err := st.LatestReviewDecision(ctx, "todo_item", todo.ID)
	if err != nil || d.Decision != "approved" || d.CommitSHA != sha2 || !strings.Contains(d.Note, "via code review") {
		t.Fatalf("item decision = %+v %v", d, err)
	}
	// Approving again does not duplicate the item decision.
	doJSON(t, srv, "POST", base+"/reviews/"+v.Review.ID+"/decide", map[string]any{"decision": "approved"}, 201, &v)
	if ds, _ := st.ListReviewDecisions(ctx, "todo_item", todo.ID); len(ds) != 1 {
		t.Fatalf("item decisions after re-approve = %d", len(ds))
	}

	// Merged is recorded by hand, only from approved; then nothing more.
	doJSON(t, srv, "POST", base+"/reviews/"+v.Review.ID+"/status", map[string]any{"status": "open"}, 409, nil)
	doJSON(t, srv, "POST", base+"/reviews/"+v.Review.ID+"/status", map[string]any{"status": "merged"}, 200, &v)
	if v.Review.Status != domain.ReviewMerged {
		t.Fatalf("merged = %+v", v.Review)
	}
	doJSON(t, srv, "POST", base+"/reviews/"+v.Review.ID+"/decide", map[string]any{"decision": "approved"}, 409, nil)
	doJSON(t, srv, "GET", base+"/reviews?live=1", nil, 200, &list)
	if len(list.Reviews) != 0 {
		t.Fatalf("merged review still live: %+v", list)
	}
	doJSON(t, srv, "GET", base+"/reviews", nil, 200, &list)
	if len(list.Reviews) != 1 {
		t.Fatalf("all reviews = %d", len(list.Reviews))
	}

	// A working-copy review covers no commits and never gates; abandon and reopen.
	var wc domain.Changeset
	doJSON(t, srv, "POST", base+"/changesets", map[string]string{"head": "working"}, 201, &wc)
	var wv reviewView
	doJSON(t, srv, "POST", base+"/changesets/"+wc.ID+"/review", nil, 201, &wv)
	if shas, _ := st.ListChangesetCommits(ctx, wc.ID); len(shas) != 0 {
		t.Fatalf("working copy covers %v", shas)
	}
	doJSON(t, srv, "POST", base+"/reviews/"+wv.Review.ID+"/status", map[string]any{"status": "abandoned"}, 200, &wv)
	doJSON(t, srv, "POST", base+"/reviews/"+wv.Review.ID+"/status", map[string]any{"status": "open"}, 200, &wv)
	if wv.Review.Status != domain.ReviewOpen {
		t.Fatalf("reopened = %+v", wv.Review)
	}
	// Reviews are project-scoped by path.
	var other domain.Project
	doJSON(t, srv, "POST", "/api/v1/projects", map[string]string{"name": "q"}, 201, &other)
	doJSON(t, srv, "POST", "/api/v1/projects/"+other.ID+"/reviews/"+wv.Review.ID+"/decide", map[string]any{"decision": "approved"}, 404, nil)
}

func TestReviewOutcome(t *testing.T) {
	now := time.Now()
	rv := &domain.Review{RequiredApprovals: 2, AuthorEmail: "author@x"}
	emails := map[string]string{"a": "author@x", "b": "b@x", "c": "c@x"}
	emailOf := func(uid string) string { return emails[uid] }
	dec := func(id, who, d string, at time.Duration) domain.ReviewDecision {
		return domain.ReviewDecision{ID: id, Reviewer: who, Decision: d, CreatedAt: now.Add(at)}
	}
	// Newest first, as the store lists them.
	ds := []domain.ReviewDecision{dec("3", "b", "approved", 3), dec("2", "a", "approved", 2)}
	status, n, counts := reviewOutcome(rv, ds, true, emailOf)
	if status != domain.ReviewOpen || n != 1 || counts["2"] || !counts["3"] {
		t.Fatalf("author's approval counted: %s %d %v", status, n, counts)
	}
	// Single-user: the author is the one human; it counts.
	if status, n, _ := reviewOutcome(&domain.Review{RequiredApprovals: 1, AuthorEmail: "author@x"}, ds[1:], false, emailOf); status != domain.ReviewApproved || n != 1 {
		t.Fatalf("single-user author exclusion applied: %s %d", status, n)
	}
	ds = append([]domain.ReviewDecision{dec("4", "c", "approved", 4)}, ds...)
	if status, n, _ := reviewOutcome(rv, ds, true, emailOf); status != domain.ReviewApproved || n != 2 {
		t.Fatalf("two approvals: %s %d", status, n)
	}
	// c changes their mind: their latest decision is what counts.
	ds = append([]domain.ReviewDecision{dec("5", "c", "rejected", 5)}, ds...)
	if status, n, _ := reviewOutcome(rv, ds, true, emailOf); status != domain.ReviewChangesRequested || n != 1 {
		t.Fatalf("latest rejection: %s %d", status, n)
	}
}
