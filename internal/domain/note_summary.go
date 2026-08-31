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

import (
	"strings"
	"unicode/utf8"
)

// SummaryMaxBytes is the budget for ItemEvent.Summary, excluding the ellipsis —
// so a truncated summary is at most SummaryMaxBytes+3 bytes, which is what the
// API path already produced for ASCII.
//
// Bytes rather than runes, deliberately: a rune budget would let a CJK summary
// reach 360+ bytes and change what the column has always stored. The fix here is
// to stop splitting runes, not to redefine the limit.
const SummaryMaxBytes = 120

// NoteSummary returns the one-line preview stored in ItemEvent.Summary: the
// first line, trimmed, truncated to SummaryMaxBytes on a rune boundary, with an
// ellipsis when anything was dropped.
//
// This lives in domain because two packages wrote their own copy of the rule and
// the copies disagreed. internal/api appended an ellipsis; internal/mcp did not.
// Both wrote the same column, so the same note previewed differently depending on
// whether a human or an agent recorded it — visible side by side in one timeline.
//
// Both also sliced bytes, so a multi-byte rune straddling the cut was severed and
// json.Marshal replaced the fragments with U+FFFD. Pure CJK and pure emoji escape
// by arithmetic (120 divides by 3 and by 4); mixed content — "fix: 日本語のバグ…"
// — does not (CE-review item 23).
func NoteSummary(text string) string {
	line := text
	if i := strings.IndexByte(text, '\n'); i >= 0 {
		line = text[:i]
	}
	line = strings.TrimSpace(line)
	if len(line) <= SummaryMaxBytes {
		return line
	}
	return strings.TrimSpace(truncateAtRuneBoundary(line, SummaryMaxBytes)) + "…"
}

// truncateAtRuneBoundary returns the longest prefix of s that is at most max
// bytes AND does not end mid-rune. If the byte at the cut is a UTF-8
// continuation byte we are inside a rune, so walk back to where it started.
func truncateAtRuneBoundary(s string, max int) string {
	if len(s) <= max {
		return s
	}
	for max > 0 && !utf8.RuneStart(s[max]) {
		max--
	}
	return s[:max]
}
