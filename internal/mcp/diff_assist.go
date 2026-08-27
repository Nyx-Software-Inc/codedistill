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

	gitpkg "codedistill/internal/git"
)

// get_commit_changes is diff-assist (Slice 3): it returns a commit's changed
// files, each with the NEW-side line ranges of every edit region. This turns
// multi-item-commit attribution from recall into tagging — the agent calls it
// once, then passes each item's subset of files/line-ranges to complete_item.
// A read: it opens git and returns the diff shape, mutating nothing.
func (t *Tools) registerGetCommitChanges(srv *server.MCPServer) {
	tool := mcp.NewTool("get_commit_changes",
		mcp.WithDescription("List the files a commit changed, each with the line ranges (hunks) it touched in the NEW version of the file. Use this to attribute a commit that fixed several items: call it once, then pass each item's subset of files/line-ranges to complete_item's `files`. Reads git; changes nothing."),
		mcp.WithString("project_name", mcp.Required(), mcp.Description("The project name (from list_projects).")),
		mcp.WithString("workspace_name", mcp.Description("Optional workspace name to disambiguate a duplicate project name.")),
		mcp.WithString("commit", mcp.Required(), mcp.Description("The commit SHA to inspect (7-40 hex; short SHAs resolve).")),
	)
	srv.AddTool(tool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		projectName := strings.TrimSpace(mcp.ParseString(req, "project_name", ""))
		workspaceName := strings.TrimSpace(mcp.ParseString(req, "workspace_name", ""))
		commit := strings.TrimSpace(mcp.ParseString(req, "commit", ""))
		if projectName == "" || commit == "" {
			return mcp.NewToolResultError("project_name and commit are required"), nil
		}
		proj, err := t.resolveProject(ctx, workspaceName, projectName)
		if err != nil {
			return mcp.NewToolResultErrorFromErr("project", err), nil
		}
		if strings.TrimSpace(proj.RepoRoot) == "" {
			return mcp.NewToolResultError("this project has no repo configured (set repo_root first)"), nil
		}
		repo, err := gitpkg.Open(proj.RepoRoot)
		if err != nil {
			return mcp.NewToolResultErrorFromErr("open repo", err), nil
		}
		changes, err := repo.CommitChanges(ctx, commit)
		if err != nil {
			return mcp.NewToolResultErrorFromErr("commit changes", err), nil
		}
		return mcp.NewToolResultJSON(map[string]any{
			"commit": commit,
			"files":  changes,
		})
	})
}
