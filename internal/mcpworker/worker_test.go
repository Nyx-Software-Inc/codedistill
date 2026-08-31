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

package mcpworker_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"codedistill/internal/domain"
	"codedistill/internal/mcpclient"
	"codedistill/internal/mcpmap"
	"codedistill/internal/mcpworker"
	"codedistill/internal/storage/sqlite"
)

// fixture wires an in-memory store with a project + a todo we can
// enqueue against. Returns the store, the todo id (used as owner_id in
// queue rows), and a cleanup func.
type fixture struct {
	store     *sqlite.Store
	todoID    string
	projectID string
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	store, err := sqlite.Open(":memory:")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { store.Close() })
	ctx := context.Background()
	if err := store.Migrate(ctx); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	proj := &domain.Project{ID: "proj-1", WorkspaceID: "local", Name: "test", CreatedAt: time.Now()}
	if err := store.CreateProject(ctx, proj); err != nil {
		t.Fatalf("create project: %v", err)
	}
	todo := &domain.TodoItem{
		ID: "todo-1", ProjectID: proj.ID, CreatorID: "local",
		Subject: "do the thing", Priority: "none", Status: "incomplete",
		Origin: "manual", Visibility: "project", CreatedAt: time.Now(),
	}
	if err := store.CreateTodoItem(ctx, todo); err != nil {
		t.Fatalf("create todo: %v", err)
	}
	return &fixture{store: store, todoID: todo.ID, projectID: proj.ID}
}

// fakeClient records calls and returns canned responses.
type fakeClient struct {
	mu      sync.Mutex
	calls   []fakeCall
	resp    *mcpclient.CallResult
	respErr error
	closed  bool
}

type fakeCall struct {
	name string
	args map[string]any
}

func (f *fakeClient) CallTool(ctx context.Context, name string, args map[string]any) (*mcpclient.CallResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, fakeCall{name: name, args: args})
	return f.resp, f.respErr
}
func (f *fakeClient) Close() error { f.closed = true; return nil }

// fakeLookup returns a canned config or nil/error per shortType.
type fakeLookup struct {
	configs map[string]*mcpworker.Config
	err     error
}

func (f *fakeLookup) Get(ctx context.Context, userID, shortType string) (*mcpworker.Config, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.configs[shortType], nil
}

func standardConfig() *mcpworker.Config {
	return &mcpworker.Config{
		Endpoint: mcpclient.Endpoint{URL: "http://destination.example/mcp"},
		Mapping: mcpmap.Mapping{
			mcpmap.OpCreate:       "create_issue",
			mcpmap.OpUpdate:       "update_issue",
			mcpmap.OpStatusChange: "close_issue",
			mcpmap.OpDelete:       "delete_issue",
		},
	}
}

func enqueue(t *testing.T, store *sqlite.Store, owner, op string, payload string) *domain.ExportQueueItem {
	t.Helper()
	// NextAttemptAt set to an hour in the past so any test "now" sees
	// the row as due. Tests that want a future-scheduled row call
	// RetryExportQueueItem to push it out.
	item := &domain.ExportQueueItem{
		UserID: "local", OwnerType: "todo_item", OwnerID: owner,
		Op: op, Payload: json.RawMessage(payload),
		NextAttemptAt: time.Now().UTC().Add(-time.Hour),
	}
	if err := store.EnqueueExportItem(context.Background(), item); err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	return item
}

func newWorker(store *sqlite.Store, client *fakeClient, lookup *fakeLookup, now time.Time) *mcpworker.Worker {
	return mcpworker.New(store, mcpworker.Options{
		PollEvery: 10 * time.Millisecond,
		MaxBatch:  10,
		Lookup:    lookup,
		Now:       func() time.Time { return now },
		Connect: func(ctx context.Context, ep mcpclient.Endpoint) (mcpworker.Client, error) {
			return client, nil
		},
	})
}

// readTodoSync queries the per-item sync columns directly so tests
// don't depend on a domain reader (the sync columns aren't surfaced
// on TodoItem yet — that's a separate slice).
func readTodoSync(t *testing.T, store *sqlite.Store, id string) (status, remoteID, lastErr string) {
	t.Helper()
	row := store.DB.QueryRowContext(context.Background(),
		`SELECT sync_status, remote_id, last_sync_error FROM todo_items WHERE id = ?`, id)
	if err := row.Scan(&status, &remoteID, &lastErr); err != nil {
		t.Fatalf("read sync: %v", err)
	}
	return
}

func queueLen(t *testing.T, store *sqlite.Store) int {
	t.Helper()
	row := store.DB.QueryRowContext(context.Background(), `SELECT COUNT(*) FROM mcp_export_queue`)
	var n int
	if err := row.Scan(&n); err != nil {
		t.Fatalf("count queue: %v", err)
	}
	return n
}

// --- tests ---

func TestTick_CreateSuccess_ExtractsRemoteID(t *testing.T) {
	fx := newFixture(t)
	now := time.Now().UTC().Truncate(time.Second)
	enqueue(t, fx.store, fx.todoID, "create", `{"title":"do the thing"}`)

	cli := &fakeClient{
		resp: &mcpclient.CallResult{
			Content: []mcpclient.ContentBlock{{Type: "text", Text: `{"id":"linear-abc","status":"open"}`}},
		},
	}
	lookup := &fakeLookup{configs: map[string]*mcpworker.Config{"todo": standardConfig()}}
	w := newWorker(fx.store, cli, lookup, now)

	if err := w.Tick(context.Background()); err != nil {
		t.Fatalf("tick: %v", err)
	}

	// Tool was called with the right name + args.
	if len(cli.calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(cli.calls))
	}
	if cli.calls[0].name != "create_issue" {
		t.Errorf("tool name: got %q want %q", cli.calls[0].name, "create_issue")
	}
	if cli.calls[0].args["title"] != "do the thing" {
		t.Errorf("args: %v", cli.calls[0].args)
	}

	// Item is synced with extracted remote id.
	status, remoteID, lastErr := readTodoSync(t, fx.store, fx.todoID)
	if status != "synced" {
		t.Errorf("status: got %q want synced", status)
	}
	if remoteID != "linear-abc" {
		t.Errorf("remote_id: got %q want linear-abc", remoteID)
	}
	if lastErr != "" {
		t.Errorf("last_sync_error: got %q want empty", lastErr)
	}

	// Queue row is gone.
	if n := queueLen(t, fx.store); n != 0 {
		t.Errorf("queue should be empty after success, has %d", n)
	}
}

func TestTick_UpdateSuccess_PreservesRemoteID(t *testing.T) {
	fx := newFixture(t)
	now := time.Now().UTC()

	// Pre-set remote_id (as if a prior 'create' had succeeded).
	_, err := fx.store.DB.ExecContext(context.Background(),
		`UPDATE todo_items SET remote_id = ?, sync_status = 'synced' WHERE id = ?`,
		"linear-existing", fx.todoID)
	if err != nil {
		t.Fatalf("seed remote_id: %v", err)
	}

	enqueue(t, fx.store, fx.todoID, "update", `{"title":"renamed"}`)

	cli := &fakeClient{
		resp: &mcpclient.CallResult{
			Content: []mcpclient.ContentBlock{{Type: "text", Text: `{"id":"different-id"}`}},
		},
	}
	lookup := &fakeLookup{configs: map[string]*mcpworker.Config{"todo": standardConfig()}}
	w := newWorker(fx.store, cli, lookup, now)

	if err := w.Tick(context.Background()); err != nil {
		t.Fatalf("tick: %v", err)
	}

	_, remoteID, _ := readTodoSync(t, fx.store, fx.todoID)
	if remoteID != "linear-existing" {
		t.Errorf("update should not overwrite remote_id; got %q want linear-existing", remoteID)
	}
}

func TestTick_IsErrorResponse_FailsWithoutRetry(t *testing.T) {
	fx := newFixture(t)
	now := time.Now().UTC()
	enqueue(t, fx.store, fx.todoID, "create", `{}`)

	cli := &fakeClient{
		resp: &mcpclient.CallResult{
			IsError: true,
			Content: []mcpclient.ContentBlock{{Type: "text", Text: "team_not_found"}},
		},
	}
	lookup := &fakeLookup{configs: map[string]*mcpworker.Config{"todo": standardConfig()}}
	w := newWorker(fx.store, cli, lookup, now)

	if err := w.Tick(context.Background()); err != nil {
		t.Fatalf("tick: %v", err)
	}

	status, _, lastErr := readTodoSync(t, fx.store, fx.todoID)
	if status != "failed" {
		t.Errorf("status: got %q want failed", status)
	}
	if lastErr != "team_not_found" {
		t.Errorf("last_sync_error: got %q want team_not_found", lastErr)
	}
	if n := queueLen(t, fx.store); n != 0 {
		t.Errorf("IsError should drop the queue row, %d left", n)
	}
}

func TestTick_TransportError_SchedulesRetry(t *testing.T) {
	fx := newFixture(t)
	now := time.Now().UTC()
	enqueue(t, fx.store, fx.todoID, "create", `{}`)

	cli := &fakeClient{respErr: errors.New("dial tcp: connection refused")}
	lookup := &fakeLookup{configs: map[string]*mcpworker.Config{"todo": standardConfig()}}
	w := newWorker(fx.store, cli, lookup, now)

	if err := w.Tick(context.Background()); err != nil {
		t.Fatalf("tick: %v", err)
	}

	// Item should NOT be marked failed — still pending; transport
	// errors are retryable.
	status, _, _ := readTodoSync(t, fx.store, fx.todoID)
	if status == "failed" {
		t.Errorf("transport error should not mark item failed yet")
	}

	// Queue row should still exist with attempts=1 and a future
	// next_attempt_at.
	if n := queueLen(t, fx.store); n != 1 {
		t.Fatalf("queue row should remain, got %d", n)
	}
	rows, err := fx.store.ListDueExportItems(context.Background(), now.Add(24*time.Hour), 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(rows))
	}
	if rows[0].Attempts != 1 {
		t.Errorf("attempts: got %d want 1", rows[0].Attempts)
	}
	if !rows[0].NextAttemptAt.After(now) {
		t.Errorf("next_attempt_at should be in the future, got %v (now=%v)", rows[0].NextAttemptAt, now)
	}
	if rows[0].LastError == "" {
		t.Errorf("last_error should be recorded")
	}
}

func TestTick_TransportError_HitsMaxAttempts_PermanentFails(t *testing.T) {
	fx := newFixture(t)
	now := time.Now().UTC()

	// Enqueue with attempts already at MaxAttempts-1; one more transport
	// error should tip into permanent fail.
	item := enqueue(t, fx.store, fx.todoID, "create", `{}`)
	if err := fx.store.RetryExportQueueItem(context.Background(), item.ID, 7, now, "previous failures"); err != nil {
		t.Fatalf("seed attempts: %v", err)
	}

	cli := &fakeClient{respErr: errors.New("still down")}
	lookup := &fakeLookup{configs: map[string]*mcpworker.Config{"todo": standardConfig()}}
	w := newWorker(fx.store, cli, lookup, now)

	if err := w.Tick(context.Background()); err != nil {
		t.Fatalf("tick: %v", err)
	}

	status, _, lastErr := readTodoSync(t, fx.store, fx.todoID)
	if status != "failed" {
		t.Errorf("status: got %q want failed (gave up)", status)
	}
	if lastErr == "" || !contains(lastErr, "gave up") {
		t.Errorf("last_sync_error should mention gave up, got %q", lastErr)
	}
	if n := queueLen(t, fx.store); n != 0 {
		t.Errorf("queue row should be deleted after max attempts, %d left", n)
	}
}

func TestTick_OpUnmapped_FailsWithoutRetry(t *testing.T) {
	fx := newFixture(t)
	now := time.Now().UTC()
	enqueue(t, fx.store, fx.todoID, "delete", `{}`)

	cfg := standardConfig()
	cfg.Mapping[mcpmap.OpDelete] = "" // user hasn't mapped delete

	cli := &fakeClient{}
	lookup := &fakeLookup{configs: map[string]*mcpworker.Config{"todo": cfg}}
	w := newWorker(fx.store, cli, lookup, now)

	if err := w.Tick(context.Background()); err != nil {
		t.Fatalf("tick: %v", err)
	}

	if len(cli.calls) != 0 {
		t.Errorf("should not call any tool when op is unmapped, got %d calls", len(cli.calls))
	}
	status, _, lastErr := readTodoSync(t, fx.store, fx.todoID)
	if status != "failed" {
		t.Errorf("status: got %q want failed", status)
	}
	if !contains(lastErr, "delete") || !contains(lastErr, "not mapped") {
		t.Errorf("last_sync_error should explain the gap, got %q", lastErr)
	}
}

func TestTick_StatusChange_FallsBackToUpdateToolWhenUnmapped(t *testing.T) {
	fx := newFixture(t)
	now := time.Now().UTC()
	enqueue(t, fx.store, fx.todoID, "status_change", `{"status":"done"}`)

	cfg := standardConfig()
	cfg.Mapping[mcpmap.OpStatusChange] = "" // destination didn't map status_change

	cli := &fakeClient{
		resp: &mcpclient.CallResult{
			Content: []mcpclient.ContentBlock{{Type: "text", Text: `{"id":"linear-x"}`}},
		},
	}
	lookup := &fakeLookup{configs: map[string]*mcpworker.Config{"todo": cfg}}
	w := newWorker(fx.store, cli, lookup, now)

	if err := w.Tick(context.Background()); err != nil {
		t.Fatalf("tick: %v", err)
	}

	// Should not drop the change — it routes through the update tool instead.
	if len(cli.calls) != 1 {
		t.Fatalf("expected 1 call (fallback to update), got %d", len(cli.calls))
	}
	if cli.calls[0].name != "update_issue" {
		t.Errorf("expected fallback to update tool %q, got %q", "update_issue", cli.calls[0].name)
	}
}

func TestTick_NoConfigForType_DropsRow(t *testing.T) {
	fx := newFixture(t)
	now := time.Now().UTC()
	enqueue(t, fx.store, fx.todoID, "create", `{}`)

	// Lookup returns nil → user un-configured the destination after enqueue.
	lookup := &fakeLookup{configs: map[string]*mcpworker.Config{}}
	cli := &fakeClient{}
	w := newWorker(fx.store, cli, lookup, now)

	if err := w.Tick(context.Background()); err != nil {
		t.Fatalf("tick: %v", err)
	}

	if len(cli.calls) != 0 {
		t.Errorf("should not call any tool with no config, got %d", len(cli.calls))
	}
	if n := queueLen(t, fx.store); n != 0 {
		t.Errorf("queue row should be dropped, %d left", n)
	}
	// Item sync_status should be untouched (still local-only).
	status, _, _ := readTodoSync(t, fx.store, fx.todoID)
	if status != "local-only" {
		t.Errorf("status should remain local-only, got %q", status)
	}
}

func TestTick_OnlyDueItemsProcessed(t *testing.T) {
	fx := newFixture(t)
	now := time.Now().UTC()

	// Two queue items: one due, one scheduled in the future.
	due := enqueue(t, fx.store, fx.todoID, "create", `{}`)
	future := enqueue(t, fx.store, fx.todoID, "update", `{}`)
	if err := fx.store.RetryExportQueueItem(context.Background(), future.ID, 1, now.Add(1*time.Hour), ""); err != nil {
		t.Fatal(err)
	}
	_ = due

	cli := &fakeClient{
		resp: &mcpclient.CallResult{Content: []mcpclient.ContentBlock{{Type: "text", Text: `{"id":"x"}`}}},
	}
	lookup := &fakeLookup{configs: map[string]*mcpworker.Config{"todo": standardConfig()}}
	w := newWorker(fx.store, cli, lookup, now)

	if err := w.Tick(context.Background()); err != nil {
		t.Fatalf("tick: %v", err)
	}

	if len(cli.calls) != 1 {
		t.Errorf("expected 1 call (only the due item), got %d", len(cli.calls))
	}
	if n := queueLen(t, fx.store); n != 1 {
		t.Errorf("future-scheduled row should still be queued, queue len %d", n)
	}
}

func TestOwnerTypeToShort_Roundtrip(t *testing.T) {
	cases := map[string]string{
		"todo_item":       "todo",
		"bug_item":        "bug",
		"knowledge_entry": "kb",
		"use_case_item":   "use_case",
	}
	for ot, want := range cases {
		got, err := mcpworker.OwnerTypeToShort(ot)
		if err != nil {
			t.Errorf("%s: %v", ot, err)
		}
		if got != want {
			t.Errorf("%s: got %q want %q", ot, got, want)
		}
	}
	if _, err := mcpworker.OwnerTypeToShort("nonsense"); err == nil {
		t.Error("expected error for unknown owner_type")
	}
}

func TestRun_StopsOnContextCancel(t *testing.T) {
	fx := newFixture(t)
	cli := &fakeClient{}
	lookup := &fakeLookup{configs: map[string]*mcpworker.Config{}}
	w := newWorker(fx.store, cli, lookup, time.Now().UTC())

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	done := make(chan error, 1)
	go func() { done <- w.Run(ctx) }()

	select {
	case err := <-done:
		if err != nil {
			t.Errorf("Run returned err: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not stop after ctx cancel")
	}
}

func contains(s, sub string) bool { return len(s) >= len(sub) && (sub == "" || indexOf(s, sub) >= 0) }
func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

// Pre-emptively pin the fmt import used in helper construction.
var _ = fmt.Sprintf
