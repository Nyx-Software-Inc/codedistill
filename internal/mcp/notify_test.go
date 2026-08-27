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
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// notifyMiddleware fires the change notifier after a mutating write tool
// succeeds, but not for reads or failed writes — the fix for bug b142cfbc
// (an LLM closing an item via MCP left the SPA stale because the write
// published no SSE update).
func TestNotifyMiddleware(t *testing.T) {
	cases := []struct {
		name     string
		tool     string
		result   *mcp.CallToolResult
		wantFire bool
	}{
		{"write success fires", "update_item", mcp.NewToolResultText("ok"), true},
		{"complete_item fires", "complete_item", mcp.NewToolResultText("ok"), true},
		{"move fires", "move_scratchpad_item", mcp.NewToolResultText("ok"), true},
		{"read does not fire", "read_item", mcp.NewToolResultText("ok"), false},
		{"list does not fire", "list_bugs", mcp.NewToolResultText("ok"), false},
		{"failed write does not fire", "update_item", mcp.NewToolResultError("bad"), false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			fired := 0
			tools := New(nil).WithNotifier(func() { fired++ })
			var next server.ToolHandlerFunc = func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
				return c.result, nil
			}
			var req mcp.CallToolRequest
			req.Params.Name = c.tool
			if _, err := tools.notifyMiddleware(next)(context.Background(), req); err != nil {
				t.Fatalf("middleware returned error: %v", err)
			}
			want := 0
			if c.wantFire {
				want = 1
			}
			if fired != want {
				t.Errorf("notifier fired %d times, want %d", fired, want)
			}
		})
	}
}

// A nil notifier (stdio MCP mode) must be safe — touch() is a no-op, no panic.
func TestNotifyMiddlewareNilNotifier(t *testing.T) {
	tools := New(nil) // no notifier wired
	var next server.ToolHandlerFunc = func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return mcp.NewToolResultText("ok"), nil
	}
	var req mcp.CallToolRequest
	req.Params.Name = "update_item"
	if _, err := tools.notifyMiddleware(next)(context.Background(), req); err != nil {
		t.Fatalf("middleware returned error: %v", err)
	}
}
