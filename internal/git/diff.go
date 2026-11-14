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

// Changesets for code review (docs/design/code-review.md): branches to pick
// from, commits on a ref, the merge-base that turns a branch into base..head,
// and the diff itself — every changed file as two-sided numbered lines, cut
// into hunks with a context the caller chooses.
//
// The diff is computed per file from the two contents (blob or working copy)
// with go-git's line diff, then shaped by shapeHunks. Shaping is pure and
// tested on its own: it is where off-by-ones live.

package git

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"

	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/utils/diff"
	"github.com/go-git/go-git/v5/utils/merkletrie"
	"github.com/sergi/go-diff/diffmatchpatch"
)

// Working is the head value meaning "the working copy", matching the
// revision convention FileContent already accepts.
const Working = "working"

// FullContext asks Diff for whole files rather than hunks.
const FullContext = -1

// DefaultContext is git's own default, and GitHub's.
const DefaultContext = 3

// maxDiffFileBytes is the per-side size past which a file is reported
// TooLarge instead of diffed: a 20 MB fixture is not something a human
// reviews line by line, and diffing it would pin the request.
const maxDiffFileBytes = 1 << 20

// maxDiffFiles caps a changeset; past it Truncated is set rather than the
// request failing. A reviewer scrolling 500 files is already lost.
const maxDiffFiles = 500

// Branch is one entry in the picker.
type Branch struct {
	Name      string `json:"name"`
	SHA       string `json:"sha"`
	IsDefault bool   `json:"is_default"`
	// IsCurrent: HEAD points at it (the checked-out branch).
	IsCurrent bool `json:"is_current"`
	// Remote: no local branch of this name; the tip is a remote-tracking ref.
	Remote bool `json:"remote"`
}

// Branches lists local branches plus remote-tracking ones with no local
// counterpart, sorted with the default first then alphabetically. The second
// return is the default branch name (see branchTips for how it is chosen).
func (r *Repo) Branches() ([]Branch, string, error) {
	tips, def, err := r.branchTips()
	if err != nil {
		return nil, "", err
	}
	local := map[string]bool{}
	if iter, err := r.r.Branches(); err == nil {
		_ = iter.ForEach(func(ref *plumbing.Reference) error {
			local[ref.Name().Short()] = true
			return nil
		})
	}
	current := ""
	if head, err := r.r.Reference(plumbing.HEAD, false); err == nil && head.Type() == plumbing.SymbolicReference {
		if n := head.Target(); n.IsBranch() {
			current = n.Short()
		}
	}
	out := make([]Branch, 0, len(tips))
	for name, h := range tips {
		out = append(out, Branch{
			Name: name, SHA: h.String(),
			IsDefault: name == def, IsCurrent: name == current, Remote: !local[name],
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].IsDefault != out[j].IsDefault {
			return out[i].IsDefault
		}
		return out[i].Name < out[j].Name
	})
	return out, def, nil
}

// Commits walks history from ref (any revision; "" means HEAD) newest first,
// optionally restricted to commits touching path. limit <= 0 means 50.
// FileCommits is this with ref == "".
func (r *Repo) Commits(ref, path string, limit int) ([]CommitInfo, error) {
	if limit <= 0 {
		limit = 50
	}
	var from plumbing.Hash
	if ref == "" {
		head, err := r.r.Head()
		if err != nil {
			if errors.Is(err, plumbing.ErrReferenceNotFound) {
				return []CommitInfo{}, nil
			}
			return nil, err
		}
		from = head.Hash()
	} else {
		h, err := r.resolveRevision(ref)
		if err != nil {
			return nil, ErrNotFound
		}
		from = h
	}
	opts := &gogit.LogOptions{From: from}
	if path != "" {
		safe, err := r.SafePath(path)
		if err != nil {
			return nil, err
		}
		opts.FileName = &safe
	}
	iter, err := r.r.Log(opts)
	if err != nil {
		return nil, fmt.Errorf("log: %w", err)
	}
	defer iter.Close()
	out := make([]CommitInfo, 0, limit)
	err = iter.ForEach(func(c *object.Commit) error {
		if len(out) >= limit {
			return errStorerStop
		}
		out = append(out, CommitInfo{
			SHA: c.Hash.String(), ShortSHA: shortRev(c.Hash.String()),
			Author: c.Author.Name, Email: c.Author.Email, Date: c.Author.When,
			Subject: subjectLine(c.Message),
		})
		return nil
	})
	if err != nil && !errors.Is(err, errStorerStop) {
		return nil, fmt.Errorf("iterate: %w", err)
	}
	return out, nil
}

// MergeBase returns the best common ancestor of two revisions — the base of
// a branch changeset. ErrNotFound when either does not resolve or they share
// no history.
func (r *Repo) MergeBase(a, b string) (string, error) {
	ca, err := r.commitAt(a)
	if err != nil {
		return "", err
	}
	cb, err := r.commitAt(b)
	if err != nil {
		return "", err
	}
	bases, err := ca.MergeBase(cb)
	if err != nil {
		return "", fmt.Errorf("merge-base: %w", err)
	}
	if len(bases) == 0 {
		return "", ErrNotFound
	}
	return bases[0].Hash.String(), nil
}

func (r *Repo) commitAt(rev string) (*object.Commit, error) {
	h, err := r.resolveRevision(rev)
	if err != nil {
		return nil, ErrNotFound
	}
	c, err := r.r.CommitObject(h)
	if err != nil {
		return nil, ErrNotFound
	}
	return c, nil
}

// DiffLine is one line of a hunk. Old and New are 1-based line numbers on
// each side; a deletion has no New, an insertion no Old.
type DiffLine struct {
	Op   string `json:"op"` // "ctx" | "add" | "del"
	Old  int    `json:"old,omitempty"`
	New  int    `json:"new,omitempty"`
	Text string `json:"text"`
}

// DiffHunk is a contiguous run of lines, as @@ -old,+new @@ would describe it.
type DiffHunk struct {
	OldStart int        `json:"old_start"`
	OldLines int        `json:"old_lines"`
	NewStart int        `json:"new_start"`
	NewLines int        `json:"new_lines"`
	Lines    []DiffLine `json:"lines"`
}

// FileDiff is one changed file. Hunks is nil for binary, too-large and (in a
// multi-file diff) generated files; the flags say which, so the client can
// render "binary", "too large to diff" or a collapsed header, never a blank.
type FileDiff struct {
	Path     string `json:"path"`
	OldPath  string `json:"old_path,omitempty"` // set on a rename
	Status   string `json:"status"`             // added | deleted | modified | renamed
	Binary   bool   `json:"binary"`
	TooLarge bool   `json:"too_large"`
	// Generated: linguist-generated / vendored by the same rule the metrics
	// use. Shown collapsed, never hidden: the human sees churn was set aside.
	Generated bool       `json:"generated"`
	Added     int        `json:"added"`
	Deleted   int        `json:"deleted"`
	Hunks     []DiffHunk `json:"hunks"`
}

// Changeset is base..head rendered for review.
type Changeset struct {
	Base      string     `json:"base"`
	Head      string     `json:"head"` // full SHA, or "working"
	Files     []FileDiff `json:"files"`
	Truncated bool       `json:"truncated"`
}

// ResolveChangeset canonicalises a base..head pair the way Diff reads it,
// without diffing: head "" or "working" stays Working; a commit head becomes
// its full SHA; base "" becomes HEAD's SHA for the working copy or the head's
// first parent for a commit ("" for a root commit or an unborn HEAD). Review
// comments hang off the canonical pair, so the same commit picked two ways
// is one changeset.
func (r *Repo) ResolveChangeset(base, head string) (baseSHA, headSHA string, err error) {
	if head == "" {
		head = Working
	}
	working := head == Working
	var headCommit *object.Commit
	if !working {
		c, err := r.commitAt(head)
		if err != nil {
			return "", "", err
		}
		headCommit = c
		head = c.Hash.String()
	}
	switch {
	case base != "":
		c, err := r.commitAt(base)
		if err != nil {
			return "", "", err
		}
		base = c.Hash.String()
	case working:
		c, err := r.commitAt("HEAD")
		if err != nil && !errors.Is(err, ErrNotFound) {
			return "", "", err
		}
		if c != nil {
			base = c.Hash.String()
		}
	case headCommit.NumParents() > 0:
		c, err := headCommit.Parent(0)
		if err != nil {
			return "", "", err
		}
		base = c.Hash.String()
	}
	return base, head, nil
}

// CommitsBetween lists the commits reachable from head and not from base,
// newest first, capped at limit — what a review of base..head covers. A
// commit changeset is its one commit; the working copy covers none (its
// base is HEAD). base "" means "everything", capped.
func (r *Repo) CommitsBetween(base, head string, limit int) ([]CommitInfo, error) {
	if head == "" || head == Working {
		return []CommitInfo{}, nil
	}
	if limit <= 0 {
		limit = 1000
	}
	from, err := r.resolveRevision(head)
	if err != nil {
		return nil, ErrNotFound
	}
	stop := map[plumbing.Hash]bool{}
	if base != "" {
		b, err := r.resolveRevision(base)
		if err != nil {
			return nil, ErrNotFound
		}
		// Everything reachable from base is excluded; walking it is the
		// honest way to get "not reachable from base" without a merge walk.
		bi, err := r.r.Log(&gogit.LogOptions{From: b})
		if err != nil {
			return nil, err
		}
		n := 0
		_ = bi.ForEach(func(c *object.Commit) error {
			stop[c.Hash] = true
			n++
			if n >= 20000 {
				return errStorerStop
			}
			return nil
		})
	}
	iter, err := r.r.Log(&gogit.LogOptions{From: from})
	if err != nil {
		return nil, err
	}
	out := []CommitInfo{}
	err = iter.ForEach(func(c *object.Commit) error {
		if stop[c.Hash] {
			return nil
		}
		out = append(out, CommitInfo{
			SHA: c.Hash.String(), ShortSHA: shortRev(c.Hash.String()),
			Author: c.Author.Name, Email: c.Author.Email, Date: c.Author.When,
			Subject: subjectLine(c.Message),
		})
		if len(out) >= limit {
			return errStorerStop
		}
		return nil
	})
	if err != nil && !errors.Is(err, errStorerStop) {
		return nil, err
	}
	return out, nil
}

// Diff computes base..head. head "" or "working" means the working copy;
// base "" means head's first parent (the empty tree for a root commit), or
// HEAD when head is the working copy. context is lines of context per hunk,
// or FullContext for whole files. onlyPath restricts the result to one file;
// generated files get hunks only when asked for singly.
func (r *Repo) Diff(ctx context.Context, base, head string, contextLines int, onlyPath string) (*Changeset, error) {
	if onlyPath != "" {
		safe, err := r.SafePath(onlyPath)
		if err != nil {
			return nil, err
		}
		onlyPath = safe
	}
	if head == "" {
		head = Working
	}
	working := head == Working

	// Resolve head.
	var headCommit *object.Commit
	if !working {
		c, err := r.commitAt(head)
		if err != nil {
			return nil, err
		}
		headCommit = c
		head = c.Hash.String()
	}

	// Resolve base.
	var baseCommit *object.Commit
	switch {
	case base != "":
		c, err := r.commitAt(base)
		if err != nil {
			return nil, err
		}
		baseCommit = c
	case working:
		c, err := r.commitAt("HEAD")
		if err != nil && !errors.Is(err, ErrNotFound) {
			return nil, err
		}
		baseCommit = c // nil on an unborn HEAD: everything is an addition
	case headCommit.NumParents() > 0:
		c, err := headCommit.Parent(0)
		if err != nil {
			return nil, err
		}
		baseCommit = c
	}
	cs := &Changeset{Head: head}
	var baseTree *object.Tree
	if baseCommit != nil {
		cs.Base = baseCommit.Hash.String()
		t, err := baseCommit.Tree()
		if err != nil {
			return nil, err
		}
		baseTree = t
	}

	var err error
	if working {
		cs.Files, err = r.diffWorking(ctx, baseTree, contextLines, onlyPath)
	} else {
		headTree, terr := headCommit.Tree()
		if terr != nil {
			return nil, terr
		}
		cs.Files, err = r.diffTrees(ctx, baseTree, headTree, contextLines, onlyPath)
	}
	if err != nil {
		return nil, err
	}
	if len(cs.Files) > maxDiffFiles {
		cs.Files = cs.Files[:maxDiffFiles]
		cs.Truncated = true
	}
	return cs, nil
}

// diffTrees handles the commit and branch shapes. Rename detection is
// go-git's default for DiffTreeContext.
func (r *Repo) diffTrees(ctx context.Context, from, to *object.Tree, contextLines int, onlyPath string) ([]FileDiff, error) {
	changes, err := object.DiffTreeContext(ctx, from, to)
	if err != nil {
		return nil, err
	}
	out := []FileDiff{}
	for _, ch := range changes {
		action, err := ch.Action()
		if err != nil {
			return nil, err
		}
		fd := FileDiff{Path: ch.To.Name, OldPath: ch.From.Name}
		switch action {
		case merkletrie.Insert:
			fd.Status, fd.OldPath = "added", ""
		case merkletrie.Delete:
			fd.Status, fd.Path, fd.OldPath = "deleted", ch.From.Name, ""
		default:
			fd.Status = "modified"
			if ch.From.Name != ch.To.Name {
				fd.Status = "renamed"
			} else {
				fd.OldPath = ""
			}
		}
		if onlyPath != "" && fd.Path != onlyPath && fd.OldPath != onlyPath {
			continue
		}
		fromF, toF, err := ch.Files()
		if err != nil {
			return nil, err
		}
		var oldB, newB []byte
		if fromF != nil {
			if oldB, err = fileBytes(fromF); err != nil {
				return nil, err
			}
		}
		if toF != nil {
			if newB, err = fileBytes(toF); err != nil {
				return nil, err
			}
		}
		r.fillFileDiff(&fd, oldB, newB, contextLines, onlyPath != "")
		out = append(out, fd)
	}
	return out, nil
}

// diffWorking handles the working-tree shape: every path the status reports
// as changed (staged or not, untracked included, ignored excluded), old side
// from the base tree, new side from disk.
func (r *Repo) diffWorking(ctx context.Context, base *object.Tree, contextLines int, onlyPath string) ([]FileDiff, error) {
	wt, err := r.r.Worktree()
	if err != nil {
		return nil, err
	}
	st, err := wt.Status()
	if err != nil {
		return nil, fmt.Errorf("status: %w", err)
	}
	paths := make([]string, 0, len(st))
	for p, s := range st {
		if s.Worktree == gogit.Unmodified && s.Staging == gogit.Unmodified {
			continue
		}
		if onlyPath != "" && p != onlyPath {
			continue
		}
		paths = append(paths, p)
	}
	sort.Strings(paths)
	out := []FileDiff{}
	for _, p := range paths {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		var oldB, newB []byte
		if base != nil {
			if f, err := base.File(p); err == nil {
				if oldB, err = fileBytes(f); err != nil {
					return nil, err
				}
			} else if !errors.Is(err, object.ErrFileNotFound) {
				return nil, err
			}
		}
		onDisk := true
		b, err := os.ReadFile(filepath.Join(r.root, filepath.FromSlash(p)))
		if err != nil {
			if !os.IsNotExist(err) {
				return nil, err
			}
			onDisk = false
		} else {
			newB = b
		}
		fd := FileDiff{Path: p}
		switch {
		case oldB == nil && !onDisk:
			continue // reported but gone on both sides (e.g. an untracked file deleted mid-request)
		case oldB == nil:
			fd.Status = "added"
		case !onDisk:
			fd.Status = "deleted"
		default:
			fd.Status = "modified"
		}
		r.fillFileDiff(&fd, oldB, newB, contextLines, onlyPath != "")
		out = append(out, fd)
	}
	return out, nil
}

func fileBytes(f *object.File) ([]byte, error) {
	rd, err := f.Reader()
	if err != nil {
		return nil, err
	}
	defer rd.Close()
	var buf bytes.Buffer
	if _, err := buf.ReadFrom(rd); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// fillFileDiff decides binary / too-large / generated and, when a line diff
// is warranted, computes it and shapes the hunks. Counts are always filled
// from the full line diff, not the shaped hunks, so a collapsed generated
// file still shows its +/-.
func (r *Repo) fillFileDiff(fd *FileDiff, oldB, newB []byte, contextLines int, single bool) {
	fd.Generated = r.isExcluded(fd.Path)
	if isBinaryBytes(oldB) || isBinaryBytes(newB) {
		fd.Binary = true
		return
	}
	if len(oldB) > maxDiffFileBytes || len(newB) > maxDiffFileBytes {
		fd.TooLarge = true
		return
	}
	lines := lineDiff(string(oldB), string(newB))
	for _, l := range lines {
		switch l.Op {
		case "add":
			fd.Added++
		case "del":
			fd.Deleted++
		}
	}
	if fd.Generated && !single {
		return
	}
	fd.Hunks = shapeHunks(lines, contextLines)
}

// isBinaryBytes mirrors git's heuristic: a NUL in the first 8 KB, or bytes
// that are not UTF-8 at all.
func isBinaryBytes(b []byte) bool {
	if len(b) == 0 {
		return false
	}
	probe := b
	if len(probe) > 8000 {
		probe = probe[:8000]
	}
	if bytes.IndexByte(probe, 0) >= 0 {
		return true
	}
	return !utf8.Valid(b)
}

// lineDiff turns two texts into the full two-sided line stream: every line
// of both files, numbered, tagged ctx/add/del. go-git's Do works on lines
// (it maps each distinct line to a rune first), so the chunks it returns
// hold whole lines.
func lineDiff(oldS, newS string) []DiffLine {
	if oldS == newS {
		return numberLines(oldS)
	}
	var out []DiffLine
	oldN, newN := 0, 0
	for _, d := range diff.Do(oldS, newS) {
		for _, text := range splitLines(d.Text) {
			switch d.Type {
			case diffmatchpatch.DiffEqual:
				oldN++
				newN++
				out = append(out, DiffLine{Op: "ctx", Old: oldN, New: newN, Text: text})
			case diffmatchpatch.DiffDelete:
				oldN++
				out = append(out, DiffLine{Op: "del", Old: oldN, Text: text})
			case diffmatchpatch.DiffInsert:
				newN++
				out = append(out, DiffLine{Op: "add", New: newN, Text: text})
			}
		}
	}
	return out
}

// numberLines renders an unchanged file as all-context lines (used for a
// single-file request with full context on a file that did not change).
func numberLines(s string) []DiffLine {
	var out []DiffLine
	for i, text := range splitLines(s) {
		out = append(out, DiffLine{Op: "ctx", Old: i + 1, New: i + 1, Text: text})
	}
	return out
}

// splitLines splits on newline keeping a final unterminated line and
// dropping the empty string after a trailing newline. "" → no lines.
func splitLines(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, "\n")
	if parts[len(parts)-1] == "" {
		parts = parts[:len(parts)-1]
	}
	return parts
}

// shapeHunks cuts the full line stream into hunks: every changed line plus
// up to contextLines of unchanged lines on either side, with regions that
// touch or overlap merged. FullContext returns one hunk holding every line.
// A stream with no changes yields no hunks (there is nothing to review).
func shapeHunks(lines []DiffLine, contextLines int) []DiffHunk {
	if len(lines) == 0 {
		return nil
	}
	changed := false
	for _, l := range lines {
		if l.Op != "ctx" {
			changed = true
			break
		}
	}
	if !changed {
		return nil
	}
	// oldBefore[i] / newBefore[i]: how many old / new lines precede index i.
	// This is what a hunk header needs — the first line of a run may be an
	// insertion with no old number of its own.
	oldBefore := make([]int, len(lines)+1)
	newBefore := make([]int, len(lines)+1)
	for i, l := range lines {
		oldBefore[i+1], newBefore[i+1] = oldBefore[i], newBefore[i]
		if l.Op != "add" {
			oldBefore[i+1]++
		}
		if l.Op != "del" {
			newBefore[i+1]++
		}
	}
	if contextLines < 0 {
		return []DiffHunk{makeHunk(lines, 0, 0)}
	}
	// Mark which indices are kept: a change, or within contextLines of one.
	keep := make([]bool, len(lines))
	for i, l := range lines {
		if l.Op == "ctx" {
			continue
		}
		lo, hi := i-contextLines, i+contextLines
		if lo < 0 {
			lo = 0
		}
		if hi > len(lines)-1 {
			hi = len(lines) - 1
		}
		for j := lo; j <= hi; j++ {
			keep[j] = true
		}
	}
	var hunks []DiffHunk
	for i := 0; i < len(lines); {
		if !keep[i] {
			i++
			continue
		}
		j := i
		for j < len(lines) && keep[j] {
			j++
		}
		hunks = append(hunks, makeHunk(lines[i:j], oldBefore[i], newBefore[i]))
		i = j
	}
	return hunks
}

// makeHunk computes the @@ header for a run of lines, given how many lines
// of each side precede it. Git's convention: a side with no lines in the run
// reports the count of lines before it and a length of 0 ("-5,0" means
// "after old line 5"); otherwise the start is the next line, 1-based.
func makeHunk(run []DiffLine, oldBefore, newBefore int) DiffHunk {
	h := DiffHunk{Lines: run}
	for _, l := range run {
		switch l.Op {
		case "ctx":
			h.OldLines++
			h.NewLines++
		case "del":
			h.OldLines++
		case "add":
			h.NewLines++
		}
	}
	h.OldStart, h.NewStart = oldBefore, newBefore
	if h.OldLines > 0 {
		h.OldStart++
	}
	if h.NewLines > 0 {
		h.NewStart++
	}
	return h
}
