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

package sqlite

import (
	"context"
	"testing"

	"codedistill/internal/domain"
)

// TestMemberStatusAndSeatCount covers status get/set + the active-seat count
// (which excludes the seeded 'local' bootstrap member). Both backends.
func TestMemberStatusAndSeatCount(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t) // migration 0006 seeds the 'local' workspace + member
	now := fixedTime(t)

	for _, id := range []string{"u1", "u2"} {
		if err := s.CreateUser(ctx, &domain.User{ID: id, DisplayName: id, CreatedAt: now}); err != nil {
			t.Fatalf("user %s: %v", id, err)
		}
	}
	add := func(uid, status string) {
		if err := s.AddWorkspaceMember(ctx, &domain.WorkspaceMember{
			WorkspaceID: "local", UserID: uid, Role: "member", Status: status, CreatedAt: now,
		}); err != nil {
			t.Fatalf("add %s: %v", uid, err)
		}
	}
	add("u1", domain.MemberActive)
	add("u2", domain.MemberInvited)

	// 'local' (seeded, active) is excluded → only u1 counts.
	if n, _ := s.CountActiveWorkspaceMembers(ctx, "local"); n != 1 {
		t.Fatalf("active = %d, want 1", n)
	}
	if m, _ := s.GetWorkspaceMember(ctx, "local", "u2"); m.Status != domain.MemberInvited {
		t.Errorf("u2 status = %q, want invited", m.Status)
	}
	// Activate u2 → 2 active.
	if err := s.SetWorkspaceMemberStatus(ctx, "local", "u2", domain.MemberActive); err != nil {
		t.Fatalf("activate: %v", err)
	}
	if n, _ := s.CountActiveWorkspaceMembers(ctx, "local"); n != 2 {
		t.Errorf("active = %d, want 2", n)
	}
	// Deactivate u1 → back to 1.
	if err := s.SetWorkspaceMemberStatus(ctx, "local", "u1", domain.MemberDeactivated); err != nil {
		t.Fatalf("deactivate: %v", err)
	}
	if n, _ := s.CountActiveWorkspaceMembers(ctx, "local"); n != 1 {
		t.Errorf("active = %d, want 1", n)
	}
}
