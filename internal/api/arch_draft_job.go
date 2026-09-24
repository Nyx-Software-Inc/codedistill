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
	"strings"
	"time"

	"codedistill/internal/domain"
	"codedistill/internal/events"
	"codedistill/internal/id"
)

// Async architecture drafting. The old draft made one LLM call per component
// INSIDE the HTTP request — ~35-45s per call on local hardware, so any real
// repo (24 components ≈ 13+ minutes) outlived every client timeout and the
// feature read as broken (Slavko's report; reproduced on Now in Android).
//
// New contract, split by cost:
//   - STRUCTURE is free: the skeleton (every component as a path-derived node
//     + structural edges) is created synchronously in the request. The full,
//     honest shape of the system exists immediately.
//   - CHARACTERIZATION streams: a per-project background job enriches nodes
//     one LLM call at a time, persisting each result as it lands. Closing the
//     panel/browser doesn't matter — the job lives in the server, and the
//     panel re-reads reality whenever it's opened.
//   - RESUMABLE: "needs enrichment" is derived from the data (a proposed node
//     with an empty description), not from job state — so a server restart
//     loses nothing but the wait, and re-drafting never nukes enriched work
//     unless explicitly asked (mode=redraft).
//
// The run itself is now a row in `jobs` rather than an entry in a private map,
// so it shows up wherever every other long job does and survives a restart as
// `interrupted` instead of vanishing. What stays in memory is only what cannot
// be written down: the cancel function, and which run currently owns the
// project.

// archRun is the in-memory half of a draft: a cancel handle plus the identity
// of the job that owns this project right now. The durable half is the jobs
// row; this exists because a context.CancelFunc cannot be persisted.
type archRun struct {
	jobID  string
	scope  string
	cancel context.CancelFunc
}

// archJobStatus is the wire shape reported inside GET /architecture. Unchanged
// from the map-based version so the panel needs no migration.
type archJobStatus struct {
	Running bool   `json:"running"`
	Total   int    `json:"total"`
	Done    int    `json:"done"`
	Scope   string `json:"scope,omitempty"`
	// EtaSeconds estimates the remaining run from measured per-call time.
	EtaSeconds int    `json:"eta_seconds"`
	Error      string `json:"error,omitempty"`
}

// archStatusOf renders a persisted job for the panel. A nominal 40s/component
// stands in before the first call lands, which domain.Job cannot supply because
// it has nothing to measure yet.
func archStatusOf(j *domain.Job, now time.Time) *archJobStatus {
	if j == nil {
		return nil
	}
	st := &archJobStatus{
		Running: j.Active(), Total: j.Total, Done: j.Done,
		Scope: j.ScopeLabel, Error: j.Error,
	}
	if st.Running {
		if eta := j.ETASeconds(now); eta > 0 {
			st.EtaSeconds = eta
		} else if remaining := j.Total - j.Done; remaining > 0 && j.Done == 0 {
			st.EtaSeconds = remaining * 40
		}
	}
	return st
}

// archJobFor returns the project's most recent draft run, or nil. Unlike the
// old map this includes finished runs, which is what lets the panel keep
// showing a final tally.
func (s *Server) archJobFor(ctx context.Context, pid string) *domain.Job {
	j, err := s.store.LatestJobFor(ctx, domain.JobArchDraft, archScopeKind, pid)
	if err != nil {
		return nil
	}
	return j
}

// The draft is a singleton per PROJECT, not per directory: two concurrent runs
// over different directories would both write architecture nodes. The narrower
// directory scope rides in scope_label, where the dashboard can show it.
const archScopeKind = "project"

// currentArchRun reports which run owns the project right now.
func (s *Server) currentArchRun(pid string) *archRun {
	s.archJobsMu.Lock()
	defer s.archJobsMu.Unlock()
	return s.archRuns[pid]
}

// startArchJob launches enrichment unless a run is already going. scope narrows
// to one top-level directory ("characterize this first"); "" enriches
// everything pending.
func (s *Server) startArchJob(ctx context.Context, pid string, comps []component, scope string) *domain.Job {
	if existing, err := s.store.ActiveJobFor(ctx, domain.JobArchDraft, archScopeKind, pid); err == nil && existing != nil {
		return existing
	}
	return s.launchArchJob(ctx, pid, comps, scope, false)
}

// restartArchJob supersedes any running enrichment and starts a fresh one. Used
// by explicit redraft: the previous graph (and its node IDs) has just been
// deleted and replaced, so the old job must stop and the NEW skeleton must be
// enriched. startArchJob would instead defer to the still-running old job (it
// may be mid-LLM-call for up to 3 min), leaving the new graph forever
// un-enriched — the redraft race in bug 100. Replacing the run record also
// makes the old job non-current, which runArchEnrichment checks before each
// writeback.
func (s *Server) restartArchJob(ctx context.Context, pid string, comps []component, scope string) *domain.Job {
	return s.launchArchJob(ctx, pid, comps, scope, true)
}

func (s *Server) launchArchJob(ctx context.Context, pid string, comps []component, scope string, supersede bool) *domain.Job {
	now := time.Now().UTC()
	job := &domain.Job{
		ID: id.New(), ProjectID: pid, Type: domain.JobArchDraft,
		ScopeKind: archScopeKind, ScopeID: pid, ScopeLabel: scope,
		Status: domain.JobRunning, StartedAt: now, UpdatedAt: now,
	}
	if err := s.store.CreateJob(ctx, job); err != nil {
		return nil
	}

	runCtx, cancel := context.WithCancel(context.Background())

	s.archJobsMu.Lock()
	if s.archRuns == nil {
		s.archRuns = map[string]*archRun{}
	}
	if prev := s.archRuns[pid]; prev != nil && supersede {
		prev.cancel()
		// The superseded run's own defer marks it cancelled; it checks
		// currentArchRun before every writeback and will stop on its next pass.
		s.finishArchJobAsync(prev.jobID, domain.JobCancelled)
	}
	s.archRuns[pid] = &archRun{jobID: job.ID, scope: scope, cancel: cancel}
	s.archJobsMu.Unlock()

	go s.runArchEnrichment(runCtx, job.ID, pid, comps, scope)
	return job
}

// finishArchJobAsync closes a superseded run's row without blocking the caller
// holding archJobsMu.
func (s *Server) finishArchJobAsync(jobID, status string) {
	go func() {
		ctx, done := context.WithTimeout(context.Background(), 10*time.Second)
		defer done()
		_ = s.store.FinishJob(ctx, jobID, status, "", time.Now().UTC())
	}()
}

// inArchScope reports whether area falls under the scope directory
// ("" = everything).
func inArchScope(area, scope string) bool {
	return scope == "" || area == scope || strings.HasPrefix(area, scope+"/")
}

// runArchEnrichment characterizes every node still lacking a description, one
// bounded LLM call at a time. Failures are skipped (the node keeps its honest
// path-derived label; a later resume retries them).
func (s *Server) runArchEnrichment(ctx context.Context, jobID, pid string, comps []component, scope string) {
	status, errMsg := domain.JobSucceeded, ""
	defer func() {
		// A superseded run must not overwrite the winner's row — but its OWN
		// row still has to be closed, or it stays "running" forever and blocks
		// every later draft.
		if ctx.Err() != nil && status != domain.JobFailed {
			status = domain.JobCancelled
		}
		fctx, done := context.WithTimeout(context.Background(), 10*time.Second)
		_ = s.store.FinishJob(fctx, jobID, status, errMsg, time.Now().UTC())
		done()
		// One completion signal (not one per node): connected clients refetch.
		s.bus.Publish(events.ItemsChanged)
	}()

	compByDir := map[string]component{}
	for _, c := range comps {
		compByDir[c.Dir] = c
	}
	nodes, err := s.store.ListArchitectureNodes(ctx, pid)
	if err != nil {
		status, errMsg = domain.JobFailed, err.Error()
		return
	}

	var pending []string // node IDs, in stored order
	byID := map[string]int{}
	for i, n := range nodes {
		if n.Provenance == "proposed" && n.Description == "" && n.Area != "" && inArchScope(n.Area, scope) {
			if _, ok := compByDir[n.Area]; ok {
				pending = append(pending, n.ID)
				byID[n.ID] = i
			}
		}
	}
	total, done := len(pending), 0
	_ = s.store.UpdateJobProgress(ctx, jobID, "", 0, total, time.Now().UTC())

	for _, nid := range pending {
		// Stop if cancelled OR superseded by a newer draft. The newer job owns
		// the current node IDs; a stale job must not keep writing (bug 100
		// redraft race).
		if ctx.Err() != nil {
			return
		}
		if cur := s.currentArchRun(pid); cur == nil || cur.jobID != jobID {
			status = domain.JobCancelled
			return
		}
		n := nodes[byID[nid]]
		// Bound each model call so one hung generation can't wedge the job.
		cctx, cdone := context.WithTimeout(ctx, 3*time.Minute)
		d, ok := s.characterizeComponent(cctx, compByDir[n.Area])
		cdone()
		if ok {
			n.Name, n.Kind, n.Description = d.Name, d.Kind, d.Description
			n.UpdatedAt = time.Now().UTC()
			if err := s.store.UpdateArchitectureNode(ctx, n); err != nil && ctx.Err() == nil {
				errMsg = err.Error()
			}
		}
		done++
		_ = s.store.UpdateJobProgress(ctx, jobID, "", done, total, time.Now().UTC())
	}
}

// cancelArchDraft stops the project's running enrichment job. Enriched nodes
// stay; the panel offers Resume for the remainder.
func (s *Server) cancelArchDraft(w http.ResponseWriter, r *http.Request) {
	pid := r.PathValue("id")
	if run := s.currentArchRun(pid); run != nil {
		run.cancel()
	}
	w.WriteHeader(http.StatusNoContent)
}
