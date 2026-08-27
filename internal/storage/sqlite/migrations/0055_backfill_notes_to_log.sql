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

-- 0055: backfill existing free-form notes into the activity log (item-editing
-- redesign, slice 5). The single-blob note fields — scratchpad_items.annotations
-- and todo_items/bug_items.notes — are superseded by the per-item activity log;
-- this converts each non-empty one into a `note` event so nothing is lost when a
-- later migration retires those columns. ADDITIVE: the source columns are left
-- in place (and the pre-migration snapshot protects the whole thing). Runs once
-- (the migration system guarantees it), so no duplicate notes.

-- Source-item annotations → a note on the scratchpad item (the Log tab merges
-- source + derived events). Groups are skipped — their annotation is a group
-- note, and groups have no activity log.
INSERT INTO item_events (id, owner_type, owner_id, kind, summary, body, source, created_at)
SELECT lower(hex(randomblob(8))), 'scratchpad_item', id, 'note',
       substr(trim(annotations), 1, 120), annotations, 'ui', updated_at
FROM scratchpad_items
WHERE annotations IS NOT NULL AND trim(annotations) <> '' AND content_type <> 'group';

-- Todo notes → a note on the todo (todo_items has no updated_at; use created_at).
INSERT INTO item_events (id, owner_type, owner_id, kind, summary, body, source, created_at)
SELECT lower(hex(randomblob(8))), 'todo_item', id, 'note',
       substr(trim(notes), 1, 120), notes, 'ui', created_at
FROM todo_items
WHERE notes IS NOT NULL AND trim(notes) <> '';

-- Bug notes → a note on the bug.
INSERT INTO item_events (id, owner_type, owner_id, kind, summary, body, source, created_at)
SELECT lower(hex(randomblob(8))), 'bug_item', id, 'note',
       substr(trim(notes), 1, 120), notes, 'ui', created_at
FROM bug_items
WHERE notes IS NOT NULL AND trim(notes) <> '';
