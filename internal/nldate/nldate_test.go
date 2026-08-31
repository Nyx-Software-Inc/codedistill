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

package nldate

import (
	"testing"
	"time"
)

func TestParse(t *testing.T) {
	// Reference: Wednesday, 2026-07-15, noon local.
	ref := time.Date(2026, 7, 15, 12, 0, 0, 0, time.Local)
	d := func(y int, m time.Month, day int) time.Time {
		return time.Date(y, m, day, 23, 59, 59, 0, time.Local)
	}

	cases := []struct {
		text string
		want time.Time
		ok   bool
	}{
		{"Paint the #basement_ceiling by Tuesday", d(2026, 7, 21), true}, // next Tue after Wed 7/15
		{"fix the crash by tomorrow", d(2026, 7, 16), true},
		{"ship it today", d(2026, 7, 15), true},
		{"handle this eod", d(2026, 7, 15), true},
		{"do it in 3 days", d(2026, 7, 18), true},
		{"finish in 2 weeks", d(2026, 7, 29), true},
		{"revisit next week", d(2026, 7, 22), true},
		{"due 2026-07-20", d(2026, 7, 20), true},
		{"deadline 7/20", d(2026, 7, 20), true},
		{"submit before friday", d(2026, 7, 17), true},
		{"wrap up by eow", d(2026, 7, 17), true},      // Friday
		{"invoice by eom", d(2026, 7, 31), true},      // end of month
		{"call by next monday", d(2026, 7, 27), true}, // this Mon=7/20, next=+7
		{"due July 20", d(2026, 7, 20), true},
		{"by 20th of July", d(2026, 7, 20), true},
		{"month/year rolls: due 1/5", d(2027, 1, 5), true}, // Jan already past 7/15 → next year
		// Conservative: no trigger, no false positive.
		{"we had a meeting on Monday about the roadmap", time.Time{}, false},
		{"review the login flow", time.Time{}, false},
		{"the ratio was 7/20 in the test", d(2026, 7, 20), false}, // slash w/o trigger → ignored
	}

	for _, c := range cases {
		got, ok := Parse(c.text, ref)
		if ok != c.ok {
			t.Errorf("Parse(%q) ok=%v want %v (got %v)", c.text, ok, c.ok, got)
			continue
		}
		if ok && !got.Equal(c.want) {
			t.Errorf("Parse(%q) = %v, want %v", c.text, got, c.want)
		}
	}
}

func TestParseEarliestWins(t *testing.T) {
	ref := time.Date(2026, 7, 15, 12, 0, 0, 0, time.Local)
	// "tomorrow" appears before "by friday" — earliest position wins.
	got, ok := Parse("do X tomorrow, and the rest by friday", ref)
	if !ok || got.Day() != 16 {
		t.Errorf("earliest match should be tomorrow (7/16), got %v ok=%v", got, ok)
	}
}
