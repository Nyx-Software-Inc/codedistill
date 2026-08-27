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
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"codedistill/internal/domain"
	"codedistill/internal/id"
)

// Pass 2 of skill provenance: remediation. When a skill version turns out to be
// flawed, route the changes made under it back for another look — flag them into
// the review queue and/or re-run verification at their commit. Only the changes
// with recorded applications are touched (we act on evidence, not a guess).

type remediateReq struct {
	Action string `json:"action"` // "review" | "reverify" | "both"
}

type remediateResp struct {
	Version           int    `json:"version"`
	Action            string `json:"action"`
	Items             int    `json:"items"`              // distinct changes under this version
	Flagged           int    `json:"flagged"`            // items newly flagged for review
	Reverified        int    `json:"reverified"`         // (item, commit) verification runs kicked off
	ReverifyAvailable bool   `json:"reverify_available"` // false when no verifier is wired
}

// POST /api/v1/skills/{id}/versions/{version}/remediate
func (s *Server) remediateSkillVersion(w http.ResponseWriter, r *http.Request) {
	sid := r.PathValue("id")
	sk, err := s.store.GetSkill(r.Context(), sid)
	if err != nil {
		writeErr(w, statusFor(err), err)
		return
	}
	ver, err := strconv.Atoi(r.PathValue("version"))
	if err != nil || ver <= 0 {
		writeMsg(w, http.StatusBadRequest, "version must be a positive integer")
		return
	}
	var req remediateReq
	if err := readJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	action := strings.TrimSpace(req.Action)
	if action != "review" && action != "reverify" && action != "both" {
		writeMsg(w, http.StatusBadRequest, `action must be "review", "reverify", or "both"`)
		return
	}

	// The wide net: every application under this version, regardless of evidence
	// class — for a remediation sweep we'd rather over-include a suspect change
	// than miss one.
	apps, err := s.store.ListSkillApplications(r.Context(), sid, ver, "")
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}

	doReview := action == "review" || action == "both"
	doReverify := action == "reverify" || action == "both"
	now := time.Now().UTC()
	reason := fmt.Sprintf("Changed under skill %q v%d — flagged for review on %s", sk.Name, ver, now.Format("2006-01-02"))

	items := map[string]bool{}
	flagged, reverified := 0, 0
	reverifyKeys := map[string]bool{}
	for _, a := range apps {
		ikey := a.OwnerType + "\x00" + a.OwnerID
		if doReview && !items[ikey] {
			if err := s.store.CreateReviewFlag(r.Context(), &domain.ReviewFlag{
				ID: id.New(), OwnerType: a.OwnerType, OwnerID: a.OwnerID,
				Reason: reason, Source: "skill_remediation", CreatedAt: now,
			}); err == nil {
				flagged++
			}
		}
		items[ikey] = true
		if doReverify && s.verifier != nil && a.CommitSHA != "" {
			rkey := ikey + "\x00" + a.CommitSHA
			if !reverifyKeys[rkey] {
				reverifyKeys[rkey] = true
				if _, err := s.verifier.Trigger(r.Context(), a.OwnerType, a.OwnerID, a.CommitSHA); err == nil {
					reverified++
				}
			}
		}
	}

	writeJSON(w, http.StatusOK, remediateResp{
		Version: ver, Action: action, Items: len(items),
		Flagged: flagged, Reverified: reverified, ReverifyAvailable: s.verifier != nil,
	})
}
