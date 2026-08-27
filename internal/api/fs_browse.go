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
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// fsBrowse backs the repo-root directory picker in the Files panel
// (Backlog bug #4): GET /api/v1/fs/browse?path=/abs/dir lists the
// subdirectories of one directory so the user can navigate to a repo
// instead of typing an absolute path. No path → the user's home dir.
//
// SECURITY: this intentionally exposes server-side directory names
// (never file contents) to whoever can reach the HTTP port. That's the
// deal for a local single-user app where browser and server share a
// machine. When server/multi-user mode lands, this endpoint MUST be
// gated off (it pairs naturally with the features.MultiUser boundary)
// — a remote user has no business browsing the host filesystem.

type fsBrowseDir struct {
	Name string `json:"name"`
	// IsGitRepo flags directories containing a .git entry (dir or
	// worktree file) so the picker can highlight candidate repo roots.
	IsGitRepo bool `json:"is_git_repo"`
}

type fsBrowseResponse struct {
	Path string `json:"path"`
	// Parent is empty when Path is the filesystem root.
	Parent string        `json:"parent,omitempty"`
	Dirs   []fsBrowseDir `json:"dirs"`
}

func (s *Server) fsBrowse(w http.ResponseWriter, r *http.Request) {
	// The gate this file's SECURITY note demanded (audit C1): on a
	// multi-user server, browsing the HOST filesystem is an admin-only
	// operation (admins configure repo paths); a remote member — let alone
	// an anonymous client — has no business walking the server's disk.
	// Single-user installs (no auth configured) keep the local-app deal.
	if s.auth != nil && !s.isAdmin(r) {
		writeMsg(w, http.StatusForbidden, "filesystem browsing on a multi-user server requires an admin")
		return
	}
	path := strings.TrimSpace(r.URL.Query().Get("path"))
	if path == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			writeMsg(w, http.StatusInternalServerError, "cannot resolve home directory: "+err.Error())
			return
		}
		path = home
	}
	if !filepath.IsAbs(path) {
		writeMsg(w, http.StatusBadRequest, "path must be absolute")
		return
	}
	path = filepath.Clean(path)

	entries, err := os.ReadDir(path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		writeMsg(w, http.StatusNotFound, "no such directory: "+path)
		return
	case errors.Is(err, fs.ErrPermission):
		writeMsg(w, http.StatusForbidden, "permission denied: "+path)
		return
	case err != nil:
		// os.ReadDir on a file returns ENOTDIR — treat anything else
		// unexpected as a bad request with the OS detail.
		writeMsg(w, http.StatusBadRequest, err.Error())
		return
	}

	resp := fsBrowseResponse{Path: path, Dirs: []fsBrowseDir{}}
	if parent := filepath.Dir(path); parent != path {
		resp.Parent = parent
	}
	for _, e := range entries {
		// Dot-dirs are skipped — repo roots under hidden trees are rare
		// and the noise (.cache, .config, …) buries everything else.
		if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		_, gitErr := os.Stat(filepath.Join(path, e.Name(), ".git"))
		resp.Dirs = append(resp.Dirs, fsBrowseDir{
			Name:      e.Name(),
			IsGitRepo: gitErr == nil,
		})
	}
	sort.Slice(resp.Dirs, func(i, j int) bool { return resp.Dirs[i].Name < resp.Dirs[j].Name })
	writeJSON(w, http.StatusOK, resp)
}
