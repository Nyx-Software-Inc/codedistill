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

-- v0.8.1-dev — completion metadata on todos + bugs.
--
-- Generalizes the pattern use_case_items has had since 0009/0011:
-- when status flips to a terminal value, record when and what commit
-- the user was on. Underpins the upcoming project dashboard (priority
-- #4 in BACKLOG) which slices throughput per branch via commit_sha.
--
-- Three new columns per table:
--   completed_at — TIMESTAMP, null = not yet completed (or reopened)
--   commit_sha   — TEXT, auto-filled from project HEAD on flip;
--                  user-editable from the detail modal
--   commit_tag   — TEXT, optional tag the user adds when shipping
--                  ("v1.2.3", "release-2026q2", etc.)
--
-- Transition rules (enforced in the API handler, not in SQL):
--   - todos: status incomplete → complete         → set the three fields
--            status complete   → incomplete       → clear all three
--   - bugs:  status open/investigating/in-progress
--            → fixed/verified/closed              → set the three fields
--            terminal → non-terminal              → clear all three
--
-- No FK / CHECK constraints — commit_sha is just text. Validation
-- (looks like a SHA?) belongs in the API layer if we want it.

-- todo_items already carries completed_at from migration 0001; we only
-- add the commit metadata here. bug_items has no completion fields at
-- all yet, so it gets all three.
ALTER TABLE todo_items ADD COLUMN commit_sha   TEXT;
ALTER TABLE todo_items ADD COLUMN commit_tag   TEXT;

ALTER TABLE bug_items  ADD COLUMN completed_at TIMESTAMP;
ALTER TABLE bug_items  ADD COLUMN commit_sha   TEXT;
ALTER TABLE bug_items  ADD COLUMN commit_tag   TEXT;
