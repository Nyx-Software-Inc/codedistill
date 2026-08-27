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
	"net/http"
	"sort"
	"time"

	"codedistill/internal/domain"
	"codedistill/internal/id"
)

// Event sources. Recorded with each lineage event so the timeline can
// show "via UI" / "via MCP" / "by the classifier".
const (
	sourceUI    = "ui"
	sourceAgent = "agent"
)

// recordEvent appends one lineage event. Best-effort: a logging warning on
// failure, never an error to the caller — history must never block a real
// mutation. Source is the actor surface; HTTP handlers pass sourceUI.
func (s *Server) recordEvent(ctx context.Context, ownerType, ownerID, kind, summary, source string) {
	e := &domain.ItemEvent{
		ID: id.New(), OwnerType: ownerType, OwnerID: ownerID,
		Kind: kind, Summary: summary, Source: source,
		// WHO acted (orthogonal to Source/HOW). Resolved from the request's
		// authenticated user; the local user in single-user mode.
		ActorUserID: userFromContext(ctx),
		CreatedAt:   time.Now().UTC(),
	}
	if err := s.store.RecordItemEvent(ctx, e); err != nil {
		s.log.Warn("lineage: record event failed",
			"owner_type", ownerType, "owner_id", ownerID, "kind", kind, "err", err)
	}
}

// itemLineage returns the merged, time-sorted event log for a scratchpad
// item AND its derived work item (todo/bug/kb/use_case), so the modal's
// History tab shows the whole arc from capture through downstream work.
func (s *Server) itemLineage(w http.ResponseWriter, r *http.Request) {
	itemID := r.PathValue("id")
	item, err := s.store.GetScratchpadItem(r.Context(), itemID)
	if err != nil {
		writeErr(w, statusFor(err), err)
		return
	}
	events := s.gatherLineage(r.Context(), item)
	if events == nil {
		events = []*domain.ItemEvent{}
	}
	writeJSON(w, http.StatusOK, events)
}

// gatherLineage collects the item's own events plus its derived item's,
// merged oldest-first.
func (s *Server) gatherLineage(ctx context.Context, item *domain.ScratchpadItem) []*domain.ItemEvent {
	var all []*domain.ItemEvent
	if own, err := s.store.ListItemEvents(ctx, "scratchpad_item", item.ID); err == nil {
		all = append(all, own...)
	}
	cat := effectiveCategory(item)
	if ot := derivedOwnerType(cat); ot != "" && item.DerivedItemID != "" {
		if der, err := s.store.ListItemEvents(ctx, ot, item.DerivedItemID); err == nil {
			all = append(all, der...)
		}
	}
	sort.SliceStable(all, func(i, j int) bool {
		return all[i].CreatedAt.Before(all[j].CreatedAt)
	})
	return all
}

// recordDerivedStatus is a small helper for the derived-item handlers
// (todo/bug/use-case) to log a status transition with a readable summary.
func (s *Server) recordDerivedStatus(ctx context.Context, ownerType, ownerID, newStatus, source string) {
	s.recordEvent(ctx, ownerType, ownerID, "status-changed", "Status → "+newStatus, source)
}
