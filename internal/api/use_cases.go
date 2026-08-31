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
	"codedistill/internal/mcpworker"
)

type updateUseCaseReq struct {
	Subject       *string `json:"subject,omitempty"`
	Description   *string `json:"description,omitempty"`
	Role          *string `json:"role,omitempty"`
	Want          *string `json:"want,omitempty"`
	Why           *string `json:"why,omitempty"`
	Status        *string `json:"status,omitempty"`   // open | approved | in_progress | completed | rejected
	Priority      *string `json:"priority,omitempty"` // high | medium | low | none
	TargetRelease *string `json:"target_release,omitempty"`
	CommitSHA     *string `json:"commit_sha,omitempty"`
	CommitTag     *string `json:"commit_tag,omitempty"`
	// ImplementationDate is normally auto-set when status flips to "completed",
	// but the client can override (e.g. to backdate a use case implemented
	// before tracking started).
	ImplementationDate *time.Time `json:"implementation_date,omitempty"`
	DueDate            *string    `json:"due_date,omitempty"` // ISO date or RFC3339; "" clears
	Tags               *[]string  `json:"tags,omitempty"`     // work-record tags (canvas rework C3)
}

func (s *Server) listUseCases(w http.ResponseWriter, r *http.Request) {
	list, err := s.store.ListUseCaseItems(r.Context(), r.PathValue("pid"))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	if !includeDone(r) {
		list = filterOpen(list, func(u *domain.UseCaseItem) bool { return domain.IsUseCaseDone(u.Status) })
	}
	if list == nil {
		list = []*domain.UseCaseItem{}
	}
	writeJSON(w, http.StatusOK, list)
}

func (s *Server) listUseCasesByScratchpad(w http.ResponseWriter, r *http.Request) {
	sid := r.PathValue("sid")
	if _, err := s.store.GetScratchpad(r.Context(), sid); err != nil {
		writeErr(w, statusFor(err), err)
		return
	}
	list, err := s.store.ListUseCaseItemsByScratchpad(r.Context(), sid)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	if !includeDone(r) {
		list = filterOpen(list, func(u *domain.UseCaseItem) bool { return domain.IsUseCaseDone(u.Status) })
	}
	if list == nil {
		list = []*domain.UseCaseItem{}
	}
	writeJSON(w, http.StatusOK, list)
}

func (s *Server) getUseCase(w http.ResponseWriter, r *http.Request) {
	u, err := s.store.GetUseCaseItem(r.Context(), r.PathValue("id"))
	if err != nil {
		writeErr(w, statusFor(err), err)
		return
	}
	writeJSON(w, http.StatusOK, u)
}

func (s *Server) updateUseCase(w http.ResponseWriter, r *http.Request) {
	u, err := s.store.GetUseCaseItem(r.Context(), r.PathValue("id"))
	if err != nil {
		writeErr(w, statusFor(err), err)
		return
	}
	var req updateUseCaseReq
	if err := readJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}

	if req.Subject != nil {
		u.Subject = *req.Subject
	}
	if req.Description != nil {
		u.Description = *req.Description
	}
	if req.Role != nil {
		u.Role = *req.Role
	}
	if req.Want != nil {
		u.Want = *req.Want
	}
	if req.Why != nil {
		u.Why = *req.Why
	}
	if req.TargetRelease != nil {
		u.TargetRelease = *req.TargetRelease
	}
	if req.CommitSHA != nil {
		u.CommitSHA = *req.CommitSHA
	}
	if req.CommitTag != nil {
		u.CommitTag = *req.CommitTag
	}
	if req.ImplementationDate != nil {
		t := *req.ImplementationDate
		u.ImplementationDate = &t
	}

	// Status transitions drive several side-effects:
	//   - proposed → implemented: stamp implementation_date if not provided,
	//     and auto-fill commit_sha from the project's git HEAD if the client
	//     left it empty (per the L3 design — saves typing in the common
	//     "I just shipped this" flow).
	//   - implemented → proposed/abandoned: leave the historical commit_sha /
	//     implementation_date in place. They're a record of when it was
	//     marked implemented, even if status later regresses; if the user
	//     wants to clear them they can pass empty strings explicitly.
	priorUCStatus := u.Status
	if req.Priority != nil {
		if !domain.ValidPriority(*req.Priority) {
			writeMsg(w, http.StatusBadRequest,
				fmt.Sprintf("priority %q: must be high, medium, low, or none", *req.Priority))
			return
		}
		u.Priority = *req.Priority
	}
	if req.Status != nil {
		if !domain.ValidUseCaseStatus(*req.Status) {
			writeMsg(w, http.StatusBadRequest, fmt.Sprintf("status %q: must be open, approved, in_progress, completed, or rejected", *req.Status))
			return
		}
		// Governance: a verification gate can block marking a use case completed.
		if doneSuccessStatus(*req.Status) && !doneSuccessStatus(u.Status) {
			if reason := s.completionBlocked(r.Context(), ownerUseCaseItem, u.ID); reason != "" {
				writeMsg(w, http.StatusConflict, reason)
				return
			}
		}
		flippingToImplemented := *req.Status == domain.UseCaseStatusCompleted && u.Status != domain.UseCaseStatusCompleted
		u.Status = *req.Status

		if flippingToImplemented {
			if u.ImplementationDate == nil {
				now := time.Now().UTC()
				u.ImplementationDate = &now
			}
			if u.CommitSHA == "" {
				if sha, err := autoFillHeadSHA(r, s, u.ProjectID); err == nil {
					u.CommitSHA = sha
				}
				// If autoFillHeadSHA fails (no repo_root, not a git repo, no
				// HEAD yet), we silently leave commit_sha empty — the action
				// is still valid; the user can set it later.
			}
		}
	}
	if req.DueDate != nil {
		due, err := parseDueDate(*req.DueDate)
		if err != nil {
			writeMsg(w, http.StatusBadRequest, err.Error())
			return
		}
		u.DueDate = due
	}
	if req.Tags != nil {
		u.Tags = normalizeTags(*req.Tags)
	}
	u.UpdatedAt = time.Now().UTC()

	if err := s.store.UpdateUseCaseItem(r.Context(), u); err != nil {
		writeErr(w, statusFor(err), err)
		return
	}
	// Log the status change (WHEN it happened) — bugs/use-cases were the gap.
	if req.Status != nil && u.Status != priorUCStatus {
		s.recordDerivedStatus(r.Context(), "use_case_item", u.ID, u.Status, sourceUI)
	}
	s.notifyExport(r.Context(), "use_case_item", u.ID, exportOp(u.Status != priorUCStatus), u)
	s.bus.Publish(events.UseCasesChanged)
	writeJSON(w, http.StatusOK, u)
}

// claimUseCase flips status to in_progress and records the claimer.
// See claimTodo for the conflict semantics.
func (s *Server) claimUseCase(w http.ResponseWriter, r *http.Request) {
	itemID := r.PathValue("id")
	var req claimReq
	if !s.validateClaimReq(w, r, &req) {
		return
	}
	if err := s.store.ClaimUseCaseItem(r.Context(), itemID, req.ClaimedBy); err != nil {
		s.writeClaimError(w, err, func() string {
			u, gerr := s.store.GetUseCaseItem(r.Context(), itemID)
			if gerr != nil || u == nil {
				return ""
			}
			return u.ClaimedBy
		})
		return
	}
	u, err := s.store.GetUseCaseItem(r.Context(), itemID)
	if err != nil {
		writeErr(w, statusFor(err), err)
		return
	}
	s.notifyExport(r.Context(), "use_case_item", u.ID, mcpworker.OpStatusChange, u)
	s.bus.Publish(events.UseCasesChanged)
	writeJSON(w, http.StatusOK, u)
}

// reopenUseCase flips a terminated use case back to open and clears
// implementation metadata. claimed_by + claimed_at preserved.
func (s *Server) reopenUseCase(w http.ResponseWriter, r *http.Request) {
	u, err := s.store.GetUseCaseItem(r.Context(), r.PathValue("id"))
	if err != nil {
		writeErr(w, statusFor(err), err)
		return
	}
	u.Status = domain.UseCaseStatusOpen
	u.ImplementationDate = nil
	u.CommitSHA = ""
	u.CommitTag = ""
	u.UpdatedAt = time.Now().UTC()
	if err := s.store.UpdateUseCaseItem(r.Context(), u); err != nil {
		writeErr(w, statusFor(err), err)
		return
	}
	s.notifyExport(r.Context(), "use_case_item", u.ID, mcpworker.OpStatusChange, u)
	s.bus.Publish(events.UseCasesChanged)
	writeJSON(w, http.StatusOK, u)
}

func (s *Server) deleteUseCase(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	rid := s.captureRemoteID(r.Context(), "use_case_item", id)
	if err := s.store.DeleteUseCaseItem(r.Context(), id); err != nil {
		writeErr(w, statusFor(err), err)
		return
	}
	s.notifyExport(r.Context(), "use_case_item", id, mcpworker.OpDelete,
		map[string]any{"id": id, "remote_id": rid})
	s.bus.Publish(events.UseCasesChanged)
	writeEmpty(w, http.StatusNoContent)
}

// autoFillHeadSHA delegates to the shared git.HeadSHAForProject helper.
// Returns the empty string + nil error when the project has no repo
// configured — caller treats that as "user can set the SHA later".
func autoFillHeadSHA(r *http.Request, s *Server, projectID string) (string, error) {
	return git.HeadSHAForProject(r.Context(), s.store, projectID)
}
