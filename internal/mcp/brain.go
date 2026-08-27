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

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"codedistill/internal/domain"
)

// brainEntry is a trimmed knowledge entry for the project-brain bundle — just
// the substance the agent needs as context, without sync/claim metadata.
type brainEntry struct {
	Number  int    `json:"number"`
	Title   string `json:"title"`
	Content string `json:"content"`
}

// get_project_brain hands the agent the project's curated context substrate
// (glass-box Phase 5): its architecture, conventions, and decisions — the "why"
// — grouped by kind. The agent reads this BEFORE building so it works with the
// project's established structure and rules instead of guessing. Backed by the
// KB; plain "reference" entries and deprecated ones are excluded.
func (t *Tools) registerGetProjectBrain(srv *server.MCPServer) {
	tool := mcp.NewTool("get_project_brain",
		mcp.WithDescription("Get the project's brain — its curated architecture, conventions, and decisions (with the why), grouped by kind. Read this BEFORE implementing so you follow the project's established structure and rules instead of guessing. Backed by the knowledge base."),
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
		list, err := t.Store.ListKnowledgeEntries(ctx, p.ID)
		if err != nil {
			return mcp.NewToolResultErrorFromErr("list kb", err), nil
		}
		grouped := map[string][]brainEntry{"architecture": {}, "convention": {}, "decision": {}}
		for _, e := range list {
			if domain.IsKnowledgeDone(e.Status) {
				continue // skip deprecated
			}
			if g, ok := grouped[e.Kind]; ok {
				grouped[e.Kind] = append(g, brainEntry{Number: e.Number, Title: e.Title, Content: e.Content})
			}
		}
		return mcp.NewToolResultJSON(map[string]any{
			"project":      p.Name,
			"architecture": grouped["architecture"],
			"conventions":  grouped["convention"],
			"decisions":    grouped["decision"],
		})
	})
}
