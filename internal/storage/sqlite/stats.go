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

	"codedistill/internal/domain"
)

// GlobalStats aggregates cross-project usage counts for the profile Stats modal
// (UC-64): row counts for projects/scratchpads/KB, and per-status breakdowns for
// todos/bugs/use-cases. A handful of cheap COUNT/GROUP BY queries.
func (s *Store) GlobalStats(ctx context.Context) (*domain.GlobalStats, error) {
	out := &domain.GlobalStats{
		Todos:    map[string]int{},
		Bugs:     map[string]int{},
		UseCases: map[string]int{},
	}
	count := func(table string) (int, error) {
		var n int
		// table is a hardcoded constant, never user input.
		err := s.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM `+table).Scan(&n)
		return n, err
	}
	var err error
	if out.Projects, err = count("projects"); err != nil {
		return nil, fmt.Errorf("count projects: %w", err)
	}
	if out.Scratchpads, err = count("scratchpads"); err != nil {
		return nil, fmt.Errorf("count scratchpads: %w", err)
	}
	if out.KB, err = count("knowledge_entries"); err != nil {
		return nil, fmt.Errorf("count knowledge entries: %w", err)
	}

	// By status, the items behind an archived card counted apart: archiving
	// hides the card, and the work item keeps whatever status it had. An item
	// whose card was deleted has no card, and is not archived.
	byStatus := func(table string, dst map[string]int, archived *int) error {
		rows, err := s.DB.QueryContext(ctx,
			`SELECT w.status, si.archived_at IS NOT NULL, COUNT(*)
			   FROM `+table+` w LEFT JOIN scratchpad_items si ON si.id = w.source_item_id
			  GROUP BY w.status, si.archived_at IS NOT NULL`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var st string
			var arch bool
			var n int
			if err := rows.Scan(&st, &arch, &n); err != nil {
				return err
			}
			if arch {
				*archived += n
			} else {
				dst[st] += n
			}
		}
		return rows.Err()
	}
	if err = byStatus("todo_items", out.Todos, &out.TodosArchived); err != nil {
		return nil, fmt.Errorf("todo status counts: %w", err)
	}
	if err = byStatus("bug_items", out.Bugs, &out.BugsArchived); err != nil {
		return nil, fmt.Errorf("bug status counts: %w", err)
	}
	if err = byStatus("use_case_items", out.UseCases, &out.UseCasesArchived); err != nil {
		return nil, fmt.Errorf("use-case status counts: %w", err)
	}
	return out, nil
}
