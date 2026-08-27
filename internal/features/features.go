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

// Package features is the single answer to "is this paid capability
// on?". The boundary (per the commercialization design, 2026-06-04):
//
//	free  — scratchpad/canvas core, AI classification, embeddings,
//	        search/Q&A, export/import, local blob store
//	paid  — MCP server (stdio + HTTP), code anchors (create/compute),
//	        implementation matcher + code indexer, multi-user
//	        workspaces, S3 blob backend
//
// Two layers decide:
//
//  1. Build baseline — the `oss` build tag (baseline_oss.go) pins paid
//     features hard-off; the default build (baseline_full.go) leaves
//     them license-controlled.
//  2. License — Init() installs the verified licensing.Status; a paid
//     feature is on iff the baseline allows it AND the license grants
//     it (degrade-to-free: no/expired license simply means off).
//
// Reads of existing data are NEVER gated — only creation and compute.
// Gate the scan button, not the list of past scans.
package features

import (
	"sync/atomic"

	"codedistill/internal/licensing"
)

// Feature names double as the strings carried in license payloads —
// keep in sync with licensegen's editionDefaults.
type Feature string

const (
	MCPServer   Feature = "mcp"          // MCP stdio command + /mcp HTTP transport
	CodeAnchors Feature = "anchors"      // anchor creation + code indexer watcher
	MultiUser   Feature = "multiuser"    // workspaces beyond the implicit "local"
	S3Blob      Feature = "s3"           // S3-compatible blob backend
	Dedup       Feature = "dedup"        // duplicate intelligence: banner + auto-group + on-demand scan
	CanvasViews Feature = "canvas_views" // alternative scratchpad views: List + Calendar
	Governance  Feature = "governance"   // glass-box enforcement gates incl. block-on-security-high (Enterprise)
	Analysis    Feature = "analysis"     // Enterprise analysis PIPELINE (SARIF ingest + auto-route); baseline scan FREE
)

// All paid features, for status reporting.
var All = []Feature{MCPServer, CodeAnchors, MultiUser, S3Blob, Dedup, CanvasViews, Governance, Analysis}

// status holds the installed license. Atomic so HTTP handlers can read
// while a future hot-reload path swaps it.
var status atomic.Pointer[licensing.Status]

func init() { status.Store(licensing.None("not initialized")) }

// Init installs the verified license status. Call once at startup
// (and again if a license is installed at runtime).
func Init(st *licensing.Status) {
	if st == nil {
		st = licensing.None("nil status")
	}
	status.Store(st)
}

// LicenseStatus returns the currently installed license status.
func LicenseStatus() *licensing.Status { return status.Load() }

// Enabled reports whether a paid feature is currently usable.
func Enabled(f Feature) bool {
	if !baselineAllows(f) {
		return false
	}
	return status.Load().HasFeature(string(f))
}

// EnabledNames returns the currently-on paid features (for the
// /api/v1/license endpoint and startup log line).
func EnabledNames() []string {
	out := []string{}
	for _, f := range All {
		if Enabled(f) {
			out = append(out, string(f))
		}
	}
	return out
}
