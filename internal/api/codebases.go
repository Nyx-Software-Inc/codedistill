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

// Codebases API. Each Project owns N codebases (replacing the single
// Project.repo_root). For single-codebase projects, the legacy repo_root
// field on Project is still populated for backwards compatibility with the
// existing Files panel; multi-codebase projects must drive the panel from
// the codebases list.

type createCodebaseReq struct {
	Name     string `json:"name"`
	RepoRoot string `json:"repo_root"`
}

type updateCodebaseReq struct {
	Name     *string `json:"name,omitempty"`
	RepoRoot *string `json:"repo_root,omitempty"`
}

func (s *Server) listCodebases(w http.ResponseWriter, r *http.Request) {
	pid := r.PathValue("pid")
	if _, err := s.store.GetProject(r.Context(), pid); err != nil {
		writeErr(w, statusFor(err), err)
		return
	}
	list, err := s.store.ListCodebases(r.Context(), pid)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	if list == nil {
		list = []*domain.Codebase{}
	}
	writeJSON(w, http.StatusOK, list)
}

func (s *Server) createCodebase(w http.ResponseWriter, r *http.Request) {
	pid := r.PathValue("pid")
	if _, err := s.store.GetProject(r.Context(), pid); err != nil {
		writeErr(w, statusFor(err), err)
		return
	}
	var req createCodebaseReq
	if err := readJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if req.Name == "" || req.RepoRoot == "" {
		writeMsg(w, http.StatusBadRequest, "name and repo_root are required")
		return
	}
	cb := &domain.Codebase{
		ID:        id.New(),
		ProjectID: pid,
		Name:      req.Name,
		RepoRoot:  req.RepoRoot,
		CreatedAt: time.Now().UTC(),
	}
	if err := s.store.CreateCodebase(r.Context(), cb); err != nil {
		writeErr(w, statusFor(err), err)
		return
	}
	writeJSON(w, http.StatusCreated, cb)
}

func (s *Server) getCodebase(w http.ResponseWriter, r *http.Request) {
	cb, err := s.store.GetCodebase(r.Context(), r.PathValue("id"))
	if err != nil {
		writeErr(w, statusFor(err), err)
		return
	}
	writeJSON(w, http.StatusOK, cb)
}

func (s *Server) updateCodebase(w http.ResponseWriter, r *http.Request) {
	cb, err := s.store.GetCodebase(r.Context(), r.PathValue("id"))
	if err != nil {
		writeErr(w, statusFor(err), err)
		return
	}
	var req updateCodebaseReq
	if err := readJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if req.Name != nil {
		if *req.Name == "" {
			writeMsg(w, http.StatusBadRequest, "name cannot be empty")
			return
		}
		cb.Name = *req.Name
	}
	if req.RepoRoot != nil {
		if *req.RepoRoot == "" {
			writeMsg(w, http.StatusBadRequest, "repo_root cannot be empty")
			return
		}
		cb.RepoRoot = *req.RepoRoot
	}
	if err := s.store.UpdateCodebase(r.Context(), cb); err != nil {
		writeErr(w, statusFor(err), err)
		return
	}
	writeJSON(w, http.StatusOK, cb)
}

func (s *Server) deleteCodebase(w http.ResponseWriter, r *http.Request) {
	if err := s.store.DeleteCodebase(r.Context(), r.PathValue("id")); err != nil {
		writeErr(w, statusFor(err), err)
		return
	}
	writeEmpty(w, http.StatusNoContent)
}
