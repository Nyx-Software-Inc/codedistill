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

package licensing

import (
	"crypto/ed25519"
	"crypto/rand"
	"testing"
	"time"
)

func testKeys(t *testing.T) (ed25519.PublicKey, ed25519.PrivateKey) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return pub, priv
}

func basePayload() *Payload {
	return &Payload{
		LicenseID: "lic-test-1",
		Customer:  "Test Co",
		Edition:   "pro",
		Seats:     5,
		Features:  []string{"mcp", "anchors", "dedup"},
		IssuedAt:  time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	}
}

func sign(t *testing.T, p *Payload, priv ed25519.PrivateKey) []byte {
	t.Helper()
	raw, err := Sign(p, priv)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

var now = time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)

func TestVerifyValid(t *testing.T) {
	pub, priv := testKeys(t)
	st := Verify(sign(t, basePayload(), priv), pub, now, "m1:abc", "0.10.0")
	if st.State != StateValid {
		t.Fatalf("state = %s (%s), want valid", st.State, st.Reason)
	}
	if !st.Active() || !st.HasFeature("mcp") || st.HasFeature("s3") {
		t.Fatalf("feature grants wrong: active=%v mcp=%v s3=%v",
			st.Active(), st.HasFeature("mcp"), st.HasFeature("s3"))
	}
}

func TestVerifyTamperedPayload(t *testing.T) {
	pub, priv := testKeys(t)
	raw := sign(t, basePayload(), priv)
	// Flip a byte inside the base64 payload region.
	for i := range raw {
		if raw[i] == 'A' {
			raw[i] = 'B'
			break
		}
	}
	if st := Verify(raw, pub, now, "", "0.10.0"); st.State != StateInvalid {
		t.Fatalf("state = %s, want invalid", st.State)
	}
}

func TestVerifyWrongKey(t *testing.T) {
	_, priv := testKeys(t)
	otherPub, _ := testKeys(t)
	if st := Verify(sign(t, basePayload(), priv), otherPub, now, "", "0.10.0"); st.State != StateInvalid {
		t.Fatalf("state = %s, want invalid", st.State)
	}
}

func TestVerifyExpiryAndGrace(t *testing.T) {
	pub, priv := testKeys(t)
	p := basePayload()
	p.ExpiresAt = now.Add(-24 * time.Hour) // expired yesterday
	raw := sign(t, p, priv)

	st := Verify(raw, pub, now, "", "0.10.0")
	if st.State != StateGrace {
		t.Fatalf("state = %s, want grace", st.State)
	}
	if !st.Active() || !st.HasFeature("mcp") {
		t.Fatal("grace period should keep features on")
	}
	wantGrace := p.ExpiresAt.Add(GracePeriod)
	if !st.GraceUntil.Equal(wantGrace) {
		t.Fatalf("grace until %v, want %v", st.GraceUntil, wantGrace)
	}

	// Past grace → expired, features off.
	st = Verify(raw, pub, now.Add(GracePeriod), "", "0.10.0")
	if st.State != StateExpired || st.Active() || st.HasFeature("mcp") {
		t.Fatalf("past grace: state=%s active=%v", st.State, st.Active())
	}
}

func TestVerifyFingerprintLock(t *testing.T) {
	pub, priv := testKeys(t)
	p := basePayload()
	p.Lock = Lock{Type: "machine", FP: "m1:expected"}
	raw := sign(t, p, priv)

	if st := Verify(raw, pub, now, "m1:expected", "0.10.0"); st.State != StateValid {
		t.Fatalf("matching fp: state = %s (%s)", st.State, st.Reason)
	}
	st := Verify(raw, pub, now, "m1:other", "0.10.0")
	if st.State != StateInvalid {
		t.Fatalf("mismatched fp: state = %s, want invalid", st.State)
	}
	if st.License == nil {
		t.Fatal("invalid-but-authentic license should still expose its payload for status display")
	}
}

func TestVerifyVersionRange(t *testing.T) {
	pub, priv := testKeys(t)
	p := basePayload()
	p.MinVersion = "0.10.0"
	p.MaxVersion = "1.0.0"
	raw := sign(t, p, priv)

	cases := []struct {
		version string
		want    State
	}{
		{"0.9.6", StateInvalid},
		{"0.10.0", StateValid},
		{"0.10.0-dev", StateValid}, // pre-release suffix ignored
		{"1.0.0", StateValid},
		{"1.0.1", StateInvalid},
	}
	for _, c := range cases {
		if st := Verify(raw, pub, now, "", c.version); st.State != c.want {
			t.Errorf("version %s: state = %s, want %s (%s)", c.version, st.State, c.want, st.Reason)
		}
	}
}

func TestVerifyGarbage(t *testing.T) {
	pub, _ := testKeys(t)
	for _, raw := range [][]byte{nil, []byte("{}"), []byte("not json"), []byte(`{"v":2,"payload":"","sig":""}`)} {
		if st := Verify(raw, pub, now, "", "0.10.0"); st.State != StateInvalid {
			t.Errorf("garbage %q: state = %s, want invalid", raw, st.State)
		}
	}
}

func TestPerpetualLicense(t *testing.T) {
	pub, priv := testKeys(t)
	// Zero ExpiresAt = perpetual; verify far in the future.
	st := Verify(sign(t, basePayload(), priv), pub, now.AddDate(50, 0, 0), "", "0.10.0")
	if st.State != StateValid {
		t.Fatalf("perpetual license: state = %s", st.State)
	}
}

func TestCompareVersions(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"0.9.6", "0.10.0", -1},
		{"0.10.0", "0.10.0", 0},
		{"v0.10.0", "0.10.0", 0},
		{"0.10.0-dev", "0.10.0", 0},
		{"1.2", "1.2.0", 0},
		{"2.0.0", "1.9.9", 1},
	}
	for _, c := range cases {
		if got := compareVersions(c.a, c.b); got != c.want {
			t.Errorf("compareVersions(%q, %q) = %d, want %d", c.a, c.b, got, c.want)
		}
	}
}

// Dual-key trust (activation service): a license signed by the SELF-SERVE key
// verifies through VerifyAny's trust list; one signed by an untrusted key is
// rejected by every key; the master key keeps working.
func TestVerifyAnyDualKey(t *testing.T) {
	now := time.Date(2026, 7, 23, 12, 0, 0, 0, time.UTC)
	mkKey := func() (ed25519.PublicKey, ed25519.PrivateKey) {
		pub, priv, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			t.Fatal(err)
		}
		return pub, priv
	}
	masterPub, masterPriv := mkKey()
	selfPub, selfPriv := mkKey()
	_, roguePriv := mkKey()
	trust := []ed25519.PublicKey{masterPub, selfPub}

	sign := func(priv ed25519.PrivateKey) []byte {
		raw, err := Sign(&Payload{LicenseID: "l1", Customer: "C", Edition: "pro",
			Features: []string{"mcp"}, IssuedAt: now}, priv)
		if err != nil {
			t.Fatal(err)
		}
		return raw
	}

	if st := VerifyAny(sign(masterPriv), trust, now, "", "1.0.0"); st.State != StateValid {
		t.Fatalf("master-signed should verify: %s (%s)", st.State, st.Reason)
	}
	if st := VerifyAny(sign(selfPriv), trust, now, "", "1.0.0"); st.State != StateValid {
		t.Fatalf("self-serve-signed should verify: %s (%s)", st.State, st.Reason)
	}
	if st := VerifyAny(sign(roguePriv), trust, now, "", "1.0.0"); st.State == StateValid {
		t.Fatal("rogue-signed must NOT verify")
	}
	if st := VerifyAny(sign(masterPriv), nil, now, "", "1.0.0"); st.State == StateValid {
		t.Fatal("empty trust list must not verify")
	}
}

// Audit C4: a license that verifies under the SELF-SERVE key is trusted only
// for edition "pro". A non-pro payload under that key means a compromised
// service key and must be rejected — the blast-radius cap, in code.
func TestVerifyAnySelfServeProOnly(t *testing.T) {
	now := time.Date(2026, 8, 3, 12, 0, 0, 0, time.UTC)
	masterPub, masterPriv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	selfPub, selfPriv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	// Point the cap at the test's self-serve key for the duration.
	orig := selfServeKey
	selfServeKey = selfPub
	t.Cleanup(func() { selfServeKey = orig })

	trust := []ed25519.PublicKey{masterPub, selfPub}
	sign := func(priv ed25519.PrivateKey, edition string, feats []string) []byte {
		raw, err := Sign(&Payload{LicenseID: "l1", Customer: "C", Edition: edition,
			Features: feats, IssuedAt: now}, priv)
		if err != nil {
			t.Fatal(err)
		}
		return raw
	}

	// Self-serve Pro: accepted.
	if st := VerifyAny(sign(selfPriv, "pro", []string{"mcp"}), trust, now, "", "1.0.0"); st.State != StateValid {
		t.Fatalf("self-serve Pro should verify: %s (%s)", st.State, st.Reason)
	}
	// Self-serve Enterprise (the compromise case): rejected.
	if st := VerifyAny(sign(selfPriv, "enterprise", []string{"multiuser", "s3"}), trust, now, "", "1.0.0"); st.State == StateValid {
		t.Fatal("self-serve Enterprise must be rejected (blast-radius cap)")
	}
	// The MASTER key may still sign Enterprise — the cap is key-specific.
	if st := VerifyAny(sign(masterPriv, "enterprise", []string{"multiuser"}), trust, now, "", "1.0.0"); st.State != StateValid {
		t.Fatalf("master-signed Enterprise should verify: %s (%s)", st.State, st.Reason)
	}
}
