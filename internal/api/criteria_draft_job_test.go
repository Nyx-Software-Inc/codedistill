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

// The drafting registry keys by ownerType+ownerID and must round-trip cleanly
// back to the (type, id) pair the UI needs — including ids that themselves
// contain no NUL but arbitrary characters.
func TestCriteriaDraftRegistry(t *testing.T) {
	s := &Server{criteriaJobs: map[string]bool{}}

	if s.criteriaDrafting("bug_item", "b1") {
		t.Fatal("nothing registered yet, want not-drafting")
	}
	s.criteriaJobs[criteriaJobKey("bug_item", "b1")] = true
	s.criteriaJobs[criteriaJobKey("todo_item", "t-99")] = true

	if !s.criteriaDrafting("bug_item", "b1") || !s.criteriaDrafting("todo_item", "t-99") {
		t.Fatal("registered items should report drafting")
	}
	if s.criteriaDrafting("bug_item", "other") {
		t.Fatal("unrelated item must not report drafting")
	}

	refs := s.listCriteriaDrafts()
	if len(refs) != 2 {
		t.Fatalf("listCriteriaDrafts = %d refs, want 2", len(refs))
	}
	got := map[string]string{}
	for _, r := range refs {
		got[r.OwnerID] = r.OwnerType
	}
	if got["b1"] != "bug_item" || got["t-99"] != "todo_item" {
		t.Errorf("refs didn't round-trip type/id: %+v", refs)
	}
}
