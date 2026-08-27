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
	"errors"
	"fmt"
	"net/http"
	"strings"

	"codedistill/internal/storage"
)

// Lifecycle helpers shared across the four item types: include_done
// filtering for list endpoints, the claim request shape + ErrConflict
// translation, and a tiny generic for filtering "done" rows out of a
// slice.

// claimReq is the POST body for /api/v1/{type}/{id}/claim.
type claimReq struct {
	ClaimedBy string `json:"claimed_by"`
}

// validateClaimReq trims + checks the claimer name, writes an error
// response and returns false on bad input.
func (s *Server) validateClaimReq(w http.ResponseWriter, r *http.Request, req *claimReq) bool {
	if err := readJSON(r, req); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return false
	}
	req.ClaimedBy = strings.TrimSpace(req.ClaimedBy)
	if req.ClaimedBy == "" {
		writeMsg(w, http.StatusBadRequest, "claimed_by is required")
		return false
	}
	return true
}

// writeClaimError translates a Claim* storage error into the right
// HTTP response. ErrConflict → 409 with the existing claimer name
// looked up by the per-type fetcher; everything else falls back to
// statusFor.
func (s *Server) writeClaimError(w http.ResponseWriter, err error, existingClaimer func() string) {
	if errors.Is(err, storage.ErrConflict) {
		claimer := ""
		if existingClaimer != nil {
			claimer = existingClaimer()
		}
		writeJSON(w, http.StatusConflict, map[string]any{
			"error":      fmt.Sprintf("already claimed by %q", claimer),
			"claimed_by": claimer,
		})
		return
	}
	writeErr(w, statusFor(err), err)
}

// includeDone parses the ?include_done=true query param. Default is
// false — list endpoints hide done items unless explicitly opted in.
func includeDone(r *http.Request) bool {
	return r.URL.Query().Get("include_done") == "true"
}

// filterOpen returns only items where isDone(item) is false. Generic
// over the item type so all four list endpoints share one filter.
func filterOpen[T any](items []T, isDone func(T) bool) []T {
	out := make([]T, 0, len(items))
	for _, it := range items {
		if !isDone(it) {
			out = append(out, it)
		}
	}
	return out
}
