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
	"codedistill/internal/id"
	"codedistill/internal/storage/sqlite"
)

// Shared test doubles that must be visible in BOTH builds.
//
// This file exists because of a build-constraint dependency: fakeSuggester used
// to live in mcp_export_test.go, which is entirely paid-feature coverage and so
// carries //go:build !oss. architecture_test.go also uses it and must keep
// running in the community build, so tagging its old home would have broken the
// CE test build outright. Anything shared across that seam belongs here.

// fakeSuggester returns canned JSON for Suggest calls.
type fakeSuggester struct {
	resp string
	err  error
}

func (f *fakeSuggester) GenerateJSON(ctx context.Context, prompt string) (string, error) {
	return f.resp, f.err
}

// seedCodeAnchor creates an anchor directly through the store, bypassing the
// paid POST route.
//
// Used where an anchor is FIXTURE for something else under test — the review
// queue's risk scoring, the URL re-detection pass — rather than the subject of
// the test. Seeding this way keeps those tests running in the COMMUNITY build
// instead of being constrained out alongside the genuine anchor-authoring
// coverage, which would have silently dropped free behaviour from CE's suite
// (CE-review item 9).
func seedCodeAnchor(t *testing.T, store *sqlite.Store, ownerType, ownerID string, a domain.CodeAnchor) *domain.CodeAnchor {
	t.Helper()
	now := time.Now().UTC()
	a.ID = id.New()
	a.OwnerType, a.OwnerID = ownerType, ownerID
	if a.Provenance == "" {
		a.Provenance = "user-set"
	}
	a.CreatedAt, a.UpdatedAt = now, now
	if err := store.CreateCodeAnchor(context.Background(), &a); err != nil {
		t.Fatalf("seed code anchor: %v", err)
	}
	return &a
}
