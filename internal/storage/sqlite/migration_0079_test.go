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
	"testing"
	"time"
)

// TestMigration0079TimestampsUTC seeds timestamps in every shape the old
// driver default (time.String) produced, plus SQLite's own CURRENT_TIMESTAMP
// form, and checks 0079 rewrites the Go-shaped ones to UTC without moving the
// instant, and leaves the rest alone.
func TestMigration0079TimestampsUTC(t *testing.T) {
	s, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	ctx := context.Background()
	applyMigrationsThrough(t, s, "0078_sequence_workflow.sql")

	cases := []struct{ id, stored, want string }{
		{"edt-mono", "2026-06-20 08:29:09.146354825 -0400 EDT m=+376.149400620", "2026-06-20 12:29:09.146354825+00:00"},
		{"utc-frac", "2026-08-03 21:34:48.9653829 +0000 UTC", "2026-08-03 21:34:48.9653829+00:00"},
		{"edt-whole", "2026-10-30 22:15:00 -0400 EDT", "2026-10-31 02:15:00+00:00"}, // crosses midnight
		{"est", "2026-12-01 09:00:00 -0500 EST", "2026-12-01 14:00:00+00:00"},
		{"east-of-utc", "2026-07-01 01:30:00.5 +0530 IST", "2026-06-30 20:00:00.5+00:00"},
		{"sqlite-native", "2026-07-01 10:00:00", "2026-07-01 10:00:00"}, // already UTC; untouched
	}
	for i, c := range cases {
		// Written as raw text: the store now formats time.Time itself, so this
		// is the only way to get the old shape into the table.
		if _, err := s.DB.ExecContext(ctx,
			`INSERT INTO projects (id, name, created_at) VALUES (?, ?, CAST(? AS TEXT))`,
			c.id, c.id, c.stored); err != nil {
			t.Fatalf("seed %d: %v", i, err)
		}
	}

	if err := s.Migrate(ctx); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	for _, c := range cases {
		var raw string
		if err := s.DB.QueryRowContext(ctx,
			`SELECT CAST(created_at AS TEXT) FROM projects WHERE id = ?`, c.id).Scan(&raw); err != nil {
			t.Fatalf("read %s: %v", c.id, err)
		}
		if raw != c.want {
			t.Errorf("%s: stored %q, want %q", c.id, raw, c.want)
		}
	}

	// And the driver still reads the rewritten rows as the same instant.
	var got time.Time
	if err := s.DB.QueryRowContext(ctx,
		`SELECT created_at FROM projects WHERE id = 'edt-whole'`).Scan(&got); err != nil {
		t.Fatal(err)
	}
	want := time.Date(2026, 10, 31, 2, 15, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Errorf("read back %v, want %v", got, want)
	}
}

// TestNewTimestampsAreUTCAndSortable pins the write side: a local time.Time
// goes in as UTC in SQLite's format, so text order is time order.
func TestNewTimestampsAreUTCAndSortable(t *testing.T) {
	s, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	ctx := context.Background()
	if err := s.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	ny, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Skip("no tz database:", err)
	}
	// The night the clocks go back: 01:30 EDT, then half an hour later it is
	// 01:00 EST. Under the old format the later row ("01:00:00 -0500 EST")
	// sorted first.
	early := time.Date(2026, 11, 1, 1, 30, 0, 0, ny) // 05:30Z
	later := early.Add(30 * time.Minute)             // 06:00Z, local 01:00 EST

	for id, ts := range map[string]time.Time{"a-early": early, "b-later": later} {
		if _, err := s.DB.ExecContext(ctx,
			`INSERT INTO projects (id, name, created_at) VALUES (?, ?, ?)`, id, id, ts); err != nil {
			t.Fatal(err)
		}
	}
	var raw string
	if err := s.DB.QueryRowContext(ctx,
		`SELECT CAST(created_at AS TEXT) FROM projects WHERE id = 'a-early'`).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	if raw != "2026-11-01 05:30:00+00:00" {
		t.Errorf("stored %q, want UTC in SQLite format", raw)
	}
	var first string
	if err := s.DB.QueryRowContext(ctx,
		`SELECT id FROM projects WHERE id IN ('a-early','b-later') ORDER BY created_at LIMIT 1`).Scan(&first); err != nil {
		t.Fatal(err)
	}
	if first != "a-early" {
		t.Errorf("ORDER BY created_at put %s first; text order should match time order", first)
	}
}
