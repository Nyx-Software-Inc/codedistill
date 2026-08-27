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
	"strings"
)

// meResponse is the GET /api/v1/me payload — who the SPA is acting as, plus the
// flags the profile menu needs (is this an admin? is this a multi-user server?).
type meResponse struct {
	ID          string `json:"id"`
	DisplayName string `json:"display_name"`
	RealName    string `json:"real_name,omitempty"` // immutable IdP name (hover identity)
	Email       string `json:"email,omitempty"`
	IsAdmin     bool   `json:"is_admin"`
	MultiUser   bool   `json:"multi_user"`
}

// me handles GET /api/v1/me: the current user. In single-user mode that's the
// local user (display name from the license holder); once per-user auth lands,
// currentUser resolves it from the session. A read, so it's never gated.
func (s *Server) me(w http.ResponseWriter, r *http.Request) {
	uid := s.currentUser(r)
	resp := meResponse{ID: uid, DisplayName: uid, MultiUser: s.auth != nil}
	if u, err := s.store.GetUser(r.Context(), uid); err == nil {
		if u.DisplayName != "" {
			resp.DisplayName = u.DisplayName
		}
		resp.Email = u.Email
		resp.RealName = u.CanonicalName
	}
	if m, err := s.store.GetWorkspaceMember(r.Context(), localWorkspaceID, uid); err == nil {
		resp.IsAdmin = m.Role == "owner" || m.Role == "admin"
	}
	writeJSON(w, http.StatusOK, resp)
}

// userIdentity (GET /api/v1/users/{id}) resolves any user's identity for the
// hover tooltip behind an attributed name — display name plus the immutable real
// name + email so "Captain Code" reveals "John Smith · john@acme.com".
func (s *Server) userIdentity(w http.ResponseWriter, r *http.Request) {
	u, err := s.store.GetUser(r.Context(), r.PathValue("id"))
	if err != nil {
		writeErr(w, statusFor(err), err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{
		"id": u.ID, "display_name": u.DisplayName, "real_name": u.CanonicalName, "email": u.Email,
	})
}

type updateMeReq struct {
	DisplayName string `json:"display_name"`
}

// updateMe handles PATCH /api/v1/me: the current user edits their own display
// name (profile menu → Edit name).
func (s *Server) updateMe(w http.ResponseWriter, r *http.Request) {
	var req updateMeReq
	if err := readJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	name := strings.TrimSpace(req.DisplayName)
	if name == "" {
		writeMsg(w, http.StatusBadRequest, "display_name is required")
		return
	}
	u, err := s.store.GetUser(r.Context(), s.currentUser(r))
	if err != nil {
		writeErr(w, statusFor(err), err)
		return
	}
	u.DisplayName = name
	if err := s.store.UpdateUser(r.Context(), u); err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"display_name": name})
}
