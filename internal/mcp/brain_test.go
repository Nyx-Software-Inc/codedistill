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

package mcp

import (
	"context"
	"strings"
	"testing"
	"time"

	"codedistill/internal/domain"
)

// get_project_brain bundles the structured KB (architecture/conventions/
// decisions) and excludes plain reference entries.
func TestGetProjectBrain(t *testing.T) {
	fx := newFixture(t)
	ctx := context.Background()
	now := time.Now().UTC()

	mk := func(id, title, kind string) {
		if err := fx.store.CreateKnowledgeEntry(ctx, &domain.KnowledgeEntry{
			ID: id, ProjectID: fx.projectID, Title: title, Content: "why: " + title,
			Kind: kind, Status: "active", CreatedAt: now,
		}); err != nil {
			t.Fatalf("kb %s: %v", id, err)
		}
	}
	mk("kbA", "Layered storage interface", "architecture")
	mk("kbD", "Use go-git, never shell out", "decision")
	mk("kbR", "A random note", "reference")

	res := fx.callTool("get_project_brain", map[string]any{"project_name": fx.projectName})
	if res.IsError {
		t.Fatalf("get_project_brain errored: %s", firstText(t, res))
	}
	body := firstText(t, res)
	if !strings.Contains(body, "Layered storage interface") || !strings.Contains(body, "Use go-git, never shell out") {
		t.Errorf("brain missing architecture/decision entries: %s", body)
	}
	if strings.Contains(body, "A random note") {
		t.Errorf("reference entries must be excluded from the brain: %s", body)
	}
}
