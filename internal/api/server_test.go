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
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"codedistill/internal/agent"
	"codedistill/internal/domain"
	"codedistill/internal/exportbundle"
	"codedistill/internal/storage"
	"codedistill/internal/storage/sqlite"
)

// End-to-end API smoke test: exercises every meaningful happy path and
// the agent integration on the Scratchpad_Item create endpoint. Uses a
// stubbed Classifier so no Ollama is required.

type stubClassifier struct{ category, reasoning string }

func (s *stubClassifier) Classify(_ context.Context, _ agent.ClassifyInput) (agent.Classification, error) {
	return agent.Classification{Category: s.category, Reasoning: s.reasoning}, nil
}

func setup(t *testing.T) (*httptest.Server, *agent.Agent) {
	srv, ag, _ := setupWithStore(t)
	return srv, ag
}

// setupWithStore is setup() that also surfaces the underlying *sqlite.Store
// for tests that need to seed rows directly (typically because the row's
// creation path has no public HTTP surface — e.g. implementation_matches).
func setupWithStore(t *testing.T) (*httptest.Server, *agent.Agent, *sqlite.Store) {
	t.Helper()
	store, err := sqlite.Open(":memory:")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { store.Close() })
	if err := store.Migrate(context.Background()); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	ag := agent.New(store, &stubClassifier{category: "BUG", reasoning: "stack trace"},
		agent.WithClock(func() time.Time {
			return time.Date(2026, 4, 22, 12, 0, 0, 0, time.UTC)
		}),
	)
	ag.Start(context.Background())
	t.Cleanup(ag.Stop)

	apiServer := NewServer(store, ag, BuildInfo{
		Version: "test", GitSHA: "deadbeef", BuildDate: "2026-01-01",
	}, nil, nil, nil, nil, nil)
	lastTestServer = apiServer
	srv := httptest.NewServer(apiServer.Handler())
	t.Cleanup(srv.Close)
	return srv, ag, store
}

// lastTestServer exposes the most recently built Server so tests can adjust
// post-construction config (e.g. the license-redeem wiring) without
// replumbing every newTestServer caller.
var lastTestServer *Server

// doJSON is a compact test helper: issues a request, asserts the status,
// and decodes the response body into v (if non-nil).
func doJSON(t *testing.T, srv *httptest.Server, method, path string, body any, wantStatus int, v any) {
	t.Helper()
	var reader io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		reader = bytes.NewReader(b)
	}
	req, _ := http.NewRequest(method, srv.URL+path, reader)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != wantStatus {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("%s %s: status %d, want %d, body=%s", method, path, resp.StatusCode, wantStatus, string(b))
	}
	if v != nil && resp.StatusCode != http.StatusNoContent {
		if err := json.NewDecoder(resp.Body).Decode(v); err != nil {
			t.Fatalf("%s %s: decode: %v", method, path, err)
		}
	}
}

func TestHealth(t *testing.T) {
	srv, _ := setup(t)
	var out map[string]string
	doJSON(t, srv, "GET", "/api/v1/health", nil, 200, &out)
	if out["status"] != "ok" {
		t.Errorf("health status = %q", out["status"])
	}
}

// Round-trip a project via GET /export → POST /import. Confirms that the
// HTTP wiring on the import side works end-to-end and the result has the
// "(imported)" naming convention from the locked design.
func TestImportEndpoint(t *testing.T) {
	srv, _ := setup(t)

	var p domain.Project
	doJSON(t, srv, "POST", "/api/v1/projects",
		map[string]string{"name": "Apollo"}, 201, &p)

	// Pull the export.
	resp, err := srv.Client().Get(srv.URL + "/api/v1/projects/" + p.ID + "/export")
	if err != nil {
		t.Fatalf("export get: %v", err)
	}
	bundle, _ := io.ReadAll(resp.Body)
	resp.Body.Close()

	// Build multipart upload.
	body := new(bytes.Buffer)
	mw := multipart.NewWriter(body)
	fw, err := mw.CreateFormFile("bundle", "test.zip")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fw.Write(bundle); err != nil {
		t.Fatal(err)
	}
	mw.Close()

	req, _ := http.NewRequest("POST", srv.URL+"/api/v1/import", body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	resp, err = srv.Client().Do(req)
	if err != nil {
		t.Fatalf("import post: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 201 {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("import status = %d, body=%s", resp.StatusCode, string(b))
	}

	var res exportbundle.ImportResult
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if res.Project.Name != "Apollo (imported)" {
		t.Errorf("imported project name = %q", res.Project.Name)
	}
	if res.Project.ID == p.ID {
		t.Error("imported project must have a fresh ID")
	}
}

func TestVersion(t *testing.T) {
	srv, _ := setup(t)
	var got BuildInfo
	doJSON(t, srv, "GET", "/api/v1/version", nil, 200, &got)
	if got.Version != "test" || got.GitSHA != "deadbeef" || got.BuildDate != "2026-01-01" {
		t.Errorf("version = %+v", got)
	}
}

// Export endpoints stream a zip with the project (or single scratchpad)
// content. Full format coverage lives in internal/exportbundle/bundle_test.go;
// this just confirms the wiring + headers.
func TestExportEndpoints(t *testing.T) {
	srv, _ := setup(t)

	var p domain.Project
	doJSON(t, srv, "POST", "/api/v1/projects", map[string]string{"name": "Apollo Demo"}, 201, &p)
	var sp domain.Scratchpad
	doJSON(t, srv, "POST", "/api/v1/projects/"+p.ID+"/scratchpads",
		map[string]string{"name": "extra"}, 201, &sp)

	for _, tc := range []struct {
		name, path, slug string
	}{
		{"project", "/api/v1/projects/" + p.ID + "/export", "apollo-demo"},
		{"scratchpad", "/api/v1/scratchpads/" + sp.ID + "/export", "apollo-demo-extra"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			resp, err := srv.Client().Get(srv.URL + tc.path)
			if err != nil {
				t.Fatalf("get: %v", err)
			}
			defer resp.Body.Close()
			if resp.StatusCode != 200 {
				t.Errorf("status = %d", resp.StatusCode)
			}
			if got := resp.Header.Get("Content-Type"); got != "application/zip" {
				t.Errorf("content-type = %q", got)
			}
			if cd := resp.Header.Get("Content-Disposition"); !strings.Contains(cd, tc.slug) {
				t.Errorf("disposition = %q, want substring %q", cd, tc.slug)
			}
			body, _ := io.ReadAll(resp.Body)
			// Zip magic bytes are "PK\x03\x04".
			if len(body) < 4 || string(body[:4]) != "PK\x03\x04" {
				t.Errorf("body is not a zip")
			}
		})
	}
}

func TestProjectLifecycle(t *testing.T) {
	srv, _ := setup(t)

	var created domain.Project
	doJSON(t, srv, "POST", "/api/v1/projects", map[string]string{"name": "Apollo"}, 201, &created)
	if created.ID == "" || created.Name != "Apollo" {
		t.Errorf("bad create: %+v", created)
	}

	var got domain.Project
	doJSON(t, srv, "GET", "/api/v1/projects/"+created.ID, nil, 200, &got)
	if got.ID != created.ID {
		t.Errorf("get mismatch")
	}

	var list []*domain.Project
	doJSON(t, srv, "GET", "/api/v1/projects", nil, 200, &list)
	if len(list) != 1 {
		t.Errorf("list len = %d", len(list))
	}

	doJSON(t, srv, "DELETE", "/api/v1/projects/"+created.ID, nil, 204, nil)
	doJSON(t, srv, "GET", "/api/v1/projects/"+created.ID, nil, 404, nil)
}

// Migration 0012's UNIQUE(workspace_id, name) on projects + the
// storage-layer wrapDupErr translation should surface to the API
// as 409 Conflict, not 500.
func TestProjectNameCollisionReturns409(t *testing.T) {
	srv, _ := setup(t)
	doJSON(t, srv, "POST", "/api/v1/projects", map[string]string{"name": "Apollo"}, 201, nil)
	doJSON(t, srv, "POST", "/api/v1/projects", map[string]string{"name": "Apollo"}, 409, nil)
}

// Renaming a project to a name already in use in the same workspace
// also surfaces as 409 — the same code path, just via UPDATE.
func TestProjectRenameCollisionReturns409(t *testing.T) {
	srv, _ := setup(t)
	var first, second domain.Project
	doJSON(t, srv, "POST", "/api/v1/projects", map[string]string{"name": "Apollo"}, 201, &first)
	doJSON(t, srv, "POST", "/api/v1/projects", map[string]string{"name": "Gemini"}, 201, &second)
	doJSON(t, srv, "PATCH", "/api/v1/projects/"+second.ID, map[string]string{"name": "Apollo"}, 409, nil)
}

// Same contract for scratchpads — UNIQUE(project_id, name).
func TestScratchpadRenameCollisionReturns409(t *testing.T) {
	srv, _ := setup(t)
	var p domain.Project
	doJSON(t, srv, "POST", "/api/v1/projects", map[string]string{"name": "p"}, 201, &p)
	// createProject auto-seeded "main"; create a sibling "extra".
	var extra domain.Scratchpad
	doJSON(t, srv, "POST", "/api/v1/projects/"+p.ID+"/scratchpads",
		map[string]string{"name": "extra"}, 201, &extra)
	// Renaming "extra" to "main" collides with the auto-seeded one.
	doJSON(t, srv, "PATCH", "/api/v1/scratchpads/"+extra.ID,
		map[string]string{"name": "main"}, 409, nil)
}

func TestInboxFlowEndToEnd(t *testing.T) {
	srv, ag := setup(t)

	// project + scratchpad
	var p domain.Project
	doJSON(t, srv, "POST", "/api/v1/projects", map[string]string{"name": "p"}, 201, &p)
	var sp domain.Scratchpad
	doJSON(t, srv, "POST", "/api/v1/projects/"+p.ID+"/scratchpads",
		map[string]string{"name": "extra"}, 201, &sp)
	if sp.ClassificationMode != "full" {
		t.Errorf("default classification_mode = %q, want full", sp.ClassificationMode)
	}

	// create item triggers async agent work
	var item domain.ScratchpadItem
	doJSON(t, srv, "POST", "/api/v1/scratchpads/"+sp.ID+"/items",
		map[string]string{"content": "clicking login throws 500"}, 201, &item)
	if item.ClassificationState != "unprocessed" {
		t.Errorf("on create state = %q, want unprocessed", item.ClassificationState)
	}

	// Drain the agent so the async classification completes.
	ag.Stop()

	// Item should now be pending-review with category "bug"
	var afterClassify domain.ScratchpadItem
	doJSON(t, srv, "GET", "/api/v1/items/"+item.ID, nil, 200, &afterClassify)
	if afterClassify.ClassificationState != "pending-review" {
		t.Errorf("after classify state = %q, want pending-review", afterClassify.ClassificationState)
	}
	if afterClassify.ProposedCategory != "bug" {
		t.Errorf("proposed category = %q, want bug", afterClassify.ProposedCategory)
	}

	// Inbox list should include our item.
	var inbox []*domain.ScratchpadItem
	doJSON(t, srv, "GET", "/api/v1/inbox", nil, 200, &inbox)
	if len(inbox) != 1 || inbox[0].ID != item.ID {
		t.Errorf("inbox = %+v", inbox)
	}

	// Accept the item -> state classified + a derived bug exists.
	var accepted domain.ScratchpadItem
	doJSON(t, srv, "POST", "/api/v1/items/"+item.ID+"/accept", nil, 200, &accepted)
	if accepted.ClassificationState != "classified" {
		t.Errorf("accepted state = %q, want classified", accepted.ClassificationState)
	}
	if accepted.DerivedItemID == "" {
		t.Fatal("accepted has no derived_item_id")
	}
	var bug domain.BugItem
	doJSON(t, srv, "GET", "/api/v1/bugs/"+accepted.DerivedItemID, nil, 200, &bug)
	if bug.Origin != "agent-derived" || bug.Subject == "" {
		t.Errorf("derived bug = %+v", bug)
	}
}

func TestReclassifyAndReject(t *testing.T) {
	srv, ag := setup(t)

	var p domain.Project
	doJSON(t, srv, "POST", "/api/v1/projects", map[string]string{"name": "p"}, 201, &p)
	var sp domain.Scratchpad
	doJSON(t, srv, "POST", "/api/v1/projects/"+p.ID+"/scratchpads",
		map[string]string{"name": "extra"}, 201, &sp)

	// Item #1: agent proposes bug; user reclassifies to todo.
	var it1 domain.ScratchpadItem
	doJSON(t, srv, "POST", "/api/v1/scratchpads/"+sp.ID+"/items",
		map[string]string{"content": "fix login"}, 201, &it1)

	// Item #2: user will reject from inbox.
	var it2 domain.ScratchpadItem
	doJSON(t, srv, "POST", "/api/v1/scratchpads/"+sp.ID+"/items",
		map[string]string{"content": "random"}, 201, &it2)

	ag.Stop()

	// Reclassify it1 as todo.
	var rec domain.ScratchpadItem
	doJSON(t, srv, "POST", "/api/v1/items/"+it1.ID+"/reclassify",
		map[string]string{"category": "todo"}, 200, &rec)
	if rec.ClassificationState != "classified" || rec.ClassificationOverride != "todo" {
		t.Errorf("reclassify: state=%q override=%q", rec.ClassificationState, rec.ClassificationOverride)
	}
	// A Todo_Item should exist now.
	var todo domain.TodoItem
	doJSON(t, srv, "GET", "/api/v1/todos/"+rec.DerivedItemID, nil, 200, &todo)
	if todo.Origin != "agent-derived" {
		t.Errorf("todo origin = %q", todo.Origin)
	}

	// Reject it2.
	var rej domain.ScratchpadItem
	doJSON(t, srv, "POST", "/api/v1/items/"+it2.ID+"/reject", nil, 200, &rej)
	if rej.ClassificationState != "skipped" || rej.SkippedReason != "user_rejected_from_inbox" {
		t.Errorf("reject: state=%q reason=%q", rej.ClassificationState, rej.SkippedReason)
	}
}

func TestOverrideOnCreateSkipsModel(t *testing.T) {
	srv, ag := setup(t)

	var p domain.Project
	doJSON(t, srv, "POST", "/api/v1/projects", map[string]string{"name": "p"}, 201, &p)
	var sp domain.Scratchpad
	doJSON(t, srv, "POST", "/api/v1/projects/"+p.ID+"/scratchpads",
		map[string]string{"name": "extra"}, 201, &sp)

	var item domain.ScratchpadItem
	doJSON(t, srv, "POST", "/api/v1/scratchpads/"+sp.ID+"/items", map[string]string{
		"content":                 "whatever",
		"classification_override": "kb",
	}, 201, &item)

	ag.Stop()

	var got domain.ScratchpadItem
	doJSON(t, srv, "GET", "/api/v1/items/"+item.ID, nil, 200, &got)
	if got.ClassificationState != "classified" {
		t.Errorf("state = %q, want classified (override=kb should short-circuit)", got.ClassificationState)
	}
	var kb domain.KnowledgeEntry
	doJSON(t, srv, "GET", "/api/v1/kb/"+got.DerivedItemID, nil, 200, &kb)
	if kb.Content == "" || kb.Title == "" {
		t.Errorf("derived kb = %+v", kb)
	}
}

func TestTodoLifecycleAndCompletion(t *testing.T) {
	srv, _ := setup(t)

	var p domain.Project
	doJSON(t, srv, "POST", "/api/v1/projects", map[string]string{"name": "p"}, 201, &p)

	var todo domain.TodoItem
	doJSON(t, srv, "POST", "/api/v1/projects/"+p.ID+"/todos", map[string]string{
		"subject": "update Go", "priority": "high",
	}, 201, &todo)

	if todo.Status != "incomplete" || todo.Origin != "manual" || todo.Priority != "high" {
		t.Errorf("bad todo: %+v", todo)
	}

	// Mark complete.
	doJSON(t, srv, "PATCH", "/api/v1/todos/"+todo.ID, map[string]string{
		"status": "complete",
	}, 200, &todo)
	if todo.Status != "complete" || todo.CompletedAt == nil {
		t.Errorf("completion did not stick: %+v", todo)
	}
}

// TestTodoCompletionAutoFillsCommitSHA exercises the "flip to complete →
// stamp commit_sha from project HEAD" path added with priority #2, and
// the reverse "flip back to incomplete → clear all three fields" branch.
func TestTodoCompletionAutoFillsCommitSHA(t *testing.T) {
	srv, _ := setup(t)
	root, _, headSHA := seedRepo(t)

	var p domain.Project
	doJSON(t, srv, "POST", "/api/v1/projects", map[string]string{"name": "p"}, 201, &p)
	doJSON(t, srv, "PATCH", "/api/v1/projects/"+p.ID, map[string]string{
		"repo_root": root,
	}, 200, &p)

	var todo domain.TodoItem
	doJSON(t, srv, "POST", "/api/v1/projects/"+p.ID+"/todos", map[string]string{
		"subject": "ship the thing",
	}, 201, &todo)

	doJSON(t, srv, "PATCH", "/api/v1/todos/"+todo.ID, map[string]string{
		"status":     "complete",
		"commit_tag": "v9.9.9",
	}, 200, &todo)
	if todo.CompletedAt == nil {
		t.Errorf("completed_at not set")
	}
	if todo.CommitSHA != headSHA {
		t.Errorf("commit_sha = %q, want HEAD %q", todo.CommitSHA, headSHA)
	}
	if todo.CommitTag != "v9.9.9" {
		t.Errorf("commit_tag = %q", todo.CommitTag)
	}

	// Flip back to incomplete — all three should clear. Decode into a
	// fresh struct: the cleared fields are omitempty, so leaving the old
	// non-empty values in `todo` would mask a real bug.
	id := todo.ID
	var reopened domain.TodoItem
	doJSON(t, srv, "PATCH", "/api/v1/todos/"+id, map[string]string{
		"status": "incomplete",
	}, 200, &reopened)
	if reopened.CompletedAt != nil || reopened.CommitSHA != "" || reopened.CommitTag != "" {
		t.Errorf("reopen did not clear fields: %+v", reopened)
	}
}

// TestTodoCompletionClientSHAPreemptsAutoFill: if the client supplies its
// own commit_sha on the same PATCH that flips status, the explicit value
// wins over project HEAD.
func TestTodoCompletionClientSHAPreemptsAutoFill(t *testing.T) {
	srv, _ := setup(t)
	root, _, _ := seedRepo(t)

	var p domain.Project
	doJSON(t, srv, "POST", "/api/v1/projects", map[string]string{"name": "p"}, 201, &p)
	doJSON(t, srv, "PATCH", "/api/v1/projects/"+p.ID, map[string]string{
		"repo_root": root,
	}, 200, &p)

	var todo domain.TodoItem
	doJSON(t, srv, "POST", "/api/v1/projects/"+p.ID+"/todos", map[string]string{
		"subject": "ship",
	}, 201, &todo)

	doJSON(t, srv, "PATCH", "/api/v1/todos/"+todo.ID, map[string]string{
		"status":     "complete",
		"commit_sha": "deadbeef0000000000000000000000000000beef",
	}, 200, &todo)
	if todo.CommitSHA != "deadbeef0000000000000000000000000000beef" {
		t.Errorf("client SHA was overwritten: %q", todo.CommitSHA)
	}
}

func TestBugWorkflowTransitions(t *testing.T) {
	srv, _ := setup(t)

	var p domain.Project
	doJSON(t, srv, "POST", "/api/v1/projects", map[string]string{"name": "p"}, 201, &p)

	var bug domain.BugItem
	doJSON(t, srv, "POST", "/api/v1/projects/"+p.ID+"/bugs", map[string]string{
		"subject": "login 500", "severity": "major",
	}, 201, &bug)
	if bug.Status != "open" {
		t.Errorf("initial status = %q", bug.Status)
	}

	// Forward transition.
	doJSON(t, srv, "PATCH", "/api/v1/bugs/"+bug.ID, map[string]string{
		"status": "investigating",
	}, 200, &bug)
	if bug.Status != "investigating" {
		t.Errorf("status = %q", bug.Status)
	}

	// Workflow is now unrestricted (any → any). Backward transitions
	// succeed; non-fix terminals work too.
	doJSON(t, srv, "PATCH", "/api/v1/bugs/"+bug.ID, map[string]string{
		"status": "open",
	}, 200, &bug)
	if bug.Status != "open" {
		t.Errorf("backward transition: status = %q", bug.Status)
	}

	doJSON(t, srv, "PATCH", "/api/v1/bugs/"+bug.ID, map[string]string{
		"status": "not_a_bug",
	}, 200, &bug)
	if bug.Status != "not_a_bug" {
		t.Errorf("non-fix terminal: status = %q", bug.Status)
	}
	// Non-fix terminal stamps completed_at but does NOT auto-fill commit_sha.
	if bug.CompletedAt == nil {
		t.Error("non-fix terminal should still stamp completed_at")
	}
	if bug.CommitSHA != "" {
		t.Errorf("non-fix terminal must not attach a commit, got %q", bug.CommitSHA)
	}
}

// TestBugTerminalTransitionAutoFillsCommitSHA: when a bug crosses into a
// terminal status (fixed/verified/closed) for the first time, completed_at
// and commit_sha are stamped from project HEAD.
func TestBugTerminalTransitionAutoFillsCommitSHA(t *testing.T) {
	srv, _ := setup(t)
	root, _, headSHA := seedRepo(t)

	var p domain.Project
	doJSON(t, srv, "POST", "/api/v1/projects", map[string]string{"name": "p"}, 201, &p)
	doJSON(t, srv, "PATCH", "/api/v1/projects/"+p.ID, map[string]string{
		"repo_root": root,
	}, 200, &p)

	var bug domain.BugItem
	doJSON(t, srv, "POST", "/api/v1/projects/"+p.ID+"/bugs", map[string]string{
		"subject": "login 500",
	}, 201, &bug)

	// Walk the workflow up to "fixed" — only the open→inv→in-prog→fixed
	// transition counts as crossing into terminal.
	for _, s := range []string{"investigating", "in-progress", "fixed"} {
		doJSON(t, srv, "PATCH", "/api/v1/bugs/"+bug.ID, map[string]string{
			"status": s,
		}, 200, &bug)
	}
	if bug.CompletedAt == nil {
		t.Errorf("completed_at not set on terminal flip")
	}
	if bug.CommitSHA != headSHA {
		t.Errorf("commit_sha = %q, want HEAD %q", bug.CommitSHA, headSHA)
	}

	// Moving fixed → verified is terminal → terminal: completed_at should
	// stay pinned to the first crossing time.
	firstTS := bug.CompletedAt
	doJSON(t, srv, "PATCH", "/api/v1/bugs/"+bug.ID, map[string]string{
		"status": "verified",
	}, 200, &bug)
	if !bug.CompletedAt.Equal(*firstTS) {
		t.Errorf("completed_at was re-stamped: was %v now %v", firstTS, bug.CompletedAt)
	}
}

// Stage 4: Name field on scratchpad items round-trips through the API.
// Defaults to empty when omitted; PATCH updates work.
func TestScratchpadItemNameRoundTrip(t *testing.T) {
	srv, _ := setup(t)

	var p domain.Project
	doJSON(t, srv, "POST", "/api/v1/projects", map[string]string{"name": "p"}, 201, &p)
	var sp domain.Scratchpad
	doJSON(t, srv, "POST", "/api/v1/projects/"+p.ID+"/scratchpads",
		map[string]string{"name": "extra"}, 201, &sp)

	// Create with explicit name.
	var named domain.ScratchpadItem
	doJSON(t, srv, "POST", "/api/v1/scratchpads/"+sp.ID+"/items", map[string]string{
		"name":    "login crash investigation",
		"content": "fix login 500",
	}, 201, &named)
	if named.Name != "login crash investigation" {
		t.Errorf("create-with-name: got %q", named.Name)
	}

	// Create without a name → empty default.
	var unnamed domain.ScratchpadItem
	doJSON(t, srv, "POST", "/api/v1/scratchpads/"+sp.ID+"/items", map[string]string{
		"content": "second item",
	}, 201, &unnamed)
	if unnamed.Name != "" {
		t.Errorf("create-without-name: got %q, want empty", unnamed.Name)
	}

	// PATCH renames the unnamed item.
	var patched domain.ScratchpadItem
	doJSON(t, srv, "PATCH", "/api/v1/items/"+unnamed.ID, map[string]any{
		"name": "renamed by user",
	}, 200, &patched)
	if patched.Name != "renamed by user" {
		t.Errorf("PATCH-rename: got %q", patched.Name)
	}

	// PATCH with name="" clears it back to empty.
	doJSON(t, srv, "PATCH", "/api/v1/items/"+unnamed.ID, map[string]any{
		"name": "",
	}, 200, &patched)
	if patched.Name != "" {
		t.Errorf("PATCH-clear-name: got %q", patched.Name)
	}
}

func TestValidationErrors(t *testing.T) {
	srv, _ := setup(t)
	// Missing name on project create.
	resp, _ := srv.Client().Post(srv.URL+"/api/v1/projects", "application/json",
		strings.NewReader(`{}`))
	if resp.StatusCode != 400 {
		t.Errorf("missing name should 400, got %d", resp.StatusCode)
	}
	resp.Body.Close()

	// Unknown project on scratchpad create.
	resp, _ = srv.Client().Post(srv.URL+"/api/v1/projects/does-not-exist/scratchpads",
		"application/json", strings.NewReader(`{"name":"x"}`))
	if resp.StatusCode != 404 {
		t.Errorf("unknown parent should 404, got %d", resp.StatusCode)
	}
	resp.Body.Close()

	// Reclassify with invalid category.
	var p domain.Project
	doJSON(t, srv, "POST", "/api/v1/projects", map[string]string{"name": "p"}, 201, &p)
	var sp domain.Scratchpad
	doJSON(t, srv, "POST", "/api/v1/projects/"+p.ID+"/scratchpads",
		map[string]string{"name": "extra"}, 201, &sp)
}

// Archived items must not reserve canvas space: restack packs without them,
// and new-item stacking (NextAvailableGridRow) ignores their stale rows.
// Unarchive repositions to the next free row so nothing overlaps.
func TestRestackIgnoresArchivedItems(t *testing.T) {
	srv, ag, store := setupWithStore(t)
	defer ag.Stop()

	var p domain.Project
	doJSON(t, srv, "POST", "/api/v1/projects", map[string]string{"name": "p"}, 201, &p)
	var sp domain.Scratchpad
	doJSON(t, srv, "POST", "/api/v1/projects/"+p.ID+"/scratchpads",
		map[string]string{"name": "pad"}, 201, &sp)

	mk := func(content string) domain.ScratchpadItem {
		var it domain.ScratchpadItem
		doJSON(t, srv, "POST", "/api/v1/scratchpads/"+sp.ID+"/items",
			map[string]string{"content": content}, 201, &it)
		return it
	}
	top, mid, bot := mk("top"), mk("middle"), mk("bottom")

	// Spread them vertically: top rows 0-5, mid 10-15, bot 20-25.
	if err := store.RestackScratchpadItems(t.Context(), []storage.ItemGridPosition{
		{ID: top.ID, GridCol: 0, GridRow: 0},
		{ID: mid.ID, GridCol: 0, GridRow: 10},
		{ID: bot.ID, GridCol: 0, GridRow: 20},
	}); err != nil {
		t.Fatal(err)
	}

	// Archive the middle one, then restack: the two visible items must pack
	// tight — the archived item's rows 10-15 must not stay reserved.
	doJSON(t, srv, "POST", "/api/v1/items/"+mid.ID+"/archive", nil, 204, nil)
	doJSON(t, srv, "POST", "/api/v1/scratchpads/"+sp.ID+"/restack", map[string]string{"mode": "tidy"}, 200, nil)

	rows := map[string]int{}
	heightSum := 0
	for _, id := range []string{top.ID, bot.ID} {
		it, err := store.GetScratchpadItem(t.Context(), id)
		if err != nil {
			t.Fatal(err)
		}
		rows[id] = it.GridRow
		heightSum += it.GridH
	}
	for id, r := range rows {
		if r >= heightSum {
			t.Errorf("visible item %s at row %d — archived item's space still reserved (visible heights total %d)", id, r, heightSum)
		}
	}

	// New captures stack at the visible frontier, not below the archived ghost.
	next, err := store.NextAvailableGridRow(t.Context(), sp.ID)
	if err != nil {
		t.Fatal(err)
	}
	if next >= 10 {
		t.Errorf("NextAvailableGridRow = %d, still counting the archived item's stale rows", next)
	}

	// Unarchive: the item must come back at a free row (not overlapping).
	doJSON(t, srv, "POST", "/api/v1/items/"+mid.ID+"/unarchive", nil, 204, nil)
	back, err := store.GetScratchpadItem(t.Context(), mid.ID)
	if err != nil {
		t.Fatal(err)
	}
	if back.ArchivedAt != nil {
		t.Fatal("item still archived")
	}
	for _, id := range []string{top.ID, bot.ID} {
		it, _ := store.GetScratchpadItem(t.Context(), id)
		overlap := back.GridRow < it.GridRow+it.GridH && it.GridRow < back.GridRow+back.GridH &&
			back.GridCol < it.GridCol+it.GridW && it.GridCol < back.GridCol+back.GridW
		if overlap {
			t.Errorf("unarchived item (row %d h %d) overlaps visible item %s (row %d h %d)",
				back.GridRow, back.GridH, id, it.GridRow, it.GridH)
		}
	}
}

// Source guard (poke-1): a classified item's original text is immutable —
// content edits are refused with guidance unless flagged as a deliberate
// correction. Unclassified items stay freely editable.
func TestUpdateItemSourceGuard(t *testing.T) {
	srv, ag, store := setupWithStore(t)
	defer ag.Stop()

	var p domain.Project
	doJSON(t, srv, "POST", "/api/v1/projects", map[string]string{"name": "p"}, 201, &p)
	var sp domain.Scratchpad
	doJSON(t, srv, "POST", "/api/v1/projects/"+p.ID+"/scratchpads", map[string]string{"name": "pad"}, 201, &sp)
	var it domain.ScratchpadItem
	doJSON(t, srv, "POST", "/api/v1/scratchpads/"+sp.ID+"/items", map[string]string{"content": "Fixd the bugs in the splat component"}, 201, &it)

	// Unclassified: content edits flow freely (text still drives classification).
	doJSON(t, srv, "PATCH", "/api/v1/items/"+it.ID, map[string]any{"content": "Fixd the bugs in the splat component!"}, 200, nil)

	// Classify it, then try the graffiti: refused with guidance.
	got, err := store.GetScratchpadItem(t.Context(), it.ID)
	if err != nil {
		t.Fatal(err)
	}
	got.ClassificationState = "classified"
	if err := store.UpdateScratchpadItem(t.Context(), got); err != nil {
		t.Fatal(err)
	}
	doJSON(t, srv, "PATCH", "/api/v1/items/"+it.ID,
		map[string]any{"content": "Fixd the bugs\n\nUPDATE: fixed in abc123"}, 409, nil)

	// The deliberate typo fix passes with correction:true.
	doJSON(t, srv, "PATCH", "/api/v1/items/"+it.ID,
		map[string]any{"content": "Fix the bugs in the splat component!", "correction": true}, 200, nil)
	after, _ := store.GetScratchpadItem(t.Context(), it.ID)
	if after.Content != "Fix the bugs in the splat component!" {
		t.Errorf("content = %q, want the corrected text", after.Content)
	}
}
