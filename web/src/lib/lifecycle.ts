/* =============================================================================
 *  Copyright (c) 2026 Nyx Software, Inc.  All rights reserved.
 *
 *  CodeDistill
 *
 *  Property of Nyx Software, Inc., provided under a dual license: the GNU Affero General
 *  Public License v3.0 (see the LICENSE file) and, separately, a commercial
 *  license available from Nyx Software, Inc. Use outside the terms of one of those
 *  licenses is prohibited.
 *
 *  SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-Nyx-Commercial
 * ============================================================================= */

// Per-type "is this status done?" predicates + the in-progress check.
// Mirrors codedistill/internal/domain/lifecycle.go — keep in sync if
// the Go-side status sets ever change.

import type { TodoStatus, BugStatus, UseCaseStatus, KnowledgeStatus } from './types';

export function isTodoDone(s: TodoStatus): boolean {
  return s === 'complete' || s === 'abandoned';
}
export function isTodoInProgress(s: TodoStatus): boolean {
  return s === 'in_progress';
}

export function isBugDone(s: BugStatus): boolean {
  switch (s) {
    case 'fixed':
    case 'verified':
    case 'closed':
    case 'not_a_bug':
    case 'wont_fix':
    case 'duplicate':
      return true;
    default:
      return false;
  }
}
export function isBugInProgress(s: BugStatus): boolean {
  // Bug uses the legacy hyphen spelling.
  return s === 'in-progress';
}

export function isUseCaseDone(s: UseCaseStatus): boolean {
  return s === 'completed' || s === 'rejected';
}
export function isUseCaseInProgress(s: UseCaseStatus): boolean {
  return s === 'in_progress';
}

export function isKnowledgeDone(s: KnowledgeStatus): boolean {
  return s === 'deprecated';
}

// User-facing label for terminal statuses ("Completed" / "Fixed" /
// "Implemented" / "Deprecated"). Used by the Done section divider
// row to tell the user *why* an item is done.
export function todoTerminalLabel(s: TodoStatus): string {
  if (s === 'complete') return 'Completed';
  if (s === 'abandoned') return 'Abandoned';
  return '';
}

export function bugTerminalLabel(s: BugStatus): string {
  switch (s) {
    case 'fixed': return 'Fixed';
    case 'verified': return 'Verified';
    case 'closed': return 'Closed';
    case 'not_a_bug': return 'Not a bug';
    case 'wont_fix': return "Won't fix";
    case 'duplicate': return 'Duplicate';
    default: return '';
  }
}

export function useCaseTerminalLabel(s: UseCaseStatus): string {
  if (s === 'completed') return 'Completed';
  if (s === 'rejected') return 'Rejected';
  return '';
}

export function knowledgeTerminalLabel(s: KnowledgeStatus): string {
  return s === 'deprecated' ? 'Deprecated' : '';
}

/** Kind-agnostic terminal check, for callers that hold a `kind` string from
 *  derivedStatus rather than a typed item. Used by the list sort: a group ranks
 *  by its most important OPEN member, so closed work must not decide where a
 *  folder sits. */
export function isDoneForKind(kind: string, status: string): boolean {
  switch (kind) {
    case 'todo':
      return isTodoDone(status as TodoStatus);
    case 'bug':
      return isBugDone(status as BugStatus);
    case 'use case':
    case 'use_case':
      return isUseCaseDone(status as UseCaseStatus);
    case 'kb':
      return isKnowledgeDone(status as KnowledgeStatus);
    default:
      return false;
  }
}
