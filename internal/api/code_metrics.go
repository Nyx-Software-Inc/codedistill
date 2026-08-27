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

	"codedistill/internal/git"
)

// Code metrics (UC-100): per-item churn + change-complexity, derived from
// the commit anchors on an item and the project's git repo. A free read —
// no feature gate — surfaced on the Throughline so a human can see "how
// much code did this intent cost, and how risky is the change to review."

type commitMetricRow struct {
	git.CommitMetrics
	Label string `json:"label,omitempty"` // the commit anchor's label, if any
}

type metricsTotals struct {
	Commits         int    `json:"commits"`
	FilesChanged    int    `json:"files_changed"` // unique files across all commits
	Added           int    `json:"added"`
	Deleted         int    `json:"deleted"`
	Net             int    `json:"net"`
	Hunks           int    `json:"hunks"`
	ExcludedFiles   int    `json:"excluded_files"`
	Complexity      string `json:"complexity"`
	ComplexityScore int    `json:"complexity_score"`
}

type codeMetricsResp struct {
	RepoConfigured bool              `json:"repo_configured"`
	Commits        []commitMetricRow `json:"commits"`
	Totals         metricsTotals     `json:"totals"`
}

// ownerProjectID resolves the project that owns an item, across owner types.
func (s *Server) ownerProjectID(ctx context.Context, ownerType, ownerID string) (string, error) {
	switch ownerType {
	case ownerTodoItem:
		it, err := s.store.GetTodoItem(ctx, ownerID)
		if err != nil {
			return "", err
		}
		return it.ProjectID, nil
	case ownerBugItem:
		it, err := s.store.GetBugItem(ctx, ownerID)
		if err != nil {
			return "", err
		}
		return it.ProjectID, nil
	case ownerUseCaseItem:
		it, err := s.store.GetUseCaseItem(ctx, ownerID)
		if err != nil {
			return "", err
		}
		return it.ProjectID, nil
	case ownerKnowledgeEntry:
		it, err := s.store.GetKnowledgeEntry(ctx, ownerID)
		if err != nil {
			return "", err
		}
		return it.ProjectID, nil
	case ownerScratchpadItem:
		it, err := s.store.GetScratchpadItem(ctx, ownerID)
		if err != nil {
			return "", err
		}
		sp, err := s.store.GetScratchpad(ctx, it.ScratchpadID)
		if err != nil {
			return "", err
		}
		return sp.ProjectID, nil
	default:
		return "", fmt.Errorf("unknown owner_type %q", ownerType)
	}
}

// codeMetricsForOwner factors over the owner-type metrics endpoints.
func (s *Server) codeMetricsForOwner(ownerType string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ownerID := r.PathValue("id")
		if err := s.ensureOwnerExists(r, ownerType, ownerID); err != nil {
			writeErr(w, statusFor(err), err)
			return
		}
		pid, err := s.ownerProjectID(r.Context(), ownerType, ownerID)
		if err != nil {
			writeErr(w, statusFor(err), err)
			return
		}
		proj, err := s.store.GetProject(r.Context(), pid)
		if err != nil {
			writeErr(w, statusFor(err), err)
			return
		}

		resp := codeMetricsResp{Commits: []commitMetricRow{}}
		// No repo configured (or it won't open) → return an empty,
		// well-formed payload rather than an error: the panel shows
		// "metrics unavailable" instead of a red banner.
		if proj.RepoRoot == "" {
			writeJSON(w, http.StatusOK, resp)
			return
		}
		repo, err := git.Open(proj.RepoRoot)
		if err != nil {
			writeJSON(w, http.StatusOK, resp)
			return
		}
		resp.RepoConfigured = true

		anchors, err := s.store.ListCodeAnchors(r.Context(), ownerType, ownerID)
		if err != nil {
			writeErr(w, http.StatusInternalServerError, err)
			return
		}

		seen := map[string]bool{}
		uniqueFiles := map[string]bool{}
		for _, a := range anchors {
			if a.Kind != "commit" || a.Revision == "" || seen[a.Revision] {
				continue
			}
			seen[a.Revision] = true
			m, mErr := repo.CommitMetrics(r.Context(), a.Revision)
			row := commitMetricRow{CommitMetrics: m, Label: a.Label}
			resp.Commits = append(resp.Commits, row)
			if mErr != nil { // ErrNotFound etc. — keep the row (Found=false), skip totals
				continue
			}
			resp.Totals.Added += m.Added
			resp.Totals.Deleted += m.Deleted
			resp.Totals.Hunks += m.Hunks
			resp.Totals.ExcludedFiles += m.ExcludedFiles
			for _, p := range m.Paths {
				uniqueFiles[p] = true
			}
		}
		resp.Totals.Commits = len(resp.Commits)
		resp.Totals.FilesChanged = len(uniqueFiles)
		resp.Totals.Net = resp.Totals.Added - resp.Totals.Deleted
		resp.Totals.Complexity, resp.Totals.ComplexityScore =
			git.Complexity(resp.Totals.Added+resp.Totals.Deleted, resp.Totals.FilesChanged, resp.Totals.Hunks)
		writeJSON(w, http.StatusOK, resp)
	}
}
