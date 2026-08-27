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
	"fmt"
	"net/http"

	"codedistill/internal/domain"
	"codedistill/internal/events"
)

// listInbox returns all Scratchpad_Items in pending-review state across the
// accessible Projects. For Phase 1 (single-user local mode) this walks
// Projects → Scratchpads → Items and filters by state. Postgres-backed
// deployments will want a dedicated query in Phase 4.
func (s *Server) listInbox(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	projects, err := s.store.ListProjects(ctx)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	inbox := []*domain.ScratchpadItem{}
	for _, p := range projects {
		scratchpads, err := s.store.ListScratchpads(ctx, p.ID)
		if err != nil {
			writeErr(w, http.StatusInternalServerError, err)
			return
		}
		for _, sp := range scratchpads {
			items, err := s.store.ListScratchpadItems(ctx, sp.ID)
			if err != nil {
				writeErr(w, http.StatusInternalServerError, err)
				return
			}
			for _, it := range items {
				if it.Hidden {
					continue
				}
				if it.ClassificationState == "pending-review" {
					inbox = append(inbox, it)
				}
			}
		}
	}
	writeJSON(w, http.StatusOK, inbox)
}

func (s *Server) acceptInbox(w http.ResponseWriter, r *http.Request) {
	if s.agent == nil {
		writeMsg(w, http.StatusServiceUnavailable, "agent not running")
		return
	}
	itemID := r.PathValue("id")
	if err := s.agent.AcceptPending(r.Context(), itemID, ""); err != nil {
		// Idempotent accept: a double-click (or a stale Needs-Review list)
		// re-accepting an already-classified item is a no-op success, not a
		// 500 — the caller's goal state is already true.
		if it, gerr := s.store.GetScratchpadItem(r.Context(), itemID); gerr == nil &&
			it.ClassificationState == "classified" {
			writeJSON(w, http.StatusOK, it)
			return
		}
		writeErr(w, statusFor(err), err)
		return
	}
	item, err := s.store.GetScratchpadItem(r.Context(), itemID)
	if err != nil {
		writeErr(w, statusFor(err), err)
		return
	}
	// Inbox actions touch items + derived entities + the inbox
	// count. Coarse fan-out: SPA refetches whatever is on screen.
	s.bus.Publish(events.ItemsChanged)
	s.bus.Publish(events.InboxChanged)
	s.bus.Publish(events.TodosChanged)
	s.bus.Publish(events.BugsChanged)
	s.bus.Publish(events.KBChanged)
	s.bus.Publish(events.UseCasesChanged)
	writeJSON(w, http.StatusOK, item)
}

func (s *Server) reclassifyInbox(w http.ResponseWriter, r *http.Request) {
	if s.agent == nil {
		writeMsg(w, http.StatusServiceUnavailable, "agent not running")
		return
	}
	var req reclassifyReq
	if err := readJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if !validDerivedCategory(req.Category) {
		writeMsg(w, http.StatusBadRequest, fmt.Sprintf("category %q: must be todo, bug, kb, or use_case", req.Category))
		return
	}
	itemID := r.PathValue("id")
	if err := s.agent.AcceptPending(r.Context(), itemID, req.Category); err != nil {
		writeErr(w, statusFor(err), err)
		return
	}
	item, err := s.store.GetScratchpadItem(r.Context(), itemID)
	if err != nil {
		writeErr(w, statusFor(err), err)
		return
	}
	// Inbox actions touch items + derived entities + the inbox
	// count. Coarse fan-out: SPA refetches whatever is on screen.
	s.bus.Publish(events.ItemsChanged)
	s.bus.Publish(events.InboxChanged)
	s.bus.Publish(events.TodosChanged)
	s.bus.Publish(events.BugsChanged)
	s.bus.Publish(events.KBChanged)
	s.bus.Publish(events.UseCasesChanged)
	writeJSON(w, http.StatusOK, item)
}

func (s *Server) rejectInbox(w http.ResponseWriter, r *http.Request) {
	if s.agent == nil {
		writeMsg(w, http.StatusServiceUnavailable, "agent not running")
		return
	}
	itemID := r.PathValue("id")
	if err := s.agent.RejectPending(r.Context(), itemID); err != nil {
		writeErr(w, statusFor(err), err)
		return
	}
	item, err := s.store.GetScratchpadItem(r.Context(), itemID)
	if err != nil {
		writeErr(w, statusFor(err), err)
		return
	}
	// Inbox actions touch items + derived entities + the inbox
	// count. Coarse fan-out: SPA refetches whatever is on screen.
	s.bus.Publish(events.ItemsChanged)
	s.bus.Publish(events.InboxChanged)
	s.bus.Publish(events.TodosChanged)
	s.bus.Publish(events.BugsChanged)
	s.bus.Publish(events.KBChanged)
	s.bus.Publish(events.UseCasesChanged)
	writeJSON(w, http.StatusOK, item)
}
