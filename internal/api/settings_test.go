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
	"encoding/json"
	"testing"

	"codedistill/internal/domain"
)

// End-to-end test for the settings API. Exercises both user and project
// scopes, the GET / PUT / LIST / DELETE quartet, and validation paths.

func TestUserSettingsAPI(t *testing.T) {
	srv, _ := setup(t)

	// PUT a primitive bool.
	var resp settingResponse
	doJSON(t, srv, "PUT", "/api/v1/users/local/settings/ui.drawer.open",
		map[string]any{"value": true}, 200, &resp)
	if string(resp.Value) != "true" {
		t.Errorf("PUT echo value = %q, want true", string(resp.Value))
	}

	// PUT a structured value (overwrite).
	doJSON(t, srv, "PUT", "/api/v1/users/local/settings/ui.drawer.open",
		map[string]any{"value": map[string]any{"open": false, "width": 280}}, 200, &resp)
	var decoded map[string]any
	if err := json.Unmarshal(resp.Value, &decoded); err != nil {
		t.Fatalf("decode response value: %v", err)
	}
	if decoded["open"] != false || decoded["width"].(float64) != 280 {
		t.Errorf("after overwrite: %+v", decoded)
	}

	// PUT a second key.
	doJSON(t, srv, "PUT", "/api/v1/users/local/settings/agent.model",
		map[string]any{"value": "qwen2.5:7b"}, 200, &resp)

	// GET the first key.
	doJSON(t, srv, "GET", "/api/v1/users/local/settings/ui.drawer.open", nil, 200, &resp)
	if resp.Key != "ui.drawer.open" {
		t.Errorf("GET key = %q", resp.Key)
	}

	// LIST all.
	var all map[string]json.RawMessage
	doJSON(t, srv, "GET", "/api/v1/users/local/settings", nil, 200, &all)
	if len(all) != 2 {
		t.Errorf("list len = %d, want 2 (got %v)", len(all), all)
	}

	// DELETE, then the key reads back as unset — 204 No Content, not 404.
	// Asking for a setting that was never written (or has been removed) is not
	// an error; the client supplies its own default (CE-review item 43).
	doJSON(t, srv, "DELETE", "/api/v1/users/local/settings/ui.drawer.open", nil, 204, nil)
	doJSON(t, srv, "GET", "/api/v1/users/local/settings/ui.drawer.open", nil, 204, nil)
}

func TestProjectSettingsAPI(t *testing.T) {
	srv, _ := setup(t)

	var p domain.Project
	doJSON(t, srv, "POST", "/api/v1/projects", map[string]string{"name": "p"}, 201, &p)

	var resp settingResponse
	doJSON(t, srv, "PUT", "/api/v1/projects/"+p.ID+"/settings/scratchpad.default_mode",
		map[string]any{"value": "strict"}, 200, &resp)
	if string(resp.Value) != `"strict"` {
		t.Errorf("PUT echo: %q", string(resp.Value))
	}

	doJSON(t, srv, "GET", "/api/v1/projects/"+p.ID+"/settings/scratchpad.default_mode", nil, 200, &resp)
	if resp.Key != "scratchpad.default_mode" {
		t.Errorf("GET key = %q", resp.Key)
	}

	// LIST returns a {key: value} map.
	var all map[string]json.RawMessage
	doJSON(t, srv, "GET", "/api/v1/projects/"+p.ID+"/settings", nil, 200, &all)
	if len(all) != 1 {
		t.Errorf("list len = %d, want 1", len(all))
	}

	// Unknown project on PUT → 404.
	doJSON(t, srv, "PUT", "/api/v1/projects/no-such-id/settings/k",
		map[string]any{"value": 1}, 404, nil)
}

func TestSettingsValidationErrors(t *testing.T) {
	srv, _ := setup(t)

	// Missing value field.
	doJSON(t, srv, "PUT", "/api/v1/users/local/settings/k", map[string]any{}, 400, nil)

	// GET a key that's never been set → 204 No Content.
	doJSON(t, srv, "GET", "/api/v1/users/local/settings/never.set", nil, 204, nil)

	// DELETE a missing key → still 404, and the asymmetry is deliberate.
	// "What is this value?" has a valid answer when nothing is stored ("there
	// isn't one"). "Remove this" does not — it names something that does not
	// exist, which is the same call made for architecture nodes and skills in
	// CE-review item 31.
	doJSON(t, srv, "DELETE", "/api/v1/users/local/settings/never.set", nil, 404, nil)
}
