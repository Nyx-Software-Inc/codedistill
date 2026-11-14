-- =============================================================================
--  Copyright (c) 2026 Nyx Software, Inc.  All rights reserved.
--  CodeDistill
--  Property of Nyx Software, Inc., provided under a dual license: the GNU Affero General
--  Public License v3.0 (see the LICENSE file) and, separately, a commercial
--  license available from Nyx Software, Inc. Use outside the terms of one of those
--  licenses is prohibited.
--  SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-Nyx-Commercial
-- =============================================================================

-- How an agent role on this model is run (docs/design/agent-loop.md):
-- 'auto' (an Anthropic model runs through Claude Code when it is installed
-- and signed in, otherwise on CodeDistill's own loop), 'builtin' (always
-- CodeDistill's own loop, billed to the API key) or 'claude-code' (always
-- Claude Code, on the customer's subscription). Open text, like protocol:
-- what is valid is a property of the Go code.
ALTER TABLE model_providers ADD COLUMN connector TEXT NOT NULL DEFAULT 'auto';
