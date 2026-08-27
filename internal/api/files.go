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
	"fmt"
	"net/http"
	"strconv"
	"time"
	"unicode/utf8"

	codegit "codedistill/internal/git"
)

// Files API surfaces the project's configured git worktree to the frontend.
// All three endpoints gate on Project.RepoRoot being set and pointing at a
// real git repo — returning 409 otherwise so the client can show the
// "Configure repository root" form.

type fileTreeResponse struct {
	Paths []string `json:"paths"`
}

type fileContentResponse struct {
	Path     string `json:"path"`
	Revision string `json:"revision"` // echoed back ("working" when unset)
	Content  string `json:"content"`
	// Binary is set when content is not valid UTF-8; the client renders a
	// "cannot preview binary file" placeholder instead of garbage bytes.
	Binary bool `json:"binary"`
	// Mtime is the working-copy file mtime in RFC3339Nano. Set only for
	// working-copy reads; the code canvas polls this to detect external
	// edits. Empty for historical revisions (their content is immutable).
	Mtime string `json:"mtime,omitempty"`
}

// openProjectRepo loads the project, verifies repo_root is set, and returns
// the opened git.Repo plus any HTTP error details already written.
func (s *Server) openProjectRepo(w http.ResponseWriter, r *http.Request) (*codegit.Repo, bool) {
	pid := r.PathValue("pid")
	p, err := s.store.GetProject(r.Context(), pid)
	if err != nil {
		writeErr(w, statusFor(err), err)
		return nil, false
	}
	if p.RepoRoot == "" {
		writeMsg(w, http.StatusConflict, "project has no repo_root configured")
		return nil, false
	}
	repo, err := codegit.Open(p.RepoRoot)
	if err != nil {
		writeMsg(w, http.StatusConflict, fmt.Sprintf("repo_root %q is not a git repo: %v", p.RepoRoot, err))
		return nil, false
	}
	return repo, true
}

func (s *Server) listFiles(w http.ResponseWriter, r *http.Request) {
	repo, ok := s.openProjectRepo(w, r)
	if !ok {
		return
	}
	paths, err := repo.Tree()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, fileTreeResponse{Paths: paths})
}

func (s *Server) getFileContent(w http.ResponseWriter, r *http.Request) {
	repo, ok := s.openProjectRepo(w, r)
	if !ok {
		return
	}
	path := r.URL.Query().Get("path")
	if path == "" {
		writeMsg(w, http.StatusBadRequest, "path query param is required")
		return
	}
	revision := r.URL.Query().Get("revision") // "" → working copy

	data, err := repo.FileContent(path, revision)
	if err != nil {
		status := http.StatusInternalServerError
		switch {
		case errors.Is(err, codegit.ErrNotFound):
			status = http.StatusNotFound
		case errors.Is(err, codegit.ErrPathEscape):
			status = http.StatusBadRequest
		}
		writeErr(w, status, err)
		return
	}
	echoed := revision
	if echoed == "" {
		echoed = "working"
	}
	resp := fileContentResponse{Path: path, Revision: echoed}
	if utf8.Valid(data) {
		resp.Content = string(data)
	} else {
		resp.Binary = true
	}
	if revision == "" {
		if t, err := repo.WorkingMtime(path); err == nil {
			resp.Mtime = t.UTC().Format(time.RFC3339Nano)
		}
	}
	writeJSON(w, http.StatusOK, resp)
}

type fileMtimeResponse struct {
	Path  string `json:"path"`
	Mtime string `json:"mtime"`
}

// getFileMtime returns just the working-copy mtime — cheap enough to poll
// every few seconds. The code canvas calls this on its tick and only
// refetches content when the mtime advances.
func (s *Server) getFileMtime(w http.ResponseWriter, r *http.Request) {
	repo, ok := s.openProjectRepo(w, r)
	if !ok {
		return
	}
	path := r.URL.Query().Get("path")
	if path == "" {
		writeMsg(w, http.StatusBadRequest, "path query param is required")
		return
	}
	t, err := repo.WorkingMtime(path)
	if err != nil {
		status := http.StatusInternalServerError
		switch {
		case errors.Is(err, codegit.ErrNotFound):
			status = http.StatusNotFound
		case errors.Is(err, codegit.ErrPathEscape):
			status = http.StatusBadRequest
		}
		writeErr(w, status, err)
		return
	}
	writeJSON(w, http.StatusOK, fileMtimeResponse{Path: path, Mtime: t.UTC().Format(time.RFC3339Nano)})
}

func (s *Server) listFileCommits(w http.ResponseWriter, r *http.Request) {
	repo, ok := s.openProjectRepo(w, r)
	if !ok {
		return
	}
	path := r.URL.Query().Get("path")
	if path == "" {
		writeMsg(w, http.StatusBadRequest, "path query param is required")
		return
	}
	limit := 50
	if v := r.URL.Query().Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n <= 0 {
			writeMsg(w, http.StatusBadRequest, "limit must be a positive integer")
			return
		}
		limit = n
	}
	commits, err := repo.FileCommits(path, limit)
	if err != nil {
		status := http.StatusInternalServerError
		switch {
		case errors.Is(err, codegit.ErrNotFound):
			status = http.StatusNotFound
		case errors.Is(err, codegit.ErrPathEscape):
			status = http.StatusBadRequest
		}
		writeErr(w, status, err)
		return
	}
	writeJSON(w, http.StatusOK, commits)
}
