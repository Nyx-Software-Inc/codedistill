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

-- Re-embed user-story scratchpad items with the boilerplate stripped.
-- NormalizeItemText now removes the "As a <role>, I would like to ..." scaffolding
-- before embedding so two unrelated stories no longer score as near-duplicates on
-- the shared template alone (backlog bug 7123ac1a). Nulling embedded_at requeues
-- these rows for the background embed sweep; the stale vector stays usable until
-- the new one lands. Only rows that already have an embedding are touched.
UPDATE scratchpad_items
SET embedded_at = NULL
WHERE embedding IS NOT NULL
  AND (lower(content) LIKE 'as a %'
    OR lower(content) LIKE 'as an %'
    OR lower(content) LIKE 'as the %');
