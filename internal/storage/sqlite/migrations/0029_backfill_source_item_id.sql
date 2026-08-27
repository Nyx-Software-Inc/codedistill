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

-- 0029: backfill derived items' source_item_id from the scratchpad
-- item's forward pointer (Backlog bug #6, v0.10.6).
--
-- Early-epoch derived rows (mid-May 2026) were created without
-- source_item_id even though the originating scratchpad item recorded
-- derived_item_id. Every scratchpad-scoped view joins through
-- source_item_id (header banner counts, drawer panes, card fading), so
-- those items were invisible at scratchpad scope and only reachable
-- from project-level lists. The deriver sets the back-pointer
-- correctly today; this repairs the historical rows by reversing the
-- forward pointer. Rows with no surviving scratchpad item (true
-- orphans) stay NULL — project-level visibility only, which is
-- correct.

UPDATE todo_items SET source_item_id =
    (SELECT i.id FROM scratchpad_items i WHERE i.derived_item_id = todo_items.id)
WHERE source_item_id IS NULL
  AND EXISTS (SELECT 1 FROM scratchpad_items i WHERE i.derived_item_id = todo_items.id);

UPDATE bug_items SET source_item_id =
    (SELECT i.id FROM scratchpad_items i WHERE i.derived_item_id = bug_items.id)
WHERE source_item_id IS NULL
  AND EXISTS (SELECT 1 FROM scratchpad_items i WHERE i.derived_item_id = bug_items.id);

UPDATE knowledge_entries SET source_item_id =
    (SELECT i.id FROM scratchpad_items i WHERE i.derived_item_id = knowledge_entries.id)
WHERE source_item_id IS NULL
  AND EXISTS (SELECT 1 FROM scratchpad_items i WHERE i.derived_item_id = knowledge_entries.id);

UPDATE use_case_items SET source_item_id =
    (SELECT i.id FROM scratchpad_items i WHERE i.derived_item_id = use_case_items.id)
WHERE source_item_id IS NULL
  AND EXISTS (SELECT 1 FROM scratchpad_items i WHERE i.derived_item_id = use_case_items.id);
