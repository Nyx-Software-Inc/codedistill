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
	"strings"
	"time"

	"codedistill/internal/domain"
	"codedistill/internal/events"
	"codedistill/internal/git"
	"codedistill/internal/id"
	"codedistill/internal/mcpworker"
)

type createTodoReq struct {
	Subject  string `json:"subject"`
	Notes    string `json:"notes,omitempty"`
	Priority string `json:"priority,omitempty"` // high|medium|low|none, default none
}

type updateTodoReq struct {
	Subject   *string `json:"subject,omitempty"`
	Notes     *string `json:"notes,omitempty"`
	Priority  *string `json:"priority,omitempty"`
	Status    *string `json:"status,omitempty"` // incomplete|in_progress|complete|abandoned
	CommitSHA *string `json:"commit_sha,omitempty"`
	CommitTag *string `json:"commit_tag,omitempty"`
	// DueDate is an ISO date ("2026-06-30") or RFC3339 string; empty
	// string clears the due date. Omitted leaves it unchanged.
	DueDate *string `json:"due_date,omitempty"`
	// Tags on the work record (canvas rework C3); replaces the full set.
	Tags *[]string `json:"tags,omitempty"`
}

// parseDueDate accepts "" (clear), a date-only "2006-01-02", or a full
// RFC3339 timestamp. Returns (nil, nil) for the clear case.
func parseDueDate(s string) (*time.Time, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, nil
	}
	for _, layout := range []string{"2006-01-02", time.RFC3339} {
		if t, err := time.Parse(layout, s); err == nil {
			u := t.UTC()
			return &u, nil
		}
	}
	return nil, fmt.Errorf("due_date %q: want YYYY-MM-DD or RFC3339", s)
}

func validPriority(p string) bool {
	return p == "high" || p == "medium" || p == "low" || p == "none"
}

func (s *Server) listTodos(w http.ResponseWriter, r *http.Request) {
	list, err := s.store.ListTodoItems(r.Context(), r.PathValue("pid"))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	if !includeDone(r) {
		list = filterOpen(list, func(t *domain.TodoItem) bool { return domain.IsTodoDone(t.Status) })
	}
	if list == nil {
		list = []*domain.TodoItem{}
	}
	writeJSON(w, http.StatusOK, list)
}

func (s *Server) listTodosByScratchpad(w http.ResponseWriter, r *http.Request) {
	sid := r.PathValue("sid")
	if _, err := s.store.GetScratchpad(r.Context(), sid); err != nil {
		writeErr(w, statusFor(err), err)
		return
	}
	list, err := s.store.ListTodoItemsByScratchpad(r.Context(), sid)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	if !includeDone(r) {
		list = filterOpen(list, func(t *domain.TodoItem) bool { return domain.IsTodoDone(t.Status) })
	}
	if list == nil {
		list = []*domain.TodoItem{}
	}
	writeJSON(w, http.StatusOK, list)
}

func (s *Server) createTodo(w http.ResponseWriter, r *http.Request) {
	projectID := r.PathValue("pid")
	if _, err := s.store.GetProject(r.Context(), projectID); err != nil {
		writeErr(w, statusFor(err), err)
		return
	}
	var req createTodoReq
	if err := readJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if req.Subject == "" {
		writeMsg(w, http.StatusBadRequest, "subject is required")
		return
	}
	priority := req.Priority
	if priority == "" {
		priority = "none"
	}
	if !validPriority(priority) {
		writeMsg(w, http.StatusBadRequest, fmt.Sprintf("priority %q: invalid", priority))
		return
	}
	t := &domain.TodoItem{
		ID: id.New(), ProjectID: projectID,
		Subject:  req.Subject,
		Priority: priority, Status: "incomplete",
		Origin: "manual", CreatedAt: time.Now().UTC(),
	}
	if err := s.store.CreateTodoItem(r.Context(), t); err != nil {
		writeErr(w, statusFor(err), err)
		return
	}
	// Legacy `notes` in the payload becomes the item's first activity-log note
	// (the blob column is retired).
	s.recordNoteEvent(r.Context(), "todo_item", t.ID, req.Notes, sourceUI)
	s.notifyExport(r.Context(), "todo_item", t.ID, mcpworker.OpCreate, t)
	s.bus.Publish(events.TodosChanged)
	writeJSON(w, http.StatusCreated, t)
}

func (s *Server) getTodo(w http.ResponseWriter, r *http.Request) {
	t, err := s.store.GetTodoItem(r.Context(), r.PathValue("id"))
	if err != nil {
		writeErr(w, statusFor(err), err)
		return
	}
	writeJSON(w, http.StatusOK, t)
}

func (s *Server) updateTodo(w http.ResponseWriter, r *http.Request) {
	t, err := s.store.GetTodoItem(r.Context(), r.PathValue("id"))
	if err != nil {
		writeErr(w, statusFor(err), err)
		return
	}
	var req updateTodoReq
	if err := readJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	priorStatus := t.Status // for lineage (UC-5)
	if req.Subject != nil {
		t.Subject = *req.Subject
	}
	if req.Priority != nil {
		if !validPriority(*req.Priority) {
			writeMsg(w, http.StatusBadRequest, fmt.Sprintf("priority %q: invalid", *req.Priority))
			return
		}
		t.Priority = *req.Priority
	}
	// Apply explicit commit metadata edits before status logic so the
	// flip can see them (a client supplying its own SHA pre-empts the
	// auto-fill).
	if req.CommitSHA != nil {
		t.CommitSHA = *req.CommitSHA
	}
	if req.CommitTag != nil {
		t.CommitTag = *req.CommitTag
	}
	if req.DueDate != nil {
		due, err := parseDueDate(*req.DueDate)
		if err != nil {
			writeMsg(w, http.StatusBadRequest, err.Error())
			return
		}
		t.DueDate = due
	}
	if req.Tags != nil {
		t.Tags = normalizeTags(*req.Tags)
	}
	if req.Status != nil {
		if !domain.ValidTodoStatus(*req.Status) {
			writeMsg(w, http.StatusBadRequest, fmt.Sprintf("status %q: invalid", *req.Status))
			return
		}
		// Governance: a verification gate can block marking an item complete.
		if doneSuccessStatus(*req.Status) && !doneSuccessStatus(t.Status) {
			if reason := s.completionBlocked(r.Context(), ownerTodoItem, t.ID); reason != "" {
				writeMsg(w, http.StatusConflict, reason)
				return
			}
		}
		// Done = terminal (complete OR abandoned). Move INTO done stamps
		// completed_at; move OUT of done clears completed_at + commit
		// metadata. Auto-fill commit_sha only on 'complete' — abandoned
		// items have no implementation to attribute.
		wasDone := domain.IsTodoDone(t.Status)
		nowDone := domain.IsTodoDone(*req.Status)
		t.Status = *req.Status
		if nowDone && !wasDone {
			if t.CompletedAt == nil {
				now := time.Now().UTC()
				t.CompletedAt = &now
			}
			if t.Status == domain.TodoStatusComplete && t.CommitSHA == "" {
				if sha, err := git.HeadSHAForProject(r.Context(), s.store, t.ProjectID); err == nil {
					t.CommitSHA = sha
				}
			}
		}
		if !nowDone && wasDone {
			t.CompletedAt = nil
			t.CommitSHA = ""
			t.CommitTag = ""
		}
	}
	if err := s.store.UpdateTodoItem(r.Context(), t); err != nil {
		writeErr(w, statusFor(err), err)
		return
	}
	// Record the legacy `notes` append ONLY after the update validates + saves
	// (blob retired → activity log). Doing it earlier meant a PATCH that 400'd on
	// an invalid status/priority still logged its note, and the retry duplicated
	// it in the append-only log (audit M19).
	if req.Notes != nil {
		s.recordNoteEvent(r.Context(), "todo_item", t.ID, *req.Notes, sourceUI)
	}
	statusChanged := req.Status != nil && t.Status != priorStatus
	if statusChanged {
		s.recordDerivedStatus(r.Context(), "todo_item", t.ID, t.Status, sourceUI)
	}
	s.notifyExport(r.Context(), "todo_item", t.ID, exportOp(statusChanged), t)
	s.bus.Publish(events.TodosChanged)
	writeJSON(w, http.StatusOK, t)
}

// claimTodo flips status to in_progress and records the claimer.
// Body: {claimed_by: "Claude" | "Rich" | ...}. 409 if already claimed.
func (s *Server) claimTodo(w http.ResponseWriter, r *http.Request) {
	itemID := r.PathValue("id")
	var req claimReq
	if !s.validateClaimReq(w, r, &req) {
		return
	}
	if err := s.store.ClaimTodoItem(r.Context(), itemID, req.ClaimedBy); err != nil {
		s.writeClaimError(w, err, func() string {
			t, gerr := s.store.GetTodoItem(r.Context(), itemID)
			if gerr != nil || t == nil {
				return ""
			}
			return t.ClaimedBy
		})
		return
	}
	t, err := s.store.GetTodoItem(r.Context(), itemID)
	if err != nil {
		writeErr(w, statusFor(err), err)
		return
	}
	s.notifyExport(r.Context(), "todo_item", t.ID, mcpworker.OpStatusChange, t)
	s.bus.Publish(events.TodosChanged)
	writeJSON(w, http.StatusOK, t)
}

// reopenTodo flips a done todo back to incomplete and clears completion
// metadata. claimed_by + claimed_at are preserved as historical record.
func (s *Server) reopenTodo(w http.ResponseWriter, r *http.Request) {
	t, err := s.store.GetTodoItem(r.Context(), r.PathValue("id"))
	if err != nil {
		writeErr(w, statusFor(err), err)
		return
	}
	t.Status = domain.TodoStatusIncomplete
	t.CompletedAt = nil
	t.CommitSHA = ""
	t.CommitTag = ""
	if err := s.store.UpdateTodoItem(r.Context(), t); err != nil {
		writeErr(w, statusFor(err), err)
		return
	}
	s.notifyExport(r.Context(), "todo_item", t.ID, mcpworker.OpStatusChange, t)
	s.bus.Publish(events.TodosChanged)
	writeJSON(w, http.StatusOK, t)
}

func (s *Server) deleteTodo(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	rid := s.captureRemoteID(r.Context(), "todo_item", id)
	if err := s.store.DeleteTodoItem(r.Context(), id); err != nil {
		writeErr(w, statusFor(err), err)
		return
	}
	s.notifyExport(r.Context(), "todo_item", id, mcpworker.OpDelete,
		map[string]any{"id": id, "remote_id": rid})
	s.bus.Publish(events.TodosChanged)
	writeEmpty(w, http.StatusNoContent)
}
