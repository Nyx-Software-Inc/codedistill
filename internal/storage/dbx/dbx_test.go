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

package dbx

import "testing"

func TestRebindSQLiteIsNoOp(t *testing.T) {
	q := `SELECT * FROM t WHERE a = ? AND b = ? OR c = ?`
	if got := SQLite.rebind(q); got != q {
		t.Errorf("SQLite rebind changed the query:\n got %q\nwant %q", got, q)
	}
}

func TestRebindPostgresToDollar(t *testing.T) {
	cases := []struct{ in, want string }{
		{`a = ?`, `a = $1`},
		{`a = ? AND b = ? OR c = ?`, `a = $1 AND b = $2 OR c = $3`},
		{`INSERT INTO t (x,y) VALUES (?, ?)`, `INSERT INTO t (x,y) VALUES ($1, $2)`},
		{`no placeholders here`, `no placeholders here`},
		// `?` inside a string literal must be left alone.
		{`a = ? AND note = 'why? really' AND b = ?`, `a = $1 AND note = 'why? really' AND b = $2`},
		// SQLite explicit numbered placeholders `?N` → `$N` (reused arg).
		{`a = ?1 OR b = ?1 OR c = ?1`, `a = $1 OR b = $1 OR c = $1`},
		{`x = ?1 AND y = ?2`, `x = $1 AND y = $2`},
	}
	for _, c := range cases {
		if got := Postgres.rebind(c.in); got != c.want {
			t.Errorf("Postgres.rebind(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestBoolLiterals(t *testing.T) {
	if SQLite.BoolTrue() != "1" || SQLite.BoolFalse() != "0" {
		t.Errorf("sqlite bool literals wrong: %s/%s", SQLite.BoolTrue(), SQLite.BoolFalse())
	}
	if Postgres.BoolTrue() != "TRUE" || Postgres.BoolFalse() != "FALSE" {
		t.Errorf("postgres bool literals wrong: %s/%s", Postgres.BoolTrue(), Postgres.BoolFalse())
	}
}
