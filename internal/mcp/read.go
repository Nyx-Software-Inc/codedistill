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
	"fmt"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"codedistill/internal/domain"
)

// scratchpadItemMCPView is the MCP-side wrapper that lifts a
// ScratchpadItem and tacks on the presigned blob URL fields when the
// item carries binary content. Embedded pointer so the JSON
// marshaller hoists all base fields to the outer level — clients see
// one flat object, not a nested {item: {...}, blob_url: "..."}.
//
// Both blob fields are omitempty so text/code_snippet/link items
// serialize identically to bare *ScratchpadItem.
type scratchpadItemMCPView struct {
	*domain.ScratchpadItem
	BlobURL          string    `json:"blob_url,omitempty"`
	BlobURLExpiresAt time.Time `json:"blob_url_expires_at,omitempty"`
}

// owner_type values match the polymorphic discriminator used in
// code_anchors.owner_type. Kept in sync with internal/api/code_anchors.go's
// constants and internal/storage/sqlite/migrations/0009_use_case_items.sql.
const (
	ownerScratchpadItem = "scratchpad_item"
	ownerTodoItem       = "todo_item"
	ownerBugItem        = "bug_item"
	ownerKnowledgeEntry = "knowledge_entry"
	ownerUseCaseItem    = "use_case_item"
)

func (t *Tools) registerReadItem(srv *server.MCPServer) {
	tool := mcp.NewTool("read_item",
		mcp.WithDescription("Read a single entity by id and type. Polymorphic on owner_type so one tool handles all five entity kinds. Returns the full row or an error if not found. For work items (todo_item, bug_item, use_case_item) the response also includes an `acceptance_criteria` array — the item's measurable definition of done. Build toward those criteria: each is a checkable statement that must be true for the work to be considered complete. State 'proposed' criteria are AI-drafted and awaiting human review; 'accepted' ones are ratified."),
		mcp.WithString("owner_type",
			mcp.Required(),
			mcp.Enum(
				ownerScratchpadItem,
				ownerTodoItem,
				ownerBugItem,
				ownerKnowledgeEntry,
				ownerUseCaseItem,
			),
			mcp.Description("Which table to read from."),
		),
		mcp.WithString("id",
			mcp.Required(),
			mcp.Description("The entity's id."),
		),
		mcp.WithReadOnlyHintAnnotation(true),
	)
	srv.AddTool(tool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		ot := mcp.ParseString(req, "owner_type", "")
		id := mcp.ParseString(req, "id", "")
		if ot == "" || id == "" {
			return mcp.NewToolResultError("owner_type and id are both required"), nil
		}
		switch ot {
		case ownerScratchpadItem:
			it, err := t.Store.GetScratchpadItem(ctx, id)
			if err != nil {
				return mcp.NewToolResultErrorFromErr("get scratchpad_item", err), nil
			}
			return mcp.NewToolResultJSON(t.enrichScratchpadItem(ctx, it))
		case ownerTodoItem:
			it, err := t.Store.GetTodoItem(ctx, id)
			if err != nil {
				return mcp.NewToolResultErrorFromErr("get todo_item", err), nil
			}
			return mcp.NewToolResultJSON(t.attachCriteria(ctx, ownerTodoItem, id, it))
		case ownerBugItem:
			it, err := t.Store.GetBugItem(ctx, id)
			if err != nil {
				return mcp.NewToolResultErrorFromErr("get bug_item", err), nil
			}
			return mcp.NewToolResultJSON(t.attachCriteria(ctx, ownerBugItem, id, it))
		case ownerKnowledgeEntry:
			it, err := t.Store.GetKnowledgeEntry(ctx, id)
			if err != nil {
				return mcp.NewToolResultErrorFromErr("get knowledge_entry", err), nil
			}
			return mcp.NewToolResultJSON(it)
		case ownerUseCaseItem:
			it, err := t.Store.GetUseCaseItem(ctx, id)
			if err != nil {
				return mcp.NewToolResultErrorFromErr("get use_case_item", err), nil
			}
			return mcp.NewToolResultJSON(t.attachCriteria(ctx, ownerUseCaseItem, id, it))
		default:
			return mcp.NewToolResultError(fmt.Sprintf("unknown owner_type %q", ot)), nil
		}
	})
}

// attachCriteria flattens a work item to a map and appends its
// acceptance_criteria, so an agent reading the item also sees the contract it
// should build toward. Falls back to the bare item on any marshal/read error;
// always sets acceptance_criteria (empty array when none).
func (t *Tools) attachCriteria(ctx context.Context, ownerType, ownerID string, item any) any {
	b, err := json.Marshal(item)
	if err != nil {
		return item
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		return item
	}
	crit, err := t.Store.ListAcceptanceCriteria(ctx, ownerType, ownerID)
	if err != nil || crit == nil {
		crit = []*domain.AcceptanceCriterion{}
	}
	m["acceptance_criteria"] = crit
	return m
}

// enrichScratchpadItem wraps a ScratchpadItem with the MCP view that
// adds blob_url + blob_url_expires_at for binary items. Pass-through
// for non-binary items (BlobSHA empty) — the wrapper's two fields
// remain zero and JSON omitempty drops them. Also pass-through when
// the Tools has no BlobStore wired (stdio mode), or when the store
// itself can't produce a URL — the LLM still gets the metadata, just
// no fetch link.
func (t *Tools) enrichScratchpadItem(ctx context.Context, it *domain.ScratchpadItem) *scratchpadItemMCPView {
	view := &scratchpadItemMCPView{ScratchpadItem: it}
	if it.BlobSHA == "" || t.Blobs == nil {
		return view
	}
	url, err := t.Blobs.URL(ctx, it.BlobSHA, t.BlobURLTTL)
	if err != nil {
		// Best-effort: a missing blob (orphan row, GC race) or store
		// hiccup shouldn't fail the whole read. The LLM gets the
		// metadata; the fetch URL is just absent.
		return view
	}
	view.BlobURL = url
	if t.BlobURLTTL > 0 {
		view.BlobURLExpiresAt = time.Now().UTC().Add(t.BlobURLTTL)
	}
	return view
}
