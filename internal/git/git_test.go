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

package git

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
)

// newTempRepo initializes a git repo in a tempdir, creates a file with `v1`
// and commits it, then rewrites the file with `v2` and commits again.
// Returns the repo root and the two commit hashes (oldest first).
func newTempRepo(t *testing.T) (root, sha1, sha2 string) {
	t.Helper()
	dir := t.TempDir()
	r, err := gogit.PlainInit(dir, false)
	if err != nil {
		t.Fatalf("plain init: %v", err)
	}
	wt, err := r.Worktree()
	if err != nil {
		t.Fatalf("worktree: %v", err)
	}
	file := filepath.Join(dir, "hello.go")

	if err := os.WriteFile(file, []byte("v1\n"), 0644); err != nil {
		t.Fatalf("write v1: %v", err)
	}
	if _, err := wt.Add("hello.go"); err != nil {
		t.Fatalf("add v1: %v", err)
	}
	sig := &object.Signature{Name: "Test", Email: "test@example.com", When: time.Unix(1_700_000_000, 0)}
	h1, err := wt.Commit("add hello", &gogit.CommitOptions{Author: sig})
	if err != nil {
		t.Fatalf("commit v1: %v", err)
	}

	if err := os.WriteFile(file, []byte("v2\n"), 0644); err != nil {
		t.Fatalf("write v2: %v", err)
	}
	if _, err := wt.Add("hello.go"); err != nil {
		t.Fatalf("add v2: %v", err)
	}
	sig2 := &object.Signature{Name: "Test", Email: "test@example.com", When: time.Unix(1_700_000_100, 0)}
	h2, err := wt.Commit("update hello", &gogit.CommitOptions{Author: sig2})
	if err != nil {
		t.Fatalf("commit v2: %v", err)
	}
	return dir, h1.String(), h2.String()
}

func TestResolveCommit(t *testing.T) {
	root, sha1, _ := newTempRepo(t)
	r, err := Open(root)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	// Short SHA resolves to the full SHA + author time.
	gotSHA, when, ok := r.ResolveCommit(sha1[:8])
	if !ok || gotSHA != sha1 {
		t.Fatalf("ResolveCommit(%s) = %q,%v; want %s,true", sha1[:8], gotSHA, ok, sha1)
	}
	if !when.Equal(time.Unix(1_700_000_000, 0)) {
		t.Errorf("when = %v, want the commit's author time", when)
	}
	// A non-existent revision fails cleanly.
	if _, _, ok := r.ResolveCommit("deadbeefdeadbeef"); ok {
		t.Error("ResolveCommit on a bogus rev should return ok=false")
	}
}

func TestTree(t *testing.T) {
	root, _, _ := newTempRepo(t)
	r, err := Open(root)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	paths, err := r.Tree()
	if err != nil {
		t.Fatalf("tree: %v", err)
	}
	if len(paths) != 1 || paths[0] != "hello.go" {
		t.Errorf("tree = %v, want [hello.go]", paths)
	}
}

func TestFileContentAtRevision(t *testing.T) {
	root, sha1, sha2 := newTempRepo(t)
	r, err := Open(root)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	// Latest.
	got, err := r.FileContent("hello.go", sha2)
	if err != nil || string(got) != "v2\n" {
		t.Errorf("sha2 content: got=%q err=%v", got, err)
	}
	// Older.
	got, err = r.FileContent("hello.go", sha1)
	if err != nil || string(got) != "v1\n" {
		t.Errorf("sha1 content: got=%q err=%v", got, err)
	}
	// Short SHA.
	got, err = r.FileContent("hello.go", sha2[:7])
	if err != nil || string(got) != "v2\n" {
		t.Errorf("short sha2 content: got=%q err=%v", got, err)
	}
	// Working copy.
	if err := os.WriteFile(filepath.Join(root, "hello.go"), []byte("dirty\n"), 0644); err != nil {
		t.Fatalf("dirty write: %v", err)
	}
	got, err = r.FileContent("hello.go", "")
	if err != nil || string(got) != "dirty\n" {
		t.Errorf("working copy: got=%q err=%v", got, err)
	}
	got, err = r.FileContent("hello.go", "working")
	if err != nil || string(got) != "dirty\n" {
		t.Errorf("working keyword: got=%q err=%v", got, err)
	}
}

func TestFileContentNotFound(t *testing.T) {
	root, sha1, _ := newTempRepo(t)
	r, _ := Open(root)
	_, err := r.FileContent("nope.go", sha1)
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestFileCommits(t *testing.T) {
	root, sha1, sha2 := newTempRepo(t)
	r, _ := Open(root)

	commits, err := r.FileCommits("hello.go", 0)
	if err != nil {
		t.Fatalf("commits: %v", err)
	}
	if len(commits) != 2 {
		t.Fatalf("expected 2 commits, got %d", len(commits))
	}
	// Newest first.
	if commits[0].SHA != sha2 || commits[1].SHA != sha1 {
		t.Errorf("order: got [%s, %s], want [%s, %s]",
			commits[0].SHA, commits[1].SHA, sha2, sha1)
	}
	if commits[0].ShortSHA != sha2[:7] {
		t.Errorf("short SHA: got %q, want %q", commits[0].ShortSHA, sha2[:7])
	}
	if commits[0].Subject != "update hello" || commits[1].Subject != "add hello" {
		t.Errorf("subjects: %q, %q", commits[0].Subject, commits[1].Subject)
	}
}

func TestFileCommitsLimit(t *testing.T) {
	root, _, sha2 := newTempRepo(t)
	r, _ := Open(root)
	commits, err := r.FileCommits("hello.go", 1)
	if err != nil {
		t.Fatalf("commits: %v", err)
	}
	if len(commits) != 1 {
		t.Fatalf("limit=1, got %d commits", len(commits))
	}
	if commits[0].SHA != sha2 {
		t.Errorf("expected newest commit, got %s", commits[0].SHA)
	}
}

func TestSafePath_RejectsTraversal(t *testing.T) {
	root, _, _ := newTempRepo(t)
	r, _ := Open(root)
	for _, bad := range []string{
		"../etc/passwd",
		"/etc/passwd",
		"a/../../b",
		"",
		"..",
	} {
		if _, err := r.SafePath(bad); err == nil {
			t.Errorf("SafePath(%q) did not error", bad)
		}
	}
	for _, ok := range []string{
		"hello.go",
		"a/b.go",
		"./hello.go", // cleans to hello.go
	} {
		if _, err := r.SafePath(ok); err != nil {
			t.Errorf("SafePath(%q) unexpected err: %v", ok, err)
		}
	}
}

func TestOpen_NotARepo(t *testing.T) {
	dir := t.TempDir()
	if _, err := Open(dir); err == nil {
		t.Error("expected error opening non-repo dir")
	}
}

func TestTree_EmptyRepoNoHead(t *testing.T) {
	dir := t.TempDir()
	if _, err := gogit.PlainInit(dir, false); err != nil {
		t.Fatalf("init: %v", err)
	}
	r, err := Open(dir)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	paths, err := r.Tree()
	if err != nil {
		t.Errorf("empty repo tree: %v", err)
	}
	if len(paths) != 0 {
		t.Errorf("empty repo tree: got %v", paths)
	}
}

// TestAttributeCommitsToBranches builds master(c1,c2) → feature(c3) →
// master(c4) and checks the documented heuristic: shared root history
// attributes to the default branch, branch-only commits to their branch,
// post-fork default-branch commits to the default branch, and unknown
// SHAs stay unresolved.
func TestAttributeCommitsToBranches(t *testing.T) {
	root, sha1, sha2 := newTempRepo(t) // two commits on master
	r0, err := gogit.PlainOpen(root)
	if err != nil {
		t.Fatalf("open raw: %v", err)
	}
	wt, err := r0.Worktree()
	if err != nil {
		t.Fatalf("worktree: %v", err)
	}

	// Branch "feature" off master tip, one commit on it.
	if err := wt.Checkout(&gogit.CheckoutOptions{
		Branch: plumbing.NewBranchReferenceName("feature"),
		Create: true,
	}); err != nil {
		t.Fatalf("checkout -b feature: %v", err)
	}
	file := filepath.Join(root, "feat.go")
	if err := os.WriteFile(file, []byte("feat\n"), 0644); err != nil {
		t.Fatalf("write feat: %v", err)
	}
	if _, err := wt.Add("feat.go"); err != nil {
		t.Fatalf("add feat: %v", err)
	}
	sig := &object.Signature{Name: "Test", Email: "test@example.com", When: time.Unix(1_700_000_200, 0)}
	h3, err := wt.Commit("feature work", &gogit.CommitOptions{Author: sig})
	if err != nil {
		t.Fatalf("commit feature: %v", err)
	}

	// Back to master, one more commit there.
	if err := wt.Checkout(&gogit.CheckoutOptions{Branch: plumbing.Master}); err != nil {
		t.Fatalf("checkout master: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "hello.go"), []byte("v3\n"), 0644); err != nil {
		t.Fatalf("write v3: %v", err)
	}
	if _, err := wt.Add("hello.go"); err != nil {
		t.Fatalf("add v3: %v", err)
	}
	sig2 := &object.Signature{Name: "Test", Email: "test@example.com", When: time.Unix(1_700_000_300, 0)}
	h4, err := wt.Commit("master work after fork", &gogit.CommitOptions{Author: sig2})
	if err != nil {
		t.Fatalf("commit v3: %v", err)
	}

	r, err := Open(root)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	unknown := "0123456789abcdef0123456789abcdef01234567"
	attr, def, err := r.AttributeCommitsToBranches([]string{sha1, sha2, h3.String(), h4.String(), unknown})
	if err != nil {
		t.Fatalf("attribute: %v", err)
	}
	if def != "master" {
		t.Errorf("default branch = %q, want master", def)
	}
	for sha, want := range map[string]string{
		sha1:        "master", // shared root
		sha2:        "master", // shared root (fork point)
		h3.String(): "feature",
		h4.String(): "master", // only master contains it
	} {
		if got := attr[sha]; got != want {
			t.Errorf("attr[%s] = %q, want %q", sha[:8], got, want)
		}
	}
	if _, ok := attr[unknown]; ok {
		t.Errorf("unknown sha should be unresolved, got %q", attr[unknown])
	}
}
