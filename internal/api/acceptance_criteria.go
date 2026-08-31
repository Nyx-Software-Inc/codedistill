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
	"strings"
	"time"

	"codedistill/internal/domain"
	"codedistill/internal/id"
)

// Acceptance criteria (glass-box Phase 2): an item's measurable "definition of
// done." Reads are free; writes go through the normal write-auth (manual,
// first-party). Soft in this slice — criteria are a ratified contract, not a
// gate; gating arrives with the governance phase.

func validCriterionState(s string) bool {
	switch s {
	case "proposed", "accepted", "satisfied", "failed", "rejected":
		return true
	}
	return false
}

func validVerificationKind(k string) bool {
	switch k {
	case "", "unspecified", "test", "check", "human":
		return true
	}
	return false
}

type acceptanceCriterionReq struct {
	Text             string `json:"text"`
	VerificationKind string `json:"verification_kind,omitempty"`
	State            string `json:"state,omitempty"`
	Position         *int   `json:"position,omitempty"`
}

// listAcceptanceCriteriaForOwner factors over the per-owner list endpoints.
func (s *Server) listAcceptanceCriteriaForOwner(ownerType string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ownerID := r.PathValue("id")
		if err := s.ensureOwnerExists(r, ownerType, ownerID); err != nil {
			writeErr(w, statusFor(err), err)
			return
		}
		list, err := s.store.ListAcceptanceCriteria(r.Context(), ownerType, ownerID)
		if err != nil {
			writeErr(w, http.StatusInternalServerError, err)
			return
		}
		if list == nil {
			list = []*domain.AcceptanceCriterion{}
		}
		writeJSON(w, http.StatusOK, list)
	}
}

// createAcceptanceCriterionForOwner appends a criterion. A hand-authored
// criterion is its own ratification, so it defaults to state=accepted /
// provenance=user-authored (the AI-drafting path in slice 2 will create
// state=proposed / provenance=ai-proposed instead).
func (s *Server) createAcceptanceCriterionForOwner(ownerType string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ownerID := r.PathValue("id")
		if err := s.ensureOwnerExists(r, ownerType, ownerID); err != nil {
			writeErr(w, statusFor(err), err)
			return
		}
		var req acceptanceCriterionReq
		if err := readJSON(r, &req); err != nil {
			writeErr(w, http.StatusBadRequest, err)
			return
		}
		if strings.TrimSpace(req.Text) == "" {
			writeMsg(w, http.StatusBadRequest, "text is required")
			return
		}
		state := req.State
		if state == "" {
			state = "accepted"
		}
		if !validCriterionState(state) {
			writeMsg(w, http.StatusBadRequest, fmt.Sprintf("invalid state %q", state))
			return
		}
		kind := req.VerificationKind
		if kind == "" {
			kind = "unspecified"
		}
		if !validVerificationKind(kind) {
			writeMsg(w, http.StatusBadRequest, fmt.Sprintf("invalid verification_kind %q", kind))
			return
		}
		pos, err := s.store.NextAcceptanceCriterionPosition(r.Context(), ownerType, ownerID)
		if err != nil {
			writeErr(w, http.StatusInternalServerError, err)
			return
		}
		now := time.Now().UTC()
		c := &domain.AcceptanceCriterion{
			ID:               id.New(),
			OwnerType:        ownerType,
			OwnerID:          ownerID,
			Position:         pos,
			Text:             strings.TrimSpace(req.Text),
			VerificationKind: kind,
			State:            state,
			Provenance:       "user-authored",
			CreatedAt:        now,
			UpdatedAt:        now,
		}
		if err := s.store.CreateAcceptanceCriterion(r.Context(), c); err != nil {
			writeErr(w, statusFor(err), err)
			return
		}
		writeJSON(w, http.StatusCreated, c)
	}
}

// updateAcceptanceCriterion patches the mutable fields of one criterion. Only
// fields present in the body change; owner is immutable.
func (s *Server) updateAcceptanceCriterion(w http.ResponseWriter, r *http.Request) {
	cid := r.PathValue("id")
	c, err := s.store.GetAcceptanceCriterion(r.Context(), cid)
	if err != nil {
		writeErr(w, statusFor(err), err)
		return
	}
	var req acceptanceCriterionReq
	if err := readJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if req.Text != "" {
		c.Text = strings.TrimSpace(req.Text)
	}
	if req.State != "" {
		if !validCriterionState(req.State) {
			writeMsg(w, http.StatusBadRequest, fmt.Sprintf("invalid state %q", req.State))
			return
		}
		c.State = req.State
	}
	if req.VerificationKind != "" {
		if !validVerificationKind(req.VerificationKind) {
			writeMsg(w, http.StatusBadRequest, fmt.Sprintf("invalid verification_kind %q", req.VerificationKind))
			return
		}
		c.VerificationKind = req.VerificationKind
	}
	if req.Position != nil {
		c.Position = *req.Position
	}
	c.UpdatedAt = time.Now().UTC()
	if err := s.store.UpdateAcceptanceCriterion(r.Context(), c); err != nil {
		writeErr(w, statusFor(err), err)
		return
	}
	writeJSON(w, http.StatusOK, c)
}

// draftAcceptanceCriteriaForOwner kicks off AI-proposed criteria for an item
// (the "Draft with AI" action) as a BACKGROUND job and returns immediately (202)
// — the caller doesn't wait, and closing the modal / reloading / closing the tab
// can't cancel it (it runs on context.Background). Free — a local-Ollama assist.
// 503 when no model/agent is wired. The drafted rows land via the normal
// acceptance-criteria GET once the job finishes (it publishes ItemsChanged).
func (s *Server) draftAcceptanceCriteriaForOwner(ownerType string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ownerID := r.PathValue("id")
		if err := s.ensureOwnerExists(r, ownerType, ownerID); err != nil {
			writeErr(w, statusFor(err), err)
			return
		}
		if s.agent == nil {
			writeMsg(w, http.StatusServiceUnavailable, "AI drafting is unavailable")
			return
		}
		s.startCriteriaDraft(ownerType, ownerID) // no-op if one's already running
		writeJSON(w, http.StatusAccepted, map[string]string{"status": "drafting"})
	}
}

// listCriteriaDrafting reports which items are currently drafting criteria, so
// the UI can badge their cards + show the in-progress state in the modal.
func (s *Server) listCriteriaDrafting(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.listCriteriaDrafts())
}

func (s *Server) deleteAcceptanceCriterion(w http.ResponseWriter, r *http.Request) {
	if err := s.store.DeleteAcceptanceCriterion(r.Context(), r.PathValue("id")); err != nil {
		writeErr(w, statusFor(err), err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
