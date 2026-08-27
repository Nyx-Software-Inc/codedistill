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

package sqlite

import (
	"errors"
	"io/fs"
)

// Community Edition stubs for the Postgres backend, which is a paid feature
// (multi-user / Enterprise). The pgx driver and the migrations_pg embed live in
// postgres_full.go and are stripped from the CE, so the CE binary carries no
// Postgres dependency. OpenDSN rejects a postgres:// DSN through OpenPostgres
// here, so pgMigrations() is never reached.

// OpenPostgres always fails in the CE.
func OpenPostgres(_ string) (*Store, error) {
	return nil, errors.New("the Postgres backend is a paid feature; use a SQLite database path")
}

// pgMigrations is never called in the CE (no Postgres store can be opened); it
// exists only so the dialect branch in Migrate compiles.
func pgMigrations() (fs.FS, string) { return nil, "" }
