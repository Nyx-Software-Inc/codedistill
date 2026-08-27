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
	"fmt"
	"sort"
	"time"

	"codedistill/internal/domain"
	"codedistill/internal/storage/dbx"
)

// DashboardThroughput returns per-day created/completed counts and open
// totals for the three terminal-state item types (todos, bugs,
// use_cases). Backs the project metrics dashboard Phase A panel #1.
//
// The terminal-status SQL literals below must mirror
// internal/domain/lifecycle.go's IsXDone helpers. If a new status is
// added there, mirror it here or open counts will skew.
func (s *Store) DashboardThroughput(ctx context.Context, projectID string, since time.Time) (*domain.DashboardThroughput, error) {
	out := &domain.DashboardThroughput{
		CreatedByDay:   map[string][]domain.DayCount{},
		CompletedByDay: map[string][]domain.DayCount{},
		Open:           map[string]int{},
		Since:          since,
	}

	// All table / column names below are package-local constants — no
	// user input flows into the fmt.Sprintf-built SQL.
	specs := []struct {
		slug          string
		table         string
		completionCol string
		openWhere     string
	}{
		{"todos", "todo_items", "completed_at", "status NOT IN ('complete','abandoned')"},
		{"bugs", "bug_items", "completed_at", "status NOT IN ('fixed','verified','closed','not_a_bug','wont_fix','duplicate')"},
		{"use_cases", "use_case_items", "implementation_date", "status NOT IN ('completed','rejected')"},
	}

	for _, sp := range specs {
		created, err := dashboardSeries(ctx, s.DB, sp.table, "created_at", projectID, since)
		if err != nil {
			return nil, fmt.Errorf("%s created series: %w", sp.slug, err)
		}
		out.CreatedByDay[sp.slug] = created

		completed, err := dashboardSeries(ctx, s.DB, sp.table, sp.completionCol, projectID, since)
		if err != nil {
			return nil, fmt.Errorf("%s completed series: %w", sp.slug, err)
		}
		out.CompletedByDay[sp.slug] = completed

		var open int
		q := fmt.Sprintf("SELECT COUNT(*) FROM %s WHERE project_id = ? AND %s", sp.table, sp.openWhere)
		if err := s.DB.QueryRowContext(ctx, q, projectID).Scan(&open); err != nil {
			return nil, fmt.Errorf("%s open count: %w", sp.slug, err)
		}
		out.Open[sp.slug] = open
	}

	return out, nil
}

// dashboardSeries pulls the timestamp column for every matching row in
// the window, then buckets in Go using time.Local. col is one of
// created_at, completed_at, implementation_date — all the dashboard
// reads. NULL rows are filtered so the "completed" series excludes
// items that never reached a terminal status.
//
// Bucketing in Go (rather than SQLite's date(ts,'localtime')) avoids
// the driver's timestamp-format roundtrip — modernc.org/sqlite writes
// time.Time via String() in a format SQLite's date() doesn't fully
// parse. The same `parseFlexibleTime` helper that handles reads in
// implementation_matches.go does the parse here.
func dashboardSeries(ctx context.Context, db *dbx.DB, table, col, projectID string, since time.Time) ([]domain.DayCount, error) {
	q := fmt.Sprintf(`
		SELECT %s
		FROM %s
		WHERE project_id = ?
		  AND %s IS NOT NULL
		  AND %s >= ?`, col, table, col, col)
	rows, err := db.QueryContext(ctx, q, projectID, since)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	buckets := map[string]int{}
	for rows.Next() {
		var ts string
		if err := rows.Scan(&ts); err != nil {
			return nil, err
		}
		t, err := parseFlexibleTime(ts)
		if err != nil {
			return nil, fmt.Errorf("parse %s.%s: %w", table, col, err)
		}
		buckets[t.Local().Format("2006-01-02")]++
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	dates := make([]string, 0, len(buckets))
	for d := range buckets {
		dates = append(dates, d)
	}
	sort.Strings(dates)
	out := make([]domain.DayCount, 0, len(dates))
	for _, d := range dates {
		out = append(out, domain.DayCount{Date: d, Count: buckets[d]})
	}
	return out, nil
}

// DashboardIndexHealth returns the indexing-health rollup for one
// project. See the interface doc in storage.go. All table/column names
// in the fmt.Sprintf-built SQL are package-local constants.
func (s *Store) DashboardIndexHealth(ctx context.Context, projectID string) (*domain.DashboardIndexHealth, error) {
	out := &domain.DashboardIndexHealth{
		Coverage:            map[string]domain.CoverageCount{},
		AnchorsByProvenance: map[string]int{},
	}

	// Embedding coverage per item table. scratchpad_items hang off
	// scratchpads; the four derived tables carry project_id directly.
	coverageSpecs := []struct {
		slug  string
		query string
	}{
		{"items", `SELECT COUNT(*), COUNT(embedding) FROM scratchpad_items
			WHERE scratchpad_id IN (SELECT id FROM scratchpads WHERE project_id = ?)`},
		{"todos", `SELECT COUNT(*), COUNT(embedding) FROM todo_items WHERE project_id = ?`},
		{"bugs", `SELECT COUNT(*), COUNT(embedding) FROM bug_items WHERE project_id = ?`},
		{"kb", `SELECT COUNT(*), COUNT(embedding) FROM knowledge_entries WHERE project_id = ?`},
		{"use_cases", `SELECT COUNT(*), COUNT(embedding) FROM use_case_items WHERE project_id = ?`},
	}
	for _, sp := range coverageSpecs {
		var cc domain.CoverageCount
		if err := s.DB.QueryRowContext(ctx, sp.query, projectID).Scan(&cc.Total, &cc.Embedded); err != nil {
			return nil, fmt.Errorf("%s coverage: %w", sp.slug, err)
		}
		out.Coverage[sp.slug] = cc
	}

	// Code chunks: count, content bytes, embedding backlog + failures.
	err := s.DB.QueryRowContext(ctx, `
		SELECT COUNT(*),
		       COALESCE(SUM(LENGTH(content)), 0),
		       COALESCE(SUM(CASE WHEN embedding IS NULL AND embed_failed_at IS NULL THEN 1 ELSE 0 END), 0),
		       COALESCE(SUM(CASE WHEN embed_failed_at IS NOT NULL THEN 1 ELSE 0 END), 0)
		FROM code_chunks WHERE project_id = ?`, projectID).
		Scan(&out.ChunkCount, &out.ChunkBytes, &out.ChunkUnembedded, &out.ChunkFailed)
	if err != nil {
		return nil, fmt.Errorf("chunk stats: %w", err)
	}

	// Anchors by provenance. code_anchors has no project_id — scope via
	// the owner tables.
	rows, err := s.DB.QueryContext(ctx, `
		SELECT a.provenance, COUNT(*)
		FROM code_anchors a
		WHERE (a.owner_type = 'todo_item'       AND a.owner_id IN (SELECT id FROM todo_items       WHERE project_id = ?1))
		   OR (a.owner_type = 'bug_item'        AND a.owner_id IN (SELECT id FROM bug_items        WHERE project_id = ?1))
		   OR (a.owner_type = 'use_case_item'   AND a.owner_id IN (SELECT id FROM use_case_items   WHERE project_id = ?1))
		   OR (a.owner_type = 'knowledge_entry' AND a.owner_id IN (SELECT id FROM knowledge_entries WHERE project_id = ?1))
		   OR (a.owner_type = 'scratchpad_item' AND a.owner_id IN (
		         SELECT i.id FROM scratchpad_items i
		         JOIN scratchpads sp ON sp.id = i.scratchpad_id
		         WHERE sp.project_id = ?1))
		GROUP BY a.provenance`, projectID)
	if err != nil {
		return nil, fmt.Errorf("anchor counts: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var prov string
		var n int
		if err := rows.Scan(&prov, &n); err != nil {
			return nil, err
		}
		out.AnchorsByProvenance[prov] = n
		out.AnchorTotal += n
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Classify-time dedup flags on this project's scratchpad items.
	err = s.DB.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM scratchpad_items
		WHERE similar_to_id IS NOT NULL
		  AND scratchpad_id IN (SELECT id FROM scratchpads WHERE project_id = ?)`, projectID).
		Scan(&out.DedupFlagged)
	if err != nil {
		return nil, fmt.Errorf("dedup count: %w", err)
	}
	return out, nil
}

// DashboardClosedItems returns closed/implemented items with their
// lifecycle timestamps + commit SHA. See the interface doc in storage.go.
func (s *Store) DashboardClosedItems(ctx context.Context, projectID string) ([]domain.DashboardClosedItem, error) {
	specs := []struct {
		slug      string
		table     string
		closedCol string
	}{
		{"todos", "todo_items", "completed_at"},
		{"bugs", "bug_items", "completed_at"},
		{"use_cases", "use_case_items", "implementation_date"},
	}
	out := []domain.DashboardClosedItem{}
	for _, sp := range specs {
		q := fmt.Sprintf(`
			SELECT created_at, %s, COALESCE(commit_sha, '')
			FROM %s
			WHERE project_id = ? AND %s IS NOT NULL`, sp.closedCol, sp.table, sp.closedCol)
		rows, err := s.DB.QueryContext(ctx, q, projectID)
		if err != nil {
			return nil, fmt.Errorf("%s closed items: %w", sp.slug, err)
		}
		for rows.Next() {
			var createdRaw, closedRaw, sha string
			if err := rows.Scan(&createdRaw, &closedRaw, &sha); err != nil {
				rows.Close()
				return nil, err
			}
			created, err := parseFlexibleTime(createdRaw)
			if err != nil {
				rows.Close()
				return nil, fmt.Errorf("parse %s.created_at: %w", sp.table, err)
			}
			closed, err := parseFlexibleTime(closedRaw)
			if err != nil {
				rows.Close()
				return nil, fmt.Errorf("parse %s.%s: %w", sp.table, sp.closedCol, err)
			}
			out = append(out, domain.DashboardClosedItem{
				Type:      sp.slug,
				CreatedAt: created,
				ClosedAt:  closed,
				CommitSHA: sha,
			})
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return nil, err
		}
		rows.Close()
	}
	return out, nil
}
