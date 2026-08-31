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

package domain

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestNoteSummary(t *testing.T) {
	tests := []struct {
		name, in, want string
	}{
		{"short passes through", "a short note", "a short note"},
		{"first line only", "first line\nsecond line", "first line"},
		{"trims surrounding space", "   padded   \nmore", "padded"},
		{"empty stays empty", "", ""},
		{"exactly at the budget", strings.Repeat("a", SummaryMaxBytes), strings.Repeat("a", SummaryMaxBytes)},
		{"one past the budget truncates", strings.Repeat("a", SummaryMaxBytes+1), strings.Repeat("a", SummaryMaxBytes) + "…"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := NoteSummary(tc.in); got != tc.want {
				t.Errorf("NoteSummary(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

// The property that matters: whatever comes out is always valid UTF-8 and
// always within budget, for any input. Pure CJK and pure emoji used to pass by
// arithmetic alone (120 divides by 3 and by 4) — it is mixed content that
// exposed the byte slice, so the offsets here deliberately walk the boundary.
func TestNoteSummary_AlwaysValidUTF8WithinBudget(t *testing.T) {
	for _, filler := range []string{"日", "🔥", "é", "a"} {
		for pad := 0; pad < 8; pad++ {
			in := strings.Repeat("x", pad) + strings.Repeat(filler, 200)
			got := NoteSummary(in)
			if !utf8.ValidString(got) {
				t.Errorf("pad=%d filler=%q: not valid UTF-8: %q", pad, filler, got)
			}
			if len(got) > SummaryMaxBytes+len("…") {
				t.Errorf("pad=%d filler=%q: %d bytes, want <= %d", pad, filler, len(got), SummaryMaxBytes+3)
			}
		}
	}
}

// Truncating an already-truncated summary must not compound ellipses.
func TestNoteSummary_Idempotent(t *testing.T) {
	once := NoteSummary(strings.Repeat("a", 500))
	if twice := NoteSummary(once); twice != once {
		t.Errorf("not idempotent:\n  once:  %q\n  twice: %q", once, twice)
	}
}
