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

//go:build oss

package api

import (
	"context"
	"net/http"
)

// Community Edition stubs for the Governance HTTP surface. The enforcement
// package (internal/governance) is paid and stripped from the CE, so these
// keep the api package compiling under -tags oss:
//   - completionBlocked never blocks (no enforcement in the CE),
//   - getGovernance reports the feature unlicensed with every policy off.
// Signatures must mirror governance.go exactly.

// completionBlocked is a no-op in the CE: completion is never governance-gated.
func (s *Server) completionBlocked(_ context.Context, _, _ string) string { return "" }

// getGovernance reports the unlicensed, all-off state the CE always sits in.
func (s *Server) getGovernance(w http.ResponseWriter, r *http.Request) {
	pid := r.PathValue("id")
	if _, err := s.store.GetProject(r.Context(), pid); err != nil {
		writeErr(w, statusFor(err), err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"licensed":     false,
		"verification": "off",
		"review":       "off",
		"architecture": "off",
		"security":     "off",
	})
}
