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

// Package nlmeta extracts priority (todos) and severity (bugs) from the
// explicit signals in captured text — the deterministic sibling of nldate
// (capture-time extraction, Backlog #11). Fire-and-forget capture only stays
// trustworthy if the system fills in what the text SAYS and never guesses at
// what it means, so the rules are deliberately strict:
//
//   - Only explicit signal words/phrases trigger ("URGENT", "p1", "no rush",
//     "typo"…). Ordinary bug phrasing ("doesn't work") is NOT a signal.
//   - A negated signal ("not urgent") never fires as its positive; the common
//     negated forms are their own low-priority signals instead.
//   - Signals for two different levels in one capture → ambiguous → blank.
//     Wrong pre-filled metadata is worse than empty metadata.
//
// Everything here is user-overridable on the work record afterward.
package nlmeta

import (
	"regexp"
	"strings"
)

// signal is one explicit phrase mapped to a level. Phrases are matched
// case-insensitively on word boundaries against the raw capture text.
type signal struct {
	re    *regexp.Regexp
	level string
}

func sig(level, phrase string) signal {
	return signal{level: level, re: regexp.MustCompile(`(?i)\b` + phrase + `\b`)}
}

// negation guards a positive signal: "not urgent" / "isn't critical" must not
// read as urgent/critical. (Negated forms that ARE meaningful — "not urgent",
// "no rush" — appear as explicit low signals below and match first.)
var negation = regexp.MustCompile(`(?i)(\bnot|n't|\bnever|\bisn'?t)\s*$`)

var prioritySignals = []signal{
	// low first, so phrase-level negatives ("not urgent") win over the bare
	// positive word they contain.
	sig("low", `not\s+urgent`), sig("low", `no\s+rush`), sig("low", `no\s+hurry`),
	sig("low", `low\s+priority`), sig("low", `nice\s+to\s+have`),
	sig("low", `whenever`), sig("low", `someday`), sig("low", `eventually`),
	sig("low", `back\s*burner`), sig("low", `p[34]`),

	sig("medium", `medium\s+priority`), sig("medium", `p2`),

	sig("high", `urgent(?:ly)?`), sig("high", `asap`),
	sig("high", `high\s+priority`), sig("high", `top\s+priority`),
	sig("high", `p[01]`), sig("high", `blocker`), sig("high", `critical`),
	sig("high", `drop\s+everything`), sig("high", `immediately`),
	sig("high", `right\s+away`),
}

var severitySignals = []signal{
	sig("trivial", `trivial`), sig("trivial", `cosmetic`),
	sig("trivial", `typo`), sig("trivial", `nit(?:pick)?`),

	sig("minor", `minor`),

	sig("major", `major`), sig("major", `severe`),

	sig("critical", `critical`), sig("critical", `crash(?:es|ing|ed)?`),
	sig("critical", `data\s+loss`), sig("critical", `security\s+(?:vulnerability|hole|bug|issue|flaw)`),
	sig("critical", `outage`), sig("critical", `blocker`),
	sig("critical", `urgent(?:ly)?`), sig("critical", `asap`),
}

// Priority returns the todo priority ("high" | "medium" | "low") explicitly
// signaled by text, or ok=false when the text carries no signal — or carries
// conflicting ones.
func Priority(text string) (string, bool) { return extract(text, prioritySignals) }

// Severity returns the bug severity ("critical" | "major" | "minor" |
// "trivial") explicitly signaled by text, with the same blank-over-guess rules.
func Severity(text string) (string, bool) { return extract(text, severitySignals) }

func extract(text string, signals []signal) (string, bool) {
	levels := map[string]bool{}
	src := text
	for _, s := range signals {
		locs := s.re.FindAllStringIndex(src, -1)
		matched := false
		for _, loc := range locs {
			if negation.MatchString(src[:loc[0]]) {
				continue // "not <signal>": suppressed
			}
			matched = true
			// Blank the span so a later, shorter pattern can't re-match inside
			// an already-claimed phrase (e.g. "urgent" inside "not urgent").
			src = src[:loc[0]] + strings.Repeat(" ", loc[1]-loc[0]) + src[loc[1]:]
		}
		if matched {
			levels[s.level] = true
		}
	}
	if len(levels) != 1 {
		return "", false // no signal, or conflicting signals → leave blank
	}
	for l := range levels {
		return l, true
	}
	return "", false
}
