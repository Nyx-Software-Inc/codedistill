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
)

// TestFsBrowse exercises the directory picker endpoint: listing,
// git-repo flagging, dot-dir skipping, parent computation, and the
// error statuses for relative / missing / non-dir paths.
func TestFsBrowse(t *testing.T) {
	srv, _ := setup(t)
	root := t.TempDir()

	for _, d := range []string{"repo/.git", "plain", ".hidden"} {
		if err := os.MkdirAll(filepath.Join(root, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "afile.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	var out struct {
		Path   string `json:"path"`
		Parent string `json:"parent"`
		Dirs   []struct {
			Name      string `json:"name"`
			IsGitRepo bool   `json:"is_git_repo"`
		} `json:"dirs"`
	}
	doJSON(t, srv, "GET", "/api/v1/fs/browse?path="+url.QueryEscape(root), nil, 200, &out)
	if out.Path != root || out.Parent != filepath.Dir(root) {
		t.Errorf("path/parent = %q/%q, want %q/%q", out.Path, out.Parent, root, filepath.Dir(root))
	}
	if len(out.Dirs) != 2 {
		t.Fatalf("dirs = %+v, want plain + repo (dot-dirs and files skipped)", out.Dirs)
	}
	// Sorted: plain, repo.
	if out.Dirs[0].Name != "plain" || out.Dirs[0].IsGitRepo {
		t.Errorf("dirs[0] = %+v, want plain (not a repo)", out.Dirs[0])
	}
	if out.Dirs[1].Name != "repo" || !out.Dirs[1].IsGitRepo {
		t.Errorf("dirs[1] = %+v, want repo (git)", out.Dirs[1])
	}

	// Default path (no param) resolves home and succeeds.
	doJSON(t, srv, "GET", "/api/v1/fs/browse", nil, 200, &out)
	if !filepath.IsAbs(out.Path) {
		t.Errorf("default path = %q, want absolute home", out.Path)
	}

	doJSON(t, srv, "GET", "/api/v1/fs/browse?path=relative/dir", nil, 400, nil)
	doJSON(t, srv, "GET", "/api/v1/fs/browse?path="+url.QueryEscape(filepath.Join(root, "nope")), nil, 404, nil)
	doJSON(t, srv, "GET", "/api/v1/fs/browse?path="+url.QueryEscape(filepath.Join(root, "afile.txt")), nil, 400, nil)
}
