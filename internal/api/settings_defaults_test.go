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
	"testing"
	"time"

	"codedistill/internal/domain"
	"codedistill/internal/throttle"
)

// The server KNOWS the shipped default for these keys — throttle.DefaultPause
// and throttle.DefaultMultiplier — and ReadConfig already treats "no row" as
// "use the default" without erroring. The settings API did not: it was a dumb
// key-value read, so an unset key 404'd.
//
// That forced every client to carry its own copy of the default, which is how
// the same value ended up defined in Go and in two Svelte components. If the
// shipped default ever changed, the engine and the UI would silently disagree
// about what the product does — and PowerBadge re-asked, and re-404'd, every
// 30 seconds forever (CE-review item 43).
func TestGetUserSetting_UnsetKnownKeyReturnsDefault(t *testing.T) {
	srv, _, _ := setupWithStore(t)

	for _, tc := range []struct {
		name, key string
		want      any
	}{
		{"battery pause", throttle.KeyBatteryPause, false},
		{"battery multiplier", throttle.KeyBatteryMultiplier, throttle.DefaultMultiplier},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var got struct {
				Key     string `json:"key"`
				Value   any    `json:"value"`
				Default bool   `json:"default"`
			}
			doJSON(t, srv, "GET", "/api/v1/users/local/settings/"+tc.key, nil, 200, &got)

			if got.Value != tc.want {
				t.Errorf("value = %v (%T), want %v", got.Value, got.Value, tc.want)
			}
			if !got.Default {
				t.Error(`"default" flag = false; a served default must say so, or a client cannot tell it from a stored value`)
			}
		})
	}
}

// A stored value must still win over the default, and must not be flagged.
func TestGetUserSetting_StoredValueWinsOverDefault(t *testing.T) {
	srv, _, _ := setupWithStore(t)
	doJSON(t, srv, "PUT", "/api/v1/users/local/settings/"+throttle.KeyBatteryPause,
		map[string]any{"value": true}, 200, nil)

	var got struct {
		Value   any  `json:"value"`
		Default bool `json:"default"`
	}
	doJSON(t, srv, "GET", "/api/v1/users/local/settings/"+throttle.KeyBatteryPause, nil, 200, &got)
	if got.Value != true {
		t.Errorf("value = %v, want true (the stored value)", got.Value)
	}
	if got.Default {
		t.Error(`stored value was flagged "default": true`)
	}
}

// A key the server has no opinion about answers 204 No Content, not 404.
//
// Asking a key-value store for something never written is not a failure. The
// SPA reads ~22 settings keys, and all but the few the ENGINE owns (the
// throttle pair) hold values only the client cares about — ui.item_font_size,
// input.submit_shortcut, ui.theme. Registering server-side defaults for those
// would copy eighteen frontend defaults into Go and recreate exactly the drift
// item 43 exists to prevent, so the server stops calling "unset" an error
// instead. 204 also covers the dynamic ui.codecanvas.<projectId> keys, which no
// static registry could ever enumerate (CE-review item 43, follow-on).
func TestGetUserSetting_UnknownKeyIs204(t *testing.T) {
	srv, _, _ := setupWithStore(t)
	doJSON(t, srv, "GET", "/api/v1/users/local/settings/ui.some.key.nobody.registered", nil, 204, nil)
}

// Project settings go through a different handler and 404'd too —
// archive.delete_action was in the console noise.
func TestGetProjectSetting_UnsetKeyIs204(t *testing.T) {
	srv, _, store := setupWithStore(t)
	if err := store.CreateProject(context.Background(),
		&domain.Project{ID: "p1", Name: "P", CreatedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	doJSON(t, srv, "GET", "/api/v1/projects/p1/settings/archive.delete_action", nil, 204, nil)
}

// The list-view sort order is a registered default too, so the SPA's load on
// first render gets 'due' with a 200 rather than a 404 it has to swallow.
func TestGetUserSetting_ListSortDefault(t *testing.T) {
	srv, _, _ := setupWithStore(t)
	var got struct {
		Value   any  `json:"value"`
		Default bool `json:"default"`
	}
	doJSON(t, srv, "GET", "/api/v1/users/local/settings/ui.list.sort", nil, 200, &got)
	if got.Value != "due" {
		t.Errorf("value = %v, want \"due\" (today's behaviour, so upgrades change nothing)", got.Value)
	}
	if !got.Default {
		t.Error(`"default" flag = false; a served default must say so`)
	}
}
