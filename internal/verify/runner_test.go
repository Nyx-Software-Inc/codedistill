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

package verify

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"codedistill/internal/domain"
)

// initRepo builds a throwaway git repo whose single commit contains a file
// "marker.txt". Returns the repo root and the commit SHA. Skips the test if
// git isn't available.
func initRepo(t *testing.T) (root, sha string) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	root = t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", root}, args...)...)
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
		}
	}
	run("init", "-q")
	if err := os.WriteFile(filepath.Join(root, "marker.txt"), []byte("present"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", "-A")
	run("commit", "-q", "-m", "initial")
	out, err := exec.Command("git", "-C", root, "rev-parse", "HEAD").Output()
	if err != nil {
		t.Fatalf("rev-parse: %v", err)
	}
	return root, strings.TrimSpace(string(out))
}

func TestRunPass(t *testing.T) {
	root, sha := initRepo(t)
	got := Run(context.Background(), Request{RepoRoot: root, CommitSHA: sha, Command: "exit 0"})
	if got.Verdict != domain.VerifyVerdictPass {
		t.Fatalf("verdict = %q, want pass (%+v)", got.Verdict, got)
	}
	if got.ExitCode == nil || *got.ExitCode != 0 {
		t.Errorf("exit code = %v, want 0", got.ExitCode)
	}
}

func TestRunFail(t *testing.T) {
	root, sha := initRepo(t)
	got := Run(context.Background(), Request{RepoRoot: root, CommitSHA: sha, Command: "exit 3"})
	if got.Verdict != domain.VerifyVerdictFail {
		t.Fatalf("verdict = %q, want fail", got.Verdict)
	}
	if got.ExitCode == nil || *got.ExitCode != 3 {
		t.Errorf("exit code = %v, want 3", got.ExitCode)
	}
}

// The command runs against a checkout of the commit, so a file committed at
// that commit is present in the worktree.
func TestRunChecksOutCommit(t *testing.T) {
	root, sha := initRepo(t)
	got := Run(context.Background(), Request{RepoRoot: root, CommitSHA: sha, Command: "cat marker.txt"})
	if got.Verdict != domain.VerifyVerdictPass {
		t.Fatalf("verdict = %q, want pass (%+v)", got.Verdict, got)
	}
	if !strings.Contains(got.Output, "present") {
		t.Errorf("output missing marker content: %q", got.Output)
	}
}

func TestRunBadSHA(t *testing.T) {
	root, _ := initRepo(t)
	got := Run(context.Background(), Request{RepoRoot: root, CommitSHA: "deadbeefdeadbeefdeadbeefdeadbeefdeadbeef", Command: "exit 0"})
	if got.Verdict != domain.VerifyVerdictError {
		t.Fatalf("verdict = %q, want error", got.Verdict)
	}
	if got.ExitCode != nil {
		t.Errorf("exit code = %v, want nil for harness error", got.ExitCode)
	}
}

func TestRunTimeout(t *testing.T) {
	root, sha := initRepo(t)
	got := Run(context.Background(), Request{
		RepoRoot: root, CommitSHA: sha, Command: "sleep 5", Timeout: 100 * time.Millisecond,
	})
	if got.Verdict != domain.VerifyVerdictError {
		t.Fatalf("verdict = %q, want error", got.Verdict)
	}
	if !strings.Contains(got.Summary, "timed out") {
		t.Errorf("summary = %q, want timeout note", got.Summary)
	}
}

func TestRunNoCommand(t *testing.T) {
	got := Run(context.Background(), Request{RepoRoot: t.TempDir(), CommitSHA: "abc1234"})
	if got.Verdict != domain.VerifyVerdictError {
		t.Fatalf("verdict = %q, want error", got.Verdict)
	}
}

// A Session runs multiple checks in one checkout and leaves no worktree behind.
func TestSessionMultipleChecks(t *testing.T) {
	root, sha := initRepo(t)
	sess, err := Open(context.Background(), root, sha)
	if err != nil {
		t.Fatalf("open: %v", err)
	}

	pass := sess.Run(context.Background(), "cat marker.txt", 0, 0)
	fail := sess.Run(context.Background(), "exit 2", 0, 0)
	sess.Close()

	if pass.Verdict != domain.VerifyVerdictPass || !strings.Contains(pass.Output, "present") {
		t.Errorf("first check: verdict=%q output=%q", pass.Verdict, pass.Output)
	}
	if fail.Verdict != domain.VerifyVerdictFail || fail.ExitCode == nil || *fail.ExitCode != 2 {
		t.Errorf("second check: verdict=%q exit=%v", fail.Verdict, fail.ExitCode)
	}

	out, _ := exec.Command("git", "-C", root, "worktree", "list", "--porcelain").Output()
	if n := strings.Count(string(out), "worktree "); n != 1 {
		t.Errorf("expected 1 worktree after Close, found %d:\n%s", n, out)
	}
}

// Open against a bogus commit fails cleanly (caller turns it into error verdicts).
func TestSessionOpenBadSHA(t *testing.T) {
	root, _ := initRepo(t)
	if _, err := Open(context.Background(), root, "deadbeefdeadbeefdeadbeefdeadbeefdeadbeef"); err == nil {
		t.Fatal("expected Open to fail on a non-existent commit")
	}
}

// No worktree leaks behind after a run.
func TestRunCleansUpWorktree(t *testing.T) {
	root, sha := initRepo(t)
	Run(context.Background(), Request{RepoRoot: root, CommitSHA: sha, Command: "exit 0"})
	out, err := exec.Command("git", "-C", root, "worktree", "list", "--porcelain").Output()
	if err != nil {
		t.Fatalf("worktree list: %v", err)
	}
	// Only the main worktree should remain (one "worktree " line).
	if n := strings.Count(string(out), "worktree "); n != 1 {
		t.Errorf("expected 1 worktree, found %d:\n%s", n, out)
	}
}

// A command-not-found (exit 127) is the tool missing, not the check failing:
// it must map to "skipped", never "fail" — a false red undermines the
// "green is earned" signal (Bug-95 walkthrough finding).
func TestRunToolNotFoundIsSkipped(t *testing.T) {
	root, sha := initRepo(t)
	got := Run(context.Background(), Request{RepoRoot: root, CommitSHA: sha, Command: "definitely-not-an-installed-tool --run"})
	if got.Verdict != domain.VerifyVerdictSkipped {
		t.Fatalf("verdict = %q, want skipped (exit 127 = tool not found)", got.Verdict)
	}
	if got.ExitCode == nil || *got.ExitCode != 127 {
		t.Errorf("exit code = %v, want 127", got.ExitCode)
	}
	// A genuine non-zero (the tool ran and failed) is still a fail.
	if f := Run(context.Background(), Request{RepoRoot: root, CommitSHA: sha, Command: "exit 1"}); f.Verdict != domain.VerifyVerdictFail {
		t.Errorf("exit 1 verdict = %q, want fail", f.Verdict)
	}
}
