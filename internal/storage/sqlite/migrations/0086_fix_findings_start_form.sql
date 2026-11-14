-- =============================================================================
--  Copyright (c) 2026 Nyx Software, Inc.  All rights reserved.
--  CodeDistill
--  Property of Nyx Software, Inc., provided under a dual license: the GNU Affero General
--  Public License v3.0 (see the LICENSE file) and, separately, a commercial
--  license available from Nyx Software, Inc. Use outside the terms of one of those
--  licenses is prohibited.
--  SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-Nyx-Commercial
-- =============================================================================

-- The start form's say over how much a job may spend
-- (docs/design/fix-scanner-findings.md, "Starting it").
UPDATE workflow_params
   SET label = 'Rounds per item',
       help = 'How many times the fixer may answer the reviewers before an item is handed to you: a number, or "unbounded". Unbounded stops at the ceiling (the conductor.round_ceiling setting, 100 unless changed). A job also stops on its own when it is going in circles.'
 WHERE workflow_id = 'fix_findings' AND key = 'round_limit';

INSERT INTO workflow_params
  (workflow_id, ordinal, key, label, help, type, required, default_val, options, multiple) VALUES
  ('fix_findings', 4, 'token_budget', 'Token budget',
   'Optional. The most tokens the job''s agents may use between them (input and output; cached reads are not counted). When it is used up the job stops and the item in hand is handed to you. Empty: no budget.',
   'text', 0, '', '', 0),
  ('fix_findings', 5, 'review_built', 'Reviewers also check the finished code',
   'The reviewers always check the approach before anything is built. Ticked, they also review the finished change, which catches an approach built wrongly and costs another review per round.',
   'bool', 0, 'false', '', 0);

-- The description still told the first version's story (a challenger after
-- the fix); say what a run now does.
UPDATE workflows
   SET description = 'Agents fix each scanner finding on one branch, highest severity first. A fixer proposes its approach and two reviewers (security and code) must agree before anything is built; a TestBuilder writes the tests while the fix is built. CodeDistill checks every fix itself: the finding is gone, nothing new appeared, the tests pass, and the new tests failed before the fix.'
 WHERE id = 'fix_findings';
