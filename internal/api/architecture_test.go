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
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"codedistill/internal/domain"
	"codedistill/internal/mcpmap"
	"codedistill/internal/storage/sqlite"
)

func archServer(t *testing.T, sug mcpmap.Suggester) (*httptest.Server, *sqlite.Store) {
	t.Helper()
	store, err := sqlite.Open(":memory:")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { store.Close() })
	if err := store.Migrate(context.Background()); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	srv := httptest.NewServer(NewServer(store, nil, BuildInfo{}, nil, nil, sug, nil, nil).Handler())
	t.Cleanup(srv.Close)
	if err := store.CreateProject(context.Background(), &domain.Project{ID: "p1", Name: "P", CreatedAt: time.Now()}); err != nil {
		t.Fatalf("project: %v", err)
	}
	return srv, store
}

func TestArchitecture_DraftRatifyEdit(t *testing.T) {
	sug := &fakeSuggester{resp: `{"nodes":[
		{"name":"API","kind":"service","description":"the HTTP layer","area":"internal/api"},
		{"name":"DB","kind":"store","description":"sqlite"}],
		"edges":[{"from":"API","to":"DB","label":"persists to"}]}`}
	srv, _ := archServer(t, sug)

	// Draft → 2 proposed nodes + 1 proposed edge.
	var resp architectureResp
	doJSON(t, srv, "POST", "/api/v1/projects/p1/architecture/draft", nil, 200, &resp)
	if len(resp.Nodes) != 2 || len(resp.Edges) != 1 {
		t.Fatalf("draft = %d nodes, %d edges; want 2,1", len(resp.Nodes), len(resp.Edges))
	}
	for _, n := range resp.Nodes {
		if n.Provenance != "proposed" {
			t.Errorf("node %s should be proposed", n.Name)
		}
	}
	var apiNode *domain.ArchitectureNode
	for _, n := range resp.Nodes {
		if n.Name == "API" {
			apiNode = n
		}
	}
	if apiNode == nil || apiNode.Kind != "service" || apiNode.Area != "internal/api" {
		t.Fatalf("API node wrong: %+v", apiNode)
	}

	// Edit + ratify one node.
	var edited domain.ArchitectureNode
	doJSON(t, srv, "PATCH", "/api/v1/architecture/nodes/"+apiNode.ID,
		map[string]any{"description": "edited", "provenance": "ratified"}, 200, &edited)
	if edited.Description != "edited" || edited.Provenance != "ratified" {
		t.Errorf("edit not applied: %+v", edited)
	}

	// Ratify all → everything ratified.
	doJSON(t, srv, "POST", "/api/v1/projects/p1/architecture/ratify", nil, 200, &resp)
	for _, n := range resp.Nodes {
		if n.Provenance != "ratified" {
			t.Errorf("node %s still proposed after ratify-all", n.Name)
		}
	}
	for _, e := range resp.Edges {
		if e.Provenance != "ratified" {
			t.Errorf("edge still proposed after ratify-all")
		}
	}

	// Delete a node removes it + its edges.
	doJSON(t, srv, "DELETE", "/api/v1/architecture/nodes/"+apiNode.ID, nil, 204, nil)
	doJSON(t, srv, "GET", "/api/v1/projects/p1/architecture", nil, 200, &resp)
	if len(resp.Nodes) != 1 || len(resp.Edges) != 0 {
		t.Errorf("after delete: %d nodes, %d edges; want 1,0", len(resp.Nodes), len(resp.Edges))
	}
}

func TestArchitecture_DraftNoModel(t *testing.T) {
	srv, _ := archServer(t, nil)
	doJSON(t, srv, "POST", "/api/v1/projects/p1/architecture/draft", nil, 503, nil)
}

func TestComputeArchDelta(t *testing.T) {
	paths := []string{"internal/api/server.go", "internal/verify/runner.go", "web/src/App.svelte"}
	topDirs := []string{"internal", "web", "docs"}
	nodes := []*domain.ArchitectureNode{
		{ID: "n1", Area: "internal/api"},  // code exists → matched (covers "internal")
		{ID: "n2", Area: "internal/nope"}, // claims code that isn't there → missing
		{ID: "n3", Area: ""},              // no area → unmapped
	}
	d := computeArchDelta(paths, topDirs, nodes)
	if d.NodeStatus["n1"] != "matched" || d.NodeStatus["n2"] != "missing" || d.NodeStatus["n3"] != "unmapped" {
		t.Fatalf("node_status = %+v", d.NodeStatus)
	}
	// "internal" is covered by n1; "web" and "docs" have no component.
	got := map[string]bool{}
	for _, a := range d.UncoveredAreas {
		got[a] = true
	}
	if !got["web"] || !got["docs"] || got["internal"] {
		t.Errorf("uncovered = %v, want web+docs (not internal)", d.UncoveredAreas)
	}
}

// initArchRepo builds a tiny git repo with two depth-2 components.
func initArchRepo(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", root}, args...)...)
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
		}
	}
	run("init", "-q")
	for p, body := range map[string]string{
		"internal/api/server.go": "package api\n",
		"internal/store/db.go":   "package store\n",
		"internal/store/sql.go":  "package store\n",
	} {
		if err := os.MkdirAll(filepath.Join(root, filepath.Dir(p)), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, p), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	run("add", "-A")
	run("commit", "-q", "-m", "initial")
	return root
}

// Async draft: the skeleton (all components, path-derived, empty description)
// returns immediately; the background job enriches; auto mode resumes instead
// of deleting; redraft is the only destructive path.
func TestArchitecture_AsyncDraftSkeletonAndEnrich(t *testing.T) {
	sug := &fakeSuggester{resp: `{"name":"Enriched","kind":"service","description":"does things"}`}
	srv, store := archServer(t, sug)
	root := initArchRepo(t)
	if err := store.UpdateProject(context.Background(), &domain.Project{ID: "p1", Name: "P", RepoRoot: root}); err != nil {
		t.Fatalf("set repo: %v", err)
	}

	// Draft returns the full skeleton immediately — components as path-derived
	// nodes with empty descriptions (the not-yet-characterized marker).
	var resp architectureResp
	doJSON(t, srv, "POST", "/api/v1/projects/p1/architecture/draft", nil, 200, &resp)
	if len(resp.Nodes) != 2 {
		t.Fatalf("skeleton nodes = %d, want 2 (internal/api + internal/store)", len(resp.Nodes))
	}
	if resp.DraftJob == nil {
		t.Fatal("draft response missing job status")
	}

	// The background job enriches both nodes.
	deadline := time.Now().Add(5 * time.Second)
	for {
		doJSON(t, srv, "GET", "/api/v1/projects/p1/architecture", nil, 200, &resp)
		if resp.UnenrichedNodes == 0 && resp.DraftJob != nil && !resp.DraftJob.Running {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("enrichment never finished: job=%+v unenriched=%d", resp.DraftJob, resp.UnenrichedNodes)
		}
		time.Sleep(20 * time.Millisecond)
	}
	for _, n := range resp.Nodes {
		if n.Name != "Enriched" || n.Description != "does things" {
			t.Errorf("node not enriched: %+v", n)
		}
	}

	// Auto re-draft with a fully enriched diagram is a non-destructive no-op.
	doJSON(t, srv, "POST", "/api/v1/projects/p1/architecture/draft", nil, 200, &resp)
	if len(resp.Nodes) != 2 || resp.Nodes[0].Description == "" {
		t.Fatalf("auto draft clobbered enriched diagram: %+v", resp.Nodes)
	}

	// Explicit redraft rebuilds the skeleton (and re-enriches).
	doJSON(t, srv, "POST", "/api/v1/projects/p1/architecture/draft", map[string]string{"mode": "redraft"}, 200, &resp)
	if len(resp.Nodes) != 2 {
		t.Fatalf("redraft nodes = %d, want 2", len(resp.Nodes))
	}
}

// A node whose enrichment failed keeps its empty description, and auto draft
// resumes it rather than starting over.
func TestArchitecture_AsyncDraftResume(t *testing.T) {
	sug := &failingSuggester{}
	srv, store := archServer(t, sug)
	root := initArchRepo(t)
	if err := store.UpdateProject(context.Background(), &domain.Project{ID: "p1", Name: "P", RepoRoot: root}); err != nil {
		t.Fatalf("set repo: %v", err)
	}

	var resp architectureResp
	doJSON(t, srv, "POST", "/api/v1/projects/p1/architecture/draft", nil, 200, &resp)
	deadline := time.Now().Add(5 * time.Second)
	for {
		doJSON(t, srv, "GET", "/api/v1/projects/p1/architecture", nil, 200, &resp)
		if resp.DraftJob != nil && !resp.DraftJob.Running {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("job never finished")
		}
		time.Sleep(20 * time.Millisecond)
	}
	// All enrichment failed → skeleton survives, honestly unenriched.
	if resp.UnenrichedNodes != 2 {
		t.Fatalf("unenriched = %d, want 2 after total model failure", resp.UnenrichedNodes)
	}
	ids := map[string]bool{}
	for _, n := range resp.Nodes {
		ids[n.ID] = true
	}

	// Auto draft now RESUMES: same nodes (not recreated), a fresh job retries.
	doJSON(t, srv, "POST", "/api/v1/projects/p1/architecture/draft", nil, 200, &resp)
	for _, n := range resp.Nodes {
		if !ids[n.ID] {
			t.Errorf("resume recreated node %s — should reuse the skeleton", n.ID)
		}
	}
}

// failingSuggester always errors — the model-unreachable mode.
type failingSuggester struct{}

func (f *failingSuggester) GenerateJSON(ctx context.Context, prompt string) (string, error) {
	return "", context.DeadlineExceeded
}

func TestInArchScope(t *testing.T) {
	cases := []struct {
		area, scope string
		want        bool
	}{
		{"core/data", "", true},
		{"core/data", "core", true},
		{"core", "core", true},
		{"coretools/x", "core", false},
		{"feature/foryou", "core", false},
	}
	for _, c := range cases {
		if got := inArchScope(c.area, c.scope); got != c.want {
			t.Errorf("inArchScope(%q,%q) = %v, want %v", c.area, c.scope, got, c.want)
		}
	}
}
