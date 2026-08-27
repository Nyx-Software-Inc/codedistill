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
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"
)

func TestCommitMetrics_RootCommit(t *testing.T) {
	root, sha1, _ := newTempRepo(t)
	r, _ := Open(root)

	m, err := r.CommitMetrics(context.Background(), sha1)
	if err != nil {
		t.Fatalf("metrics: %v", err)
	}
	// Root commit adds hello.go with one line ("v1\n"), against the empty tree.
	if !m.Found {
		t.Error("Found should be true")
	}
	if m.Added != 1 || m.Deleted != 0 || m.Net != 1 {
		t.Errorf("added/deleted/net = %d/%d/%d, want 1/0/1", m.Added, m.Deleted, m.Net)
	}
	if m.FilesChanged != 1 {
		t.Errorf("files = %d, want 1", m.FilesChanged)
	}
	if m.Hunks != 1 {
		t.Errorf("hunks = %d, want 1", m.Hunks)
	}
	if m.Complexity != "Low" {
		t.Errorf("complexity = %q, want Low", m.Complexity)
	}
	if len(m.Paths) != 1 || m.Paths[0] != "hello.go" {
		t.Errorf("paths = %v, want [hello.go]", m.Paths)
	}
}

func TestCommitMetrics_Edit(t *testing.T) {
	root, _, sha2 := newTempRepo(t)
	r, _ := Open(root)

	m, err := r.CommitMetrics(context.Background(), sha2)
	if err != nil {
		t.Fatalf("metrics: %v", err)
	}
	// Edit replaces "v1\n" with "v2\n": one line deleted, one added.
	if m.Added != 1 || m.Deleted != 1 || m.Net != 0 {
		t.Errorf("added/deleted/net = %d/%d/%d, want 1/1/0", m.Added, m.Deleted, m.Net)
	}
	if m.FilesChanged != 1 || m.Hunks != 1 {
		t.Errorf("files/hunks = %d/%d, want 1/1", m.FilesChanged, m.Hunks)
	}
}

// CommitChanges on the edit commit: hello.go modified, one hunk at new-side line 1.
func TestCommitChanges_Edit(t *testing.T) {
	root, _, sha2 := newTempRepo(t)
	r, _ := Open(root)
	changes, err := r.CommitChanges(context.Background(), sha2)
	if err != nil {
		t.Fatalf("CommitChanges: %v", err)
	}
	if len(changes) != 1 {
		t.Fatalf("want 1 changed file, got %d: %+v", len(changes), changes)
	}
	c := changes[0]
	if c.Path != "hello.go" || c.Status != "modified" {
		t.Errorf("file = %+v, want hello.go modified", c)
	}
	if len(c.Hunks) != 1 || c.Hunks[0].StartLine != 1 || c.Hunks[0].EndLine != 1 {
		t.Errorf("hunks = %+v, want one [1,1]", c.Hunks)
	}
}

// CommitChanges on the root commit: hello.go added, one hunk starting at line 1.
func TestCommitChanges_RootAdd(t *testing.T) {
	root, sha1, _ := newTempRepo(t)
	r, _ := Open(root)
	changes, err := r.CommitChanges(context.Background(), sha1)
	if err != nil {
		t.Fatalf("CommitChanges: %v", err)
	}
	if len(changes) != 1 || changes[0].Status != "added" {
		t.Fatalf("want 1 added file, got %+v", changes)
	}
	if len(changes[0].Hunks) != 1 || changes[0].Hunks[0].StartLine != 1 {
		t.Errorf("hunks = %+v, want one starting at line 1", changes[0].Hunks)
	}
}

func TestCommitChanges_NotFound(t *testing.T) {
	root, _, _ := newTempRepo(t)
	r, _ := Open(root)
	if _, err := r.CommitChanges(context.Background(), "deadbeefdeadbeef"); !errors.Is(err, ErrNotFound) {
		t.Errorf("want ErrNotFound for a missing rev, got %v", err)
	}
}

func TestCommitMetrics_NotFound(t *testing.T) {
	root, _, _ := newTempRepo(t)
	r, _ := Open(root)

	m, err := r.CommitMetrics(context.Background(), "deadbeefdeadbeefdeadbeefdeadbeefdeadbeef")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
	if m.Found {
		t.Error("Found should be false for a missing revision")
	}
	if m.SHA == "" {
		t.Error("SHA should still be populated so the row renders")
	}
}

func TestCommitMetrics_ExcludesGenerated(t *testing.T) {
	dir := t.TempDir()
	r, err := gogit.PlainInit(dir, false)
	if err != nil {
		t.Fatal(err)
	}
	wt, _ := r.Worktree()
	// One source file (3 lines) + one generated bundle under dist/ (5 lines)
	// + a lockfile (2 lines), all in a single commit.
	mustWrite(t, dir, "app.go", "a\nb\nc\n")
	mustWrite(t, dir, "web/dist/bundle.js", "x\ny\nz\nw\nv\n")
	mustWrite(t, dir, "package-lock.json", "{\n}\n")
	for _, f := range []string{"app.go", "web/dist/bundle.js", "package-lock.json"} {
		if _, err := wt.Add(f); err != nil {
			t.Fatalf("add %s: %v", f, err)
		}
	}
	sig := &object.Signature{Name: "T", Email: "t@e.com", When: time.Unix(1_700_000_000, 0)}
	h, err := wt.Commit("mixed", &gogit.CommitOptions{Author: sig})
	if err != nil {
		t.Fatal(err)
	}

	repo, _ := Open(dir)
	m, err := repo.CommitMetrics(context.Background(), h.String())
	if err != nil {
		t.Fatal(err)
	}
	// Headline counts only app.go (3 lines, 1 file); the bundle + lockfile
	// are set aside.
	if m.Added != 3 {
		t.Errorf("added = %d, want 3 (source only, generated excluded)", m.Added)
	}
	if m.FilesChanged != 1 {
		t.Errorf("files_changed = %d, want 1", m.FilesChanged)
	}
	if m.ExcludedFiles != 2 {
		t.Errorf("excluded_files = %d, want 2 (dist bundle + lockfile)", m.ExcludedFiles)
	}
	if len(m.Paths) != 1 || m.Paths[0] != "app.go" {
		t.Errorf("paths = %v, want [app.go]", m.Paths)
	}
}

func TestCommitMetrics_HonorsGitattributes(t *testing.T) {
	dir := t.TempDir()
	r, err := gogit.PlainInit(dir, false)
	if err != nil {
		t.Fatal(err)
	}
	wt, _ := r.Worktree()
	// pb/ is NOT in the built-in default list — only .gitattributes marks it.
	if isGenerated("pb/api.pb.go") {
		t.Fatal("precondition: pb/ must not be default-excluded, else the test is moot")
	}
	mustWrite(t, dir, ".gitattributes", "pb/** linguist-generated=true\n")
	mustWrite(t, dir, "main.go", "package main\n")
	mustWrite(t, dir, "pb/api.pb.go", "// generated\nx\ny\n")
	for _, f := range []string{".gitattributes", "main.go", "pb/api.pb.go"} {
		if _, err := wt.Add(f); err != nil {
			t.Fatalf("add %s: %v", f, err)
		}
	}
	sig := &object.Signature{Name: "T", Email: "t@e.com", When: time.Unix(1_700_000_000, 0)}
	h, err := wt.Commit("c", &gogit.CommitOptions{Author: sig})
	if err != nil {
		t.Fatal(err)
	}

	repo, _ := Open(dir)
	m, err := repo.CommitMetrics(context.Background(), h.String())
	if err != nil {
		t.Fatal(err)
	}
	if m.ExcludedFiles != 1 {
		t.Errorf("excluded_files = %d, want 1 (pb/ via .gitattributes)", m.ExcludedFiles)
	}
	for _, p := range m.Paths {
		if strings.HasPrefix(p, "pb/") {
			t.Errorf("pb/ file should be excluded by .gitattributes; paths = %v", m.Paths)
		}
	}
}

func TestIsGenerated(t *testing.T) {
	gen := []string{
		"internal/webui/dist/assets/index-ABC.js", "web/dist/bundle.js",
		"node_modules/foo/index.js", "vendor/x/y.go", "package-lock.json",
		"go.sum", "app.min.js", "styles.min.css", "build/out.js.map",
	}
	src := []string{
		"internal/git/metrics.go", "web/src/App.svelte", "main.go",
		"distillery/real.go", // contains "dist" as a substring but not a segment
	}
	for _, p := range gen {
		if !isGenerated(p) {
			t.Errorf("isGenerated(%q) = false, want true", p)
		}
	}
	for _, p := range src {
		if isGenerated(p) {
			t.Errorf("isGenerated(%q) = true, want false", p)
		}
	}
}

func mustWrite(t *testing.T, dir, rel, content string) {
	t.Helper()
	full := filepath.Join(dir, rel)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestComplexityBands(t *testing.T) {
	cases := []struct {
		churn, files, hunks int
		want                string
	}{
		{10, 1, 1, "Low"},       // 10 + 8 + 4 = 22
		{40, 1, 2, "Low"},       // 40 + 8 + 8 = 56
		{120, 3, 6, "Moderate"}, // 120 + 24 + 24 = 168
		{500, 10, 25, "High"},   // 500 + 80 + 100 = 680
		{2000, 30, 60, "Very High"},
	}
	for _, c := range cases {
		got, _ := Complexity(c.churn, c.files, c.hunks)
		if got != c.want {
			score := c.churn + 8*c.files + 4*c.hunks
			t.Errorf("Complexity(churn=%d,files=%d,hunks=%d) score=%d = %q, want %q",
				c.churn, c.files, c.hunks, score, got, c.want)
		}
	}
}
