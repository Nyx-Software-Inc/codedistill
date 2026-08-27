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

-- 0030: rename use case statuses to the product vocabulary chosen
-- 2026-06-12 (v0.10.14): proposed→open, implemented→completed,
-- abandoned→rejected, plus a new 'approved' status (selected for
-- implementation) that needs no data change. The status CHECK was
-- dropped in 0021; internal/domain/lifecycle.go is the validator.

UPDATE use_case_items SET status = 'open'      WHERE status = 'proposed';
UPDATE use_case_items SET status = 'completed' WHERE status = 'implemented';
UPDATE use_case_items SET status = 'rejected'  WHERE status = 'abandoned';
