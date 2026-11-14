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
	"errors"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	gitpkg "codedistill/internal/git"
	"codedistill/internal/storage"
)

// list_review_comments closes the review loop the other way: an agent whose
// branch a human reviewed reads what the reviewer said, anchored to lines.
// A free read: it resolves the changeset through git and reads the comments;
// it mints nothing (a changeset nobody commented on is simply empty).
func (t *Tools) registerListReviewComments(srv *server.MCPServer) {
	tool := mcp.NewTool("list_review_comments",
		mcp.WithDescription("List the human review comments on a changeset — a commit, a branch, or the working copy — each anchored to a file and line range (side \"old\" or \"new\"). Use this after a reviewer looked at your branch to read what they said. Reads only."),
		mcp.WithString("project_name", mcp.Required(), mcp.Description("The project name (from list_projects).")),
		mcp.WithString("workspace_name", mcp.Description("Optional workspace name to disambiguate a duplicate project name.")),
		mcp.WithString("head", mcp.Required(), mcp.Description("A commit SHA, a branch name, or \"working\" for the working copy.")),
		mcp.WithString("base", mcp.Description("Optional base. Default: the commit's first parent; for a branch name, its merge-base with the default branch; for the working copy, HEAD.")),
		mcp.WithBoolean("include_resolved", mcp.Description("Include resolved comments (default false).")),
	)
	srv.AddTool(tool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		projectName := strings.TrimSpace(mcp.ParseString(req, "project_name", ""))
		workspaceName := strings.TrimSpace(mcp.ParseString(req, "workspace_name", ""))
		head := strings.TrimSpace(mcp.ParseString(req, "head", ""))
		base := strings.TrimSpace(mcp.ParseString(req, "base", ""))
		includeResolved := mcp.ParseBoolean(req, "include_resolved", false)
		if projectName == "" || head == "" {
			return mcp.NewToolResultError("project_name and head are required"), nil
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
		// A branch name with no base means "vs the default branch", which is
		// what the UI stores for a branch pick; a bare commit means vs parent.
		if base == "" && head != gitpkg.Working {
			if branches, def, err := repo.Branches(); err == nil {
				for _, b := range branches {
					if b.Name == head && b.Name != def {
						if mb, err := repo.MergeBase(head, def); err == nil {
							base = mb
						}
						break
					}
				}
			}
		}
		baseSHA, headSHA, err := repo.ResolveChangeset(base, head)
		if err != nil {
			return mcp.NewToolResultErrorFromErr("resolve changeset", err), nil
		}
		cs, err := t.Store.FindChangeset(ctx, proj.ID, baseSHA, headSHA)
		if errors.Is(err, storage.ErrNotFound) {
			return mcp.NewToolResultJSON(map[string]any{
				"base": baseSHA, "head": headSHA, "comments": []any{},
			})
		}
		if err != nil {
			return mcp.NewToolResultErrorFromErr("changeset", err), nil
		}
		list, err := t.Store.ListReviewComments(ctx, cs.ID)
		if err != nil {
			return mcp.NewToolResultErrorFromErr("comments", err), nil
		}
		out := make([]map[string]any, 0, len(list))
		for _, c := range list {
			if c.ResolvedAt != nil && !includeResolved {
				continue
			}
			m := map[string]any{
				"id": c.ID, "path": c.Path, "side": c.Side,
				"line_start": c.LineStart, "line_end": c.LineEnd,
				"body": c.Body, "author": c.Author, "created_at": c.CreatedAt,
				"resolved": c.ResolvedAt != nil,
			}
			if c.PromotedOwnerID != "" {
				m["promoted_to"] = map[string]string{"type": c.PromotedOwnerType, "id": c.PromotedOwnerID}
			}
			out = append(out, m)
		}
		return mcp.NewToolResultJSON(map[string]any{
			"changeset_id": cs.ID, "kind": cs.Kind, "base": cs.BaseSHA, "head": cs.HeadSHA, "ref": cs.Ref,
			"comments": out,
		})
	})
}
