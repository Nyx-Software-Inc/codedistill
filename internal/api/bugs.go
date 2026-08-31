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
	"time"

	"codedistill/internal/domain"
	"codedistill/internal/events"
	"codedistill/internal/git"
	"codedistill/internal/id"
	"codedistill/internal/mcpworker"
)

type createBugReq struct {
	Subject           string `json:"subject"`
	Notes             string `json:"notes,omitempty"`
	Severity          string `json:"severity,omitempty"` // critical|major|minor|trivial, default minor
	StepsToReproduce  string `json:"steps_to_reproduce,omitempty"`
	ExpectedBehavior  string `json:"expected_behavior,omitempty"`
	ActualBehavior    string `json:"actual_behavior,omitempty"`
	Environment       string `json:"environment,omitempty"`
	AffectedComponent string `json:"affected_component,omitempty"`
}

type updateBugReq struct {
	Subject           *string   `json:"subject,omitempty"`
	Notes             *string   `json:"notes,omitempty"`
	Severity          *string   `json:"severity,omitempty"`
	Status            *string   `json:"status,omitempty"`
	StepsToReproduce  *string   `json:"steps_to_reproduce,omitempty"`
	ExpectedBehavior  *string   `json:"expected_behavior,omitempty"`
	ActualBehavior    *string   `json:"actual_behavior,omitempty"`
	Environment       *string   `json:"environment,omitempty"`
	AffectedComponent *string   `json:"affected_component,omitempty"`
	CommitSHA         *string   `json:"commit_sha,omitempty"`
	CommitTag         *string   `json:"commit_tag,omitempty"`
	DueDate           *string   `json:"due_date,omitempty"` // ISO date or RFC3339; "" clears
	Tags              *[]string `json:"tags,omitempty"`     // work-record tags (canvas rework C3)
}

// isTerminalBugStatus reports whether a status represents any closed-out
// state — fix-type (fixed/verified/closed) or non-fix-type
// (not_a_bug/wont_fix/duplicate). Drives completed_at stamping on
// transition. commit_sha auto-fill is gated separately on
// isFixTerminalBugStatus.
func isTerminalBugStatus(s string) bool {
	return isFixTerminalBugStatus(s) ||
		s == "not_a_bug" || s == "wont_fix" || s == "duplicate"
}

func validSeverity(s string) bool {
	return s == "critical" || s == "major" || s == "minor" || s == "trivial"
}

func validBugStatus(s string) bool {
	switch s {
	// Active states.
	case "open", "investigating", "in-progress":
		return true
	// Fix-type terminals — auto-fill commit_sha on entry.
	case "fixed", "verified", "closed":
		return true
	// Non-fix terminals — leave the active queue without attaching a commit.
	case "not_a_bug", "wont_fix", "duplicate":
		return true
	}
	return false
}

// isFixTerminalBugStatus reports whether a status represents an
// actual code fix (vs. a non-fix close like "not a bug"). Only fix-type
// terminals get commit_sha auto-filled from project HEAD — attaching a
// HEAD SHA to "not a bug" would be misleading.
func isFixTerminalBugStatus(s string) bool {
	return s == "fixed" || s == "verified" || s == "closed"
}

func (s *Server) listBugs(w http.ResponseWriter, r *http.Request) {
	list, err := s.store.ListBugItems(r.Context(), r.PathValue("pid"))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	if !includeDone(r) {
		list = filterOpen(list, func(b *domain.BugItem) bool { return isTerminalBugStatus(b.Status) })
	}
	if list == nil {
		list = []*domain.BugItem{}
	}
	writeJSON(w, http.StatusOK, list)
}

func (s *Server) listBugsByScratchpad(w http.ResponseWriter, r *http.Request) {
	sid := r.PathValue("sid")
	if _, err := s.store.GetScratchpad(r.Context(), sid); err != nil {
		writeErr(w, statusFor(err), err)
		return
	}
	list, err := s.store.ListBugItemsByScratchpad(r.Context(), sid)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	if !includeDone(r) {
		list = filterOpen(list, func(b *domain.BugItem) bool { return isTerminalBugStatus(b.Status) })
	}
	if list == nil {
		list = []*domain.BugItem{}
	}
	writeJSON(w, http.StatusOK, list)
}

func (s *Server) createBug(w http.ResponseWriter, r *http.Request) {
	projectID := r.PathValue("pid")
	if _, err := s.store.GetProject(r.Context(), projectID); err != nil {
		writeErr(w, statusFor(err), err)
		return
	}
	var req createBugReq
	if err := readJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if req.Subject == "" {
		writeMsg(w, http.StatusBadRequest, "subject is required")
		return
	}
	sev := req.Severity
	if sev == "" {
		sev = "minor"
	}
	if !validSeverity(sev) {
		writeMsg(w, http.StatusBadRequest, fmt.Sprintf("severity %q: invalid", sev))
		return
	}
	b := &domain.BugItem{
		ID: id.New(), ProjectID: projectID,
		Subject:  req.Subject,
		Severity: sev, Status: "open",
		StepsToReproduce:  req.StepsToReproduce,
		ExpectedBehavior:  req.ExpectedBehavior,
		ActualBehavior:    req.ActualBehavior,
		Environment:       req.Environment,
		AffectedComponent: req.AffectedComponent,
		Origin:            "manual",
		CreatedAt:         time.Now().UTC(),
	}
	if err := s.store.CreateBugItem(r.Context(), b); err != nil {
		writeErr(w, statusFor(err), err)
		return
	}
	// Legacy `notes` in the payload becomes the item's first activity-log note
	// (the blob column is retired).
	s.recordNoteEvent(r.Context(), "bug_item", b.ID, req.Notes, sourceUI)
	s.notifyExport(r.Context(), "bug_item", b.ID, mcpworker.OpCreate, b)
	s.bus.Publish(events.BugsChanged)
	writeJSON(w, http.StatusCreated, b)
}

func (s *Server) getBug(w http.ResponseWriter, r *http.Request) {
	b, err := s.store.GetBugItem(r.Context(), r.PathValue("id"))
	if err != nil {
		writeErr(w, statusFor(err), err)
		return
	}
	writeJSON(w, http.StatusOK, b)
}

func (s *Server) updateBug(w http.ResponseWriter, r *http.Request) {
	b, err := s.store.GetBugItem(r.Context(), r.PathValue("id"))
	if err != nil {
		writeErr(w, statusFor(err), err)
		return
	}
	var req updateBugReq
	if err := readJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if req.Subject != nil {
		b.Subject = *req.Subject
	}
	if req.Severity != nil {
		if !validSeverity(*req.Severity) {
			writeMsg(w, http.StatusBadRequest, fmt.Sprintf("severity %q: invalid", *req.Severity))
			return
		}
		b.Severity = *req.Severity
	}
	// Apply explicit commit metadata edits before status logic — a
	// client supplying its own SHA pre-empts the auto-fill.
	if req.CommitSHA != nil {
		b.CommitSHA = *req.CommitSHA
	}
	if req.CommitTag != nil {
		b.CommitTag = *req.CommitTag
	}
	priorBugStatus := b.Status
	if req.Status != nil {
		if !validBugStatus(*req.Status) {
			writeMsg(w, http.StatusBadRequest, fmt.Sprintf("status %q: invalid", *req.Status))
			return
		}
		// Governance: a verification gate can block marking a bug fixed.
		if doneSuccessStatus(*req.Status) && !doneSuccessStatus(b.Status) {
			if reason := s.completionBlocked(r.Context(), ownerBugItem, b.ID); reason != "" {
				writeMsg(w, http.StatusConflict, reason)
				return
			}
		}
		// No transition gate — solo-dev workflow needs to flow freely:
		// re-open a fixed bug, jump directly from open to "not a bug",
		// etc. The matcher confirm flow already bypassed the old
		// strict-forward rule (see implementation_matches.go); this
		// brings the regular update handler in line.
		flippingTerminal := isTerminalBugStatus(*req.Status) && !isTerminalBugStatus(b.Status)
		flippingNonTerminal := !isTerminalBugStatus(*req.Status) && isTerminalBugStatus(b.Status)
		b.Status = *req.Status
		if flippingTerminal {
			if b.CompletedAt == nil {
				now := time.Now().UTC()
				b.CompletedAt = &now
			}
			// Only attach a commit to fix-type terminals. "Not a bug"
			// or "duplicate" shouldn't claim a HEAD SHA.
			if isFixTerminalBugStatus(*req.Status) && b.CommitSHA == "" {
				if sha, err := git.HeadSHAForProject(r.Context(), s.store, b.ProjectID); err == nil {
					b.CommitSHA = sha
				}
			}
		}
		if flippingNonTerminal {
			// Reopen ("fixed → open" etc.): clear completion metadata
			// so the bug looks fresh in the active queue. Mirrors the
			// todo reopen path. The user can re-trigger auto-fill by
			// flipping to a terminal again.
			b.CompletedAt = nil
			b.CommitSHA = ""
			b.CommitTag = ""
		}
	}
	if req.StepsToReproduce != nil {
		b.StepsToReproduce = *req.StepsToReproduce
	}
	if req.ExpectedBehavior != nil {
		b.ExpectedBehavior = *req.ExpectedBehavior
	}
	if req.ActualBehavior != nil {
		b.ActualBehavior = *req.ActualBehavior
	}
	if req.Environment != nil {
		b.Environment = *req.Environment
	}
	if req.AffectedComponent != nil {
		b.AffectedComponent = *req.AffectedComponent
	}
	if req.DueDate != nil {
		due, err := parseDueDate(*req.DueDate)
		if err != nil {
			writeMsg(w, http.StatusBadRequest, err.Error())
			return
		}
		b.DueDate = due
	}
	if req.Tags != nil {
		b.Tags = normalizeTags(*req.Tags)
	}
	if err := s.store.UpdateBugItem(r.Context(), b); err != nil {
		writeErr(w, statusFor(err), err)
		return
	}
	// Record the legacy `notes` append ONLY after the update validates + saves
	// (blob retired → activity log). Earlier, a PATCH that 400'd on an invalid
	// severity/status still logged its note, and the retry duplicated it in the
	// append-only log (audit M19).
	if req.Notes != nil {
		s.recordNoteEvent(r.Context(), "bug_item", b.ID, *req.Notes, sourceUI)
	}
	// Narrate the status change into the Log so the timeline says WHEN a bug
	// was fixed/reopened — the todo path already does this; bugs/use-cases
	// were the gap (matches the "loop speaks" discipline).
	if req.Status != nil && b.Status != priorBugStatus {
		s.recordDerivedStatus(r.Context(), "bug_item", b.ID, b.Status, sourceUI)
	}
	s.notifyExport(r.Context(), "bug_item", b.ID, exportOp(b.Status != priorBugStatus), b)
	s.bus.Publish(events.BugsChanged)
	writeJSON(w, http.StatusOK, b)
}

// claimBug flips status to 'in-progress' (legacy hyphen) and records
// the claimer. See claimTodo for the conflict semantics.
func (s *Server) claimBug(w http.ResponseWriter, r *http.Request) {
	itemID := r.PathValue("id")
	var req claimReq
	if !s.validateClaimReq(w, r, &req) {
		return
	}
	if err := s.store.ClaimBugItem(r.Context(), itemID, req.ClaimedBy); err != nil {
		s.writeClaimError(w, err, func() string {
			b, gerr := s.store.GetBugItem(r.Context(), itemID)
			if gerr != nil || b == nil {
				return ""
			}
			return b.ClaimedBy
		})
		return
	}
	b, err := s.store.GetBugItem(r.Context(), itemID)
	if err != nil {
		writeErr(w, statusFor(err), err)
		return
	}
	s.notifyExport(r.Context(), "bug_item", b.ID, mcpworker.OpStatusChange, b)
	s.bus.Publish(events.BugsChanged)
	writeJSON(w, http.StatusOK, b)
}

// reopenBug flips a closed-out bug back to 'open' and clears completion
// metadata. claimed_by + claimed_at are preserved as historical record.
func (s *Server) reopenBug(w http.ResponseWriter, r *http.Request) {
	b, err := s.store.GetBugItem(r.Context(), r.PathValue("id"))
	if err != nil {
		writeErr(w, statusFor(err), err)
		return
	}
	b.Status = "open"
	b.CompletedAt = nil
	b.CommitSHA = ""
	b.CommitTag = ""
	if err := s.store.UpdateBugItem(r.Context(), b); err != nil {
		writeErr(w, statusFor(err), err)
		return
	}
	s.notifyExport(r.Context(), "bug_item", b.ID, mcpworker.OpStatusChange, b)
	s.bus.Publish(events.BugsChanged)
	writeJSON(w, http.StatusOK, b)
}

func (s *Server) deleteBug(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	rid := s.captureRemoteID(r.Context(), "bug_item", id)
	if err := s.store.DeleteBugItem(r.Context(), id); err != nil {
		writeErr(w, statusFor(err), err)
		return
	}
	s.notifyExport(r.Context(), "bug_item", id, mcpworker.OpDelete,
		map[string]any{"id": id, "remote_id": rid})
	s.bus.Publish(events.BugsChanged)
	writeEmpty(w, http.StatusNoContent)
}
