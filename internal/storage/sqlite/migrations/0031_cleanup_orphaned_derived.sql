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

-- 0031: one-time cleanup of orphaned derived items left by
-- re-classification (CodeDestill_imports bug #1).
--
-- Before v0.10.20, re-classifying a scratchpad item (e.g. todo → use
-- case) minted a fresh derived row and re-pointed the scratchpad item's
-- derived_item_id at it, but never deleted the previous derived row.
-- The stale row kept its source_item_id, so the header counts (which
-- join derived rows to the scratchpad via source_item_id) double-counted
-- that source item. The going-forward fix lives in
-- agent.commitClassified; this migration removes the rows already
-- orphaned.
--
-- A row is a superseded orphan when its source scratchpad item exists
-- and points its derived_item_id at a DIFFERENT row. Rows whose source
-- is NULL (hand-created) or whose source still points at them are kept.

DELETE FROM todo_items WHERE id IN (
  SELECT t.id FROM todo_items t
  JOIN scratchpad_items si ON t.source_item_id = si.id
  WHERE si.derived_item_id IS NOT NULL AND si.derived_item_id <> t.id
);

DELETE FROM bug_items WHERE id IN (
  SELECT b.id FROM bug_items b
  JOIN scratchpad_items si ON b.source_item_id = si.id
  WHERE si.derived_item_id IS NOT NULL AND si.derived_item_id <> b.id
);

DELETE FROM knowledge_entries WHERE id IN (
  SELECT k.id FROM knowledge_entries k
  JOIN scratchpad_items si ON k.source_item_id = si.id
  WHERE si.derived_item_id IS NOT NULL AND si.derived_item_id <> k.id
);

DELETE FROM use_case_items WHERE id IN (
  SELECT u.id FROM use_case_items u
  JOIN scratchpad_items si ON u.source_item_id = si.id
  WHERE si.derived_item_id IS NOT NULL AND si.derived_item_id <> u.id
);
