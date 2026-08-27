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
	"net/url"
	"regexp"
	"strings"
	"time"

	"codedistill/internal/domain"
	"codedistill/internal/id"
)

// Owner-type constants for the code_anchors table. Kept here rather than in
// domain so business rules (which owners exist, which may carry anchors) stay
// in the API layer; the domain package just stores whatever the caller sets.
const (
	ownerScratchpadItem = "scratchpad_item"
	ownerTodoItem       = "todo_item"
	ownerBugItem        = "bug_item"
	ownerKnowledgeEntry = "knowledge_entry"
	ownerUseCaseItem    = "use_case_item"
)

type codeAnchorReq struct {
	Kind      string `json:"kind"`
	Path      string `json:"path,omitempty"`
	LineStart int    `json:"line_start,omitempty"`
	LineEnd   int    `json:"line_end,omitempty"`
	Revision  string `json:"revision,omitempty"`
	URL       string `json:"url,omitempty"`
	Label     string `json:"label,omitempty"`
	// Provenance is accepted from the client for convenience but defaults to
	// "user-set" when empty. URL auto-detect sets "url-detected" server-side;
	// the file-drop modal (M-Anchor-2) will set "file-dropped".
	Provenance string `json:"provenance,omitempty"`
}

var shaPattern = regexp.MustCompile(`^[0-9a-fA-F]{7,40}$`)

func validAnchorKind(k string) bool {
	return k == "file" || k == "commit" || k == "pr"
}

func validProvenance(p string) bool {
	return p == "user-set" || p == "url-detected" || p == "file-dropped" || p == "agent-suggested"
}

// validateAnchorReq enforces the kind-specific required fields described in
// domain.CodeAnchor. Returns an HTTP status and error message on failure.
func validateAnchorReq(req *codeAnchorReq) (int, string) {
	if !validAnchorKind(req.Kind) {
		return http.StatusBadRequest, fmt.Sprintf("kind %q: must be file, commit, or pr", req.Kind)
	}
	if req.Provenance != "" && !validProvenance(req.Provenance) {
		return http.StatusBadRequest, fmt.Sprintf("provenance %q: must be user-set, url-detected, file-dropped, or agent-suggested", req.Provenance)
	}
	switch req.Kind {
	case "file":
		if strings.TrimSpace(req.Path) == "" {
			return http.StatusBadRequest, "path is required for kind=file"
		}
		if req.LineStart < 0 || req.LineEnd < 0 {
			return http.StatusBadRequest, "line numbers must be non-negative"
		}
		if req.LineStart > 0 && req.LineEnd > 0 && req.LineEnd < req.LineStart {
			return http.StatusBadRequest, "line_end must be >= line_start"
		}
		if req.Revision != "" && !shaPattern.MatchString(req.Revision) {
			return http.StatusBadRequest, "revision must be 7–40 hex chars"
		}
	case "commit":
		if !shaPattern.MatchString(req.Revision) {
			return http.StatusBadRequest, "revision (7–40 hex chars) is required for kind=commit"
		}
	case "pr":
		if req.URL == "" {
			return http.StatusBadRequest, "url is required for kind=pr"
		}
		if _, err := url.Parse(req.URL); err != nil {
			return http.StatusBadRequest, fmt.Sprintf("url: %v", err)
		}
	}
	return 0, ""
}

// ensureOwnerExists verifies the owner exists in the appropriate table.
// Returns (storage-error, 404-if-missing) via the normal statusFor pathway.
func (s *Server) ensureOwnerExists(r *http.Request, ownerType, ownerID string) error {
	switch ownerType {
	case ownerScratchpadItem:
		_, err := s.store.GetScratchpadItem(r.Context(), ownerID)
		return err
	case ownerTodoItem:
		_, err := s.store.GetTodoItem(r.Context(), ownerID)
		return err
	case ownerBugItem:
		_, err := s.store.GetBugItem(r.Context(), ownerID)
		return err
	case ownerKnowledgeEntry:
		_, err := s.store.GetKnowledgeEntry(r.Context(), ownerID)
		return err
	case ownerUseCaseItem:
		_, err := s.store.GetUseCaseItem(r.Context(), ownerID)
		return err
	default:
		return fmt.Errorf("unknown owner_type %q", ownerType)
	}
}

// listProjectCodeAnchorsByPath returns every kind=file anchor in the project
// that targets the requested path, regardless of owner type. Used by the code
// canvas to populate the gutter for an open file.
func (s *Server) listProjectCodeAnchorsByPath(w http.ResponseWriter, r *http.Request) {
	pid := r.PathValue("pid")
	if _, err := s.store.GetProject(r.Context(), pid); err != nil {
		writeErr(w, statusFor(err), err)
		return
	}
	path := r.URL.Query().Get("path")
	if path == "" {
		writeMsg(w, http.StatusBadRequest, "path query param is required")
		return
	}
	list, err := s.store.ListCodeAnchorsByPath(r.Context(), pid, path)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	if list == nil {
		list = []*domain.CodeAnchorWithOwner{}
	}
	writeJSON(w, http.StatusOK, list)
}

// listScratchpadCodeAnchors returns every anchor for items in the given
// scratchpad. Used by the SPA to badge each card with an anchor indicator.
func (s *Server) listScratchpadCodeAnchors(w http.ResponseWriter, r *http.Request) {
	spID := r.PathValue("id")
	if _, err := s.store.GetScratchpad(r.Context(), spID); err != nil {
		writeErr(w, statusFor(err), err)
		return
	}
	list, err := s.store.ListCodeAnchorsForScratchpad(r.Context(), spID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	if list == nil {
		list = []*domain.CodeAnchor{}
	}
	writeJSON(w, http.StatusOK, list)
}

// listCodeAnchorsForOwner factors over the four owner-type list endpoints.
func (s *Server) listCodeAnchorsForOwner(ownerType string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ownerID := r.PathValue("id")
		if err := s.ensureOwnerExists(r, ownerType, ownerID); err != nil {
			writeErr(w, statusFor(err), err)
			return
		}
		list, err := s.store.ListCodeAnchors(r.Context(), ownerType, ownerID)
		if err != nil {
			writeErr(w, http.StatusInternalServerError, err)
			return
		}
		if list == nil {
			list = []*domain.CodeAnchor{}
		}
		writeJSON(w, http.StatusOK, list)
	}
}

// createCodeAnchorForOwner factors over the four owner-type create endpoints.
func (s *Server) createCodeAnchorForOwner(ownerType string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ownerID := r.PathValue("id")
		if err := s.ensureOwnerExists(r, ownerType, ownerID); err != nil {
			writeErr(w, statusFor(err), err)
			return
		}

		var req codeAnchorReq
		if err := readJSON(r, &req); err != nil {
			writeErr(w, http.StatusBadRequest, err)
			return
		}
		if code, msg := validateAnchorReq(&req); code != 0 {
			writeMsg(w, code, msg)
			return
		}
		provenance := req.Provenance
		if provenance == "" {
			provenance = "user-set"
		}
		now := time.Now().UTC()
		anchor := &domain.CodeAnchor{
			ID:         id.New(),
			OwnerType:  ownerType,
			OwnerID:    ownerID,
			Kind:       req.Kind,
			Path:       req.Path,
			LineStart:  req.LineStart,
			LineEnd:    req.LineEnd,
			Revision:   req.Revision,
			URL:        req.URL,
			Label:      req.Label,
			Provenance: provenance,
			CreatedAt:  now,
			UpdatedAt:  now,
		}
		if err := s.store.CreateCodeAnchor(r.Context(), anchor); err != nil {
			writeErr(w, http.StatusInternalServerError, err)
			return
		}
		// Log WHEN a code location was linked — the Log owns the timeline,
		// the Provenance tab owns the what/where (no redundancy). A file
		// anchor names the file+lines; a commit anchor names the commit.
		summary := "Code location linked"
		if anchor.Kind == "commit" && anchor.Revision != "" {
			rev := anchor.Revision
			if len(rev) > 7 {
				rev = rev[:7]
			}
			summary = "Linked commit " + rev
		} else if anchor.Path != "" {
			summary = "Code location linked: " + anchor.Path
			if anchor.LineStart > 0 {
				summary = fmt.Sprintf("%s:%d-%d", summary, anchor.LineStart, anchor.LineEnd)
			}
		}
		s.recordEvent(r.Context(), ownerType, ownerID, "anchored", summary, sourceUI)
		writeJSON(w, http.StatusCreated, anchor)
	}
}

// updateCodeAnchor replaces the mutable fields of an existing anchor.
// Kind and owner cannot change; delete and recreate if they need to.
func (s *Server) updateCodeAnchor(w http.ResponseWriter, r *http.Request) {
	anchor, err := s.store.GetCodeAnchor(r.Context(), r.PathValue("id"))
	if err != nil {
		writeErr(w, statusFor(err), err)
		return
	}
	var req codeAnchorReq
	if err := readJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	// Kind is immutable on PATCH — fall back to stored value if not supplied.
	if req.Kind == "" {
		req.Kind = anchor.Kind
	}
	if req.Kind != anchor.Kind {
		writeMsg(w, http.StatusBadRequest, "kind cannot be changed on an existing anchor; delete and recreate")
		return
	}
	if code, msg := validateAnchorReq(&req); code != 0 {
		writeMsg(w, code, msg)
		return
	}
	anchor.Path = req.Path
	anchor.LineStart = req.LineStart
	anchor.LineEnd = req.LineEnd
	anchor.Revision = req.Revision
	anchor.URL = req.URL
	anchor.Label = req.Label
	if req.Provenance != "" {
		anchor.Provenance = req.Provenance
	}
	anchor.UpdatedAt = time.Now().UTC()
	if err := s.store.UpdateCodeAnchor(r.Context(), anchor); err != nil {
		writeErr(w, statusFor(err), err)
		return
	}
	writeJSON(w, http.StatusOK, anchor)
}

func (s *Server) deleteCodeAnchor(w http.ResponseWriter, r *http.Request) {
	if err := s.store.DeleteCodeAnchor(r.Context(), r.PathValue("id")); err != nil {
		writeErr(w, statusFor(err), err)
		return
	}
	writeEmpty(w, http.StatusNoContent)
}
