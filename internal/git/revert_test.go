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
	"os"
	"path/filepath"
	"testing"
	"time"

	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"
)

func TestRevertedCommits(t *testing.T) {
	dir := t.TempDir()
	gr, err := gogit.PlainInit(dir, false)
	if err != nil {
		t.Fatalf("plain init: %v", err)
	}
	wt, _ := gr.Worktree()
	file := filepath.Join(dir, "f.go")
	sig := func(s int64) *object.Signature {
		return &object.Signature{Name: "T", Email: "t@e.com", When: time.Unix(1_700_000_000+s, 0)}
	}
	commit := func(content, msg string, s int64) string {
		if err := os.WriteFile(file, []byte(content), 0644); err != nil {
			t.Fatalf("write: %v", err)
		}
		if _, err := wt.Add("f.go"); err != nil {
			t.Fatalf("add: %v", err)
		}
		h, err := wt.Commit(msg, &gogit.CommitOptions{Author: sig(s)})
		if err != nil {
			t.Fatalf("commit: %v", err)
		}
		return h.String()
	}

	commit("v1\n", "add f", 0)
	bad := commit("v2\n", "risky change", 10)
	// A revert commit, exactly as `git revert` writes its body.
	commit("v1\n", "Revert \"risky change\"\n\nThis reverts commit "+bad+".", 20)

	r, err := Open(dir)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	reverted, err := r.RevertedCommits(100)
	if err != nil {
		t.Fatalf("RevertedCommits: %v", err)
	}
	if !reverted[bad] {
		t.Errorf("expected %s in reverted set, got %v", bad, reverted)
	}
	if len(reverted) != 1 {
		t.Errorf("expected exactly one reverted commit, got %d: %v", len(reverted), reverted)
	}
}
