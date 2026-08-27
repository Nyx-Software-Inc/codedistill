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
	"sync"
	"time"

	"codedistill/internal/events"
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

// archJob is one project's enrichment run. A completed job stays in the map
// (running=false) so the panel can show the final tally until the next draft.
type archJob struct {
	mu      sync.Mutex
	total   int
	done    int
	running bool
	scope   string // "" = whole diagram; else a top-level dir being prioritized
	errMsg  string
	startAt time.Time
	cancel  context.CancelFunc
}

// archJobStatus is the wire shape reported inside GET /architecture.
type archJobStatus struct {
	Running bool   `json:"running"`
	Total   int    `json:"total"`
	Done    int    `json:"done"`
	Scope   string `json:"scope,omitempty"`
	// EtaSeconds estimates the remaining run from measured per-call time
	// (a nominal 40s/component before the first call lands).
	EtaSeconds int    `json:"eta_seconds"`
	Error      string `json:"error,omitempty"`
}

func (j *archJob) status() *archJobStatus {
	j.mu.Lock()
	defer j.mu.Unlock()
	st := &archJobStatus{Running: j.running, Total: j.total, Done: j.done, Scope: j.scope, Error: j.errMsg}
	if remaining := j.total - j.done; j.running && remaining > 0 {
		per := 40.0
		if j.done > 0 && !j.startAt.IsZero() {
			per = time.Since(j.startAt).Seconds() / float64(j.done)
		}
		st.EtaSeconds = int(per * float64(remaining))
	}
	return st
}

// archJobFor returns the project's job record, if any.
func (s *Server) archJobFor(pid string) *archJob {
	s.archJobsMu.Lock()
	defer s.archJobsMu.Unlock()
	return s.archJobs[pid]
}

// startArchJob registers and launches an enrichment job for the project unless
// one is already running. scope narrows the run to one top-level directory
// ("characterize this first"); "" enriches everything pending. Returns the
// active job either way.
func (s *Server) startArchJob(pid string, comps []component, scope string) *archJob {
	s.archJobsMu.Lock()
	if s.archJobs == nil {
		s.archJobs = map[string]*archJob{}
	}
	if j := s.archJobs[pid]; j != nil {
		j.mu.Lock()
		running := j.running
		j.mu.Unlock()
		if running {
			s.archJobsMu.Unlock()
			return j
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	j := &archJob{running: true, scope: scope, startAt: time.Now(), cancel: cancel}
	s.archJobs[pid] = j
	s.archJobsMu.Unlock()

	go s.runArchEnrichment(ctx, j, pid, comps, scope)
	return j
}

// restartArchJob supersedes any running enrichment and starts a fresh one. Used
// by explicit redraft: the previous graph (and its node IDs) has just been
// deleted and replaced, so the old job must stop and the NEW skeleton must be
// enriched. startArchJob would instead defer to the still-"running" old job
// (it may be mid-LLM-call for up to 3 min), leaving the new graph forever
// un-enriched — the redraft race in bug 100. Replacing the map entry also makes
// the old job non-current, which runArchEnrichment checks before each writeback.
func (s *Server) restartArchJob(pid string, comps []component, scope string) *archJob {
	s.archJobsMu.Lock()
	if s.archJobs == nil {
		s.archJobs = map[string]*archJob{}
	}
	if j := s.archJobs[pid]; j != nil {
		j.mu.Lock()
		if j.running && j.cancel != nil {
			j.cancel()
		}
		j.mu.Unlock()
	}
	ctx, cancel := context.WithCancel(context.Background())
	j := &archJob{running: true, scope: scope, startAt: time.Now(), cancel: cancel}
	s.archJobs[pid] = j
	s.archJobsMu.Unlock()

	go s.runArchEnrichment(ctx, j, pid, comps, scope)
	return j
}

// inArchScope reports whether area falls under the scope directory
// ("" = everything).
func inArchScope(area, scope string) bool {
	return scope == "" || area == scope || strings.HasPrefix(area, scope+"/")
}

// runArchEnrichment characterizes every node still lacking a description, one
// bounded LLM call at a time. Failures are skipped (the node keeps its honest
// path-derived label; a later resume retries them).
func (s *Server) runArchEnrichment(ctx context.Context, j *archJob, pid string, comps []component, scope string) {
	defer func() {
		j.mu.Lock()
		j.running = false
		j.mu.Unlock()
		// One completion signal (not one per node): connected clients refetch.
		s.bus.Publish(events.ItemsChanged)
	}()

	compByDir := map[string]component{}
	for _, c := range comps {
		compByDir[c.Dir] = c
	}
	nodes, err := s.store.ListArchitectureNodes(ctx, pid)
	if err != nil {
		j.mu.Lock()
		j.errMsg = err.Error()
		j.mu.Unlock()
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
	j.mu.Lock()
	j.total = len(pending)
	j.mu.Unlock()

	for _, nid := range pending {
		// Stop if cancelled OR superseded by a newer draft (redraft replaced us
		// in the job map). The newer job owns the current node IDs; a stale job
		// must not keep writing (bug 100 redraft race).
		if ctx.Err() != nil || s.archJobFor(pid) != j {
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
				j.mu.Lock()
				j.errMsg = err.Error()
				j.mu.Unlock()
			}
		}
		j.mu.Lock()
		j.done++
		j.mu.Unlock()
	}
}

// cancelArchDraft stops the project's running enrichment job. Enriched nodes
// stay; the panel offers Resume for the remainder.
func (s *Server) cancelArchDraft(w http.ResponseWriter, r *http.Request) {
	pid := r.PathValue("id")
	if j := s.archJobFor(pid); j != nil {
		j.mu.Lock()
		if j.running && j.cancel != nil {
			j.cancel()
		}
		j.mu.Unlock()
	}
	w.WriteHeader(http.StatusNoContent)
}
