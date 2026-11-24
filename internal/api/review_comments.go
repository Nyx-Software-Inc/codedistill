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
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"codedistill/internal/domain"
	"codedistill/internal/events"
	codegit "codedistill/internal/git"
	"codedistill/internal/id"
	"codedistill/internal/mcpworker"
	"codedistill/internal/storage"
)

// Review comments — the write side of code review (docs/design/code-review.md,
// slice 4). A changeset row is minted on first sight of a (base, head) pair,
// canonicalised through git so the same commit picked two ways is one row;
// comments hang off it; a comment can be promoted to a bug or todo that
// carries the code anchor and the changeset as provenance.

type ensureChangesetReq struct {
	Base string `json:"base"`
	Head string `json:"head"`
	Kind string `json:"kind"` // commit | branch | working; inferred when empty
	Ref  string `json:"ref"`
}

// ensureChangeset: POST /projects/{pid}/changesets. Returns the canonical row
// (201 when minted, 200 when it existed).
func (s *Server) ensureChangeset(w http.ResponseWriter, r *http.Request) {
	repo, ok := s.openProjectRepo(w, r)
	if !ok {
		return
	}
	var req ensureChangesetReq
	if err := readJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	cs, created, err := s.resolveChangeset(r, repo, req)
	if err != nil {
		code := gitStatus(err)
		if code == http.StatusInternalServerError {
			code = statusFor(err)
		}
		writeErr(w, code, err)
		return
	}
	status := http.StatusOK
	if created {
		status = http.StatusCreated
	}
	writeJSON(w, status, cs)
}

// resolveChangeset canonicalises the pair and finds or mints the row.
func (s *Server) resolveChangeset(r *http.Request, repo *codegit.Repo, req ensureChangesetReq) (*domain.Changeset, bool, error) {
	base, head, err := repo.ResolveChangeset(strings.TrimSpace(req.Base), strings.TrimSpace(req.Head))
	if err != nil {
		return nil, false, err
	}
	kind := req.Kind
	switch {
	case head == codegit.Working:
		kind = "working"
	case kind != "branch" && kind != "commit":
		kind = "commit"
	}
	pid := r.PathValue("pid")
	if got, err := s.store.FindChangeset(r.Context(), pid, base, head); err == nil {
		return got, false, nil
	} else if !errors.Is(err, storage.ErrNotFound) {
		return nil, false, err
	}
	cs := &domain.Changeset{
		ID: id.New(), ProjectID: pid, Kind: kind, BaseSHA: base, HeadSHA: head,
		Ref: strings.TrimSpace(req.Ref), CreatedAt: time.Now().UTC(),
	}
	if err := s.store.EnsureChangeset(r.Context(), cs); err != nil {
		return nil, false, err
	}
	return cs, true, nil
}

// changesetOf loads the changeset in the path and checks it belongs to the
// project in the path, so a comment id cannot be reached through another
// project's URL.
func (s *Server) changesetOf(w http.ResponseWriter, r *http.Request) (*domain.Changeset, bool) {
	cs, err := s.store.GetChangeset(r.Context(), r.PathValue("cid"))
	if err != nil {
		writeErr(w, statusFor(err), err)
		return nil, false
	}
	if cs.ProjectID != r.PathValue("pid") {
		writeMsg(w, http.StatusNotFound, "changeset not found in this project")
		return nil, false
	}
	return cs, true
}

// commentOf loads a comment and checks it belongs to the changeset in the path.
func (s *Server) commentOf(w http.ResponseWriter, r *http.Request, cs *domain.Changeset) (*domain.ReviewComment, bool) {
	c, err := s.store.GetReviewComment(r.Context(), r.PathValue("id"))
	if err != nil {
		writeErr(w, statusFor(err), err)
		return nil, false
	}
	if c.ChangesetID != cs.ID {
		writeMsg(w, http.StatusNotFound, "comment not found in this changeset")
		return nil, false
	}
	return c, true
}

// withAuthorNames fills AuthorName for display. The author column holds a
// user id; in single-user mode that is the local user and the name is
// whatever they set.
func (s *Server) withAuthorNames(r *http.Request, cs []domain.ReviewComment) {
	names := map[string]string{}
	for i := range cs {
		a := cs[i].Author
		if a == "" {
			continue
		}
		n, ok := names[a]
		if !ok {
			if u, err := s.store.GetUser(r.Context(), a); err == nil && u.DisplayName != "" {
				n = u.DisplayName
			} else {
				n = a
			}
			names[a] = n
		}
		cs[i].AuthorName = n
	}
}

type reviewCommentsResponse struct {
	Changeset *domain.Changeset      `json:"changeset"`
	Comments  []domain.ReviewComment `json:"comments"`
}

func (s *Server) listReviewComments(w http.ResponseWriter, r *http.Request) {
	cs, ok := s.changesetOf(w, r)
	if !ok {
		return
	}
	list, err := s.store.ListReviewComments(r.Context(), cs.ID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	s.withAuthorNames(r, list)
	writeJSON(w, http.StatusOK, reviewCommentsResponse{Changeset: cs, Comments: list})
}

type createReviewCommentReq struct {
	Path      string `json:"path"`
	Side      string `json:"side"`
	LineStart int    `json:"line_start"`
	LineEnd   int    `json:"line_end"`
	Body      string `json:"body"`
}

func validateAnchor(path, side string, lineStart, lineEnd int) (int, string) {
	if strings.TrimSpace(path) == "" {
		return http.StatusBadRequest, "path is required"
	}
	if side != "old" && side != "new" {
		return http.StatusBadRequest, `side must be "old" or "new"`
	}
	if lineStart < 1 || lineEnd < lineStart {
		return http.StatusBadRequest, "line_start must be ≥ 1 and line_end ≥ line_start"
	}
	return 0, ""
}

func (s *Server) createReviewComment(w http.ResponseWriter, r *http.Request) {
	cs, ok := s.changesetOf(w, r)
	if !ok {
		return
	}
	var req createReviewCommentReq
	if err := readJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if req.LineEnd == 0 {
		req.LineEnd = req.LineStart
	}
	if code, msg := validateAnchor(req.Path, req.Side, req.LineStart, req.LineEnd); code != 0 {
		writeMsg(w, code, msg)
		return
	}
	body := strings.TrimSpace(req.Body)
	if body == "" {
		writeMsg(w, http.StatusBadRequest, "body is required")
		return
	}
	now := time.Now().UTC()
	c := &domain.ReviewComment{
		ID: id.New(), ChangesetID: cs.ID, Path: req.Path, Side: req.Side,
		LineStart: req.LineStart, LineEnd: req.LineEnd, Body: body,
		Author: s.currentUser(r), CreatedAt: now, UpdatedAt: now,
	}
	if err := s.store.CreateReviewComment(r.Context(), c); err != nil {
		writeErr(w, statusFor(err), err)
		return
	}
	one := []domain.ReviewComment{*c}
	s.withAuthorNames(r, one)
	writeJSON(w, http.StatusCreated, one[0])
}

type updateReviewCommentReq struct {
	Body     *string `json:"body"`
	Resolved *bool   `json:"resolved"`
}

func (s *Server) updateReviewComment(w http.ResponseWriter, r *http.Request) {
	cs, ok := s.changesetOf(w, r)
	if !ok {
		return
	}
	c, ok := s.commentOf(w, r, cs)
	if !ok {
		return
	}
	var req updateReviewCommentReq
	if err := readJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	now := time.Now().UTC()
	if req.Body != nil {
		// Anyone may resolve a comment; only its author rewrites it.
		if c.Author != s.currentUser(r) {
			writeMsg(w, http.StatusForbidden, "only the comment's author can edit it")
			return
		}
		b := strings.TrimSpace(*req.Body)
		if b == "" {
			writeMsg(w, http.StatusBadRequest, "body cannot be empty")
			return
		}
		c.Body = b
	}
	if req.Resolved != nil {
		if *req.Resolved {
			if c.ResolvedAt == nil {
				c.ResolvedAt = &now
			}
		} else {
			c.ResolvedAt = nil
		}
	}
	c.UpdatedAt = now
	if err := s.store.UpdateReviewComment(r.Context(), c); err != nil {
		writeErr(w, statusFor(err), err)
		return
	}
	one := []domain.ReviewComment{*c}
	s.withAuthorNames(r, one)
	writeJSON(w, http.StatusOK, one[0])
}

func (s *Server) deleteReviewComment(w http.ResponseWriter, r *http.Request) {
	cs, ok := s.changesetOf(w, r)
	if !ok {
		return
	}
	c, ok := s.commentOf(w, r, cs)
	if !ok {
		return
	}
	// Its author, or an admin removing something that should not be there.
	if c.Author != s.currentUser(r) && !s.can(r, domain.PermAdminister) {
		writeMsg(w, http.StatusForbidden, "only the comment's author can delete it")
		return
	}
	if err := s.store.DeleteReviewComment(r.Context(), c.ID); err != nil {
		writeErr(w, statusFor(err), err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

type promoteReviewCommentReq struct {
	Type     string `json:"type"`     // "bug" | "todo"
	Severity string `json:"severity"` // bug: critical|major|minor|trivial (default minor)
	Priority string `json:"priority"` // todo: high|medium|low|none (default none)
}

type promoteReviewCommentResponse struct {
	Comment   domain.ReviewComment `json:"comment"`
	OwnerType string               `json:"owner_type"`
	OwnerID   string               `json:"owner_id"`
}

// promoteReviewComment turns a comment into a bug or todo: the item's subject
// is the comment's first line, the full body is its first log note, and a
// file anchor at head (the commit, or the working copy) ties it to the code.
// The changeset is the provenance the item is born with.
func (s *Server) promoteReviewComment(w http.ResponseWriter, r *http.Request) {
	cs, ok := s.changesetOf(w, r)
	if !ok {
		return
	}
	c, ok := s.commentOf(w, r, cs)
	if !ok {
		return
	}
	if c.PromotedOwnerID != "" {
		writeMsg(w, http.StatusConflict, "comment is already promoted")
		return
	}
	var req promoteReviewCommentReq
	if err := readJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	ctx := r.Context()
	now := time.Now().UTC()
	subject := strings.TrimSpace(strings.SplitN(c.Body, "\n", 2)[0])
	if len(subject) > 140 {
		subject = subject[:137] + "…"
	}
	var ownerType, ownerID string
	switch req.Type {
	case "bug":
		sev := req.Severity
		if sev == "" {
			sev = "minor"
		}
		if !validSeverity(sev) {
			writeMsg(w, http.StatusBadRequest, fmt.Sprintf("severity %q: invalid", sev))
			return
		}
		b := &domain.BugItem{
			ID: id.New(), ProjectID: cs.ProjectID, CreatorID: s.currentUser(r),
			Subject: subject, Severity: sev, Status: "open", Origin: "manual", CreatedAt: now,
		}
		if err := s.store.CreateBugItem(ctx, b); err != nil {
			writeErr(w, statusFor(err), err)
			return
		}
		ownerType, ownerID = ownerBugItem, b.ID
		defer s.bus.Publish(events.BugsChanged)
		defer s.notifyExport(ctx, ownerBugItem, b.ID, mcpworker.OpCreate, b)
	case "todo":
		pri := req.Priority
		if pri == "" {
			pri = "none"
		}
		if !validPriority(pri) {
			writeMsg(w, http.StatusBadRequest, fmt.Sprintf("priority %q: invalid", pri))
			return
		}
		t := &domain.TodoItem{
			ID: id.New(), ProjectID: cs.ProjectID, CreatorID: s.currentUser(r),
			Subject: subject, Priority: pri, Status: "incomplete", Origin: "manual", CreatedAt: now,
		}
		if err := s.store.CreateTodoItem(ctx, t); err != nil {
			writeErr(w, statusFor(err), err)
			return
		}
		ownerType, ownerID = ownerTodoItem, t.ID
		defer s.bus.Publish(events.TodosChanged)
		defer s.notifyExport(ctx, ownerTodoItem, t.ID, mcpworker.OpCreate, t)
	default:
		writeMsg(w, http.StatusBadRequest, `type must be "bug" or "todo"`)
		return
	}

	// Provenance: where it came from, in the log; where it points, as an
	// anchor at the head the reviewer was reading.
	s.recordEvent(ctx, ownerType, ownerID, "created",
		fmt.Sprintf("Promoted from a review comment on %s (%s)", c.Path, changesetLabel(cs)), sourceUI)
	s.recordNoteEvent(ctx, ownerType, ownerID, c.Body, sourceUI)
	rev := cs.HeadSHA
	if rev == codegit.Working {
		rev = ""
	}
	anchor := &domain.CodeAnchor{
		ID: id.New(), OwnerType: ownerType, OwnerID: ownerID, Kind: "file",
		Path: c.Path, LineStart: c.LineStart, LineEnd: c.LineEnd, Revision: rev,
		Label: "review comment", Provenance: "user-set", CreatedAt: now, UpdatedAt: now,
	}
	if err := s.store.CreateCodeAnchor(ctx, anchor); err != nil {
		s.log.Warn("review: anchor for promoted comment failed", "comment", c.ID, "err", err)
	}

	c.PromotedOwnerType, c.PromotedOwnerID = ownerType, ownerID
	c.UpdatedAt = now
	if err := s.store.UpdateReviewComment(ctx, c); err != nil {
		writeErr(w, statusFor(err), err)
		return
	}
	one := []domain.ReviewComment{*c}
	s.withAuthorNames(r, one)
	writeJSON(w, http.StatusCreated, promoteReviewCommentResponse{Comment: one[0], OwnerType: ownerType, OwnerID: ownerID})
}

func changesetLabel(cs *domain.Changeset) string {
	short := func(s string) string {
		if len(s) > 7 {
			return s[:7]
		}
		return s
	}
	switch {
	case cs.HeadSHA == codegit.Working:
		return "working copy vs " + short(cs.BaseSHA)
	case cs.Kind == "branch" && cs.Ref != "":
		return cs.Ref + " at " + short(cs.HeadSHA)
	default:
		return "commit " + short(cs.HeadSHA)
	}
}
