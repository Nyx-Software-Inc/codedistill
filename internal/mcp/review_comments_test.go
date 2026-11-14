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

// list_review_comments is a free read that validates its inputs; the
// changeset resolution and the comment store are covered in internal/git and
// internal/api.
func TestListReviewComments_UnknownProject(t *testing.T) {
	fx := newFixture(t)
	res := fx.callTool("list_review_comments", map[string]any{
		"project_name": "does-not-exist",
		"head":         "main",
	})
	if !res.IsError {
		t.Fatalf("expected an error for an unknown project")
	}
	res = fx.callTool("list_review_comments", map[string]any{"project_name": "x"})
	if !res.IsError {
		t.Fatalf("expected an error without head")
	}
}
