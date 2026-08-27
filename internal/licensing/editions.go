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

package licensing

// EditionFeatures maps edition → default feature grant. Single source shared
// by licensegen (hand issuance) and the activation service (self-serve
// issuance) so the two lanes can never drift. Must stay in sync with
// internal/features.
//
// Baseline code scan is FREE (no flag). "analysis" gates the Enterprise
// analysis PIPELINE (SARIF ingest + auto-route); "governance" gates the
// enforcement gates (incl. block-on-security-high). Both Enterprise-only.
var EditionFeatures = map[string][]string{
	"pro":        {"mcp", "anchors", "dedup", "canvas_views"},
	"enterprise": {"mcp", "anchors", "multiuser", "s3", "dedup", "canvas_views", "analysis", "governance"},
}
