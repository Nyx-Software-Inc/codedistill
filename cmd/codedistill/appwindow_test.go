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

package main

import (
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAppURL(t *testing.T) {
	cases := map[string]string{
		"127.0.0.1:8080": "http://127.0.0.1:8080",
		":9090":          "http://127.0.0.1:9090",
		"0.0.0.0:8080":   "http://127.0.0.1:8080",
		"[::]:8080":      "http://127.0.0.1:8080",
		"myhost:8080":    "http://myhost:8080",
	}
	for in, want := range cases {
		if got := appURL(in); got != want {
			t.Errorf("appURL(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestResolveAppModeExplicit(t *testing.T) {
	if !resolveAppMode("on") {
		t.Error("on should force app mode")
	}
	if resolveAppMode("off") {
		t.Error("off should disable app mode")
	}
	// "auto" under `go test` has no TTY on stdout — must resolve to off, the
	// same guard that keeps services and headless boxes windowless.
	if resolveAppMode("auto") {
		t.Error("auto without a TTY should disable app mode")
	}
}

// A small saved window grows to 90% × 85% of the screen, centred; a large or
// maximised one, and the rest of the file, are left alone.
func TestEnlargeSavedWindow(t *testing.T) {
	p := filepath.Join(t.TempDir(), "Preferences")
	os.WriteFile(p, []byte(`{"other":{"keep":true},"browser":{"app_window_placement":{
		"127":{"0":{"0":{"1_/":{"bottom":708,"left":0,"maximized":false,"right":1382,"top":0,
			"work_area_bottom":1200,"work_area_left":0,"work_area_right":1920,"work_area_top":0}}}},
		"big":{"bottom":1150,"left":10,"maximized":false,"right":1900,"top":10,
			"work_area_bottom":1200,"work_area_left":0,"work_area_right":1920,"work_area_top":0},
		"DevToolsApp":{"bottom":640,"left":0,"right":640,"top":0,
			"work_area_bottom":1200,"work_area_left":0,"work_area_right":1920,"work_area_top":0}}}}`), 0o600)
	enlargeSavedWindow(p, slog.New(slog.NewTextHandler(io.Discard, nil)))
	var got struct {
		Other   map[string]bool `json:"other"`
		Browser struct {
			Placement map[string]json.RawMessage `json:"app_window_placement"`
		} `json:"browser"`
	}
	b, _ := os.ReadFile(p)
	if err := json.Unmarshal(b, &got); err != nil || !got.Other["keep"] {
		t.Fatalf("file = %s", b)
	}
	s := string(got.Browser.Placement["127"])
	for _, want := range []string{`"left":96`, `"top":90`, `"right":1824`, `"bottom":1110`} {
		if !strings.Contains(s, want) {
			t.Errorf("small window: missing %s in %s", want, s)
		}
	}
	if s := string(got.Browser.Placement["big"]); !strings.Contains(s, `"right":1900`) {
		t.Errorf("a large window was changed: %s", s)
	}
	if s := string(got.Browser.Placement["DevToolsApp"]); !strings.Contains(s, `"right":640`) {
		t.Errorf("devtools was changed: %s", s)
	}
}
