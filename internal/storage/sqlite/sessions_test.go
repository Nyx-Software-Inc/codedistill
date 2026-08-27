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
	"time"

	"codedistill/internal/domain"
	"codedistill/internal/storage"
)

// TestSessionRoundTrip covers create / valid-by-expiry / delete / revoke-all,
// against both SQLite and Postgres via newTestStore.
func TestSessionRoundTrip(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	now := fixedTime(t)
	mk := func(id string, exp time.Time) *domain.Session {
		return &domain.Session{ID: id, UserID: "local", CreatedAt: now, LastActiveAt: now, ExpiresAt: exp}
	}

	if err := s.CreateSession(ctx, mk("h1", now.Add(time.Hour))); err != nil {
		t.Fatalf("create: %v", err)
	}
	got, err := s.GetValidSession(ctx, "h1", now)
	if err != nil || got.UserID != "local" {
		t.Fatalf("get valid: %v / %+v", err, got)
	}
	// Past expiry → treated as not found.
	if _, err := s.GetValidSession(ctx, "h1", now.Add(2*time.Hour)); err == nil {
		t.Errorf("expired session should be ErrNotFound")
	}
	// Delete one.
	if err := s.DeleteSession(ctx, "h1"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := s.GetValidSession(ctx, "h1", now); err == nil {
		t.Errorf("deleted session should be gone")
	}
	// Revoke all of a user's sessions (deactivation / sign-out-everywhere).
	_ = s.CreateSession(ctx, mk("h2", now.Add(time.Hour)))
	_ = s.CreateSession(ctx, mk("h3", now.Add(time.Hour)))
	if err := s.DeleteUserSessions(ctx, "local"); err != nil {
		t.Fatalf("delete user sessions: %v", err)
	}
	if _, err := s.GetValidSession(ctx, "h2", now); err == nil {
		t.Errorf("user sessions should be revoked")
	}

	// FK: a session for a non-existent user is rejected.
	err = s.CreateSession(ctx, &domain.Session{ID: "hx", UserID: "ghost", CreatedAt: now, LastActiveAt: now, ExpiresAt: now.Add(time.Hour)})
	if err == nil {
		t.Errorf("session for unknown user should violate the FK")
	}
	_ = storage.ErrNotFound
}

// TouchSession must slide the expiry forward (rolling session), and
// DeleteExpiredSessions must prune only the lapsed rows (audit M22).
func TestSessionSlideAndPrune(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	now := fixedTime(t)

	// A session about to expire in 1h.
	if err := s.CreateSession(ctx, &domain.Session{
		ID: "s1", UserID: "local", CreatedAt: now, LastActiveAt: now, ExpiresAt: now.Add(time.Hour),
	}); err != nil {
		t.Fatalf("create: %v", err)
	}
	// Activity 30m later rolls it a full TTL forward.
	later := now.Add(30 * time.Minute)
	rolled := later.Add(30 * 24 * time.Hour)
	if err := s.TouchSession(ctx, "s1", later, rolled); err != nil {
		t.Fatalf("touch: %v", err)
	}
	// It's now valid well past its ORIGINAL expiry.
	got, err := s.GetValidSession(ctx, "s1", now.Add(2*time.Hour))
	if err != nil {
		t.Fatalf("session should be valid past its original expiry after touch: %v", err)
	}
	if !got.ExpiresAt.Equal(rolled) || !got.LastActiveAt.Equal(later) {
		t.Errorf("after touch: expires=%v active=%v, want %v / %v", got.ExpiresAt, got.LastActiveAt, rolled, later)
	}

	// Prune: an already-expired session is removed; the live one survives.
	_ = s.CreateSession(ctx, &domain.Session{
		ID: "dead", UserID: "local", CreatedAt: now, LastActiveAt: now, ExpiresAt: now.Add(-time.Hour),
	})
	n, err := s.DeleteExpiredSessions(ctx, now)
	if err != nil {
		t.Fatalf("prune: %v", err)
	}
	if n != 1 {
		t.Errorf("pruned %d, want 1 (only the expired one)", n)
	}
	if _, err := s.GetValidSession(ctx, "s1", later); err != nil {
		t.Errorf("live session must survive the prune: %v", err)
	}
}
