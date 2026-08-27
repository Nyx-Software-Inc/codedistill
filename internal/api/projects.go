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
	"codedistill/internal/events"
	"codedistill/internal/id"
)

type createProjectReq struct {
	Name     string `json:"name"`
	RepoRoot string `json:"repo_root,omitempty"`
}

type updateProjectReq struct {
	Name     *string `json:"name,omitempty"`
	RepoRoot *string `json:"repo_root,omitempty"`
}

func (s *Server) listProjects(w http.ResponseWriter, r *http.Request) {
	projects, err := s.store.ListProjects(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	if projects == nil {
		projects = []*domain.Project{}
	}
	writeJSON(w, http.StatusOK, projects)
}

func (s *Server) createProject(w http.ResponseWriter, r *http.Request) {
	var req createProjectReq
	if err := readJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if req.Name == "" {
		writeMsg(w, http.StatusBadRequest, "name is required")
		return
	}
	now := time.Now().UTC()
	p := &domain.Project{
		ID:        id.New(),
		Name:      req.Name,
		RepoRoot:  req.RepoRoot,
		CreatedAt: now,
	}
	if err := s.store.CreateProject(r.Context(), p); err != nil {
		writeErr(w, statusFor(err), err)
		return
	}
	// Seed a default "main" scratchpad so the project is immediately usable —
	// mirrors ensureDefaults in cmd/codedistill/main.go for the legacy
	// 'default' project. Without this, the frontend's first refresh after
	// switching to the new project calls listItems('') and 404s, leaving the
	// previous project's items, totals, and canvas onscreen.
	sp := &domain.Scratchpad{
		ID:                 id.New(),
		ProjectID:          p.ID,
		Name:               "main",
		ClassificationMode: "full",
		CreatedAt:          now,
	}
	if err := s.store.CreateScratchpad(r.Context(), sp); err != nil {
		// Project succeeded; if the seed scratchpad fails, log and let the
		// user create one manually rather than rolling back the project.
		s.log.Warn("createProject: default scratchpad seed failed", "project_id", p.ID, "err", err)
	}
	s.bus.Publish(events.ProjectsChanged)
	writeJSON(w, http.StatusCreated, p)
}

func (s *Server) getProject(w http.ResponseWriter, r *http.Request) {
	p, err := s.store.GetProject(r.Context(), r.PathValue("id"))
	if err != nil {
		writeErr(w, statusFor(err), err)
		return
	}
	writeJSON(w, http.StatusOK, p)
}

func (s *Server) updateProject(w http.ResponseWriter, r *http.Request) {
	p, err := s.store.GetProject(r.Context(), r.PathValue("id"))
	if err != nil {
		writeErr(w, statusFor(err), err)
		return
	}
	var req updateProjectReq
	if err := readJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if req.Name != nil {
		if *req.Name == "" {
			writeMsg(w, http.StatusBadRequest, "name cannot be empty")
			return
		}
		p.Name = *req.Name
	}
	if req.RepoRoot != nil {
		p.RepoRoot = *req.RepoRoot
	}
	if err := s.store.UpdateProject(r.Context(), p); err != nil {
		writeErr(w, statusFor(err), err)
		return
	}
	s.bus.Publish(events.ProjectsChanged)
	writeJSON(w, http.StatusOK, p)
}

func (s *Server) deleteProject(w http.ResponseWriter, r *http.Request) {
	if err := s.store.DeleteProject(r.Context(), r.PathValue("id")); err != nil {
		writeErr(w, statusFor(err), err)
		return
	}
	s.bus.Publish(events.ProjectsChanged)
	writeEmpty(w, http.StatusNoContent)
}

