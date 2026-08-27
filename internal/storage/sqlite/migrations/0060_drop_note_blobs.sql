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

-- 0060: retire the single-blob note columns (item-editing redesign, final
-- step). scratchpad_items.annotations, todo_items.notes, and bug_items.notes
-- are superseded by the append-only activity log (notes) and the group_notes
-- table (group frames). 0055 converted their contents once at v0.19.0; the
-- catch-up pass below converts anything edited in the columns since —
-- guarded by NOT EXISTS on an identical note body, so nothing duplicates and
-- nothing is lost. Protected by the automatic pre-migration snapshot.

-- Catch-up: source-item annotations edited since 0055 (groups excluded — their
-- note was copied to group_notes in 0056 and dual-written since).
INSERT INTO item_events (id, owner_type, owner_id, kind, summary, body, source, created_at)
SELECT lower(hex(randomblob(8))), 'scratchpad_item', si.id, 'note',
       substr(trim(si.annotations), 1, 120), si.annotations, 'ui', si.updated_at
FROM scratchpad_items si
WHERE si.annotations IS NOT NULL AND trim(si.annotations) <> '' AND si.content_type <> 'group'
  AND NOT EXISTS (SELECT 1 FROM item_events e
                   WHERE e.owner_type = 'scratchpad_item' AND e.owner_id = si.id
                     AND e.kind = 'note' AND e.body = si.annotations);

INSERT INTO item_events (id, owner_type, owner_id, kind, summary, body, source, created_at)
SELECT lower(hex(randomblob(8))), 'todo_item', t.id, 'note',
       substr(trim(t.notes), 1, 120), t.notes, 'ui', t.created_at
FROM todo_items t
WHERE t.notes IS NOT NULL AND trim(t.notes) <> ''
  AND NOT EXISTS (SELECT 1 FROM item_events e
                   WHERE e.owner_type = 'todo_item' AND e.owner_id = t.id
                     AND e.kind = 'note' AND e.body = t.notes);

INSERT INTO item_events (id, owner_type, owner_id, kind, summary, body, source, created_at)
SELECT lower(hex(randomblob(8))), 'bug_item', b.id, 'note',
       substr(trim(b.notes), 1, 120), b.notes, 'ui', b.created_at
FROM bug_items b
WHERE b.notes IS NOT NULL AND trim(b.notes) <> ''
  AND NOT EXISTS (SELECT 1 FROM item_events e
                   WHERE e.owner_type = 'bug_item' AND e.owner_id = b.id
                     AND e.kind = 'note' AND e.body = b.notes);

-- The drops. Group frames' notes live on in group_notes (read through by the
-- store); everything else lives on in item_events.
ALTER TABLE scratchpad_items DROP COLUMN annotations;
ALTER TABLE todo_items DROP COLUMN notes;
ALTER TABLE bug_items DROP COLUMN notes;
