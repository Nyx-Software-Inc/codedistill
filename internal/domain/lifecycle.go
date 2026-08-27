// =============================================================================
//  Copyright (c) 2026 Nyx Software, Inc.  All rights reserved.
//
//  CodeDistill
//
//  Property of Nyx Software, Inc., provided under a dual license: the GNU Affero General
//  Public License v3.0 (see the LICENSE file) and, separately, a commercial
//  license available from Nyx Software, Inc. Use outside the terms of one of those
//  licenses is prohibited.
//
//  SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-Nyx-Commercial
// =============================================================================

package domain

// Lifecycle predicates and validators per item type. Centralized so the
// API handlers, MCP tools, and frontend filters all share one definition
// of "done" and one set of allowed status values per type.

// ===== TODO =====

const (
	TodoStatusIncomplete = "incomplete"
	TodoStatusInProgress = "in_progress"
	TodoStatusComplete   = "complete"
	TodoStatusAbandoned  = "abandoned"
)

// IsTodoDone reports whether the status represents a terminal state
// that the default "open" filter should hide.
func IsTodoDone(status string) bool {
	switch status {
	case TodoStatusComplete, TodoStatusAbandoned:
		return true
	}
	return false
}

// ValidTodoStatus reports whether status is one of the four allowed
// values. Used by the API handler to reject malformed PATCH bodies.
func ValidTodoStatus(status string) bool {
	switch status {
	case TodoStatusIncomplete, TodoStatusInProgress, TodoStatusComplete, TodoStatusAbandoned:
		return true
	}
	return false
}

// ===== BUG =====
//
// Bug uses the legacy hyphen spelling for in-progress (migration 0001).
// The terminal set was widened in migration 0020 to include not_a_bug,
// wont_fix, duplicate.

const (
	BugStatusOpen          = "open"
	BugStatusInvestigating = "investigating"
	BugStatusInProgress    = "in-progress"
	BugStatusFixed         = "fixed"
	BugStatusVerified      = "verified"
	BugStatusClosed        = "closed"
	BugStatusNotABug       = "not_a_bug"
	BugStatusWontFix       = "wont_fix"
	BugStatusDuplicate     = "duplicate"
)

func IsBugDone(status string) bool {
	switch status {
	case BugStatusFixed, BugStatusVerified, BugStatusClosed,
		BugStatusNotABug, BugStatusWontFix, BugStatusDuplicate:
		return true
	}
	return false
}

func ValidBugStatus(status string) bool {
	switch status {
	case BugStatusOpen, BugStatusInvestigating, BugStatusInProgress,
		BugStatusFixed, BugStatusVerified, BugStatusClosed,
		BugStatusNotABug, BugStatusWontFix, BugStatusDuplicate:
		return true
	}
	return false
}

// ===== USE_CASE =====

// Renamed + widened 2026-06-12 (v0.10.14, Rich's vocabulary): the old
// proposed/implemented/abandoned set became open/completed/rejected and
// gained "approved" (selected for implementation, between open and
// in_progress). Migration 0030 renamed existing rows.
const (
	UseCaseStatusOpen       = "open"
	UseCaseStatusApproved   = "approved"
	UseCaseStatusInProgress = "in_progress"
	UseCaseStatusCompleted  = "completed"
	UseCaseStatusRejected   = "rejected"
)

func IsUseCaseDone(status string) bool {
	switch status {
	case UseCaseStatusCompleted, UseCaseStatusRejected:
		return true
	}
	return false
}

func ValidUseCaseStatus(status string) bool {
	switch status {
	case UseCaseStatusOpen, UseCaseStatusApproved, UseCaseStatusInProgress,
		UseCaseStatusCompleted, UseCaseStatusRejected:
		return true
	}
	return false
}

// WasImplemented reports whether a terminal status means the item was positively
// IMPLEMENTED — work was delivered, so a code location SHOULD exist — as opposed
// to abandoned / rejected / wont-fix / not-a-bug / duplicate, which legitimately
// have no code. ownerType is the CodeAnchor owner tag ("todo_item" | "bug_item"
// | "use_case_item"). Used to flag "done, but no code location recorded".
func WasImplemented(ownerType, status string) bool {
	switch ownerType {
	case "todo_item":
		return status == TodoStatusComplete
	case "bug_item":
		switch status {
		case BugStatusFixed, BugStatusVerified, BugStatusClosed:
			return true
		}
	case "use_case_item":
		return status == UseCaseStatusCompleted
	}
	return false
}

// ===== KNOWLEDGE =====
//
// KB has no in-progress concept — entries are reference material, not
// units of work. The status set is intentionally minimal.

const (
	KnowledgeStatusActive     = "active"
	KnowledgeStatusDeprecated = "deprecated"
)

func IsKnowledgeDone(status string) bool {
	return status == KnowledgeStatusDeprecated
}

func ValidKnowledgeStatus(status string) bool {
	switch status {
	case KnowledgeStatusActive, KnowledgeStatusDeprecated:
		return true
	}
	return false
}
