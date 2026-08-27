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

func TestAcceptanceCriteriaCRUD(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	now := time.Now().UTC()

	// Empty owner → empty list (never nil) + position 0.
	list, err := s.ListAcceptanceCriteria(ctx, "use_case_item", "uc1")
	if err != nil {
		t.Fatalf("list empty: %v", err)
	}
	if len(list) != 0 {
		t.Fatalf("expected 0 criteria, got %d", len(list))
	}
	pos, err := s.NextAcceptanceCriterionPosition(ctx, "use_case_item", "uc1")
	if err != nil || pos != 0 {
		t.Fatalf("next pos on empty = %d, %v; want 0", pos, err)
	}

	// Create two, appended in order.
	for i, txt := range []string{"first criterion", "second criterion"} {
		p, _ := s.NextAcceptanceCriterionPosition(ctx, "use_case_item", "uc1")
		if p != i {
			t.Errorf("next pos = %d, want %d", p, i)
		}
		c := &domain.AcceptanceCriterion{
			ID: "ac" + txt[:1], OwnerType: "use_case_item", OwnerID: "uc1",
			Position: p, Text: txt, VerificationKind: "unspecified",
			State: "proposed", Provenance: "ai-proposed", CreatedAt: now, UpdatedAt: now,
		}
		if err := s.CreateAcceptanceCriterion(ctx, c); err != nil {
			t.Fatalf("create %q: %v", txt, err)
		}
	}

	list, _ = s.ListAcceptanceCriteria(ctx, "use_case_item", "uc1")
	if len(list) != 2 || list[0].Text != "first criterion" || list[1].Position != 1 {
		t.Fatalf("unexpected list: %+v", list)
	}

	// Update: accept the first + set verification_kind + satisfied_by.
	c := list[0]
	c.State = "accepted"
	c.VerificationKind = "test"
	c.SatisfiedBy = "abc1234"
	c.TestCommand = "go test -run TestExport ./..."
	c.Text = "first criterion (edited)"
	if err := s.UpdateAcceptanceCriterion(ctx, c); err != nil {
		t.Fatalf("update: %v", err)
	}
	got, err := s.GetAcceptanceCriterion(ctx, c.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.State != "accepted" || got.VerificationKind != "test" || got.SatisfiedBy != "abc1234" || got.Text != "first criterion (edited)" {
		t.Errorf("update not persisted: %+v", got)
	}
	if got.TestCommand != "go test -run TestExport ./..." {
		t.Errorf("test_command not persisted: %q", got.TestCommand)
	}

	// Delete + not-found semantics.
	if err := s.DeleteAcceptanceCriterion(ctx, c.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := s.GetAcceptanceCriterion(ctx, c.ID); !errors.Is(err, storage.ErrNotFound) {
		t.Errorf("get after delete: err = %v, want ErrNotFound", err)
	}
	if err := s.DeleteAcceptanceCriterion(ctx, c.ID); !errors.Is(err, storage.ErrNotFound) {
		t.Errorf("delete missing: err = %v, want ErrNotFound", err)
	}
	if list, _ := s.ListAcceptanceCriteria(ctx, "use_case_item", "uc1"); len(list) != 1 {
		t.Errorf("expected 1 remaining, got %d", len(list))
	}
}
