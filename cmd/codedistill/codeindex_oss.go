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
	"context"
	"fmt"
	"log/slog"

	"codedistill/internal/events"
	"codedistill/internal/ollama"
	"codedistill/internal/storage"
	"codedistill/internal/throttle"
)

// Community-edition stubs: the code-chunk indexer (internal/codeindex) is
// a paid feature stripped from the open-source build. The serve watcher is
// a no-op and the index-code command refuses.
func startCodeIndexWatcher(_ context.Context, _ storage.Storage, _ *ollama.Client, _ throttle.Reader, _ *events.Bus, _ *slog.Logger) {
}

func cmdIndexCode(_, _ string) error {
	return fmt.Errorf("index-code is a paid feature, not available in the community edition")
}
