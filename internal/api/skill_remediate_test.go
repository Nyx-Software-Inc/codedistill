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

package api

import (
	"context"
	"strings"
	"testing"
	"time"

	"codedistill/internal/domain"
)

// TestSkillRemediation_FlagsIntoReviewQueue is the pass-2 end-to-end: remediating
// a superseded skill version flags the changes made under it into the review
// queue with a reason, and a human approval clears the flag.
func TestSkillRemediation_FlagsIntoReviewQueue(t *testing.T) {
	srv, ag, store := setupWithStore(t)
	ag.Stop()
	ctx := context.Background()
	now := time.Now().UTC()
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}

	must(store.CreateProject(ctx, &domain.Project{ID: "p1", Name: "p", CreatedAt: now}))
	must(store.CreateTodoItem(ctx, &domain.TodoItem{
		ID: "t1", ProjectID: "p1", Number: 1, Subject: "the change",
		Status: domain.TodoStatusComplete, Priority: "none", Origin: "manual",
		Visibility: "project", CreatorID: "local", CreatedAt: now,
	}))
	// A commit anchor so the item appears in the review queue (real remediation
	// targets are always implemented, so they always have one).
	must(store.CreateCodeAnchor(ctx, &domain.CodeAnchor{
		ID: "a1", OwnerType: "todo_item", OwnerID: "t1", Kind: "commit",
		Revision: "abc1234", Provenance: "agent-suggested", CreatedAt: now, UpdatedAt: now,
	}))
	// Skill v1 → v2, so v1 is a superseded (remediable) version.
	sk := &domain.Skill{ID: "sk1", ProjectID: "p1", Name: "Review flow", Content: "v1", Enabled: true, CreatedAt: now, UpdatedAt: now}
	must(store.CreateSkill(ctx, sk))
	sk.Content = "v2"
	must(store.UpdateSkill(ctx, sk))
	// t1 was changed under skill v1.
	must(store.RecordSkillApplication(ctx, &domain.SkillApplication{
		ID: "app1", SkillID: "sk1", SkillVersion: 1, OwnerType: "todo_item", OwnerID: "t1",
		CommitSHA: "abc1234", Evidence: domain.SkillEvidenceAttested, CreatedAt: now,
	}))

	// Remediate v1 → flag its changes for review.
	var rem remediateResp
	doJSON(t, srv, "POST", "/api/v1/skills/sk1/versions/1/remediate",
		map[string]string{"action": "review"}, 200, &rem)
	if rem.Flagged != 1 || rem.Items != 1 {
		t.Fatalf("remediate: flagged=%d items=%d, want 1/1", rem.Flagged, rem.Items)
	}

	// The review queue now surfaces t1 as flagged + needs-review, with the reason.
	entry := func() *reviewQueueEntry {
		var q reviewQueueResp
		doJSON(t, srv, "GET", "/api/v1/projects/p1/review-queue", nil, 200, &q)
		for i := range q.Items {
			if q.Items[i].ID == "t1" {
				return &q.Items[i]
			}
		}
		return nil
	}
	e := entry()
	if e == nil {
		t.Fatal("t1 not in review queue")
	}
	if !e.Flagged || !e.NeedsReview {
		t.Fatalf("want flagged + needs_review, got flagged=%v needs_review=%v", e.Flagged, e.NeedsReview)
	}
	if !strings.Contains(strings.Join(e.Reasons, " "), "Review flow") {
		t.Fatalf("reason should name the skill, got %v", e.Reasons)
	}

	// Human approval resolves the flag — t1 is no longer flagged.
	doJSON(t, srv, "POST", "/api/v1/todos/t1/review", map[string]string{"decision": "approved"}, 201, nil)
	if e := entry(); e != nil && e.Flagged {
		t.Fatal("flag should be cleared after approval")
	}
}
