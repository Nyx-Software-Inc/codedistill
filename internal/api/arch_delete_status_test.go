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
)

// DELETE of a node that does not exist answered 204 No Content — a false
// success — because DeleteArchitectureNode did no RowsAffected check and the
// handler hardcoded 500 for everything else.
//
// 404 chosen over idempotent-204 deliberately (Rich's call): every other delete
// in the codebase surfaces ErrNotFound, and for the architecture diagram
// specifically a silent success is the wrong failure mode. A stale node id held
// by the UI would "delete successfully" and the user would believe the diagram
// changed — in the one feature built with an as-built overlay so it cannot lie
// (CE-review item 31).
func TestDeleteArchNode_MissingIs404(t *testing.T) {
	srv, _, store := setupWithStore(t)
	ctx := context.Background()
	now := time.Now().UTC()
	if err := store.CreateProject(ctx, &domain.Project{ID: "p1", Name: "P", CreatedAt: now}); err != nil {
		t.Fatal(err)
	}
	if err := store.CreateArchitectureNode(ctx, &domain.ArchitectureNode{
		ID: "n1", ProjectID: "p1", Name: "Billing", Kind: "service",
		Provenance: "ratified", CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}

	doJSON(t, srv, "DELETE", "/api/v1/architecture/nodes/no-such-node", nil, 404, nil)
	// The real delete still returns 204 — the check must not break valid deletes.
	doJSON(t, srv, "DELETE", "/api/v1/architecture/nodes/n1", nil, 204, nil)
	// And a repeat of a now-gone node is 404 too, not a second silent success.
	doJSON(t, srv, "DELETE", "/api/v1/architecture/nodes/n1", nil, 404, nil)
}
