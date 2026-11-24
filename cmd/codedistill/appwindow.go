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
	"encoding/json"
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
	prefs := filepath.Join(profile, "Default", "Preferences")
	enlargeSavedWindow(prefs, log)
	allowPaneWindows(prefs, url, log)
	cmd := exec.Command(bin,
		"--app="+url,
		"--user-data-dir="+profile,
		"--no-first-run",
		"--no-default-browser-check",
		"--new-window",
		// The first launch, before the browser has a size of its own to
		// remember; Chromium keeps it on the screen.
		"--window-size=1600,1000",
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

// enlargeSavedWindow makes the window the browser will reopen at least a
// comfortable size. The browser opens an app window where it last was, and
// its own first guess (about 1380×710 on a 1920×1200 screen) was too small
// to work in. A window under three quarters of the screen either way grows
// to 90% × 85% of it, centred; one the user made bigger stays as it is.
//
// The profile is CodeDistill's own, so this edits nothing of the user's
// browser. Anything unexpected in the file leaves it untouched.
func enlargeSavedWindow(prefsPath string, log *slog.Logger) {
	b, err := os.ReadFile(prefsPath)
	if err != nil {
		return
	}
	var prefs map[string]any
	if json.Unmarshal(b, &prefs) != nil {
		return
	}
	browser, _ := prefs["browser"].(map[string]any)
	placements, _ := browser["app_window_placement"].(map[string]any)
	if placements == nil {
		return
	}
	changed := false
	var walk func(m map[string]any)
	walk = func(m map[string]any) {
		for k, v := range m {
			sub, ok := v.(map[string]any)
			if !ok || k == "DevToolsApp" {
				continue
			}
			if _, isBounds := sub["work_area_right"]; isBounds {
				changed = enlargeBounds(sub) || changed
				continue
			}
			walk(sub)
		}
	}
	walk(placements)
	if !changed {
		return
	}
	out, err := json.Marshal(prefs)
	if err != nil {
		return
	}
	if err := os.WriteFile(prefsPath, out, 0o600); err == nil {
		log.Info("app window: enlarged the saved window size")
	}
}

// allowPaneWindows lets CodeDistill open its own windows: a pane popped out
// of the main window is reopened when the app starts, when nothing has been
// clicked, and Chromium's popup blocker refuses a window opened then. Window
// management is what lets a popped-out window open on another screen
// (docs/design/panes.md, "Platform limits").
//
// Both are granted to the app's own origin, in CodeDistill's own profile.
// Before the first launch there is no Preferences file yet; one holding only
// these settings is written, and Chromium fills in the rest.
func allowPaneWindows(prefsPath, origin string, log *slog.Logger) {
	prefs := map[string]any{}
	if b, err := os.ReadFile(prefsPath); err == nil {
		if json.Unmarshal(b, &prefs) != nil {
			return
		}
	} else if !os.IsNotExist(err) {
		return
	}
	pattern := origin + ",*"
	changed := false
	for _, kind := range []string{"popups", "window_placement"} {
		exceptions := child(child(child(prefs, "profile"), "content_settings"), "exceptions")
		rules := child(exceptions, kind)
		if r, ok := rules[pattern].(map[string]any); ok && r["setting"] == float64(1) {
			continue
		}
		rules[pattern] = map[string]any{"setting": 1}
		changed = true
	}
	if !changed {
		return
	}
	out, err := json.Marshal(prefs)
	if err != nil {
		return
	}
	if err := os.MkdirAll(filepath.Dir(prefsPath), 0o700); err != nil {
		return
	}
	if err := os.WriteFile(prefsPath, out, 0o600); err == nil {
		log.Info("app window: allowed CodeDistill's own pane windows", "origin", origin)
	}
}

// child returns m[k] as an object, creating it (or replacing a non-object).
func child(m map[string]any, k string) map[string]any {
	c, ok := m[k].(map[string]any)
	if !ok {
		c = map[string]any{}
		m[k] = c
	}
	return c
}

// enlargeBounds applies the rule above to one saved placement.
func enlargeBounds(p map[string]any) bool {
	num := func(k string) (float64, bool) { v, ok := p[k].(float64); return v, ok }
	if max, _ := p["maximized"].(bool); max {
		return false
	}
	l, ok1 := num("left")
	t, ok2 := num("top")
	r, ok3 := num("right")
	bt, ok4 := num("bottom")
	wl, ok5 := num("work_area_left")
	wt, ok6 := num("work_area_top")
	wr, ok7 := num("work_area_right")
	wb, ok8 := num("work_area_bottom")
	if !(ok1 && ok2 && ok3 && ok4 && ok5 && ok6 && ok7 && ok8) || wr <= wl || wb <= wt {
		return false
	}
	ww, wh := wr-wl, wb-wt
	if r-l >= ww*0.75 && bt-t >= wh*0.75 {
		return false
	}
	w, h := float64(int(ww*0.9)), float64(int(wh*0.85))
	left, top := wl+float64(int((ww-w)/2)), wt+float64(int((wh-h)/2))
	p["left"], p["top"], p["right"], p["bottom"] = left, top, left+w, top+h
	return true
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
