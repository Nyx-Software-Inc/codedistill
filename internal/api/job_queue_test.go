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
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"codedistill/internal/domain"
	"codedistill/internal/storage/sqlite"
)

// queueRig is a server whose "work" jobs are played by a runner the test
// holds: each runs until the test releases it.
type queueRig struct {
	srv     *httptest.Server
	api     *Server
	store   *sqlite.Store
	mu      sync.Mutex
	started []string
	release map[string]chan struct{}
}

const testJobType = "test_work"

func newQueueRig(t *testing.T, slots int) *queueRig {
	t.Helper()
	store, err := sqlite.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	ctx := context.Background()
	if err := store.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	for _, p := range []string{"p1", "p2"} {
		if err := store.CreateProject(ctx, &domain.Project{ID: p, Name: p, CreatedAt: now}); err != nil {
			t.Fatal(err)
		}
	}
	v, _ := json.Marshal(slots)
	if err := store.SetUserSetting(ctx, &domain.UserSetting{UserID: LocalUserID, Key: settingJobSlots, Value: v, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	api := NewServer(store, nil, BuildInfo{Version: "test"}, nil, nil, nil, nil, nil)
	t.Cleanup(api.Close)
	rig := &queueRig{api: api, store: store, release: map[string]chan struct{}{}}
	api.jobQueue().Register(testJobType, func(ctx context.Context, j *domain.Job) error {
		rig.mu.Lock()
		rig.started = append(rig.started, j.ScopeLabel)
		ch := rig.release[j.ScopeLabel]
		rig.mu.Unlock()
		select {
		case <-ch:
		case <-ctx.Done():
		}
		return store.FinishJob(context.WithoutCancel(ctx), j.ID, domain.JobSucceeded, "", time.Now().UTC())
	})
	rig.srv = httptest.NewServer(api.Handler())
	t.Cleanup(rig.srv.Close)
	return rig
}

// submit queues a job named label in a project, as a submit handler does.
func (r *queueRig) submit(t *testing.T, project, label, priority string) (string, queuedPlace) {
	t.Helper()
	r.mu.Lock()
	r.release[label] = make(chan struct{})
	r.mu.Unlock()
	j := &domain.Job{ID: "job-" + label, ProjectID: project, Type: testJobType, ScopeLabel: label, Priority: priority}
	place, err := r.api.enqueueJob(context.Background(), j, map[string]string{"what": label})
	if err != nil {
		t.Fatal(err)
	}
	return j.ID, place
}

func (r *queueRig) startedSoFar() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.started...)
}

func (r *queueRig) finish(label string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	close(r.release[label])
}

func eventually(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out: %s", what)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// The slots are shared by every project; the next job to start is the
// highest priority waiting, whichever project it is in; a waiting job can be
// moved and cancelled, and says where it stands.
func TestJobQueue_SlotsAreSharedAcrossProjects(t *testing.T) {
	rig := newQueueRig(t, 1)
	ctx := context.Background()

	_, first := rig.submit(t, "p1", "first", "")
	if !first.StartsNow || first.Position != 1 || first.Slots != 1 {
		t.Errorf("first place = %+v", first)
	}
	eventually(t, "first to start", func() bool { return len(rig.startedSoFar()) == 1 })

	// Another project gets no slot of its own.
	low, lowPlace := rig.submit(t, "p2", "low", domain.PriorityLow)
	_, _ = rig.submit(t, "p2", "medium", "")
	high, _ := rig.submit(t, "p1", "high", domain.PriorityHigh)
	if lowPlace.StartsNow || lowPlace.Position != 1 || lowPlace.Running != 1 {
		t.Errorf("low place = %+v", lowPlace)
	}
	time.Sleep(100 * time.Millisecond)
	if got := rig.startedSoFar(); len(got) != 1 {
		t.Fatalf("more running than the one slot: %v", got)
	}

	// The job list says where each waiting job stands: high, medium, low.
	var list []jobView
	doJSON(t, rig.srv, "GET", "/api/v1/jobs?project_id=p2", nil, 200, &list)
	pos := map[string]int{}
	for _, v := range list {
		pos[v.ScopeLabel] = v.Position
	}
	if pos["medium"] != 2 || pos["low"] != 3 {
		t.Errorf("positions in p2 = %v (high, in p1, is first)", pos)
	}

	// Moving one up, and refusing what cannot be moved.
	var moved queuedPlace
	doJSON(t, rig.srv, "PUT", "/api/v1/jobs/"+low+"/priority", map[string]string{"priority": "high"}, 200, &moved)
	// Both High now; it joined the line before the other, so it goes first.
	if moved.Position != 1 {
		t.Errorf("low, made high, stands %d; want 1", moved.Position)
	}
	doJSON(t, rig.srv, "PUT", "/api/v1/jobs/job-first/priority", map[string]string{"priority": "low"}, 409, nil)
	doJSON(t, rig.srv, "PUT", "/api/v1/jobs/"+high+"/priority", map[string]string{"priority": "immediate"}, 400, nil)

	// Cancelling a waiting job takes it out of the line; it never runs.
	doJSON(t, rig.srv, "POST", "/api/v1/jobs/"+high+"/cancel", nil, 204, nil)
	if j, _ := rig.store.GetJob(ctx, high); j.Status != domain.JobCancelled {
		t.Fatalf("cancelled job = %s", j.Status)
	}
	doJSON(t, rig.srv, "POST", "/api/v1/jobs/"+low+"/pause", nil, 409, nil)

	var slots map[string]any
	doJSON(t, rig.srv, "GET", "/api/v1/jobs/slots", nil, 200, &slots)
	if slots["slots"] != float64(1) || slots["configured"] != float64(1) || slots["running"] != float64(1) ||
		slots["queued"] != float64(2) || slots["suggested"].(float64) < 1 || slots["aging_hours"] != float64(3) {
		t.Errorf("slots = %v", slots)
	}

	// Each ending starts the next, in order.
	rig.finish("first")
	eventually(t, "low (now high) next", func() bool { s := rig.startedSoFar(); return len(s) == 2 && s[1] == "low" })
	rig.finish("low")
	eventually(t, "medium last", func() bool { s := rig.startedSoFar(); return len(s) == 3 && s[2] == "medium" })
	rig.finish("medium")
	if got := rig.startedSoFar(); len(got) != 3 {
		t.Errorf("started %v; the cancelled job ran", got)
	}
}

// A job left waiting when the server stopped starts when it is back.
func TestJobQueue_WaitingJobsSurviveARestart(t *testing.T) {
	rig := newQueueRig(t, 2)
	ctx := context.Background()
	now := time.Now().UTC().Add(-time.Hour)
	// Written as a previous process left it: queued, its runner long gone.
	left := &domain.Job{ID: "job-left", ProjectID: "p1", Type: testJobType, ScopeLabel: "left",
		Status: domain.JobQueued, Priority: domain.PriorityMedium, QueuedAt: &now, StartedAt: now, UpdatedAt: now}
	if err := rig.store.CreateJob(ctx, left); err != nil {
		t.Fatal(err)
	}
	rig.mu.Lock()
	rig.release["left"] = make(chan struct{})
	rig.mu.Unlock()
	rig.api.StartJobQueue(ctx)
	eventually(t, "the waiting job to start", func() bool { return len(rig.startedSoFar()) == 1 })
	rig.finish("left")
	eventually(t, "it to finish", func() bool {
		j, _ := rig.store.GetJob(ctx, "job-left")
		return j.Status == domain.JobSucceeded
	})
}

// Immediate is admitted when it can start: a slot is free, or a running job
// can pause to make one. Otherwise it is refused, saying why.
func TestJobQueue_ImmediateIsAdmittedOnlyWhenItCanStart(t *testing.T) {
	rig := newQueueRig(t, 1)
	ctx := context.Background()
	r := httptest.NewRequest("POST", "/api/v1/jobs", nil)
	// A workflow allowed Immediate: its configuration, not its name, decides.
	wf := &domain.Workflow{ID: "any", MaxPriority: domain.PriorityImmediate}
	if p, err := rig.api.submitPriority(r, wf, ""); err != nil || p != domain.PriorityMedium {
		t.Errorf("default = %q %v", p, err)
	}
	if _, err := rig.api.submitPriority(r, wf, "urgent"); err == nil {
		t.Error("an unknown priority accepted")
	}
	if p, err := rig.api.submitPriority(r, wf, domain.PriorityImmediate); err != nil || p != domain.PriorityImmediate {
		t.Fatalf("a free slot: %q %v", p, err)
	}

	// The one slot holds a job that cannot pause.
	rig.submit(t, "p1", "busy", "")
	eventually(t, "busy running", func() bool { return len(rig.startedSoFar()) == 1 })
	if _, err := rig.api.submitPriority(r, wf, domain.PriorityImmediate); err == nil ||
		!strings.Contains(err.Error(), "no running job can pause") {
		t.Errorf("nothing can pause: %v", err)
	}
	// It holds an Immediate job.
	rig.store.DB.ExecContext(ctx, `UPDATE jobs SET priority = 'immediate' WHERE id = 'job-busy'`)
	if _, err := rig.api.submitPriority(r, wf, domain.PriorityImmediate); err == nil ||
		!strings.Contains(err.Error(), "every job slot already holds an Immediate job") {
		t.Errorf("all Immediate: %v", err)
	}
	// It holds a fix job, which can pause.
	rig.store.DB.ExecContext(ctx, `UPDATE jobs SET priority = 'low', type = ? WHERE id = 'job-busy'`, domain.JobFixFindings)
	if _, err := rig.api.submitPriority(r, wf, domain.PriorityImmediate); err != nil {
		t.Errorf("a fix job can make room: %v", err)
	}
	rig.store.DB.ExecContext(ctx, `UPDATE jobs SET type = ? WHERE id = 'job-busy'`, testJobType)
	rig.finish("busy")
}

// A paused fix job resumes: back in line, then started again. Other kinds
// cannot resume in place and are refused.
func TestJobQueue_Resume(t *testing.T) {
	rig := newQueueRig(t, 1)
	ctx := context.Background()
	now := time.Now().UTC()
	for _, j := range []*domain.Job{
		{ID: "fix-paused", ProjectID: "p1", Type: domain.JobFixFindings, ScopeLabel: "fix", Status: domain.JobRunning, StartedAt: now, UpdatedAt: now},
		{ID: "doc-paused", ProjectID: "p1", Type: domain.JobDecompose, ScopeLabel: "doc", Status: domain.JobRunning, StartedAt: now, UpdatedAt: now},
	} {
		if err := rig.store.CreateJob(ctx, j); err != nil {
			t.Fatal(err)
		}
		rig.store.PauseJob(ctx, j.ID, domain.PausedByPerson, now)
	}
	// The fix job's runner is the test's, so resuming it is observable.
	rig.api.jobQueue().Register(domain.JobFixFindings, func(ctx context.Context, j *domain.Job) error {
		rig.mu.Lock()
		rig.started = append(rig.started, j.ScopeLabel)
		rig.mu.Unlock()
		return rig.store.FinishJob(ctx, j.ID, domain.JobSucceeded, "", time.Now().UTC())
	})
	doJSON(t, rig.srv, "POST", "/api/v1/jobs/doc-paused/resume", nil, 409, nil)
	var place queuedPlace
	doJSON(t, rig.srv, "POST", "/api/v1/jobs/fix-paused/resume", nil, 200, &place)
	if place.Position != 1 {
		t.Errorf("place = %+v", place)
	}
	eventually(t, "the fix job to resume", func() bool {
		j, _ := rig.store.GetJob(ctx, "fix-paused")
		return j.Status == domain.JobSucceeded
	})
	doJSON(t, rig.srv, "POST", "/api/v1/jobs/fix-paused/resume", nil, 409, nil)
}

// A workflow's configuration decides how high its jobs may go, whether their
// priority can change, and whether they can pause; nothing names a workflow.
func TestJobQueue_WorkflowSettingsDecide(t *testing.T) {
	rig := newQueueRig(t, 1)
	ctx := context.Background()
	r := httptest.NewRequest("POST", "/api/v1/jobs", nil)
	if _, err := rig.api.submitPriority(r, &domain.Workflow{ID: "w"}, domain.PriorityImmediate); err == nil ||
		!strings.Contains(err.Error(), "no higher than High") {
		t.Errorf("a workflow allowed High only: %v", err)
	}
	if p, err := rig.api.submitPriority(r, &domain.Workflow{ID: "w"}, domain.PriorityHigh); err != nil || p != domain.PriorityHigh {
		t.Errorf("high: %q %v", p, err)
	}

	// A workflow of its own, with fixed priority, that can pause.
	now := time.Now().UTC()
	if err := rig.store.CreateWorkflow(ctx, &domain.Workflow{ID: testJobType, Name: "Test work", Enabled: true,
		Resumable: true, FixedPriority: true, MaxPriority: domain.PriorityHigh, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	if !rig.api.canPause(testJobType) || !rig.api.fixedPriority(testJobType) {
		t.Error("the workflow's settings were not read")
	}
	if rig.api.canPause(domain.JobDecompose) {
		t.Error("splitting a document cannot carry on the same job, so it must not claim to pause")
	}
	rig.submit(t, "p1", "holding", "")
	eventually(t, "holding to start", func() bool { return len(rig.startedSoFar()) == 1 })
	id, _ := rig.submit(t, "p1", "waiting", domain.PriorityLow)
	doJSON(t, rig.srv, "PUT", "/api/v1/jobs/"+id+"/priority", map[string]string{"priority": "high"}, 409, nil)
	rig.finish("holding")
	rig.finish("waiting")
}
