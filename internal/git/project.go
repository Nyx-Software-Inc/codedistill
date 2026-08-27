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

package git

import (
	"context"
	"errors"
	"fmt"

	"codedistill/internal/storage"
)

// HeadSHAForProject reads HEAD on the project's primary codebase (today:
// the legacy projects.repo_root, which still backs single-codebase
// projects). Returns the full SHA, or "" + a descriptive error when the
// project has no repo configured. Shared between the api/ and mcp/
// packages so both auto-fill flows behave identically.
//
// Callers treat ("", err) as "user can set the SHA later" — non-fatal.
func HeadSHAForProject(ctx context.Context, store storage.Storage, projectID string) (string, error) {
	p, err := store.GetProject(ctx, projectID)
	if err != nil {
		return "", fmt.Errorf("get project: %w", err)
	}
	if p.RepoRoot == "" {
		return "", errors.New("no repo_root configured")
	}
	repo, err := Open(p.RepoRoot)
	if err != nil {
		return "", err
	}
	return repo.HeadSHA()
}
