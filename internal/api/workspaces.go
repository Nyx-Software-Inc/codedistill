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
	"time"

	"codedistill/internal/domain"
	"codedistill/internal/id"
)

// Workspaces API. Phase 2 introduces the type as the multi-tenant root.
// In single-user mode there is one implicit workspace ("local") seeded by
// migration 0006; the API is exposed for forward-compatibility with the
// hosted/enterprise tier even though the local UI doesn't surface it yet.

type createWorkspaceReq struct {
	Name      string `json:"name"`
	Plan      string `json:"plan,omitempty"`       // default "free"
	SeatLimit int    `json:"seat_limit,omitempty"` // 0 = unlimited
}

type updateWorkspaceReq struct {
	Name      *string `json:"name,omitempty"`
	Plan      *string `json:"plan,omitempty"`
	SeatLimit *int    `json:"seat_limit,omitempty"`
}

func validPlan(p string) bool {
	return p == "free" || p == "team" || p == "enterprise"
}

func (s *Server) listWorkspaces(w http.ResponseWriter, r *http.Request) {
	list, err := s.store.ListWorkspaces(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	if list == nil {
		list = []*domain.Workspace{}
	}
	writeJSON(w, http.StatusOK, list)
}

func (s *Server) createWorkspace(w http.ResponseWriter, r *http.Request) {
	var req createWorkspaceReq
	if err := readJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if req.Name == "" {
		writeMsg(w, http.StatusBadRequest, "name is required")
		return
	}
	plan := req.Plan
	if plan == "" {
		plan = "free"
	}
	if !validPlan(plan) {
		writeMsg(w, http.StatusBadRequest, "plan must be free, team, or enterprise")
		return
	}
	ws := &domain.Workspace{
		ID:        id.New(),
		Name:      req.Name,
		Plan:      plan,
		SeatLimit: req.SeatLimit,
		CreatedAt: time.Now().UTC(),
	}
	if err := s.store.CreateWorkspace(r.Context(), ws); err != nil {
		writeErr(w, statusFor(err), err)
		return
	}
	writeJSON(w, http.StatusCreated, ws)
}

func (s *Server) getWorkspace(w http.ResponseWriter, r *http.Request) {
	ws, err := s.store.GetWorkspace(r.Context(), r.PathValue("id"))
	if err != nil {
		writeErr(w, statusFor(err), err)
		return
	}
	writeJSON(w, http.StatusOK, ws)
}

func (s *Server) updateWorkspace(w http.ResponseWriter, r *http.Request) {
	ws, err := s.store.GetWorkspace(r.Context(), r.PathValue("id"))
	if err != nil {
		writeErr(w, statusFor(err), err)
		return
	}
	var req updateWorkspaceReq
	if err := readJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if req.Name != nil {
		if *req.Name == "" {
			writeMsg(w, http.StatusBadRequest, "name cannot be empty")
			return
		}
		ws.Name = *req.Name
	}
	if req.Plan != nil {
		if !validPlan(*req.Plan) {
			writeMsg(w, http.StatusBadRequest, "plan must be free, team, or enterprise")
			return
		}
		ws.Plan = *req.Plan
	}
	if req.SeatLimit != nil {
		ws.SeatLimit = *req.SeatLimit
	}
	if err := s.store.UpdateWorkspace(r.Context(), ws); err != nil {
		writeErr(w, statusFor(err), err)
		return
	}
	writeJSON(w, http.StatusOK, ws)
}

func (s *Server) deleteWorkspace(w http.ResponseWriter, r *http.Request) {
	if err := s.store.DeleteWorkspace(r.Context(), r.PathValue("id")); err != nil {
		writeErr(w, statusFor(err), err)
		return
	}
	writeEmpty(w, http.StatusNoContent)
}
