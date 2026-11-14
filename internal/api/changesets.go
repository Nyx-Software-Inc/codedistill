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
	"errors"
	"net/http"
	"strconv"

	codegit "codedistill/internal/git"
)

// Changesets API — the read side of code review (docs/design/code-review.md):
// what to pick from (branches, commits on a ref), how a branch becomes
// base..head (merge-base), and the diff itself. All gated on Project.RepoRoot
// like the files endpoints, and read-only: nothing here touches the repo.

type branchesResponse struct {
	Branches []codegit.Branch `json:"branches"`
	Default  string           `json:"default"`
}

func (s *Server) listBranches(w http.ResponseWriter, r *http.Request) {
	repo, ok := s.openProjectRepo(w, r)
	if !ok {
		return
	}
	bs, def, err := repo.Branches()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, branchesResponse{Branches: bs, Default: def})
}

// listCommits: GET /files/commits?ref=&path=&limit=. Either ref or path may
// be empty — ref "" is HEAD, path "" is every commit. The old endpoint
// required path; existing callers still pass it and get what they always did.
func (s *Server) listCommits(w http.ResponseWriter, r *http.Request) {
	repo, ok := s.openProjectRepo(w, r)
	if !ok {
		return
	}
	q := r.URL.Query()
	limit := 50
	if v := q.Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n <= 0 {
			writeMsg(w, http.StatusBadRequest, "limit must be a positive integer")
			return
		}
		limit = n
	}
	commits, err := repo.Commits(q.Get("ref"), q.Get("path"), limit)
	if err != nil {
		writeErr(w, gitStatus(err), err)
		return
	}
	writeJSON(w, http.StatusOK, commits)
}

type mergeBaseResponse struct {
	SHA string `json:"sha"`
}

// getMergeBase: GET /changesets/merge-base?head=<ref>&target=<ref>. The base
// of a branch changeset is its merge-base with the branch it will land on.
func (s *Server) getMergeBase(w http.ResponseWriter, r *http.Request) {
	repo, ok := s.openProjectRepo(w, r)
	if !ok {
		return
	}
	head, target := r.URL.Query().Get("head"), r.URL.Query().Get("target")
	if head == "" || target == "" {
		writeMsg(w, http.StatusBadRequest, "head and target query params are required")
		return
	}
	sha, err := repo.MergeBase(head, target)
	if err != nil {
		writeErr(w, gitStatus(err), err)
		return
	}
	writeJSON(w, http.StatusOK, mergeBaseResponse{SHA: sha})
}

// getChangesetDiff: GET /changesets/diff?base=&head=&context=&path=.
//
//	head     a revision, or "working" / empty for the working copy
//	base     a revision; empty means head's parent (HEAD for the working copy)
//	context  lines per hunk (default 3), or "full" for whole files
//	path     one file only; generated files get hunks only this way
func (s *Server) getChangesetDiff(w http.ResponseWriter, r *http.Request) {
	repo, ok := s.openProjectRepo(w, r)
	if !ok {
		return
	}
	q := r.URL.Query()
	ctxLines := codegit.DefaultContext
	switch v := q.Get("context"); v {
	case "":
	case "full":
		ctxLines = codegit.FullContext
	default:
		n, err := strconv.Atoi(v)
		if err != nil || n < 0 {
			writeMsg(w, http.StatusBadRequest, `context must be a non-negative integer or "full"`)
			return
		}
		ctxLines = n
	}
	cs, err := repo.Diff(r.Context(), q.Get("base"), q.Get("head"), ctxLines, q.Get("path"))
	if err != nil {
		writeErr(w, gitStatus(err), err)
		return
	}
	writeJSON(w, http.StatusOK, cs)
}

// gitStatus maps the git package's sentinel errors to HTTP the way the files
// handlers do, so every repo-reading endpoint answers alike.
func gitStatus(err error) int {
	switch {
	case errors.Is(err, codegit.ErrNotFound):
		return http.StatusNotFound
	case errors.Is(err, codegit.ErrPathEscape):
		return http.StatusBadRequest
	}
	return http.StatusInternalServerError
}
