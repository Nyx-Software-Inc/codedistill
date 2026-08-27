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

package agent

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestFirstLineOrTruncate(t *testing.T) {
	cases := []struct{ in, want string }{
		{"short subject", "short subject"},
		{"first line\nsecond line", "first line"},
		// Word-boundary cut: no mid-word stump, no trailing punctuation.
		{strings.Repeat("word ", 60) + "ending", strings.TrimRight(strings.Repeat("word ", 50), " ") + "…"},
	}
	for _, c := range cases {
		if got := firstLineOrTruncate(c.in, 250); got != c.want {
			t.Errorf("firstLineOrTruncate(%.40q) = %q, want %q", c.in, got, c.want)
		}
	}
	// Multibyte safety: cutting through runes must stay valid UTF-8.
	long := strings.Repeat("é→", 300)
	got := firstLineOrTruncate(long, 250)
	if !utf8.ValidString(got) {
		t.Errorf("truncation produced invalid UTF-8")
	}
	if utf8.RuneCountInString(got) > 251 {
		t.Errorf("rune count = %d, want ≤ 251", utf8.RuneCountInString(got))
	}
}
