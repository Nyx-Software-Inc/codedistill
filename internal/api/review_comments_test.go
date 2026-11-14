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

func TestReviewCommentsAPI(t *testing.T) {
	srv, _ := setup(t)
	root, sha1, sha2 := seedRepo(t)
	var p domain.Project
	doJSON(t, srv, "POST", "/api/v1/projects", map[string]string{"name": "p"}, 201, &p)
	base := "/api/v1/projects/" + p.ID
	doJSON(t, srv, "POST", base+"/changesets", map[string]string{"head": sha2}, 409, nil) // no repo yet
	doJSON(t, srv, "PATCH", base, map[string]any{"repo_root": root}, 200, &p)

	// A commit with base "" canonicalises to its parent; the same pair given
	// explicitly is the same row (200, not 201).
	var cs domain.Changeset
	doJSON(t, srv, "POST", base+"/changesets", map[string]string{"head": sha2}, 201, &cs)
	if cs.BaseSHA != sha1 || cs.HeadSHA != sha2 || cs.Kind != "commit" || cs.ID == "" {
		t.Fatalf("changeset = %+v", cs)
	}
	var again domain.Changeset
	doJSON(t, srv, "POST", base+"/changesets", map[string]string{"base": sha1, "head": sha2, "kind": "branch"}, 200, &again)
	if again.ID != cs.ID {
		t.Fatalf("second ensure minted a new row: %s vs %s", again.ID, cs.ID)
	}
	// The working copy is its own changeset against HEAD.
	var wc domain.Changeset
	doJSON(t, srv, "POST", base+"/changesets", map[string]string{"head": "working"}, 201, &wc)
	if wc.Kind != "working" || wc.HeadSHA != "working" || wc.BaseSHA != sha2 {
		t.Fatalf("working changeset = %+v", wc)
	}
	doJSON(t, srv, "POST", base+"/changesets", map[string]string{"head": "nope"}, 404, nil)

	cpath := base + "/changesets/" + cs.ID + "/comments"
	// Validation.
	doJSON(t, srv, "POST", cpath, map[string]any{"path": "hello.go", "side": "new", "line_start": 1, "body": ""}, 400, nil)
	doJSON(t, srv, "POST", cpath, map[string]any{"path": "hello.go", "side": "up", "line_start": 1, "body": "x"}, 400, nil)
	doJSON(t, srv, "POST", cpath, map[string]any{"path": "hello.go", "side": "new", "line_start": 0, "body": "x"}, 400, nil)
	doJSON(t, srv, "POST", cpath, map[string]any{"path": "", "side": "new", "line_start": 1, "body": "x"}, 400, nil)

	var c domain.ReviewComment
	doJSON(t, srv, "POST", cpath, map[string]any{
		"path": "hello.go", "side": "new", "line_start": 1, "body": "  why v2?\nsecond line  ",
	}, 201, &c)
	if c.LineEnd != 1 || c.Body != "why v2?\nsecond line" || c.Author == "" || c.AuthorName == "" || c.ChangesetID != cs.ID {
		t.Fatalf("comment = %+v", c)
	}
	var list reviewCommentsResponse
	doJSON(t, srv, "GET", cpath, nil, 200, &list)
	if list.Changeset.ID != cs.ID || len(list.Comments) != 1 || list.Comments[0].ID != c.ID {
		t.Fatalf("list = %+v", list)
	}
	// Another changeset does not see it, and cannot reach it by id.
	var other reviewCommentsResponse
	doJSON(t, srv, "GET", base+"/changesets/"+wc.ID+"/comments", nil, 200, &other)
	if len(other.Comments) != 0 {
		t.Fatalf("working changeset sees %d comments", len(other.Comments))
	}
	doJSON(t, srv, "PATCH", base+"/changesets/"+wc.ID+"/comments/"+c.ID, map[string]any{"resolved": true}, 404, nil)

	// Edit and resolve, then unresolve.
	doJSON(t, srv, "PATCH", cpath+"/"+c.ID, map[string]any{"body": "why v2, really?", "resolved": true}, 200, &c)
	if c.Body != "why v2, really?" || c.ResolvedAt == nil {
		t.Fatalf("after patch = %+v", c)
	}
	// A fresh target: resolved_at is omitempty, and decoding into the old
	// struct would keep the stale pointer.
	var un domain.ReviewComment
	doJSON(t, srv, "PATCH", cpath+"/"+c.ID, map[string]any{"resolved": false}, 200, &un)
	if un.ResolvedAt != nil {
		t.Fatalf("unresolve left %v", un.ResolvedAt)
	}
	doJSON(t, srv, "PATCH", cpath+"/"+c.ID, map[string]any{"body": "   "}, 400, nil)

	// Promote to a bug: the bug exists in the project with the comment's first
	// line as subject, carries a file anchor at head, and the comment records it.
	doJSON(t, srv, "POST", cpath+"/"+c.ID+"/promote", map[string]any{"type": "note"}, 400, nil)
	var pr promoteReviewCommentResponse
	doJSON(t, srv, "POST", cpath+"/"+c.ID+"/promote", map[string]any{"type": "bug", "severity": "major"}, 201, &pr)
	if pr.OwnerType != "bug_item" || pr.OwnerID == "" || pr.Comment.PromotedOwnerID != pr.OwnerID {
		t.Fatalf("promote = %+v", pr)
	}
	var bug domain.BugItem
	doJSON(t, srv, "GET", "/api/v1/bugs/"+pr.OwnerID, nil, 200, &bug)
	if bug.Subject != "why v2, really?" || bug.Severity != "major" || bug.ProjectID != p.ID || bug.Origin != "manual" {
		t.Fatalf("bug = %+v", bug)
	}
	var anchors []domain.CodeAnchor
	doJSON(t, srv, "GET", "/api/v1/bugs/"+pr.OwnerID+"/code-anchors", nil, 200, &anchors)
	if len(anchors) != 1 || anchors[0].Path != "hello.go" || anchors[0].LineStart != 1 || anchors[0].Revision != sha2 {
		t.Fatalf("anchors = %+v", anchors)
	}
	// Twice is a conflict.
	doJSON(t, srv, "POST", cpath+"/"+c.ID+"/promote", map[string]any{"type": "todo"}, 409, nil)

	// A second comment promotes to a todo; the working-copy head anchors to
	// the working copy (no revision).
	var c2 domain.ReviewComment
	doJSON(t, srv, "POST", base+"/changesets/"+wc.ID+"/comments", map[string]any{
		"path": "hello.go", "side": "old", "line_start": 1, "line_end": 2, "body": "rename this",
	}, 201, &c2)
	doJSON(t, srv, "POST", base+"/changesets/"+wc.ID+"/comments/"+c2.ID+"/promote", map[string]any{"type": "todo", "priority": "high"}, 201, &pr)
	var todo domain.TodoItem
	doJSON(t, srv, "GET", "/api/v1/todos/"+pr.OwnerID, nil, 200, &todo)
	if todo.Subject != "rename this" || todo.Priority != "high" {
		t.Fatalf("todo = %+v", todo)
	}
	// A fresh slice: revision is omitempty, and decoding into the bug's
	// anchor would keep its SHA.
	var tAnchors []domain.CodeAnchor
	doJSON(t, srv, "GET", "/api/v1/todos/"+pr.OwnerID+"/code-anchors", nil, 200, &tAnchors)
	if len(tAnchors) != 1 || tAnchors[0].Revision != "" || tAnchors[0].LineEnd != 2 {
		t.Fatalf("todo anchors = %+v", tAnchors)
	}

	// Delete.
	doJSON(t, srv, "DELETE", cpath+"/"+c.ID, nil, 204, nil)
	doJSON(t, srv, "DELETE", cpath+"/"+c.ID, nil, 404, nil)
	doJSON(t, srv, "GET", cpath, nil, 200, &list)
	if len(list.Comments) != 0 {
		t.Fatalf("after delete: %d comments", len(list.Comments))
	}
}
