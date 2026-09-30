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
	"time"

	"codedistill/internal/domain"
	"codedistill/internal/id"
)

// What has to happen before what.
//
// The whole graph is returned at once: answering "what can I start" needs all
// of it, and a project's edge count is bounded by its item count.

func (s *Server) listDependencies(w http.ResponseWriter, r *http.Request) {
	projectID := r.URL.Query().Get("project_id")
	if projectID == "" {
		writeMsg(w, http.StatusBadRequest, "project_id is required")
		return
	}
	deps, err := s.store.ListDependencies(r.Context(), projectID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	if deps == nil {
		deps = []*domain.ItemDependency{}
	}

	// Split by standing rather than returned as one list: a proposed edge is a
	// question and an accepted one is a fact, and a screen that renders them
	// identically is asking the reader to remember which is which.
	proposed, accepted := 0, 0
	for _, d := range deps {
		switch d.Status {
		case domain.EdgeAdvisory:
			proposed++
		case domain.EdgeAccepted:
			accepted++
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"dependencies": deps,
		"proposed":     proposed,
		"accepted":     accepted,
	})
}

// createDependency records an edge a person drew. Accepted immediately: a
// human drawing an arrow IS the ratification.
func (s *Server) createDependency(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ProjectID string `json:"project_id"`
		FromKind  string `json:"from_kind"`
		FromID    string `json:"from_id"`
		ToKind    string `json:"to_kind"`
		ToID      string `json:"to_id"`
		Kind      string `json:"kind"`
		Rationale string `json:"rationale"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if req.Kind == "" {
		req.Kind = domain.DependencyBlocks
	}
	d := &domain.ItemDependency{
		ID: id.New(), ProjectID: req.ProjectID,
		FromKind: req.FromKind, FromID: req.FromID,
		ToKind: req.ToKind, ToID: req.ToID,
		Kind: req.Kind, Rationale: req.Rationale,
		Origin: domain.DependencyFromHuman, Status: domain.EdgeAccepted,
		CreatedAt: time.Now().UTC(), CreatedBy: s.currentUser(r),
	}
	if err := s.store.CreateDependency(r.Context(), d); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusCreated, d)
}

// decideDependency ratifies or rejects a proposed edge.
//
// A rejected edge is kept, not deleted, so a later run does not propose it
// again — re-asking a question already answered is how a review screen teaches
// people to stop reading it.
func (s *Server) decideDependency(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Status string `json:"status"` // accepted | rejected
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if err := s.store.SetDependencyStatus(r.Context(), r.PathValue("id"), req.Status); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// deleteDependency removes an edge outright. For a human edge drawn in error —
// a rejected PROPOSAL should be decided, not deleted, so the rejection is
// remembered.
func (s *Server) deleteDependency(w http.ResponseWriter, r *http.Request) {
	if err := s.store.DeleteDependency(r.Context(), r.PathValue("id")); err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
