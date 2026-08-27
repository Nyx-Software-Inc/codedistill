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
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"

	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"

	"codedistill/internal/domain"
	"codedistill/internal/git"
)

// seedRepo initializes a throwaway git repo with two commits on hello.go.
// Returns the path and both commit hashes (oldest first).
func seedRepo(t *testing.T) (root, sha1, sha2 string) {
	t.Helper()
	dir := t.TempDir()
	r, err := gogit.PlainInit(dir, false)
	if err != nil {
		t.Fatalf("init: %v", err)
	}
	wt, _ := r.Worktree()
	file := filepath.Join(dir, "hello.go")
	if err := os.WriteFile(file, []byte("v1\n"), 0644); err != nil {
		t.Fatalf("write v1: %v", err)
	}
	_, _ = wt.Add("hello.go")
	sig := &object.Signature{Name: "T", Email: "t@x", When: time.Unix(1_700_000_000, 0)}
	h1, _ := wt.Commit("c1", &gogit.CommitOptions{Author: sig})
	if err := os.WriteFile(file, []byte("v2\n"), 0644); err != nil {
		t.Fatalf("write v2: %v", err)
	}
	_, _ = wt.Add("hello.go")
	sig2 := &object.Signature{Name: "T", Email: "t@x", When: time.Unix(1_700_000_100, 0)}
	h2, _ := wt.Commit("c2", &gogit.CommitOptions{Author: sig2})
	return dir, h1.String(), h2.String()
}

// --- Tests ---

func TestFilesAPI_RequiresRepoRoot(t *testing.T) {
	srv, _ := setup(t)
	var p domain.Project
	doJSON(t, srv, "POST", "/api/v1/projects", map[string]string{"name": "p"}, 201, &p)

	// No repo_root configured yet → 409 conflict.
	doJSON(t, srv, "GET", "/api/v1/projects/"+p.ID+"/files", nil, 409, nil)
	doJSON(t, srv, "GET", "/api/v1/projects/"+p.ID+"/files/content?path=x", nil, 409, nil)
	doJSON(t, srv, "GET", "/api/v1/projects/"+p.ID+"/files/commits?path=x", nil, 409, nil)
}

func TestFilesAPI_TreeAndContent(t *testing.T) {
	srv, _ := setup(t)
	root, sha1, sha2 := seedRepo(t)

	var p domain.Project
	doJSON(t, srv, "POST", "/api/v1/projects", map[string]string{"name": "p"}, 201, &p)
	doJSON(t, srv, "PATCH", "/api/v1/projects/"+p.ID, map[string]any{"repo_root": root}, 200, &p)

	// Tree.
	var tree fileTreeResponse
	doJSON(t, srv, "GET", "/api/v1/projects/"+p.ID+"/files", nil, 200, &tree)
	if len(tree.Paths) != 1 || tree.Paths[0] != "hello.go" {
		t.Errorf("tree: %+v", tree)
	}

	// Content at sha2 (latest).
	var c fileContentResponse
	doJSON(t, srv, "GET", "/api/v1/projects/"+p.ID+"/files/content?path=hello.go&revision="+sha2, nil, 200, &c)
	if c.Content != "v2\n" || c.Binary {
		t.Errorf("content sha2: %+v", c)
	}
	// Content at sha1 (earlier).
	doJSON(t, srv, "GET", "/api/v1/projects/"+p.ID+"/files/content?path=hello.go&revision="+sha1, nil, 200, &c)
	if c.Content != "v1\n" {
		t.Errorf("content sha1: %+v", c)
	}
	// Working copy (no revision param).
	doJSON(t, srv, "GET", "/api/v1/projects/"+p.ID+"/files/content?path=hello.go", nil, 200, &c)
	if c.Content != "v2\n" || c.Revision != "working" {
		t.Errorf("working content: %+v", c)
	}
}

func TestFilesAPI_ContentNotFound(t *testing.T) {
	srv, _ := setup(t)
	root, _, _ := seedRepo(t)
	var p domain.Project
	doJSON(t, srv, "POST", "/api/v1/projects", map[string]string{"name": "p"}, 201, &p)
	doJSON(t, srv, "PATCH", "/api/v1/projects/"+p.ID, map[string]any{"repo_root": root}, 200, &p)

	doJSON(t, srv, "GET", "/api/v1/projects/"+p.ID+"/files/content?path=nope.go", nil, 404, nil)
}

func TestFilesAPI_ContentPathTraversalRejected(t *testing.T) {
	srv, _ := setup(t)
	root, _, _ := seedRepo(t)
	var p domain.Project
	doJSON(t, srv, "POST", "/api/v1/projects", map[string]string{"name": "p"}, 201, &p)
	doJSON(t, srv, "PATCH", "/api/v1/projects/"+p.ID, map[string]any{"repo_root": root}, 200, &p)

	escape := url.QueryEscape("../etc/passwd")
	doJSON(t, srv, "GET", "/api/v1/projects/"+p.ID+"/files/content?path="+escape, nil, 400, nil)
}

func TestFilesAPI_Commits(t *testing.T) {
	srv, _ := setup(t)
	root, sha1, sha2 := seedRepo(t)
	var p domain.Project
	doJSON(t, srv, "POST", "/api/v1/projects", map[string]string{"name": "p"}, 201, &p)
	doJSON(t, srv, "PATCH", "/api/v1/projects/"+p.ID, map[string]any{"repo_root": root}, 200, &p)

	var commits []git.CommitInfo
	doJSON(t, srv, "GET", "/api/v1/projects/"+p.ID+"/files/commits?path=hello.go", nil, 200, &commits)
	if len(commits) != 2 {
		t.Fatalf("expected 2 commits, got %d", len(commits))
	}
	if commits[0].SHA != sha2 || commits[1].SHA != sha1 {
		t.Errorf("commit order: %+v", commits)
	}
	if commits[0].ShortSHA != sha2[:7] {
		t.Errorf("short sha: %q", commits[0].ShortSHA)
	}
}

func TestFilesAPI_CommitsLimit(t *testing.T) {
	srv, _ := setup(t)
	root, _, sha2 := seedRepo(t)
	var p domain.Project
	doJSON(t, srv, "POST", "/api/v1/projects", map[string]string{"name": "p"}, 201, &p)
	doJSON(t, srv, "PATCH", "/api/v1/projects/"+p.ID, map[string]any{"repo_root": root}, 200, &p)

	var commits []git.CommitInfo
	doJSON(t, srv, "GET", "/api/v1/projects/"+p.ID+"/files/commits?path=hello.go&limit=1", nil, 200, &commits)
	if len(commits) != 1 || commits[0].SHA != sha2 {
		t.Errorf("limit=1 result: %+v", commits)
	}
}
