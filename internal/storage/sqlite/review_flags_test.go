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

// TestReviewFlags covers the flag lifecycle: create → active → clear.
func TestReviewFlags(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	now := fixedTime(t)

	f := &domain.ReviewFlag{
		ID: "f1", OwnerType: "todo_item", OwnerID: "t1",
		Reason: "changed under skill v2", Source: "skill_remediation", CreatedAt: now,
	}
	if err := s.CreateReviewFlag(ctx, f); err != nil {
		t.Fatalf("create: %v", err)
	}

	active, err := s.ActiveReviewFlags(ctx, "todo_item", "t1")
	if err != nil {
		t.Fatalf("active: %v", err)
	}
	if len(active) != 1 {
		t.Fatalf("active = %d, want 1", len(active))
	}
	if active[0].ClearedAt != nil {
		t.Errorf("new flag should be uncleared, got cleared_at=%v", active[0].ClearedAt)
	}
	if active[0].Reason != "changed under skill v2" {
		t.Errorf("reason = %q", active[0].Reason)
	}

	// A different item is unaffected.
	if other, _ := s.ActiveReviewFlags(ctx, "todo_item", "t2"); len(other) != 0 {
		t.Fatalf("unrelated item has %d flags, want 0", len(other))
	}

	// Clearing resolves the flag; it drops out of the active set.
	if err := s.ClearReviewFlags(ctx, "todo_item", "t1", now.Add(1)); err != nil {
		t.Fatalf("clear: %v", err)
	}
	if active, _ := s.ActiveReviewFlags(ctx, "todo_item", "t1"); len(active) != 0 {
		t.Fatalf("after clear active = %d, want 0", len(active))
	}
}
