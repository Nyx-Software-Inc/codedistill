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

func TestReviewDecisions(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	now := time.Now().UTC()

	// None yet → ErrNotFound.
	if _, err := s.LatestReviewDecision(ctx, "bug_item", "b1"); !errors.Is(err, storage.ErrNotFound) {
		t.Fatalf("latest on empty: err = %v, want ErrNotFound", err)
	}

	// Record approve, then reject — latest wins.
	if err := s.CreateReviewDecision(ctx, &domain.ReviewDecision{
		ID: "rd1", OwnerType: "bug_item", OwnerID: "b1", Decision: "approved",
		CommitSHA: "abc1234", Reviewer: "local", CreatedAt: now,
	}); err != nil {
		t.Fatalf("create approve: %v", err)
	}
	if err := s.CreateReviewDecision(ctx, &domain.ReviewDecision{
		ID: "rd2", OwnerType: "bug_item", OwnerID: "b1", Decision: "rejected",
		Note: "missed the empty case", Reviewer: "local", CreatedAt: now.Add(time.Minute),
	}); err != nil {
		t.Fatalf("create reject: %v", err)
	}

	got, err := s.LatestReviewDecision(ctx, "bug_item", "b1")
	if err != nil {
		t.Fatalf("latest: %v", err)
	}
	if got.Decision != "rejected" || got.Note != "missed the empty case" {
		t.Errorf("latest = %+v, want the rejected decision", got)
	}
}
