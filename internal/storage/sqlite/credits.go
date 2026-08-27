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

// CreditsSummary returns the workspace-wide vanity totals behind the
// hidden credits modal. Six independent COUNT queries; cheap enough to
// run on demand. Not scoped to a project — the modal is "you" stats
// across everything the user has ever distilled.
func (s *Store) CreditsSummary(ctx context.Context) (*domain.CreditsSummary, error) {
	out := &domain.CreditsSummary{}

	// (count target, destination pointer). Kept as inline pairs rather
	// than a struct slice — a few rows, no need for ceremony.
	queries := []struct {
		sql  string
		dest *int
	}{
		{`SELECT COUNT(*) FROM scratchpad_items`, &out.ItemsCreated},
		{`SELECT COUNT(*) FROM todo_items WHERE status = 'complete'`, &out.TodosCompleted},
		{`SELECT COUNT(*) FROM bug_items`, &out.BugsFiled},
	}
	for _, q := range queries {
		if err := s.DB.QueryRowContext(ctx, q.sql).Scan(q.dest); err != nil {
			return nil, fmt.Errorf("credits query %q: %w", q.sql, err)
		}
	}
	return out, nil
}
