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
	"codedistill/internal/mcpworker"
)

type createKBReq struct {
	Title   string `json:"title"`
	Content string `json:"content"`
	Kind    string `json:"kind,omitempty"` // architecture|convention|decision|reference
}

// validKBKind reports whether k is a project-brain kind (glass-box Phase 5).
func validKBKind(k string) bool {
	switch k {
	case "architecture", "convention", "decision", "reference":
		return true
	}
	return false
}

type updateKBReq struct {
	Title   *string   `json:"title,omitempty"`
	Content *string   `json:"content,omitempty"`
	Kind    *string   `json:"kind,omitempty"`   // architecture|convention|decision|reference
	Status  *string   `json:"status,omitempty"` // active | deprecated
	Tags    *[]string `json:"tags,omitempty"`   // work-record tags (canvas rework C3)
}

func (s *Server) listKB(w http.ResponseWriter, r *http.Request) {
	list, err := s.store.ListKnowledgeEntries(r.Context(), r.PathValue("pid"))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	if !includeDone(r) {
		list = filterOpen(list, func(k *domain.KnowledgeEntry) bool { return domain.IsKnowledgeDone(k.Status) })
	}
	if list == nil {
		list = []*domain.KnowledgeEntry{}
	}
	writeJSON(w, http.StatusOK, list)
}

func (s *Server) listKBByScratchpad(w http.ResponseWriter, r *http.Request) {
	sid := r.PathValue("sid")
	if _, err := s.store.GetScratchpad(r.Context(), sid); err != nil {
		writeErr(w, statusFor(err), err)
		return
	}
	list, err := s.store.ListKnowledgeEntriesByScratchpad(r.Context(), sid)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	if !includeDone(r) {
		list = filterOpen(list, func(k *domain.KnowledgeEntry) bool { return domain.IsKnowledgeDone(k.Status) })
	}
	if list == nil {
		list = []*domain.KnowledgeEntry{}
	}
	writeJSON(w, http.StatusOK, list)
}

func (s *Server) createKB(w http.ResponseWriter, r *http.Request) {
	projectID := r.PathValue("pid")
	if _, err := s.store.GetProject(r.Context(), projectID); err != nil {
		writeErr(w, statusFor(err), err)
		return
	}
	var req createKBReq
	if err := readJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if req.Title == "" || req.Content == "" {
		writeMsg(w, http.StatusBadRequest, "title and content are required")
		return
	}
	kind := req.Kind
	if kind == "" {
		kind = "reference"
	} else if !validKBKind(kind) {
		writeMsg(w, http.StatusBadRequest, "kind must be architecture, convention, decision, or reference")
		return
	}
	k := &domain.KnowledgeEntry{
		ID: id.New(), ProjectID: projectID,
		Title: req.Title, Content: req.Content, Kind: kind,
		CreatedAt: time.Now().UTC(),
	}
	if err := s.store.CreateKnowledgeEntry(r.Context(), k); err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	s.notifyExport(r.Context(), "knowledge_entry", k.ID, mcpworker.OpCreate, k)
	s.bus.Publish(events.KBChanged)
	writeJSON(w, http.StatusCreated, k)
}

func (s *Server) getKB(w http.ResponseWriter, r *http.Request) {
	k, err := s.store.GetKnowledgeEntry(r.Context(), r.PathValue("id"))
	if err != nil {
		writeErr(w, statusFor(err), err)
		return
	}
	writeJSON(w, http.StatusOK, k)
}

func (s *Server) updateKB(w http.ResponseWriter, r *http.Request) {
	k, err := s.store.GetKnowledgeEntry(r.Context(), r.PathValue("id"))
	if err != nil {
		writeErr(w, statusFor(err), err)
		return
	}
	var req updateKBReq
	if err := readJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if req.Title != nil {
		k.Title = *req.Title
	}
	if req.Content != nil {
		k.Content = *req.Content
	}
	if req.Kind != nil {
		if !validKBKind(*req.Kind) {
			writeMsg(w, http.StatusBadRequest, "kind must be architecture, convention, decision, or reference")
			return
		}
		k.Kind = *req.Kind
	}
	if req.Status != nil {
		if !domain.ValidKnowledgeStatus(*req.Status) {
			writeMsg(w, http.StatusBadRequest, "status must be active or deprecated")
			return
		}
		k.Status = *req.Status
	}
	if req.Tags != nil {
		k.Tags = normalizeTags(*req.Tags)
	}
	if err := s.store.UpdateKnowledgeEntry(r.Context(), k); err != nil {
		writeErr(w, statusFor(err), err)
		return
	}
	s.notifyExport(r.Context(), "knowledge_entry", k.ID, mcpworker.OpUpdate, k)
	s.bus.Publish(events.KBChanged)
	writeJSON(w, http.StatusOK, k)
}

// reopenKB flips a deprecated KB entry back to active. KB has no
// claim semantics so there's no claim handler — just reopen.
func (s *Server) reopenKB(w http.ResponseWriter, r *http.Request) {
	k, err := s.store.GetKnowledgeEntry(r.Context(), r.PathValue("id"))
	if err != nil {
		writeErr(w, statusFor(err), err)
		return
	}
	k.Status = domain.KnowledgeStatusActive
	if err := s.store.UpdateKnowledgeEntry(r.Context(), k); err != nil {
		writeErr(w, statusFor(err), err)
		return
	}
	s.notifyExport(r.Context(), "knowledge_entry", k.ID, mcpworker.OpUpdate, k)
	s.bus.Publish(events.KBChanged)
	writeJSON(w, http.StatusOK, k)
}

func (s *Server) deleteKB(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	rid := s.captureRemoteID(r.Context(), "knowledge_entry", id)
	if err := s.store.DeleteKnowledgeEntry(r.Context(), id); err != nil {
		writeErr(w, statusFor(err), err)
		return
	}
	s.notifyExport(r.Context(), "knowledge_entry", id, mcpworker.OpDelete,
		map[string]any{"id": id, "remote_id": rid})
	s.bus.Publish(events.KBChanged)
	writeEmpty(w, http.StatusNoContent)
}
