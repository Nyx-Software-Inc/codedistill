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

package mcpclient

import (
	"net/http"
	"testing"
)

// EXC-16: credential headers win on collision, deterministically.
//
// Each case is a SINGLE observation. The repetition lives in the MCPHEADERS gate's own
// `-count=200`, so the number of independent observations is produced by `go test` rather than by
// anything in this file. That is deliberate: the two earlier forms of this carrier declared the
// round count as a constant and then reported it as a log line, and both were satisfiable by a
// carrier that did the work once and claimed otherwise, because both numbers were written here.
//
// `destination` models what the transport actually does with the merged map: mcp-go applies every
// entry with req.Header.Set(k, v), which canonicalizes the key, so two entries differing only in
// case collapse onto one header and the last applied wins. Ranging over a Go map visits keys in an
// unspecified order, so this models the real nondeterminism rather than smoothing it away.
func destination(t *testing.T, headers map[string]string) http.Header {
	t.Helper()
	got := http.Header{}
	for k, v := range headers {
		got.Set(k, v)
	}
	return got
}

func TestCredentialWinsCanonicalCollision(t *testing.T) {
	got := destination(t, mergeHeaders(
		map[string]string{"authorization": "Bearer caller"},
		&Credentials{Type: "bearer", Token: "credential"},
	))
	if want := "Bearer credential"; got.Get("Authorization") != want {
		t.Fatalf("Authorization = %q, want %q", got.Get("Authorization"), want)
	}
}

func TestCredentialWinsCanonicalCollisionNamedHeader(t *testing.T) {
	got := destination(t, mergeHeaders(
		map[string]string{"x-api-key": "caller"},
		&Credentials{Type: "header", Name: "X-Api-Key", Token: "credential"},
	))
	if want := "credential"; got.Get("X-Api-Key") != want {
		t.Fatalf("X-Api-Key = %q, want %q", got.Get("X-Api-Key"), want)
	}
}

// Passes on both trees: an exact-case collision is one map key, so the credential assignment
// already overwrote it before this change. It pins behaviour that must not move.
func TestCredentialWinsExactCaseCollision(t *testing.T) {
	got := destination(t, mergeHeaders(
		map[string]string{"Authorization": "Bearer caller"},
		&Credentials{Type: "bearer", Token: "credential"},
	))
	if want := "Bearer credential"; got.Get("Authorization") != want {
		t.Fatalf("Authorization = %q, want %q", got.Get("Authorization"), want)
	}
}

// Passes on both trees: a non-colliding caller header reaches the destination when no credential
// is supplied at all.
func TestUserHeadersReachTransport(t *testing.T) {
	got := destination(t, mergeHeaders(map[string]string{"X-Tenant": "acme"}, nil))
	if want := "acme"; got.Get("X-Tenant") != want {
		t.Fatalf("X-Tenant = %q, want %q", got.Get("X-Tenant"), want)
	}
}

// Passes on both trees, and it is the only case that separates the intended fix from the most
// plausible wrong one. Dropping every caller header whenever credentials are present satisfies both
// collision cases while silently discarding the tenant header a destination requires; measured, that
// mutation left the other four names green. A non-colliding caller header supplied ALONGSIDE
// credentials is what forbids it.
func TestUserHeadersSurviveCredentialMerge(t *testing.T) {
	got := destination(t, mergeHeaders(
		map[string]string{"X-Tenant": "acme", "authorization": "Bearer caller"},
		&Credentials{Type: "bearer", Token: "credential"},
	))
	// This case asserts ONLY that the non-colliding header survives. It deliberately does not
	// assert which Authorization value arrives: that property belongs to the two collision cases,
	// which are the baseline proof, and asserting it here would make this a third name that fails
	// at base — measured at 23 of 200 before this was narrowed — contradicting its own contract of
	// passing on both trees.
	if want := "acme"; got.Get("X-Tenant") != want {
		t.Fatalf("X-Tenant = %q, want %q", got.Get("X-Tenant"), want)
	}
}
