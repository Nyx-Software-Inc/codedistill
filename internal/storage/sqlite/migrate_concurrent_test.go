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

package sqlite

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// Two CodeDistill processes may migrate the same database at the same time, and
// our own architecture manufactures the window: the user upgrades, starts
// `serve`, and their MCP client relaunches `codedistill … mcp` on reconnect.
//
// Migrate used to read `SELECT COUNT(*) FROM schema_migrations WHERE version = ?`
// OUTSIDE the transaction that applied the migration, with nothing serializing
// the gap. SetMaxOpenConns(1) is per-handle, so it gave no cross-process
// protection, and no busy_timeout was configured anywhere — modernc.org/sqlite
// sets no default — so the loser failed instantly instead of waiting.
//
// The loser therefore failed to START, with one of: "table … already exists"
// (29 of 60 migrations use bare CREATE TABLE), a schema_migrations UNIQUE
// violation, or "database is locked". None of them say "another process is
// migrating, retry"; all of them read as corruption, thirty seconds after an
// upgrade (CE-review item 15).
//
// Separate Store handles on one file contend at the SQLite file level, so
// goroutines reproduce cross-process contention faithfully — no subprocess
// harness needed. Run with -count to prove a pass isn't luck.
func TestMigrate_Concurrent(t *testing.T) {
	const racers = 8
	path := filepath.Join(t.TempDir(), "codedistill.db")

	stores := make([]*Store, racers)
	for i := range stores {
		st, err := Open(path)
		if err != nil {
			t.Fatalf("open %d: %v", i, err)
		}
		stores[i] = st
		defer st.Close()
	}

	// Release every racer at once to keep the contention window wide.
	var start sync.WaitGroup
	start.Add(1)
	var done sync.WaitGroup
	errs := make([]error, racers)
	for i, st := range stores {
		done.Add(1)
		go func(i int, st *Store) {
			defer done.Done()
			start.Wait()
			errs[i] = st.Migrate(context.Background())
		}(i, st)
	}
	start.Done()
	done.Wait()

	for i, err := range errs {
		if err != nil {
			t.Errorf("racer %d failed to migrate: %v\n"+
				"A concurrent migration must wait and skip, not fail startup.", i, err)
		}
	}

	// Every migration applied exactly once — the PRIMARY KEY would reject a
	// double-insert, but this also catches a migration silently skipped.
	entries, err := fs.ReadDir(migrationsFS, "migrations")
	if err != nil {
		t.Fatal(err)
	}
	var want int
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".sql") {
			want++
		}
	}
	var applied int
	if err := stores[0].DB.QueryRowContext(context.Background(),
		`SELECT COUNT(*) FROM schema_migrations`).Scan(&applied); err != nil {
		t.Fatal(err)
	}
	if applied != want {
		t.Errorf("schema_migrations has %d rows; want %d (one per migration file)", applied, want)
	}
}

// backupBeforeMigrate does os.Stat(backupPath) then VACUUM INTO backupPath.
// Both processes see no file, both start the vacuum, and VACUUM INTO refuses an
// existing output — so the loser dies with "output file already exists".
//
// This fires BEFORE any DDL and has the widest window of the lot, because
// VACUUM INTO copies the entire database. A snapshot another process already
// wrote is a success, not a failure: the safety net exists either way.
func TestBackupBeforeMigrate_Concurrent(t *testing.T) {
	const racers = 8
	path := filepath.Join(t.TempDir(), "codedistill.db")

	// A migrated database, so appliedCount > 0 and the backup is not skipped as
	// a first-run creation.
	seed, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := seed.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	seed.Close()

	stores := make([]*Store, racers)
	for i := range stores {
		st, err := Open(path)
		if err != nil {
			t.Fatalf("open %d: %v", i, err)
		}
		stores[i] = st
		defer st.Close()
	}

	var start sync.WaitGroup
	start.Add(1)
	var done sync.WaitGroup
	errs := make([]error, racers)
	for i, st := range stores {
		done.Add(1)
		go func(i int, st *Store) {
			defer done.Done()
			start.Wait()
			errs[i] = st.backupBeforeMigrate(context.Background(), "9999_pending.sql")
		}(i, st)
	}
	start.Done()
	done.Wait()

	for i, err := range errs {
		if err != nil {
			t.Errorf("racer %d: %v\nA snapshot another process already wrote is a success.", i, err)
		}
	}
}

// The Postgres counterpart, and the one that matters most: Postgres is the
// multi-user server tier, where two instances starting together (a rolling
// restart, two replicas) is likelier than on a single-user desktop.
//
// Postgres has no BEGIN IMMEDIATE, so applyMigrationPostgres serializes on
// pg_advisory_xact_lock instead. This proves the lock actually serializes under
// contention — TestPostgresEndToEnd only proves the statement is syntactically
// valid and the ?→$1 rebinding works.
//
//	CODEDISTILL_TEST_PG=postgres://postgres:test@localhost:5432/cdtest go test \
//	  ./internal/storage/sqlite -run TestMigrate_ConcurrentPostgres -v
func TestMigrate_ConcurrentPostgres(t *testing.T) {
	dsn := os.Getenv("CODEDISTILL_TEST_PG")
	if dsn == "" {
		t.Skip("set CODEDISTILL_TEST_PG=postgres://… to a fresh DB")
	}
	const racers = 8

	reset, err := OpenPostgres(dsn)
	if err != nil {
		t.Fatalf("open postgres: %v", err)
	}
	if _, err := reset.DB.Raw().Exec("DROP SCHEMA public CASCADE; CREATE SCHEMA public"); err != nil {
		t.Fatalf("reset public schema: %v", err)
	}
	reset.Close()

	stores := make([]*Store, racers)
	for i := range stores {
		st, err := OpenPostgres(dsn)
		if err != nil {
			t.Fatalf("open %d: %v", i, err)
		}
		stores[i] = st
		defer st.Close()
	}

	var start sync.WaitGroup
	start.Add(1)
	var done sync.WaitGroup
	errs := make([]error, racers)
	for i, st := range stores {
		done.Add(1)
		go func(i int, st *Store) {
			defer done.Done()
			start.Wait()
			errs[i] = st.Migrate(context.Background())
		}(i, st)
	}
	start.Done()
	done.Wait()

	for i, err := range errs {
		if err != nil {
			t.Errorf("racer %d failed to migrate: %v", i, err)
		}
	}

	var applied int
	if err := stores[0].DB.QueryRowContext(context.Background(),
		`SELECT COUNT(*) FROM schema_migrations`).Scan(&applied); err != nil {
		t.Fatal(err)
	}
	if applied == 0 {
		t.Error("no migrations recorded")
	}
}
