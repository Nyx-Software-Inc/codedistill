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

// One ranking order across item kinds that do not share a field.
//
// Todos and use cases carry Priority (high|medium|low|none). Bugs carry
// Severity (critical|major|minor|trivial) and deliberately do NOT get a second
// axis: two rankings on one entity would leave every consumer inventing its own
// answer to "is a trivial bug at high priority above a critical bug at low
// priority?", and users guessing which one drives a sort. Severity is mapped
// onto the same order instead (Rich's call, 2026-08-31).
//
// KB entries have no ranking and never will — they are reference material, not
// work.

// Priorities is the canonical order, most important first. Also the vocabulary
// for the CHECK constraints on todo_items.priority and use_case_items.priority.
var Priorities = []string{"high", "medium", "low", "none"}

// priorityRanks and severityRanks share a scale so a mixed list sorts sensibly.
// Lower is more important; the gap at 4 is "ranked but lowest", and Unranked
// sits beyond every real value so unrankable rows land last without a special
// case at each call site.
var priorityRanks = map[string]int{"high": 1, "medium": 2, "low": 3, "none": 4}

var severityRanks = map[string]int{"critical": 1, "major": 2, "minor": 3, "trivial": 4}

// Unranked is the rank of anything with no ranking: KB entries, groups with no
// open members, and any unrecognised value. Sorts after every ranked item.
const Unranked = 99

// PriorityRank maps a todo/use-case priority to its position. Unknown or empty
// values are Unranked rather than silently "none" — "we don't know" and "the
// user chose none" are different claims.
func PriorityRank(priority string) int {
	if r, ok := priorityRanks[priority]; ok {
		return r
	}
	return Unranked
}

// SeverityRank maps a bug severity onto the SAME scale as PriorityRank, so a
// list mixing todos, bugs and use cases orders by one comparable number.
func SeverityRank(severity string) int {
	if r, ok := severityRanks[severity]; ok {
		return r
	}
	return Unranked
}

// RankFor returns the comparable rank for an item of the given kind. kind uses
// the values the UI already shows (see ListView's kindOf): "todo", "bug",
// "use case"/"use_case", "kb".
//
// The caller passes whichever field its kind carries; this is the single place
// that knows bugs rank by severity and everything else by priority, so a sort
// does not have to.
func RankFor(kind, priorityOrSeverity string) int {
	switch kind {
	case "bug":
		return SeverityRank(priorityOrSeverity)
	case "kb":
		return Unranked // reference material is not work; it has no priority
	default:
		return PriorityRank(priorityOrSeverity)
	}
}

// ValidPriority reports whether s is an accepted priority. Empty is valid and
// means "unset" — the column defaults to 'none'. Mirrors ValidUseCaseStatus.
func ValidPriority(s string) bool {
	if s == "" {
		return true
	}
	_, ok := priorityRanks[s]
	return ok
}
