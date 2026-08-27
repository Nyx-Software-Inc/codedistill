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
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"codedistill/internal/domain"
)

// Settings API — generic per-user and per-project key/value endpoints.
//
// Wire format on the way in: { "value": <any JSON> }. The server accepts
// arbitrary JSON shapes (primitives, arrays, objects) and stores them as
// JSON-encoded text. On the way out, the same {"value": <any>} shape is
// preserved so clients can round-trip without re-parsing.
//
// Cascade lookup (scratchpad → project → user → default) is encoded at the
// call site, not here. These endpoints are flat per-scope reads/writes.

type settingPutReq struct {
	Value json.RawMessage `json:"value"`
}

// settingResponse is the wire shape for GET / PUT replies. It mirrors the
// PUT request shape so clients can pipe their request body through unchanged.
type settingResponse struct {
	Key       string          `json:"key"`
	Value     json.RawMessage `json:"value"`
	UpdatedAt time.Time       `json:"updated_at"`
}

func validSettingKey(k string) bool {
	// Free-form but non-empty and not a path-traversal trap. Keys typically
	// look like "ui.drawer.open" or "agent.model" but we don't enforce a
	// schema — settings grow organically.
	k = strings.TrimSpace(k)
	if k == "" || strings.ContainsAny(k, "/\\") {
		return false
	}
	return true
}

// --- user settings ---

func (s *Server) listUserSettings(w http.ResponseWriter, r *http.Request) {
	uid := r.PathValue("uid")
	list, err := s.store.ListUserSettings(r.Context(), uid)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	out := make(map[string]json.RawMessage, len(list))
	for _, st := range list {
		out[st.Key] = st.Value
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) getUserSetting(w http.ResponseWriter, r *http.Request) {
	uid := r.PathValue("uid")
	key := r.PathValue("key")
	if !validSettingKey(key) {
		writeMsg(w, http.StatusBadRequest, "invalid key")
		return
	}
	st, err := s.store.GetUserSetting(r.Context(), uid, key)
	if err != nil {
		writeErr(w, statusFor(err), err)
		return
	}
	writeJSON(w, http.StatusOK, settingResponse{Key: st.Key, Value: st.Value, UpdatedAt: st.UpdatedAt})
}

func (s *Server) putUserSetting(w http.ResponseWriter, r *http.Request) {
	uid := r.PathValue("uid")
	key := r.PathValue("key")
	if !validSettingKey(key) {
		writeMsg(w, http.StatusBadRequest, "invalid key")
		return
	}
	var req settingPutReq
	if err := readJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if len(req.Value) == 0 {
		writeMsg(w, http.StatusBadRequest, "value is required")
		return
	}
	now := time.Now().UTC()
	st := &domain.UserSetting{
		UserID: uid, Key: key, Value: req.Value, UpdatedAt: now,
	}
	if err := s.store.SetUserSetting(r.Context(), st); err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, settingResponse{Key: st.Key, Value: st.Value, UpdatedAt: st.UpdatedAt})
}

func (s *Server) deleteUserSetting(w http.ResponseWriter, r *http.Request) {
	uid := r.PathValue("uid")
	key := r.PathValue("key")
	if !validSettingKey(key) {
		writeMsg(w, http.StatusBadRequest, "invalid key")
		return
	}
	if err := s.store.DeleteUserSetting(r.Context(), uid, key); err != nil {
		writeErr(w, statusFor(err), err)
		return
	}
	writeEmpty(w, http.StatusNoContent)
}

// --- project settings ---

func (s *Server) listProjectSettings(w http.ResponseWriter, r *http.Request) {
	pid := r.PathValue("pid")
	if _, err := s.store.GetProject(r.Context(), pid); err != nil {
		writeErr(w, statusFor(err), err)
		return
	}
	list, err := s.store.ListProjectSettings(r.Context(), pid)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	out := make(map[string]json.RawMessage, len(list))
	for _, st := range list {
		out[st.Key] = st.Value
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) getProjectSetting(w http.ResponseWriter, r *http.Request) {
	pid := r.PathValue("pid")
	key := r.PathValue("key")
	if !validSettingKey(key) {
		writeMsg(w, http.StatusBadRequest, "invalid key")
		return
	}
	st, err := s.store.GetProjectSetting(r.Context(), pid, key)
	if err != nil {
		writeErr(w, statusFor(err), err)
		return
	}
	writeJSON(w, http.StatusOK, settingResponse{Key: st.Key, Value: st.Value, UpdatedAt: st.UpdatedAt})
}

func (s *Server) putProjectSetting(w http.ResponseWriter, r *http.Request) {
	pid := r.PathValue("pid")
	key := r.PathValue("key")
	if !validSettingKey(key) {
		writeMsg(w, http.StatusBadRequest, "invalid key")
		return
	}
	if _, err := s.store.GetProject(r.Context(), pid); err != nil {
		writeErr(w, statusFor(err), err)
		return
	}
	var req settingPutReq
	if err := readJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if len(req.Value) == 0 {
		writeMsg(w, http.StatusBadRequest, "value is required")
		return
	}
	now := time.Now().UTC()
	st := &domain.ProjectSetting{
		ProjectID: pid, Key: key, Value: req.Value, UpdatedAt: now,
	}
	if err := s.store.SetProjectSetting(r.Context(), st); err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, settingResponse{Key: st.Key, Value: st.Value, UpdatedAt: st.UpdatedAt})
}

func (s *Server) deleteProjectSetting(w http.ResponseWriter, r *http.Request) {
	pid := r.PathValue("pid")
	key := r.PathValue("key")
	if !validSettingKey(key) {
		writeMsg(w, http.StatusBadRequest, "invalid key")
		return
	}
	if err := s.store.DeleteProjectSetting(r.Context(), pid, key); err != nil {
		writeErr(w, statusFor(err), err)
		return
	}
	writeEmpty(w, http.StatusNoContent)
}
