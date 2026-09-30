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

import "testing"

// The status vocabulary decides what is unblocked, so getting it wrong
// misreports the whole view — and it WAS wrong: "fixed" was missing, so 28
// fixed bugs counted as outstanding work and blocked whatever depended on them
// forever. These are the values the real tables hold, counted from a live
// database rather than imagined.
func TestSettledCoversTheStatusesTheTablesActuallyHold(t *testing.T) {
	settled := []string{
		// bug_items
		"closed", "fixed", "not_a_bug", "duplicate",
		// todo_items
		"complete", "abandoned",
		// use_case_items
		"completed", "rejected",
		// and the spellings other paths write
		"done", "resolved", "implemented",
	}
	for _, s := range settled {
		if !isSettled(s) {
			t.Errorf("%q is settled work but counts as outstanding — it would also block anything depending on it", s)
		}
	}

	open := []string{"open", "incomplete", "in_progress", "approved", ""}
	for _, s := range open {
		if isSettled(s) {
			t.Errorf("%q is still work but counts as settled — it would silently unblock its dependents", s)
		}
	}
}
