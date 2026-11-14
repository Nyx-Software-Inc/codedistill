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
	"os"
	"path/filepath"
	"testing"

	"codedistill/internal/domain"
	codegit "codedistill/internal/git"
)

func TestChangesetsAPI(t *testing.T) {
	srv, _ := setup(t)
	root, sha1, sha2 := seedRepo(t)
	var p domain.Project
	doJSON(t, srv, "POST", "/api/v1/projects", map[string]string{"name": "p"}, 201, &p)
	base := "/api/v1/projects/" + p.ID

	// Gated on repo_root like the files endpoints.
	doJSON(t, srv, "GET", base+"/files/branches", nil, 409, nil)
	doJSON(t, srv, "GET", base+"/changesets/diff?head="+sha2, nil, 409, nil)
	doJSON(t, srv, "PATCH", base, map[string]any{"repo_root": root}, 200, &p)

	var br branchesResponse
	doJSON(t, srv, "GET", base+"/files/branches", nil, 200, &br)
	if len(br.Branches) != 1 || br.Branches[0].SHA != sha2 || !br.Branches[0].IsCurrent || br.Default != br.Branches[0].Name {
		t.Fatalf("branches = %+v", br)
	}

	// Commits on a ref, no path — the new shape; by path — the old one.
	var commits []codegit.CommitInfo
	doJSON(t, srv, "GET", base+"/files/commits?ref="+br.Default, nil, 200, &commits)
	if len(commits) != 2 || commits[0].SHA != sha2 {
		t.Fatalf("commits = %+v", commits)
	}
	doJSON(t, srv, "GET", base+"/files/commits?path=hello.go&limit=1", nil, 200, &commits)
	if len(commits) != 1 || commits[0].SHA != sha2 {
		t.Fatalf("commits by path = %+v", commits)
	}
	doJSON(t, srv, "GET", base+"/files/commits?ref=nope", nil, 404, nil)
	doJSON(t, srv, "GET", base+"/files/commits?limit=0", nil, 400, nil)

	var mb mergeBaseResponse
	doJSON(t, srv, "GET", base+"/changesets/merge-base?head="+sha2+"&target="+sha1, nil, 200, &mb)
	if mb.SHA != sha1 {
		t.Fatalf("merge-base = %s", mb.SHA)
	}
	doJSON(t, srv, "GET", base+"/changesets/merge-base?head="+sha2, nil, 400, nil)

	// Commit vs parent, default context.
	var cs codegit.Changeset
	doJSON(t, srv, "GET", base+"/changesets/diff?head="+sha2, nil, 200, &cs)
	if cs.Base != sha1 || cs.Head != sha2 || len(cs.Files) != 1 {
		t.Fatalf("diff = %+v", cs)
	}
	f := cs.Files[0]
	if f.Path != "hello.go" || f.Status != "modified" || f.Added != 1 || f.Deleted != 1 || len(f.Hunks) != 1 {
		t.Fatalf("file = %+v", f)
	}
	if h := f.Hunks[0]; len(h.Lines) != 2 || h.Lines[0].Op != "del" || h.Lines[0].Text != "v1" || h.Lines[1].Op != "add" || h.Lines[1].Text != "v2" {
		t.Fatalf("hunk = %+v", h)
	}

	// Working copy vs HEAD, single file, full context.
	if err := os.WriteFile(filepath.Join(root, "hello.go"), []byte("v2\nv3\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	doJSON(t, srv, "GET", base+"/changesets/diff?path=hello.go&context=full", nil, 200, &cs)
	if cs.Head != "working" || cs.Base != sha2 || len(cs.Files) != 1 || len(cs.Files[0].Hunks) != 1 {
		t.Fatalf("working diff = %+v", cs)
	}
	if h := cs.Files[0].Hunks[0]; len(h.Lines) != 2 || h.Lines[0].Op != "ctx" || h.Lines[1].Op != "add" {
		t.Fatalf("working hunk = %+v", h)
	}

	doJSON(t, srv, "GET", base+"/changesets/diff?head="+sha2+"&context=-1", nil, 400, nil)
	doJSON(t, srv, "GET", base+"/changesets/diff?head=nope", nil, 404, nil)
	doJSON(t, srv, "GET", base+"/changesets/diff?head="+sha2+"&path=../x", nil, 400, nil)
}
