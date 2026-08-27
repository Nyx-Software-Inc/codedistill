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

// Package verify is the deterministic layer of the glass-box verification
// portfolio (Phase 3, slice 1). It runs a project's configured check command
// against the exact commit an item was implemented at, in an isolated git
// worktree, and returns a structured verdict.
//
// Isolation is the point: the command runs on a detached checkout of the
// recorded commit in a temp worktree, never the user's working tree. So a
// "pass" means the tests were green AT that commit — not green because of
// uncommitted local edits — and the user's checkout is never disturbed.
//
// Security envelope (slice 1, local single-user desktop): the command is
// user-configured per project, runs locally via `sh -c`, is timeout-bounded,
// and its output is size-capped. Hosted/multi-user sandboxing is a later
// concern. Nothing runs unless a command is configured (the caller checks).
package verify

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"codedistill/internal/domain"
)

// DefaultTimeout bounds a single verification run. DefaultMaxOutput caps the
// captured stdout+stderr so a chatty suite can't bloat the DB.
const (
	DefaultTimeout   = 5 * time.Minute
	DefaultMaxOutput = 64 * 1024 // 64 KiB
)

// Request is one deterministic verification to run.
type Request struct {
	RepoRoot  string        // the project's git repo on disk (cwd of the worktree)
	CommitSHA string        // the commit to check out and run against
	Command   string        // the check, run via `sh -c` (e.g. "go test ./...")
	Timeout   time.Duration // 0 → DefaultTimeout
	MaxOutput int           // 0 → DefaultMaxOutput
}

// Outcome is the verdict of a run. Verdict is always one of the domain
// VerifyVerdict* terminal values (never "running"); a harness failure that
// never reached the command maps to VerifyVerdictError with ExitCode nil.
type Outcome struct {
	Verdict  string
	ExitCode *int
	Summary  string
	Output   string
	Duration time.Duration
}

// Run executes req against a one-off worktree and always returns an Outcome —
// every failure mode (missing commit, checkout failure, timeout,
// command-not-found, non-zero exit) is folded into a verdict so the caller
// always has a result to persist. It never returns an error. For multiple
// checks at the same commit, Open a Session once instead.
func Run(ctx context.Context, req Request) Outcome {
	if req.Command == "" {
		return errorOutcome("no verification command configured")
	}
	if req.CommitSHA == "" {
		return errorOutcome("no commit to verify against")
	}
	sess, err := Open(ctx, req.RepoRoot, req.CommitSHA)
	if err != nil {
		return errorOutcome(err.Error())
	}
	defer sess.Close()
	return sess.Run(ctx, req.Command, req.Timeout, req.MaxOutput)
}

// Session is an isolated checkout of a commit that can run several checks before
// teardown — so the item-level suite and every per-criterion command share one
// worktree (one checkout, not N). Open it once, Run each check, always Close.
type Session struct {
	repoRoot string
	parent   string // temp parent dir to remove on Close
	worktree string // the detached checkout; the cwd for every check
}

// Open creates an isolated worktree checked out at commitSHA. The returned error
// is a clean, caller-facing message (most often: the commit doesn't exist —
// fabricated or rewritten history); the caller turns it into error verdicts.
func Open(ctx context.Context, repoRoot, commitSHA string) (*Session, error) {
	if fi, err := os.Stat(repoRoot); err != nil || !fi.IsDir() {
		return nil, fmt.Errorf("repo root %q is not a directory", repoRoot)
	}
	// git creates the leaf dir, so hand it a non-existent path under a fresh parent.
	parent, err := os.MkdirTemp("", "cd-verify-")
	if err != nil {
		return nil, fmt.Errorf("create temp dir: %w", err)
	}
	worktree := filepath.Join(parent, "wt")
	if out, addErr := addWorktree(ctx, repoRoot, worktree, commitSHA); addErr != nil {
		os.RemoveAll(parent)
		return nil, fmt.Errorf("could not check out commit %s: %s", shortSHA(commitSHA), firstLine(out, addErr))
	}
	return &Session{repoRoot: repoRoot, parent: parent, worktree: worktree}, nil
}

// Close tears the worktree down. Best-effort; safe to call once.
func (s *Session) Close() {
	removeWorktree(s.repoRoot, s.worktree)
	os.RemoveAll(s.parent)
}

// Run executes one command in the session's checkout and returns its verdict.
// Each call is independently timeout- and output-bounded; duration measures the
// command alone (the shared checkout cost isn't charged per check).
func (s *Session) Run(ctx context.Context, command string, timeout time.Duration, maxOut int) Outcome {
	if command == "" {
		return errorOutcome("no verification command configured")
	}
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	if maxOut <= 0 {
		maxOut = DefaultMaxOutput
	}
	start := time.Now()
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	capOut := &capWriter{limit: maxOut}
	// #nosec G204 -- command is the operator-configured verify.test_command, run in
	// an isolated worktree with a timeout and output cap. Executing it is the feature.
	cmd := exec.CommandContext(runCtx, "sh", "-c", command)
	cmd.Dir = s.worktree
	cmd.Stdout = capOut
	cmd.Stderr = capOut

	runErr := cmd.Run()
	dur := time.Since(start)
	output := capOut.String()

	// Timeout: the command was killed because runCtx expired.
	if errors.Is(runCtx.Err(), context.DeadlineExceeded) {
		return Outcome{
			Verdict:  domain.VerifyVerdictError,
			Summary:  fmt.Sprintf("timed out after %s", timeout),
			Output:   output,
			Duration: dur,
		}
	}
	if runErr == nil {
		code := 0
		return Outcome{
			Verdict:  domain.VerifyVerdictPass,
			ExitCode: &code,
			Summary:  "passed (exit 0)",
			Output:   output,
			Duration: dur,
		}
	}
	var exitErr *exec.ExitError
	if errors.As(runErr, &exitErr) {
		code := exitErr.ExitCode()
		// Exit 127 (command not found) / 126 (found, not executable) mean the
		// TOOL couldn't run, not that the check failed — a missing scanner
		// must not read as a red fail. Report it as skipped with a hint.
		if code == 127 || code == 126 {
			return Outcome{
				Verdict:  domain.VerifyVerdictSkipped,
				ExitCode: &code,
				Summary:  fmt.Sprintf("skipped — tool not found (exit %d); install it and re-run", code),
				Output:   output,
				Duration: dur,
			}
		}
		// A non-zero exit means the command ran and reported failure → fail.
		return Outcome{
			Verdict:  domain.VerifyVerdictFail,
			ExitCode: &code,
			Summary:  fmt.Sprintf("failed (exit %d)", code),
			Output:   output,
			Duration: dur,
		}
	}
	// Anything else (sh missing, couldn't start) is a harness error.
	return Outcome{
		Verdict:  domain.VerifyVerdictError,
		Summary:  "could not run command: " + runErr.Error(),
		Output:   output,
		Duration: dur,
	}
}

func errorOutcome(summary string) Outcome {
	return Outcome{Verdict: domain.VerifyVerdictError, Summary: summary}
}

func addWorktree(ctx context.Context, repoRoot, worktree, commit string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "git", "-C", repoRoot, "worktree", "add", "--detach", worktree, commit)
	return cmd.CombinedOutput()
}

// removeWorktree tears down the worktree. Best-effort: a fresh short context so
// it isn't pre-cancelled by a run timeout, and a prune to clear the admin entry
// even if the dir was already gone.
func removeWorktree(repoRoot, worktree string) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	_ = exec.CommandContext(ctx, "git", "-C", repoRoot, "worktree", "remove", "--force", worktree).Run()
	_ = exec.CommandContext(ctx, "git", "-C", repoRoot, "worktree", "prune").Run()
}

// capWriter buffers up to limit bytes and silently drops the rest, recording
// that truncation happened. It always reports a full write so the child process
// is never blocked or errored by a closed pipe.
type capWriter struct {
	buf       bytes.Buffer
	limit     int
	truncated bool
}

func (w *capWriter) Write(p []byte) (int, error) {
	if remaining := w.limit - w.buf.Len(); remaining > 0 {
		if len(p) > remaining {
			w.buf.Write(p[:remaining])
			w.truncated = true
		} else {
			w.buf.Write(p)
		}
	} else if len(p) > 0 {
		w.truncated = true
	}
	return len(p), nil
}

func (w *capWriter) String() string {
	if w.truncated {
		return w.buf.String() + "\n… (output truncated)"
	}
	return w.buf.String()
}

func shortSHA(s string) string {
	if len(s) > 12 {
		return s[:12]
	}
	return s
}

func firstLine(out []byte, fallback error) string {
	s := string(bytes.TrimSpace(out))
	if s == "" {
		if fallback != nil {
			return fallback.Error()
		}
		return "unknown error"
	}
	if i := bytes.IndexByte([]byte(s), '\n'); i >= 0 {
		return s[:i]
	}
	return s
}
