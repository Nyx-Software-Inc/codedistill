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
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"codedistill/internal/domain"
)

// A Claude model run through Claude Code has no key of its own; its model list
// comes with the key of another provider at the same endpoint, and never with
// a key stored for a different one.
func TestDiscoverModels_BorrowsAKeyFromTheSameEndpoint(t *testing.T) {
	var gotKey string
	anth := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotKey = r.Header.Get("x-api-key")
		w.Write([]byte(`{"data":[{"id":"claude-fable-5-2","type":"model"}]}`))
	}))
	defer anth.Close()
	srv, _, store := setupWithStore(t)
	s := lastTestServer
	ctx := context.Background()
	now := time.Now().UTC()
	key, _ := s.encryptKey("sk-stored")
	store.CreateModelProvider(ctx, &domain.ModelProvider{ID: "api", Name: "Claude API", Protocol: "anthropic", Endpoint: anth.URL + "/",
		Model: "claude-opus-5-5", APIKey: key, Enabled: true, CreatedAt: now, UpdatedAt: now})

	var out struct {
		Models  []struct{ ID string } `json:"models"`
		KeyFrom string                `json:"key_from"`
	}
	doJSON(t, srv, "POST", "/api/v1/model-providers/discover", map[string]any{"protocol": "anthropic", "endpoint": anth.URL}, 200, &out)
	if gotKey != "sk-stored" || out.KeyFrom != "Claude API" || len(out.Models) != 1 || out.Models[0].ID != "claude-fable-5-2" {
		t.Fatalf("same endpoint: key %q, out %+v", gotKey, out)
	}

	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotKey = r.Header.Get("x-api-key")
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer other.Close()
	gotKey = "unset"
	out.KeyFrom = ""
	doJSON(t, srv, "POST", "/api/v1/model-providers/discover", map[string]any{"protocol": "anthropic", "endpoint": other.URL}, 200, &out)
	if gotKey != "" || out.KeyFrom != "" {
		t.Fatalf("a stored key went to another endpoint: %q (%q)", gotKey, out.KeyFrom)
	}
}
