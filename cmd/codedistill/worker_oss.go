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
	"errors"

	mcppkg "codedistill/internal/mcp"
)

// Workers run agents for the Conductor, which the community edition does not
// include.
func cmdWorker([]string) error {
	return errors.New("workers are part of CodeDistill Pro; the community edition has no Conductor to run them")
}

func jobScopeFromEnv() mcppkg.JobScope { return mcppkg.JobScope{} }
