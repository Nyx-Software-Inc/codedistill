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

// Desktop app window: serve spawns the UI as a Chromium-family --app window
// and ties the two lifetimes together by PROCESS SUPERVISION, not browser
// events (which cannot distinguish close from reload): window closed -> child
// exits -> serve shuts down; serve exits -> the window is closed. A dedicated
// --user-data-dir forces a separate browser process we own — launching the
// user's installed PWA would hand off to an already-running browser instance
// and break the supervision. All state lives server-side, so the dedicated
// profile is invisible in practice.

package main

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"
)

// resolveAppMode maps the -app flag (auto|on|off) to a decision. Auto turns
// the window on only for an interactive desktop session: stdout is a terminal
// (a systemd unit or redirected log is not) and a display exists. The server
// package and headless boxes therefore never sprout windows.
func resolveAppMode(flagVal string) bool {
	switch flagVal {
	case "on":
		return true
	case "off":
		return false
	}
	if fi, err := os.Stdout.Stat(); err != nil || fi.Mode()&os.ModeCharDevice == 0 {
		return false
	}
	if runtime.GOOS == "linux" && os.Getenv("DISPLAY") == "" && os.Getenv("WAYLAND_DISPLAY") == "" {
		return false
	}
	return true
}

// appURL derives the browser-facing URL from the serve bind address: a
// wildcard or empty host becomes 127.0.0.1 (the window always talks loopback).
func appURL(addr string) string {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return "http://" + addr
	}
	switch host {
	case "", "0.0.0.0", "::", "[::]":
		host = "127.0.0.1"
	}
	return fmt.Sprintf("http://%s", net.JoinHostPort(host, port))
}

// discoverAppBrowser finds a Chromium-family binary — they all support
// --app + --user-data-dir. Order: the mainstream ones first.
func discoverAppBrowser() (string, bool) {
	names := []string{
		"google-chrome", "google-chrome-stable", "chromium", "chromium-browser",
		"brave-browser", "microsoft-edge", "vivaldi",
	}
	if runtime.GOOS == "darwin" {
		for _, p := range []string{
			"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
			"/Applications/Chromium.app/Contents/MacOS/Chromium",
			"/Applications/Brave Browser.app/Contents/MacOS/Brave Browser",
			"/Applications/Microsoft Edge.app/Contents/MacOS/Microsoft Edge",
		} {
			if info, err := os.Stat(p); err == nil && !info.IsDir() {
				return p, true
			}
		}
	}
	for _, n := range names {
		if p, err := exec.LookPath(n); err == nil {
			return p, true
		}
	}
	return "", false
}

// launchAppWindow waits for the server to answer, then opens the UI. With a
// Chromium-family browser it returns a non-nil done channel that closes when
// the window's process exits (the caller treats that as a shutdown signal) and
// a stop func that closes the window. Without one it falls back to the default
// browser — UI opens, lifecycles stay uncoupled — and done is nil.
func launchAppWindow(ctx context.Context, addr string, log *slog.Logger) (done <-chan struct{}, stop func()) {
	url := appURL(addr)
	if !waitReachable(ctx, url, 5*time.Second) {
		log.Warn("app window: server not answering yet; opening anyway", "url", url)
	}

	bin, ok := discoverAppBrowser()
	if !ok {
		log.Warn("app window: no Chromium-family browser found — opening a plain tab; window/server lifetimes stay uncoupled")
		openInDefaultBrowser(url, log)
		return nil, func() {}
	}

	profile := appProfileDir()
	cmd := exec.Command(bin,
		"--app="+url,
		"--user-data-dir="+profile,
		"--no-first-run",
		"--no-default-browser-check",
		"--new-window",
	)
	if err := cmd.Start(); err != nil {
		log.Warn("app window: launch failed — opening a plain tab", "browser", bin, "err", err)
		openInDefaultBrowser(url, log)
		return nil, func() {}
	}
	log.Info("app window opened", "browser", filepath.Base(bin), "url", url)

	ch := make(chan struct{})
	go func() {
		_ = cmd.Wait()
		close(ch)
	}()
	return ch, func() {
		if cmd.Process == nil {
			return
		}
		_ = cmd.Process.Signal(os.Interrupt)
		select {
		case <-ch:
		case <-time.After(3 * time.Second):
			_ = cmd.Process.Kill()
			<-ch
		}
	}
}

// appProfileDir is the dedicated browser profile backing the app window.
// Disposable — all real state lives on the server — so it goes under the
// user cache dir.
func appProfileDir() string {
	base, err := os.UserCacheDir()
	if err != nil {
		base = os.TempDir()
	}
	return filepath.Join(base, "codedistill", "app-window")
}

func waitReachable(ctx context.Context, url string, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	client := &http.Client{Timeout: time.Second}
	for time.Now().Before(deadline) {
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, url+"/api/v1/version", nil)
		if resp, err := client.Do(req); err == nil {
			resp.Body.Close()
			return true
		}
		select {
		case <-ctx.Done():
			return false
		case <-time.After(200 * time.Millisecond):
		}
	}
	return false
}

func openInDefaultBrowser(url string, log *slog.Logger) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	if err := cmd.Start(); err != nil {
		log.Info("open the UI manually", "url", url)
		return
	}
	go func() { _ = cmd.Wait() }()
}
