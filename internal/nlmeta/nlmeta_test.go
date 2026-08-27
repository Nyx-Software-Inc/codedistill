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

package nlmeta

import "testing"

func TestPriority(t *testing.T) {
	cases := []struct {
		text string
		want string
		ok   bool
	}{
		{"URGENT: fix the login flow", "high", true},
		{"need this asap for the demo", "high", true},
		{"this is p1 for the release", "high", true},
		{"High priority — dashboard export", "high", true},
		{"drop everything and fix billing", "high", true},

		{"medium priority cleanup of the settings page", "medium", true},
		{"p2: tidy the drawer animations", "medium", true},

		{"low priority: rename that variable", "low", true},
		{"nice to have: keyboard shortcuts on the canvas", "low", true},
		{"no rush on this one", "low", true},
		{"someday we should refactor the exporter", "low", true},

		// Negation: never fire the positive; common negated forms are low.
		{"this is not urgent at all", "low", true},
		{"it isn't critical, just annoying", "", false},

		// Conflict → blank.
		{"urgent but also kind of a nice to have", "", false},

		// No signal → blank. Ordinary intensity words are not signals.
		{"fix the label alignment on the profile menu", "", false},
		{"really important customer!", "", false},
		{"the word priority alone means nothing", "", false},
	}
	for _, c := range cases {
		got, ok := Priority(c.text)
		if got != c.want || ok != c.ok {
			t.Errorf("Priority(%q) = %q,%v; want %q,%v", c.text, got, ok, c.want, c.ok)
		}
	}
}

func TestSeverity(t *testing.T) {
	cases := []struct {
		text string
		want string
		ok   bool
	}{
		{"URGENT: crash on login", "critical", true},
		{"app crashes when I drop an image", "critical", true},
		{"possible data loss when the sync races", "critical", true},
		{"security vulnerability in the token check", "critical", true},

		{"major regression in the exporter", "major", true},
		{"severe slowdown on large canvases", "major", true},

		{"minor: misaligned icon in the drawer", "minor", true},

		{"typo in the settings help text", "trivial", true},
		{"cosmetic: button hover color is off", "trivial", true},
		{"nit: trailing whitespace in the export", "trivial", true},

		// Negation and conflicts stay blank.
		{"this is not critical, ship next week", "", false},
		{"critical crash but honestly the fix is a typo", "", false},

		// Ordinary bug phrasing is NOT a signal — default stays minor upstream.
		{"the save button doesn't work on Firefox", "", false},
		{"wrong count shown in the header", "", false},
	}
	for _, c := range cases {
		got, ok := Severity(c.text)
		if got != c.want || ok != c.ok {
			t.Errorf("Severity(%q) = %q,%v; want %q,%v", c.text, got, ok, c.want, c.ok)
		}
	}
}
