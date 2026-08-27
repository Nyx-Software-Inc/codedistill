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

import "testing"

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
