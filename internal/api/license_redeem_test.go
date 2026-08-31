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
	minVersion string // when set, the signed payload carries this min_version
	maxVersion string // when set, the signed payload carries this max_version
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
			ExpiresAt:  time.Now().UTC().AddDate(1, 0, 0),
			Lock:       licensing.Lock{Type: "machine", FP: req.Fingerprint},
			MinVersion: f.minVersion,
			MaxVersion: f.maxVersion,
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

// TestLicenseRedeemVersionRange pins the complete consumer set of the build version
// that redemption hands to licensing.VerifyAny. Verify reads that one argument in TWO
// guards — MinVersion at licensing.go:215 and MaxVersion at :219 — and the two move in
// OPPOSITE directions once the argument stops being "": a satisfied minimum goes from
// refused to accepted, and an exceeded maximum goes from accepted to refused. The name
// says "range" because the population is both guards.
//
// Each guard is bracketed in both directions, and three of the four arms fail against
// base source. The max direction is the load-bearing one: splitVersion("") is the zero
// version, so compareVersions("", max) > 0 can never hold and that guard was unreachable
// at redemption, which let a licence whose coverage window had ended install even though
// the CLI path — which passes the real version — refuses the same file at the next start.
// The min direction shows the same empty argument as `license requires version >= X
// (running )`, the blank "running" value being the tell.
//
// max_version ABOVE the running version is the deliberate control: it is correct on both
// sides of the fix. Without it, a carrier whose every arm failed at base could be
// satisfied by an implementation that refused every versioned licence.
//
// The running version is NUMERIC and that is load-bearing rather than cosmetic: the
// shared api harness builds its Server with Version "test", and splitVersion("test") is
// ALSO the zero version, so a carrier inheriting the default would reach the same verdict
// on both sides and observe nothing. Both refusal arms assert the RENDERED running
// version for the same reason — that string is what distinguishes the real version
// from "".
func TestLicenseRedeemVersionRange(t *testing.T) {
	const running = "1.2.3"

	redeemWith := func(t *testing.T, minVersion, maxVersion string) (int, string) {
		t.Helper()
		pub, priv, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			t.Fatal(err)
		}
		fake := &fakeActivation{priv: priv, minVersion: minVersion, maxVersion: maxVersion}
		act := httptest.NewServer(fake.handler())
		t.Cleanup(act.Close)

		srv, _, _ := setupWithStore(t)
		s := apiSrvFromTest(t, srv)
		// The harness default is "test", which compares equal to "": see above.
		s.build.Version = running
		s.redeem = &redeemConfig{
			dest: filepath.Join(t.TempDir(), licensing.FileName), activateURL: act.URL,
			pubkeys: []ed25519.PublicKey{pub},
		}
		req := httptest.NewRequest(http.MethodPost, "/api/v1/license/redeem",
			strings.NewReader(`{"claim_code":"cdk_minver"}`))
		rec := httptest.NewRecorder()
		s.redeemLicense(rec, req)
		return rec.Code, rec.Body.String()
	}

	// One refusal assertion for both guards. The reason is DECODED rather than
	// substring-matched on the raw body: writeMsg emits JSON and Go escapes ">" as
	// \u003e, so a raw match fails on a body that is in fact correct.
	assertRefused := func(t *testing.T, code int, body, want string) {
		t.Helper()
		if code != http.StatusBadGateway {
			t.Fatalf("status %d, want 502; body %s", code, body)
		}
		var refusal struct {
			Error string `json:"error"`
		}
		if err := json.Unmarshal([]byte(body), &refusal); err != nil {
			t.Fatalf("refusal body is not JSON: %v\nbody: %s", err, body)
		}
		if !strings.Contains(refusal.Error, want) {
			t.Errorf("refusal reason %q does not contain %q; a blank running value means the "+
				"build version never reached VerifyAny", refusal.Error, want)
		}
	}

	// --- min_version BELOW the running version: redemption must succeed --------
	// Discriminating for the min guard. At base this is a 502, because "" compares
	// below every non-zero minimum.
	t.Run("min_version below the running version is accepted", func(t *testing.T) {
		code, body := redeemWith(t, "1.0.0", "")
		if code != http.StatusOK {
			t.Fatalf("redeem with min_version 1.0.0 and running %s: status %d, body %s; the "+
				"server must verify against its own build version", running, code, body)
		}
	})

	// --- min_version ABOVE the running version: still refused ------------------
	// Proves the fix passes the REAL version rather than disabling the check, and the
	// rendered running value proves which version reached the verifier.
	t.Run("min_version above the running version is refused with the running version named", func(t *testing.T) {
		code, body := redeemWith(t, "2.0.0", "")
		assertRefused(t, code, body, "license requires version >= 2.0.0 (running "+running+")")
	})

	// --- max_version ABOVE the running version: redemption must succeed --------
	// The control for the max guard. It passes at base too, and it has to: without it
	// the arm below could be satisfied by refusing every max_version license.
	t.Run("max_version above the running version is accepted", func(t *testing.T) {
		code, body := redeemWith(t, "", "2.0.0")
		if code != http.StatusOK {
			t.Fatalf("redeem with max_version 2.0.0 and running %s: status %d, body %s; a "+
				"license covering this release must install", running, code, body)
		}
	})

	// --- max_version BELOW the running version: must be refused ---------------
	// Discriminating for the max guard, and the arm that fails at base in the
	// ACCEPT -> REFUSE direction: with "" the comparison can never exceed the bound, so
	// the guard was dead and an out-of-coverage license installed silently.
	t.Run("max_version below the running version is refused with the running version named", func(t *testing.T) {
		code, body := redeemWith(t, "", "1.0.0")
		assertRefused(t, code, body, "license covers versions up to 1.0.0 (running "+running+")")
	})
}
