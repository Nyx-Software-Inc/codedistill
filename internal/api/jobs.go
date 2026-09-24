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
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"codedistill/internal/domain"
)

// The jobs monitor: what is running, what it is running ON, and how far along.
//
// Four private job systems existed before the jobs table — an in-memory
// classify queue, verification runs, analysis scans, architecture drafting —
// and none of them could answer "what is this machine doing right now" in one
// place. That question is the whole feature.

// jobView is a job plus what only the server can compute: an ETA measured from
// observed throughput, and the worker actually serving it.
type jobView struct {
	*domain.Job

	// Percent is -1 when the total is not yet known. The UI must render that
	// as indeterminate rather than as zero: a bar pinned at 0% for four
	// minutes reads as broken, and one that jumps when the denominator arrives
	// reads as a lie.
	Percent    int `json:"percent"`
	ETASeconds int `json:"eta_seconds"`

	// WorkerType and ProviderName say which model is doing the work. A job that
	// has been running for twenty minutes is a different conversation depending
	// on whether it is a local 7B or a hosted model.
	WorkerType    string `json:"worker_type,omitempty"`
	ProviderName  string `json:"provider_name,omitempty"`
	ProviderLocal bool   `json:"provider_local,omitempty"`

	ElapsedSeconds int `json:"elapsed_seconds"`

	// What the run PRODUCED, for workflows whose output waits on a person.
	//
	// A decompose that "succeeded" has not finished doing anything useful: it
	// found 49 things and none of them are work until someone says so. Reporting
	// the status alone is how a run can look complete while its entire output
	// sits in a table nobody has been told about.
	Proposals      int `json:"proposals,omitempty"`
	AwaitingReview int `json:"awaiting_review,omitempty"`
}

// workerForJob maps a workflow to the worker type that serves it.
//
// A table rather than a column on the job: today each workflow has exactly one
// worker, and writing that down here is honest about it. When the epistemic
// pair arrives a job will have several, and this becomes a lookup into a
// per-job worker list rather than a constant.
var workerForJob = map[string]string{
	domain.JobDecompose: domain.WorkerDecomposer,
	domain.JobArchDraft: domain.WorkerReviewer,
}

func (s *Server) listJobs(w http.ResponseWriter, r *http.Request) {
	projectID := r.URL.Query().Get("project_id")
	if projectID == "" {
		writeMsg(w, http.StatusBadRequest, "project_id is required")
		return
	}
	var statuses []string
	if q := r.URL.Query().Get("status"); q != "" {
		statuses = append(statuses, q)
	}
	limit := 50
	if n, err := strconv.Atoi(r.URL.Query().Get("limit")); err == nil && n > 0 {
		limit = n
	}

	jobs, err := s.store.ListJobs(r.Context(), projectID, statuses, limit)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}

	now := time.Now().UTC()
	// Provider names are resolved once per worker type rather than per job: a
	// list of fifty jobs is usually two or three distinct workers.
	providerCache := map[string]*domain.ModelProvider{}

	// One query for the whole page rather than one per job.
	runsByJob := map[string]*domain.DecomposeRunSummary{}
	if runs, err := s.store.ListDecomposeRuns(r.Context(), projectID, 200); err == nil {
		for _, run := range runs {
			runsByJob[run.JobID] = run
		}
	}

	out := make([]jobView, 0, len(jobs))
	for _, j := range jobs {
		v := jobView{
			Job: j, Percent: j.Percent(), ETASeconds: j.ETASeconds(now),
			WorkerType: workerForJob[j.Type],
		}
		end := now
		if j.FinishedAt != nil {
			end = *j.FinishedAt
		}
		v.ElapsedSeconds = int(end.Sub(j.StartedAt).Seconds())

		if run := runsByJob[j.ID]; run != nil {
			v.Proposals, v.AwaitingReview = run.Total, run.Pending
		}

		if v.WorkerType != "" {
			p, seen := providerCache[v.WorkerType]
			if !seen {
				p, _ = s.store.ResolveWorkerModel(r.Context(), v.WorkerType, projectID)
				providerCache[v.WorkerType] = p
			}
			if p != nil {
				v.ProviderName, v.ProviderLocal = p.Name, p.IsLocal
			}
		}
		out = append(out, v)
	}
	writeJSON(w, http.StatusOK, out)
}

// jobDetail is one run's full shape: where its time went, and what that
// usually costs.
//
// The norms are the point. Elapsed time carries no verdict on its own — "12
// minutes" means nothing until you know the usual reading pass is 24 — and
// that comparison is what turns a progress bar into something a person can act
// on at minute 25 of a long run.
func (s *Server) jobDetail(w http.ResponseWriter, r *http.Request) {
	j, err := s.store.GetJob(r.Context(), r.PathValue("id"))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	if j == nil {
		writeMsg(w, http.StatusNotFound, "no such job")
		return
	}
	phases, err := s.store.ListJobPhases(r.Context(), j.ID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	if phases == nil {
		phases = []*domain.JobPhase{}
	}
	// Failing to compute norms must not fail the detail view: a first run has
	// no history and that is the normal case, not an error.
	norms, _ := s.store.PhaseNorms(r.Context(), j.ProjectID, j.Type)
	if norms == nil {
		norms = []domain.PhaseNorm{}
	}

	now := time.Now().UTC()
	type phaseView struct {
		*domain.JobPhase
		Seconds int  `json:"seconds"`
		Running bool `json:"running"`
	}
	views := make([]phaseView, 0, len(phases))
	for _, ph := range phases {
		views = append(views, phaseView{JobPhase: ph, Seconds: ph.Seconds(now), Running: ph.Running()})
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"job":    jobView{Job: j, Percent: j.Percent(), ETASeconds: j.ETASeconds(now), WorkerType: workerForJob[j.Type]},
		"phases": views,
		"norms":  norms,
	})
}

// pauseJob stops a run that is expected to continue.
//
// NOT instant, and the response says so: a model call is atomic, so a
// ten-minute generate cannot be interrupted and the pause takes effect at the
// next checkpoint. Refused for a workflow that has not declared itself
// resumable, because a Pause button that silently means Cancel is worse than
// no button.
func (s *Server) pauseJob(w http.ResponseWriter, r *http.Request) {
	j, err := s.store.GetJob(r.Context(), r.PathValue("id"))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	if j == nil {
		writeMsg(w, http.StatusNotFound, "no such job")
		return
	}
	if !j.Active() {
		w.WriteHeader(http.StatusNoContent) // already stopped; two clicks are not an error
		return
	}
	wf, err := s.store.GetWorkflow(r.Context(), j.Type)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	if wf == nil || !wf.Resumable {
		writeMsg(w, http.StatusConflict,
			"this workflow cannot be resumed, so pausing it would lose the run — cancel it instead")
		return
	}
	now := time.Now().UTC()
	if err := s.store.FinishJob(r.Context(), j.ID, domain.JobPaused, "", now); err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	_ = s.store.FinishJobPhases(r.Context(), j.ID, "", now)
	if run := s.currentArchRun(j.ProjectID); run != nil && run.jobID == j.ID {
		run.cancel()
	}
	// A run this server started stops at its next checkpoint. One started from
	// the command line is not ours to stop — its row says paused and the
	// process will notice on its own.
	s.stopJob(j.ID)
	writeJSON(w, http.StatusOK, map[string]any{
		"status": domain.JobPaused,
		"note":   "stopping at the next checkpoint — a model call in flight will finish first",
	})
}

// cancelJob stops a running job.
//
// It records the intent; the worker notices on its next cancellation check.
// Deliberately not waiting for that: a job mid-model-call may take minutes to
// return, and a UI that hangs until it does is worse than one that says
// "cancelling".
func (s *Server) cancelJob(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	j, err := s.store.GetJob(r.Context(), id)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	if j == nil {
		writeMsg(w, http.StatusNotFound, "no such job")
		return
	}
	if !j.Active() {
		// Already finished. Not an error: two clicks on a slow list should not
		// produce a failure.
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if err := s.store.FinishJob(r.Context(), id, domain.JobCancelled, "", time.Now().UTC()); err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	// Architecture drafting holds a cancel func in memory for its own run.
	if run := s.currentArchRun(j.ProjectID); run != nil && run.jobID == id {
		run.cancel()
	}
	s.stopJob(id)
	w.WriteHeader(http.StatusNoContent)
}

// listWorkflows returns the definitions with their steps and resolved models.
func (s *Server) listWorkflows(w http.ResponseWriter, r *http.Request) {
	rows, err := s.store.ListWorkflows(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	if rows == nil {
		rows = []*domain.Workflow{}
	}
	writeJSON(w, http.StatusOK, rows)
}

// setStepProvider points one step of a workflow at a model. This is where
// worker configuration lives now: "decompose runs on Claude" is a fact about
// decomposing, not about the list of connections.
func (s *Server) setStepProvider(w http.ResponseWriter, r *http.Request) {
	ordinal, err := strconv.Atoi(r.PathValue("ordinal"))
	if err != nil {
		writeMsg(w, http.StatusBadRequest, "ordinal must be a number")
		return
	}
	var req struct {
		ProviderID string `json:"provider_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if err := s.store.SetStepProvider(r.Context(), r.PathValue("id"), ordinal,
		req.ProviderID, time.Now().UTC()); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
