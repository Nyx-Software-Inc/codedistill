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
	"strings"
	"time"

	"codedistill/internal/domain"
	"codedistill/internal/git"
	"codedistill/internal/jobqueue"
)

// The job queue (docs/design/job-queue.md).
//
// Every job is submitted as a queued row and started by the scheduler when a
// job slot is free. Submitting does what must fail early (a missing document,
// no model configured), saves what was asked for, and returns; the job's
// runner rebuilds everything else from the row when the job starts, which may
// be an hour later or after a restart. So what a job reads about the code, it
// reads when it starts.

const (
	settingJobSlots  = "jobs.slots"       // jobs that may run at once, installation-wide
	settingJobAgingH = "jobs.aging_hours" // hours a job waits before moving up a priority level
)

// jobQueue is the server's scheduler, made on first use with every runner
// registered. It runs from StartJobQueue (serve), or from the first submit
// (tests, and anything embedding the server without serve).
func (s *Server) jobQueue() *jobqueue.Scheduler {
	s.queueMake.Do(func() {
		q := jobqueue.New(s.store, jobqueue.Config{Slots: s.jobSlots, Aging: s.jobAging, Log: s.log,
			CanPause: s.canPause, FixedPriority: s.fixedPriority, Pause: s.pauseToMakeRoom})
		q.Register(domain.JobDecompose, s.runDecompose)
		q.Register(domain.JobSequence, s.runSequence)
		q.Register(domain.JobArchDraft, s.runArchDraft)
		s.registerFixRunner(q)
		s.queue = q
	})
	return s.queue
}

// StartJobQueue starts the scheduler: queued jobs, including any left from
// before a restart, start as slots allow. Call once the stale running jobs
// have been marked interrupted, so they do not count against the slots.
func (s *Server) StartJobQueue(ctx context.Context) {
	q := s.jobQueue()
	s.queueRun.Do(func() {
		ctx, cancel := context.WithCancel(ctx)
		s.queueStop = cancel
		s.queueDone = make(chan struct{})
		go func() { q.Run(ctx); close(s.queueDone) }()
	})
}

// Close stops the job queue and the jobs it started, and waits for them. For
// whatever embeds a server without serve's own shutdown (tests).
func (s *Server) Close() {
	if s.queueStop != nil {
		s.queueStop()
		<-s.queueDone
	}
}

// jobSlots is how many jobs may run at once: the setting, else what this
// machine suggests.
func (s *Server) jobSlots(ctx context.Context) int {
	if n, ok := s.lookupSettingInt64(ctx, settingJobSlots, "", LocalUserID); ok && n >= 1 {
		return int(n)
	}
	return jobqueue.Suggest(jobqueue.Machine())
}

func (s *Server) jobAging(ctx context.Context) time.Duration {
	if n, ok := s.lookupSettingInt64(ctx, settingJobAgingH, "", LocalUserID); ok && n >= 1 {
		return time.Duration(n) * time.Hour
	}
	return jobqueue.DefaultAging
}

// workflowOf is the configuration a job runs under: its workflow, whose id is
// the job's type. What a job may do (pause, its highest priority, whether its
// priority moves) is read from here and never from which workflow it is
// (docs/design/workflows-and-roles.md, Part 1). Nil for a type with no
// workflow, which may do none of those things.
func (s *Server) workflowOf(ctx context.Context, jobType string) *domain.Workflow {
	w, err := s.store.GetWorkflow(ctx, jobType)
	if err != nil {
		return nil
	}
	return w
}

// canPause reports whether a job's workflow can pause and resume: only those
// are paused to make room for an Immediate job, offered Resume, and put back
// in line after a restart.
func (s *Server) canPause(jobType string) bool {
	w := s.workflowOf(context.Background(), jobType)
	return w != nil && w.Resumable
}

// fixedPriority reports whether a job's workflow keeps the priority its jobs
// were given.
func (s *Server) fixedPriority(jobType string) bool {
	w := s.workflowOf(context.Background(), jobType)
	return w != nil && w.FixedPriority
}

// pauseToMakeRoom pauses a running job for an Immediate one. The job stops at
// its next checkpoint; the queue puts it back in line once it has.
func (s *Server) pauseToMakeRoom(ctx context.Context, j *domain.Job) error {
	ok, err := s.store.PauseJob(ctx, j.ID, domain.PausedByQueue, time.Now().UTC())
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("the job is no longer running")
	}
	_ = s.store.FinishJobPhases(ctx, j.ID, "", time.Now().UTC())
	s.stopJob(j.ID)
	return nil
}

// admitImmediate says whether a job may be Immediate now, and if not, why:
// only an admin on a multi-user server, since it may pause someone else's job;
// and only when it can start, because a slot is free or a running job can
// pause to make one.
func (s *Server) admitImmediate(r *http.Request) error {
	ctx := r.Context()
	if !s.can(r, domain.PermManageAllJobs) {
		return fmt.Errorf("making a job Immediate needs the “manage everyone's jobs” permission, since it may pause someone else's job — choose High to go to the front of the line")
	}
	running, err := s.store.ListRunningJobs(ctx)
	if err != nil {
		return err
	}
	slots := s.jobSlots(ctx)
	if len(running) < slots {
		return nil
	}
	if jobqueue.ChooseVictim(running, s.canPause) != nil {
		return nil
	}
	immediate := 0
	for _, j := range running {
		if j.Priority == domain.PriorityImmediate {
			immediate++
		}
	}
	if immediate >= slots {
		return fmt.Errorf("every job slot already holds an Immediate job — choose High to go first in line")
	}
	return fmt.Errorf("no running job can pause to make room: none of their workflows can pause and resume — choose High to go first in line")
}

// queuedPlace is what a submit tells its caller: the job is waiting, where it
// stands, and whether a slot is already free for it.
type queuedPlace struct {
	Status    string `json:"status"`
	Position  int    `json:"position"`
	Slots     int    `json:"slots"`
	Running   int    `json:"running"`
	StartsNow bool   `json:"starts_now"`
}

// enqueueJob submits a job: a queued row, the parameters it was submitted
// with, and a nudge to the scheduler.
func (s *Server) enqueueJob(ctx context.Context, j *domain.Job, params map[string]string) (queuedPlace, error) {
	now := time.Now().UTC()
	j.Status, j.StartedAt, j.UpdatedAt, j.QueuedAt = domain.JobQueued, now, now, &now
	if j.Priority == "" {
		j.Priority = domain.PriorityMedium
	}
	if err := s.store.CreateJob(ctx, j); err != nil {
		return queuedPlace{}, err
	}
	if len(params) > 0 {
		if err := s.store.SaveJobParams(ctx, j.ID, params); err != nil {
			_ = s.store.FinishJob(context.WithoutCancel(ctx), j.ID, domain.JobFailed, "could not record what was asked for", now)
			return queuedPlace{}, err
		}
	}
	place := s.placeOf(ctx, j.ID)
	s.StartJobQueue(context.Background()) // already running under serve
	s.jobQueue().Kick()
	return place, nil
}

// placeOf reports where a waiting job stands.
func (s *Server) placeOf(ctx context.Context, id string) queuedPlace {
	p := queuedPlace{Status: domain.JobQueued, Slots: s.jobSlots(ctx)}
	p.Running, _ = s.store.CountRunningJobs(ctx)
	if queued, err := s.store.ListQueuedJobs(ctx); err == nil {
		p.Position = jobqueue.Position(queued, id, time.Now().UTC(), s.jobAging(ctx), s.fixedPriority)
	}
	p.StartsNow = p.Position > 0 && p.Position <= p.Slots-p.Running
	// Admitted only when it can start: it makes room if it has to.
	if j, err := s.store.GetJob(ctx, id); err == nil && j != nil && j.Priority == domain.PriorityImmediate {
		p.StartsNow = true
	}
	return p
}

// submitPriority reads the priority a job of workflow wf is given: no higher
// than the workflow allows, and Immediate only when it can run
// (admitImmediate).
func (s *Server) submitPriority(r *http.Request, wf *domain.Workflow, p string) (string, error) {
	p = strings.TrimSpace(p)
	if p == "" {
		return domain.PriorityMedium, nil
	}
	if !domain.ValidJobPriority(p) {
		return "", fmt.Errorf("priority must be low, medium, high or immediate")
	}
	if wf == nil {
		// No workflow configuration names a limit: the default, High.
		wf = &domain.Workflow{}
	}
	if !wf.AllowsPriority(p) {
		max := domain.PriorityHigh
		if wf.MaxPriority != "" {
			max = wf.MaxPriority
		}
		return "", fmt.Errorf("this workflow's jobs go no higher than %s", priorityName(max))
	}
	if p == domain.PriorityImmediate {
		if err := s.admitImmediate(r); err != nil {
			return "", err
		}
	}
	return p, nil
}

func priorityName(p string) string {
	if p == "" {
		return ""
	}
	return strings.ToUpper(p[:1]) + p[1:]
}

// jobSlotsView is GET /api/v1/jobs/slots: how many jobs may run, what this
// machine suggests and why, and how full the slots are.
func (s *Server) jobSlotsView(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	memBytes, cores := jobqueue.Machine()
	suggested := jobqueue.Suggest(memBytes, cores)
	out := map[string]any{
		"slots": s.jobSlots(ctx), "suggested": suggested,
		"memory_gb": float64(memBytes>>20) / 1024, "cores": cores,
		"aging_hours": int(s.jobAging(ctx) / time.Hour),
	}
	if n, ok := s.lookupSettingInt64(ctx, settingJobSlots, "", LocalUserID); ok && n >= 1 {
		out["configured"] = n
	}
	out["running"], _ = s.store.CountRunningJobs(ctx)
	queued, _ := s.store.ListQueuedJobs(ctx)
	out["queued"] = len(queued)
	writeJSON(w, http.StatusOK, out)
}

// setJobPriority is PUT /api/v1/jobs/{id}/priority: move a waiting job in the
// line.
func (s *Server) setJobPriority(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Priority string `json:"priority"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
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
	if !s.allowJobControl(w, r, j) {
		return
	}
	wf := s.workflowOf(r.Context(), j.Type)
	if wf != nil && wf.FixedPriority {
		writeMsg(w, http.StatusConflict, "this workflow's jobs keep the priority they were given")
		return
	}
	p, err := s.submitPriority(r, wf, req.Priority)
	if err != nil || req.Priority == "" {
		if err == nil {
			err = fmt.Errorf("priority must be low, medium, high or immediate")
		}
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error(), "field": "priority"})
		return
	}
	ok, err := s.store.SetJobPriority(r.Context(), id, p, time.Now().UTC())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	if !ok {
		writeMsg(w, http.StatusConflict, "only a job waiting in line has a place to change; this one has started or ended")
		return
	}
	s.jobQueue().Kick()
	writeJSON(w, http.StatusOK, s.placeOf(r.Context(), id))
}

// resumeJob is POST /api/v1/jobs/{id}/resume: a paused job, or one a restart
// interrupted, goes back in line and carries on where it stopped.
func (s *Server) resumeJob(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	j, err := s.store.GetJob(ctx, r.PathValue("id"))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	if j == nil {
		writeMsg(w, http.StatusNotFound, "no such job")
		return
	}
	if !s.allowJobControl(w, r, j) {
		return
	}
	if j.Status != domain.JobPaused && j.Status != domain.JobInterrupted {
		writeMsg(w, http.StatusConflict, "only a paused or interrupted job can be resumed")
		return
	}
	if !s.canPause(j.Type) {
		writeMsg(w, http.StatusConflict, "this workflow cannot carry on where it stopped — start it again instead")
		return
	}
	ok, err := s.store.RequeueJob(ctx, j.ID, "", time.Now().UTC())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	if !ok {
		writeMsg(w, http.StatusConflict, "the job changed meanwhile; refresh and try again")
		return
	}
	s.StartJobQueue(context.Background())
	s.jobQueue().Kick()
	writeJSON(w, http.StatusOK, s.placeOf(ctx, j.ID))
}

// ResumableJobTypes are the job types a restart puts back in line rather than
// marking interrupted: those whose workflow can pause and resume.
func ResumableJobTypes(ctx context.Context, store interface {
	ListWorkflows(ctx context.Context) ([]*domain.Workflow, error)
}) []string {
	wfs, err := store.ListWorkflows(ctx)
	if err != nil {
		return nil
	}
	var out []string
	for _, w := range wfs {
		if w.Resumable {
			out = append(out, w.ID)
		}
	}
	return out
}

// ── Runners: each job type, from its row ────────────────────────────────────

// runDecompose reads the document again when the job starts: it may have been
// edited, or the job may be starting after a restart.
func (s *Server) runDecompose(ctx context.Context, j *domain.Job) error {
	params, err := s.store.JobParams(ctx, j.ID)
	if err != nil {
		return err
	}
	item, err := s.store.GetScratchpadItem(ctx, params["source_item"])
	if err != nil || item == nil {
		return fmt.Errorf("the document is no longer there")
	}
	src, label, err := s.documentFromItem(ctx, item)
	if err != nil {
		return err
	}
	gen, _, err := StepClient(ctx, s.store, domain.JobDecompose, 0,
		domain.WorkerDecomposer, j.ProjectID, decomposeCallTimeout)
	if err != nil {
		return err
	}
	if gen == nil {
		return fmt.Errorf("no model is configured for the decomposer — choose one in Settings › Workflows")
	}
	runCtx, cancel := context.WithCancel(ctx)
	s.trackJob(j.ID, cancel)
	defer s.untrackJob(j.ID)
	return s.decomposeNow(runCtx, j, item, src, label, gen, params["again"] == "true" || params["again"] == "1")
}

// runSequence reads the scratchpad's work items when the job starts.
func (s *Server) runSequence(ctx context.Context, j *domain.Job) error {
	params, err := s.store.JobParams(ctx, j.ID)
	if err != nil {
		return err
	}
	spID := params["scratchpad"]
	sp, err := s.store.GetScratchpad(ctx, spID)
	if err != nil || sp == nil {
		return fmt.Errorf("the scratchpad no longer exists")
	}
	runCtx, cancel := context.WithCancel(ctx)
	s.trackJob(j.ID, cancel)
	defer s.untrackJob(j.ID)
	return s.sequenceNow(runCtx, j, sp)
}

// runArchDraft finds the repository's components when the job starts, as the
// panel did when it was asked.
func (s *Server) runArchDraft(ctx context.Context, j *domain.Job) error {
	proj, err := s.store.GetProject(ctx, j.ProjectID)
	if err != nil || proj == nil {
		return fmt.Errorf("the project no longer exists")
	}
	repo, err := git.Open(proj.RepoRoot)
	if err != nil {
		return fmt.Errorf("could not open the repository: %w", err)
	}
	comps := discoverComponents(repo, maxArchComponents)
	runCtx, cancel := context.WithCancel(ctx)
	s.archJobsMu.Lock()
	if s.archRuns == nil {
		s.archRuns = map[string]*archRun{}
	}
	s.archRuns[j.ProjectID] = &archRun{jobID: j.ID, scope: j.ScopeLabel, cancel: cancel}
	s.archJobsMu.Unlock()
	if err := s.store.CreateJob(ctx, j); err != nil {
		cancel()
		return err
	}
	s.runArchEnrichment(runCtx, j.ID, j.ProjectID, comps, j.ScopeLabel)
	return nil
}
