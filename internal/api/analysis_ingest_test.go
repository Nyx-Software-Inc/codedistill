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
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"codedistill/internal/domain"
	"codedistill/internal/events"
	"codedistill/internal/storage/sqlite"
)

func ingestServer(t *testing.T) (*httptest.Server, *sqlite.Store) {
	t.Helper()
	store, err := sqlite.Open(":memory:")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { store.Close() })
	if err := store.Migrate(context.Background()); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	srv := httptest.NewServer(
		NewServer(store, nil, BuildInfo{}, nil, nil, nil, nil, nil).
			WithEventBus(events.NewBus()).Handler())
	t.Cleanup(srv.Close)
	if err := store.CreateProject(context.Background(),
		&domain.Project{ID: "p1", Name: "P", CreatedAt: time.Now()}); err != nil {
		t.Fatalf("project: %v", err)
	}
	return srv, store
}

const ingestSARIF = `{"version":"2.1.0","runs":[{
  "tool":{"driver":{"name":"semgrep","rules":[
    {"id":"kotlin.sqli","properties":{"tags":["security"],"security-severity":"8.2"},
     "defaultConfiguration":{"level":"error"}}]}},
  "results":[{"ruleId":"kotlin.sqli","level":"error","message":{"text":"SQL injection"},
    "locations":[{"physicalLocation":{"artifactLocation":{"uri":"app/Db.kt"},
      "region":{"startLine":5,"endLine":5}}}]}]}]}`

const emptySARIF = `{"version":"2.1.0","runs":[{"tool":{"driver":{"name":"semgrep"}},"results":[]}]}`

func TestIngestSARIF_EndToEnd(t *testing.T) {
	srv, _ := ingestServer(t)

	// Ingest one security finding.
	var out struct{ Findings, New, Resolved int }
	post(t, srv, "/api/v1/projects/p1/analysis/ingest", ingestSARIF, &out)
	if out.New != 1 || out.Findings != 1 {
		t.Fatalf("ingest = %+v, want 1 new / 1 finding", out)
	}

	// It shows up on the findings page with the SARIF-derived taxonomy + source=ci.
	var findings []domain.CodeFinding
	getJSON(t, srv, "/api/v1/projects/p1/analysis/findings", &findings)
	if len(findings) != 1 {
		t.Fatalf("want 1 finding listed, got %d", len(findings))
	}
	f := findings[0]
	if f.Category != "security" || f.SecuritySeverity != 8.2 || f.Source != "ci" ||
		f.RuleID != "kotlin.sqli" || f.FilePath != "app/Db.kt" || f.Severity != "high" {
		t.Fatalf("ingested finding wrong: %+v", f)
	}

	// Re-ingest an empty result (the issue was fixed) → it resolves, source-scoped.
	post(t, srv, "/api/v1/projects/p1/analysis/ingest", emptySARIF, &out)
	if out.Resolved != 1 {
		t.Errorf("empty re-ingest should resolve 1, got %+v", out)
	}
	getJSON(t, srv, "/api/v1/projects/p1/analysis/findings", &findings)
	if len(findings) != 0 {
		t.Errorf("resolved finding should leave the page, got %d", len(findings))
	}
}

// TestIngestSARIF_AutoRoute proves the Enterprise auto-route: with a target
// scratchpad configured, an ingested high-severity finding becomes a scratchpad
// item (and leaves the findings page, now pushed).
func TestIngestSARIF_AutoRoute(t *testing.T) {
	srv, store := ingestServer(t)
	ctx := context.Background()
	now := time.Now().UTC()
	if err := store.CreateScratchpad(ctx, &domain.Scratchpad{
		ID: "sp1", ProjectID: "p1", Name: "Issues", ClassificationMode: "full", CreatedAt: now,
	}); err != nil {
		t.Fatalf("scratchpad: %v", err)
	}
	v, _ := json.Marshal("Issues") // route by scratchpad NAME
	if err := store.SetProjectSetting(ctx, &domain.ProjectSetting{
		ProjectID: "p1", Key: "analysis.findings_target_scratchpad", Value: v,
	}); err != nil {
		t.Fatalf("setting: %v", err)
	}

	// Ingest a high-severity finding → auto-routed to sp1 (default min = high).
	post(t, srv, "/api/v1/projects/p1/analysis/ingest", ingestSARIF, nil)

	items, err := store.ListScratchpadItems(ctx, "sp1")
	if err != nil {
		t.Fatalf("list items: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("want 1 auto-routed item, got %d", len(items))
	}
	// The routed finding is now pushed, so it's off the findings page.
	var findings []domain.CodeFinding
	getJSON(t, srv, "/api/v1/projects/p1/analysis/findings", &findings)
	if len(findings) != 0 {
		t.Errorf("routed finding should have left the page, got %d", len(findings))
	}
}

func post(t *testing.T, srv *httptest.Server, path, body string, out any) {
	t.Helper()
	resp, err := http.Post(srv.URL+path, "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("post %s: %v", path, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("post %s: status %d", path, resp.StatusCode)
	}
	if out != nil {
		if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
			t.Fatalf("decode %s: %v", path, err)
		}
	}
}

func getJSON(t *testing.T, srv *httptest.Server, path string, out any) {
	t.Helper()
	resp, err := http.Get(srv.URL + path)
	if err != nil {
		t.Fatalf("get %s: %v", path, err)
	}
	defer resp.Body.Close()
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		t.Fatalf("decode %s: %v", path, err)
	}
}
