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

-- Name model (multi-user auth). display_name is the editable vanity name;
-- canonical_name is the IdP-provided REAL name, immutable by the user. Together
-- with email (the unique key) it's what hover shows, so 'Captain Code' always
-- resolves to 'John Smith · john@acme.com'. See docs/design/auth.md.
ALTER TABLE users ADD COLUMN canonical_name TEXT NOT NULL DEFAULT '';
