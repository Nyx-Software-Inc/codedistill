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
	"testing"

	"codedistill/internal/domain"
)

func TestVerificationAPI_NoVerifier(t *testing.T) {
	srv, ag := setup(t)

	var p domain.Project
	doJSON(t, srv, "POST", "/api/v1/projects", map[string]string{"name": "p"}, 201, &p)
	var sp domain.Scratchpad
	doJSON(t, srv, "POST", "/api/v1/projects/"+p.ID+"/scratchpads", map[string]string{"name": "extra"}, 201, &sp)
	var item domain.ScratchpadItem
	doJSON(t, srv, "POST", "/api/v1/scratchpads/"+sp.ID+"/items",
		map[string]string{"content": "boom 500", "classification_override": "bug"}, 201, &item)

	// Drain the agent so the bug is derived.
	ag.Stop()
	var got domain.ScratchpadItem
	doJSON(t, srv, "GET", "/api/v1/items/"+item.ID, nil, 200, &got)
	if got.DerivedItemID == "" {
		t.Fatalf("no derived bug; state=%q", got.ClassificationState)
	}
	bugID := got.DerivedItemID

	// GET is a free read even with no verifier wired: empty results, and the
	// response reports verification is unavailable on this server.
	var resp verificationResp
	doJSON(t, srv, "GET", "/api/v1/bugs/"+bugID+"/verification", nil, 200, &resp)
	if len(resp.Results) != 0 {
		t.Errorf("expected 0 results, got %d", len(resp.Results))
	}
	if resp.Available {
		t.Errorf("Available should be false when no verifier is wired")
	}
	if len(resp.Checks) != 0 {
		t.Errorf("Checks should be empty, got %d", len(resp.Checks))
	}

	// POST trigger with no verifier wired → 503.
	doJSON(t, srv, "POST", "/api/v1/bugs/"+bugID+"/verify", nil, 503, nil)

	// Unknown owner → 404 on both verbs.
	doJSON(t, srv, "GET", "/api/v1/bugs/no-such-id/verification", nil, 404, nil)
	doJSON(t, srv, "POST", "/api/v1/bugs/no-such-id/verify", nil, 404, nil)
}
