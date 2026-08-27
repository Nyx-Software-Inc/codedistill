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

func TestCustomFields_DefCRUDAndValues(t *testing.T) {
	srv, ag := setup(t)

	var p domain.Project
	doJSON(t, srv, "POST", "/api/v1/projects", map[string]string{"name": "p"}, 201, &p)

	// Create a number field that applies to bugs.
	var def domain.CustomFieldDef
	doJSON(t, srv, "POST", "/api/v1/projects/"+p.ID+"/custom-fields",
		map[string]any{"name": "Story Points", "field_type": "number", "applies_to": []string{"bug_item"}}, 201, &def)
	if def.ID == "" || def.FieldType != "number" {
		t.Fatalf("bad def: %+v", def)
	}

	// A select field with no options is rejected.
	doJSON(t, srv, "POST", "/api/v1/projects/"+p.ID+"/custom-fields",
		map[string]any{"name": "Component", "field_type": "select", "applies_to": []string{"bug_item"}}, 400, nil)

	// applies_to is required.
	doJSON(t, srv, "POST", "/api/v1/projects/"+p.ID+"/custom-fields",
		map[string]any{"name": "Nope", "field_type": "text"}, 400, nil)

	// Seed a bug to attach a value to.
	var sp domain.Scratchpad
	doJSON(t, srv, "POST", "/api/v1/projects/"+p.ID+"/scratchpads", map[string]string{"name": "x"}, 201, &sp)
	var item domain.ScratchpadItem
	doJSON(t, srv, "POST", "/api/v1/scratchpads/"+sp.ID+"/items",
		map[string]string{"content": "boom", "classification_override": "bug"}, 201, &item)
	ag.Stop() // flush the classification so the derived bug exists
	var got domain.ScratchpadItem
	doJSON(t, srv, "GET", "/api/v1/items/"+item.ID, nil, 200, &got)
	bugID := got.DerivedItemID
	if bugID == "" {
		t.Fatalf("no derived bug")
	}

	// The field shows up on the bug (applies_to), value empty.
	var views []map[string]any
	doJSON(t, srv, "GET", "/api/v1/bugs/"+bugID+"/custom-fields", nil, 200, &views)
	if len(views) != 1 || views[0]["name"] != "Story Points" || views[0]["value"] != "" {
		t.Fatalf("expected one empty Story Points field, got %+v", views)
	}

	// A non-number value is rejected; a number sticks.
	doJSON(t, srv, "PUT", "/api/v1/bugs/"+bugID+"/custom-fields/"+def.ID, map[string]string{"value": "abc"}, 400, nil)
	doJSON(t, srv, "PUT", "/api/v1/bugs/"+bugID+"/custom-fields/"+def.ID, map[string]string{"value": "5"}, 200, nil)

	doJSON(t, srv, "GET", "/api/v1/bugs/"+bugID+"/custom-fields", nil, 200, &views)
	if views[0]["value"] != "5" {
		t.Errorf("value = %v, want 5", views[0]["value"])
	}

	// Deleting the def cascades the value away.
	doJSON(t, srv, "DELETE", "/api/v1/custom-fields/"+def.ID, nil, 204, nil)
	doJSON(t, srv, "GET", "/api/v1/bugs/"+bugID+"/custom-fields", nil, 200, &views)
	if len(views) != 0 {
		t.Errorf("expected no fields after delete, got %+v", views)
	}
}
