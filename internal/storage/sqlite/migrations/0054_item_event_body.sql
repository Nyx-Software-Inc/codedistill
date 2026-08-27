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

-- 0054: activity log (item-editing redesign, slice 1). item_events gains a
-- markdown `body` for note-kind entries — a human/agent note carries full
-- markdown here; one-line system events (status-changed, etc.) keep using
-- `summary` and leave `body` empty. Additive, no data touched.
ALTER TABLE item_events ADD COLUMN body TEXT NOT NULL DEFAULT '';
