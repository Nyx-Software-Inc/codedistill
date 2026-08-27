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
	"testing"

	"codedistill/internal/domain"
)

func TestSkills_CRUD(t *testing.T) {
	srv, _ := setup(t) // test env licenses all features (TestMain)

	var p domain.Project
	doJSON(t, srv, "POST", "/api/v1/projects", map[string]string{"name": "p"}, 201, &p)

	// Create.
	var sk domain.Skill
	doJSON(t, srv, "POST", "/api/v1/projects/"+p.ID+"/skills",
		map[string]any{"name": "Review flow", "content": "do the thing"}, 201, &sk)
	if sk.ID == "" || !sk.Enabled {
		t.Fatalf("bad skill: %+v", sk)
	}

	// Name required.
	doJSON(t, srv, "POST", "/api/v1/projects/"+p.ID+"/skills", map[string]any{"content": "x"}, 400, nil)

	// List.
	var list []domain.Skill
	doJSON(t, srv, "GET", "/api/v1/projects/"+p.ID+"/skills", nil, 200, &list)
	if len(list) != 1 || list[0].Name != "Review flow" {
		t.Fatalf("list = %+v", list)
	}

	// Update (disable + edit content).
	doJSON(t, srv, "PATCH", "/api/v1/skills/"+sk.ID,
		map[string]any{"name": "Review flow", "content": "updated", "enabled": false}, 200, nil)
	doJSON(t, srv, "GET", "/api/v1/projects/"+p.ID+"/skills", nil, 200, &list)
	if list[0].Content != "updated" || list[0].Enabled {
		t.Errorf("update not applied: %+v", list[0])
	}

	// Delete.
	doJSON(t, srv, "DELETE", "/api/v1/skills/"+sk.ID, nil, 204, nil)
	doJSON(t, srv, "GET", "/api/v1/projects/"+p.ID+"/skills", nil, 200, &list)
	if len(list) != 0 {
		t.Errorf("expected no skills after delete, got %d", len(list))
	}
}
