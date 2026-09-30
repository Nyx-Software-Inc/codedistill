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
	"net/http"

	"codedistill/internal/domain"
)

// What can be started right now, and what is stopping the rest.
//
// COMPUTED, never stored. A stored "ready" flag is wrong the instant a
// dependency is finished or dismissed, and a queue that quietly lies about what
// is startable is worse than no queue — the same reasoning as the architecture
// overlay and drift detection. The cost is a couple of queries; the project's
// edge count is bounded by its item count.

type readyItem struct {
	Kind    string `json:"kind"`
	ID      string `json:"id"`
	Ref     string `json:"ref,omitempty"`
	Subject string `json:"subject"`
	Status  string `json:"status"`

	// BlockedBy is why this cannot be started, when it cannot. Each entry
	// carries the edge id so the reason can be argued with in place: a derived
	// edge is advisory, and the moment to disagree with it is when it is
	// actually costing you something.
	BlockedBy []blocker `json:"blocked_by,omitempty"`

	// Unblocks counts what starting this would release. The tie-breaker that
	// makes an ordering useful rather than merely true: of the things you CAN
	// start, the one that frees the most is usually the one to do.
	Unblocks int `json:"unblocks"`

	// Informs are weaker relationships — worth knowing, never blocking.
	Informs []blocker `json:"informs,omitempty"`
}

type blocker struct {
	EdgeID    string `json:"edge_id"`
	Kind      string `json:"kind"` // item kind of the blocker
	ID        string `json:"id"`
	Ref       string `json:"ref,omitempty"`
	Subject   string `json:"subject"`
	Status    string `json:"status"` // the BLOCKER's own status: done or not
	Rationale string `json:"rationale,omitempty"`
	// Advisory is true when no human has confirmed this edge, so the UI can
	// offer to dismiss it without implying someone chose it.
	Advisory bool `json:"advisory"`
}

// readiness answers "what now" for a project.
func (s *Server) readiness(w http.ResponseWriter, r *http.Request) {
	projectID := r.URL.Query().Get("project_id")
	if projectID == "" {
		writeMsg(w, http.StatusBadRequest, "project_id is required")
		return
	}

	// Every kind together: a bug can block a use case, and looking at one kind
	// at a time would miss exactly those edges.
	type rec struct {
		kind, id, ref, subject, status string
		settled                        bool
	}
	byID := map[string]*rec{}
	var all []*rec

	add := func(r *rec) { byID[r.id] = r; all = append(all, r) }

	if ucs, err := s.store.ListUseCaseItems(r.Context(), projectID); err == nil {
		for _, u := range ucs {
			add(&rec{kind: "use_case", id: u.ID, ref: refLabel(u.Number),
				subject: u.Subject, status: u.Status, settled: isSettled(u.Status)})
		}
	}
	if todos, err := s.store.ListTodoItems(r.Context(), projectID); err == nil {
		for _, t := range todos {
			add(&rec{kind: "todo", id: t.ID, subject: t.Subject,
				status: t.Status, settled: isSettled(t.Status)})
		}
	}
	if bugs, err := s.store.ListBugItems(r.Context(), projectID); err == nil {
		for _, b := range bugs {
			add(&rec{kind: "bug", id: b.ID, subject: b.Subject,
				status: b.Status, settled: isSettled(b.Status)})
		}
	}

	deps, err := s.store.ListDependencies(r.Context(), projectID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}

	blockers := map[string][]blocker{}
	informs := map[string][]blocker{}
	unblocks := map[string]int{}
	for _, d := range deps {
		if !d.InForce() {
			// Dismissed. Kept in the table so it is not re-proposed, and
			// deliberately invisible here — a released constraint is not a
			// constraint.
			continue
		}
		to := byID[d.ToID]
		if to == nil {
			// The blocker was deleted. The edge is stale rather than
			// satisfied, and treating a vanished blocker as "done" would
			// silently unblock work on the strength of a deletion.
			continue
		}
		b := blocker{
			EdgeID: d.ID, Kind: to.kind, ID: to.id, Ref: to.ref,
			Subject: to.subject, Status: to.status, Rationale: d.Rationale,
			Advisory: d.Status == domain.EdgeAdvisory,
		}
		if d.Kind == domain.DependencyInforms {
			informs[d.FromID] = append(informs[d.FromID], b)
			continue
		}
		// Only an UNSETTLED blocker blocks. This is the whole reason the view
		// is computed: finishing one item changes what is startable, and
		// nothing has to be recalculated or remembered for that to be true.
		if !to.settled {
			blockers[d.FromID] = append(blockers[d.FromID], b)
			unblocks[d.ToID]++
		}
	}

	ready := []readyItem{}
	blocked := []readyItem{}
	for _, it := range all {
		if it.settled {
			continue
		}
		v := readyItem{
			Kind: it.kind, ID: it.id, Ref: it.ref, Subject: it.subject,
			Status: it.status, BlockedBy: blockers[it.id],
			Unblocks: unblocks[it.id], Informs: informs[it.id],
		}
		if len(v.BlockedBy) > 0 {
			blocked = append(blocked, v)
			continue
		}
		ready = append(ready, v)
	}

	// Most-unblocking first among the startable. Of the things you can do, the
	// one that frees the most others is usually the one to do — which is the
	// only actionable thing an ordering buys you.
	sortByUnblocks(ready)
	sortByUnblocks(blocked)

	writeJSON(w, http.StatusOK, map[string]any{
		"ready":   ready,
		"blocked": blocked,
		// Stated rather than left to be inferred from two empty lists: a
		// project with no edges at all has everything "ready", which is true
		// and also means nobody has worked out the order yet.
		"edges_in_force": len(deps),
		"has_ordering":   anyInForce(deps),
	})
}

func sortByUnblocks(xs []readyItem) {
	for i := 1; i < len(xs); i++ {
		for j := i; j > 0 && xs[j].Unblocks > xs[j-1].Unblocks; j-- {
			xs[j], xs[j-1] = xs[j-1], xs[j]
		}
	}
}

func anyInForce(deps []*domain.ItemDependency) bool {
	for _, d := range deps {
		if d.InForce() {
			return true
		}
	}
	return false
}

// isSettled reports whether an item is no longer work.
//
// MEASURED against the statuses the tables actually hold, not guessed. The
// previous version listed six values and missed five, which mattered in both
// directions: 28 bugs marked "fixed", 6 todos "abandoned" and 2 use cases
// "rejected" were reported as needing work AND as still blocking whatever
// depended on them. A dependency on a fixed bug blocked forever.
//
// Two groups, because they mean different things and both stop the work:
//
//	FINISHED         the work happened
//	NOT HAPPENING    it was abandoned, rejected, or was never real
//
// Distinguished in the comment rather than the code because readiness asks one
// question — is this still work — and for that they are the same answer.
// Anything reporting on OUTCOMES must not reuse this.
func isSettled(status string) bool {
	switch status {
	// Finished.
	case "done", "complete", "completed", "closed", "resolved", "implemented", "fixed":
		return true
	// Not happening.
	case "abandoned", "rejected", "not_a_bug", "duplicate":
		return true
	}
	return false
}

func refLabel(number int) string {
	if number <= 0 {
		return ""
	}
	return "UC-" + itoa(number)
}

// itoa avoids pulling strconv in for one call site.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [12]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}
