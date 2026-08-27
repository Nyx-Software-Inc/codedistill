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

// Package licensing implements the Ed25519-signed license file that
// unlocks paid features (see internal/features). Design per BACKLOG
// "License-key verification for self-hosted enterprise": a signed JSON
// payload carrying edition, seats, features, expiry, version range and
// an optional machine/server lock.
//
// Enforcement philosophy is degrade-to-free: a missing, invalid or
// expired license never stops the binary or blocks reads/exports — it
// only switches paid features off (after a 14-day post-expiry grace
// window). The license is therefore a capability grant, not a kill
// switch.
package licensing

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// GracePeriod is how long past expires_at paid features keep working.
// The SPA shows a renewal banner for the whole window.
const GracePeriod = 14 * 24 * time.Hour

// Lock binds a license to one install. Type "machine" matches the
// single-user machine fingerprint (see Fingerprint); "server" is the
// same mechanism with per-server intent — the distinction is for
// humans reading the license, verification treats them identically.
// A zero Lock means the license is not node-locked.
type Lock struct {
	Type string `json:"type,omitempty"` // "machine" | "server"
	FP   string `json:"fp,omitempty"`   // value Fingerprint() must equal
}

// Payload is the signed body of a license. Field order doesn't matter
// for verification — the signature covers the exact raw bytes embedded
// in the envelope, not a re-marshaled form.
type Payload struct {
	LicenseID  string    `json:"license_id"`
	Customer   string    `json:"customer"`
	Email      string    `json:"email,omitempty"`
	Edition    string    `json:"edition"` // "pro" | "enterprise"
	Seats      int       `json:"seats,omitempty"`
	Features   []string  `json:"features"`
	IssuedAt   time.Time `json:"issued_at"`
	ExpiresAt  time.Time `json:"expires_at,omitempty"` // zero = perpetual
	MinVersion string    `json:"min_version,omitempty"`
	MaxVersion string    `json:"max_version,omitempty"`
	Lock       Lock      `json:"lock,omitempty"`
}

// envelope is the on-disk file shape. payload is base64(std) of the
// payload JSON; sig is base64(std) of the Ed25519 signature over those
// exact payload bytes.
type envelope struct {
	V       int    `json:"v"`
	Payload string `json:"payload"`
	Sig     string `json:"sig"`
}

// State is the verification outcome, ordered roughly by health.
type State string

const (
	StateNone    State = "none"    // no license file found
	StateInvalid State = "invalid" // bad signature, parse error, lock or version mismatch
	StateValid   State = "valid"
	StateGrace   State = "grace"   // expired, inside GracePeriod
	StateExpired State = "expired" // expired, past grace
)

// Status is what the rest of the app consumes: the decoded payload (nil
// unless the signature checked out) plus the effective state. Paid
// features are on iff Active().
type Status struct {
	State      State
	License    *Payload
	GraceUntil time.Time // set when State is grace or expired
	Reason     string    // human-readable detail for invalid/none
}

// Active reports whether the license currently grants its features.
func (s *Status) Active() bool {
	return s != nil && (s.State == StateValid || s.State == StateGrace)
}

// HasFeature reports whether the license grants the named feature and
// is active. Callers normally go through internal/features instead.
func (s *Status) HasFeature(name string) bool {
	if !s.Active() || s.License == nil {
		return false
	}
	for _, f := range s.License.Features {
		if f == name {
			return true
		}
	}
	return false
}

// None returns the Status for "no license present".
func None(reason string) *Status {
	return &Status{State: StateNone, Reason: reason}
}

func invalid(reason string) *Status {
	return &Status{State: StateInvalid, Reason: reason}
}

// reasonBadSignature is the exact Verify reason for a signature that no key
// produced — VerifyAny keys its try-the-next-key decision off it.
const reasonBadSignature = "signature verification failed"

// selfServeKey is the key VerifyAny applies the pro-only cap to. A package
// var (not the const-derived embedded key directly) so tests can exercise
// the cap with a throwaway keypair.
var selfServeKey = ed25519.PublicKey(embeddedSelfServeKey)

// VerifyAny verifies raw against a trust list: the first key whose signature
// matches decides the outcome. Every other failure mode (lock mismatch,
// expiry, version range) is returned as-is — only a bad signature moves on to
// the next key. Backs the dual-key model (offline master + the activation
// service's self-serve key).
//
// Blast-radius cap (audit C4): a license that verifies under the SELF-SERVE
// key is only trusted for edition "pro". The activation service never mints
// anything else, so a non-pro payload under that key can only mean a
// compromised service key — enforcing the documented "can mint Pro, never
// Enterprise" invariant in code, not comments.
func VerifyAny(raw []byte, pubs []ed25519.PublicKey, now time.Time, fingerprint, version string) *Status {
	if len(pubs) == 0 {
		return invalid("no trusted signing keys")
	}
	var last *Status
	for _, pub := range pubs {
		last = Verify(raw, pub, now, fingerprint, version)
		if !(last.State == StateInvalid && last.Reason == reasonBadSignature) {
			if pub.Equal(selfServeKey) &&
				last.License != nil && last.License.Edition != "pro" {
				return invalid("self-serve licenses are pro-only (edition " + last.License.Edition + " requires a master-signed license)")
			}
			return last
		}
	}
	return last
}

// Sign produces the on-disk license file bytes for a payload. Lives
// here (not in licensegen) so tests can round-trip with a throwaway
// key; the production private key only ever touches licensegen.
func Sign(p *Payload, priv ed25519.PrivateKey) ([]byte, error) {
	body, err := json.Marshal(p)
	if err != nil {
		return nil, fmt.Errorf("marshal payload: %w", err)
	}
	env := envelope{
		V:       1,
		Payload: base64.StdEncoding.EncodeToString(body),
		Sig:     base64.StdEncoding.EncodeToString(ed25519.Sign(priv, body)),
	}
	return json.MarshalIndent(env, "", "  ")
}

// Verify checks raw license-file bytes against pub and the current
// environment. fingerprint is the local machine fingerprint (used only
// when the license carries a lock); version is the running binary
// version for min/max range checks. Never returns an error — every
// failure mode is a Status the caller can render.
func Verify(raw []byte, pub ed25519.PublicKey, now time.Time, fingerprint, version string) *Status {
	var env envelope
	if err := json.Unmarshal(raw, &env); err != nil {
		return invalid("license file is not valid JSON: " + err.Error())
	}
	if env.V != 1 {
		return invalid(fmt.Sprintf("unsupported license format v%d", env.V))
	}
	body, err := base64.StdEncoding.DecodeString(env.Payload)
	if err != nil {
		return invalid("payload is not valid base64")
	}
	sig, err := base64.StdEncoding.DecodeString(env.Sig)
	if err != nil {
		return invalid("signature is not valid base64")
	}
	if !ed25519.Verify(pub, body, sig) {
		return invalid(reasonBadSignature)
	}
	var p Payload
	if err := json.Unmarshal(body, &p); err != nil {
		return invalid("signed payload is not valid JSON: " + err.Error())
	}

	// Signature is good — every later failure still reports the decoded
	// payload so `license status` can show what the file claims to be.
	if p.Lock.FP != "" && p.Lock.FP != fingerprint {
		return &Status{State: StateInvalid, License: &p,
			Reason: "license is locked to a different machine (run `codedistill license fingerprint` and request a re-issue)"}
	}
	if p.MinVersion != "" && compareVersions(version, p.MinVersion) < 0 {
		return &Status{State: StateInvalid, License: &p,
			Reason: fmt.Sprintf("license requires version >= %s (running %s)", p.MinVersion, version)}
	}
	if p.MaxVersion != "" && compareVersions(version, p.MaxVersion) > 0 {
		return &Status{State: StateInvalid, License: &p,
			Reason: fmt.Sprintf("license covers versions up to %s (running %s) — renew to use this release", p.MaxVersion, version)}
	}

	if !p.ExpiresAt.IsZero() && now.After(p.ExpiresAt) {
		graceUntil := p.ExpiresAt.Add(GracePeriod)
		if now.Before(graceUntil) {
			return &Status{State: StateGrace, License: &p, GraceUntil: graceUntil}
		}
		return &Status{State: StateExpired, License: &p, GraceUntil: graceUntil,
			Reason: "license expired " + p.ExpiresAt.Format("2006-01-02")}
	}
	return &Status{State: StateValid, License: &p}
}

// compareVersions compares two dotted semver-ish strings numerically,
// ignoring any pre-release suffix ("0.10.0-dev" == "0.10.0"). Missing
// segments count as zero, so "1.2" == "1.2.0". Returns -1/0/+1.
func compareVersions(a, b string) int {
	pa, pb := splitVersion(a), splitVersion(b)
	for i := 0; i < 3; i++ {
		if pa[i] != pb[i] {
			if pa[i] < pb[i] {
				return -1
			}
			return 1
		}
	}
	return 0
}

func splitVersion(v string) [3]int {
	v = strings.TrimPrefix(strings.TrimSpace(v), "v")
	if i := strings.IndexAny(v, "-+"); i >= 0 {
		v = v[:i]
	}
	var out [3]int
	for i, seg := range strings.SplitN(v, ".", 3) {
		n, err := strconv.Atoi(seg)
		if err != nil {
			return out
		}
		out[i] = n
	}
	return out
}
