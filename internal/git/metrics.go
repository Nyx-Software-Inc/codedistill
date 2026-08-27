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
	"path"
	"strings"

	"github.com/go-git/go-git/v5/plumbing/format/diff"
	"github.com/go-git/go-git/v5/plumbing/object"
)

// CommitMetrics is the churn + change-shape summary for a single commit,
// computed against its first parent (or the empty tree for a root commit).
//
// "Updated" lines are deliberately absent: git records a modified line as a
// delete + an add, so there is no honest "updated" count to report. We
// surface added / deleted / net and let the human read churn directly.
type CommitMetrics struct {
	SHA          string `json:"sha"`
	ShortSHA     string `json:"short_sha"`
	FilesChanged int    `json:"files_changed"`
	Added        int    `json:"added"`
	Deleted      int    `json:"deleted"`
	Net          int    `json:"net"`
	// Hunks is the number of distinct @@ edit regions across the diff — a
	// scatter signal: 200 lines in one block reviews far easier than 200
	// lines spread across 40 spots.
	Hunks int `json:"hunks"`
	// ExcludedFiles counts files dropped from the headline numbers because
	// they're generated/vendored (build output, lockfiles, minified bundles
	// — see isGenerated). Reported for transparency: the human sees that
	// churn was set aside, never silently. The added/deleted/files/hunks/
	// complexity above are SOURCE-only — what a person actually reviews.
	ExcludedFiles int `json:"excluded_files"`
	// Complexity is a transparent heuristic — NOT a guarantee — meant to
	// route human attention for risk assessment. Band + the raw score that
	// produced it; the signals above are shown alongside so the basis is
	// visible, never a black box. See Complexity().
	Complexity      string `json:"complexity"`
	ComplexityScore int    `json:"complexity_score"`
	// Found is false when the anchored revision no longer resolves in the
	// project's repo (rewritten history, wrong repo, fabricated SHA). The
	// row still renders so the throughline doesn't silently drop a commit.
	Found bool `json:"found"`
	// Paths is the set of files this commit touched. Not serialized — the
	// API layer unions it across an item's commits for a unique-files total.
	Paths []string `json:"-"`
}

// CommitMetrics computes churn + change-shape for one revision against its
// first parent. Returns ErrNotFound (with SHA populated) when the revision
// doesn't resolve, so callers can render a "commit missing" row rather than
// failing the whole request.
func (r *Repo) CommitMetrics(ctx context.Context, rev string) (CommitMetrics, error) {
	out := CommitMetrics{SHA: rev, ShortSHA: shortRev(rev)}

	h, err := r.resolveRevision(rev)
	if err != nil {
		return out, ErrNotFound
	}
	commit, err := r.r.CommitObject(h)
	if err != nil {
		return out, ErrNotFound
	}
	out.SHA = h.String()
	out.ShortSHA = shortRev(out.SHA)

	to, err := commit.Tree()
	if err != nil {
		return out, err
	}
	var from *object.Tree // nil == empty tree (root commit → all additions)
	if commit.NumParents() > 0 {
		parent, err := commit.Parent(0)
		if err != nil {
			return out, err
		}
		if from, err = parent.Tree(); err != nil {
			return out, err
		}
	}

	changes, err := object.DiffTreeContext(ctx, from, to)
	if err != nil {
		return out, err
	}
	patch, err := changes.PatchContext(ctx)
	if err != nil {
		return out, err
	}

	// One pass over file patches: count source churn + hunks, set generated/
	// vendored files aside. Counting from chunks (not patch.Stats) keeps the
	// path and its line counts on the same record — important because go-git
	// doesn't follow renames, so a content-hashed bundle rename shows as a
	// full delete + full add. Excluding those paths neutralizes that phantom
	// churn at the source.
	for _, fp := range patch.FilePatches() {
		from, to := fp.Files()
		p := ""
		if to != nil {
			p = to.Path()
		} else if from != nil {
			p = from.Path() // pure deletion
		}
		if r.isExcluded(p) {
			out.ExcludedFiles++
			continue
		}
		out.Paths = append(out.Paths, p)
		out.FilesChanged++
		if fp.IsBinary() {
			continue
		}
		inHunk := false
		for _, ch := range fp.Chunks() {
			switch ch.Type() {
			case diff.Add:
				out.Added += countLines(ch.Content())
			case diff.Delete:
				out.Deleted += countLines(ch.Content())
			case diff.Equal:
				inHunk = false
				continue
			}
			if !inHunk {
				out.Hunks++
				inHunk = true
			}
		}
	}
	out.Net = out.Added - out.Deleted
	out.Complexity, out.ComplexityScore = Complexity(out.Added+out.Deleted, out.FilesChanged, out.Hunks)
	out.Found = true
	return out, nil
}

// FileChangeHunk is a changed region of a file, as the line range it occupies in
// the NEW version — the location an agent would anchor to.
type FileChangeHunk struct {
	StartLine int `json:"start_line"`
	EndLine   int `json:"end_line"`
}

// FileChange is one file a commit touched: its path, how it changed, and the
// new-side line ranges of each edit region. Hunks is empty for pure deletions
// and binary files.
type FileChange struct {
	Path   string           `json:"path"`
	Status string           `json:"status"` // "added" | "modified" | "deleted" | "renamed"
	Hunks  []FileChangeHunk `json:"hunks,omitempty"`
}

// CommitChanges returns the per-file changes of rev (vs its first parent, or the
// empty tree for a root commit), each with its edit regions as NEW-side line
// ranges. This is the raw material for diff-assist: attributing a multi-item
// commit's hunks to the items they belong to. Excluded (vendored/generated)
// paths are skipped, matching CommitMetrics. Returns ErrNotFound when rev
// doesn't resolve.
func (r *Repo) CommitChanges(ctx context.Context, rev string) ([]FileChange, error) {
	h, err := r.resolveRevision(rev)
	if err != nil {
		return nil, ErrNotFound
	}
	commit, err := r.r.CommitObject(h)
	if err != nil {
		return nil, ErrNotFound
	}
	to, err := commit.Tree()
	if err != nil {
		return nil, err
	}
	var from *object.Tree // nil == empty tree (root commit → all additions)
	if commit.NumParents() > 0 {
		parent, err := commit.Parent(0)
		if err != nil {
			return nil, err
		}
		if from, err = parent.Tree(); err != nil {
			return nil, err
		}
	}
	changes, err := object.DiffTreeContext(ctx, from, to)
	if err != nil {
		return nil, err
	}
	patch, err := changes.PatchContext(ctx)
	if err != nil {
		return nil, err
	}

	var out []FileChange
	for _, fp := range patch.FilePatches() {
		fromF, toF := fp.Files()
		path, status := "", "modified"
		switch {
		case fromF == nil && toF != nil:
			path, status = toF.Path(), "added"
		case fromF != nil && toF == nil:
			path, status = fromF.Path(), "deleted"
		case toF != nil:
			path = toF.Path()
			if fromF != nil && fromF.Path() != toF.Path() {
				status = "renamed"
			}
		}
		if path == "" || r.isExcluded(path) {
			continue
		}
		fc := FileChange{Path: path, Status: status}
		if !fp.IsBinary() {
			fc.Hunks = newSideHunks(fp.Chunks())
		}
		out = append(out, fc)
	}
	return out, nil
}

// newSideHunks walks a file patch's chunks and returns the new-file line range of
// each contiguous edit region (a run of Add/Delete between Equal chunks). Deletes
// occupy no new-side lines but anchor the region's start.
func newSideHunks(chunks []diff.Chunk) []FileChangeHunk {
	var hunks []FileChangeHunk
	newLine := 1
	inHunk := false
	start, added := 0, 0
	flush := func() {
		if !inHunk {
			return
		}
		end := start
		if added > 0 {
			end = start + added - 1
		}
		hunks = append(hunks, FileChangeHunk{StartLine: start, EndLine: end})
		inHunk, added = false, 0
	}
	for _, ch := range chunks {
		n := countLines(ch.Content())
		switch ch.Type() {
		case diff.Equal:
			flush()
			newLine += n
		case diff.Add:
			if !inHunk {
				start, inHunk = newLine, true
			}
			added += n
			newLine += n
		case diff.Delete:
			if !inHunk {
				start, inHunk = newLine, true
			}
			// deletions occupy no new-side lines
		}
	}
	flush()
	return hunks
}

// CommitDiff returns the unified diff of rev against its first parent (or the
// empty tree for a root commit), capped at maxBytes — a trailing truncation
// marker is appended when it overflows. Feeds the adversarial AI reviewer the
// actual change. Returns ErrNotFound (so a caller can degrade gracefully) when
// rev doesn't resolve.
func (r *Repo) CommitDiff(ctx context.Context, rev string, maxBytes int) (string, error) {
	h, err := r.resolveRevision(rev)
	if err != nil {
		return "", ErrNotFound
	}
	commit, err := r.r.CommitObject(h)
	if err != nil {
		return "", ErrNotFound
	}
	to, err := commit.Tree()
	if err != nil {
		return "", err
	}
	var from *object.Tree // nil == empty tree (root commit → all additions)
	if commit.NumParents() > 0 {
		parent, err := commit.Parent(0)
		if err != nil {
			return "", err
		}
		if from, err = parent.Tree(); err != nil {
			return "", err
		}
	}
	changes, err := object.DiffTreeContext(ctx, from, to)
	if err != nil {
		return "", err
	}
	patch, err := changes.PatchContext(ctx)
	if err != nil {
		return "", err
	}
	s := patch.String()
	if maxBytes > 0 && len(s) > maxBytes {
		s = s[:maxBytes] + "\n… (diff truncated)\n"
	}
	return s, nil
}

// Complexity maps three diff signals to a transparent review-effort band.
// It is a heuristic for routing human attention, not a correctness claim.
//
//	score = churn + 8*files + 4*hunks
//
// Weights reflect that raw churn dominates, but spread across files
// (blast radius) and scattered hunks (context-switching cost) each add
// review burden out of proportion to their line count. Thresholds are
// judgment calls, tuned to feel right on this codebase's commits and meant
// to be adjusted with data. Bands: Low < 60 ≤ Moderate < 250 ≤ High < 800
// ≤ Very High.
func Complexity(churn, files, hunks int) (band string, score int) {
	score = churn + 8*files + 4*hunks
	switch {
	case score >= 800:
		return "Very High", score
	case score >= 250:
		return "High", score
	case score >= 60:
		return "Moderate", score
	default:
		return "Low", score
	}
}

// isGenerated reports whether a repo-relative path is build output or a
// vendored/locked dependency — churn a human never reviews, so it's set
// aside from the headline metrics (the count is still surfaced). Matches by
// path segment so it's project-agnostic: any dist/ or node_modules/ dir,
// vendored trees, minified bundles, sourcemaps, and the common lockfiles.
//
// This is the built-in default layer. A repo can extend it via .gitattributes
// (linguist-generated / linguist-vendored) — see Repo.isExcluded, which ORs
// this with attrSaysGenerated.
func isGenerated(p string) bool {
	p = strings.ToLower(strings.TrimSpace(p))
	if p == "" {
		return false
	}
	// go-git may render a rename as "old => new"; judge the destination.
	if i := strings.Index(p, " => "); i >= 0 {
		p = strings.TrimSpace(p[i+4:])
	}
	for _, seg := range strings.Split(p, "/") {
		switch seg {
		case "node_modules", "dist", "vendor", ".next", ".svelte-kit", ".turbo":
			return true
		}
	}
	if strings.HasSuffix(p, ".min.js") || strings.HasSuffix(p, ".min.css") || strings.HasSuffix(p, ".map") {
		return true
	}
	switch path.Base(p) {
	case "package-lock.json", "yarn.lock", "pnpm-lock.yaml", "go.sum", "composer.lock", "cargo.lock":
		return true
	}
	return false
}

// countLines counts the lines in a diff chunk's content. go-git emits one
// trailing newline per line; a final line without a newline (rare) still
// counts as one.
func countLines(s string) int {
	if s == "" {
		return 0
	}
	n := strings.Count(s, "\n")
	if !strings.HasSuffix(s, "\n") {
		n++
	}
	return n
}

func shortRev(s string) string {
	if len(s) > 7 {
		return s[:7]
	}
	return s
}
