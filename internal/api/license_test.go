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
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"codedistill/internal/features"
	"codedistill/internal/licensing"
)

// gateMux returns a handler with only license-relevant plumbing — no
// storage needed because the gate rejects before the handler runs.
func gateMux() http.Handler {
	s := &Server{}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/license", s.licenseStatus)
	mux.HandleFunc("POST /gated", s.requireFeature(features.Dedup, func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"ok": "true"})
	}))
	return mux
}

func TestRequireFeature_Locked(t *testing.T) {
	defer features.Init(allFeaturesStatus()) // restore for other tests

	srv := httptest.NewServer(gateMux())
	defer srv.Close()

	// Licensed (TestMain default) → passes through.
	resp, err := http.Post(srv.URL+"/gated", "application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("licensed: status %d, want 200", resp.StatusCode)
	}

	// Unlicensed → 402 with the stable feature_locked code.
	features.Init(licensing.None("test"))
	resp, err = http.Post(srv.URL+"/gated", "application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusPaymentRequired {
		t.Fatalf("unlicensed: status %d, want 402", resp.StatusCode)
	}
	var body struct {
		Code    string `json:"code"`
		Feature string `json:"feature"`
		Error   string `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body.Code != "feature_locked" || body.Feature != "dedup" {
		t.Fatalf("body = %+v, want code=feature_locked feature=matcher", body)
	}
}

func TestLicenseStatusEndpoint(t *testing.T) {
	defer features.Init(allFeaturesStatus())

	srv := httptest.NewServer(gateMux())
	defer srv.Close()

	get := func() LicenseInfo {
		t.Helper()
		resp, err := http.Get(srv.URL + "/api/v1/license")
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status %d, want 200", resp.StatusCode)
		}
		var info LicenseInfo
		if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
			t.Fatal(err)
		}
		return info
	}

	// Licensed: full feature list, edition surfaced.
	info := get()
	if info.State != "valid" || info.Edition != "enterprise" || len(info.Features) != len(features.All) {
		t.Fatalf("licensed info = %+v", info)
	}

	// Unlicensed: state none, empty (not null) features.
	features.Init(licensing.None("no license file"))
	info = get()
	if info.State != "none" || len(info.Features) != 0 {
		t.Fatalf("unlicensed info = %+v", info)
	}
	if !strings.Contains(info.Reason, "no license") {
		t.Fatalf("reason = %q, want pass-through of loader reason", info.Reason)
	}
}
