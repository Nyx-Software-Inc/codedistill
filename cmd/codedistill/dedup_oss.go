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

//go:build oss

package main

import (
	"log/slog"

	"codedistill/internal/agent"
	"codedistill/internal/storage"
)

// newDuplicateGrouper is the community-edition stub: the paid
// duplicate-intelligence package (internal/dedup) is not part of the
// open-source build, so there is nothing to wire — duplicate
// detection, auto-grouping, and the on-demand scan simply don't exist.
func newDuplicateGrouper(_ storage.Storage, _ *slog.Logger) agent.DuplicateGrouper {
	return nil
}
