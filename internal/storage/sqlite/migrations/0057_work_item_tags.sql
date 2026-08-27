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

-- 0057: tags move onto the work record (item-editing canvas rework, slice C3).
-- Each derived type gets a `tags` JSON column (same format as
-- scratchpad_items.tags), seeded from the source item's tags. Additive.
ALTER TABLE todo_items ADD COLUMN tags TEXT NOT NULL DEFAULT '[]';
ALTER TABLE bug_items ADD COLUMN tags TEXT NOT NULL DEFAULT '[]';
ALTER TABLE use_case_items ADD COLUMN tags TEXT NOT NULL DEFAULT '[]';
ALTER TABLE knowledge_entries ADD COLUMN tags TEXT NOT NULL DEFAULT '[]';

-- Backfill: copy the source item's tags JSON onto the derived work record.
UPDATE todo_items SET tags = COALESCE(
    (SELECT si.tags FROM scratchpad_items si WHERE si.id = todo_items.source_item_id), '[]');
UPDATE bug_items SET tags = COALESCE(
    (SELECT si.tags FROM scratchpad_items si WHERE si.id = bug_items.source_item_id), '[]');
UPDATE use_case_items SET tags = COALESCE(
    (SELECT si.tags FROM scratchpad_items si WHERE si.id = use_case_items.source_item_id), '[]');
UPDATE knowledge_entries SET tags = COALESCE(
    (SELECT si.tags FROM scratchpad_items si WHERE si.id = knowledge_entries.source_item_id), '[]');
