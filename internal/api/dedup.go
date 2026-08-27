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

	"codedistill/internal/agent"
	"codedistill/internal/events"
)

// dedupScan handles POST /api/v1/dedup/scan — the on-demand duplicate
// pass. Within each pad in scope it clusters near-duplicates into
// "Similar items" groups; for multi-pad scopes it also reports
// cross-pad near-duplicates (which can't be grouped — different
// canvases). Paid: registered behind requireFeature(features.Dedup);
// this handler additionally guards a nil grouper (feature flag on but
// no impl wired, e.g. tests without the dedup package).
//
// Body: {"scope": "scratchpad"|"project"|"global", "scope_id": "..."}.
func (s *Server) dedupScan(w http.ResponseWriter, r *http.Request) {
	if s.grouper == nil {
		writeMsg(w, http.StatusServiceUnavailable, "duplicate intelligence is not available on this server")
		return
	}
	var req agent.DedupScanRequest
	if err := readJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	switch req.Scope {
	case agent.DedupScopeScratchpad, agent.DedupScopeProject, agent.DedupScopeGlobal:
		// ok
	default:
		writeMsg(w, http.StatusBadRequest, "scope must be one of scratchpad | project | global")
		return
	}
	if req.Scope != agent.DedupScopeGlobal && req.ScopeID == "" {
		writeMsg(w, http.StatusBadRequest, "scope_id is required for scratchpad / project scope")
		return
	}
	res, err := s.grouper.Scan(r.Context(), req)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	// Flagging set "possibly related" banners on items — nudge SSE
	// subscribers so open canvases refetch and show them.
	if res.ItemsFlagged > 0 {
		s.bus.Publish(events.ItemsChanged)
	}
	writeJSON(w, http.StatusOK, res)
}
