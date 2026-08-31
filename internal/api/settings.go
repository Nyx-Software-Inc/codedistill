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
	"errors"
	"net/http"
	"strings"
	"time"

	"codedistill/internal/domain"
	"codedistill/internal/storage"
	"codedistill/internal/throttle"
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
	// Default marks a value the server supplied from settingDefaults because no
	// row exists. Clients that need to distinguish "the user chose this" from
	// "this is what we ship" can; clients that just want the effective value can
	// ignore it.
	Default bool `json:"default,omitempty"`
}

// settingDefaults holds the keys whose shipped default the SERVER knows. For
// these, an unset key is not "no such thing" — it is a known value, so the API
// answers with it instead of 404ing.
//
// Sourced from the package that owns the behaviour so there is exactly ONE
// definition. Before this, throttle.DefaultPause lived in Go while two Svelte
// components each carried their own literal copy; changing the shipped default
// would have left the engine and the UI silently disagreeing about what the
// product does. It also meant every read of an untouched setting was an error
// by construction, which is what buried the console in 404s (CE-review item 43).
//
// Deliberately NOT a catch-all: validSettingKey enforces no schema because
// settings "grow organically", so inventing a default for an arbitrary key
// would turn a real not-found into a silent lie. Unregistered keys still 404.
var settingDefaults = map[string]any{
	throttle.KeyBatteryPause:      throttle.DefaultPause,
	throttle.KeyBatteryMultiplier: throttle.DefaultMultiplier,
	// List-view sort order. 'due' is what the view has always done, so an
	// upgrade changes nothing until the user picks otherwise.
	"ui.list.sort": "due",
}

// defaultSettingValue returns the JSON encoding of a registered default.
// A marshal failure is reported as "no default" rather than a 500: the honest
// fallback for a broken default is the pre-existing 404, not a server error on
// a read path.
func defaultSettingValue(key string) (json.RawMessage, bool) {
	v, ok := settingDefaults[key]
	if !ok {
		return nil, false
	}
	raw, err := json.Marshal(v)
	if err != nil {
		return nil, false
	}
	return raw, true
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
		if errors.Is(err, storage.ErrNotFound) {
			if raw, ok := defaultSettingValue(key); ok {
				writeJSON(w, http.StatusOK, settingResponse{Key: key, Value: raw, Default: true})
				return
			}
			// 204, not 404: asking a key-value store for something never written
			// is not a failure. Most settings hold values only the CLIENT cares
			// about, and it supplies its own default — the server has no business
			// having an opinion about ui.item_font_size. Registering those
			// defaults here would copy eighteen frontend values into Go and
			// recreate the drift item 43 was about. See settingDefaults.
			w.WriteHeader(http.StatusNoContent)
			return
		}
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
		if errors.Is(err, storage.ErrNotFound) {
			// Same contract as the user endpoint above. Project settings have no
			// registered defaults today, so an unset key is always 204.
			w.WriteHeader(http.StatusNoContent)
			return
		}
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
