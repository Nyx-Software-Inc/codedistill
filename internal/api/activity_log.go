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
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"codedistill/internal/domain"
	"codedistill/internal/id"
)

// Activity log (item-editing redesign, slice 1). A work item's timeline, built
// on the append-only item_events substrate — the system events already land
// here (status-changed, etc.); this adds human/agent-authored `note` entries
// carrying a markdown body. See docs/design/item-editing-redesign.md.

// listActivityLog returns a work item's events oldest-first (notes + system).
func (s *Server) listActivityLog(w http.ResponseWriter, r *http.Request, ownerType string) {
	events, err := s.store.ListItemEvents(r.Context(), ownerType, r.PathValue("id"))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	if events == nil {
		events = []*domain.ItemEvent{}
	}
	writeJSON(w, http.StatusOK, events)
}

// appendNote adds a markdown `note` entry to a work item's activity log. The log
// is append-only — notes are never edited or deleted, matching the provenance
// model (post a correction, don't rewrite history).
func (s *Server) appendNote(w http.ResponseWriter, r *http.Request, ownerType string) {
	var req struct {
		Text string `json:"text"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	text := strings.TrimSpace(req.Text)
	if text == "" {
		writeErr(w, http.StatusBadRequest, fmt.Errorf("note text is required"))
		return
	}
	// The owner must exist — item_events has no FK to the owner tables, so
	// without this a note POSTed to a bogus id would be accepted (201) and
	// orphaned in the append-only log (audit M19). ErrNotFound → 404.
	ownerID := r.PathValue("id")
	if err := s.ensureOwnerExists(r, ownerType, ownerID); err != nil {
		writeErr(w, statusFor(err), err)
		return
	}
	e := &domain.ItemEvent{
		ID:          id.New(),
		OwnerType:   ownerType,
		OwnerID:     ownerID,
		Kind:        "note",
		Summary:     noteSummary(text),
		Body:        text,
		Source:      sourceUI,
		ActorUserID: userFromContext(r.Context()),
		CreatedAt:   time.Now().UTC(),
	}
	if err := s.store.RecordItemEvent(r.Context(), e); err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusCreated, e)
}

// recordNoteEvent appends a `note` entry to an owner's activity log from a
// non-log endpoint (e.g. a create/update payload carrying legacy `notes` text
// now that the blob columns are retired). Best-effort like recordEvent.
func (s *Server) recordNoteEvent(ctx context.Context, ownerType, ownerID, text, source string) {
	text = strings.TrimSpace(text)
	if text == "" {
		return
	}
	e := &domain.ItemEvent{
		ID:          id.New(),
		OwnerType:   ownerType,
		OwnerID:     ownerID,
		Kind:        "note",
		Summary:     noteSummary(text),
		Body:        text,
		Source:      source,
		ActorUserID: userFromContext(ctx),
		CreatedAt:   time.Now().UTC(),
	}
	if err := s.store.RecordItemEvent(ctx, e); err != nil {
		s.log.Warn("note event failed", "owner", ownerType+"/"+ownerID, "err", err)
	}
}

// getLatestNotes returns the latest note per owner across the store — the canvas
// pulse source. Free read; the client maps owners to its visible cards.
func (s *Server) getLatestNotes(w http.ResponseWriter, r *http.Request) {
	notes, err := s.store.LatestNotes(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	if notes == nil {
		notes = []*domain.ItemEvent{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"notes": notes})
}

// noteSummary is the one-line preview (first non-blank line, truncated) used in
// compact timelines and the canvas pulse.
func noteSummary(text string) string {
	line := text
	if i := strings.IndexByte(text, '\n'); i >= 0 {
		line = text[:i]
	}
	line = strings.TrimSpace(line)
	const max = 120
	if len(line) > max {
		return strings.TrimSpace(line[:max]) + "…"
	}
	return line
}
