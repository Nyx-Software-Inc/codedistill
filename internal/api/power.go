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

	"codedistill/internal/power"
)

// GET /api/v1/power
//
// Returns the current power source as a tiny JSON blob the SPA polls to
// drive (a) the live "on battery" / "indexing throttled" status badge
// and (b) the live indicator on the Indexing settings panel. The
// underlying source is cached by power.Watcher and refreshed every
// 30s, so this handler is just a synchronous read — no OS calls per
// request.
//
// Shape: { "source": "ac" | "battery" | "unknown" }.
//
// When the server was constructed without WithPowerSource (tests, or
// builds where the watcher isn't wired) the handler returns "unknown".
// SPA treats unknown the same as AC for badge purposes.
func (s *Server) powerStatus(w http.ResponseWriter, r *http.Request) {
	src := power.SourceUnknown
	if s.powerSource != nil {
		src = s.powerSource()
	}
	writeJSON(w, http.StatusOK, map[string]string{"source": src.String()})
}
