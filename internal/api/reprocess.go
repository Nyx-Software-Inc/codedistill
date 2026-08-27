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

import "net/http"

// reprocessItem re-enqueues a stuck scratchpad item for classification — the
// per-item "Reprocess" action. Covers captures stranded at unprocessed/failed
// (or orphaned mid-`processing`) because the classifier missed them or Ollama
// was down. The reconciliation sweep also heals these automatically, but this
// gives the user immediate control (and resets the failed-retry cap).
func (s *Server) reprocessItem(w http.ResponseWriter, r *http.Request) {
	item, err := s.store.GetScratchpadItem(r.Context(), r.PathValue("id"))
	if err != nil {
		writeErr(w, statusFor(err), err)
		return
	}
	if s.agent == nil {
		writeMsg(w, http.StatusServiceUnavailable, "classification agent is not running")
		return
	}
	s.agent.ReprocessNow(item.ID)
	writeJSON(w, http.StatusAccepted, map[string]string{"id": item.ID, "status": "reprocessing"})
}
