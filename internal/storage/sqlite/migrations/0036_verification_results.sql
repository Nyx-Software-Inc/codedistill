-- =============================================================================
--  Copyright (c) 2026 Nyx Software, Inc.  All rights reserved.
--
--  CodeDistill
--
--  Property of Nyx Software, Inc., provided under a dual license: the GNU Affero General
--  Public License v3.0 (see the LICENSE file) and, separately, a commercial
--  license available from Nyx Software, Inc. Use outside the terms of one of those
--  licenses is prohibited.
--
--  SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-Nyx-Commercial
-- =============================================================================

-- v0.11.x — verification results (glass-box Phase 3, slice 1).
--
-- The verification portfolio's persistent record: each row is one check the
-- box ran (or will run) against an item, and its verdict. This is the trust
-- signal the Phase-4 trust dial later consumes. Slice 1 produces only the
-- deterministic layer (box runs the project's configured test command in an
-- isolated worktree at the item's recorded commit); the layer/kind/produced_by
-- columns reserve the seams for the AI-review and human layers (plan #6).
--
-- owner_type/owner_id mirror code_anchors + acceptance_criteria addressing so
-- the whole glass-box model stays polymorphic (no FK; cascade handled in Go).
-- Results attach to a work item now; the same addressing lets a later slice
-- attach a result to a single acceptance_criterion once criteria map to tests.
--
-- layer:       deterministic | ai-review | human  (reliability tier, plan #6).
-- kind:        test | lint | build | check        (what the check is).
-- verdict:     running | pass | fail | error.
--              running = async run in flight; pass = command exited 0;
--              fail = command ran and reported failure (non-zero exit);
--              error = harness couldn't get a clean verdict (checkout failed,
--              timeout, command not found) — distinct from a real test failure.
-- commit_sha:  the commit the check ran against (the item's recorded commit).
-- produced_by: box | agent | ai | human (who/what generated the verdict).
CREATE TABLE verification_results (
    id          TEXT PRIMARY KEY,
    owner_type  TEXT NOT NULL,
    owner_id    TEXT NOT NULL,
    layer       TEXT NOT NULL DEFAULT 'deterministic',
    kind        TEXT NOT NULL DEFAULT 'test',
    check_name  TEXT NOT NULL,
    verdict     TEXT NOT NULL DEFAULT 'running',
    commit_sha  TEXT,
    exit_code   INTEGER,
    summary     TEXT NOT NULL DEFAULT '',
    output      TEXT NOT NULL DEFAULT '',
    duration_ms INTEGER NOT NULL DEFAULT 0,
    produced_by TEXT NOT NULL DEFAULT 'box',
    created_at  TIMESTAMP NOT NULL,
    updated_at  TIMESTAMP NOT NULL
);

CREATE INDEX idx_vr_owner ON verification_results(owner_type, owner_id, created_at DESC);
