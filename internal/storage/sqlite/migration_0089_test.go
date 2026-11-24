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

	"codedistill/internal/domain"
)

// 0089 rebuilds the jobs table. Ten tables refer to it with ON DELETE CASCADE;
// rebuilt with foreign keys on, DROP TABLE would delete every job's params,
// items and runs. Every child row must survive, and every job keep its data.
func TestMigration0089KeepsEveryJobsChildren(t *testing.T) {
	s, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	ctx := context.Background()
	applyMigrationsThrough(t, s, "0088_provider_connector.sql")

	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := s.DB.ExecContext(ctx, q, args...); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	now := time.Date(2026, 11, 20, 10, 0, 0, 0, time.UTC)
	exec(`INSERT INTO projects (id, name, created_at) VALUES ('p', 'p', ?)`, now)
	exec(`INSERT INTO jobs (id, project_id, type, status, started_at, updated_at, branch, base_sha, tokens_in)
	      VALUES ('j1', 'p', 'fix_findings', 'succeeded', ?, ?, 'cd/fix/j1', 'abc', 42)`, now, now)
	exec(`INSERT INTO job_params (job_id, key, value) VALUES ('j1', 'scratchpad', 'sp1'), ('j1', 'round_limit', '3')`)
	exec(`INSERT INTO job_phases (id, job_id, ordinal, phase, started_at) VALUES ('ph1', 'j1', 0, 'reading', ?)`, now)

	if err := s.Migrate(ctx); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	var params, phases int
	s.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM job_params WHERE job_id = 'j1'`).Scan(&params)
	s.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM job_phases WHERE job_id = 'j1'`).Scan(&phases)
	if params != 2 || phases != 1 {
		t.Fatalf("children lost: %d params, %d phases", params, phases)
	}
	j, err := s.GetJob(ctx, "j1")
	if err != nil || j == nil {
		t.Fatalf("job: %v %v", j, err)
	}
	if j.Branch != "cd/fix/j1" || j.BaseSHA != "abc" || j.TokensIn != 42 || j.Priority != domain.PriorityMedium || j.QueuedAt != nil {
		t.Errorf("job after migration = %+v", j)
	}
	// Foreign keys are back on: a child of a missing job is refused.
	if _, err := s.DB.ExecContext(ctx, `INSERT INTO job_params (job_id, key, value) VALUES ('nope', 'k', 'v')`); err == nil {
		t.Error("foreign keys were left off after the migration")
	}
}
