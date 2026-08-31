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
	"time"

	"codedistill/internal/domain"
	"codedistill/internal/id"
)

// recordEvent appends one lineage event (UC-5) with source="mcp" — every
// write through this surface is attributed to the MCP client, so the
// item's History tab can show "via MCP". Best-effort: a failed history
// write never fails the underlying tool call.
func (t *Tools) recordEvent(ctx context.Context, ownerType, ownerID, kind, summary string) {
	_ = t.Store.RecordItemEvent(ctx, &domain.ItemEvent{
		ID: id.New(), OwnerType: ownerType, OwnerID: ownerID,
		Kind: kind, Summary: summary, Source: "mcp", CreatedAt: time.Now().UTC(),
	})
}

// recordNote appends a markdown `note` entry to an item's activity log with
// source="mcp" — the agent-side counterpart of the UI's "Add note". Running
// notes land here, not in a mutable blob field: the log is append-only and is
// what the Log tab and the canvas pulse read.
func (t *Tools) recordNote(ctx context.Context, ownerType, ownerID, body string) {
	// One implementation shared with internal/api. This path used to omit the
	// ellipsis the API added, so the same note previewed differently depending on
	// whether a human or an agent recorded it (CE-review item 23).
	summary := domain.NoteSummary(body)
	_ = t.Store.RecordItemEvent(ctx, &domain.ItemEvent{
		ID: id.New(), OwnerType: ownerType, OwnerID: ownerID,
		Kind: "note", Summary: summary, Body: body, Source: "mcp", CreatedAt: time.Now().UTC(),
	})
}
