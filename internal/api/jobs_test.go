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
	"strings"
	"testing"
	"time"

	"codedistill/internal/domain"
)

func seedJobFor(t *testing.T, store interface {
	CreateJob(context.Context, *domain.Job) error
}, id, projectID, jobType, status string, done, total int) *domain.Job {
	t.Helper()
	now := time.Now().UTC().Add(-5 * time.Minute)
	j := &domain.Job{
		ID: id, ProjectID: projectID, Type: jobType,
		ScopeKind: "scratchpad_item", ScopeID: "doc-1", ScopeLabel: "spec.odt",
		Status: status, Phase: "reading", Done: done, Total: total,
		StartedAt: now, UpdatedAt: now,
	}
	if err := store.CreateJob(context.Background(), j); err != nil {
		t.Fatalf("seed job: %v", err)
	}
	return j
}

// A total that is not yet known must render as indeterminate, never as zero.
// A bar pinned at 0% for four minutes reads as broken, and one that jumps when
// the denominator arrives reads as a lie.
func TestJobListDistinguishesUnknownProgressFromNone(t *testing.T) {
	srv, store := newTestServer(t)
	ctx := context.Background()
	if err := store.CreateProject(ctx, &domain.Project{ID: "p1", Name: "P", CreatedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	seedJobFor(t, store, "j-unknown", "p1", domain.JobDecompose, domain.JobRunning, 0, 0)
	seedJobFor(t, store, "j-started", "p1", domain.JobDecompose, domain.JobRunning, 0, 5)

	var out []map[string]any
	doJSON(t, srv, "GET", "/api/v1/jobs?project_id=p1", nil, 200, &out)
	if len(out) != 2 {
		t.Fatalf("got %d jobs", len(out))
	}
	byID := map[string]map[string]any{}
	for _, j := range out {
		byID[j["id"].(string)] = j
	}
	if got := byID["j-unknown"]["percent"]; got != float64(-1) {
		t.Errorf("unsized job reports percent %v, want -1 (indeterminate)", got)
	}
	if got := byID["j-started"]["percent"]; got != float64(0) {
		t.Errorf("sized job with no progress reports %v, want 0", got)
	}
}

// A job running for twenty minutes is a different conversation depending on
// whether it is a local 7B or a hosted model, so the monitor says which.
func TestJobListNamesTheWorkerAndModel(t *testing.T) {
	srv, store := newTestServer(t)
	ctx := context.Background()
	if err := store.CreateProject(ctx, &domain.Project{ID: "p1", Name: "P", CreatedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	p := &domain.ModelProvider{
		ID: "prov1", Name: "Claude", Protocol: "anthropic",
		Endpoint: "https://api.anthropic.com/v1", Model: "claude-x",
		ContextTokens: 200000, IsLocal: false, Enabled: true,
		CreatedAt: now, UpdatedAt: now,
	}
	if err := store.CreateModelProvider(ctx, p); err != nil {
		t.Fatal(err)
	}
	if err := store.SetWorkerModel(ctx, domain.WorkerDecomposer, "", p.ID, now); err != nil {
		t.Fatal(err)
	}
	seedJobFor(t, store, "j1", "p1", domain.JobDecompose, domain.JobRunning, 2, 5)

	var out []map[string]any
	doJSON(t, srv, "GET", "/api/v1/jobs?project_id=p1", nil, 200, &out)
	if len(out) != 1 {
		t.Fatalf("got %d jobs", len(out))
	}
	if out[0]["worker_type"] != domain.WorkerDecomposer {
		t.Errorf("worker_type = %v", out[0]["worker_type"])
	}
	if out[0]["provider_name"] != "Claude" {
		t.Errorf("provider_name = %v; the monitor cannot say which model is working", out[0]["provider_name"])
	}
	// A non-local provider must be flagged: a document leaving the machine is
	// not something to discover afterwards.
	if out[0]["provider_local"] != false && out[0]["provider_local"] != nil {
		t.Errorf("provider_local = %v, want false for a hosted model", out[0]["provider_local"])
	}
	if out[0]["elapsed_seconds"].(float64) < 200 {
		t.Errorf("elapsed = %v, want ~300 for a job started five minutes ago", out[0]["elapsed_seconds"])
	}
}

// Cancelling twice must not fail: two clicks on a slow list are not an error.
func TestCancelJobIsIdempotent(t *testing.T) {
	srv, store := newTestServer(t)
	ctx := context.Background()
	if err := store.CreateProject(ctx, &domain.Project{ID: "p1", Name: "P", CreatedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	seedJobFor(t, store, "j1", "p1", domain.JobDecompose, domain.JobRunning, 1, 5)

	doJSON(t, srv, "POST", "/api/v1/jobs/j1/cancel", nil, 204, nil)
	got, _ := store.GetJob(ctx, "j1")
	if got.Status != domain.JobCancelled {
		t.Fatalf("status = %q, want cancelled", got.Status)
	}
	// Second click: already finished, still no error.
	doJSON(t, srv, "POST", "/api/v1/jobs/j1/cancel", nil, 204, nil)

	doJSON(t, srv, "POST", "/api/v1/jobs/nope/cancel", nil, 404, nil)
}

// Jobs are per project. A monitor that leaks another project's work is worse
// than no monitor.
func TestJobListIsScopedToTheProject(t *testing.T) {
	srv, store := newTestServer(t)
	ctx := context.Background()
	for _, id := range []string{"p1", "p2"} {
		if err := store.CreateProject(ctx, &domain.Project{ID: id, Name: id, CreatedAt: time.Now()}); err != nil {
			t.Fatal(err)
		}
	}
	seedJobFor(t, store, "j1", "p1", domain.JobDecompose, domain.JobRunning, 1, 5)
	seedJobFor(t, store, "j2", "p2", domain.JobArchDraft, domain.JobRunning, 1, 5)

	var out []map[string]any
	doJSON(t, srv, "GET", "/api/v1/jobs?project_id=p1", nil, 200, &out)
	if len(out) != 1 || out[0]["id"] != "j1" {
		t.Fatalf("project scoping leaked: %+v", out)
	}
	doJSON(t, srv, "GET", "/api/v1/jobs", nil, 400, nil)
}

// A Pause button that silently means Cancel is worse than no button.
func TestPauseIsRefusedForANonResumableWorkflow(t *testing.T) {
	srv, store := newTestServer(t)
	ctx := context.Background()
	if err := store.CreateProject(ctx, &domain.Project{ID: "p1", Name: "P", CreatedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	// A workflow that cannot continue where it left off.
	if err := store.CreateWorkflow(ctx, &domain.Workflow{
		ID: "one-shot", Name: "One shot", Resumable: false, Enabled: true,
		CreatedAt: now, UpdatedAt: now,
		Steps: []domain.WorkflowStep{{WorkerType: domain.WorkerSolutioner}},
	}); err != nil {
		t.Fatal(err)
	}
	seedJobFor(t, store, "j1", "p1", "one-shot", domain.JobRunning, 1, 5)

	doJSON(t, srv, "POST", "/api/v1/jobs/j1/pause", nil, 409, nil)
	got, _ := store.GetJob(ctx, "j1")
	if got.Status != domain.JobRunning {
		t.Fatalf("status = %q; a refused pause must leave the run alone", got.Status)
	}

	// Decompose declares itself resumable, so it pauses.
	seedJobFor(t, store, "j2", "p1", domain.JobDecompose, domain.JobRunning, 1, 5)
	var out map[string]any
	doJSON(t, srv, "POST", "/api/v1/jobs/j2/pause", nil, 200, &out)
	if out["status"] != domain.JobPaused {
		t.Fatalf("status = %v", out["status"])
	}
	// The response must not imply the stop was instant.
	if note, _ := out["note"].(string); !strings.Contains(note, "next checkpoint") {
		t.Errorf("note = %q; pausing is not instant and the UI must not imply it is", note)
	}
	paused, _ := store.GetJob(ctx, "j2")
	if !paused.Resumable() {
		t.Error("a paused job does not report itself resumable")
	}
}

// Elapsed time carries no verdict until you know what a run usually costs.
func TestJobDetailCarriesPhasesAndNorms(t *testing.T) {
	srv, store := newTestServer(t)
	ctx := context.Background()
	if err := store.CreateProject(ctx, &domain.Project{ID: "p1", Name: "P", CreatedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	base := time.Now().UTC().Add(-time.Hour)

	// One finished run, to establish the norm.
	seedJobFor(t, store, "past", "p1", domain.JobDecompose, domain.JobRunning, 5, 5)
	if err := store.StartJobPhase(ctx, &domain.JobPhase{
		ID: "ph-past", JobID: "past", Phase: "reading", WorkerType: domain.WorkerDecomposer,
	}, base); err != nil {
		t.Fatal(err)
	}
	if err := store.FinishJobPhases(ctx, "past", "", base.Add(20*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if err := store.FinishJob(ctx, "past", domain.JobSucceeded, "", base.Add(20*time.Minute)); err != nil {
		t.Fatal(err)
	}

	// And one in flight.
	seedJobFor(t, store, "now", "p1", domain.JobDecompose, domain.JobRunning, 2, 5)
	if err := store.StartJobPhase(ctx, &domain.JobPhase{
		ID: "ph-now", JobID: "now", Phase: "reading", WorkerType: domain.WorkerDecomposer,
	}, time.Now().UTC().Add(-8*time.Minute)); err != nil {
		t.Fatal(err)
	}

	var out map[string]any
	doJSON(t, srv, "GET", "/api/v1/jobs/now", nil, 200, &out)

	phases, _ := out["phases"].([]any)
	if len(phases) != 1 {
		t.Fatalf("phases = %v", out["phases"])
	}
	ph := phases[0].(map[string]any)
	if ph["running"] != true {
		t.Error("the in-flight phase is not marked running")
	}
	if ph["seconds"].(float64) < 400 {
		t.Errorf("seconds = %v, want ~480", ph["seconds"])
	}

	norms, _ := out["norms"].([]any)
	if len(norms) != 1 {
		t.Fatalf("norms = %v — without a norm, elapsed time has no verdict", out["norms"])
	}
	if n := norms[0].(map[string]any); n["median_sec"].(float64) != 1200 {
		t.Errorf("median = %v, want 1200", n["median_sec"])
	}
}
