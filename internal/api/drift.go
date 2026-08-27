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
	"time"

	"codedistill/internal/git"
)

// Drift detection (glass-box Phase 5, slice 5.1). The lineage-health view: where
// intent and code have come apart. Three signals —
//   - untraced code: commits since you started recording provenance that aren't
//     tied to any item (the #1 AI-dev smell — code with no intent);
//   - unbuilt intent: items marked done with no implementation recorded;
//   - diverged implementation: items that are built but whose verification is red.
// Advisory — it surfaces, it doesn't act.

const driftCommitLimit = 300

type driftCommit struct {
	SHA      string    `json:"sha"`
	ShortSHA string    `json:"short_sha"`
	Subject  string    `json:"subject"`
	Author   string    `json:"author"`
	Date     time.Time `json:"date"`
}

type driftItem struct {
	OwnerType string `json:"owner_type"`
	ID        string `json:"id"`
	Number    int    `json:"number"`
	Subject   string `json:"subject"`
	Status    string `json:"status"`
	Reason    string `json:"reason"`
}

type driftResp struct {
	Untraced       []driftCommit `json:"untraced"`
	Unbuilt        []driftItem   `json:"unbuilt"`
	Diverged       []driftItem   `json:"diverged"`
	RepoConfigured bool          `json:"repo_configured"`
	CommitsScanned int           `json:"commits_scanned"`
}

// doneSuccessStatus reports a terminal *success* state — the item is considered
// shipped. Abandoned/rejected/deprecated states are excluded: those aren't
// "unbuilt intent", they're intentionally-dropped intent.
func doneSuccessStatus(status string) bool {
	switch status {
	case "completed", "complete", "done", "fixed", "verified", "resolved", "closed", "shipped", "implemented":
		return true
	}
	return false
}

func (s *Server) drift(w http.ResponseWriter, r *http.Request) {
	pid := r.PathValue("id")
	proj, err := s.store.GetProject(r.Context(), pid)
	if err != nil {
		writeErr(w, statusFor(err), err)
		return
	}
	resp := driftResp{Untraced: []driftCommit{}, Unbuilt: []driftItem{}, Diverged: []driftItem{}}

	recordedRevs := map[string]bool{} // raw commit-anchor revisions across the project

	assess := func(ownerType, id string, number int, subject, status string) {
		anchors, _ := s.store.ListCodeAnchors(r.Context(), ownerType, id)
		hasCommit := false
		for _, a := range anchors {
			if a.Kind == "commit" && a.Revision != "" {
				recordedRevs[a.Revision] = true
				hasCommit = true
			}
		}
		switch {
		case doneSuccessStatus(status) && !hasCommit:
			resp.Unbuilt = append(resp.Unbuilt, driftItem{
				OwnerType: ownerType, ID: id, Number: number, Subject: subject, Status: status,
				Reason: "marked done, but no implementation was recorded",
			})
		case hasCommit:
			if reason := s.divergenceReason(r.Context(), ownerType, id); reason != "" {
				resp.Diverged = append(resp.Diverged, driftItem{
					OwnerType: ownerType, ID: id, Number: number, Subject: subject, Status: status, Reason: reason,
				})
			}
		}
	}

	if todos, err := s.store.ListTodoItems(r.Context(), pid); err == nil {
		for _, it := range todos {
			assess(ownerTodoItem, it.ID, it.Number, it.Subject, it.Status)
		}
	}
	if bugs, err := s.store.ListBugItems(r.Context(), pid); err == nil {
		for _, it := range bugs {
			assess(ownerBugItem, it.ID, it.Number, it.Subject, it.Status)
		}
	}
	if ucs, err := s.store.ListUseCaseItems(r.Context(), pid); err == nil {
		for _, it := range ucs {
			assess(ownerUseCaseItem, it.ID, it.Number, it.Subject, it.Status)
		}
	}
	// KB entries can carry commit anchors too — count them toward the recorded
	// set so KB-linked commits aren't flagged untraced, but don't build/diverge them.
	if kb, err := s.store.ListKnowledgeEntries(r.Context(), pid); err == nil {
		for _, e := range kb {
			if anchors, err := s.store.ListCodeAnchors(r.Context(), ownerKnowledgeEntry, e.ID); err == nil {
				for _, a := range anchors {
					if a.Kind == "commit" && a.Revision != "" {
						recordedRevs[a.Revision] = true
					}
				}
			}
		}
	}

	// Untraced: commits since the earliest recorded provenance that aren't tied
	// to any item. Needs a repo + at least one recorded commit to set a baseline.
	if proj.RepoRoot != "" && len(recordedRevs) > 0 {
		if repo, err := git.Open(proj.RepoRoot); err == nil {
			resp.RepoConfigured = true
			recordedSHAs := map[string]bool{}
			var baseline time.Time
			for rev := range recordedRevs {
				sha, when, ok := repo.ResolveCommit(rev)
				if !ok {
					continue
				}
				recordedSHAs[sha] = true
				if baseline.IsZero() || when.Before(baseline) {
					baseline = when
				}
			}
			if !baseline.IsZero() {
				// since is exclusive; nudge back a second so the baseline commit
				// itself is considered (it's recorded, so it filters out anyway).
				since := baseline.Add(-time.Second)
				commits, _ := repo.WalkCommitsSince(&since, driftCommitLimit)
				resp.CommitsScanned = len(commits)
				for _, c := range commits {
					if c.IsMerge || recordedSHAs[c.SHA] {
						continue
					}
					resp.Untraced = append(resp.Untraced, driftCommit{
						SHA: c.SHA, ShortSHA: c.ShortSHA, Subject: c.Subject, Author: c.Author, Date: c.Date,
					})
				}
			}
		}
	}

	writeJSON(w, http.StatusOK, resp)
}

// divergenceReason returns a non-empty reason when an implemented item has
// drifted from its contract: verification failing/refuted, or a criterion that
// verification marked failed.
func (s *Server) divergenceReason(ctx context.Context, ownerType, id string) string {
	_, failing, refuted := s.verificationSummary(ctx, ownerType, id)
	if failing {
		return "verification is failing on the recorded implementation"
	}
	if refuted {
		return "AI review refuted a criterion against the implementation"
	}
	if crits, err := s.store.ListAcceptanceCriteria(ctx, ownerType, id); err == nil {
		for _, c := range crits {
			if c.State == "failed" {
				return "an acceptance criterion is marked failed"
			}
		}
	}
	return ""
}
