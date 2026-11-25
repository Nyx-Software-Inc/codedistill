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
	"net/http/httptest"
	"testing"
	"time"

	"codedistill/internal/domain"
	"codedistill/internal/storage/sqlite"
)

func TestJobsAnalytics(t *testing.T) {
	ctx := context.Background()
	store, err := sqlite.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	if err := store.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	for _, p := range []*domain.Project{{ID: "p1", Name: "Locator", CreatedAt: now}, {ID: "p2", Name: "Hemera", CreatedAt: now}} {
		if err := store.CreateProject(ctx, p); err != nil {
			t.Fatal(err)
		}
	}
	ago := func(d time.Duration) time.Time { return now.Add(-d) }
	fin := ago(10 * time.Minute)
	q := ago(5 * time.Minute)
	for _, j := range []*domain.Job{
		{ID: "j1", ProjectID: "p1", Type: domain.JobFixFindings, ScopeLabel: "Bugs", Status: domain.JobSucceeded,
			StartedAt: ago(50 * time.Minute), UpdatedAt: fin, FinishedAt: &fin},
		{ID: "j2", ProjectID: "p2", Type: domain.JobFixFindings, ScopeLabel: "Security", Status: domain.JobRunning,
			StartedAt: ago(20 * time.Minute), UpdatedAt: now},
		{ID: "j3", ProjectID: "p2", Type: domain.JobSequence, ScopeLabel: "Order", Status: domain.JobQueued,
			StartedAt: q, UpdatedAt: q, QueuedAt: &q},
	} {
		if err := store.CreateJob(ctx, j); err != nil {
			t.Fatal(err)
		}
	}
	n := func(v int64) *int64 { return &v }
	for _, r := range []*domain.AgentRun{
		{ID: "r1", JobID: "j1", Role: "solutioner", Agent: "claude-code", Model: "claude-opus-5-5", Outcome: "committed",
			StartedAt: ago(40 * time.Minute), DurationMS: 60000, TokensIn: n(100), TokensOut: n(900), CacheRead: n(9000)},
		{ID: "r2", JobID: "j1", Role: "challenger", Lens: "security", Agent: "built-in", Model: "qwen2.5:7b", Outcome: "verdicts",
			StartedAt: ago(30 * time.Minute), DurationMS: 60000, TokensIn: n(2000), TokensOut: n(100)},
		{ID: "r3", JobID: "j2", Role: "solutioner", Agent: "claude-code", Model: "claude-opus-5-5", Outcome: "planned",
			StartedAt: ago(15 * time.Minute), DurationMS: 60000, TokensIn: n(50), TokensOut: n(450), CacheRead: n(4500)},
		// Ended long before the last hour: not counted in the 1h window.
		{ID: "r0", JobID: "j1", Role: "solutioner", Agent: "claude-code", Model: "claude-opus-5-5", Outcome: "planned",
			StartedAt: ago(3 * time.Hour), DurationMS: 60000, TokensIn: n(77777)},
	} {
		if err := store.CreateAgentRun(ctx, r); err != nil {
			t.Fatal(err)
		}
	}
	// A local model server with one model, half in GPU memory.
	ollama := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/ps" {
			w.Write([]byte(`{"models":[{"name":"qwen2.5:7b","size":4700000000,"size_vram":2000000000}]}`))
		}
	}))
	defer ollama.Close()
	if err := store.CreateModelProvider(ctx, &domain.ModelProvider{ID: "local", Name: "Local", Protocol: "ollama",
		Endpoint: ollama.URL, Model: "qwen2.5:7b", Enabled: true, IsLocal: true, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	s := NewServer(store, nil, BuildInfo{}, nil, nil, nil, nil, nil)
	t.Cleanup(s.Close)
	srv := httptest.NewServer(s.Handler())
	defer srv.Close()

	var a struct {
		Window   string `json:"window"`
		Running  int    `json:"running"`
		Queued   int    `json:"queued"`
		Timeline []struct {
			Running, Queued int
		} `json:"timeline"`
		Tokens struct {
			Total   tokenCounts     `json:"total"`
			ByJob   []tokensByJob   `json:"by_job"`
			ByRole  []tokensByRole  `json:"by_role"`
			ByModel []tokensByModel `json:"by_model"`
		} `json:"tokens"`
	}
	doJSON(t, srv, "GET", "/api/v1/jobs/analytics?window=1h", nil, 200, &a)
	if a.Window != "1h" || a.Running != 1 || a.Queued != 1 || len(a.Timeline) != 60 {
		t.Fatalf("now: %+v (timeline %d)", a, len(a.Timeline))
	}
	last := a.Timeline[len(a.Timeline)-1]
	if last.Running != 1 || last.Queued != 1 {
		t.Errorf("latest slice = %+v", last)
	}
	if a.Tokens.Total.Total != 100+900+9000+2000+100+50+450+4500 {
		t.Errorf("total = %+v (the run from three hours ago must not count)", a.Tokens.Total)
	}
	if len(a.Tokens.ByJob) != 2 || a.Tokens.ByJob[0].JobID != "j1" || a.Tokens.ByJob[0].Project != "Locator" || a.Tokens.ByJob[0].Label != "Bugs" {
		t.Errorf("by job = %+v", a.Tokens.ByJob)
	}
	if len(a.Tokens.ByRole) != 2 || a.Tokens.ByRole[0].Role != "solutioner" || a.Tokens.ByRole[0].Total != 15000 {
		t.Errorf("by role = %+v", a.Tokens.ByRole)
	}
	if len(a.Tokens.ByModel) != 2 || a.Tokens.ByModel[0].Model != "claude-opus-5-5" {
		t.Errorf("by model = %+v", a.Tokens.ByModel)
	}

	var m struct {
		Samples []struct {
			CPUPercent float64 `json:"cpu_percent"`
			MemTotal   uint64  `json:"mem_total"`
		} `json:"samples"`
		GPU struct {
			Readable bool
			Why      string
		} `json:"gpu"`
		Models []loadedModel `json:"models"`
	}
	doJSON(t, srv, "GET", "/api/v1/machine", nil, 200, &m)
	if len(m.Models) != 1 || m.Models[0].On != "split" || m.Models[0].Name != "qwen2.5:7b" {
		t.Errorf("models = %+v", m.Models)
	}
	// The first look starts sampling; a sample follows within a moment.
	deadline := time.Now().Add(3 * time.Second)
	for len(m.Samples) == 0 && time.Now().Before(deadline) {
		time.Sleep(50 * time.Millisecond)
		doJSON(t, srv, "GET", "/api/v1/machine", nil, 200, &m)
	}
	if len(m.Samples) == 0 || m.Samples[0].MemTotal == 0 {
		t.Errorf("no machine sample: %+v", m.Samples)
	}
	if !m.GPU.Readable && m.GPU.Why == "" {
		t.Error("the GPU is neither read nor explained")
	}
}
