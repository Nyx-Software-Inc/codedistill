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
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	mcpserver "github.com/mark3labs/mcp-go/server"

	"codedistill/internal/agent"
	"codedistill/internal/domain"
	"codedistill/internal/mcp"
	"codedistill/internal/mcpclient"
	"codedistill/internal/mcpmap"
	"codedistill/internal/mcpworker"
	"codedistill/internal/storage/sqlite"
)

// fakeSuggester returns canned JSON for Suggest calls.
type fakeSuggester struct{ resp string; err error }

func (f *fakeSuggester) GenerateJSON(ctx context.Context, prompt string) (string, error) {
	return f.resp, f.err
}

// startCodedistillMCP spins up an in-process CodeDistill MCP server
// over HTTP via httptest. Provides a real MCP destination the
// /mcp-export/discover endpoint can probe.
func startCodedistillMCP(t *testing.T) string {
	t.Helper()
	store, err := sqlite.Open(":memory:")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	if err := store.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })

	mux := http.NewServeMux()
	mux.Handle("/mcp", mcpserver.NewStreamableHTTPServer(mcp.NewServer(store, "test", nil, nil, nil, 0, true, nil)))
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)
	return ts.URL + "/mcp"
}

// startAPI returns a server with optional suggester wired in. Uses the
// same pattern as server_test.go's stand-up.
func startAPI(t *testing.T, sug mcpmap.Suggester) *httptest.Server {
	t.Helper()
	store, err := sqlite.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	ag := agent.New(store, &stubClassifier{})
	srv := httptest.NewServer(NewServer(
		store, ag, BuildInfo{Version: "test"}, nil, nil, sug, nil, nil,
	).Handler())
	t.Cleanup(srv.Close)
	return srv
}

func postJSON(t *testing.T, srv *httptest.Server, path string, body any, wantStatus int, into any) {
	t.Helper()
	b, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.Post(srv.URL+path, "application/json", bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != wantStatus {
		buf := new(bytes.Buffer)
		_, _ = buf.ReadFrom(resp.Body)
		t.Fatalf("%s: status %d (want %d), body=%s", path, resp.StatusCode, wantStatus, buf.String())
	}
	if into != nil {
		if err := json.NewDecoder(resp.Body).Decode(into); err != nil {
			t.Fatalf("decode: %v", err)
		}
	}
}

func TestDiscoverDestination_AgainstCodedistill(t *testing.T) {
	dest := startCodedistillMCP(t)
	srv := startAPI(t, nil)

	var got struct {
		Tools []mcpclient.Tool `json:"tools"`
	}
	postJSON(t, srv, "/api/v1/mcp-export/discover", map[string]any{
		"endpoint": map[string]any{"url": dest},
	}, http.StatusOK, &got)

	if len(got.Tools) < 8 {
		t.Errorf("expected >=8 tools from CodeDistill server, got %d", len(got.Tools))
	}
	found := false
	for _, tool := range got.Tools {
		if tool.Name == "list_todos" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected list_todos in discovered catalog")
	}
}

func TestDiscoverDestination_BadURL(t *testing.T) {
	srv := startAPI(t, nil)
	postJSON(t, srv, "/api/v1/mcp-export/discover", map[string]any{
		"endpoint": map[string]any{"url": ""},
	}, http.StatusBadRequest, nil)
}

func TestDiscoverDestination_UnreachableHost(t *testing.T) {
	srv := startAPI(t, nil)
	// 127.0.0.1:1 is reserved (tcpmux) and almost always closed.
	postJSON(t, srv, "/api/v1/mcp-export/discover", map[string]any{
		"endpoint": map[string]any{"url": "http://127.0.0.1:1/mcp"},
	}, http.StatusBadGateway, nil)
}

func TestSuggestMapping_HappyPath(t *testing.T) {
	sug := &fakeSuggester{
		resp: `{"create":"create_issue","update":"update_issue","status_change":"close_issue","delete":"delete_issue"}`,
	}
	srv := startAPI(t, sug)

	var got struct {
		Mapping mcpmap.Mapping `json:"mapping"`
	}
	postJSON(t, srv, "/api/v1/mcp-export/suggest-mapping", map[string]any{
		"item_type": "todo",
		"catalog": []map[string]any{
			{"name": "create_issue", "description": "create"},
			{"name": "update_issue", "description": "update"},
			{"name": "close_issue", "description": "close"},
			{"name": "delete_issue", "description": "delete"},
		},
	}, http.StatusOK, &got)

	if got.Mapping[mcpmap.OpCreate] != "create_issue" {
		t.Errorf("create: got %q want create_issue", got.Mapping[mcpmap.OpCreate])
	}
	if got.Mapping[mcpmap.OpDelete] != "delete_issue" {
		t.Errorf("delete: got %q want delete_issue", got.Mapping[mcpmap.OpDelete])
	}
}

func TestSuggestMapping_ServiceUnavailableWhenNoSuggester(t *testing.T) {
	srv := startAPI(t, nil)
	postJSON(t, srv, "/api/v1/mcp-export/suggest-mapping", map[string]any{
		"item_type": "todo",
		"catalog":   []map[string]any{{"name": "x"}},
	}, http.StatusServiceUnavailable, nil)
}

func TestSuggestMapping_GuardsBadInput(t *testing.T) {
	sug := &fakeSuggester{resp: `{}`}
	srv := startAPI(t, sug)

	postJSON(t, srv, "/api/v1/mcp-export/suggest-mapping", map[string]any{
		"item_type": "todo",
		"catalog":   []map[string]any{},
	}, http.StatusBadRequest, nil)

	postJSON(t, srv, "/api/v1/mcp-export/suggest-mapping", map[string]any{
		"item_type": "",
		"catalog":   []map[string]any{{"name": "x"}},
	}, http.StatusBadRequest, nil)
}

// Pin imports used only via fmt.Sprintf in error helpers etc.
var _ = strings.Contains
var _ = fmt.Sprintf

// setupWithExportHook stands up an API server with the real
// settings-backed export hook wired (setupWithStore passes nil), so the
// migrate endpoint has something to enqueue through.
func setupWithExportHook(t *testing.T) (*httptest.Server, *sqlite.Store) {
	t.Helper()
	store, err := sqlite.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	if err := store.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	ag := agent.New(store, &stubClassifier{category: "BUG", reasoning: "x"})
	ag.Start(context.Background())
	t.Cleanup(ag.Stop)
	hook := mcpworker.NewHook(store, mcpworker.NewSettingsLookup(store), nil)
	srv := httptest.NewServer(NewServer(store, ag, BuildInfo{Version: "test"}, nil, hook, nil, nil, nil).Handler())
	t.Cleanup(srv.Close)
	return srv, store
}

// TestMigrateEndpoint covers the initial-migration bulk push: only
// local-only items enqueue (already-synced ones are skipped), items
// flip to 'pending', the queue gains rows, and the no-config /
// bad-type rejections work.
func TestMigrateEndpoint(t *testing.T) {
	srv, store := setupWithExportHook(t)
	ctx := context.Background()
	now := time.Now()

	if err := store.CreateProject(ctx, &domain.Project{ID: "p1", Name: "P", CreatedAt: now}); err != nil {
		t.Fatal(err)
	}
	if err := store.CreateScratchpad(ctx, &domain.Scratchpad{
		ID: "sp1", ProjectID: "p1", Name: "main", ClassificationMode: "full", CreatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.CreateScratchpadItem(ctx, &domain.ScratchpadItem{
		ID: "si1", ScratchpadID: "sp1", ContentType: "text", Content: "seed",
		ClassificationState: "classified", CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	seedTodo := func(id string) {
		t.Helper()
		if err := store.CreateTodoItem(ctx, &domain.TodoItem{
			ID: id, ProjectID: "p1", SourceItemID: "si1", Subject: id,
			Priority: "none", Status: "incomplete", Origin: "agent-derived", CreatedAt: now,
		}); err != nil {
			t.Fatal(err)
		}
	}
	seedTodo("t1")
	seedTodo("t2")
	// t2 is already synced — must be skipped, not re-created remotely.
	if err := store.MarkItemSynced(ctx, "todo_item", "t2", "REMOTE-9", now); err != nil {
		t.Fatal(err)
	}

	// Config for todos only: create mapped, so migration is possible.
	cfgJSON, _ := json.Marshal(mcpworker.Config{
		Endpoint: mcpclient.Endpoint{URL: "http://example.invalid/mcp"},
		Mapping:  mcpmap.Mapping{"create": "create_issue"},
	})
	if err := store.SetUserSetting(ctx, &domain.UserSetting{
		UserID: "local", Key: mcpworker.SettingsKey("todo"), Value: cfgJSON,
	}); err != nil {
		t.Fatal(err)
	}

	var resp struct {
		Enqueued int `json:"enqueued"`
		Skipped  int `json:"skipped"`
	}
	doJSON(t, srv, "POST", "/api/v1/mcp-export/migrate",
		map[string]string{"item_type": "todo"}, 200, &resp)
	if resp.Enqueued != 1 || resp.Skipped != 1 {
		t.Errorf("migrate = %+v, want enqueued 1 / skipped 1", resp)
	}

	// t1 flipped to pending; t2 untouched.
	_, status, _, err := store.GetItemSyncState(ctx, "todo_item", "t1")
	if err != nil || status != "pending" {
		t.Errorf("t1 sync = %q (%v), want pending", status, err)
	}
	_, status, _, _ = store.GetItemSyncState(ctx, "todo_item", "t2")
	if status != "synced" {
		t.Errorf("t2 sync = %q, want synced (skipped)", status)
	}

	// Queue holds exactly the one create op (raw SQL — the worker owns
	// the due-time comparison semantics).
	var qn int
	var qOwner, qOp string
	if err := store.DB.QueryRowContext(ctx,
		`SELECT COUNT(*), MAX(owner_id), MAX(op) FROM mcp_export_queue`).
		Scan(&qn, &qOwner, &qOp); err != nil {
		t.Fatal(err)
	}
	if qn != 1 || qOwner != "t1" || qOp != "create" {
		t.Errorf("queue = %d rows (owner=%s op=%s), want one create for t1", qn, qOwner, qOp)
	}

	// Re-running is idempotent: t1 is now pending → skipped.
	doJSON(t, srv, "POST", "/api/v1/mcp-export/migrate",
		map[string]string{"item_type": "todo"}, 200, &resp)
	if resp.Enqueued != 0 || resp.Skipped != 2 {
		t.Errorf("second migrate = %+v, want enqueued 0 / skipped 2", resp)
	}

	// No config for bugs → 409; junk type → 400.
	doJSON(t, srv, "POST", "/api/v1/mcp-export/migrate",
		map[string]string{"item_type": "bug"}, 409, nil)
	doJSON(t, srv, "POST", "/api/v1/mcp-export/migrate",
		map[string]string{"item_type": "ticket"}, 400, nil)
}
