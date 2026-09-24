-- =============================================================================
--  Copyright (c) 2026 Nyx Software, Inc.  All rights reserved.
--  CodeDistill
--  Property of Nyx Software, Inc., provided under a dual license: the GNU Affero General
--  Public License v3.0 (see the LICENSE file) and, separately, a commercial
--  license available from Nyx Software, Inc. Use outside the terms of one of those
--  licenses is prohibited.
--  SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-Nyx-Commercial
-- =============================================================================

-- model_roles becomes workflow_workers.
--
-- "Role" already meant two other things here: a member's role (owner, admin,
-- member) and a sentence's role in decomposition (introduce, elaborate,
-- retract, meta). A third meaning was one too many.
--
-- The vocabulary that fits is the product's own: a WORKFLOW is a kind of job —
-- decompose a document, draft an architecture — and it runs WORKERS. Each
-- worker has a type, and each type is served by a configured model. Today
-- every workflow has exactly one worker; the epistemic pair will have two, and
-- an agent sweep will have n, which is why the noun needs room to be counted.
--
-- Renamed rather than migrated in place: nothing is released, and the column
-- rename is free now and a compatibility shim later.

ALTER TABLE model_roles RENAME TO workflow_workers;
ALTER TABLE workflow_workers RENAME COLUMN role TO worker_type;
