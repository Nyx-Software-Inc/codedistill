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

// License status endpoint + the requireFeature gate used on paid
// routes. Gating philosophy (see internal/features): only creation
// and compute are gated — reads/exports of existing data never are.
package api

import (
	"net/http"
	"time"

	"codedistill/internal/features"
	"codedistill/internal/licensing"
)

// LicenseInfo is the GET /api/v1/license response consumed by the SPA
// (grace banner + hiding gated affordances).
type LicenseInfo struct {
	State      string   `json:"state"` // none|invalid|valid|grace|expired
	Edition    string   `json:"edition,omitempty"`
	Customer   string   `json:"customer,omitempty"`
	Seats      int      `json:"seats,omitempty"`
	Features   []string `json:"features"` // currently-ON paid features
	ExpiresAt  string   `json:"expires_at,omitempty"`
	GraceUntil string   `json:"grace_until,omitempty"`
	Reason     string   `json:"reason,omitempty"`
	OSSBuild   bool     `json:"oss_build"`
}

func (s *Server) licenseStatus(w http.ResponseWriter, _ *http.Request) {
	st := features.LicenseStatus()
	info := LicenseInfo{
		State:    string(st.State),
		Features: features.EnabledNames(),
		Reason:   st.Reason,
		OSSBuild: features.OSSBuild,
	}
	if st.License != nil {
		info.Edition = st.License.Edition
		info.Customer = st.License.Customer
		info.Seats = st.License.Seats
		if !st.License.ExpiresAt.IsZero() {
			info.ExpiresAt = st.License.ExpiresAt.UTC().Format(time.RFC3339)
		}
	}
	if !st.GraceUntil.IsZero() {
		info.GraceUntil = st.GraceUntil.UTC().Format(time.RFC3339)
	}
	writeJSON(w, http.StatusOK, info)
}

// requireFeature wraps a handler so it answers 402 with a stable
// machine-readable code when the paid feature is off. The SPA treats
// code=feature_locked as "hide/disable this affordance".
func (s *Server) requireFeature(f features.Feature, h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !features.Enabled(f) {
			writeJSON(w, http.StatusPaymentRequired, map[string]string{
				"error":   featureLockedMessage(f),
				"code":    "feature_locked",
				"feature": string(f),
			})
			return
		}
		h(w, r)
	}
}

func featureLockedMessage(f features.Feature) string {
	base := "this capability requires a CodeDistill license"
	st := features.LicenseStatus()
	switch st.State {
	case licensing.StateGrace, licensing.StateExpired:
		return base + " — your license has expired, renew to restore it"
	case licensing.StateInvalid:
		return base + " — the installed license is invalid (" + st.Reason + ")"
	default:
		return base + " (see `codedistill license status`)"
	}
}
