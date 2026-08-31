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
	"strings"
	"testing"
	"unicode/utf8"
)

// noteSummary sliced BYTES at 120, so a multi-byte rune straddling that offset
// was cut in half — then the ellipsis was appended to the corrupted string.
//
// Pure CJK or pure emoji survive by luck (120 divides by both 3 and 4). It is
// MIXED content that breaks, which is the common shape: "fix: 日本語のバグ…".
// json.Marshal then substitutes U+FFFD, so the UI shows replacement glyphs
// (CE-review item 23).
func TestNoteSummary_NeverSplitsARune(t *testing.T) {
	for _, tc := range []struct{ name, in string }{
		{"ascii then CJK", "x" + strings.Repeat("日", 60)},
		{"ascii then emoji", "xy" + strings.Repeat("🔥", 40)},
		{"pure CJK", strings.Repeat("日", 60)},
		{"pure emoji", strings.Repeat("🔥", 40)},
		{"long ascii", strings.Repeat("a", 150)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := noteSummary(tc.in)
			if !utf8.ValidString(got) {
				t.Errorf("summary is not valid UTF-8 — a rune was cut in half.\n  got: %q", got)
			}
		})
	}
}

// The byte budget is 120 for the text plus the 3-byte ellipsis, which is what
// the API already produced for ASCII. Rich's call: keep bytes, just stop
// splitting runes — a rune-count budget would let a CJK summary reach 360+
// bytes and change storage expectations.
func TestNoteSummary_RespectsByteBudget(t *testing.T) {
	const maxWithEllipsis = 123
	for _, in := range []string{
		strings.Repeat("a", 500),
		strings.Repeat("日", 200),
		"x" + strings.Repeat("🔥", 100),
	} {
		if got := noteSummary(in); len(got) > maxWithEllipsis {
			t.Errorf("summary is %d bytes, want <= %d\n  got: %q", len(got), maxWithEllipsis, got)
		}
	}
}

// Truncation must be signalled, and short notes must pass through untouched.
func TestNoteSummary_EllipsisOnlyWhenTruncated(t *testing.T) {
	if got := noteSummary("a short note"); got != "a short note" {
		t.Errorf("short note = %q, want it unchanged", got)
	}
	if got := noteSummary("first line\nsecond line"); got != "first line" {
		t.Errorf("multiline = %q, want just the first line", got)
	}
	if got := noteSummary(strings.Repeat("a", 150)); !strings.HasSuffix(got, "…") {
		t.Errorf("truncated note = %q, want a trailing ellipsis", got)
	}
}
