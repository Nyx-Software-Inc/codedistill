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
	"time"

	"codedistill/internal/domain"
	"codedistill/internal/id"
)

// Skills (backlog item #16): project-scoped instruction sets the agent retrieves
// over MCP. A paid feature — the write endpoints are gated on features.MCPServer
// (Skills are delivered over MCP, so the MCP Pro feature gates them).

type skillReq struct {
	Name     string `json:"name"`
	Content  string `json:"content"`
	Enabled  *bool  `json:"enabled"`
	Position int    `json:"position"`
}

func (s *Server) listSkills(w http.ResponseWriter, r *http.Request) {
	pid := r.PathValue("id")
	if _, err := s.store.GetProject(r.Context(), pid); err != nil {
		writeErr(w, statusFor(err), err)
		return
	}
	skills, err := s.store.ListSkills(r.Context(), pid, false)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, skills)
}

func (s *Server) createSkill(w http.ResponseWriter, r *http.Request) {
	pid := r.PathValue("id")
	if _, err := s.store.GetProject(r.Context(), pid); err != nil {
		writeErr(w, statusFor(err), err)
		return
	}
	var req skillReq
	if err := readJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if strings.TrimSpace(req.Name) == "" {
		writeMsg(w, http.StatusBadRequest, "name is required")
		return
	}
	now := time.Now().UTC()
	sk := &domain.Skill{
		ID: id.New(), ProjectID: pid, Name: strings.TrimSpace(req.Name),
		Content: req.Content, Enabled: req.Enabled == nil || *req.Enabled,
		Position: req.Position, CreatedAt: now, UpdatedAt: now,
	}
	if err := s.store.CreateSkill(r.Context(), sk); err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusCreated, sk)
}

func (s *Server) updateSkill(w http.ResponseWriter, r *http.Request) {
	sk, err := s.store.GetSkill(r.Context(), r.PathValue("id"))
	if err != nil {
		writeErr(w, statusFor(err), err)
		return
	}
	var req skillReq
	if err := readJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if strings.TrimSpace(req.Name) != "" {
		sk.Name = strings.TrimSpace(req.Name)
	}
	sk.Content = req.Content
	if req.Enabled != nil {
		sk.Enabled = *req.Enabled
	}
	sk.Position = req.Position
	sk.UpdatedAt = time.Now().UTC()
	if err := s.store.UpdateSkill(r.Context(), sk); err != nil {
		writeErr(w, statusFor(err), err)
		return
	}
	writeJSON(w, http.StatusOK, sk)
}

func (s *Server) deleteSkill(w http.ResponseWriter, r *http.Request) {
	if err := s.store.DeleteSkill(r.Context(), r.PathValue("id")); err != nil {
		writeErr(w, statusFor(err), err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
