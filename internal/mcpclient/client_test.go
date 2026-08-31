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

package mcpclient_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	mcpserver "github.com/mark3labs/mcp-go/server"

	"codedistill/internal/mcp"
	"codedistill/internal/mcpclient"
	"codedistill/internal/storage/sqlite"
)

// newTestServer spins up CodeDistill's own MCP server over HTTP via
// httptest. Round-tripping against our own implementation gives us a
// realistic integration test without external deps — and pins both
// sides of the contract in one go.
func newTestServer(t *testing.T) (string, func()) {
	t.Helper()
	store, err := sqlite.Open(":memory:")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	if err := store.Migrate(context.Background()); err != nil {
		store.Close()
		t.Fatalf("migrate: %v", err)
	}

	srv := mcp.NewServer(store, "test", nil, nil, nil, 0, true, nil)
	mux := http.NewServeMux()
	mux.Handle("/mcp", mcpserver.NewStreamableHTTPServer(srv))
	ts := httptest.NewServer(mux)

	cleanup := func() {
		ts.Close()
		store.Close()
	}
	return ts.URL + "/mcp", cleanup
}

func TestNew_BadURL(t *testing.T) {
	_, err := mcpclient.New(context.Background(), mcpclient.Endpoint{URL: ""})
	if err == nil {
		t.Fatal("expected error for empty URL")
	}
	if !strings.Contains(err.Error(), "URL required") {
		t.Errorf("expected 'URL required' error, got: %v", err)
	}
}

func TestListTools_AgainstCodedistillServer(t *testing.T) {
	url, cleanup := newTestServer(t)
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	cli, err := mcpclient.New(ctx, mcpclient.Endpoint{URL: url})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer cli.Close()

	tools, err := cli.ListTools(ctx)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}

	// CodeDistill's v1 surface is 10 tools (see internal/mcp/tools.go).
	// Asserting the exact count would couple this test to that count;
	// asserting >=8 keeps it useful while letting the surface grow.
	if len(tools) < 8 {
		t.Fatalf("expected >=8 tools, got %d", len(tools))
	}

	// Read tools are present in EVERY build — "MCP read free" is the funnel
	// hook. Write tools exist only where the mcp feature is compiled in, so the
	// expectation for those comes from expectedWriteTools, which differs by
	// build tag. Keeping one test rather than splitting the file means the
	// community build still verifies its own catalog (CE-review item 9).
	wantNames := map[string]bool{
		"list_projects":    false,
		"list_scratchpads": false,
		"list_todos":       false,
		"read_item":        false,
	}
	for _, n := range expectedWriteTools {
		wantNames[n] = false
	}
	for _, tool := range tools {
		if _, ok := wantNames[tool.Name]; ok {
			wantNames[tool.Name] = true
		}
		if tool.Description == "" {
			t.Errorf("tool %q has empty description", tool.Name)
		}
		if len(tool.InputSchema) == 0 {
			t.Errorf("tool %q has empty input schema", tool.Name)
		}
		// Schema must round-trip as valid JSON object.
		var probe map[string]any
		if err := json.Unmarshal(tool.InputSchema, &probe); err != nil {
			t.Errorf("tool %q schema not valid JSON: %v", tool.Name, err)
		}
	}
	for name, found := range wantNames {
		if !found {
			t.Errorf("expected tool %q in catalog, not found", name)
		}
	}
}

func TestCallTool_ListProjects(t *testing.T) {
	url, cleanup := newTestServer(t)
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	cli, err := mcpclient.New(ctx, mcpclient.Endpoint{URL: url})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer cli.Close()

	res, err := cli.CallTool(ctx, "list_projects", map[string]any{})
	if err != nil {
		t.Fatalf("CallTool list_projects: %v", err)
	}
	if res.IsError {
		t.Fatalf("list_projects returned IsError: content=%v", res.Content)
	}
	if len(res.Content) == 0 {
		t.Fatal("list_projects returned no content blocks")
	}
	// Empty store has no projects, so the response should still be a
	// well-formed text block (likely a JSON-encoded {"projects": []}).
	if res.Content[0].Type != "text" {
		t.Errorf("expected text content, got %q", res.Content[0].Type)
	}
	if len(res.Raw) == 0 {
		t.Error("expected raw response payload preserved")
	}
}

func TestCallTool_UnknownTool(t *testing.T) {
	url, cleanup := newTestServer(t)
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	cli, err := mcpclient.New(ctx, mcpclient.Endpoint{URL: url})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer cli.Close()

	// Calling a nonexistent tool: the server returns either an error
	// response (transport.err == nil, IsError == true) or a JSON-RPC
	// method-not-found error (transport.err != nil). Either is a fine
	// signal — the worker's job is to mark sync_failed without retry.
	res, err := cli.CallTool(ctx, "definitely_not_a_real_tool", map[string]any{})
	if err == nil && (res == nil || !res.IsError) {
		t.Fatalf("expected error or IsError=true for unknown tool, got res=%+v err=%v", res, err)
	}
}

func TestCallTool_RequiresName(t *testing.T) {
	url, cleanup := newTestServer(t)
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	cli, err := mcpclient.New(ctx, mcpclient.Endpoint{URL: url})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer cli.Close()

	if _, err := cli.CallTool(ctx, "", map[string]any{}); err == nil {
		t.Fatal("expected error for empty tool name")
	}
}

func TestClose_Idempotent(t *testing.T) {
	url, cleanup := newTestServer(t)
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	cli, err := mcpclient.New(ctx, mcpclient.Endpoint{URL: url})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := cli.Close(); err != nil {
		t.Fatalf("first Close: %v", err)
	}
	if err := cli.Close(); err != nil {
		t.Fatalf("second Close should be no-op: %v", err)
	}
	if _, err := cli.ListTools(ctx); err == nil {
		t.Fatal("ListTools after Close should error")
	}
}

func TestMergeHeaders_AuthFormats(t *testing.T) {
	// Indirect test via New + a roundtrip-recording server: we install
	// the credentials, fire one request, then assert what the server
	// observed. This is more useful than unit-testing the unexported
	// mergeHeaders since it pins the on-the-wire behavior.
	var seen http.Header
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = r.Header.Clone()
		// Reply with something that fails MCP initialize cleanly so the
		// test doesn't have to mount a real MCP server.
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// We don't care that New fails — only that the request carried the
	// expected Authorization header before failing.
	_, _ = mcpclient.New(ctx, mcpclient.Endpoint{
		URL: srv.URL + "/mcp",
		Credentials: &mcpclient.Credentials{
			Type:  "bearer",
			Token: "shh-secret",
		},
	})
	if got := seen.Get("Authorization"); got != "Bearer shh-secret" {
		t.Errorf("Authorization header: got %q, want %q", got, "Bearer shh-secret")
	}

	seen = nil
	_, _ = mcpclient.New(ctx, mcpclient.Endpoint{
		URL: srv.URL + "/mcp",
		Credentials: &mcpclient.Credentials{
			Type:  "header",
			Name:  "X-Api-Key",
			Token: "abc123",
		},
	})
	if got := seen.Get("X-Api-Key"); got != "abc123" {
		t.Errorf("X-Api-Key header: got %q, want %q", got, "abc123")
	}
}
