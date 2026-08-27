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

import "testing"

// get_commit_changes is registered as a free read and validates its inputs — an
// unknown project is an error, not a panic. (The diff/hunk correctness itself is
// covered at the git layer: TestCommitChanges_* in internal/git.)
func TestGetCommitChanges_UnknownProject(t *testing.T) {
	fx := newFixture(t)
	res := fx.callTool("get_commit_changes", map[string]any{
		"project_name": "does-not-exist",
		"commit":       "abc1234def",
	})
	if !res.IsError {
		t.Fatalf("expected an error for an unknown project")
	}
}
