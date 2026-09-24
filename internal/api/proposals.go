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
	"strconv"
	"strings"
	"time"

	"codedistill/internal/decompose"
	"codedistill/internal/domain"
	"codedistill/internal/id"
)

// Reviewing what a decomposition proposed.
//
// A run produces PROPOSALS, never items. Turning one into work is a human act,
// and until this existed the only way to perform it was the CLI's -accept — so
// the UI could start a forty-minute job and then had nowhere to put the answer.
//
// Every verdict goes through the store's DecideProposal, which only moves a
// PENDING proposal. A double-click on a slow review screen is therefore a no-op
// rather than a second item.

// listDecomposeRuns is the review queue: which documents have been read, and
// how much of each is still waiting on a person.
func (s *Server) listDecomposeRuns(w http.ResponseWriter, r *http.Request) {
	projectID := r.URL.Query().Get("project_id")
	if projectID == "" {
		writeMsg(w, http.StatusBadRequest, "project_id is required")
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	runs, err := s.store.ListDecomposeRuns(r.Context(), projectID, limit)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	if runs == nil {
		runs = []*domain.DecomposeRunSummary{}
	}
	writeJSON(w, http.StatusOK, runs)
}

// getDecomposeRun returns one run with its proposals AND the document they cite.
//
// The source lines ride along deliberately. A proposal's whole claim to
// legitimacy is that it points at lines of a real document, and a citation the
// reviewer cannot read is indistinguishable from one that was invented — which
// is the failure the whole four-layer design exists to make impossible. Sending
// the text with the proposals means the review screen can show the evidence
// without a second round trip per card.
func (s *Server) getDecomposeRun(w http.ResponseWriter, r *http.Request) {
	jobID := r.PathValue("jobID")
	run, err := s.store.GetDecomposeRun(r.Context(), jobID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	if run == nil {
		writeMsg(w, http.StatusNotFound, "no such run")
		return
	}

	// Every status, not just pending: a reviewer needs to see what they already
	// decided, and a rejected proposal with its reason is the record of a
	// judgement rather than something to hide.
	props, err := s.store.ListProposals(r.Context(), jobID, nil)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	if props == nil {
		props = []*domain.DecomposeProposal{}
	}

	out := map[string]any{"run": run, "proposals": props}

	// The document, if it is still there. A deleted source is not an error —
	// the proposals remain valid work — but the citations become unreadable,
	// and the UI should say so rather than render blank quotes.
	if item, err := s.store.GetScratchpadItem(r.Context(), run.SourceItemID); err == nil && item != nil {
		if src, _, err := s.documentFromItem(r.Context(), item); err == nil {
			out["source_lines"] = src.Lines
		}
	}
	writeJSON(w, http.StatusOK, out)
}

// targetScratchpad answers "where does the derived work go" for a run.
//
// Read back from the parameters the run was SUBMITTED with, not from the
// workflow definition: the definition can be edited afterwards, and a run's
// answer should not change under it. Falls back to the document's own
// scratchpad, which is what someone who never set it expects.
func (s *Server) targetScratchpad(ctx context.Context, run *domain.DecomposeRun) (string, error) {
	if params, err := s.store.JobParams(ctx, run.JobID); err == nil {
		if sp := strings.TrimSpace(params["target_scratchpad"]); sp != "" {
			return sp, nil
		}
	}
	item, err := s.store.GetScratchpadItem(ctx, run.SourceItemID)
	if err != nil || item == nil {
		return "", fmt.Errorf("cannot tell which scratchpad to put the work in — the source document is gone")
	}
	return item.ScratchpadID, nil
}

// decideProposal records one verdict: accept, reject, or link to existing work.
func (s *Server) decideProposal(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Status string `json:"status"` // accepted | rejected | linked
		ItemID string `json:"item_id"`
		Reason string `json:"reason"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}

	p, err := s.proposal(r, r.PathValue("id"))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	if p == nil {
		writeMsg(w, http.StatusNotFound, "no such proposal")
		return
	}
	if p.Status != domain.ProposalPending {
		// Already decided. Not an error: a stale screen or a double-click
		// should not read as a failure, and the current state is the answer.
		writeJSON(w, http.StatusOK, map[string]any{
			"status": p.Status, "item_id": firstNonEmpty(p.CreatedItemID, p.LinkedItemID),
			"note": "already decided",
		})
		return
	}

	run, err := s.store.GetDecomposeRun(r.Context(), p.JobID)
	if err != nil || run == nil {
		writeMsg(w, http.StatusConflict, "the run this proposal belongs to is gone")
		return
	}
	userID := s.currentUser(r)
	now := time.Now().UTC()

	switch req.Status {
	case domain.ProposalAccepted:
		// The item is created by the same code the CLI uses, so an item made
		// from the review screen and one made by -accept are the same thing —
		// including the anchor back to the lines it came from.
		// Accept records the verdict itself, attributed to this user. A second
		// DecideProposal here would be a no-op against an already-decided row —
		// which is precisely how the missing attribution went unnoticed.
		pad, err := s.targetScratchpad(r.Context(), run)
		if err != nil {
			writeErr(w, http.StatusConflict, err)
			return
		}
		itemID, err := decompose.Accept(r.Context(), s.store, p, pad, userID, id.New, now)
		if err != nil {
			writeErr(w, http.StatusInternalServerError, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"status": domain.ProposalAccepted, "item_id": itemID})

	case domain.ProposalRejected:
		if err := s.store.DecideProposal(r.Context(), p.ID, domain.ProposalRejected, "", strings.TrimSpace(req.Reason), userID, now); err != nil {
			writeErr(w, http.StatusBadRequest, err)
			return
		}
		// The reason is what makes a suppressed proposal reviewable when the
		// same document is read again, rather than silently re-proposed.
		writeJSON(w, http.StatusOK, map[string]any{"status": domain.ProposalRejected})

	case domain.ProposalLinked:
		if req.ItemID == "" {
			writeJSON(w, http.StatusBadRequest,
				map[string]any{"error": "linking needs the existing item to link to", "field": "item_id"})
			return
		}
		if err := s.store.DecideProposal(r.Context(), p.ID, domain.ProposalLinked, req.ItemID, "", userID, now); err != nil {
			writeErr(w, http.StatusBadRequest, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"status": domain.ProposalLinked, "item_id": req.ItemID})

	default:
		writeMsg(w, http.StatusBadRequest, "status must be accepted, rejected or linked")
	}
}

// acceptAllProposals turns every pending proposal in a run into an item.
//
// Failures are reported per proposal and do NOT stop the rest: a bulk accept
// that aborts halfway leaves the reviewer with no idea which half landed. The
// response says exactly what happened to each one that did not.
func (s *Server) acceptAllProposals(w http.ResponseWriter, r *http.Request) {
	jobID := r.PathValue("jobID")
	run, err := s.store.GetDecomposeRun(r.Context(), jobID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	if run == nil {
		writeMsg(w, http.StatusNotFound, "no such run")
		return
	}
	pending, err := s.store.ListProposals(r.Context(), jobID, []string{domain.ProposalPending})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}

	pad, err := s.targetScratchpad(r.Context(), run)
	if err != nil {
		writeErr(w, http.StatusConflict, err)
		return
	}
	userID := s.currentUser(r)
	created := 0
	failures := []map[string]string{}
	for _, p := range pending {
		now := time.Now().UTC()
		if _, err := decompose.Accept(r.Context(), s.store, p, pad, userID, id.New, now); err != nil {
			failures = append(failures, map[string]string{"id": p.ID, "subject": p.Subject, "error": err.Error()})
			continue
		}
		created++
	}
	writeJSON(w, http.StatusOK, map[string]any{"created": created, "failed": failures})
}

// decideBatch applies a whole review in one call.
//
// The review screen is a batch: a reviewer works through forty proposals,
// ticking what to create, and then commits. Forty separate requests would make
// a partial failure invisible — some landed, some did not, and the screen has no
// coherent state to show. One call returns a verdict per proposal.
//
// Failures do not stop the batch, for the same reason: a bulk operation that
// aborts halfway leaves nobody knowing which half happened.
func (s *Server) decideBatch(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Decisions []struct {
			ID     string `json:"id"`
			Status string `json:"status"`
			ItemID string `json:"item_id"`
			Reason string `json:"reason"`
		} `json:"decisions"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	jobID := r.PathValue("jobID")
	run, err := s.store.GetDecomposeRun(r.Context(), jobID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	if run == nil {
		writeMsg(w, http.StatusNotFound, "no such run")
		return
	}
	props, err := s.store.ListProposals(r.Context(), jobID, nil)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	byID := make(map[string]*domain.DecomposeProposal, len(props))
	for _, p := range props {
		byID[p.ID] = p
	}

	pad, err := s.targetScratchpad(r.Context(), run)
	if err != nil {
		writeErr(w, http.StatusConflict, err)
		return
	}
	userID := s.currentUser(r)
	accepted, rejected, linked := 0, 0, 0
	failures := []map[string]string{}
	items := map[string]string{}

	for _, d := range req.Decisions {
		p := byID[d.ID]
		if p == nil {
			failures = append(failures, map[string]string{"id": d.ID, "error": "not part of this run"})
			continue
		}
		if p.Status != domain.ProposalPending {
			// Already decided elsewhere. Skipped silently: it is the state the
			// caller wanted, reached by another route.
			continue
		}
		now := time.Now().UTC()
		switch d.Status {
		case domain.ProposalAccepted:
			itemID, err := decompose.Accept(r.Context(), s.store, p, pad, userID, id.New, now)
			if err != nil {
				failures = append(failures, map[string]string{"id": d.ID, "subject": p.Subject, "error": err.Error()})
				continue
			}
			items[d.ID] = itemID
			accepted++
		case domain.ProposalRejected:
			if err := s.store.DecideProposal(r.Context(), p.ID, domain.ProposalRejected, "", strings.TrimSpace(d.Reason), userID, now); err != nil {
				failures = append(failures, map[string]string{"id": d.ID, "subject": p.Subject, "error": err.Error()})
				continue
			}
			rejected++
		case domain.ProposalLinked:
			if d.ItemID == "" {
				failures = append(failures, map[string]string{"id": d.ID, "subject": p.Subject, "error": "linking needs an item to link to"})
				continue
			}
			if err := s.store.DecideProposal(r.Context(), p.ID, domain.ProposalLinked, d.ItemID, "", userID, now); err != nil {
				failures = append(failures, map[string]string{"id": d.ID, "subject": p.Subject, "error": err.Error()})
				continue
			}
			items[d.ID] = d.ItemID
			linked++
		default:
			failures = append(failures, map[string]string{"id": d.ID, "error": "status must be accepted, rejected or linked"})
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"accepted": accepted, "rejected": rejected, "linked": linked,
		"items": items, "failed": failures,
	})
}

// proposal fetches one by id. ListProposals is keyed by job, so this walks the
// run's proposals — a review screen holds tens, not thousands, and a dedicated
// single-row query would be a second place the column list lives.
func (s *Server) proposal(r *http.Request, propID string) (*domain.DecomposeProposal, error) {
	jobID := r.URL.Query().Get("job_id")
	if jobID == "" {
		// Not supplied: find it the only way available without a new query.
		runs, err := s.store.ListDecomposeRuns(r.Context(), r.URL.Query().Get("project_id"), 200)
		if err != nil {
			return nil, err
		}
		for _, run := range runs {
			props, err := s.store.ListProposals(r.Context(), run.JobID, nil)
			if err != nil {
				return nil, err
			}
			for _, p := range props {
				if p.ID == propID {
					return p, nil
				}
			}
		}
		return nil, nil
	}
	props, err := s.store.ListProposals(r.Context(), jobID, nil)
	if err != nil {
		return nil, err
	}
	for _, p := range props {
		if p.ID == propID {
			return p, nil
		}
	}
	return nil, nil
}
