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

// Package sqlite implements storage.Storage against a local SQLite database
// via the pure-Go modernc.org/sqlite driver (single-binary friendly, no CGo).
package sqlite

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"io/fs"
	"os"
	"sort"
	"strings"
	"time"

	"codedistill/internal/storage/dbx"

	_ "modernc.org/sqlite"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// The Postgres backend (driver + the migrations_pg embed) is a paid feature and
// lives in postgres_full.go behind //go:build !oss; pgMigrations() is the seam
// the runner uses to reach its migration set, stubbed empty in the CE.

// Store is a SQLite-backed implementation of storage.Storage. DB is the
// dialect-aware wrapper (dbx) so the same query methods can later run against
// Postgres; for SQLite the wrapper's placeholder rebind is a no-op.
type Store struct {
	DB *dbx.DB
	// path is the SQLite file path (from Open's dsn). Empty or ":memory:" means
	// not file-backed, so the pre-migration backup is skipped. Postgres stores
	// leave this empty.
	path string
}

// Open opens (or creates) the SQLite database at dsn.
// dsn is a filesystem path; use ":memory:" for an in-memory db in tests.
func Open(dsn string) (_ *Store, err error) {
	db, oerr := sql.Open("sqlite", connString(dsn))
	if oerr != nil {
		return nil, fmt.Errorf("sqlite open: %w", oerr)
	}
	// Any failure after this point must not leak the handle + its connection
	// (audit M24). The named return lets one defer cover every error path.
	defer func() {
		if err != nil {
			_ = db.Close()
		}
	}()
	// SQLite is single-writer; holding the pool to one connection avoids
	// spurious contention and (critically) ensures in-memory databases
	// share state across "connections" in tests.
	db.SetMaxOpenConns(1)
	if err = db.Ping(); err != nil {
		return nil, fmt.Errorf("sqlite ping: %w", err)
	}
	if _, err = db.Exec(`PRAGMA foreign_keys = ON`); err != nil {
		return nil, fmt.Errorf("enable foreign keys: %w", err)
	}
	// WAL mode lets a second process (the upcoming `codedistill mcp` stdio
	// subcommand) read/write the same DB while `codedistill serve` is
	// running, without "database is locked" errors. WAL is per-file metadata
	// and persists once set; no harm setting it on every open. SQLite
	// silently ignores WAL on :memory: databases, so tests are unaffected.
	// cmdReset already cleans the -wal/-shm sidecars.
	if _, err = db.Exec(`PRAGMA journal_mode = WAL`); err != nil {
		return nil, fmt.Errorf("enable wal mode: %w", err)
	}
	return &Store{DB: dbx.New(db, dbx.SQLite), path: dsn}, nil
}

// busyTimeout is how long a connection waits for SQLite's write lock before
// giving up with SQLITE_BUSY. Nothing configured one before, and
// modernc.org/sqlite supplies no default, so a process that lost a race failed
// INSTANTLY rather than waiting — which is what turned a routine upgrade
// collision into a hard startup failure (CE-review item 15). Ten seconds
// comfortably covers applying one migration; the pre-migration VACUUM INTO of a
// large database is deliberately kept outside any lock hold so it can't eat this
// budget (see backupBeforeMigrate).
const busyTimeout = 10 * time.Second

// connString returns the driver DSN for a database path: the path itself plus
// the pragmas every connection needs.
//
// This is deliberately separate from Store.path, which must stay a plain
// FILESYSTEM path — backupBeforeMigrate and Backup derive snapshot filenames
// from it, so query parameters leaking in there would produce paths like
// "codedistill.db?_pragma=...premigrate-0061.bak".
//
// The driver splits a non-"file:" DSN at the first '?', opens the left side as
// the filename, and applies _pragma from the right (modernc.org/sqlite
// conn.go:43-79), so this works on a bare path and on ":memory:" alike.
func connString(dsn string) string {
	sep := "?"
	if strings.Contains(dsn, "?") {
		sep = "&"
	}
	return fmt.Sprintf("%s%s_pragma=busy_timeout(%d)", dsn, sep, busyTimeout.Milliseconds())
}

// OpenDSN dispatches by scheme: a postgres:// or postgresql:// URL opens the
// Postgres backend; anything else (a file path, ":memory:", "sqlite:…") opens
// SQLite. This is the single entry point callers use to pick a backend.
func OpenDSN(dsn string) (*Store, error) {
	if strings.HasPrefix(dsn, "postgres://") || strings.HasPrefix(dsn, "postgresql://") {
		return OpenPostgres(dsn)
	}
	return Open(strings.TrimPrefix(dsn, "sqlite:"))
}

func (s *Store) Close() error { return s.DB.Close() }

// Migrate applies every migration in migrations/*.sql that has not yet been
// recorded in schema_migrations. Migrations must be lexicographically ordered
// by filename (e.g., 0001_initial.sql, 0002_add_tags.sql).
func (s *Store) Migrate(ctx context.Context) error {
	var fsys fs.FS = migrationsFS
	dir := "migrations"
	if s.DB.Dialect() == dbx.Postgres {
		fsys, dir = pgMigrations()
	}

	if err := s.ensureMigrationsTable(ctx); err != nil {
		return err
	}

	entries, err := fs.ReadDir(fsys, dir)
	if err != nil {
		return fmt.Errorf("read migrations dir: %w", err)
	}

	var names []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".sql") {
			continue
		}
		names = append(names, e.Name())
	}
	sort.Strings(names)

	// Data-loss safety net: before applying ANY pending migration, snapshot the
	// database. Upgrades run migrations on real user data (single-user desktop),
	// so a consistent restore point must exist first. Refuse to migrate if the
	// snapshot can't be written — better to not start than to migrate without a
	// safety net.
	if pending, err := s.pendingMigrations(ctx, names); err != nil {
		return err
	} else if len(pending) > 0 {
		if err := s.backupBeforeMigrate(ctx, pending[len(pending)-1]); err != nil {
			return fmt.Errorf("pre-migration backup: %w", err)
		}
	}

	for _, name := range names {
		// Cheap unlocked pre-check. On an already-migrated database this is the
		// only thing that runs, so the steady-state path never pays for a write
		// lock. It is NOT sufficient on its own — applyMigration re-checks under
		// the lock, which is what makes the decision safe.
		var count int
		if err := s.DB.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM schema_migrations WHERE version = ?`, name,
		).Scan(&count); err != nil {
			return fmt.Errorf("check %s: %w", name, err)
		}
		if count > 0 {
			continue
		}

		data, err := fs.ReadFile(fsys, dir+"/"+name)
		if err != nil {
			return fmt.Errorf("read %s: %w", name, err)
		}

		if err := s.applyMigration(ctx, name, string(data)); err != nil {
			return err
		}
	}
	return nil
}

// ensureMigrationsTable creates schema_migrations if absent, under the same lock
// the migrations themselves take.
//
// CREATE TABLE IF NOT EXISTS looks like it needs no protection. It does: on
// Postgres it is explicitly NOT concurrency-safe — two sessions running it
// together race inserting the type row and the loser gets
// "duplicate key value violates unique constraint pg_type_typname_nsp_index".
// That fires at the very top of Migrate, before any per-migration locking, so
// serializing the migrations alone left 7 of 8 concurrent starts still failing.
// Only testing against a real Postgres surfaced this; SQLite serializes the same
// statement happily once busy_timeout is set (CE-review item 15).
func (s *Store) ensureMigrationsTable(ctx context.Context) error {
	const ddl = `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version    TEXT PRIMARY KEY,
			applied_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
		)
	`
	if s.DB.Dialect() == dbx.Postgres {
		tx, err := s.DB.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		defer func() { _ = tx.Rollback() }()
		if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(?)`, migrationLockKey); err != nil {
			return fmt.Errorf("lock for schema_migrations: %w", err)
		}
		if _, err := tx.ExecContext(ctx, ddl); err != nil {
			return fmt.Errorf("create schema_migrations: %w", err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("create schema_migrations: %w", err)
		}
		return nil
	}

	conn, err := s.DB.Raw().Conn(ctx)
	if err != nil {
		return fmt.Errorf("acquire connection for schema_migrations: %w", err)
	}
	defer conn.Close()
	if _, err := conn.ExecContext(ctx, `BEGIN IMMEDIATE`); err != nil {
		return fmt.Errorf("begin schema_migrations: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_, _ = conn.ExecContext(context.WithoutCancel(ctx), `ROLLBACK`)
		}
	}()
	if _, err := conn.ExecContext(ctx, ddl); err != nil {
		return fmt.Errorf("create schema_migrations: %w", err)
	}
	if _, err := conn.ExecContext(ctx, `COMMIT`); err != nil {
		return fmt.Errorf("create schema_migrations: %w", err)
	}
	committed = true
	return nil
}

// applyMigration applies one migration under a write lock taken BEFORE the
// "has it been applied?" question is asked.
//
// The old code read schema_migrations outside the transaction that applied the
// migration, so two processes upgrading the same database could both read
// "not applied" and both proceed. The loser then hit a bare CREATE TABLE
// ("table … already exists"), a schema_migrations UNIQUE violation, or
// SQLITE_BUSY — none of which say "another process is migrating", and all of
// which read as corruption to a user who upgraded a minute ago.
//
// The re-check INSIDE the lock is the fix: the loser blocks, wakes once the
// winner commits, sees the migration recorded, and skips it.
func (s *Store) applyMigration(ctx context.Context, name, script string) error {
	if s.DB.Dialect() == dbx.Postgres {
		return s.applyMigrationPostgres(ctx, name, script)
	}
	return s.applyMigrationSQLite(ctx, name, script)
}

// migrationLockKey namespaces the Postgres advisory lock. Arbitrary but stable —
// every CodeDistill process must pick the same number for the lock to serialize
// anything.
const migrationLockKey int64 = 0x0C0DED15

func (s *Store) applyMigrationSQLite(ctx context.Context, name, script string) error {
	// A dedicated connection: BEGIN IMMEDIATE / COMMIT are connection state, and
	// database/sql gives no guarantee that two Exec calls land on the same
	// connection.
	conn, err := s.DB.Raw().Conn(ctx)
	if err != nil {
		return fmt.Errorf("acquire connection for %s: %w", name, err)
	}
	defer conn.Close()

	// IMMEDIATE takes the write lock up front instead of deferring it to the
	// first write, which is precisely the gap this bug lived in. A concurrent
	// migrator blocks here for busyTimeout rather than failing instantly.
	if _, err := conn.ExecContext(ctx, `BEGIN IMMEDIATE`); err != nil {
		return fmt.Errorf("begin %s: %w", name, err)
	}
	committed := false
	defer func() {
		if !committed {
			// Best effort, and deliberately not ctx-bound: a cancelled context
			// must still release the write lock.
			_, _ = conn.ExecContext(context.WithoutCancel(ctx), `ROLLBACK`)
		}
	}()

	var count int
	if err := conn.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM schema_migrations WHERE version = ?`, name).Scan(&count); err != nil {
		return fmt.Errorf("re-check %s: %w", name, err)
	}
	if count > 0 {
		return nil // another process applied it while we waited for the lock
	}

	if _, err := conn.ExecContext(ctx, script); err != nil {
		return fmt.Errorf("apply %s: %w", name, err)
	}
	if _, err := conn.ExecContext(ctx,
		`INSERT INTO schema_migrations (version) VALUES (?)`, name); err != nil {
		return fmt.Errorf("record %s: %w", name, err)
	}
	if _, err := conn.ExecContext(ctx, `COMMIT`); err != nil {
		return fmt.Errorf("commit %s: %w", name, err)
	}
	committed = true
	return nil
}

func (s *Store) applyMigrationPostgres(ctx context.Context, name, script string) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }() // no-op once committed

	// Postgres has no BEGIN IMMEDIATE; a transaction-scoped advisory lock is the
	// equivalent, and releases automatically on commit or rollback. This matters
	// more here than on SQLite: Postgres is the multi-user server tier, where
	// two instances starting at once is likelier, not rarer.
	if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(?)`, migrationLockKey); err != nil {
		return fmt.Errorf("lock for %s: %w", name, err)
	}

	var count int
	if err := tx.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM schema_migrations WHERE version = ?`, name).Scan(&count); err != nil {
		return fmt.Errorf("re-check %s: %w", name, err)
	}
	if count > 0 {
		return nil
	}

	if _, err := tx.ExecContext(ctx, script); err != nil {
		return fmt.Errorf("apply %s: %w", name, err)
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO schema_migrations (version) VALUES (?)`, name); err != nil {
		return fmt.Errorf("record %s: %w", name, err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit %s: %w", name, err)
	}
	return nil
}

// Backup writes a consistent snapshot of the database to dest via VACUUM INTO
// (WAL-safe; works while `serve` is running since it only reads committed
// state). dest must not already exist. SQLite only — Postgres backups are
// pg_dump's job. Powers the `codedistill backup` command.
func (s *Store) Backup(ctx context.Context, dest string) error {
	if s.DB.Dialect() != dbx.SQLite {
		return fmt.Errorf("backup is only supported for the SQLite backend (use pg_dump for Postgres)")
	}
	if _, err := os.Stat(dest); err == nil {
		return fmt.Errorf("refusing to overwrite existing file %s", dest)
	}
	if _, err := s.DB.ExecContext(ctx, `VACUUM INTO ?`, dest); err != nil {
		return fmt.Errorf("vacuum into %s: %w", dest, err)
	}
	return nil
}

// pendingMigrations returns, in order, the migration names not yet recorded in
// schema_migrations.
func (s *Store) pendingMigrations(ctx context.Context, names []string) ([]string, error) {
	applied := map[string]bool{}
	rows, err := s.DB.QueryContext(ctx, `SELECT version FROM schema_migrations`)
	if err != nil {
		return nil, fmt.Errorf("load applied migrations: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err != nil {
			return nil, err
		}
		applied[v] = true
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	var pending []string
	for _, n := range names {
		if !applied[n] {
			pending = append(pending, n)
		}
	}
	return pending, nil
}

// backupBeforeMigrate writes a consistent snapshot of the database next to it
// before migrations run, named for the migration being upgraded TO. No-op for
// in-memory / Postgres stores and for an already-existing snapshot (so re-runs
// don't clobber it). Uses VACUUM INTO, which produces a clean copy that
// includes any WAL contents — a raw file copy could miss un-checkpointed pages.
func (s *Store) backupBeforeMigrate(ctx context.Context, targetVersion string) error {
	if s.DB.Dialect() != dbx.SQLite {
		return nil // Postgres backups are the operator's job (pg_dump)
	}
	if s.path == "" || strings.Contains(s.path, ":memory:") {
		return nil // not file-backed
	}
	// Skip a brand-new database (first-run creation) — there's nothing to
	// protect, and an empty snapshot is just clutter. Only genuine upgrades
	// (an existing DB with applied migrations + something new) get a backup.
	var appliedCount int
	if err := s.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM schema_migrations`).Scan(&appliedCount); err != nil {
		return fmt.Errorf("count applied migrations: %w", err)
	}
	if appliedCount == 0 {
		return nil
	}
	backupPath := s.path + ".premigrate-" + strings.TrimSuffix(targetVersion, ".sql") + ".bak"
	if _, err := os.Stat(backupPath); err == nil {
		return nil // snapshot already exists (a prior attempt) — keep it
	}
	if _, err := s.DB.ExecContext(ctx, `VACUUM INTO ?`, backupPath); err != nil {
		// Two processes upgrading together both Stat, both see nothing, and both
		// vacuum. VACUUM INTO refuses an existing output file, and the copy is
		// slow (it duplicates the whole database), so this had the widest window
		// of any part of the race — and it fired BEFORE any DDL.
		//
		// A snapshot another process already wrote is a success: the safety net
		// this function exists to provide is in place either way. That is the
		// same judgement the Stat check above already makes; this just also
		// applies it when the winner finished mid-vacuum instead of before it.
		if _, statErr := os.Stat(backupPath); statErr == nil {
			return nil
		}
		return fmt.Errorf("vacuum into %s: %w", backupPath, err)
	}
	return nil
}
