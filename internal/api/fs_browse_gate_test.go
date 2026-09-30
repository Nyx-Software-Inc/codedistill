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
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"codedistill/internal/features"
	"codedistill/internal/licensing"
)

// The audit finding, as a test.
//
// fs_browse hands out server-side directory names, and GET is auth-exempt
// (auth.go:51), so this gate is the only thing in front of it. Its own comment
// had said for two releases that the endpoint MUST be off once multi-user
// landed. Multi-user landed in v0.15 and the gate keyed off SSO instead, so a
// LICENSED multi-user install with no OIDC configured browsed the host
// filesystem unauthenticated — and isAdmin resolves an unauthenticated caller
// to "local", which migration 0006 seeds as an active workspace owner.
func TestFsBrowseIsRefusedOnAMultiUserServer(t *testing.T) {
	orig := features.LicenseStatus()
	t.Cleanup(func() { features.Init(orig) })

	// A licensed multi-user install. Deliberately with s.auth nil — no SSO
	// configured — because that is the combination the old gate let through.
	features.Init(&licensing.Status{
		State: licensing.StateValid,
		License: &licensing.Payload{
			Edition: "enterprise", Features: []string{"multiuser"},
			IssuedAt: time.Now().UTC(),
		},
	})
	if !features.Enabled(features.MultiUser) {
		t.Fatal("test setup failed to enable multiuser")
	}

	s := &Server{}
	rec := httptest.NewRecorder()
	s.fsBrowse(rec, httptest.NewRequest(http.MethodGet, "/api/v1/fs/browse?path=/", nil))

	if rec.Code != http.StatusForbidden {
		t.Fatalf("multi-user server answered %d — the host filesystem is browsable to an unauthenticated caller", rec.Code)
	}
	// The message has to say what to do instead, or the person configuring a
	// server just files a bug about a broken picker.
	if !strings.Contains(rec.Body.String(), "repository root") {
		t.Errorf("refusal should point at the alternative, got %q", rec.Body.String())
	}
}

// And it must still work on the single-user desktop install, which is the whole
// reason the endpoint exists. Over-gating turns the repo-root picker into a
// text box on every install.
func TestFsBrowseStillWorksSingleUser(t *testing.T) {
	orig := features.LicenseStatus()
	t.Cleanup(func() { features.Init(orig) })
	features.Init(licensing.None("single user"))

	if features.Enabled(features.MultiUser) {
		t.Fatal("test setup leaked multiuser")
	}
	s := &Server{}
	rec := httptest.NewRecorder()
	s.fsBrowse(rec, httptest.NewRequest(http.MethodGet, "/api/v1/fs/browse?path="+t.TempDir(), nil))

	if rec.Code == http.StatusForbidden {
		t.Fatalf("single-user install was refused: %s", rec.Body.String())
	}
}
