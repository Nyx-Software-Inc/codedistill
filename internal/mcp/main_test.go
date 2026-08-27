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

package mcp

import (
	"os"
	"testing"
	"time"

	"codedistill/internal/features"
	"codedistill/internal/licensing"
)

// TestMain installs a fully-featured license so tests exercise tool behavior
// rather than the feature gate (mirrors internal/api). Lets the governance gate
// in mark_implemented actually enforce under test.
func TestMain(m *testing.M) {
	feats := make([]string, len(features.All))
	for i, f := range features.All {
		feats[i] = string(f)
	}
	features.Init(&licensing.Status{
		State: licensing.StateValid,
		License: &licensing.Payload{
			LicenseID: "lic-test", Customer: "Test Harness", Edition: "enterprise",
			Features: feats, IssuedAt: time.Now().UTC(),
		},
	})
	os.Exit(m.Run())
}
