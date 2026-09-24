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
	"sort"
	"time"

	"codedistill/internal/domain"
	"codedistill/internal/events"
	"codedistill/internal/id"
	"codedistill/internal/storage"
)

type createScratchpadReq struct {
	Name               string `json:"name"`
	ClassificationMode string `json:"classification_mode,omitempty"` // default "full"
}

type updateScratchpadReq struct {
	Name               *string `json:"name,omitempty"`
	ClassificationMode *string `json:"classification_mode,omitempty"`
}

func validClassificationMode(m string) bool {
	return m == "off" || m == "strict" || m == "full"
}

func (s *Server) listScratchpads(w http.ResponseWriter, r *http.Request) {
	list, err := s.store.ListScratchpads(r.Context(), r.PathValue("pid"))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	if list == nil {
		list = []*domain.Scratchpad{}
	}
	writeJSON(w, http.StatusOK, list)
}

func (s *Server) createScratchpad(w http.ResponseWriter, r *http.Request) {
	projectID := r.PathValue("pid")
	if _, err := s.store.GetProject(r.Context(), projectID); err != nil {
		writeErr(w, statusFor(err), err)
		return
	}

	var req createScratchpadReq
	if err := readJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if req.Name == "" {
		writeMsg(w, http.StatusBadRequest, "name is required")
		return
	}
	mode := req.ClassificationMode
	if mode == "" {
		mode = "full"
	}
	if !validClassificationMode(mode) {
		writeMsg(w, http.StatusBadRequest, fmt.Sprintf("classification_mode %q: must be off, strict, or full", mode))
		return
	}
	sp := &domain.Scratchpad{
		ID: id.New(), ProjectID: projectID, Name: req.Name,
		ClassificationMode: mode, CreatedAt: time.Now().UTC(),
	}
	if err := s.store.CreateScratchpad(r.Context(), sp); err != nil {
		writeErr(w, statusFor(err), err)
		return
	}
	s.bus.Publish(events.ScratchpadsChanged)
	writeJSON(w, http.StatusCreated, sp)
}

func (s *Server) getScratchpad(w http.ResponseWriter, r *http.Request) {
	sp, err := s.store.GetScratchpad(r.Context(), r.PathValue("id"))
	if err != nil {
		writeErr(w, statusFor(err), err)
		return
	}
	writeJSON(w, http.StatusOK, sp)
}

func (s *Server) updateScratchpad(w http.ResponseWriter, r *http.Request) {
	sp, err := s.store.GetScratchpad(r.Context(), r.PathValue("id"))
	if err != nil {
		writeErr(w, statusFor(err), err)
		return
	}
	var req updateScratchpadReq
	if err := readJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if req.Name != nil {
		sp.Name = *req.Name
	}
	if req.ClassificationMode != nil {
		if !validClassificationMode(*req.ClassificationMode) {
			writeMsg(w, http.StatusBadRequest, fmt.Sprintf("classification_mode %q: must be off, strict, or full", *req.ClassificationMode))
			return
		}
		sp.ClassificationMode = *req.ClassificationMode
	}
	if err := s.store.UpdateScratchpad(r.Context(), sp); err != nil {
		writeErr(w, statusFor(err), err)
		return
	}
	s.bus.Publish(events.ScratchpadsChanged)
	writeJSON(w, http.StatusOK, sp)
}

func (s *Server) deleteScratchpad(w http.ResponseWriter, r *http.Request) {
	if err := s.store.DeleteScratchpad(r.Context(), r.PathValue("id")); err != nil {
		writeErr(w, statusFor(err), err)
		return
	}
	s.bus.Publish(events.ScratchpadsChanged)
	writeEmpty(w, http.StatusNoContent)
}

// gridWidth matches the canvas grid. Items are placed
// with their original grid_w preserved; if grid_w > gridWidth (legacy
// data) we clamp to gridWidth so the item still fits.
//
// MUST match the canvas renderer's column count (ScratchpadGrid COLS).
// They diverged once — renderer 24, backend 12 — which made restack
// pack into the left half of the canvas (CodeDestill_imports bug #2).
const gridWidth = 24

// restackReq is the optional POST body for restack.
//
// mode: "tidy" (default — keep sizes, close gaps), "fit" (recompute
// natural sizes for textual items from content, then pack), "cols2" /
// "cols3" (force a uniform 2- or 3-column width, then pack).
// auto_resize=true is the legacy spelling of cols2 and wins only when
// mode is empty.
type restackReq struct {
	Mode       string `json:"mode"`
	AutoResize bool   `json:"auto_resize"`
}

// textualContentType reports whether fit-to-content sizing applies.
// Binary-ish types (image, file, sketch, composite) and group frames
// keep their geometry — their size reflects visual intent, not text
// length.
func textualContentType(ct string) bool {
	return ct == "text" || ct == "code_snippet" || ct == "link"
}

// restackScratchpad rewrites every item's (grid_col, grid_row) — and,
// in fit/column modes, (grid_w, grid_h) — so the layout is compact.
// Items are walked in current spatial order so left-to-right /
// top-to-bottom intent survives; skylinePack slides each into the
// lowest gap that fits.
//
// Both visible and hidden items participate so unhiding later doesn't
// reveal an item overlapping a visible one. The whole rewrite happens
// in a single transaction (RestackScratchpadItems) so a concurrent
// refetch can't see a half-applied state.
//
// Group frames (content_type=group) are positioned but never resized;
// their on-canvas bbox derives from their members.
func (s *Server) restackScratchpad(w http.ResponseWriter, r *http.Request) {
	sid := r.PathValue("id")
	if _, err := s.store.GetScratchpad(r.Context(), sid); err != nil {
		writeErr(w, statusFor(err), err)
		return
	}
	req := restackReq{}
	// Body is optional — older clients call this with no body and
	// expect the pre-toggle behavior. Only surface decode errors when
	// the caller actually sent something we couldn't parse.
	if r.ContentLength > 0 {
		if err := readJSON(r, &req); err != nil {
			writeErr(w, http.StatusBadRequest, err)
			return
		}
	}
	mode := req.Mode
	if mode == "" {
		if req.AutoResize {
			mode = "cols2"
		} else {
			mode = "tidy"
		}
	}
	colWidths := map[string]int{"cols2": gridWidth / 2, "cols3": gridWidth / 3} // 12-wide / 8-wide on the 24-col grid
	if _, ok := colWidths[mode]; !ok && mode != "tidy" && mode != "fit" {
		writeMsg(w, http.StatusBadRequest, "mode must be one of tidy | fit | cols2 | cols3")
		return
	}

	items, err := s.store.ListScratchpadItems(r.Context(), sid)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}

	// Decide each item's target geometry for the pack. resized=true
	// propagates the new w/h into the storage write.
	type target struct {
		item    *domain.ScratchpadItem
		w, h    int
		resized bool
	}
	targets := make([]target, 0, len(items))
	for _, it := range items {
		// Off-canvas items (archived, hidden) must not reserve grid space —
		// packing them left permanent invisible holes in the layout that no
		// restack could clear (Backlog bug, 2026-08-01). They keep their
		// stale coordinates; unarchive repositions to the next free row.
		if it.Hidden || it.ArchivedAt != nil {
			continue
		}
		t := target{item: it, w: it.GridW, h: it.GridH}
		switch {
		case it.ContentType == "group":
			// never resized
		case mode == "fit" && textualContentType(it.ContentType):
			t.w, t.h = domain.NaturalCardSize(it.Content)
			t.resized = true
		case colWidths[mode] > 0:
			t.w = colWidths[mode]
			t.resized = true
		}
		if t.w <= 0 {
			t.w = 1
		}
		if t.w > gridWidth {
			t.w = gridWidth
		}
		if t.h <= 0 {
			t.h = 1
		}
		targets = append(targets, t)
	}

	// Spatial order: current (row, col) so relative arrangement survives.
	sort.SliceStable(targets, func(i, j int) bool {
		if targets[i].item.GridRow != targets[j].item.GridRow {
			return targets[i].item.GridRow < targets[j].item.GridRow
		}
		return targets[i].item.GridCol < targets[j].item.GridCol
	})

	geoms := make([]gridGeom, len(targets))
	for i, t := range targets {
		geoms[i] = gridGeom{W: t.w, H: t.h}
	}
	spots := skylinePack(geoms)

	// Only write rows whose placement (or size, in resizing modes)
	// actually changes. A fully-stable layout returns 0 WITHOUT writing
	// or publishing — which is what lets the auto-tidy loop (UC-12)
	// terminate: tidy → items.changed → auto-tidy fires again → no-op.
	positions := make([]storage.ItemGridPosition, 0, len(targets))
	for i, t := range targets {
		moved := t.item.GridCol != spots[i].Col || t.item.GridRow != spots[i].Row
		sized := t.resized && (t.item.GridW != t.w || t.item.GridH != t.h)
		if !moved && !sized {
			continue
		}
		pos := storage.ItemGridPosition{
			ID:      t.item.ID,
			GridCol: spots[i].Col,
			GridRow: spots[i].Row,
		}
		if t.resized {
			pos.GridW = t.w
			pos.GridH = t.h
		}
		positions = append(positions, pos)
	}
	if len(positions) == 0 {
		writeJSON(w, http.StatusOK, map[string]int{"items_restacked": 0})
		return
	}
	if err := s.store.RestackScratchpadItems(r.Context(), positions); err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	s.bus.Publish(events.ItemsChanged)
	writeJSON(w, http.StatusOK, map[string]int{"items_restacked": len(positions)})
}

type gridGeom struct{ W, H int }
type gridSpot struct{ Col, Row int }

// skylinePack places each geometry (in input order) at the lowest row
// — then leftmost column — where it fits among the already-placed
// items, on a fixed 12-column grid. Unlike shelf packing, gaps under
// short items get filled by later cards that fit them, so mixed-height
// layouts stay tight (Backlog bug #2). O(n² · rows) — fine for canvas
// sizes (tens of cards).
func skylinePack(geoms []gridGeom) []gridSpot {
	placed := make([]gridSpot, 0, len(geoms))
	dims := make([]gridGeom, 0, len(geoms))
	fits := func(col, row, w, h int) bool {
		for i, p := range placed {
			d := dims[i]
			if col+w <= p.Col || p.Col+d.W <= col || row+h <= p.Row || p.Row+d.H <= row {
				continue
			}
			return false
		}
		return true
	}
	out := make([]gridSpot, len(geoms))
	for i, g := range geoms {
		w := g.W
		if w <= 0 {
			w = 1
		}
		if w > gridWidth {
			w = gridWidth
		}
		h := g.H
		if h <= 0 {
			h = 1
		}
	place:
		for row := 0; ; row++ {
			for col := 0; col+w <= gridWidth; col++ {
				if fits(col, row, w, h) {
					out[i] = gridSpot{Col: col, Row: row}
					placed = append(placed, out[i])
					dims = append(dims, gridGeom{W: w, H: h})
					break place
				}
			}
		}
	}
	return out
}
