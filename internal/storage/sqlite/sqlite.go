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
	db, oerr := sql.Open("sqlite", dsn)
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

	if _, err := s.DB.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version    TEXT PRIMARY KEY,
			applied_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
		)
	`); err != nil {
		return fmt.Errorf("create schema_migrations: %w", err)
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

		tx, err := s.DB.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, string(data)); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("apply %s: %w", name, err)
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO schema_migrations (version) VALUES (?)`, name,
		); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("record %s: %w", name, err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit %s: %w", name, err)
		}
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
		return fmt.Errorf("vacuum into %s: %w", backupPath, err)
	}
	return nil
}
