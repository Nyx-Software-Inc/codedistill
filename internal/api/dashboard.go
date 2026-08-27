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
	"math"
	"net/http"
	"sort"
	"strconv"
	"time"

	codegit "codedistill/internal/git"
)

// dashboardThroughputDefaultDays is the look-back window when the
// caller doesn't pass ?days=. Matches the locked design's "rolling 30d
// throughput" for Phase A panel #1.
const dashboardThroughputDefaultDays = 30

// dashboardThroughput handles GET /api/v1/projects/{id}/dashboard/throughput.
// Returns per-day created/completed counts for todos/bugs/use_cases
// over the look-back window plus current open totals per type.
//
// Query params:
//
//	days  — optional int, 1..365; defaults to 30. Window endpoint is
//	        "now"; start is midnight local time `days-1` days ago, so
//	        the request day is always fully included.
func (s *Server) dashboardThroughput(w http.ResponseWriter, r *http.Request) {
	pid := r.PathValue("id")
	if _, err := s.store.GetProject(r.Context(), pid); err != nil {
		writeErr(w, statusFor(err), err)
		return
	}

	days := dashboardThroughputDefaultDays
	if q := r.URL.Query().Get("days"); q != "" {
		n, err := strconv.Atoi(q)
		if err != nil || n < 1 || n > 365 {
			writeMsg(w, http.StatusBadRequest, "days must be an integer 1..365")
			return
		}
		days = n
	}

	now := time.Now()
	// Floor "today" to local midnight, then walk back days-1 calendar
	// days so the window includes the current day as a full bucket.
	todayMidnight := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.Local)
	since := todayMidnight.AddDate(0, 0, -(days - 1))

	out, err := s.store.DashboardThroughput(r.Context(), pid, since)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

// dashboardIndexHealth handles GET /api/v1/projects/{id}/dashboard/index-health.
// Straight storage passthrough — embedding coverage, code-chunk totals,
// anchor counts by provenance, dedup count.
func (s *Server) dashboardIndexHealth(w http.ResponseWriter, r *http.Request) {
	pid := r.PathValue("id")
	if _, err := s.store.GetProject(r.Context(), pid); err != nil {
		writeErr(w, statusFor(err), err)
		return
	}
	out, err := s.store.DashboardIndexHealth(r.Context(), pid)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

// lifecycleTypeStats is the time-to-close rollup for one item type.
// Hours are wall-clock from created_at to the type's completion stamp.
type lifecycleTypeStats struct {
	Count       int     `json:"count"`
	MedianHours float64 `json:"median_hours"`
	P90Hours    float64 `json:"p90_hours"`
}

// dashboardBranchRow is one bar in the per-branch panel: closed-item
// counts by type slug attributed to one branch.
type dashboardBranchRow struct {
	Branch string         `json:"branch"`
	Counts map[string]int `json:"counts"`
	Total  int            `json:"total"`
}

// dashboardLifecycle is the response for the lifecycle endpoint —
// panels #4 (time-to-close) and #5 (per-branch) share one fetch.
// NoCommit counts closed items that never got a commit_sha; Unresolved
// counts SHAs not reachable from any current branch (rebased away,
// or the ref was deleted). RepoAvailable is false when the project has
// no repo_root or it doesn't open — branch data is then empty but
// time-to-close still populates.
type dashboardLifecycle struct {
	TimeToClose   map[string]lifecycleTypeStats `json:"time_to_close"`
	Branches      []dashboardBranchRow          `json:"branches"`
	NoCommit      map[string]int                `json:"no_commit"`
	Unresolved    map[string]int                `json:"unresolved"`
	RepoAvailable bool                          `json:"repo_available"`
	DefaultBranch string                        `json:"default_branch,omitempty"`
}

// dashboardLifecycle handles GET /api/v1/projects/{id}/dashboard/lifecycle.
func (s *Server) dashboardLifecycle(w http.ResponseWriter, r *http.Request) {
	pid := r.PathValue("id")
	p, err := s.store.GetProject(r.Context(), pid)
	if err != nil {
		writeErr(w, statusFor(err), err)
		return
	}
	items, err := s.store.DashboardClosedItems(r.Context(), pid)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}

	out := &dashboardLifecycle{
		TimeToClose: map[string]lifecycleTypeStats{},
		Branches:    []dashboardBranchRow{},
		NoCommit:    map[string]int{},
		Unresolved:  map[string]int{},
	}

	// Panel #4: time-to-close per type. Negative durations (clock skew,
	// hand-edited timestamps) clamp to zero rather than skewing the
	// percentiles.
	hoursByType := map[string][]float64{}
	for _, ci := range items {
		h := ci.ClosedAt.Sub(ci.CreatedAt).Hours()
		if h < 0 {
			h = 0
		}
		hoursByType[ci.Type] = append(hoursByType[ci.Type], h)
	}
	for slug, hs := range hoursByType {
		sort.Float64s(hs)
		out.TimeToClose[slug] = lifecycleTypeStats{
			Count:       len(hs),
			MedianHours: percentile(hs, 0.50),
			P90Hours:    percentile(hs, 0.90),
		}
	}

	// Panel #5: attribute each closed item's commit to a branch.
	var attr map[string]string
	if p.RepoRoot != "" {
		if repo, err := codegit.Open(p.RepoRoot); err == nil {
			shas := make([]string, 0, len(items))
			for _, ci := range items {
				if ci.CommitSHA != "" {
					shas = append(shas, ci.CommitSHA)
				}
			}
			if attr, out.DefaultBranch, err = repo.AttributeCommitsToBranches(shas); err != nil {
				writeErr(w, http.StatusInternalServerError, err)
				return
			}
			out.RepoAvailable = true
		}
	}
	rowByBranch := map[string]*dashboardBranchRow{}
	for _, ci := range items {
		switch {
		case ci.CommitSHA == "":
			out.NoCommit[ci.Type]++
		case attr[ci.CommitSHA] == "":
			out.Unresolved[ci.Type]++
		default:
			b := attr[ci.CommitSHA]
			row := rowByBranch[b]
			if row == nil {
				row = &dashboardBranchRow{Branch: b, Counts: map[string]int{}}
				rowByBranch[b] = row
			}
			row.Counts[ci.Type]++
			row.Total++
		}
	}
	for _, row := range rowByBranch {
		out.Branches = append(out.Branches, *row)
	}
	// Default branch first, then by total desc, then name for stability.
	sort.Slice(out.Branches, func(i, j int) bool {
		a, b := out.Branches[i], out.Branches[j]
		if (a.Branch == out.DefaultBranch) != (b.Branch == out.DefaultBranch) {
			return a.Branch == out.DefaultBranch
		}
		if a.Total != b.Total {
			return a.Total > b.Total
		}
		return a.Branch < b.Branch
	})
	writeJSON(w, http.StatusOK, out)
}

// percentile returns the nearest-rank percentile of an ascending-sorted
// slice; 0 for an empty slice.
func percentile(sorted []float64, q float64) float64 {
	if len(sorted) == 0 {
		return 0
	}
	rank := int(math.Ceil(q*float64(len(sorted)))) - 1
	if rank < 0 {
		rank = 0
	}
	if rank >= len(sorted) {
		rank = len(sorted) - 1
	}
	return sorted[rank]
}
