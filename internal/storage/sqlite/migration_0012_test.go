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
	"sort"
	"strings"
	"testing"
	"time"
)

// applyMigrationsThrough runs every migration whose filename sorts <= upTo,
// recording each in schema_migrations. Used by the 0012 collision test to
// reach a "pre-uniqueness" state where duplicate names can be inserted
// without the new index blocking them.
func applyMigrationsThrough(t *testing.T, s *Store, upTo string) {
	t.Helper()
	ctx := context.Background()

	if _, err := s.DB.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version    TEXT PRIMARY KEY,
			applied_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
		)
	`); err != nil {
		t.Fatalf("schema_migrations: %v", err)
	}

	entries, err := migrationsFS.ReadDir("migrations")
	if err != nil {
		t.Fatalf("readdir: %v", err)
	}
	var names []string
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".sql") {
			continue
		}
		if e.Name() <= upTo {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)

	for _, name := range names {
		data, err := migrationsFS.ReadFile("migrations/" + name)
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		tx, err := s.DB.BeginTx(ctx, nil)
		if err != nil {
			t.Fatalf("begin %s: %v", name, err)
		}
		if _, err := tx.ExecContext(ctx, string(data)); err != nil {
			_ = tx.Rollback()
			t.Fatalf("apply %s: %v", name, err)
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO schema_migrations (version) VALUES (?)`, name,
		); err != nil {
			_ = tx.Rollback()
			t.Fatalf("record %s: %v", name, err)
		}
		if err := tx.Commit(); err != nil {
			t.Fatalf("commit %s: %v", name, err)
		}
	}
}

// applyMigrationByName runs one migration file standalone and records it.
func applyMigrationByName(t *testing.T, s *Store, name string) {
	t.Helper()
	ctx := context.Background()
	data, err := migrationsFS.ReadFile("migrations/" + name)
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin %s: %v", name, err)
	}
	if _, err := tx.ExecContext(ctx, string(data)); err != nil {
		_ = tx.Rollback()
		t.Fatalf("apply %s: %v", name, err)
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO schema_migrations (version) VALUES (?)`, name,
	); err != nil {
		_ = tx.Rollback()
		t.Fatalf("record %s: %v", name, err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit %s: %v", name, err)
	}
}

// TestMigration0012AutoRenamesCollidingNames verifies the backfill: when
// two or more rows share (parent_id, name) before the migration, the
// oldest by (created_at, id) keeps its name and the rest get -2, -3, …
// suffixes.
func TestMigration0012AutoRenamesCollidingNames(t *testing.T) {
	s, err := Open(":memory:")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { s.Close() })

	applyMigrationsThrough(t, s, "0011_per_project_numbering_and_use_case_extraction.sql")

	ctx := context.Background()
	t0 := time.Date(2026, 5, 7, 12, 0, 0, 0, time.UTC)

	// Seed three scratchpads in the same project sharing the name "main".
	// Project + workspace + user already exist via migration 0006's seed.
	if _, err := s.DB.ExecContext(ctx,
		`INSERT INTO projects (id, workspace_id, name, created_at)
		 VALUES ('p1', 'local', 'Test', ?)`, t0,
	); err != nil {
		t.Fatalf("seed project: %v", err)
	}
	for i, sid := range []string{"sp-a", "sp-b", "sp-c"} {
		if _, err := s.DB.ExecContext(ctx,
			`INSERT INTO scratchpads (id, project_id, name, classification_mode, created_at)
			 VALUES (?, 'p1', 'main', 'full', ?)`,
			sid, t0.Add(time.Duration(i)*time.Second),
		); err != nil {
			t.Fatalf("seed scratchpad %s: %v", sid, err)
		}
	}

	// Also seed a project-name collision in the same workspace.
	if _, err := s.DB.ExecContext(ctx,
		`INSERT INTO projects (id, workspace_id, name, created_at)
		 VALUES ('p2', 'local', 'Test', ?)`, t0.Add(time.Second),
	); err != nil {
		t.Fatalf("seed project p2: %v", err)
	}

	// Apply 0012 — the migration under test.
	applyMigrationByName(t, s, "0012_unique_names.sql")

	// Oldest scratchpad keeps "main"; later siblings get -2, -3.
	cases := []struct {
		id   string
		want string
	}{
		{"sp-a", "main"},
		{"sp-b", "main-2"},
		{"sp-c", "main-3"},
	}
	for _, c := range cases {
		var got string
		if err := s.DB.QueryRowContext(ctx,
			`SELECT name FROM scratchpads WHERE id = ?`, c.id,
		).Scan(&got); err != nil {
			t.Fatalf("scan %s: %v", c.id, err)
		}
		if got != c.want {
			t.Errorf("%s: name = %q, want %q", c.id, got, c.want)
		}
	}

	// Project collision: oldest keeps "Test", later gets "Test-2".
	var p1Name, p2Name string
	_ = s.DB.QueryRowContext(ctx, `SELECT name FROM projects WHERE id = 'p1'`).Scan(&p1Name)
	_ = s.DB.QueryRowContext(ctx, `SELECT name FROM projects WHERE id = 'p2'`).Scan(&p2Name)
	if p1Name != "Test" {
		t.Errorf("p1 name = %q, want Test", p1Name)
	}
	if p2Name != "Test-2" {
		t.Errorf("p2 name = %q, want Test-2", p2Name)
	}

	// Post-migration: unique index now refuses a fourth "main" under p1.
	_, err = s.DB.ExecContext(ctx,
		`INSERT INTO scratchpads (id, project_id, name, classification_mode, created_at)
		 VALUES ('sp-d', 'p1', 'main', 'full', ?)`,
		t0.Add(time.Hour),
	)
	if err == nil {
		t.Errorf("expected UNIQUE constraint violation inserting fourth 'main' scratchpad")
	}
}

// TestMigration0012EnforcesUniquenessOnFreshDB verifies the index is in
// place even on a clean DB (no collisions to backfill).
func TestMigration0012EnforcesUniquenessOnFreshDB(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	if _, err := s.DB.ExecContext(ctx,
		`INSERT INTO projects (id, workspace_id, name, created_at)
		 VALUES ('p1', 'local', 'Solo', ?)`, time.Now().UTC(),
	); err != nil {
		t.Fatalf("seed: %v", err)
	}
	_, err := s.DB.ExecContext(ctx,
		`INSERT INTO projects (id, workspace_id, name, created_at)
		 VALUES ('p2', 'local', 'Solo', ?)`, time.Now().UTC(),
	)
	if err == nil {
		t.Errorf("expected UNIQUE violation on duplicate (workspace_id, name) insert")
	}
}
