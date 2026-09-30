-- =============================================================================
--  Copyright (c) 2026 Nyx Software, Inc.  All rights reserved.
--  CodeDistill
--  Property of Nyx Software, Inc., provided under a dual license: the GNU Affero General
--  Public License v3.0 (see the LICENSE file) and, separately, a commercial
--  license available from Nyx Software, Inc. Use outside the terms of one of those
--  licenses is prohibited.
--  SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-Nyx-Commercial
-- =============================================================================

-- "Work out the order" as a workflow anyone can run.
--
-- Its own workflow rather than a step inside decomposition, because the items
-- need not have come from a document. Someone who read the specification and
-- typed forty-nine use cases in by hand has exactly the same problem as someone
-- who decomposed it, and no reason to be told the answer lives inside a feature
-- they did not use.
--
-- NOT resumable: it is a single model call over the whole set. There is no
-- checkpoint to resume from, and offering a Pause that silently means Cancel is
-- worse than offering none.

INSERT INTO workflows (id, name, description, builtin, resumable, enabled, created_at, updated_at) VALUES
  ('sequence', 'Work out the order',
   'Read the work on a scratchpad and propose what has to happen before what. Every edge cites its reason, and nothing is applied until you agree with it.',
   1, 0, 1, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP);

INSERT INTO workflow_steps (workflow_id, ordinal, worker_type, label, needs_context) VALUES
  ('sequence', 0, 'decomposer',
   'Reads every item at once and proposes an ordering', 16384);

INSERT INTO workflow_params
  (workflow_id, ordinal, key, label, help, type, required, default_val, multiple) VALUES
  ('sequence', 0, 'scratchpad', 'Order the work on',
   'Every use case, todo and bug on this scratchpad is considered together — a bug blocking a use case is exactly the kind of ordering a per-kind pass would miss.',
   'scratchpad', 1, '', 0);
