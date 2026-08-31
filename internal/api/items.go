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
	"fmt"
	"net/http"
	"strings"
	"time"

	"codedistill/internal/domain"
	"codedistill/internal/events"
	"codedistill/internal/id"
	"codedistill/internal/storage"
	"codedistill/internal/tags"
)

type createItemReq struct {
	Name        string `json:"name,omitempty"`
	Content     string `json:"content"`
	ContentType string `json:"content_type,omitempty"` // default "text"
	// optional; see domain.ClassificationOverrides — todo|bug|kb|use_case|skip.
	// This comment said todo|bug|kb|skip and had missed use_case since v0.7.0,
	// the same staleness as the -override flag help (CE-review item 24).
	ClassificationOverride string `json:"classification_override,omitempty"`
}

type updateItemReq struct {
	Name        *string `json:"name,omitempty"`
	Content     *string `json:"content,omitempty"`
	ContentType *string `json:"content_type,omitempty"`
	// Correction marks a content edit on a CLASSIFIED item as a deliberate
	// fix of the original text (a typo). Without it, content edits on
	// classified items are refused — the source is immutable provenance;
	// progress updates belong in the activity log (poke-1 source guard).
	Correction             *bool     `json:"correction,omitempty"`
	ClassificationOverride *string   `json:"classification_override,omitempty"`
	Hidden                 *bool     `json:"hidden,omitempty"`
	Annotations            *string   `json:"annotations,omitempty"`
	Tags                   *[]string `json:"tags,omitempty"`
	// Grid layout (all-or-none — partial updates allowed; fields not sent are unchanged).
	GridCol *int `json:"grid_col,omitempty"`
	GridRow *int `json:"grid_row,omitempty"`
	GridW   *int `json:"grid_w,omitempty"`
	GridH   *int `json:"grid_h,omitempty"`
	// Blob metadata patches (Slice 4 follow-up): lets the
	// SketchEditor attach a preview PNG (uploaded via the
	// standalone /blobs endpoint) to an existing sketch item, and
	// gives the composite-doc flow room to attach inline images
	// later. Patching to empty string clears blob_sha; omitting
	// leaves it unchanged.
	BlobSHA  *string `json:"blob_sha,omitempty"`
	MimeType *string `json:"mime_type,omitempty"`
	ByteSize *int64  `json:"byte_size,omitempty"`
	Width    *int    `json:"width,omitempty"`
	Height   *int    `json:"height,omitempty"`
	// Slice 6 — group membership + collapse state. Setting
	// group_id to "" removes the item from its group; the handler
	// auto-deletes the previous group if it has no remaining
	// children. collapsed is only meaningful on items where
	// content_type='group'.
	GroupID   *string `json:"group_id,omitempty"`
	Collapsed *bool   `json:"collapsed,omitempty"`
}

// Default shape and placement for newly-created items; the API stacks new
// items at the bottom of the existing layout (full-ish width).
const (
	defaultGridW = 24 // full canvas width (matches renderer COLS / packer gridWidth)
	defaultGridH = 4
)

type reclassifyReq struct {
	Category string `json:"category"` // todo|bug|kb|use_case
}

func validContentType(t string) bool {
	// 'image' and 'file' are valid here too even though they
	// normally arrive via the blob upload path — the validation has
	// to mirror what the DB CHECK constraint allows, since any value
	// rejected here would be a 400 even on legitimate updates that
	// pass through PATCH. 'sketch' (Slice 4) is JSON content posted
	// via the regular create path.
	switch t {
	case "text", "code_snippet", "link", "image", "file", "sketch", "composite", "group":
		return true
	}
	return false
}

func validOverride(o string) bool {
	return o == "" || o == "todo" || o == "bug" || o == "kb" || o == "skip" || o == "use_case"
}

// normalizeTags / extractHashtags / mergeTags now live in internal/tags,
// shared with the MCP capture path (Backlog #13). Thin aliases keep the
// existing call sites readable.
func normalizeTags(in []string) []string { return tags.Normalize(in) }

func extractHashtags(content string) []string { return tags.Extract(content) }

func mergeTags(existing, extra []string) []string { return tags.Merge(existing, extra) }

func validDerivedCategory(c string) bool {
	return c == "todo" || c == "bug" || c == "kb" || c == "use_case"
}

func (s *Server) listItems(w http.ResponseWriter, r *http.Request) {
	list, err := s.store.ListScratchpadItems(r.Context(), r.PathValue("sid"))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	// Archived items (backlog item #19) are off the canvas by default;
	// ?include_archived=true returns them too (the Show-archived toggle).
	if r.URL.Query().Get("include_archived") != "true" {
		kept := make([]*domain.ScratchpadItem, 0, len(list))
		for _, it := range list {
			if it.ArchivedAt == nil {
				kept = append(kept, it)
			}
		}
		list = kept
	}
	if list == nil {
		list = []*domain.ScratchpadItem{}
	}
	writeJSON(w, http.StatusOK, list)
}

// archiveItem / unarchiveItem toggle an item's archived state (backlog item #19):
// off the canvas but retained + searchable; un-archive restores it.
func (s *Server) archiveItem(w http.ResponseWriter, r *http.Request) {
	now := time.Now().UTC()
	if err := s.store.SetScratchpadItemArchived(r.Context(), r.PathValue("id"), &now); err != nil {
		writeErr(w, statusFor(err), err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) unarchiveItem(w http.ResponseWriter, r *http.Request) {
	// Archived items don't reserve canvas space (restack packs without them),
	// so this item's stored coordinates may now be under a visible card.
	// Reposition to the next free row — computed BEFORE clearing the archived
	// flag so the item's own stale row can't inflate the answer.
	item, err := s.store.GetScratchpadItem(r.Context(), r.PathValue("id"))
	if err != nil {
		writeErr(w, statusFor(err), err)
		return
	}
	nextRow, rowErr := s.store.NextAvailableGridRow(r.Context(), item.ScratchpadID)
	if err := s.store.SetScratchpadItemArchived(r.Context(), item.ID, nil); err != nil {
		writeErr(w, statusFor(err), err)
		return
	}
	if rowErr == nil && (item.GridRow != nextRow || item.GridCol != 0) {
		// Best-effort: a failed reposition still leaves the item un-archived.
		_ = s.store.RestackScratchpadItems(r.Context(), []storage.ItemGridPosition{
			{ID: item.ID, GridCol: 0, GridRow: nextRow},
		})
	}
	w.WriteHeader(http.StatusNoContent)
}

// createItem creates a Scratchpad_Item and enqueues it for classification.
// Returns 201 Created with the item as stored. Classification runs asynchronously
// dismissSimilarity clears the dedup candidate fields on an item — the
// "not a duplicate" action from the pending-review banner. Idempotent.
// groupSimilar groups an item with its possible-duplicate match (the "Group"
// action on the banner). Needs the paid duplicate grouper.
func (s *Server) groupSimilar(w http.ResponseWriter, r *http.Request) {
	if s.grouper == nil {
		writeMsg(w, http.StatusServiceUnavailable, "duplicate grouping is not available (requires the Dedup feature)")
		return
	}
	itemID := r.PathValue("id")
	item, err := s.store.GetScratchpadItem(r.Context(), itemID)
	if err != nil {
		writeErr(w, statusFor(err), err)
		return
	}
	if item.SimilarToID == "" {
		writeMsg(w, http.StatusBadRequest, "item has no possible-duplicate to group with")
		return
	}
	if err := s.grouper.GroupPair(r.Context(), itemID, item.SimilarToID); err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	s.bus.Publish(events.ItemsChanged)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) dismissSimilarity(w http.ResponseWriter, r *http.Request) {
	itemID := r.PathValue("id")
	if err := s.store.ClearItemSimilarity(r.Context(), itemID); err != nil {
		writeErr(w, statusFor(err), err)
		return
	}
	item, err := s.store.GetScratchpadItem(r.Context(), itemID)
	if err != nil {
		writeErr(w, statusFor(err), err)
		return
	}
	s.bus.Publish(events.ItemsChanged)
	writeJSON(w, http.StatusOK, item)
}

// moveItemReq targets the destination scratchpad for a card move operation.
type moveItemReq struct {
	ScratchpadID string `json:"scratchpad_id"`
}

// moveItem changes a scratchpad item's owning scratchpad. Validates that
// the destination exists in the same project as the source — moves
// across projects are rejected (anchors are repo-scoped and the dest
// project may have a different repo_root).
func (s *Server) moveItem(w http.ResponseWriter, r *http.Request) {
	itemID := r.PathValue("id")
	var req moveItemReq
	if err := readJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if strings.TrimSpace(req.ScratchpadID) == "" {
		writeMsg(w, http.StatusBadRequest, "scratchpad_id is required")
		return
	}
	src, err := s.store.GetScratchpadItem(r.Context(), itemID)
	if err != nil {
		writeErr(w, statusFor(err), err)
		return
	}
	if src.ScratchpadID == req.ScratchpadID {
		writeJSON(w, http.StatusOK, src)
		return
	}
	srcSp, err := s.store.GetScratchpad(r.Context(), src.ScratchpadID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	dstSp, err := s.store.GetScratchpad(r.Context(), req.ScratchpadID)
	if err != nil {
		writeErr(w, statusFor(err), err)
		return
	}
	crossProject := srcSp.ProjectID != dstSp.ProjectID
	category := effectiveCategory(src)
	if crossProject {
		// Code anchors reference the SOURCE project's codebase and would
		// dangle in the destination. Rather than silently drop or keep
		// stale anchors, block the cross-project move when the item or
		// its derived work item carries any — the user removes them
		// first as a deliberate choice. (UC-1.)
		n, err := s.countMoveAnchors(r.Context(), itemID, category, src.DerivedItemID)
		if err != nil {
			writeErr(w, http.StatusInternalServerError, err)
			return
		}
		if n > 0 {
			writeMsg(w, http.StatusConflict, fmt.Sprintf(
				"this item has %d code anchor(s) tied to %q's codebase; remove them before moving it to another project",
				n, srcSp.Name))
			return
		}
	}
	if crossProject {
		// Atomic: move the item AND re-home its derived todo/bug/kb/
		// use-case (with a fresh per-project number) in one transaction,
		// so a number collision can't leave the item in the new project
		// while its derived row stays behind (split-brain).
		if err := s.store.MoveItemToProject(r.Context(), itemID, req.ScratchpadID,
			category, src.DerivedItemID, dstSp.ProjectID); err != nil {
			writeErr(w, statusFor(err), err)
			return
		}
	} else {
		if err := s.store.MoveScratchpadItem(r.Context(), itemID, req.ScratchpadID); err != nil {
			writeErr(w, statusFor(err), err)
			return
		}
	}
	moved, err := s.store.GetScratchpadItem(r.Context(), itemID)
	if err != nil {
		writeErr(w, statusFor(err), err)
		return
	}
	summary := "Moved to " + dstSp.Name
	if crossProject {
		summary = "Moved to " + dstSp.Name + " (project " + dstSp.ProjectID + ")"
	}
	s.recordEvent(r.Context(), "scratchpad_item", itemID, "moved", summary, sourceUI)
	s.bus.Publish(events.ItemsChanged)
	if crossProject && category != "" {
		s.bus.Publish(derivedChangedEvent(category))
	}
	writeJSON(w, http.StatusOK, moved)
}

// effectiveCategory returns the item's resolved work-item category:
// an explicit override wins over the classifier's proposal.
func effectiveCategory(it *domain.ScratchpadItem) string {
	if it.ClassificationOverride != "" {
		return it.ClassificationOverride
	}
	return it.ProposedCategory
}

// derivedOwnerType maps a category to the anchor owner_type for its
// derived work item; "" for categories with no derived row (e.g. skip).
func derivedOwnerType(category string) string {
	switch category {
	case "todo":
		return "todo_item"
	case "bug":
		return "bug_item"
	case "kb":
		return "knowledge_entry"
	case "use_case":
		return "use_case_item"
	}
	return ""
}

// derivedChangedEvent maps a category to the event that refreshes its
// list view, so a cross-project re-home updates both projects' panes.
func derivedChangedEvent(category string) string {
	switch category {
	case "todo":
		return events.TodosChanged
	case "bug":
		return events.BugsChanged
	case "kb":
		return events.KBChanged
	case "use_case":
		return events.UseCasesChanged
	}
	return events.ItemsChanged
}

// countMoveAnchors totals the code anchors on the item itself plus those
// on its derived work item — the set that would dangle on a cross-project
// move.
func (s *Server) countMoveAnchors(ctx context.Context, itemID, category, derivedID string) (int, error) {
	own, err := s.store.ListCodeAnchors(ctx, "scratchpad_item", itemID)
	if err != nil {
		return 0, err
	}
	total := len(own)
	if ot := derivedOwnerType(category); ot != "" && derivedID != "" {
		der, err := s.store.ListCodeAnchors(ctx, ot, derivedID)
		if err != nil {
			return 0, err
		}
		total += len(der)
	}
	return total, nil
}

// and updates the item's state/category/reasoning later; clients poll GET /items/{id}
// or subscribe to a change stream (Phase 2+).
func (s *Server) createItem(w http.ResponseWriter, r *http.Request) {
	sid := r.PathValue("sid")
	if _, err := s.store.GetScratchpad(r.Context(), sid); err != nil {
		writeErr(w, statusFor(err), err)
		return
	}

	var req createItemReq
	if err := readJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	ct := req.ContentType
	if ct == "" {
		// Auto-upgrade to 'link' when the content is a single bare
		// http(s) URL — this is what makes Slice 3's OpenGraph fetch
		// reachable from the UI's "paste-and-Enter" flow. Explicit
		// content_type='text' (or any other) is always respected.
		if looksLikeBareURL(req.Content) {
			ct = "link"
		} else {
			ct = "text"
		}
	}
	// Content is required for text-shaped types but legitimately
	// empty for structural types whose identity isn't text:
	//   - group:    name + child membership (Slice 6)
	//   - sketch:   may be opened blank, then saved with scene JSON
	//   - image / file: bytes live in the blob, not the content col
	//                   (these usually arrive via the blob upload
	//                   endpoint, but the create path should be
	//                   permissive if a caller skips that).
	if req.Content == "" && ct != "group" && ct != "sketch" && ct != "image" && ct != "file" {
		writeMsg(w, http.StatusBadRequest, "content is required")
		return
	}
	if !validContentType(ct) {
		writeMsg(w, http.StatusBadRequest, fmt.Sprintf("content_type %q: must be one of text, code_snippet, link, image, file, sketch, composite, group", ct))
		return
	}
	if !validOverride(req.ClassificationOverride) {
		writeMsg(w, http.StatusBadRequest, fmt.Sprintf("classification_override %q: must be todo, bug, kb, use_case, skip, or empty", req.ClassificationOverride))
		return
	}
	nextRow, err := s.store.NextAvailableGridRow(r.Context(), sid)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	now := time.Now().UTC()
	item := &domain.ScratchpadItem{
		ID: id.New(), ScratchpadID: sid,
		Name:        strings.TrimSpace(req.Name),
		ContentType: ct, Content: req.Content,
		ClassificationState:    "unprocessed",
		ClassificationOverride: req.ClassificationOverride,
		Tags:                   normalizeTags(extractHashtags(req.Content)),
		GridCol:                0,
		GridRow:                nextRow,
		GridW:                  defaultGridW,
		GridH:                  defaultGridH,
		CreatedAt:              now, UpdatedAt: now,
	}
	// Cards are born at their natural size (Backlog bug #2): a short
	// note arrives as a small card, not a full-width slab. Binary-ish
	// types keep the legacy default; the upload paths size those.
	if textualContentType(ct) {
		item.GridW, item.GridH = naturalSize(req.Content)
	}
	if err := s.store.CreateScratchpadItem(r.Context(), item); err != nil {
		writeErr(w, statusFor(err), err)
		return
	}
	if err := refreshDetectedAnchors(r.Context(), s.store, item.ID, item.Content, now); err != nil {
		s.log.Warn("url-detect on create failed", "item_id", item.ID, "err", err)
	}
	if s.agent != nil {
		s.agent.Enqueue(item.ID)
	}
	// Rich Canvas Slice 3: kick off OpenGraph fetch in the background
	// for link items. The goroutine uses context.Background since the
	// request context dies the moment we write the response. The
	// fetcher always stamps og_fetched_at on completion (success or
	// failure) so we never re-fetch indefinitely. SPA polls items
	// every POLL_MS and the card re-renders when OG data lands.
	if item.ContentType == "link" {
		go s.fetchAndStoreOG(context.Background(), item.ID, item.Content)
	}
	s.recordEvent(r.Context(), "scratchpad_item", item.ID, "created", "Item created", sourceUI)
	s.bus.Publish(events.ItemsChanged)
	writeJSON(w, http.StatusCreated, item)
}

func (s *Server) getItem(w http.ResponseWriter, r *http.Request) {
	item, err := s.store.GetScratchpadItem(r.Context(), r.PathValue("id"))
	if err != nil {
		writeErr(w, statusFor(err), err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}

// updateItem allows editing content and toggling the classification override.
// Phase 1: if the override changes to a category or skip, the item is re-enqueued
// so the agent applies it (delete+recreate derived item if needed is Phase 2).
func (s *Server) updateItem(w http.ResponseWriter, r *http.Request) {
	item, err := s.store.GetScratchpadItem(r.Context(), r.PathValue("id"))
	if err != nil {
		writeErr(w, statusFor(err), err)
		return
	}
	var req updateItemReq
	if err := readJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	overrideChanged := false
	// Snapshot for lineage (UC-5): compare after the mutation to record
	// only fields that actually changed.
	priorTags := strings.Join(item.Tags, ",")
	priorAnnotations := item.Annotations
	priorContent := item.Content
	if req.Name != nil {
		item.Name = strings.TrimSpace(*req.Name)
	}
	if req.Content != nil {
		// Source guard: once classified, the original text is immutable
		// provenance. A deliberate typo fix passes correction:true (the
		// Source tab's Correct… button does); everything else — progress
		// notes, status appendices — belongs in the activity log.
		if *req.Content != item.Content && item.ClassificationState == "classified" &&
			(req.Correction == nil || !*req.Correction) {
			writeMsg(w, http.StatusConflict,
				"the source text of a classified item is immutable — add updates to the item's Log instead; to fix a typo in the original, resend with \"correction\": true")
			return
		}
		item.Content = *req.Content
	}
	if req.ContentType != nil {
		if !validContentType(*req.ContentType) {
			writeMsg(w, http.StatusBadRequest, fmt.Sprintf("content_type %q: invalid", *req.ContentType))
			return
		}
		item.ContentType = *req.ContentType
	}
	if req.ClassificationOverride != nil {
		if !validOverride(*req.ClassificationOverride) {
			writeMsg(w, http.StatusBadRequest, fmt.Sprintf("classification_override %q: invalid", *req.ClassificationOverride))
			return
		}
		if *req.ClassificationOverride != item.ClassificationOverride {
			overrideChanged = true
			item.ClassificationOverride = *req.ClassificationOverride
		}
	}
	if req.Hidden != nil {
		item.Hidden = *req.Hidden
	}
	if req.Annotations != nil && item.ContentType == "group" {
		// A group frame's single mutable note (persisted via group_notes
		// below); for non-group items `annotations` appends a log note instead
		// (the blob column is retired).
		item.Annotations = *req.Annotations
	}
	if req.Tags != nil {
		item.Tags = normalizeTags(*req.Tags)
	}
	// When the content changed, surface any inline #hashtags into tags
	// (additive — editing never removes a tag). Runs after the explicit
	// tags assignment so it merges into the final set (Todo #2).
	if req.Content != nil {
		item.Tags = mergeTags(item.Tags, extractHashtags(item.Content))
	}
	if req.GridCol != nil {
		item.GridCol = *req.GridCol
	}
	if req.GridRow != nil {
		item.GridRow = *req.GridRow
	}
	if req.GridW != nil {
		item.GridW = *req.GridW
	}
	if req.GridH != nil {
		item.GridH = *req.GridH
	}
	if req.BlobSHA != nil {
		item.BlobSHA = *req.BlobSHA
	}
	if req.MimeType != nil {
		item.MimeType = *req.MimeType
	}
	if req.ByteSize != nil {
		item.ByteSize = *req.ByteSize
	}
	if req.Width != nil {
		item.Width = *req.Width
	}
	if req.Height != nil {
		item.Height = *req.Height
	}
	// Track the previous group_id BEFORE applying the new one so
	// we can check whether the old group is now empty (and
	// therefore eligible for auto-delete) after the update commits.
	previousGroupID := item.GroupID
	if req.GroupID != nil {
		item.GroupID = *req.GroupID
	}
	if req.Collapsed != nil {
		item.Collapsed = *req.Collapsed
	}
	item.UpdatedAt = time.Now().UTC()
	if err := s.store.UpdateScratchpadItem(r.Context(), item); err != nil {
		writeErr(w, statusFor(err), err)
		return
	}
	// Canvas rework C2: a group frame's note lives in its own store (the
	// annotations column is gone); anything else's `annotations` payload is an
	// appended activity-log note.
	if req.Annotations != nil {
		if item.ContentType == "group" {
			if err := s.store.SetGroupNote(r.Context(), item.ID, item.Annotations, item.UpdatedAt); err != nil {
				s.log.Warn("group note store failed", "group", item.ID, "err", err)
			}
		} else {
			s.recordNoteEvent(r.Context(), "scratchpad_item", item.ID, *req.Annotations, sourceUI)
		}
	}
	// Only rebuild URL anchors when content actually changed — metadata-only
	// patches (grid, tags, override) shouldn't burn through DB writes.
	if req.Content != nil {
		if err := refreshDetectedAnchors(r.Context(), s.store, item.ID, item.Content, item.UpdatedAt); err != nil {
			s.log.Warn("url-detect on update failed", "item_id", item.ID, "err", err)
		}
	}
	if overrideChanged && s.agent != nil && item.ClassificationOverride != "" {
		s.agent.Enqueue(item.ID)
	}
	// Slice 6: if this PATCH moved the item out of a group (group_id
	// changed) and the previous group has no remaining children,
	// the group itself becomes orphaned UI — auto-delete it. Skip
	// when the previous id is the same as the new one (no move)
	// and when the previous id is empty (no prior group).
	if req.GroupID != nil && previousGroupID != "" && previousGroupID != item.GroupID {
		s.maybeDeleteEmptyGroup(r.Context(), previousGroupID)
	}
	// Lineage (UC-5): record the content-bearing edits a user cares to
	// trace. Geometry/visibility patches are intentionally not logged.
	// A correction to the immutable source's original text (item-editing
	// redesign, slice 2): logged for provenance, and — critically — decoupled
	// from re-derivation (only an override change re-derives), so fixing a typo
	// never silently re-classifies or clobbers the derived work item.
	if req.Content != nil && item.Content != priorContent {
		s.recordEvent(r.Context(), "scratchpad_item", item.ID, "corrected", "Original text edited", sourceUI)
	}
	if req.Annotations != nil && item.ContentType == "group" && item.Annotations != priorAnnotations {
		s.recordEvent(r.Context(), "scratchpad_item", item.ID, "annotated", "Group note edited", sourceUI)
	}
	if (req.Tags != nil || req.Content != nil) && strings.Join(item.Tags, ",") != priorTags {
		s.recordEvent(r.Context(), "scratchpad_item", item.ID, "tags-changed", "Tags updated", sourceUI)
	}
	if overrideChanged {
		s.recordEvent(r.Context(), "scratchpad_item", item.ID, "reclassified",
			"Reclassified to "+item.ClassificationOverride, sourceUI)
	}
	s.bus.Publish(events.ItemsChanged)
	writeJSON(w, http.StatusOK, item)
}

// maybeDeleteEmptyGroup deletes a group item when no scratchpad
// items reference it via group_id. Best-effort — failures are
// logged, not returned, since this is a downstream cleanup of an
// otherwise-successful update.
func (s *Server) maybeDeleteEmptyGroup(ctx context.Context, groupID string) {
	if groupID == "" {
		return
	}
	group, err := s.store.GetScratchpadItem(ctx, groupID)
	if err != nil {
		// Group may have been deleted concurrently — that's fine.
		return
	}
	if group.ContentType != "group" {
		// PATCH allowed setting an item's group_id to a non-group
		// item id (caller bug). Don't delete a real item under us.
		return
	}
	// List the scratchpad's items, count children referencing this
	// group. Using the existing list method since there's no
	// dedicated "count children" call yet; for v1 the scratchpad's
	// item count is small enough that this is cheap.
	siblings, err := s.store.ListScratchpadItems(ctx, group.ScratchpadID)
	if err != nil {
		s.log.Warn("group auto-delete: list siblings failed", "group_id", groupID, "err", err)
		return
	}
	for _, sib := range siblings {
		if sib.GroupID == groupID {
			return // group still has children
		}
	}
	if err := s.store.DeleteScratchpadItem(ctx, groupID); err != nil {
		s.log.Warn("group auto-delete: delete failed", "group_id", groupID, "err", err)
	}
}

func (s *Server) deleteItem(w http.ResponseWriter, r *http.Request) {
	if err := s.store.DeleteScratchpadItem(r.Context(), r.PathValue("id")); err != nil {
		writeErr(w, statusFor(err), err)
		return
	}
	s.bus.Publish(events.ItemsChanged)
	writeEmpty(w, http.StatusNoContent)
}
