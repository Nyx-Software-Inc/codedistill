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

package api

import (
	"context"
	"testing"
	"time"

	"codedistill/internal/domain"
)

// TestDueDateRoundTrip: set, clear (persisted), and reject a bad format
// on a todo's due_date (CodeDestill_imports todo #6).
func TestDueDateRoundTrip(t *testing.T) {
	srv, _, store := setupWithStore(t)
	ctx := context.Background()
	if err := store.CreateProject(ctx, &domain.Project{ID: "p1", Name: "P", CreatedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	var td domain.TodoItem
	doJSON(t, srv, "POST", "/api/v1/projects/p1/todos", map[string]string{"subject": "x"}, 201, &td)

	var set domain.TodoItem
	doJSON(t, srv, "PATCH", "/api/v1/todos/"+td.ID, map[string]string{"due_date": "2026-12-31"}, 200, &set)
	if set.DueDate == nil || set.DueDate.Format("2006-01-02") != "2026-12-31" {
		t.Fatalf("set due=%v", set.DueDate)
	}

	var cleared domain.TodoItem
	doJSON(t, srv, "PATCH", "/api/v1/todos/"+td.ID, map[string]string{"due_date": ""}, 200, &cleared)
	if cleared.DueDate != nil {
		t.Errorf("due not cleared: %v", cleared.DueDate)
	}
	var got domain.TodoItem
	doJSON(t, srv, "GET", "/api/v1/todos/"+td.ID, nil, 200, &got)
	if got.DueDate != nil {
		t.Errorf("clear not persisted: %v", got.DueDate)
	}
	doJSON(t, srv, "PATCH", "/api/v1/todos/"+td.ID, map[string]string{"due_date": "nope"}, 400, nil)
}
