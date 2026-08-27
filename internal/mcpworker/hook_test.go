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
	"testing"
	"time"

	"codedistill/internal/domain"
	"codedistill/internal/mcpclient"
	"codedistill/internal/mcpmap"
	"codedistill/internal/mcpworker"
	"codedistill/internal/storage/sqlite"
)

func writeConfig(t *testing.T, store *sqlite.Store, shortType string, cfg *mcpworker.Config) {
	t.Helper()
	val, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("marshal cfg: %v", err)
	}
	if err := store.SetUserSetting(context.Background(), &domain.UserSetting{
		UserID: "local", Key: mcpworker.SettingsKey(shortType), Value: val,
	}); err != nil {
		t.Fatalf("set user setting: %v", err)
	}
}

func TestSettingsLookup_MissingReturnsNil(t *testing.T) {
	fx := newFixture(t)
	lookup := mcpworker.NewSettingsLookup(fx.store)

	cfg, err := lookup.Get(context.Background(), "local", "todo")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if cfg != nil {
		t.Errorf("expected nil for missing key, got %+v", cfg)
	}
}

func TestSettingsLookup_ReadsConfig(t *testing.T) {
	fx := newFixture(t)
	want := standardConfig()
	writeConfig(t, fx.store, "todo", want)

	lookup := mcpworker.NewSettingsLookup(fx.store)
	got, err := lookup.Get(context.Background(), "local", "todo")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got == nil {
		t.Fatal("expected config, got nil")
	}
	if got.Endpoint.URL != want.Endpoint.URL {
		t.Errorf("URL: got %q want %q", got.Endpoint.URL, want.Endpoint.URL)
	}
	if got.Mapping[mcpmap.OpCreate] != want.Mapping[mcpmap.OpCreate] {
		t.Errorf("create mapping: got %q want %q",
			got.Mapping[mcpmap.OpCreate], want.Mapping[mcpmap.OpCreate])
	}
}

func TestSettingsLookup_EmptyURLTreatedAsUnconfigured(t *testing.T) {
	fx := newFixture(t)
	cfg := &mcpworker.Config{Mapping: mcpmap.Mapping{mcpmap.OpCreate: "x"}}
	writeConfig(t, fx.store, "todo", cfg)

	lookup := mcpworker.NewSettingsLookup(fx.store)
	got, err := lookup.Get(context.Background(), "local", "todo")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got != nil {
		t.Errorf("expected nil for empty URL, got %+v", got)
	}
}

func TestHook_Notify_NoConfig_IsNoOp(t *testing.T) {
	fx := newFixture(t)
	lookup := mcpworker.NewSettingsLookup(fx.store)
	hook := mcpworker.NewHook(fx.store, lookup, nil)

	hook.Notify(context.Background(), "todo_item", fx.todoID, mcpworker.OpCreate, map[string]string{"x": "y"})

	if n := queueLen(t, fx.store); n != 0 {
		t.Errorf("expected empty queue with no config, got %d", n)
	}
	status, _, _ := readTodoSync(t, fx.store, fx.todoID)
	if status != "local-only" {
		t.Errorf("status should remain local-only, got %q", status)
	}
}

func TestHook_Notify_EnqueuesWhenConfigured(t *testing.T) {
	fx := newFixture(t)
	writeConfig(t, fx.store, "todo", standardConfig())
	lookup := mcpworker.NewSettingsLookup(fx.store)
	hook := mcpworker.NewHook(fx.store, lookup, nil)

	hook.Notify(context.Background(), "todo_item", fx.todoID, mcpworker.OpCreate, map[string]any{"title": "x"})

	if n := queueLen(t, fx.store); n != 1 {
		t.Fatalf("expected 1 queue row, got %d", n)
	}
	status, _, _ := readTodoSync(t, fx.store, fx.todoID)
	if status != "pending" {
		t.Errorf("status: got %q want pending", status)
	}

	rows, err := fx.store.ListDueExportItems(context.Background(), time.Now().UTC().Add(time.Hour), 10)
	if err != nil {
		t.Fatal(err)
	}
	if rows[0].Op != "create" {
		t.Errorf("op: got %q want create", rows[0].Op)
	}
	var args map[string]any
	if err := json.Unmarshal(rows[0].Payload, &args); err != nil {
		t.Fatalf("payload unmarshal: %v", err)
	}
	if args["title"] != "x" {
		t.Errorf("payload not marshaled correctly: %v", args)
	}
}

func TestHook_Notify_SkipsUnmappedOp(t *testing.T) {
	fx := newFixture(t)
	cfg := standardConfig()
	cfg.Mapping[mcpmap.OpDelete] = "" // user hasn't mapped delete
	writeConfig(t, fx.store, "todo", cfg)
	lookup := mcpworker.NewSettingsLookup(fx.store)
	hook := mcpworker.NewHook(fx.store, lookup, nil)

	hook.Notify(context.Background(), "todo_item", fx.todoID, mcpworker.OpDelete, map[string]any{"id": fx.todoID})

	if n := queueLen(t, fx.store); n != 0 {
		t.Errorf("unmapped op should not enqueue, got %d", n)
	}
}

// TestEndToEnd ties it all together: configure a destination, simulate
// an API write by calling the hook directly, then run one worker tick
// against a fake destination client and verify the call was made and
// the item was marked synced. This is the smallest realistic flow:
// the only thing not exercised is the actual HTTP transport (which
// has its own tests in mcpclient).
func TestEndToEnd_HookEnqueueWorkerDispatches(t *testing.T) {
	fx := newFixture(t)
	writeConfig(t, fx.store, "todo", standardConfig())
	lookup := mcpworker.NewSettingsLookup(fx.store)
	hook := mcpworker.NewHook(fx.store, lookup, nil)

	// Simulate the API handler: do the local write (already in fixture),
	// then notify the hook.
	hook.Notify(context.Background(), "todo_item", fx.todoID, mcpworker.OpCreate,
		map[string]any{"title": "do the thing"})

	// Confirm enqueue happened.
	if n := queueLen(t, fx.store); n != 1 {
		t.Fatalf("expected 1 queued row, got %d", n)
	}

	// Run the worker against a fake destination.
	cli := &fakeClient{
		resp: &mcpclient.CallResult{
			Content: []mcpclient.ContentBlock{
				{Type: "text", Text: `{"id":"linear-end-to-end"}`},
			},
		},
	}
	w := mcpworker.New(fx.store, mcpworker.Options{
		Lookup: lookup,
		Now:    func() time.Time { return time.Now().UTC().Add(time.Hour) }, // make row due
		Connect: func(ctx context.Context, ep mcpclient.Endpoint) (mcpworker.Client, error) {
			return cli, nil
		},
	})
	if err := w.Tick(context.Background()); err != nil {
		t.Fatalf("tick: %v", err)
	}

	if len(cli.calls) != 1 || cli.calls[0].name != "create_issue" {
		t.Errorf("expected create_issue call, got %+v", cli.calls)
	}
	status, remoteID, _ := readTodoSync(t, fx.store, fx.todoID)
	if status != "synced" {
		t.Errorf("status: got %q want synced", status)
	}
	if remoteID != "linear-end-to-end" {
		t.Errorf("remote_id: got %q want linear-end-to-end", remoteID)
	}
	if n := queueLen(t, fx.store); n != 0 {
		t.Errorf("queue should be empty, got %d", n)
	}
}

func TestHook_NilHook_IsSafe(t *testing.T) {
	// Calling Notify on a nil *Hook must not panic — handlers may have
	// nil hooks (tests, or when MCP-export isn't wired up).
	var h *mcpworker.Hook
	h.Notify(context.Background(), "todo_item", "anything", mcpworker.OpCreate, nil)
}
