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

// A bare "17 claims the document could not support" said something was wrong
// without saying what. Grouping by cause is the whole value of the line, so
// every reason the checker can emit must map to a class rather than falling
// through to the raw string.
func TestEveryRejectionReasonIsClassified(t *testing.T) {
	// These mirror the messages CheckMove and DeriveMoves produce. If one is
	// reworded there and not here, this test is what notices.
	for _, reason := range []string{
		"window 30-59 returned unparseable JSON",
		"sentence 99 does not exist",
		"target 412 does not exist",
		"target 40 is not earlier than 12",
		"target 7 never introduced anything",
		"target on a role that cannot have one",
		`unknown role "ponder"`,
	} {
		got := classifyRejection(reason)
		if got == reason {
			t.Errorf("unclassified, would print raw: %q", reason)
		}
		if got == "" {
			t.Errorf("empty class for %q", reason)
		}
	}

	// Anything unrecognised must still print rather than vanish.
	if got := classifyRejection("something new"); got != "something new" {
		t.Errorf("an unknown reason was swallowed: %q", got)
	}
}

func TestPlural(t *testing.T) {
	for _, tc := range []struct {
		n    int
		want string
	}{
		{0, "0 ideas"}, {1, "1 idea"}, {2, "2 ideas"},
	} {
		if got := plural(tc.n, "idea"); got != tc.want {
			t.Errorf("plural(%d) = %q, want %q", tc.n, got, tc.want)
		}
	}
}
