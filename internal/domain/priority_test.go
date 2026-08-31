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
	"sort"
	"testing"
)

// Bugs rank by severity and deliberately have no priority field. RankFor is the
// single place that knows this, so a mixed list sorts by one comparable number
// and no caller has to answer "is a trivial bug at high priority above a
// critical bug at low priority?" — the question that made a second axis on bugs
// the wrong call.
func TestRankFor_MixedKindsOrderSensibly(t *testing.T) {
	type row struct{ kind, field, label string }
	rows := []row{
		{"todo", "low", "low todo"},
		{"bug", "critical", "critical bug"},
		{"use case", "high", "high use case"},
		{"kb", "", "kb entry"},
		{"bug", "trivial", "trivial bug"},
		{"todo", "high", "high todo"},
		{"use case", "none", "unprioritised use case"},
	}
	sort.SliceStable(rows, func(i, j int) bool {
		return RankFor(rows[i].kind, rows[i].field) < RankFor(rows[j].kind, rows[j].field)
	})

	want := []string{
		"critical bug", "high use case", "high todo", // rank 1
		"low todo", "trivial bug", // 3, 4
		"unprioritised use case", // 4
		"kb entry",               // Unranked
	}
	// Only the tiers are asserted, not the order within a tier — SliceStable
	// preserves input order there and pinning it would over-specify.
	got := make([]int, len(rows))
	for i, r := range rows {
		got[i] = RankFor(r.kind, r.field)
	}
	for i := 1; i < len(got); i++ {
		if got[i] < got[i-1] {
			t.Fatalf("ranks not ascending after sort: %v (%v)", got, rows)
		}
	}
	if rows[0].label != want[0] {
		t.Errorf("most important = %q, want %q", rows[0].label, want[0])
	}
	if rows[len(rows)-1].label != "kb entry" {
		t.Errorf("least important = %q, want the kb entry (reference material has no priority)", rows[len(rows)-1].label)
	}
}

// "We don't know" and "the user chose none" are different claims, and conflating
// them is the class of defect this review cycle kept finding.
func TestRankFor_UnknownIsNotNone(t *testing.T) {
	if PriorityRank("banana") == PriorityRank("none") {
		t.Error("an unrecognised priority ranks the same as an explicit 'none'")
	}
	if PriorityRank("banana") != Unranked {
		t.Errorf("unknown priority = %d, want Unranked(%d)", PriorityRank("banana"), Unranked)
	}
	// A bug severity passed to the priority path must NOT silently rank.
	if PriorityRank("critical") != Unranked {
		t.Error("severity leaked through the priority mapping; the two vocabularies must stay distinct")
	}
}

func TestValidPriority(t *testing.T) {
	for _, ok := range []string{"", "high", "medium", "low", "none"} {
		if !ValidPriority(ok) {
			t.Errorf("ValidPriority(%q) = false, want true", ok)
		}
	}
	for _, bad := range []string{"critical", "p1", "HIGH", "banana"} {
		if ValidPriority(bad) {
			t.Errorf("ValidPriority(%q) = true, want false", bad)
		}
	}
}
