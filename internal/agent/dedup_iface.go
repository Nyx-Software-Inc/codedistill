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

package agent

import "context"

// DuplicateGrouper is the seam for the paid "duplicate intelligence"
// capability (similarity banner + auto-grouping + on-demand cluster +
// cross-pad detection). The INTERFACE lives in core so it survives the
// public-mirror strip; the IMPLEMENTATION lives in internal/dedup,
// which is removed from the AGPL community edition entirely (UC-3
// stage-2 pattern). The free build wires nil here → no duplicate
// detection of any kind. The commercial build wires the real impl,
// license-gated on features.Dedup.
//
// All methods are no-op-safe to skip when the grouper is nil; callers
// guard with `if a.grouper != nil`.
type DuplicateGrouper interface {
	// OnClassified runs the incremental pass after a freshly-classified
	// item is embedded: flag the nearest near-duplicate (the SPA banner)
	// and, when one exists in the same scratchpad, auto-group the pair.
	// vec is the item's embedding (already computed by the agent).
	// Failures are swallowed — never fail classification over grouping.
	OnClassified(ctx context.Context, projectID, itemID string, vec []float32)

	// Scan runs the on-demand pass: cluster every near-duplicate group
	// WITHIN each scratchpad in scope, and (for multi-pad scopes) report
	// cross-pad near-duplicates that can't be grouped because they live
	// on different canvases.
	Scan(ctx context.Context, req DedupScanRequest) (*DedupScanResult, error)

	// GroupPair groups an item with a chosen partner (the user accepting a
	// "possibly related" banner): create-or-join the group + place the item,
	// then clear the item's banner. Same-pad only.
	GroupPair(ctx context.Context, itemID, partnerID string) error
}

// Dedup scope values for DedupScanRequest.Scope.
const (
	DedupScopeScratchpad = "scratchpad" // one pad (ScopeID = scratchpad id)
	DedupScopeProject    = "project"    // every pad in a project (ScopeID = project id)
	DedupScopeGlobal     = "global"     // every pad everywhere (ScopeID ignored)
)

// DedupScanRequest selects what the on-demand scan covers.
type DedupScanRequest struct {
	Scope   string `json:"scope"`
	ScopeID string `json:"scope_id"`
}

// DedupScanResult summarizes an on-demand scan. ItemsFlagged +
// PadsScanned describe the within-pad pass (near-duplicates FLAGGED with a
// "possibly related" banner — never grouped, since grouping is user-initiated);
// CrossPad lists inter-pad near-duplicates (report only — never grouped or moved).
type DedupScanResult struct {
	ItemsFlagged int             `json:"items_flagged"`
	PadsScanned  int             `json:"pads_scanned"`
	CrossPad     []CrossPadMatch `json:"cross_pad"`
	// Truncated is true when the cross-pad pass hit its safety cap and
	// stopped enumerating further pairs.
	Truncated bool `json:"truncated"`
}

// CrossPadMatch is one near-duplicate pair whose two items live on
// DIFFERENT scratchpads. Surfaced as information; the user decides
// whether to act (we never move items across pads as a side effect).
type CrossPadMatch struct {
	AID    string  `json:"a_id"`
	ATitle string  `json:"a_title"`
	APad   string  `json:"a_pad"`
	BID    string  `json:"b_id"`
	BTitle string  `json:"b_title"`
	BPad   string  `json:"b_pad"`
	Score  float64 `json:"score"`
}
