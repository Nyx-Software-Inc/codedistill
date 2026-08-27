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

// Package datamodel builds an entity-relationship model from a project's SQL
// schema — CREATE TABLE / ALTER TABLE / DROP TABLE replayed in order to the
// final schema state, with relations from declared FOREIGN KEYs plus, clearly
// marked as inferred, `*_id` naming-convention matches.
//
// It is DETERMINISTIC and honest by design (glass-box "can't lie"): it draws
// only what the SQL declares, marks convention guesses as inferred, and reports
// what it parsed rather than inventing structure. UC-45 slice 1.
//
// The parser targets the DDL subset real migrations use (SQLite + Postgres
// dialects), not a full SQL grammar: anything it doesn't recognize is skipped,
// never guessed.
package datamodel

import (
	"path"
	"regexp"
	"sort"
	"strings"
)

// FKRef is a column-level foreign-key target.
type FKRef struct {
	Table  string `json:"table"`
	Column string `json:"column"`
}

// Column is one field of a table in the final (post-migration) schema.
type Column struct {
	Name     string `json:"name"`
	Type     string `json:"type"`
	PK       bool   `json:"pk"`
	Nullable bool   `json:"nullable"`
	FK       *FKRef `json:"fk,omitempty"`
	// AddedIn is the source file whose statement introduced this column —
	// the CREATE TABLE body or a later ALTER TABLE ADD. Element-level
	// provenance: the diagram can say where each piece came from.
	AddedIn string `json:"added_in,omitempty"`
}

// Table is a relation (in the DB sense) with its columns, in declaration order.
// DefinedIn/ModifiedBy carry per-table provenance: which file CREATE'd the
// table and which later files altered it (apply order, deduplicated).
type Table struct {
	Name       string   `json:"name"`
	Columns    []Column `json:"columns"`
	DefinedIn  string   `json:"defined_in,omitempty"`
	ModifiedBy []string `json:"modified_by,omitempty"`
}

// Relation is an edge in the ER diagram. Inferred=false means a declared SQL
// FOREIGN KEY (solid line); Inferred=true means a `*_id` naming-convention guess
// (dashed line, honestly labeled as a guess).
type Relation struct {
	FromTable string `json:"from_table"`
	FromCol   string `json:"from_col"`
	ToTable   string `json:"to_table"`
	ToCol     string `json:"to_col"`
	Inferred  bool   `json:"inferred"`
}

// Model is the whole ER picture plus provenance: which files it read and what it
// couldn't resolve.
type Model struct {
	Tables    []Table    `json:"tables"`
	Relations []Relation `json:"relations"`
	Sources   []string   `json:"sources_parsed"`
	Warnings  []string   `json:"warnings"`
}

// Source is one SQL file's path + contents, fed to Parse in apply order.
type Source struct {
	Path string
	SQL  string
}

// noiseSegments are directories whose .sql files are almost never the project's
// own schema (generated mirrors, deps, build output).
var noiseSegments = map[string]bool{
	"node_modules": true, "vendor": true, ".git": true, "dist": true, "build": true,
}

// SelectSources picks — from a repo's tracked path list — which .sql files to
// read, and in what order. Preference: a canonical schema.sql/structure.sql
// (final state, no replay needed) → files under a migrations/ dir (replayed
// lexicographically, matching how apps apply them) → any other .sql. Pure so the
// selection is unit-testable without a repo.
func SelectSources(paths []string) []string {
	var schema, migrations, other []string
	for _, p := range paths {
		if !strings.HasSuffix(strings.ToLower(p), ".sql") || isNoise(p) {
			continue
		}
		base := strings.ToLower(path.Base(p))
		switch {
		case base == "schema.sql" || base == "structure.sql":
			schema = append(schema, p)
		case hasSegment(p, "migrations") || hasSegment(p, "migrate"):
			migrations = append(migrations, p)
		default:
			other = append(other, p)
		}
	}
	sort.Strings(schema)
	sort.Strings(migrations)
	sort.Strings(other)
	switch {
	case len(schema) > 0:
		return schema
	case len(migrations) > 0:
		return migrations
	default:
		return other
	}
}

func isNoise(p string) bool {
	for _, seg := range strings.Split(p, "/") {
		if noiseSegments[seg] {
			return true
		}
	}
	return false
}

func hasSegment(p, seg string) bool {
	for _, s := range strings.Split(p, "/") {
		if strings.EqualFold(s, seg) {
			return true
		}
	}
	return false
}

// Parse replays the given SQL sources in order and returns the final ER model.
func Parse(sources []Source) *Model {
	st := &schema{byName: map[string]*tableState{}}
	var parsed []string
	for _, src := range sources {
		clean := stripComments(src.SQL)
		touched := false
		for _, stmt := range splitOutsideGroups(clean, ';') {
			if strings.TrimSpace(stmt) == "" {
				continue
			}
			if st.apply(stmt, src.Path) {
				touched = true
			}
		}
		if touched {
			parsed = append(parsed, src.Path)
		}
	}
	return st.finalize(parsed)
}

// --- schema state ------------------------------------------------------------

type rawRel struct {
	fromCol string
	toTable string
	toCol   string
}

type tableState struct {
	name       string // display name (original case)
	order      int    // creation order, for stable output
	cols       []*Column
	colBy      map[string]*Column // lower(name) -> col
	rels       []rawRel           // declared FKs (column-level + table-level)
	definedIn  string             // file whose CREATE TABLE produced this table
	modifiedBy []string           // later files that altered it, apply order
	modSeen    map[string]bool
}

// markModified records src as a file that altered this table after its CREATE.
func (t *tableState) markModified(src string) {
	if src == "" || src == t.definedIn || t.modSeen[src] {
		return
	}
	if t.modSeen == nil {
		t.modSeen = map[string]bool{}
	}
	t.modSeen[src] = true
	t.modifiedBy = append(t.modifiedBy, src)
}

type schema struct {
	byName map[string]*tableState // lower(name) -> table
	next   int
}

func (s *schema) table(name string) *tableState {
	return s.byName[strings.ToLower(unquoteIdent(name))]
}

func (s *schema) apply(stmt, src string) bool {
	up := strings.ToUpper(strings.TrimSpace(stmt))
	switch {
	case strings.HasPrefix(up, "CREATE TABLE"):
		return s.applyCreate(stmt, src)
	case strings.HasPrefix(up, "ALTER TABLE"):
		return s.applyAlter(stmt, src)
	case strings.HasPrefix(up, "DROP TABLE"):
		return s.applyDrop(stmt)
	}
	return false
}

func (s *schema) applyCreate(stmt, src string) bool {
	rest, ok := trimFoldPrefix(strings.TrimSpace(stmt), "CREATE TABLE")
	if !ok {
		return false
	}
	if r, ok := trimFoldPrefix(rest, "IF NOT EXISTS"); ok {
		rest = r
	}
	paren := strings.IndexByte(rest, '(')
	if paren < 0 {
		return false
	}
	name := unquoteIdent(strings.TrimSpace(rest[:paren]))
	if name == "" {
		return false
	}
	body, ok := parenBody(rest[paren:])
	if !ok {
		return false
	}
	t := &tableState{name: name, order: s.next, colBy: map[string]*Column{}, definedIn: src}
	s.next++
	for _, item := range splitOutsideGroups(body, ',') {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		switch strings.ToUpper(firstWord(item)) {
		case "PRIMARY", "FOREIGN", "UNIQUE", "CHECK", "CONSTRAINT":
			s.tableConstraint(t, item)
		default:
			c := parseColumn(item)
			c.AddedIn = src
			t.addColumn(c)
		}
	}
	s.byName[strings.ToLower(name)] = t // last CREATE wins (idempotent re-create)
	return true
}

func (s *schema) applyAlter(stmt, src string) bool {
	rest, ok := trimFoldPrefix(strings.TrimSpace(stmt), "ALTER TABLE")
	if !ok {
		return false
	}
	toks := fieldsOutsideGroups(rest)
	if len(toks) < 2 {
		return false
	}
	t := s.table(toks[0])
	if t == nil {
		return false
	}
	action := strings.ToUpper(toks[1])
	switch action {
	case "ADD":
		// ADD [COLUMN] <coldef>. fieldsOutsideGroups keeps parenthesized/quoted
		// groups intact, so rejoining the tail reconstructs the column def.
		start := 2
		if len(toks) > 2 && strings.EqualFold(toks[2], "COLUMN") {
			start = 3
		}
		if start < len(toks) {
			if c := parseColumn(strings.Join(toks[start:], " ")); c.Name != "" {
				c.AddedIn = src
				s.constrainAddedCol(t, c)
			}
		}
		t.markModified(src)
		return true
	case "RENAME":
		if s.applyRename(t, toks) {
			t.markModified(src)
			return true
		}
		return false
	case "DROP":
		col := ""
		if len(toks) >= 3 && strings.EqualFold(toks[2], "COLUMN") && len(toks) >= 4 {
			col = toks[3]
		} else if len(toks) >= 3 {
			col = toks[2]
		}
		t.dropColumn(unquoteIdent(col))
		t.markModified(src)
		return true
	}
	return false
}

func (s *schema) applyRename(t *tableState, toks []string) bool {
	// ALTER TABLE x RENAME TO y   |   RENAME [COLUMN] old TO new
	if len(toks) >= 4 && strings.EqualFold(toks[2], "TO") {
		s.renameTable(t, unquoteIdent(toks[3]))
		return true
	}
	// RENAME [COLUMN] old TO new
	i := 2
	if i < len(toks) && strings.EqualFold(toks[i], "COLUMN") {
		i++
	}
	if i+2 < len(toks) && strings.EqualFold(toks[i+1], "TO") {
		t.renameColumn(unquoteIdent(toks[i]), unquoteIdent(toks[i+2]))
		return true
	}
	return false
}

func (s *schema) renameTable(t *tableState, newName string) {
	if newName == "" {
		return
	}
	delete(s.byName, strings.ToLower(t.name))
	t.name = newName
	s.byName[strings.ToLower(newName)] = t
}

func (s *schema) applyDrop(stmt string) bool {
	rest, ok := trimFoldPrefix(strings.TrimSpace(stmt), "DROP TABLE")
	if !ok {
		return false
	}
	if r, ok := trimFoldPrefix(rest, "IF EXISTS"); ok {
		rest = r
	}
	name := unquoteIdent(strings.TrimSpace(firstWord(rest)))
	if name == "" {
		return false
	}
	delete(s.byName, strings.ToLower(name))
	return true
}

// tableConstraint handles a body item that's a table-level constraint. Only
// FOREIGN KEY and PRIMARY KEY carry structure we render.
func (s *schema) tableConstraint(t *tableState, item string) {
	up := strings.ToUpper(item)
	if strings.Contains(up, "FOREIGN KEY") {
		if m := reTableFK.FindStringSubmatch(item); m != nil {
			cols := splitIdents(m[1])
			target := unquoteIdent(m[2])
			tcols := splitIdents(m[3])
			for i, c := range cols {
				tc := "id"
				if i < len(tcols) {
					tc = tcols[i]
				} else if len(tcols) == 1 {
					tc = tcols[0]
				}
				t.rels = append(t.rels, rawRel{fromCol: c, toTable: target, toCol: tc})
				if col := t.colBy[strings.ToLower(c)]; col != nil && col.FK == nil {
					col.FK = &FKRef{Table: target, Column: tc}
				}
			}
		}
		return
	}
	if strings.Contains(up, "PRIMARY KEY") {
		if m := rePKcols.FindStringSubmatch(item); m != nil {
			for _, c := range splitIdents(m[1]) {
				if col := t.colBy[strings.ToLower(c)]; col != nil {
					col.PK = true
					col.Nullable = false
				}
			}
		}
	}
}

// constrainAddedCol records a column added via ALTER TABLE, including an inline
// REFERENCES clause if present.
func (s *schema) constrainAddedCol(t *tableState, c Column) {
	t.addColumn(c)
	if c.FK != nil {
		t.rels = append(t.rels, rawRel{fromCol: c.Name, toTable: c.FK.Table, toCol: c.FK.Column})
	}
}

func (t *tableState) addColumn(c Column) {
	if c.Name == "" {
		return
	}
	if c.FK != nil {
		t.rels = append(t.rels, rawRel{fromCol: c.Name, toTable: c.FK.Table, toCol: c.FK.Column})
	}
	key := strings.ToLower(c.Name)
	if _, exists := t.colBy[key]; exists {
		return
	}
	col := c
	t.cols = append(t.cols, &col)
	t.colBy[key] = &col
}

func (t *tableState) dropColumn(name string) {
	key := strings.ToLower(name)
	if _, ok := t.colBy[key]; !ok {
		return
	}
	delete(t.colBy, key)
	out := t.cols[:0]
	for _, c := range t.cols {
		if !strings.EqualFold(c.Name, name) {
			out = append(out, c)
		}
	}
	t.cols = out
}

func (t *tableState) renameColumn(oldName, newName string) {
	key := strings.ToLower(oldName)
	col, ok := t.colBy[key]
	if !ok || newName == "" {
		return
	}
	delete(t.colBy, key)
	col.Name = newName
	t.colBy[strings.ToLower(newName)] = col
	for i := range t.rels {
		if strings.EqualFold(t.rels[i].fromCol, oldName) {
			t.rels[i].fromCol = newName
		}
	}
}

// finalize renders the final schema into a Model: ordered tables, resolved
// declared relations, then inferred `*_id` relations, plus warnings.
func (s *schema) finalize(parsed []string) *Model {
	tables := make([]*tableState, 0, len(s.byName))
	for _, t := range s.byName {
		tables = append(tables, t)
	}
	sort.Slice(tables, func(i, j int) bool { return tables[i].order < tables[j].order })

	m := &Model{Sources: parsed, Tables: []Table{}, Relations: []Relation{}, Warnings: []string{}}
	for _, t := range tables {
		cols := make([]Column, len(t.cols))
		for i, c := range t.cols {
			cols[i] = *c
		}
		m.Tables = append(m.Tables, Table{
			Name: t.name, Columns: cols,
			DefinedIn: t.definedIn, ModifiedBy: t.modifiedBy,
		})
	}

	seen := map[string]bool{}
	add := func(r Relation) {
		k := strings.ToLower(r.FromTable + "\x00" + r.FromCol + "\x00" + r.ToTable + "\x00" + r.ToCol)
		if seen[k] {
			return
		}
		seen[k] = true
		m.Relations = append(m.Relations, r)
	}
	declaredFrom := map[string]bool{} // fromTable\x00fromCol has a declared FK

	for _, t := range tables {
		for _, rr := range t.rels {
			target := s.table(rr.toTable)
			if target == nil {
				m.Warnings = append(m.Warnings,
					"foreign key "+t.name+"."+rr.fromCol+" references unknown table "+unquoteIdent(rr.toTable))
				continue
			}
			toCol := rr.toCol
			if toCol == "" {
				toCol = target.pkName()
			}
			declaredFrom[strings.ToLower(t.name+"\x00"+rr.fromCol)] = true
			add(Relation{FromTable: t.name, FromCol: rr.fromCol, ToTable: target.name, ToCol: toCol})
		}
	}

	// Inferred: a `<base>_id` column pointing at a table named like <base>.
	for _, t := range tables {
		for _, c := range t.cols {
			low := strings.ToLower(c.Name)
			if !strings.HasSuffix(low, "_id") || low == "_id" {
				continue
			}
			if declaredFrom[strings.ToLower(t.name+"\x00"+c.Name)] {
				continue
			}
			base := strings.TrimSuffix(low, "_id")
			if target := s.matchByName(base); target != nil && target != t {
				add(Relation{FromTable: t.name, FromCol: c.Name, ToTable: target.name, ToCol: target.pkName(), Inferred: true})
			}
		}
	}
	return m
}

func (t *tableState) pkName() string {
	for _, c := range t.cols {
		if c.PK {
			return c.Name
		}
	}
	if _, ok := t.colBy["id"]; ok {
		return "id"
	}
	return "id"
}

// matchByName finds a table whose name matches the singular base by simple
// English pluralization (base, base+s, base+es, y→ies). Deterministic, first
// match in creation order.
func (s *schema) matchByName(base string) *tableState {
	cands := []string{base, base + "s", base + "es"}
	if strings.HasSuffix(base, "y") {
		cands = append(cands, base[:len(base)-1]+"ies")
	}
	var best *tableState
	for _, t := range s.byName {
		for _, cand := range cands {
			if strings.EqualFold(t.name, cand) {
				if best == nil || t.order < best.order {
					best = t
				}
			}
		}
	}
	return best
}

// --- column parsing ----------------------------------------------------------

var constraintKW = map[string]bool{
	"PRIMARY": true, "NOT": true, "NULL": true, "DEFAULT": true, "REFERENCES": true,
	"UNIQUE": true, "CHECK": true, "GENERATED": true, "COLLATE": true,
	"AUTOINCREMENT": true, "AS": true, "CONSTRAINT": true,
}

var (
	reInlineFK = regexp.MustCompile(`(?is)\bREFERENCES\s+([^\s(]+)\s*(?:\(\s*([^)\s]+)\s*\))?`)
	reTableFK  = regexp.MustCompile(`(?is)\bFOREIGN\s+KEY\s*\(([^)]*)\)\s*REFERENCES\s+([^\s(]+)\s*(?:\(([^)]*)\))?`)
	rePKcols   = regexp.MustCompile(`(?is)\bPRIMARY\s+KEY\s*\(([^)]*)\)`)
)

// parseColumn parses one column definition ("name TYPE constraints...").
func parseColumn(def string) Column {
	toks := fieldsOutsideGroups(def)
	if len(toks) == 0 {
		return Column{}
	}
	c := Column{Name: unquoteIdent(toks[0]), Nullable: true}
	i := 1
	var typeToks []string
	for i < len(toks) && !constraintKW[strings.ToUpper(toks[i])] {
		typeToks = append(typeToks, toks[i])
		i++
	}
	c.Type = strings.Join(typeToks, " ")
	rest := strings.ToUpper(def)
	if strings.Contains(rest, "PRIMARY KEY") {
		c.PK = true
	}
	if strings.Contains(rest, "NOT NULL") || c.PK {
		c.Nullable = false
	}
	if m := reInlineFK.FindStringSubmatch(def); m != nil {
		tc := "id"
		if m[2] != "" {
			tc = unquoteIdent(m[2])
		}
		c.FK = &FKRef{Table: unquoteIdent(m[1]), Column: tc}
	}
	return c
}

func splitIdents(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if id := unquoteIdent(strings.TrimSpace(p)); id != "" {
			out = append(out, id)
		}
	}
	return out
}
