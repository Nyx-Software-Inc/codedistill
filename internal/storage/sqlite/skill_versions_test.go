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

// TestSkillVersioning locks the versioning invariants: create seeds v1, a
// name/content change cuts a new version, and an enabled/position-only edit does
// NOT (skip-unchanged). Purge removes one snapshot.
func TestSkillVersioning(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	seedProject(t, s)
	now := fixedTime(t)

	sk := &domain.Skill{ID: "sk1", ProjectID: "p1", Name: "review-flow", Content: "v1 body", Enabled: true, CreatedAt: now, UpdatedAt: now}
	if err := s.CreateSkill(ctx, sk); err != nil {
		t.Fatalf("create: %v", err)
	}
	if sk.Version != 1 {
		t.Fatalf("create version = %d, want 1", sk.Version)
	}
	if vs, _ := s.ListSkillVersions(ctx, "sk1"); len(vs) != 1 {
		t.Fatalf("history after create = %d rows, want 1", len(vs))
	}

	// Content change → new version.
	sk.Content = "v2 body"
	if err := s.UpdateSkill(ctx, sk); err != nil {
		t.Fatalf("update content: %v", err)
	}
	if sk.Version != 2 {
		t.Fatalf("after content change version = %d, want 2", sk.Version)
	}

	// enabled-only change → NO new version (skip-unchanged).
	sk.Enabled = false
	if err := s.UpdateSkill(ctx, sk); err != nil {
		t.Fatalf("update enabled: %v", err)
	}
	if sk.Version != 2 {
		t.Fatalf("after enabled-only change version = %d, want still 2", sk.Version)
	}
	vs, _ := s.ListSkillVersions(ctx, "sk1")
	if len(vs) != 2 {
		t.Fatalf("history = %d rows, want 2 (enabled-only edit must not cut a version)", len(vs))
	}
	// Newest first, and v1 content preserved immutably.
	if vs[0].Version != 2 || vs[1].Version != 1 {
		t.Fatalf("history order = [%d,%d], want [2,1]", vs[0].Version, vs[1].Version)
	}
	if v1, err := s.GetSkillVersion(ctx, "sk1", 1); err != nil || v1.Content != "v1 body" {
		t.Fatalf("v1 snapshot = %+v err=%v, want content 'v1 body'", v1, err)
	}

	// Compliance purge of one snapshot.
	if err := s.PurgeSkillVersion(ctx, "sk1", 1); err != nil {
		t.Fatalf("purge: %v", err)
	}
	if vs, _ := s.ListSkillVersions(ctx, "sk1"); len(vs) != 1 {
		t.Fatalf("history after purge = %d, want 1", len(vs))
	}
}

// TestSkillApplicationEvidenceGate is the load-bearing "can't lie" test: an
// application is never written without an evidence class, and the reverse-lookup
// filters by version and evidence.
func TestSkillApplicationEvidenceGate(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	seedProject(t, s)
	now := fixedTime(t)

	// No evidence → rejected.
	if err := s.RecordSkillApplication(ctx, &domain.SkillApplication{
		ID: "a0", SkillID: "sk1", SkillVersion: 1, OwnerType: "todo_item", OwnerID: "t1", CreatedAt: now,
	}); err == nil {
		t.Fatal("expected error recording an application with no evidence class")
	}

	mk := func(idv string, ver int, ev string) {
		if err := s.RecordSkillApplication(ctx, &domain.SkillApplication{
			ID: idv, SkillID: "sk1", SkillVersion: ver, OwnerType: "todo_item", OwnerID: "t" + idv,
			CommitSHA: "abc123", Evidence: ev, CreatedAt: now,
		}); err != nil {
			t.Fatalf("record %s: %v", idv, err)
		}
	}
	mk("1", 1, domain.SkillEvidenceAttested)
	mk("2", 2, domain.SkillEvidenceAttested)
	mk("3", 2, domain.SkillEvidenceRetrieved)

	// Reverse lookup: all for the skill.
	if apps, _ := s.ListSkillApplications(ctx, "sk1", 0, ""); len(apps) != 3 {
		t.Fatalf("all applications = %d, want 3", len(apps))
	}
	// Narrow to v2.
	if apps, _ := s.ListSkillApplications(ctx, "sk1", 2, ""); len(apps) != 2 {
		t.Fatalf("v2 applications = %d, want 2", len(apps))
	}
	// Narrow to v2 + attested only (the high-confidence net).
	if apps, _ := s.ListSkillApplications(ctx, "sk1", 2, domain.SkillEvidenceAttested); len(apps) != 1 {
		t.Fatalf("v2 attested applications = %d, want 1", len(apps))
	}
}
