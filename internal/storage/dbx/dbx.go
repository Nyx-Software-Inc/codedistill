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

// Package dbx is a thin dialect-aware wrapper over database/sql. It exists so the
// one set of storage query methods (the 160+ on the SQL store) can run against
// either SQLite or Postgres: the wrapper rebinds `?` placeholders to `$1,$2,…`
// for Postgres (a no-op for SQLite) and carries the few other per-dialect
// differences (boolean literals). Call sites use the same Exec/Query methods as
// raw *sql.DB, so adopting it is mechanical and behavior-neutral for SQLite.
//
// Phase 1 of the Postgres backend (docs/design/postgres-backend.md) introduces
// this seam; only the SQLite dialect is wired today. The Postgres dialect's
// rebind is implemented + tested here so Phase 3 is just driver wiring.
package dbx

import (
	"context"
	"database/sql"
	"strconv"
	"strings"
)

// Dialect selects the SQL flavor. The zero value is SQLite (the default and only
// wired backend today), so an un-set dialect behaves like the legacy code.
type Dialect int

const (
	SQLite Dialect = iota
	Postgres
)

func (d Dialect) String() string {
	if d == Postgres {
		return "postgres"
	}
	return "sqlite"
}

// BoolTrue / BoolFalse give the dialect's boolean literal, for the handful of
// queries that compare a boolean column against a literal (`enabled = 1`).
// SQLite stores booleans as 0/1; Postgres uses TRUE/FALSE.
func (d Dialect) BoolTrue() string {
	if d == Postgres {
		return "TRUE"
	}
	return "1"
}

func (d Dialect) BoolFalse() string {
	if d == Postgres {
		return "FALSE"
	}
	return "0"
}

// rebind converts `?` placeholders to `$1,$2,…` for Postgres. No-op for SQLite.
func (d Dialect) rebind(query string) string {
	if d != Postgres {
		return query
	}
	return toDollar(query)
}

// toDollar rewrites positional `?` placeholders as `$1,$2,…`, skipping any `?`
// inside single-quoted string literals (our queries don't embed one, but this
// keeps the rewrite safe if one ever appears).
func toDollar(query string) string {
	var b strings.Builder
	b.Grow(len(query) + 8)
	n := 0
	inString := false
	for i := 0; i < len(query); i++ {
		c := query[i]
		if c == '\'' {
			inString = !inString
			b.WriteByte(c)
			continue
		}
		if c == '?' && !inString {
			// SQLite's explicit numbered placeholder `?N` maps to Postgres `$N`
			// (same meaning: the Nth arg, reusable). A bare `?` auto-increments.
			j := i + 1
			for j < len(query) && query[j] >= '0' && query[j] <= '9' {
				j++
			}
			if j > i+1 {
				b.WriteByte('$')
				b.WriteString(query[i+1 : j])
				i = j - 1
				continue
			}
			n++
			b.WriteByte('$')
			b.WriteString(strconv.Itoa(n))
			continue
		}
		b.WriteByte(c)
	}
	return b.String()
}

// DB wraps *sql.DB with dialect-aware placeholder rebinding. Its method set
// mirrors the subset of *sql.DB the store uses, so call sites are unchanged.
type DB struct {
	db      *sql.DB
	dialect Dialect
}

// New wraps a *sql.DB with a dialect.
func New(db *sql.DB, d Dialect) *DB { return &DB{db: db, dialect: d} }

// Dialect returns the wrapper's dialect (for the few dialect-aware query bits).
func (d *DB) Dialect() Dialect { return d.dialect }

// Raw returns the underlying *sql.DB for dialect-specific setup (PRAGMAs, pings).
func (d *DB) Raw() *sql.DB { return d.db }

func (d *DB) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	return d.db.ExecContext(ctx, d.dialect.rebind(query), args...)
}

func (d *DB) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	return d.db.QueryContext(ctx, d.dialect.rebind(query), args...)
}

func (d *DB) QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row {
	return d.db.QueryRowContext(ctx, d.dialect.rebind(query), args...)
}

func (d *DB) BeginTx(ctx context.Context, opts *sql.TxOptions) (*Tx, error) {
	tx, err := d.db.BeginTx(ctx, opts)
	if err != nil {
		return nil, err
	}
	return &Tx{tx: tx, dialect: d.dialect}, nil
}

func (d *DB) Close() error { return d.db.Close() }

// Tx wraps *sql.Tx with the same dialect-aware rebinding.
type Tx struct {
	tx      *sql.Tx
	dialect Dialect
}

func (t *Tx) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	return t.tx.ExecContext(ctx, t.dialect.rebind(query), args...)
}

func (t *Tx) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	return t.tx.QueryContext(ctx, t.dialect.rebind(query), args...)
}

func (t *Tx) QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row {
	return t.tx.QueryRowContext(ctx, t.dialect.rebind(query), args...)
}

func (t *Tx) PrepareContext(ctx context.Context, query string) (*sql.Stmt, error) {
	return t.tx.PrepareContext(ctx, t.dialect.rebind(query))
}

func (t *Tx) Commit() error   { return t.tx.Commit() }
func (t *Tx) Rollback() error { return t.tx.Rollback() }
