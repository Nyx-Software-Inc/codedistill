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

// TestAPITokenRoundTrip: create / resolve-by-hash / list / owner-scoped delete /
// unique hash. Runs on both SQLite and Postgres via newTestStore.
func TestAPITokenRoundTrip(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	now := fixedTime(t)

	if err := s.CreateAPIToken(ctx, &domain.APIToken{ID: "t1", UserID: "local", TokenHash: "hash1", Name: "agent1", CreatedAt: now}); err != nil {
		t.Fatalf("create: %v", err)
	}
	uid, lastUsed, err := s.GetAPITokenUser(ctx, "hash1")
	if err != nil || uid != "local" {
		t.Fatalf("resolve: %v / %q", err, uid)
	}
	if lastUsed != nil {
		t.Errorf("last_used_at should be nil before first use, got %v", lastUsed)
	}
	// After a touch, last_used_at is reported (throttle input for the auth path).
	if err := s.TouchAPIToken(ctx, "hash1", now); err != nil {
		t.Fatalf("touch: %v", err)
	}
	if _, lu, _ := s.GetAPITokenUser(ctx, "hash1"); lu == nil || !lu.Equal(now) {
		t.Errorf("last_used_at = %v, want %v", lu, now)
	}
	if _, _, err := s.GetAPITokenUser(ctx, "nope"); err == nil {
		t.Errorf("unknown hash should be ErrNotFound")
	}
	if list, _ := s.ListAPITokens(ctx, "local"); len(list) != 1 || list[0].Name != "agent1" {
		t.Fatalf("list = %+v", list)
	}
	// Owner-scoped delete: another user can't delete it.
	_ = s.DeleteAPIToken(ctx, "t1", "someone-else")
	if list, _ := s.ListAPITokens(ctx, "local"); len(list) != 1 {
		t.Errorf("delete by non-owner must be a no-op")
	}
	if err := s.DeleteAPIToken(ctx, "t1", "local"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if list, _ := s.ListAPITokens(ctx, "local"); len(list) != 0 {
		t.Errorf("token should be revoked")
	}
	// token_hash is UNIQUE.
	_ = s.CreateAPIToken(ctx, &domain.APIToken{ID: "t2", UserID: "local", TokenHash: "dup", CreatedAt: now})
	if err := s.CreateAPIToken(ctx, &domain.APIToken{ID: "t3", UserID: "local", TokenHash: "dup", CreatedAt: now}); err == nil {
		t.Errorf("duplicate token_hash should violate UNIQUE")
	}
}
