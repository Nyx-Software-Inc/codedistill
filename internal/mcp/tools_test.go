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

package mcp

import (
	"context"
	"encoding/json"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"codedistill/internal/domain"
	"codedistill/internal/storage/sqlite"
)

// fixture builds a Tools bound to an in-memory SQLite store with a
// minimal seed: the migration-seeded "local" workspace plus one project,
// one scratchpad, and one of each derived item. All entities have
// known names so the name-based MCP tool surface can be exercised.
type fixture struct {
	t     *testing.T
	store *sqlite.Store
	tools *Tools
	now   time.Time

	workspaceName string
	projectName   string
	padName       string

	projectID string
	padID     string
	itemID    string
	todoID    string
	bugID     string
	kbID      string
	useCaseID string
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	store, err := sqlite.Open(":memory:")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { store.Close() })
	if err := store.Migrate(context.Background()); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	now := time.Date(2026, 5, 7, 12, 0, 0, 0, time.UTC)
	ctx := context.Background()
	fx := &fixture{
		t: t, store: store, tools: New(store), now: now,
		workspaceName: "Local", // seeded by migration 0006
		projectName:   "Test Project",
		padName:       "main",
		projectID:     "p1", padID: "sp1",
		itemID: "it1", todoID: "td1", bugID: "bg1", kbID: "kb1", useCaseID: "uc1",
	}

	if err := store.CreateProject(ctx, &domain.Project{
		ID: fx.projectID, Name: fx.projectName, CreatedAt: now,
	}); err != nil {
		t.Fatalf("seed project: %v", err)
	}
	if err := store.CreateScratchpad(ctx, &domain.Scratchpad{
		ID: fx.padID, ProjectID: fx.projectID, Name: fx.padName,
		ClassificationMode: "full", CreatedAt: now,
	}); err != nil {
		t.Fatalf("seed scratchpad: %v", err)
	}
	if err := store.CreateScratchpadItem(ctx, &domain.ScratchpadItem{
		ID: fx.itemID, ScratchpadID: fx.padID,
		ContentType: "text", Content: "the source paste",
		ClassificationState: "classified", ProposedCategory: "todo",
		CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatalf("seed item: %v", err)
	}
	if err := store.CreateTodoItem(ctx, &domain.TodoItem{
		ID: fx.todoID, ProjectID: fx.projectID, SourceItemID: fx.itemID,
		Subject: "do the thing", Priority: "none", Status: "incomplete",
		Origin: "agent-derived", CreatedAt: now,
	}); err != nil {
		t.Fatalf("seed todo: %v", err)
	}
	if err := store.CreateBugItem(ctx, &domain.BugItem{
		ID: fx.bugID, ProjectID: fx.projectID, SourceItemID: fx.itemID,
		Subject: "the thing crashes", Severity: "minor", Status: "open",
		Origin: "agent-derived", CreatedAt: now,
	}); err != nil {
		t.Fatalf("seed bug: %v", err)
	}
	if err := store.CreateKnowledgeEntry(ctx, &domain.KnowledgeEntry{
		ID: fx.kbID, ProjectID: fx.projectID, SourceItemID: fx.itemID,
		Title: "ref", Content: "knowledge", CreatedAt: now,
	}); err != nil {
		t.Fatalf("seed kb: %v", err)
	}
	if err := store.CreateUseCaseItem(ctx, &domain.UseCaseItem{
		ID: fx.useCaseID, ProjectID: fx.projectID, SourceItemID: fx.itemID,
		Subject: "users can do X", Description: "the source paste",
		Status: "open", Origin: "agent-derived",
		CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatalf("seed use_case: %v", err)
	}
	return fx
}

// callTool simulates a tools/call by registering the tools on a fresh
// server, then invoking the tool's handler directly. mcp-go doesn't
// expose a public dispatch helper, so we drive the handler ourselves
// — the schema validation is unit-tested by mcp-go itself.
func (fx *fixture) callTool(name string, args map[string]any) *mcp.CallToolResult {
	return fx.callToolCtx(context.Background(), name, args)
}

func (fx *fixture) callToolCtx(ctx context.Context, name string, args map[string]any) *mcp.CallToolResult {
	fx.t.Helper()
	srv := server.NewMCPServer("test", "0.0.0", server.WithToolCapabilities(true))
	fx.tools.Register(srv, true)
	req := mcp.CallToolRequest{}
	req.Params.Name = name
	req.Params.Arguments = args
	res := srv.HandleMessage(ctx, mustJSON(map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/call",
		"params":  req.Params,
	}))
	// HandleMessage returns either a JSONRPCResponse (success) or a
	// JSONRPCError (transport-level failure). Tool-level errors land
	// *inside* a successful response with IsError=true on the result.
	raw := mustJSON(res)
	var env struct {
		Result *mcp.CallToolResult `json:"result"`
		Error  any                 `json:"error,omitempty"`
	}
	if err := json.Unmarshal(raw, &env); err != nil {
		fx.t.Fatalf("unmarshal envelope: %v (raw=%s)", err, raw)
	}
	if env.Error != nil {
		fx.t.Fatalf("transport error: %v (raw=%s)", env.Error, raw)
	}
	if env.Result == nil {
		fx.t.Fatalf("nil result (raw=%s)", raw)
	}
	return env.Result
}

func mustJSON(v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return b
}

// firstText pulls the first text payload out of a tool result. Both
// JSON-content and error-content tool results route through here in
// the tests below.
func firstText(t *testing.T, res *mcp.CallToolResult) string {
	t.Helper()
	if len(res.Content) == 0 {
		t.Fatalf("result has no content")
	}
	tc, ok := mcp.AsTextContent(res.Content[0])
	if !ok {
		t.Fatalf("first content is not text: %T", res.Content[0])
	}
	return tc.Text
}

// --- list_* tools ---

func TestReadItemIncludesAcceptanceCriteria(t *testing.T) {
	fx := newFixture(t)
	now := time.Now().UTC()
	if err := fx.store.CreateAcceptanceCriterion(context.Background(), &domain.AcceptanceCriterion{
		ID: "ac1", OwnerType: ownerTodoItem, OwnerID: fx.todoID,
		Position: 0, Text: "the export contains deleted lines",
		VerificationKind: "unspecified", State: "proposed", Provenance: "ai-proposed",
		CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatalf("seed criterion: %v", err)
	}
	res := fx.callTool("read_item", map[string]any{"owner_type": ownerTodoItem, "id": fx.todoID})
	if res.IsError {
		t.Fatalf("read_item: %s", firstText(t, res))
	}
	body := firstText(t, res)
	if !strings.Contains(body, "acceptance_criteria") || !strings.Contains(body, "the export contains deleted lines") {
		t.Errorf("read_item missing acceptance_criteria: %s", body)
	}
}

func TestListProjectsReturnsSeed(t *testing.T) {
	fx := newFixture(t)
	res := fx.callTool("list_projects", nil)
	if res.IsError {
		t.Fatalf("unexpected error: %s", firstText(t, res))
	}
	body := firstText(t, res)
	if !strings.Contains(body, fx.projectID) {
		t.Errorf("body missing project id: %s", body)
	}
}

func TestListScratchpadsScopes(t *testing.T) {
	fx := newFixture(t)
	res := fx.callTool("list_scratchpads", map[string]any{
		"project_name": fx.projectName,
	})
	if res.IsError {
		t.Fatalf("unexpected error: %s", firstText(t, res))
	}
	body := firstText(t, res)
	if !strings.Contains(body, fx.padName) {
		t.Errorf("body missing pad name: %s", body)
	}
}

func TestListScratchpadsRequiresProjectName(t *testing.T) {
	fx := newFixture(t)
	res := fx.callTool("list_scratchpads", map[string]any{"project_name": ""})
	if !res.IsError {
		t.Errorf("expected error result for missing project_name")
	}
}

func TestListScratchpadsResolvesUnknownProjectAsError(t *testing.T) {
	fx := newFixture(t)
	res := fx.callTool("list_scratchpads", map[string]any{
		"project_name": "Does Not Exist",
	})
	if !res.IsError {
		t.Errorf("expected error for missing project")
	}
}

func TestListScratchpadItemsScopes(t *testing.T) {
	fx := newFixture(t)
	res := fx.callTool("list_scratchpad_items", map[string]any{
		"project_name":    fx.projectName,
		"scratchpad_name": fx.padName,
	})
	if res.IsError {
		t.Fatalf("unexpected error: %s", firstText(t, res))
	}
	body := firstText(t, res)
	if !strings.Contains(body, fx.itemID) {
		t.Errorf("body missing item id: %s", body)
	}
}

func TestListScratchpadItemsRequiresBothNames(t *testing.T) {
	fx := newFixture(t)
	// Only project_name provided.
	res := fx.callTool("list_scratchpad_items", map[string]any{
		"project_name": fx.projectName,
	})
	if !res.IsError {
		t.Errorf("expected error for missing scratchpad_name")
	}
}

func TestListDerivedTables(t *testing.T) {
	fx := newFixture(t)
	cases := []struct {
		tool     string
		expectID string
	}{
		{"list_todos", fx.todoID},
		{"list_bugs", fx.bugID},
		{"list_kb", fx.kbID},
		{"list_use_cases", fx.useCaseID},
	}
	for _, c := range cases {
		t.Run(c.tool, func(t *testing.T) {
			res := fx.callTool(c.tool, map[string]any{
				"project_name": fx.projectName,
			})
			if res.IsError {
				t.Fatalf("%s: unexpected error: %s", c.tool, firstText(t, res))
			}
			body := firstText(t, res)
			if !strings.Contains(body, c.expectID) {
				t.Errorf("%s body missing id %s: %s", c.tool, c.expectID, body)
			}
			// v0.8.2 cache passthrough: every list_* response surfaces
			// sync_status so MCP clients see the routed-or-not state.
			// Default is "local-only" (no destination configured).
			if !strings.Contains(body, `"sync_status":"local-only"`) {
				t.Errorf("%s body missing sync_status field: %s", c.tool, body)
			}
		})
	}
}

// --- workspace_name handling ---

func TestListsAcceptExplicitWorkspaceName(t *testing.T) {
	fx := newFixture(t)
	// Single workspace exists; explicit workspace_name="Local" should
	// resolve identically to omitting it.
	res := fx.callTool("list_todos", map[string]any{
		"workspace_name": fx.workspaceName,
		"project_name":   fx.projectName,
	})
	if res.IsError {
		t.Fatalf("unexpected error: %s", firstText(t, res))
	}
	body := firstText(t, res)
	if !strings.Contains(body, fx.todoID) {
		t.Errorf("body missing todo id: %s", body)
	}
}

func TestListsRejectMultipleWorkspacesWithoutExplicitName(t *testing.T) {
	fx := newFixture(t)
	// Add a second workspace; resolveDefaultWorkspace should now refuse.
	if err := fx.store.CreateWorkspace(context.Background(), &domain.Workspace{
		ID: "ws-other", Name: "Other", Plan: "free", CreatedAt: fx.now,
	}); err != nil {
		t.Fatalf("seed second workspace: %v", err)
	}
	res := fx.callTool("list_todos", map[string]any{
		"project_name": fx.projectName,
	})
	if !res.IsError {
		t.Errorf("expected error when multiple workspaces exist and workspace_name omitted")
	}
}

// --- read_item ---

func TestReadItemPolymorphic(t *testing.T) {
	fx := newFixture(t)
	cases := []struct {
		ownerType string
		id        string
	}{
		{ownerScratchpadItem, fx.itemID},
		{ownerTodoItem, fx.todoID},
		{ownerBugItem, fx.bugID},
		{ownerKnowledgeEntry, fx.kbID},
		{ownerUseCaseItem, fx.useCaseID},
	}
	for _, c := range cases {
		t.Run(c.ownerType, func(t *testing.T) {
			res := fx.callTool("read_item", map[string]any{
				"owner_type": c.ownerType,
				"id":         c.id,
			})
			if res.IsError {
				t.Fatalf("unexpected error: %s", firstText(t, res))
			}
			body := firstText(t, res)
			if !strings.Contains(body, c.id) {
				t.Errorf("body missing id %s: %s", c.id, body)
			}
		})
	}
}

func TestReadItemNotFound(t *testing.T) {
	fx := newFixture(t)
	res := fx.callTool("read_item", map[string]any{
		"owner_type": ownerTodoItem,
		"id":         "does-not-exist",
	})
	if !res.IsError {
		t.Errorf("expected error result for missing id")
	}
}

// TestReadItemEnrichesBlobURL covers the Slice 1 MCP integration:
// when a scratchpad_item carries a blob_sha AND the Tools is wired
// with a BlobStore, read_item must include blob_url +
// blob_url_expires_at. Items without blobs (the default fixture) and
// blob items without a BlobStore (stdio mode) must NOT include them.
func TestReadItemEnrichesBlobURL(t *testing.T) {
	fx := newFixture(t)
	ctx := context.Background()

	// Text item from the fixture: blob_url must not appear.
	res := fx.callTool("read_item", map[string]any{
		"owner_type": ownerScratchpadItem,
		"id":         fx.itemID,
	})
	if res.IsError {
		t.Fatalf("text item read errored: %s", firstText(t, res))
	}
	if strings.Contains(firstText(t, res), `"blob_url"`) {
		t.Errorf("text item should not surface blob_url; got %s", firstText(t, res))
	}

	// Seed a binary item directly + wire a stub BlobStore that always
	// returns a known URL.
	blobItem := &domain.ScratchpadItem{
		ID: "siImg", ScratchpadID: fx.padID,
		ContentType: "image", Content: "",
		ClassificationState: "unprocessed",
		Tags:                []string{},
		BlobSHA:             "deadbeef" + strings.Repeat("0", 56),
		MimeType:            "image/png",
		ByteSize:            1024, Width: 32, Height: 24,
		CreatedAt: fx.now, UpdatedAt: fx.now,
	}
	if err := fx.store.CreateScratchpadItem(ctx, blobItem); err != nil {
		t.Fatalf("seed blob item: %v", err)
	}

	// Without a BlobStore wired, blob_url should still be absent
	// even though the item has a BlobSHA.
	res = fx.callTool("read_item", map[string]any{
		"owner_type": ownerScratchpadItem,
		"id":         blobItem.ID,
	})
	if res.IsError {
		t.Fatalf("blob item read errored (no store): %s", firstText(t, res))
	}
	if strings.Contains(firstText(t, res), `"blob_url"`) {
		t.Errorf("no BlobStore: blob_url should be absent; got %s", firstText(t, res))
	}

	// With a stub BlobStore wired, blob_url + blob_url_expires_at
	// should appear.
	fx.tools.Blobs = stubBlobStore{url: "/api/v1/blobs/" + blobItem.BlobSHA}
	fx.tools.BlobURLTTL = 15 * time.Minute
	res = fx.callTool("read_item", map[string]any{
		"owner_type": ownerScratchpadItem,
		"id":         blobItem.ID,
	})
	if res.IsError {
		t.Fatalf("blob item read errored (with store): %s", firstText(t, res))
	}
	body := firstText(t, res)
	if !strings.Contains(body, `"blob_url":"/api/v1/blobs/`+blobItem.BlobSHA+`"`) {
		t.Errorf("missing blob_url in response: %s", body)
	}
	if !strings.Contains(body, `"blob_url_expires_at"`) {
		t.Errorf("missing blob_url_expires_at in response: %s", body)
	}
}

// stubBlobStore satisfies blobstore.BlobStore by returning a canned
// URL. Other methods panic — tests only invoke URL.
type stubBlobStore struct{ url string }

func (s stubBlobStore) Put(context.Context, io.Reader) (string, int64, error) {
	panic("stubBlobStore.Put not used in tests")
}
func (s stubBlobStore) Get(context.Context, string) (io.ReadCloser, error) {
	panic("stubBlobStore.Get not used in tests")
}
func (s stubBlobStore) Delete(context.Context, string) error {
	panic("stubBlobStore.Delete not used in tests")
}
func (s stubBlobStore) URL(_ context.Context, _ string, _ time.Duration) (string, error) {
	return s.url, nil
}
