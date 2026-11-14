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
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
)

// reviewRepo builds: main c1 (a.txt 1..10, bin.dat) → c2 (a.txt line 5
// changed; b.txt added), then branch "feat" off c2 with c3 (a.txt line 9
// changed, b.txt deleted), then main advances with c4 (c.txt added) so the
// merge-base of feat and main is c2, not main's tip.
type reviewFixture struct {
	root           string
	c1, c2, c3, c4 string
	repo           *Repo
}

func buildReviewRepo(t *testing.T) reviewFixture {
	t.Helper()
	dir := t.TempDir()
	r, err := gogit.PlainInit(dir, false)
	if err != nil {
		t.Fatal(err)
	}
	wt, _ := r.Worktree()
	write := func(name, content string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := wt.Add(name); err != nil {
			t.Fatal(err)
		}
	}
	commit := func(msg string, at int64) string {
		h, err := wt.Commit(msg, &gogit.CommitOptions{
			Author: &object.Signature{Name: "T", Email: "t@x", When: time.Unix(at, 0)},
		})
		if err != nil {
			t.Fatal(err)
		}
		return h.String()
	}
	tenLines := func(fifth, ninth string) string {
		ls := []string{"l1", "l2", "l3", "l4", "l5", "l6", "l7", "l8", "l9", "l10"}
		ls[4], ls[8] = fifth, ninth
		return strings.Join(ls, "\n") + "\n"
	}
	write("a.txt", tenLines("l5", "l9"))
	write("bin.dat", "\x00\x01\x02")
	c1 := commit("c1", 1_700_000_000)
	write("a.txt", tenLines("five", "l9"))
	write("b.txt", "b1\nb2\n")
	c2 := commit("c2", 1_700_000_100)

	// Rename the default branch to main so the default-branch rule is exercised.
	head, _ := r.Head()
	if err := r.Storer.SetReference(plumbing.NewHashReference(plumbing.NewBranchReferenceName("main"), head.Hash())); err != nil {
		t.Fatal(err)
	}
	if err := wt.Checkout(&gogit.CheckoutOptions{Branch: plumbing.NewBranchReferenceName("main")}); err != nil {
		t.Fatal(err)
	}
	_ = r.Storer.RemoveReference(head.Name())

	if err := wt.Checkout(&gogit.CheckoutOptions{Branch: plumbing.NewBranchReferenceName("feat"), Create: true}); err != nil {
		t.Fatal(err)
	}
	write("a.txt", tenLines("five", "nine"))
	if _, err := wt.Remove("b.txt"); err != nil {
		t.Fatal(err)
	}
	c3 := commit("c3", 1_700_000_200)

	if err := wt.Checkout(&gogit.CheckoutOptions{Branch: plumbing.NewBranchReferenceName("main")}); err != nil {
		t.Fatal(err)
	}
	write("c.txt", "c\n")
	c4 := commit("c4", 1_700_000_300)

	repo, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	return reviewFixture{root: dir, c1: c1, c2: c2, c3: c3, c4: c4, repo: repo}
}

func TestBranches(t *testing.T) {
	f := buildReviewRepo(t)
	bs, def, err := f.repo.Branches()
	if err != nil {
		t.Fatal(err)
	}
	if def != "main" {
		t.Fatalf("default = %q", def)
	}
	if len(bs) != 2 || bs[0].Name != "main" || bs[1].Name != "feat" {
		t.Fatalf("branches = %+v", bs)
	}
	if !bs[0].IsDefault || !bs[0].IsCurrent || bs[0].SHA != f.c4 {
		t.Errorf("main = %+v", bs[0])
	}
	if bs[1].IsDefault || bs[1].IsCurrent || bs[1].SHA != f.c3 || bs[1].Remote {
		t.Errorf("feat = %+v", bs[1])
	}
}

func TestCommitsOnRef(t *testing.T) {
	f := buildReviewRepo(t)
	cs, err := f.repo.Commits("feat", "", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(cs) != 3 || cs[0].SHA != f.c3 || cs[2].SHA != f.c1 {
		t.Fatalf("feat commits = %+v", cs)
	}
	cs, err = f.repo.Commits("", "", 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(cs) != 2 || cs[0].SHA != f.c4 {
		t.Fatalf("HEAD commits = %+v", cs)
	}
	cs, err = f.repo.Commits("main", "b.txt", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(cs) != 1 || cs[0].SHA != f.c2 {
		t.Fatalf("b.txt on main = %+v", cs)
	}
	if _, err := f.repo.Commits("nope", "", 10); err == nil {
		t.Error("unknown ref should fail")
	}
}

func TestMergeBase(t *testing.T) {
	f := buildReviewRepo(t)
	mb, err := f.repo.MergeBase("feat", "main")
	if err != nil {
		t.Fatal(err)
	}
	if mb != f.c2 {
		t.Fatalf("merge-base = %s, want c2 %s", mb, f.c2)
	}
}

func TestDiff_CommitAgainstParent(t *testing.T) {
	f := buildReviewRepo(t)
	cs, err := f.repo.Diff(context.Background(), "", f.c2, DefaultContext, "")
	if err != nil {
		t.Fatal(err)
	}
	if cs.Base != f.c1 || cs.Head != f.c2 {
		t.Fatalf("base/head = %s/%s", cs.Base, cs.Head)
	}
	if len(cs.Files) != 2 {
		t.Fatalf("files = %+v", cs.Files)
	}
	a, b := cs.Files[0], cs.Files[1]
	if a.Path != "a.txt" || a.Status != "modified" || a.Added != 1 || a.Deleted != 1 {
		t.Errorf("a.txt = %+v", a)
	}
	if len(a.Hunks) != 1 {
		t.Fatalf("a.txt hunks = %+v", a.Hunks)
	}
	h := a.Hunks[0]
	// Line 5 changed with 3 lines of context: old 2..8 (7 lines), new 2..8.
	if h.OldStart != 2 || h.OldLines != 7 || h.NewStart != 2 || h.NewLines != 7 {
		t.Errorf("hunk header = %+v", h)
	}
	ops := ""
	for _, l := range h.Lines {
		ops += l.Op[:1]
	}
	if ops != "cccdaccc" {
		t.Errorf("ops = %s", ops)
	}
	if h.Lines[3].Text != "l5" || h.Lines[3].Old != 5 || h.Lines[4].Text != "five" || h.Lines[4].New != 5 {
		t.Errorf("changed lines = %+v %+v", h.Lines[3], h.Lines[4])
	}
	if b.Path != "b.txt" || b.Status != "added" || b.Added != 2 || len(b.Hunks) != 1 || b.Hunks[0].OldStart != 0 || b.Hunks[0].NewStart != 1 {
		t.Errorf("b.txt = %+v", b)
	}
}

func TestDiff_RootCommitAndBinary(t *testing.T) {
	f := buildReviewRepo(t)
	cs, err := f.repo.Diff(context.Background(), "", f.c1, DefaultContext, "")
	if err != nil {
		t.Fatal(err)
	}
	if cs.Base != "" || len(cs.Files) != 2 {
		t.Fatalf("root diff = %+v", cs)
	}
	bin := cs.Files[1]
	if bin.Path != "bin.dat" || !bin.Binary || bin.Hunks != nil || bin.Status != "added" {
		t.Errorf("bin.dat = %+v", bin)
	}
}

func TestDiff_BranchAgainstMergeBase(t *testing.T) {
	f := buildReviewRepo(t)
	cs, err := f.repo.Diff(context.Background(), f.c2, "feat", DefaultContext, "")
	if err != nil {
		t.Fatal(err)
	}
	if cs.Head != f.c3 || len(cs.Files) != 2 {
		t.Fatalf("branch diff = %+v", cs)
	}
	if cs.Files[0].Path != "a.txt" || cs.Files[0].Status != "modified" {
		t.Errorf("a = %+v", cs.Files[0])
	}
	if cs.Files[1].Path != "b.txt" || cs.Files[1].Status != "deleted" || cs.Files[1].Deleted != 2 {
		t.Errorf("b = %+v", cs.Files[1])
	}
	// c.txt (main only) must not appear: the base is the merge-base.
	for _, fd := range cs.Files {
		if fd.Path == "c.txt" {
			t.Error("c.txt leaked into the branch changeset")
		}
	}
}

func TestDiff_SingleFileFullContext(t *testing.T) {
	f := buildReviewRepo(t)
	cs, err := f.repo.Diff(context.Background(), "", f.c2, FullContext, "a.txt")
	if err != nil {
		t.Fatal(err)
	}
	if len(cs.Files) != 1 || len(cs.Files[0].Hunks) != 1 {
		t.Fatalf("single = %+v", cs)
	}
	h := cs.Files[0].Hunks[0]
	if len(h.Lines) != 11 || h.OldStart != 1 || h.OldLines != 10 || h.NewStart != 1 || h.NewLines != 10 {
		t.Errorf("full hunk = %+v", h)
	}
	if _, err := f.repo.Diff(context.Background(), "", f.c2, FullContext, "../etc/passwd"); err == nil {
		t.Error("path escape should fail")
	}
}

func TestDiff_WorkingTree(t *testing.T) {
	f := buildReviewRepo(t)
	if err := os.WriteFile(filepath.Join(f.root, "c.txt"), []byte("c\nmore\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(f.root, "new.txt"), []byte("n\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(f.root, "a.txt")); err != nil {
		t.Fatal(err)
	}
	cs, err := f.repo.Diff(context.Background(), "", Working, DefaultContext, "")
	if err != nil {
		t.Fatal(err)
	}
	if cs.Base != f.c4 || cs.Head != Working {
		t.Fatalf("base/head = %s/%s", cs.Base, cs.Head)
	}
	got := map[string]string{}
	for _, fd := range cs.Files {
		got[fd.Path] = fd.Status
	}
	want := map[string]string{"a.txt": "deleted", "c.txt": "modified", "new.txt": "added"}
	if len(got) != len(want) {
		t.Fatalf("files = %v", got)
	}
	for p, st := range want {
		if got[p] != st {
			t.Errorf("%s = %q, want %q", p, got[p], st)
		}
	}
}

// --- shapeHunks: pure, so the off-by-ones are held still here ---

func mk(ops string) []DiffLine {
	var out []DiffLine
	o, n := 0, 0
	for i, c := range ops {
		l := DiffLine{Text: string(rune('a' + i%26))}
		switch c {
		case 'c':
			o++
			n++
			l.Op, l.Old, l.New = "ctx", o, n
		case 'd':
			o++
			l.Op, l.Old = "del", o
		case 'a':
			n++
			l.Op, l.New = "add", n
		}
		out = append(out, l)
	}
	return out
}

func hunkOps(h DiffHunk) string {
	s := ""
	for _, l := range h.Lines {
		s += l.Op[:1]
	}
	return s
}

func TestShapeHunks(t *testing.T) {
	cases := []struct {
		name  string
		ops   string
		ctx   int
		want  []string // ops per hunk
		heads [][4]int // old_start, old_lines, new_start, new_lines
	}{
		{"no change", "cccc", 3, nil, nil},
		{"empty", "", 3, nil, nil},
		{"one change, wide context", "ccdacc", 3, []string{"ccdacc"}, [][4]int{{1, 5, 1, 5}}},
		{"zero context", "ccdacc", 0, []string{"da"}, [][4]int{{3, 1, 3, 1}}},
		{"two regions split", "cdcccccccac", 2, []string{"cdcc", "ccac"}, [][4]int{{1, 4, 1, 3}, {8, 3, 7, 4}}},
		{"two regions merged", "cdcccac", 2, []string{"cdcccac"}, [][4]int{{1, 6, 1, 6}}},
		{"pure add at top, zero context", "acc", 0, []string{"a"}, [][4]int{{0, 0, 1, 1}}},
		{"pure delete of everything", "dd", 3, []string{"dd"}, [][4]int{{1, 2, 0, 0}}},
		{"pure add after old line 2, zero context", "ccacc", 0, []string{"a"}, [][4]int{{2, 0, 3, 1}}},
		{"full context", "cdc", FullContext, []string{"cdc"}, [][4]int{{1, 3, 1, 2}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			hs := shapeHunks(mk(tc.ops), tc.ctx)
			if len(hs) != len(tc.want) {
				t.Fatalf("got %d hunks %+v, want %d", len(hs), hs, len(tc.want))
			}
			for i, h := range hs {
				if hunkOps(h) != tc.want[i] {
					t.Errorf("hunk %d ops = %s, want %s", i, hunkOps(h), tc.want[i])
				}
				got := [4]int{h.OldStart, h.OldLines, h.NewStart, h.NewLines}
				if got != tc.heads[i] {
					t.Errorf("hunk %d header = %v, want %v", i, got, tc.heads[i])
				}
			}
		})
	}
}

func TestLineDiff_TrailingNewlineAndEmpty(t *testing.T) {
	ls := lineDiff("a\nb\n", "a\nb\nc")
	if len(ls) != 3 || ls[2].Op != "add" || ls[2].Text != "c" || ls[2].New != 3 {
		t.Errorf("lines = %+v", ls)
	}
	if ls := lineDiff("", ""); len(ls) != 0 {
		t.Errorf("empty = %+v", ls)
	}
	if ls := lineDiff("", "x\n"); len(ls) != 1 || ls[0].Op != "add" {
		t.Errorf("from empty = %+v", ls)
	}
}
