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
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"codedistill/internal/licensing"
)

// Fake activation service: signs a machine-locked license for whatever
// fingerprint the request carries, records the codes it saw, and can be
// switched to a refusal mode.
type fakeActivation struct {
	priv       ed25519.PrivateKey
	seenCodes  []string
	refuse     int    // when non-zero, respond with this status
	refuseBody string // body for the refusal
}

func (f *fakeActivation) handler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			ClaimCode   string `json:"claim_code"`
			Fingerprint string `json:"fingerprint"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		f.seenCodes = append(f.seenCodes, req.ClaimCode)
		if f.refuse != 0 {
			http.Error(w, f.refuseBody, f.refuse)
			return
		}
		lic, _ := licensing.Sign(&licensing.Payload{
			LicenseID: "lt", Customer: "Redeem Tester", Edition: "pro",
			Features: []string{"mcp"}, IssuedAt: time.Now().UTC(),
			ExpiresAt: time.Now().UTC().AddDate(1, 0, 0),
			Lock:      licensing.Lock{Type: "machine", FP: req.Fingerprint},
		}, f.priv)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"license":  base64.StdEncoding.EncodeToString(lic),
			"customer": "Redeem Tester", "edition": "pro",
			"expires_at": time.Now().UTC().AddDate(1, 0, 0),
		})
	}
}

func TestLicenseRedeemAndRefresh(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	fake := &fakeActivation{priv: priv}
	act := httptest.NewServer(fake.handler())
	defer act.Close()

	srv, _, _ := setupWithStore(t)
	dir := t.TempDir()
	dest := filepath.Join(dir, licensing.FileName)
	apiSrvFromTest(t, srv).redeem = &redeemConfig{
		dest: dest, activateURL: act.URL,
		pubkeys: []ed25519.PublicKey{pub},
	}

	// Redeem installs the license + stores the claim code.
	var out struct {
		Customer        string `json:"customer"`
		RestartRequired bool   `json:"restart_required"`
	}
	doJSON(t, srv, "POST", "/api/v1/license/redeem",
		map[string]string{"claim_code": "cdk_testcode"}, 200, &out)
	if out.Customer != "Redeem Tester" || !out.RestartRequired {
		t.Fatalf("redeem response: %+v", out)
	}
	raw, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("license not installed: %v", err)
	}
	fp, _ := licensing.Fingerprint()
	if st := licensing.VerifyAny(raw, []ed25519.PublicKey{pub}, time.Now().UTC(), fp, ""); st.State != licensing.StateValid {
		t.Fatalf("installed license invalid for this machine: %s (%s)", st.State, st.Reason)
	}
	if code, _ := os.ReadFile(filepath.Join(dir, "claim-code")); strings.TrimSpace(string(code)) != "cdk_testcode" {
		t.Fatalf("claim code not stored: %q", code)
	}

	// Refresh re-uses the stored code without the client resending it.
	doJSON(t, srv, "POST", "/api/v1/license/refresh", nil, 200, nil)
	if len(fake.seenCodes) != 2 || fake.seenCodes[1] != "cdk_testcode" {
		t.Fatalf("refresh should reuse the stored code: %v", fake.seenCodes)
	}

	// Service refusals surface verbatim with their status (seats exhausted).
	fake.refuse, fake.refuseBody = http.StatusConflict, "all seats on this purchase have been redeemed"
	doJSON(t, srv, "POST", "/api/v1/license/redeem",
		map[string]string{"claim_code": "cdk_testcode"}, http.StatusConflict, nil)
}

// apiSrvFromTest digs the *Server out of the httptest fixture. newTestServer
// returns the httptest server whose handler wraps our mux; we need the Server
// value itself to set the redeem config — expose it via the fixture's store of
// truth: reconstructing is cheaper than replumbing, so the fixture registers
// the last-built Server in a package var.
func apiSrvFromTest(t *testing.T, _ *httptest.Server) *Server {
	t.Helper()
	if lastTestServer == nil {
		t.Fatal("newTestServer did not record the Server")
	}
	return lastTestServer
}
