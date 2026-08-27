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
	"os"
	"path/filepath"
	"testing"
)

// The pre-migration snapshot is the data-loss safety net for upgrades: before
// any pending migration runs on a real (already-migrated) database, VACUUM INTO
// writes a consistent copy next to it.
func TestBackupBeforeMigrate(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	path := filepath.Join(dir, "codedistill.db")

	st, err := Open(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	// First run: applies all migrations. A fresh DB must NOT be backed up
	// (nothing to protect) — no .premigrate file should appear.
	if err := st.Migrate(ctx); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if hits, _ := filepath.Glob(path + ".premigrate-*.bak"); len(hits) != 0 {
		t.Fatalf("fresh DB should not be backed up, found: %v", hits)
	}

	// Simulate an upgrade: the DB now has applied migrations, so backing up
	// before a (hypothetical) new migration must produce a snapshot.
	if err := st.backupBeforeMigrate(ctx, "0099_future.sql"); err != nil {
		t.Fatalf("backup: %v", err)
	}
	backup := path + ".premigrate-0099_future.bak"
	fi, err := os.Stat(backup)
	if err != nil {
		t.Fatalf("expected snapshot at %s: %v", backup, err)
	}
	if fi.Size() == 0 {
		t.Fatalf("snapshot is empty")
	}

	// The snapshot must be a valid, openable SQLite DB carrying the schema.
	snap, err := Open(backup)
	if err != nil {
		t.Fatalf("open snapshot: %v", err)
	}
	var applied int
	if err := snap.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM schema_migrations`).Scan(&applied); err != nil {
		t.Fatalf("query snapshot: %v", err)
	}
	if applied == 0 {
		t.Fatalf("snapshot has no applied migrations recorded")
	}
	snap.Close() // closing a WAL db mutates the file — do it before the idempotency check

	// Idempotent: a second call with the same target must not error or clobber.
	fiBefore, _ := os.Stat(backup)
	if err := st.backupBeforeMigrate(ctx, "0099_future.sql"); err != nil {
		t.Fatalf("second backup: %v", err)
	}
	fiAfter, _ := os.Stat(backup)
	if !fiAfter.ModTime().Equal(fiBefore.ModTime()) {
		t.Fatalf("existing snapshot was clobbered")
	}

	// In-memory stores are never backed up.
	mem, err := Open(":memory:")
	if err != nil {
		t.Fatalf("open mem: %v", err)
	}
	defer mem.Close()
	if err := mem.Migrate(ctx); err != nil {
		t.Fatalf("mem migrate: %v", err)
	}
	if err := mem.backupBeforeMigrate(ctx, "0099_future.sql"); err != nil {
		t.Fatalf("mem backup should be a no-op, got: %v", err)
	}
}
