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

package datamodel

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func parse1(sql string) *Model { return Parse([]Source{{Path: "t.sql", SQL: sql}}) }

func tbl(m *Model, name string) *Table {
	for i := range m.Tables {
		if strings.EqualFold(m.Tables[i].Name, name) {
			return &m.Tables[i]
		}
	}
	return nil
}

func col(t *Table, name string) *Column {
	if t == nil {
		return nil
	}
	for i := range t.Columns {
		if strings.EqualFold(t.Columns[i].Name, name) {
			return &t.Columns[i]
		}
	}
	return nil
}

func hasRel(m *Model, from, fromCol, to string, inferred bool) bool {
	for _, r := range m.Relations {
		if strings.EqualFold(r.FromTable, from) && strings.EqualFold(r.FromCol, fromCol) &&
			strings.EqualFold(r.ToTable, to) && r.Inferred == inferred {
			return true
		}
	}
	return false
}

func TestColumnsAndDeclaredFK(t *testing.T) {
	m := parse1(`
CREATE TABLE users (
  id TEXT PRIMARY KEY,
  email TEXT NOT NULL UNIQUE,
  age INTEGER,
  org_id TEXT NOT NULL REFERENCES orgs(id)
);
CREATE TABLE orgs (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL
);`)

	if len(m.Tables) != 2 {
		t.Fatalf("want 2 tables, got %d: %+v", len(m.Tables), m.Tables)
	}
	u := tbl(m, "users")
	if u == nil || len(u.Columns) != 4 {
		t.Fatalf("users columns wrong: %+v", u)
	}
	if c := col(u, "id"); c == nil || !c.PK || c.Nullable {
		t.Errorf("id should be PK+not-null, got %+v", c)
	}
	if c := col(u, "email"); c == nil || c.Nullable {
		t.Errorf("email should be not-null, got %+v", c)
	}
	if c := col(u, "age"); c == nil || !c.Nullable {
		t.Errorf("age should be nullable, got %+v", c)
	}
	if c := col(u, "org_id"); c == nil || c.FK == nil || !strings.EqualFold(c.FK.Table, "orgs") {
		t.Errorf("org_id FK wrong: %+v", c)
	}
	// Declared FK is solid (not inferred); it must NOT also produce an inferred dup.
	if !hasRel(m, "users", "org_id", "orgs", false) {
		t.Errorf("missing declared relation users.org_id->orgs; rels=%+v", m.Relations)
	}
	if len(m.Relations) != 1 {
		t.Errorf("want exactly 1 relation (declared, no inferred dup), got %+v", m.Relations)
	}
	if m.Sources[0] != "t.sql" {
		t.Errorf("sources not recorded: %+v", m.Sources)
	}
}

func TestTableLevelFK(t *testing.T) {
	m := parse1(`
CREATE TABLE a (id TEXT PRIMARY KEY);
CREATE TABLE b (
  id TEXT PRIMARY KEY,
  a_ref TEXT,
  FOREIGN KEY (a_ref) REFERENCES a (id)
);`)
	if !hasRel(m, "b", "a_ref", "a", false) {
		t.Errorf("missing table-level FK b.a_ref->a; rels=%+v", m.Relations)
	}
}

func TestInferredIDRelation(t *testing.T) {
	m := parse1(`
CREATE TABLE projects (id TEXT PRIMARY KEY);
CREATE TABLE tasks (id TEXT PRIMARY KEY, project_id TEXT NOT NULL);`)
	if !hasRel(m, "tasks", "project_id", "projects", true) {
		t.Errorf("want inferred tasks.project_id->projects; rels=%+v", m.Relations)
	}
	if hasRel(m, "tasks", "project_id", "projects", false) {
		t.Errorf("should be inferred, not declared")
	}
}

func TestPolymorphicNoInference(t *testing.T) {
	// owner_id with no owner/owners table must NOT invent a relation (honesty).
	m := parse1(`CREATE TABLE anchors (id TEXT PRIMARY KEY, owner_type TEXT, owner_id TEXT);`)
	if len(m.Relations) != 0 {
		t.Errorf("polymorphic owner_id should produce no relation, got %+v", m.Relations)
	}
}

func TestAlterAddDropRename(t *testing.T) {
	m := parse1(`
CREATE TABLE t (id TEXT PRIMARY KEY);
ALTER TABLE t ADD COLUMN note TEXT;
ALTER TABLE t ADD COLUMN parent_id TEXT REFERENCES t(id);
CREATE TABLE gone (id TEXT PRIMARY KEY);
DROP TABLE gone;
ALTER TABLE t RENAME COLUMN note TO memo;`)
	if tbl(m, "gone") != nil {
		t.Errorf("dropped table still present")
	}
	tt := tbl(m, "t")
	if tt == nil || col(tt, "memo") == nil || col(tt, "note") != nil {
		t.Errorf("rename column failed: %+v", tt)
	}
	if col(tt, "parent_id") == nil {
		t.Errorf("added column parent_id missing")
	}
	if !hasRel(m, "t", "parent_id", "t", false) {
		t.Errorf("self-ref FK via ALTER ADD missing; rels=%+v", m.Relations)
	}
}

func TestRenameTable(t *testing.T) {
	m := parse1(`
CREATE TABLE old_name (id TEXT PRIMARY KEY, x_id TEXT);
CREATE TABLE xs (id TEXT PRIMARY KEY);
ALTER TABLE old_name RENAME TO new_name;`)
	if tbl(m, "old_name") != nil || tbl(m, "new_name") == nil {
		t.Errorf("table rename failed: %+v", m.Tables)
	}
	if !hasRel(m, "new_name", "x_id", "xs", true) {
		t.Errorf("inferred relation should follow renamed table; rels=%+v", m.Relations)
	}
}

func TestCommentAndStringSafety(t *testing.T) {
	m := parse1(`
-- a comment with ; and -- inside, and a , comma
/* block ; , comment */
CREATE TABLE c (id TEXT PRIMARY KEY, note TEXT DEFAULT 'a;b, -- not a comment');`)
	c := tbl(m, "c")
	if c == nil || len(c.Columns) != 2 {
		t.Fatalf("string/comment mis-parsed: %+v", c)
	}
	if col(c, "note") == nil || !col(c, "note").Nullable {
		t.Errorf("note column wrong: %+v", c)
	}
}

func TestUnknownFKWarns(t *testing.T) {
	m := parse1(`CREATE TABLE w (id TEXT PRIMARY KEY, z_id TEXT REFERENCES zzz(id));`)
	if len(m.Relations) != 0 {
		t.Errorf("relation to unknown table should be dropped, got %+v", m.Relations)
	}
	if len(m.Warnings) == 0 {
		t.Errorf("expected a warning for unknown FK target")
	}
}

func TestSelectSources(t *testing.T) {
	withSchema := SelectSources([]string{
		"internal/db/migrations/0002_b.sql",
		"internal/db/migrations/0001_a.sql",
		"db/schema.sql",
		"web/src/app.ts",
	})
	if len(withSchema) != 1 || withSchema[0] != "db/schema.sql" {
		t.Errorf("schema.sql should win: %+v", withSchema)
	}

	migs := SelectSources([]string{
		"internal/db/migrations/0002_b.sql",
		"internal/db/migrations/0001_a.sql",
		"dist/oss-mirror/migrations/0001_x.sql", // noise, excluded
		"notes.sql",
	})
	want := []string{"internal/db/migrations/0001_a.sql", "internal/db/migrations/0002_b.sql"}
	if strings.Join(migs, "|") != strings.Join(want, "|") {
		t.Errorf("migrations selection wrong: got %+v want %+v", migs, want)
	}
}

// TestRealMigrations is the dogfood: parse CodeDistill's own migrations and
// prove the parser produces a real, connected schema (core tables + at least one
// inferred *_id edge like project_id->projects). Tolerant of schema evolution.
func TestRealMigrations(t *testing.T) {
	dir := filepath.Join("..", "storage", "sqlite", "migrations")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Skipf("migrations dir not found (%v) — skipping dogfood check", err)
	}
	var names []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".sql") {
			names = append(names, e.Name())
		}
	}
	if len(names) == 0 {
		t.Skip("no .sql migrations found")
	}
	// os.ReadDir already returns sorted names; SelectSources would too.
	var srcs []Source
	for _, n := range names {
		data, err := os.ReadFile(filepath.Join(dir, n))
		if err != nil {
			t.Fatalf("read %s: %v", n, err)
		}
		srcs = append(srcs, Source{Path: n, SQL: string(data)})
	}
	m := Parse(srcs)
	if len(m.Tables) < 5 {
		t.Fatalf("expected many tables from real migrations, got %d", len(m.Tables))
	}
	if tbl(m, "projects") == nil {
		t.Errorf("core table 'projects' not found in parsed schema")
	}
	inferred := 0
	for _, r := range m.Relations {
		if r.Inferred {
			inferred++
		}
	}
	if inferred == 0 {
		t.Errorf("expected at least one inferred *_id relation from the real schema (e.g. project_id->projects)")
	}
	t.Logf("real schema: %d tables, %d relations (%d inferred), %d files, %d warnings",
		len(m.Tables), len(m.Relations), inferred, len(m.Sources), len(m.Warnings))
}

func TestProvenance(t *testing.T) {
	m := Parse([]Source{
		{Path: "migrations/0001_init.sql", SQL: `
			CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT);
			CREATE TABLE posts (id INTEGER PRIMARY KEY, user_id INTEGER REFERENCES users(id));`},
		{Path: "migrations/0002_email.sql", SQL: `
			ALTER TABLE users ADD COLUMN email TEXT;
			ALTER TABLE users ADD COLUMN nick TEXT;`},
		{Path: "experimental/0003_flags.sql", SQL: `
			ALTER TABLE posts ADD COLUMN flagged INTEGER;
			CREATE TABLE experiments (id INTEGER PRIMARY KEY);`},
	})

	u := tbl(m, "users")
	if u == nil || u.DefinedIn != "migrations/0001_init.sql" {
		t.Fatalf("users defined_in = %+v", u)
	}
	// Two ALTERs from the same file record it once; the defining file never
	// appears in modified_by.
	if len(u.ModifiedBy) != 1 || u.ModifiedBy[0] != "migrations/0002_email.sql" {
		t.Errorf("users modified_by = %v", u.ModifiedBy)
	}
	if c := col(u, "name"); c == nil || c.AddedIn != "migrations/0001_init.sql" {
		t.Errorf("users.name added_in = %+v", c)
	}
	if c := col(u, "email"); c == nil || c.AddedIn != "migrations/0002_email.sql" {
		t.Errorf("users.email added_in = %+v", c)
	}

	p := tbl(m, "posts")
	if p == nil || p.DefinedIn != "migrations/0001_init.sql" {
		t.Fatalf("posts defined_in = %+v", p)
	}
	if len(p.ModifiedBy) != 1 || p.ModifiedBy[0] != "experimental/0003_flags.sql" {
		t.Errorf("posts modified_by = %v", p.ModifiedBy)
	}
	if e := tbl(m, "experiments"); e == nil || e.DefinedIn != "experimental/0003_flags.sql" || len(e.ModifiedBy) != 0 {
		t.Errorf("experiments provenance = %+v", e)
	}
}

func TestProvenanceRenameAndDrop(t *testing.T) {
	m := Parse([]Source{
		{Path: "a.sql", SQL: `CREATE TABLE t (id INTEGER PRIMARY KEY, old TEXT, junk TEXT);`},
		{Path: "b.sql", SQL: `
			ALTER TABLE t RENAME COLUMN old TO fresh;
			ALTER TABLE t DROP COLUMN junk;`},
	})
	tt := tbl(m, "t")
	if tt == nil || len(tt.ModifiedBy) != 1 || tt.ModifiedBy[0] != "b.sql" {
		t.Fatalf("t modified_by = %+v", tt)
	}
	// A renamed column keeps the provenance of the file that first added it.
	if c := col(tt, "fresh"); c == nil || c.AddedIn != "a.sql" {
		t.Errorf("t.fresh added_in = %+v", c)
	}
}
