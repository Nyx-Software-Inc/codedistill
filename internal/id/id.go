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

// Package id produces short random identifiers for domain entities.
// Used by the agent, the REST API, and the CLI seed path so every layer
// shares one generator.
package id

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"
)

// New returns a 16-hex-char random ID. Falls back to a nanosecond timestamp
// if the system RNG is unavailable (extremely unlikely in practice).
func New() string {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("ts%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b)
}
