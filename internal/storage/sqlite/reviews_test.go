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

func TestReviews(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)

	if err := s.CreateProject(ctx, &domain.Project{ID: "p1", Name: "P", CreatedAt: now}); err != nil {
		t.Fatal(err)
	}
	cs := &domain.Changeset{ProjectID: "p1", Kind: "branch", BaseSHA: "base", HeadSHA: "tip", Ref: "feat", CreatedAt: now}
	if err := s.EnsureChangeset(ctx, cs); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetReviewByChangeset(ctx, cs.ID); !errors.Is(err, storage.ErrNotFound) {
		t.Fatalf("no review yet: %v", err)
	}
	rv := &domain.Review{ID: "r1", ChangesetID: cs.ID, ProjectID: "p1", Status: domain.ReviewOpen, RequiredApprovals: 2, AuthorEmail: "a@x", CreatedAt: now, UpdatedAt: now}
	if err := s.CreateReview(ctx, rv); err != nil {
		t.Fatal(err)
	}
	// One review per changeset.
	if err := s.CreateReview(ctx, &domain.Review{ID: "r2", ChangesetID: cs.ID, ProjectID: "p1", Status: domain.ReviewOpen, CreatedAt: now, UpdatedAt: now}); err == nil {
		t.Fatal("second review on the same changeset was accepted")
	}
	got, err := s.GetReviewByChangeset(ctx, cs.ID)
	if err != nil || got.ID != "r1" || got.RequiredApprovals != 2 {
		t.Fatalf("by changeset: %+v %v", got, err)
	}

	// Commits the changeset covers, and the gate's question.
	if err := s.SetChangesetCommits(ctx, cs.ID, []string{"c1", "c2"}); err != nil {
		t.Fatal(err)
	}
	if shas, _ := s.ListChangesetCommits(ctx, cs.ID); len(shas) != 2 {
		t.Fatalf("commits = %v", shas)
	}
	if open, err := s.OpenReviewForCommits(ctx, "p1", []string{"zzz", "c2"}); err != nil || open.ID != "r1" {
		t.Fatalf("open review for c2: %+v %v", open, err)
	}
	if _, err := s.OpenReviewForCommits(ctx, "p1", []string{"zzz"}); !errors.Is(err, storage.ErrNotFound) {
		t.Fatalf("open review for unknown commit: %v", err)
	}
	if _, err := s.OpenReviewForCommits(ctx, "p2", []string{"c1"}); !errors.Is(err, storage.ErrNotFound) {
		t.Fatalf("open review across projects: %v", err)
	}
	rv.Status = domain.ReviewApproved
	rv.UpdatedAt = now.Add(time.Minute)
	if err := s.UpdateReview(ctx, rv); err != nil {
		t.Fatal(err)
	}
	if _, err := s.OpenReviewForCommits(ctx, "p1", []string{"c1"}); !errors.Is(err, storage.ErrNotFound) {
		t.Fatalf("approved review still blocks: %v", err)
	}
	if list, _ := s.ListReviews(ctx, "p1", []string{domain.ReviewOpen}); len(list) != 0 {
		t.Fatalf("open list = %v", list)
	}
	if list, _ := s.ListReviews(ctx, "p1", nil); len(list) != 1 || list[0].Status != domain.ReviewApproved {
		t.Fatalf("all list = %v", list)
	}

	// Decisions on the review, newest first.
	for i, d := range []string{"approved", "rejected"} {
		if err := s.CreateReviewDecision(ctx, &domain.ReviewDecision{
			ID: "d" + d, OwnerType: "review", OwnerID: "r1", Decision: d, Reviewer: "u" + d, CreatedAt: now.Add(time.Duration(i) * time.Second),
		}); err != nil {
			t.Fatal(err)
		}
	}
	ds, err := s.ListReviewDecisions(ctx, "review", "r1")
	if err != nil || len(ds) != 2 || ds[0].Decision != "rejected" {
		t.Fatalf("decisions = %+v %v", ds, err)
	}

	// Items anchored to the covered commits.
	if err := s.CreateTodoItem(ctx, &domain.TodoItem{ID: "t1", ProjectID: "p1", Subject: "x", Priority: "none", Status: "incomplete", Origin: "manual", CreatedAt: now}); err != nil {
		t.Fatal(err)
	}
	if err := s.CreateCodeAnchor(ctx, &domain.CodeAnchor{ID: "a1", OwnerType: "todo_item", OwnerID: "t1", Kind: "commit", Revision: "c2", Provenance: "user-set", CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	if err := s.CreateCodeAnchor(ctx, &domain.CodeAnchor{ID: "a2", OwnerType: "todo_item", OwnerID: "t1", Kind: "file", Path: "x.go", Provenance: "user-set", CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	refs, err := s.ItemsByCommits(ctx, "p1", []string{"c1", "c2"})
	if err != nil || len(refs) != 1 || refs[0].OwnerID != "t1" {
		t.Fatalf("items by commits = %+v %v", refs, err)
	}
	if refs, _ := s.ItemsByCommits(ctx, "p1", []string{"c9"}); len(refs) != 0 {
		t.Fatalf("unexpected items: %+v", refs)
	}
}
