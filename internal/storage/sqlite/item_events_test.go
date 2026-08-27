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

// TestItemEventActorRoundTrip verifies the attribution column: WHO (actor) is
// stored + returned, and pre-attribution rows (empty actor) round-trip as empty.
// Runs against both SQLite and Postgres via newTestStore.
func TestItemEventActorRoundTrip(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	now := fixedTime(t)

	if err := s.RecordItemEvent(ctx, &domain.ItemEvent{
		ID: "e1", OwnerType: "scratchpad_item", OwnerID: "x",
		Kind: "created", Summary: "made", Source: "ui",
		ActorUserID: "local", CreatedAt: now,
	}); err != nil {
		t.Fatalf("record with actor: %v", err)
	}
	if err := s.RecordItemEvent(ctx, &domain.ItemEvent{
		ID: "e2", OwnerType: "scratchpad_item", OwnerID: "x",
		Kind: "moved", Source: "agent", CreatedAt: now.Add(1),
	}); err != nil {
		t.Fatalf("record without actor: %v", err)
	}

	evs, err := s.ListItemEvents(ctx, "scratchpad_item", "x")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(evs) != 2 {
		t.Fatalf("want 2 events, got %d", len(evs))
	}
	if evs[0].ActorUserID != "local" {
		t.Errorf("actor = %q, want local", evs[0].ActorUserID)
	}
	if evs[0].Source != "ui" {
		t.Errorf("source = %q, want ui (orthogonal to actor)", evs[0].Source)
	}
	if evs[1].ActorUserID != "" {
		t.Errorf("empty actor should round-trip empty, got %q", evs[1].ActorUserID)
	}
}
