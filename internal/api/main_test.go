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
	"os"
	"testing"
	"time"

	"codedistill/internal/features"
	"codedistill/internal/licensing"
)

// TestMain installs a fully-featured license status so endpoint tests
// exercise handler behavior rather than the license gate. The gate
// itself is covered by TestRequireFeature_Locked, which swaps in an
// unlicensed status and restores this one.
func TestMain(m *testing.M) {
	features.Init(allFeaturesStatus())
	os.Exit(m.Run())
}

func allFeaturesStatus() *licensing.Status {
	feats := make([]string, len(features.All))
	for i, f := range features.All {
		feats[i] = string(f)
	}
	return &licensing.Status{
		State: licensing.StateValid,
		License: &licensing.Payload{
			LicenseID: "lic-test",
			Customer:  "Test Harness",
			Edition:   "enterprise",
			Features:  feats,
			IssuedAt:  time.Now().UTC(),
		},
	}
}
