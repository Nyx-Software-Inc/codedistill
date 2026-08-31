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
	"errors"
	"testing"
	"time"

	"codedistill/internal/domain"
	"codedistill/internal/storage"
)

// Deleting something that isn't there must report ErrNotFound so the HTTP layer
// can answer 404, the way DeleteTodoItem / DeleteBugItem / DeleteKnowledgeEntry
// already do and 15 delete handlers already expect via statusFor.
//
// Architecture nodes and skills were the two outliers: neither checked
// RowsAffected, so both reported success for a no-op and the API answered
// 204 No Content. For the architecture diagram in particular that is the wrong
// failure mode — a stale node id held by the UI would "delete successfully" and
// the user would believe the diagram changed (CE-review item 31).
func TestDelete_MissingRowReportsNotFound(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	now := time.Now().UTC()
	if err := s.CreateProject(ctx, &domain.Project{ID: "p1", Name: "P", CreatedAt: now}); err != nil {
		t.Fatal(err)
	}

	t.Run("architecture node", func(t *testing.T) {
		if err := s.DeleteArchitectureNode(ctx, "no-such-node"); !errors.Is(err, storage.ErrNotFound) {
			t.Errorf("err = %v, want storage.ErrNotFound (a no-op delete must not report success)", err)
		}
	})

	t.Run("skill", func(t *testing.T) {
		if err := s.DeleteSkill(ctx, "no-such-skill"); !errors.Is(err, storage.ErrNotFound) {
			t.Errorf("err = %v, want storage.ErrNotFound", err)
		}
	})

	// The real delete must keep working — a not-found check must not start
	// rejecting valid deletes.
	t.Run("existing node still deletes", func(t *testing.T) {
		n := &domain.ArchitectureNode{
			ID: "n1", ProjectID: "p1", Name: "Billing", Kind: "service",
			Provenance: "ratified", CreatedAt: now, UpdatedAt: now,
		}
		if err := s.CreateArchitectureNode(ctx, n); err != nil {
			t.Fatal(err)
		}
		if err := s.DeleteArchitectureNode(ctx, "n1"); err != nil {
			t.Errorf("deleting a real node: %v", err)
		}
		if err := s.DeleteArchitectureNode(ctx, "n1"); !errors.Is(err, storage.ErrNotFound) {
			t.Errorf("second delete: err = %v, want ErrNotFound", err)
		}
	})
}

// UpdateArchitectureNode returned a bare fmt.Errorf("...: not found") with no
// %w, so errors.Is could not see the sentinel and statusFor would classify it
// 500. GetArchitectureNode in the same file wraps correctly — the file was
// inconsistent with itself. Masked today because the handler pre-checks with
// Get, so reaching the 500 needs a TOCTOU race (CE-review item 31).
func TestUpdateArchitectureNode_MissingWrapsNotFound(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	now := time.Now().UTC()
	if err := s.CreateProject(ctx, &domain.Project{ID: "p1", Name: "P", CreatedAt: now}); err != nil {
		t.Fatal(err)
	}
	err := s.UpdateArchitectureNode(ctx, &domain.ArchitectureNode{
		ID: "ghost", ProjectID: "p1", Name: "Gone", Kind: "service",
		Provenance: "ratified", CreatedAt: now, UpdatedAt: now,
	})
	if !errors.Is(err, storage.ErrNotFound) {
		t.Errorf("err = %v; want it to wrap storage.ErrNotFound so statusFor can answer 404 instead of 500", err)
	}
}
