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
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"codedistill/internal/domain"
)

// list_* tools take human-meaningful names rather than opaque ids since
// migration 0012 enforces UNIQUE(workspace_id, name) on projects and
// UNIQUE(project_id, name) on scratchpads. workspace_name is optional;
// it defaults to "local" (the implicit single-user workspace). Errors
// land inside the CallToolResult per MCP convention — the model can
// recover by calling list_projects / list_scratchpads after a miss.

// withWorkspaceName is the option list every project-scoped tool reuses
// for its optional workspace_name argument. Centralized so the
// description text stays consistent.
func withWorkspaceName() mcp.ToolOption {
	return mcp.WithString("workspace_name",
		mcp.Description("Workspace the project lives in. Optional; defaults to \"local\" for single-user installs."),
	)
}

func (t *Tools) registerListProjects(srv *server.MCPServer) {
	tool := mcp.NewTool("list_projects",
		mcp.WithDescription("List every project visible to this MCP session, across every workspace. Use the returned name values as project_name arguments to other list_* tools (in the same workspace)."),
		mcp.WithReadOnlyHintAnnotation(true),
	)
	srv.AddTool(tool, func(ctx context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		list, err := t.Store.ListProjects(ctx)
		if err != nil {
			return mcp.NewToolResultErrorFromErr("list projects", err), nil
		}
		if list == nil {
			list = []*domain.Project{}
		}
		return mcp.NewToolResultJSON(map[string]any{"projects": list})
	})
}

func (t *Tools) registerListScratchpads(srv *server.MCPServer) {
	tool := mcp.NewTool("list_scratchpads",
		mcp.WithDescription("List every scratchpad in a project. Returns the full row for each scratchpad — use the returned `name` values for list_scratchpad_items."),
		mcp.WithString("project_name",
			mcp.Required(),
			mcp.Description("The project name (from list_projects)."),
		),
		withWorkspaceName(),
		mcp.WithReadOnlyHintAnnotation(true),
	)
	srv.AddTool(tool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		p, err := t.resolveProject(ctx,
			mcp.ParseString(req, "workspace_name", ""),
			mcp.ParseString(req, "project_name", ""),
		)
		if err != nil {
			return mcp.NewToolResultErrorFromErr("resolve project", err), nil
		}
		list, err := t.Store.ListScratchpads(ctx, p.ID)
		if err != nil {
			return mcp.NewToolResultErrorFromErr("list scratchpads", err), nil
		}
		if list == nil {
			list = []*domain.Scratchpad{}
		}
		return mcp.NewToolResultJSON(map[string]any{"scratchpads": list})
	})
}

func (t *Tools) registerListScratchpadItems(srv *server.MCPServer) {
	tool := mcp.NewTool("list_scratchpad_items",
		mcp.WithDescription("List every item in a scratchpad — the raw paste-and-classify queue entries. Includes hidden and pending-review items; filter client-side if needed."),
		mcp.WithString("project_name",
			mcp.Required(),
			mcp.Description("The project name (from list_projects)."),
		),
		mcp.WithString("scratchpad_name",
			mcp.Required(),
			mcp.Description("The scratchpad name within that project (from list_scratchpads)."),
		),
		mcp.WithString("proposed_category",
			mcp.Description("Optional: keep only items the classifier proposed as this category — one of todo, bug, knowledge, use_case. Case-insensitive; empty/omitted returns all."),
		),
		withWorkspaceName(),
		mcp.WithReadOnlyHintAnnotation(true),
	)
	srv.AddTool(tool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		sp, err := t.resolveScratchpad(ctx,
			mcp.ParseString(req, "workspace_name", ""),
			mcp.ParseString(req, "project_name", ""),
			mcp.ParseString(req, "scratchpad_name", ""),
		)
		if err != nil {
			return mcp.NewToolResultErrorFromErr("resolve scratchpad", err), nil
		}
		list, err := t.Store.ListScratchpadItems(ctx, sp.ID)
		if err != nil {
			return mcp.NewToolResultErrorFromErr("list scratchpad items", err), nil
		}
		if cat := strings.ToLower(strings.TrimSpace(mcp.ParseString(req, "proposed_category", ""))); cat != "" {
			filtered := make([]*domain.ScratchpadItem, 0, len(list))
			for _, it := range list {
				if strings.ToLower(it.ProposedCategory) == cat {
					filtered = append(filtered, it)
				}
			}
			list = filtered
		}
		if list == nil {
			list = []*domain.ScratchpadItem{}
		}
		return mcp.NewToolResultJSON(map[string]any{"items": list})
	})
}

// includeDoneOption is the shared description for the include_done
// boolean param threaded through every list_* tool. Default false so
// agents see only open work unless they explicitly ask for history.
func includeDoneOption() mcp.ToolOption {
	return mcp.WithBoolean("include_done",
		mcp.Description("Include items in a terminal/done state (complete, fixed, implemented, deprecated, etc.). Default false — open and in-progress only. Set true when you need historical context, an audit log, or to confirm whether a specific item was completed."),
	)
}

func (t *Tools) registerListTodos(srv *server.MCPServer) {
	tool := mcp.NewTool("list_todos",
		mcp.WithDescription("List todos in a project. Default: only open + in-progress. Pass include_done=true to also see complete + abandoned."),
		mcp.WithString("project_name",
			mcp.Required(),
			mcp.Description("The project name (from list_projects)."),
		),
		withWorkspaceName(),
		includeDoneOption(),
		mcp.WithReadOnlyHintAnnotation(true),
	)
	srv.AddTool(tool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		p, err := t.resolveProject(ctx,
			mcp.ParseString(req, "workspace_name", ""),
			mcp.ParseString(req, "project_name", ""),
		)
		if err != nil {
			return mcp.NewToolResultErrorFromErr("resolve project", err), nil
		}
		list, err := t.Store.ListTodoItems(ctx, p.ID)
		if err != nil {
			return mcp.NewToolResultErrorFromErr("list todos", err), nil
		}
		if !mcp.ParseBoolean(req, "include_done", false) {
			out := list[:0]
			for _, it := range list {
				if !domain.IsTodoDone(it.Status) {
					out = append(out, it)
				}
			}
			list = out
		}
		if list == nil {
			list = []*domain.TodoItem{}
		}
		return mcp.NewToolResultJSON(map[string]any{"todos": list})
	})
}

func (t *Tools) registerListBugs(srv *server.MCPServer) {
	tool := mcp.NewTool("list_bugs",
		mcp.WithDescription("List bugs in a project. Default: only active (open, investigating, in-progress). Pass include_done=true to also see fixed/verified/closed and not_a_bug/wont_fix/duplicate."),
		mcp.WithString("project_name",
			mcp.Required(),
			mcp.Description("The project name (from list_projects)."),
		),
		withWorkspaceName(),
		includeDoneOption(),
		mcp.WithReadOnlyHintAnnotation(true),
	)
	srv.AddTool(tool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		p, err := t.resolveProject(ctx,
			mcp.ParseString(req, "workspace_name", ""),
			mcp.ParseString(req, "project_name", ""),
		)
		if err != nil {
			return mcp.NewToolResultErrorFromErr("resolve project", err), nil
		}
		list, err := t.Store.ListBugItems(ctx, p.ID)
		if err != nil {
			return mcp.NewToolResultErrorFromErr("list bugs", err), nil
		}
		if !mcp.ParseBoolean(req, "include_done", false) {
			out := list[:0]
			for _, it := range list {
				if !domain.IsBugDone(it.Status) {
					out = append(out, it)
				}
			}
			list = out
		}
		if list == nil {
			list = []*domain.BugItem{}
		}
		return mcp.NewToolResultJSON(map[string]any{"bugs": list})
	})
}

func (t *Tools) registerListKB(srv *server.MCPServer) {
	tool := mcp.NewTool("list_kb",
		mcp.WithDescription("List knowledge-base entries in a project. Default: active only. Pass include_done=true to also see deprecated entries (kept for history)."),
		mcp.WithString("project_name",
			mcp.Required(),
			mcp.Description("The project name (from list_projects)."),
		),
		withWorkspaceName(),
		includeDoneOption(),
		mcp.WithReadOnlyHintAnnotation(true),
	)
	srv.AddTool(tool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		p, err := t.resolveProject(ctx,
			mcp.ParseString(req, "workspace_name", ""),
			mcp.ParseString(req, "project_name", ""),
		)
		if err != nil {
			return mcp.NewToolResultErrorFromErr("resolve project", err), nil
		}
		list, err := t.Store.ListKnowledgeEntries(ctx, p.ID)
		if err != nil {
			return mcp.NewToolResultErrorFromErr("list kb", err), nil
		}
		if !mcp.ParseBoolean(req, "include_done", false) {
			out := list[:0]
			for _, it := range list {
				if !domain.IsKnowledgeDone(it.Status) {
					out = append(out, it)
				}
			}
			list = out
		}
		if list == nil {
			list = []*domain.KnowledgeEntry{}
		}
		return mcp.NewToolResultJSON(map[string]any{"entries": list})
	})
}

func (t *Tools) registerListUseCases(srv *server.MCPServer) {
	tool := mcp.NewTool("list_use_cases",
		mcp.WithDescription("List use cases in a project — persistent capabilities the product enables (UC-{n}). Default: only open/approved/in-progress. Pass include_done=true to also see completed + rejected."),
		mcp.WithString("project_name",
			mcp.Required(),
			mcp.Description("The project name (from list_projects)."),
		),
		withWorkspaceName(),
		includeDoneOption(),
		mcp.WithReadOnlyHintAnnotation(true),
	)
	srv.AddTool(tool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		p, err := t.resolveProject(ctx,
			mcp.ParseString(req, "workspace_name", ""),
			mcp.ParseString(req, "project_name", ""),
		)
		if err != nil {
			return mcp.NewToolResultErrorFromErr("resolve project", err), nil
		}
		list, err := t.Store.ListUseCaseItems(ctx, p.ID)
		if err != nil {
			return mcp.NewToolResultErrorFromErr("list use_cases", err), nil
		}
		if !mcp.ParseBoolean(req, "include_done", false) {
			out := list[:0]
			for _, it := range list {
				if !domain.IsUseCaseDone(it.Status) {
					out = append(out, it)
				}
			}
			list = out
		}
		if list == nil {
			list = []*domain.UseCaseItem{}
		}
		return mcp.NewToolResultJSON(map[string]any{"use_cases": list})
	})
}
