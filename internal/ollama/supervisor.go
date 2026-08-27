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

package ollama

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

// Supervisor probes the configured endpoint on startup; if the Ollama
// daemon isn't already responding, it spawns `ollama serve` as a child
// process and waits for the port to accept connections. The returned stop
// function tears down only a child we started — if Ollama was already
// running when EnsureRunning was called, stop is a no-op.
//
// Usage in the server boot path:
//
//	sup := &ollama.Supervisor{Endpoint: "http://localhost:11434", Log: log}
//	stop, err := sup.EnsureRunning(ctx)
//	if err != nil { return err }
//	defer stop()
type Supervisor struct {
	// Endpoint is the HTTP URL to probe (e.g. http://localhost:11434).
	Endpoint string
	// BinPath overrides auto-discovery. When empty, discoverBin walks $PATH
	// and the common macOS/Linux install locations.
	BinPath string
	// Log receives structured events. When nil, slog.Default() is used.
	Log *slog.Logger
	// ReadyTimeout bounds how long EnsureRunning waits for the spawned child
	// to start accepting connections. Zero means use the default (30s).
	ReadyTimeout time.Duration
}

const defaultReadyTimeout = 30 * time.Second

// EnsureRunning is the single entry point. Safe to call even when Ollama is
// already up — it probes first and only spawns if the probe fails.
func (s *Supervisor) EnsureRunning(ctx context.Context) (func(), error) {
	log := s.Log
	if log == nil {
		log = slog.Default()
	}
	timeout := s.ReadyTimeout
	if timeout == 0 {
		timeout = defaultReadyTimeout
	}

	if probe(s.Endpoint) == nil {
		log.Info("ollama already running; leaving existing process alone", "endpoint", s.Endpoint)
		return func() {}, nil
	}

	bin := s.BinPath
	if bin == "" {
		found, err := discoverBin()
		if err != nil {
			return nil, fmt.Errorf("ollama isn't running and binary not found: %w (install Ollama or pass -ollama-bin)", err)
		}
		bin = found
	}
	if _, err := os.Stat(bin); err != nil {
		return nil, fmt.Errorf("ollama binary at %q: %w", bin, err)
	}

	log.Info("starting ollama serve", "bin", bin)
	// #nosec G204 -- bin is the operator's -ollama-bin flag or a discovered ollama
	// path, os.Stat-checked above; not remote input.
	cmd := exec.Command(bin, "serve")
	cmd.Env = os.Environ() // pass through OLLAMA_HOST, OLLAMA_MODELS, etc.
	// Ollama writes a fair amount of noise to stderr; drop it so the parent's
	// structured logs stay readable. Flip to os.Stderr for debugging.
	cmd.Stdout = nil
	cmd.Stderr = nil

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start ollama: %w", err)
	}
	stop := func() { stopChild(cmd, log) }

	if err := waitReady(ctx, s.Endpoint, timeout); err != nil {
		stop()
		return nil, err
	}
	log.Info("ollama ready", "pid", cmd.Process.Pid, "endpoint", s.Endpoint)
	return stop, nil
}

// probe is a 2-second GET against the tags endpoint. /api/tags is a cheap
// read that returns 200 once the HTTP server is up — good for readiness.
func probe(endpoint string) error {
	cli := &http.Client{Timeout: 2 * time.Second}
	resp, err := cli.Get(endpoint + "/api/tags")
	if err != nil {
		return err
	}
	resp.Body.Close()
	if resp.StatusCode >= 500 {
		return fmt.Errorf("ollama responded with %d", resp.StatusCode)
	}
	return nil
}

// waitReady polls every 250ms until the endpoint answers or the deadline/ctx
// fires. Returns nil on readiness.
func waitReady(ctx context.Context, endpoint string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if probe(endpoint) == nil {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(250 * time.Millisecond):
		}
	}
	return fmt.Errorf("ollama did not respond on %s within %s", endpoint, timeout)
}

// stopChild sends SIGINT first (Ollama's `serve` subcommand handles it
// cleanly), waits up to 5s for exit, then SIGKILLs. Non-blocking on errors.
func stopChild(cmd *exec.Cmd, log *slog.Logger) {
	if cmd == nil || cmd.Process == nil {
		return
	}
	_ = cmd.Process.Signal(os.Interrupt)
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case <-done:
		log.Info("ollama stopped")
	case <-time.After(5 * time.Second):
		log.Warn("ollama didn't exit after SIGINT; sending SIGKILL")
		_ = cmd.Process.Kill()
		<-done
	}
}

// discoverBin walks $PATH first, then a short list of well-known install
// locations covering the Ollama.app bundle on macOS, Homebrew (both arches),
// and the standalone tarball install into ~/.local.
func discoverBin() (string, error) {
	if p, err := exec.LookPath("ollama"); err == nil {
		return p, nil
	}
	home, _ := os.UserHomeDir()
	candidates := []string{
		"/Applications/Ollama.app/Contents/Resources/ollama", // macOS dmg install
		"/Applications/Ollama.app/Contents/MacOS/ollama",     // alt layout some releases use
		"/opt/homebrew/bin/ollama",                           // Homebrew ARM
		"/usr/local/bin/ollama",                              // Homebrew Intel / generic
		filepath.Join(home, ".local/ollama/bin/ollama"),      // standalone tarball install
		filepath.Join(home, ".ollama/bin/ollama"),
	}
	for _, c := range candidates {
		if info, err := os.Stat(c); err == nil && !info.IsDir() {
			return c, nil
		}
	}
	return "", fmt.Errorf("not on $PATH and none of the common install locations exist")
}
