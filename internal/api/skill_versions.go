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
	"strconv"
)

// Skill versioning + evidence-gated throughline tie (Pro). Read endpoints for
// the version history, the reverse-lookup ("what did skill vN touch?"), and the
// retrieval audit log; plus the deliberate compliance purge of one version.
// See docs/design/skill-provenance-standard.md.

// GET /api/v1/skills/{id}/versions — immutable version history, newest first.
func (s *Server) listSkillVersions(w http.ResponseWriter, r *http.Request) {
	sid := r.PathValue("id")
	if _, err := s.store.GetSkill(r.Context(), sid); err != nil {
		writeErr(w, statusFor(err), err)
		return
	}
	vs, err := s.store.ListSkillVersions(r.Context(), sid)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, vs)
}

// GET /api/v1/skills/{id}/versions/{version} — one snapshot's full content.
func (s *Server) getSkillVersion(w http.ResponseWriter, r *http.Request) {
	sid := r.PathValue("id")
	ver, err := strconv.Atoi(r.PathValue("version"))
	if err != nil || ver <= 0 {
		writeMsg(w, http.StatusBadRequest, "version must be a positive integer")
		return
	}
	v, err := s.store.GetSkillVersion(r.Context(), sid, ver)
	if err != nil {
		writeErr(w, statusFor(err), err)
		return
	}
	writeJSON(w, http.StatusOK, v)
}

// GET /api/v1/skills/{id}/applications?version=&evidence= — the reverse-lookup:
// every change bound to this skill. Optional version (0/absent = any) and
// evidence ("attested"|"retrieved"; absent = any) narrow the result.
func (s *Server) listSkillApplications(w http.ResponseWriter, r *http.Request) {
	sid := r.PathValue("id")
	if _, err := s.store.GetSkill(r.Context(), sid); err != nil {
		writeErr(w, statusFor(err), err)
		return
	}
	ver, _ := strconv.Atoi(r.URL.Query().Get("version")) // 0 on absent/invalid = any
	evidence := r.URL.Query().Get("evidence")
	apps, err := s.store.ListSkillApplications(r.Context(), sid, ver, evidence)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, apps)
}

// GET /api/v1/skills/{id}/retrievals?version= — the fetch audit log.
func (s *Server) listSkillRetrievals(w http.ResponseWriter, r *http.Request) {
	sid := r.PathValue("id")
	if _, err := s.store.GetSkill(r.Context(), sid); err != nil {
		writeErr(w, statusFor(err), err)
		return
	}
	ver, _ := strconv.Atoi(r.URL.Query().Get("version"))
	rs, err := s.store.ListSkillRetrievals(r.Context(), sid, ver)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, rs)
}

// DELETE /api/v1/skills/{id}/versions/{version} — deliberate compliance purge of
// one immutable snapshot (e.g. a secret pasted into a version). Gated like the
// other skill writes; does not renumber remaining versions.
func (s *Server) purgeSkillVersion(w http.ResponseWriter, r *http.Request) {
	sid := r.PathValue("id")
	ver, err := strconv.Atoi(r.PathValue("version"))
	if err != nil || ver <= 0 {
		writeMsg(w, http.StatusBadRequest, "version must be a positive integer")
		return
	}
	if err := s.store.PurgeSkillVersion(r.Context(), sid, ver); err != nil {
		writeErr(w, statusFor(err), err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
