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
	"net/http/httptest"
	"testing"

	"codedistill/internal/domain"
)

// A group of one is not a group. These cover the three ways membership drops —
// delete, archive, and drag-out — because only the third was wired up before.

type groupFixture struct {
	sp      string
	group   string
	members []string
}

// newGroupFixture builds a scratchpad with one group frame holding n members.
func newGroupFixture(t *testing.T, srv *httptest.Server, n int) groupFixture {
	t.Helper()
	var p domain.Project
	doJSON(t, srv, "POST", "/api/v1/projects", map[string]string{"name": "p"}, 201, &p)
	var sp domain.Scratchpad
	doJSON(t, srv, "POST", "/api/v1/projects/"+p.ID+"/scratchpads", map[string]string{"name": "x"}, 201, &sp)

	var grp domain.ScratchpadItem
	doJSON(t, srv, "POST", "/api/v1/scratchpads/"+sp.ID+"/items",
		map[string]string{"content": "the group", "content_type": "group"}, 201, &grp)

	f := groupFixture{sp: sp.ID, group: grp.ID}
	for i := 0; i < n; i++ {
		var it domain.ScratchpadItem
		doJSON(t, srv, "POST", "/api/v1/scratchpads/"+sp.ID+"/items",
			map[string]string{"content": "member"}, 201, &it)
		doJSON(t, srv, "PATCH", "/api/v1/items/"+it.ID,
			map[string]any{"group_id": grp.ID}, 200, nil)
		f.members = append(f.members, it.ID)
	}
	return f
}

// groupAlive reports whether the group frame still exists, and how many items
// are on the canvas.
func groupAlive(t *testing.T, srv *httptest.Server, f groupFixture) (bool, int) {
	t.Helper()
	var live []domain.ScratchpadItem
	doJSON(t, srv, "GET", "/api/v1/scratchpads/"+f.sp+"/items", nil, 200, &live)
	for _, it := range live {
		if it.ID == f.group {
			return true, len(live)
		}
	}
	return false, len(live)
}

func TestGroup_DeletingDownToOneDissolvesIt(t *testing.T) {
	srv, _ := setup(t)
	f := newGroupFixture(t, srv, 3)

	// Two deletes leaves one member — not a group any more.
	doJSON(t, srv, "DELETE", "/api/v1/items/"+f.members[0], nil, 204, nil)
	if alive, _ := groupAlive(t, srv, f); !alive {
		t.Fatal("group dissolved at two members — it was still a real group")
	}
	doJSON(t, srv, "DELETE", "/api/v1/items/"+f.members[1], nil, 204, nil)

	alive, n := groupAlive(t, srv, f)
	if alive {
		t.Error("group of one survived; a group of one is not a group")
	}
	// The survivor stays. Dissolving the group must never take a member with it
	// — the FK is ON DELETE SET NULL precisely so this holds.
	if n != 1 {
		t.Errorf("expected the surviving member alone on the canvas, got %d items", n)
	}
}

func TestGroup_SurvivorIsUngroupedNotDangling(t *testing.T) {
	srv, _ := setup(t)
	f := newGroupFixture(t, srv, 2)

	doJSON(t, srv, "DELETE", "/api/v1/items/"+f.members[0], nil, 204, nil)

	var survivor domain.ScratchpadItem
	doJSON(t, srv, "GET", "/api/v1/items/"+f.members[1], nil, 200, &survivor)
	// A group_id pointing at a deleted row would render as a member of nothing.
	if survivor.GroupID != "" {
		t.Errorf("survivor still references the dissolved group: %q", survivor.GroupID)
	}
}

func TestGroup_ArchivingCountsAsLeaving(t *testing.T) {
	srv, _ := setup(t)
	f := newGroupFixture(t, srv, 2)

	// Decision (b): an archived member is off the canvas, so it has left the
	// group. Without this the frame outlives its contents — which is exactly
	// what archiving 21 items did to the Locator Launch pad.
	doJSON(t, srv, "POST", "/api/v1/items/"+f.members[0]+"/archive", nil, 204, nil)

	if alive, _ := groupAlive(t, srv, f); alive {
		t.Error("group survived with one archived and one live member")
	}
}

func TestGroup_DraggingOutDownToOneDissolvesIt(t *testing.T) {
	srv, _ := setup(t)
	f := newGroupFixture(t, srv, 2)

	// Clearing group_id is what a drag out of the frame does.
	doJSON(t, srv, "PATCH", "/api/v1/items/"+f.members[0],
		map[string]any{"group_id": ""}, 200, nil)

	if alive, _ := groupAlive(t, srv, f); alive {
		t.Error("group survived a drag-out that left one member")
	}
}

func TestGroup_DeletingEveryMemberLeavesNoFrame(t *testing.T) {
	srv, _ := setup(t)
	f := newGroupFixture(t, srv, 2)

	for _, id := range f.members {
		doJSON(t, srv, "DELETE", "/api/v1/items/"+id, nil, 204, nil)
	}
	alive, n := groupAlive(t, srv, f)
	if alive {
		t.Error("empty group frame survived; delete did no cleanup at all before this")
	}
	if n != 0 {
		t.Errorf("expected an empty canvas, got %d items", n)
	}
}

func TestGroup_TwoMembersIsLeftAlone(t *testing.T) {
	srv, _ := setup(t)
	f := newGroupFixture(t, srv, 3)

	doJSON(t, srv, "DELETE", "/api/v1/items/"+f.members[0], nil, 204, nil)

	alive, n := groupAlive(t, srv, f)
	if !alive {
		t.Error("a two-member group is a real group and must be left alone")
	}
	if n != 3 { // group frame + two members
		t.Errorf("expected frame + 2 members = 3, got %d", n)
	}
}
