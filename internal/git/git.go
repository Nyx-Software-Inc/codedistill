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

// Package git wraps go-git for the CodeAnchor Files panel. Exposes just
// enough surface to list tracked files, read a file at a specific revision
// (or working copy), and walk a file's commit history.
//
// All paths accepted on the API surface are repo-relative and must not
// escape the worktree. See SafePath for the traversal guard.
package git

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/format/gitattributes"
	"github.com/go-git/go-git/v5/plumbing/object"
)

// ErrNotFound is returned when a path or revision does not resolve in the repo.
var ErrNotFound = errors.New("git: not found")

// ErrPathEscape is returned when a path traverses outside the worktree.
var ErrPathEscape = errors.New("git: path escapes repository root")

// Repo is a thin handle over a go-git repository. Open returns one; callers
// hold it for the duration of a request and don't need to close it.
type Repo struct {
	r    *gogit.Repository
	root string

	// Lazily-loaded .gitattributes matcher for linguist-generated/vendored,
	// used by the code-metrics exclusion. Loaded at most once per Repo.
	genMatcher gitattributes.Matcher
	genOnce    sync.Once
}

// Open returns a Repo handle for the given worktree path. Errors if the path
// is not a git repository. Path is stored absolute for later traversal checks.
func Open(worktree string) (*Repo, error) {
	abs, err := filepath.Abs(worktree)
	if err != nil {
		return nil, fmt.Errorf("abs path: %w", err)
	}
	repo, err := gogit.PlainOpen(abs)
	if err != nil {
		return nil, fmt.Errorf("open repo at %s: %w", abs, err)
	}
	return &Repo{r: repo, root: abs}, nil
}

// HeadSHA returns the full commit SHA at HEAD. Returns ErrNotFound for repos
// with no commits yet (a freshly init'd worktree). Used by the use_case
// mark-implemented flow to auto-fill commit_sha from the project's repo.
func (r *Repo) HeadSHA() (string, error) {
	ref, err := r.r.Head()
	if err != nil {
		if errors.Is(err, plumbing.ErrReferenceNotFound) {
			return "", ErrNotFound
		}
		return "", fmt.Errorf("head: %w", err)
	}
	return ref.Hash().String(), nil
}

// SafePath rejects absolute paths and traversals; returns the cleaned
// relative path (forward-slashed) on success. Used before handing a
// user-supplied path to go-git or the filesystem.
func (r *Repo) SafePath(p string) (string, error) {
	p = strings.TrimSpace(p)
	if p == "" {
		return "", ErrNotFound
	}
	if filepath.IsAbs(p) {
		return "", ErrPathEscape
	}
	cleaned := filepath.ToSlash(filepath.Clean(p))
	if cleaned == "." || cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return "", ErrPathEscape
	}
	return cleaned, nil
}

// Tree returns all tracked file paths at HEAD, sorted. Directories are not
// materialized separately — the client derives structure from path slashes.
// Empty repos (no HEAD) return an empty slice without error.
func (r *Repo) Tree() ([]string, error) {
	head, err := r.r.Head()
	if err != nil {
		// No HEAD = empty repo; return empty tree rather than erroring.
		if errors.Is(err, plumbing.ErrReferenceNotFound) {
			return []string{}, nil
		}
		return nil, fmt.Errorf("head: %w", err)
	}
	commit, err := r.r.CommitObject(head.Hash())
	if err != nil {
		return nil, fmt.Errorf("commit object: %w", err)
	}
	tree, err := commit.Tree()
	if err != nil {
		return nil, fmt.Errorf("tree: %w", err)
	}
	var paths []string
	err = tree.Files().ForEach(func(f *object.File) error {
		paths = append(paths, f.Name)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walk: %w", err)
	}
	sort.Strings(paths)
	return paths, nil
}

// FileContent returns file bytes at the given path and revision.
// Revision "" or "working" reads the working copy from disk; otherwise
// revision is an SHA (short or long) resolved against the object db.
func (r *Repo) FileContent(path, revision string) ([]byte, error) {
	safe, err := r.SafePath(path)
	if err != nil {
		return nil, err
	}
	if revision == "" || revision == "working" {
		full := filepath.Join(r.root, filepath.FromSlash(safe))
		data, err := os.ReadFile(full)
		if err != nil {
			if os.IsNotExist(err) {
				return nil, ErrNotFound
			}
			return nil, err
		}
		return data, nil
	}
	hash, err := r.resolveRevision(revision)
	if err != nil {
		return nil, err
	}
	commit, err := r.r.CommitObject(hash)
	if err != nil {
		return nil, fmt.Errorf("commit %s: %w", revision, err)
	}
	file, err := commit.File(safe)
	if err != nil {
		if errors.Is(err, object.ErrFileNotFound) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("file at %s@%s: %w", safe, revision, err)
	}
	reader, err := file.Reader()
	if err != nil {
		return nil, fmt.Errorf("reader: %w", err)
	}
	defer reader.Close()
	return io.ReadAll(reader)
}

// WorkingMtime returns the working-copy modification time for the given
// path. Used by the code canvas to detect external edits and trigger an
// auto-refresh on the next poll. Historical revisions don't need this —
// their content is immutable.
func (r *Repo) WorkingMtime(path string) (time.Time, error) {
	safe, err := r.SafePath(path)
	if err != nil {
		return time.Time{}, err
	}
	full := filepath.Join(r.root, filepath.FromSlash(safe))
	info, err := os.Stat(full)
	if err != nil {
		if os.IsNotExist(err) {
			return time.Time{}, ErrNotFound
		}
		return time.Time{}, err
	}
	return info.ModTime(), nil
}

// CommitInfo is a flat shape of the commit fields the frontend needs to
// render a commit list and pick a revision.
type CommitInfo struct {
	SHA      string    `json:"sha"`
	ShortSHA string    `json:"short_sha"`
	Author   string    `json:"author"`
	Email    string    `json:"email"`
	Date     time.Time `json:"date"`
	Subject  string    `json:"subject"`
}

// FileCommits walks commits that touched the given path, newest first, up to
// limit entries. limit <= 0 applies a sane default of 50.
func (r *Repo) FileCommits(path string, limit int) ([]CommitInfo, error) {
	safe, err := r.SafePath(path)
	if err != nil {
		return nil, err
	}
	if limit <= 0 {
		limit = 50
	}
	head, err := r.r.Head()
	if err != nil {
		if errors.Is(err, plumbing.ErrReferenceNotFound) {
			return []CommitInfo{}, nil
		}
		return nil, err
	}
	filter := safe
	iter, err := r.r.Log(&gogit.LogOptions{From: head.Hash(), FileName: &filter})
	if err != nil {
		return nil, fmt.Errorf("log: %w", err)
	}
	defer iter.Close()

	out := make([]CommitInfo, 0, limit)
	err = iter.ForEach(func(c *object.Commit) error {
		if len(out) >= limit {
			return errStorerStop
		}
		sha := c.Hash.String()
		short := sha
		if len(short) > 7 {
			short = short[:7]
		}
		out = append(out, CommitInfo{
			SHA:      sha,
			ShortSHA: short,
			Author:   c.Author.Name,
			Email:    c.Author.Email,
			Date:     c.Author.When,
			Subject:  subjectLine(c.Message),
		})
		return nil
	})
	if err != nil && !errors.Is(err, errStorerStop) {
		return nil, fmt.Errorf("iterate: %w", err)
	}
	return out, nil
}

// CommitWithFiles bundles a commit with the files it touched. Used by
// the implementation matcher (priority #3) to build a per-commit signal
// for embedding ("message + filenames"). FilesChanged is sorted; on the
// initial commit (no parent), every tree entry is listed.
type CommitWithFiles struct {
	CommitInfo
	Body         string   `json:"body"`
	FilesChanged []string `json:"files_changed"`
	IsMerge      bool     `json:"is_merge"`
}

// WalkCommitsSince walks the repo's commit history from HEAD newest-first,
// returning commits whose author timestamp is strictly greater than since
// (or every commit when since is nil). Stops at limit if limit > 0.
// Returns an empty slice on a HEAD-less repo.
//
// Each commit carries its full message body + the set of file paths it
// changed (vs. its first parent; for the root commit, the full tree).
// Costs ~one tree-diff per commit, fine for solo-dev scale.
func (r *Repo) WalkCommitsSince(since *time.Time, limit int) ([]CommitWithFiles, error) {
	head, err := r.r.Head()
	if err != nil {
		if errors.Is(err, plumbing.ErrReferenceNotFound) {
			return []CommitWithFiles{}, nil
		}
		return nil, err
	}
	iter, err := r.r.Log(&gogit.LogOptions{From: head.Hash()})
	if err != nil {
		return nil, fmt.Errorf("log: %w", err)
	}
	defer iter.Close()

	out := make([]CommitWithFiles, 0, 16)
	err = iter.ForEach(func(c *object.Commit) error {
		if since != nil && !c.Author.When.After(*since) {
			return errStorerStop
		}
		if limit > 0 && len(out) >= limit {
			return errStorerStop
		}
		files, ferr := commitFiles(c)
		if ferr != nil {
			return ferr
		}
		sha := c.Hash.String()
		short := sha
		if len(short) > 7 {
			short = short[:7]
		}
		out = append(out, CommitWithFiles{
			CommitInfo: CommitInfo{
				SHA:      sha,
				ShortSHA: short,
				Author:   c.Author.Name,
				Email:    c.Author.Email,
				Date:     c.Author.When,
				Subject:  subjectLine(c.Message),
			},
			Body:         strings.TrimSpace(c.Message),
			FilesChanged: files,
			IsMerge:      c.NumParents() > 1,
		})
		return nil
	})
	if err != nil && !errors.Is(err, errStorerStop) {
		return nil, fmt.Errorf("iterate: %w", err)
	}
	return out, nil
}

// revertRe matches the line `git revert` (and go-git's Revert) writes into a
// revert commit's body: "This reverts commit <sha>." We key trust evidence off
// the named SHA, so a reverted change counts against the project's track record.
var revertRe = regexp.MustCompile(`(?m)^This reverts commit ([0-9a-f]{7,40})\b`)

// RevertedCommits scans up to limit recent commits (newest-first from HEAD) for
// git-revert markers and returns the set of reverted commit SHAs as written in
// the messages (usually full 40-hex). Cheap — reads commit messages only, no
// diffing. An empty result is the normal case (most history has no reverts).
func (r *Repo) RevertedCommits(limit int) (map[string]bool, error) {
	head, err := r.r.Head()
	if err != nil {
		if errors.Is(err, plumbing.ErrReferenceNotFound) {
			return map[string]bool{}, nil
		}
		return nil, err
	}
	iter, err := r.r.Log(&gogit.LogOptions{From: head.Hash()})
	if err != nil {
		return nil, fmt.Errorf("log: %w", err)
	}
	defer iter.Close()

	reverted := map[string]bool{}
	seen := 0
	err = iter.ForEach(func(c *object.Commit) error {
		if limit > 0 && seen >= limit {
			return errStorerStop
		}
		seen++
		for _, m := range revertRe.FindAllStringSubmatch(c.Message, -1) {
			reverted[m[1]] = true
		}
		return nil
	})
	if err != nil && !errors.Is(err, errStorerStop) {
		return nil, fmt.Errorf("iterate: %w", err)
	}
	return reverted, nil
}

// commitFiles returns the list of file paths changed by c relative to its
// first parent (or the full tree for the root commit). Sorted, deduped.
func commitFiles(c *object.Commit) ([]string, error) {
	set := map[string]struct{}{}
	if c.NumParents() == 0 {
		tree, err := c.Tree()
		if err != nil {
			return nil, err
		}
		_ = tree.Files().ForEach(func(f *object.File) error {
			set[f.Name] = struct{}{}
			return nil
		})
	} else {
		parent, err := c.Parent(0)
		if err != nil {
			return nil, err
		}
		patch, err := parent.Patch(c)
		if err != nil {
			return nil, err
		}
		for _, fp := range patch.FilePatches() {
			from, to := fp.Files()
			if from != nil {
				set[from.Path()] = struct{}{}
			}
			if to != nil {
				set[to.Path()] = struct{}{}
			}
		}
	}
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	sort.Strings(out)
	return out, nil
}

// errStorerStop is a sentinel used to short-circuit ForEach once `limit` commits
// have been collected. go-git's iterator treats any non-nil error as a halt.
var errStorerStop = errors.New("git: stop iteration (limit reached)")

func subjectLine(msg string) string {
	msg = strings.TrimSpace(msg)
	if i := strings.IndexByte(msg, '\n'); i >= 0 {
		return msg[:i]
	}
	return msg
}

// resolveRevision accepts a short or long SHA and returns the full hash.
// Uses go-git's ResolveRevision to support short SHAs (via disambiguation
// against the object database).
func (r *Repo) resolveRevision(rev string) (plumbing.Hash, error) {
	h, err := r.r.ResolveRevision(plumbing.Revision(rev))
	if err != nil {
		return plumbing.ZeroHash, fmt.Errorf("resolve %q: %w", rev, err)
	}
	return *h, nil
}

// ResolveCommit resolves a short-or-long revision to its full SHA and author
// timestamp. ok is false when the revision doesn't resolve (fabricated /
// rewritten history). Used by drift detection to normalize recorded commit
// anchors to full SHAs and find the earliest-recorded baseline.
func (r *Repo) ResolveCommit(rev string) (sha string, when time.Time, ok bool) {
	h, err := r.resolveRevision(rev)
	if err != nil {
		return "", time.Time{}, false
	}
	c, err := r.r.CommitObject(h)
	if err != nil {
		return "", time.Time{}, false
	}
	return h.String(), c.Author.When, true
}

// branchAttributionMaxCommits caps the ancestor walk per branch tip so a
// pathological repo can't pin a dashboard request. Commits beyond the cap
// simply attribute to whichever branch reached them first (or stay
// unresolved), which is acceptable for a stats panel.
const branchAttributionMaxCommits = 50000

// AttributeCommitsToBranches maps each given commit SHA to the branch the
// work most plausibly happened on, for the dashboard's per-branch panel.
// Returns the sha→branch map (SHAs not reachable from any branch are
// absent) and the default branch name.
//
// Branch refs considered: local branches, plus remote-tracking branches
// that have no local counterpart (short name wins; "origin/HEAD" symbolic
// refs are skipped). The default branch is HEAD's branch when attached,
// else "main"/"master" when present, else the lexically first branch.
//
// Attribution is a heuristic — git keeps no record of which branch a
// commit was made on:
//
//   - reachable from exactly one branch → that branch
//   - reachable from every branch (shared root history) → default branch
//   - otherwise → the most specific containing branch: the non-default
//     branch with the fewest reachable commits (deleted/merged feature
//     branches therefore fold into the default branch)
func (r *Repo) AttributeCommitsToBranches(shas []string) (map[string]string, string, error) {
	tips, defaultBranch, err := r.branchTips()
	if err != nil {
		return nil, "", err
	}
	if len(tips) == 0 {
		return map[string]string{}, defaultBranch, nil
	}

	want := make(map[plumbing.Hash]string, len(shas)) // hash → original sha string
	for _, s := range shas {
		s = strings.TrimSpace(s)
		if len(s) != 40 {
			continue // dashboard rows always carry full SHAs; skip junk
		}
		want[plumbing.NewHash(s)] = s
	}

	// Walk ancestors per branch tip, recording which branches contain
	// each wanted commit and how many commits each branch reaches.
	containing := map[string][]string{} // sha string → branch names
	reachCount := map[string]int{}      // branch name → ancestor count (capped)
	for name, tip := range tips {
		seen := map[plumbing.Hash]struct{}{}
		queue := []plumbing.Hash{tip}
		for len(queue) > 0 && len(seen) < branchAttributionMaxCommits {
			h := queue[len(queue)-1]
			queue = queue[:len(queue)-1]
			if _, ok := seen[h]; ok {
				continue
			}
			seen[h] = struct{}{}
			if s, ok := want[h]; ok {
				containing[s] = append(containing[s], name)
			}
			c, err := r.r.CommitObject(h)
			if err != nil {
				continue // shallow clone edge; treat as walk boundary
			}
			queue = append(queue, c.ParentHashes...)
		}
		reachCount[name] = len(seen)
	}

	out := make(map[string]string, len(containing))
	for sha, branches := range containing {
		out[sha] = pickBranch(branches, defaultBranch, len(tips), reachCount)
	}
	return out, defaultBranch, nil
}

// pickBranch applies the attribution heuristic documented on
// AttributeCommitsToBranches to one commit's containing-branch set.
func pickBranch(branches []string, defaultBranch string, totalBranches int, reachCount map[string]int) string {
	if len(branches) == 1 {
		return branches[0]
	}
	if len(branches) == totalBranches {
		return defaultBranch // shared root history
	}
	best := ""
	for _, b := range branches {
		if b == defaultBranch {
			continue
		}
		if best == "" || reachCount[b] < reachCount[best] {
			best = b
		}
	}
	if best == "" {
		return defaultBranch
	}
	return best
}

// branchTips collects branch name → tip hash and picks the default
// branch. Local branches win over same-named remote-tracking branches.
func (r *Repo) branchTips() (map[string]plumbing.Hash, string, error) {
	tips := map[string]plumbing.Hash{}
	local := map[string]bool{}

	iter, err := r.r.References()
	if err != nil {
		return nil, "", fmt.Errorf("references: %w", err)
	}
	err = iter.ForEach(func(ref *plumbing.Reference) error {
		if ref.Type() != plumbing.HashReference {
			return nil // skip symbolic refs (e.g. origin/HEAD)
		}
		name := ref.Name()
		switch {
		case name.IsBranch():
			short := name.Short()
			tips[short] = ref.Hash()
			local[short] = true
		case name.IsRemote():
			// refs/remotes/<remote>/<branch> → <branch>
			parts := strings.SplitN(strings.TrimPrefix(name.String(), "refs/remotes/"), "/", 2)
			if len(parts) != 2 || parts[1] == "HEAD" {
				return nil
			}
			if !local[parts[1]] {
				tips[parts[1]] = ref.Hash()
			}
		}
		return nil
	})
	if err != nil {
		return nil, "", fmt.Errorf("walk references: %w", err)
	}

	defaultBranch := ""
	if head, err := r.r.Reference(plumbing.HEAD, false); err == nil && head.Type() == plumbing.SymbolicReference {
		if n := head.Target(); n.IsBranch() {
			defaultBranch = n.Short()
		}
	}
	if _, ok := tips[defaultBranch]; !ok || defaultBranch == "" {
		switch {
		case tips["main"] != plumbing.ZeroHash:
			defaultBranch = "main"
		case tips["master"] != plumbing.ZeroHash:
			defaultBranch = "master"
		default:
			names := make([]string, 0, len(tips))
			for n := range tips {
				names = append(names, n)
			}
			sort.Strings(names)
			if len(names) > 0 {
				defaultBranch = names[0]
			}
		}
	}
	return tips, defaultBranch, nil
}
