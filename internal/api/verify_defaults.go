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

	"codedistill/internal/verifyprofile"
)

// verifyDefaults handles GET /api/v1/projects/{id}/verify-defaults. It detects
// the project's language from its repo_root marker files and returns example
// verification commands for it, so the Verification settings show language-
// appropriate placeholders (and a "use these defaults" affordance) instead of
// hardcoded Go examples. Language == "" means undetected — the UI shows neutral
// hints. Always 200 (a missing/unknown repo is a normal, non-error case).
func (s *Server) verifyDefaults(w http.ResponseWriter, r *http.Request) {
	pid := r.PathValue("id")
	p, err := s.store.GetProject(r.Context(), pid)
	if err != nil {
		writeErr(w, statusFor(err), err)
		return
	}
	writeJSON(w, http.StatusOK, verifyprofile.Detect(p.RepoRoot))
}
