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

-- Matches panel needs the commit author to help triage suggestions
-- (especially in team/multi-user mode). Backfill leaves existing
-- rows with NULL; the UI hides the author when empty rather than
-- showing "Unknown". New matches written by the matcher worker
-- populate this from CommitWithFiles.Author.
--
-- We don't add a separate commit_body column: commit_message
-- already holds the full message (subject + blank line + body),
-- as produced by go-git's c.Message. The frontend splits it at
-- the first newline for display.

ALTER TABLE implementation_matches ADD COLUMN commit_author TEXT NOT NULL DEFAULT '';
