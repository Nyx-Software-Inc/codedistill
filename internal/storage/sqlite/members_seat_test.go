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
	"fmt"
	"sync"
	"testing"
	"time"

	"codedistill/internal/domain"
)

func mkUser(t *testing.T, s *Store, id string) {
	t.Helper()
	if err := s.CreateUser(context.Background(), &domain.User{ID: id, DisplayName: id, Provider: "oidc", CreatedAt: time.Now().UTC()}); err != nil {
		t.Fatalf("create user %s: %v", id, err)
	}
}

// The seat-guarded writes must never let active membership exceed the licensed
// seat count, even under a concurrent-login race (audit M18). SQLite serializes
// writers, so the count-in-the-write guard is atomic here.
func TestTryAddWorkspaceMember_NoOversell(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	const seats = 2
	const contenders = 8

	for i := 0; i < contenders; i++ {
		mkUser(t, s, fmt.Sprintf("u%d", i))
	}

	var wg sync.WaitGroup
	var mu sync.Mutex
	added := 0
	for i := 0; i < contenders; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			ok, err := s.TryAddWorkspaceMember(ctx, &domain.WorkspaceMember{
				WorkspaceID: "local", UserID: fmt.Sprintf("u%d", i), Role: "member",
				Status: domain.MemberActive, CreatedAt: time.Now().UTC(),
			}, seats)
			if err != nil {
				t.Errorf("try add u%d: %v", i, err)
				return
			}
			if ok {
				mu.Lock()
				added++
				mu.Unlock()
			}
		}(i)
	}
	wg.Wait()

	if added != seats {
		t.Errorf("added = %d, want exactly %d (no oversell)", added, seats)
	}
	// The DB must agree (excludes the seeded 'local' owner).
	n, err := s.CountActiveWorkspaceMembers(ctx, "local")
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != seats {
		t.Errorf("active members = %d, want %d", n, seats)
	}
}

// Activating an invited member is likewise seat-guarded, and once the cap is
// full further activations are rejected (return false) rather than oversold.
func TestTryActivateWorkspaceMember_Guarded(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	const seats = 1

	mkUser(t, s, "a")
	mkUser(t, s, "b")
	// Both invited (non-active) — no seat consumed yet.
	for _, uid := range []string{"a", "b"} {
		if err := s.AddWorkspaceMember(ctx, &domain.WorkspaceMember{
			WorkspaceID: "local", UserID: uid, Role: "member",
			Status: domain.MemberInvited, CreatedAt: time.Now().UTC(),
		}); err != nil {
			t.Fatalf("add invited %s: %v", uid, err)
		}
	}

	// First activation fits the single seat.
	ok, err := s.TryActivateWorkspaceMember(ctx, "local", "a", seats)
	if err != nil || !ok {
		t.Fatalf("activate a: ok=%v err=%v, want true/nil", ok, err)
	}
	// Second is over the cap → rejected, and b stays invited.
	ok, err = s.TryActivateWorkspaceMember(ctx, "local", "b", seats)
	if err != nil {
		t.Fatalf("activate b: %v", err)
	}
	if ok {
		t.Error("activate b succeeded over the seat cap — oversold")
	}
	m, err := s.GetWorkspaceMember(ctx, "local", "b")
	if err != nil {
		t.Fatalf("get b: %v", err)
	}
	if m.Status == domain.MemberActive {
		t.Error("b became active despite a full seat cap")
	}
}
