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

func TestReviewComments(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)

	// Ensure is idempotent on (project, base, head) and keeps the first id.
	cs := &domain.Changeset{ProjectID: "p1", Kind: "commit", BaseSHA: "aaa", HeadSHA: "bbb", CreatedAt: now}
	if err := s.EnsureChangeset(ctx, cs); err != nil {
		t.Fatalf("ensure: %v", err)
	}
	if cs.ID == "" {
		t.Fatal("ensure left ID empty")
	}
	again := &domain.Changeset{ProjectID: "p1", Kind: "commit", BaseSHA: "aaa", HeadSHA: "bbb", CreatedAt: now.Add(time.Hour)}
	if err := s.EnsureChangeset(ctx, again); err != nil || again.ID != cs.ID {
		t.Fatalf("ensure again: id %q want %q, err %v", again.ID, cs.ID, err)
	}
	other := &domain.Changeset{ProjectID: "p1", Kind: "working", BaseSHA: "bbb", HeadSHA: "working", CreatedAt: now}
	if err := s.EnsureChangeset(ctx, other); err != nil || other.ID == cs.ID {
		t.Fatalf("ensure other: %v (id %q)", err, other.ID)
	}
	if got, err := s.GetChangeset(ctx, cs.ID); err != nil || got.HeadSHA != "bbb" {
		t.Fatalf("get: %+v %v", got, err)
	}
	if _, err := s.GetChangeset(ctx, "nope"); !errors.Is(err, storage.ErrNotFound) {
		t.Fatalf("get missing: %v", err)
	}

	// Comments list in reading order: path, then line, then time.
	mk := func(id, path, side string, line int, body string, at time.Time) *domain.ReviewComment {
		return &domain.ReviewComment{
			ID: id, ChangesetID: cs.ID, Path: path, Side: side, LineStart: line, LineEnd: line,
			Body: body, Author: "local", CreatedAt: at, UpdatedAt: at,
		}
	}
	for _, c := range []*domain.ReviewComment{
		mk("c3", "b.go", "new", 5, "third", now.Add(2*time.Second)),
		mk("c1", "a.go", "new", 10, "first", now),
		mk("c2", "a.go", "old", 3, "second", now.Add(time.Second)),
		mk("c2b", "a.go", "old", 3, "reply", now.Add(3*time.Second)),
	} {
		if err := s.CreateReviewComment(ctx, c); err != nil {
			t.Fatalf("create %s: %v", c.ID, err)
		}
	}
	list, err := s.ListReviewComments(ctx, cs.ID)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	var ids []string
	for _, c := range list {
		ids = append(ids, c.ID)
	}
	if want := []string{"c2", "c2b", "c1", "c3"}; !equalStrings(ids, want) {
		t.Fatalf("order = %v, want %v", ids, want)
	}
	if n, _ := s.ListReviewComments(ctx, other.ID); len(n) != 0 {
		t.Fatalf("other changeset has %d comments", len(n))
	}

	// Update: resolve and promote round-trip; a missing id is ErrNotFound.
	c1, _ := s.GetReviewComment(ctx, "c1")
	res := now.Add(time.Minute)
	c1.Body = "first, edited"
	c1.ResolvedAt = &res
	c1.PromotedOwnerType, c1.PromotedOwnerID = "bug_item", "bug1"
	c1.UpdatedAt = res
	if err := s.UpdateReviewComment(ctx, c1); err != nil {
		t.Fatalf("update: %v", err)
	}
	got, _ := s.GetReviewComment(ctx, "c1")
	if got.Body != "first, edited" || got.ResolvedAt == nil || !got.ResolvedAt.Equal(res) || got.PromotedOwnerID != "bug1" {
		t.Fatalf("after update = %+v", got)
	}
	if err := s.UpdateReviewComment(ctx, &domain.ReviewComment{ID: "nope"}); !errors.Is(err, storage.ErrNotFound) {
		t.Fatalf("update missing: %v", err)
	}
	if err := s.DeleteReviewComment(ctx, "c3"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if err := s.DeleteReviewComment(ctx, "c3"); !errors.Is(err, storage.ErrNotFound) {
		t.Fatalf("delete again: %v", err)
	}
	if list, _ = s.ListReviewComments(ctx, cs.ID); len(list) != 3 {
		t.Fatalf("after delete: %d comments", len(list))
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
