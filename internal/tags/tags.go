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

// Package tags holds the inline-#hashtag extraction and tag normalization
// shared by every capture path (HTTP API, MCP tools, analysis import) —
// extracted from internal/api so agent captures get the same tag behavior
// as UI captures (Backlog #13: the MCP path used to hardcode empty tags).
package tags

import (
	"regexp"
	"strings"
)

// hashtagRE matches inline #hashtags: a # followed by a letter then word
// chars / hyphen. Leading-digit (#1) is excluded so issue-ish references
// and markdown headings ("# Title") aren't mistaken for tags.
var hashtagRE = regexp.MustCompile(`#([\p{L}][\p{L}\p{N}_-]*)`)

// Extract pulls inline #hashtags out of text as tag names (without the #).
// The text itself is never modified — the tag is surfaced alongside the
// content, not stripped from it.
func Extract(content string) []string {
	ms := hashtagRE.FindAllStringSubmatch(content, -1)
	out := make([]string, 0, len(ms))
	for _, m := range ms {
		out = append(out, m[1])
	}
	return out
}

// Normalize trims, lowercases, drops empties, and dedupes while preserving
// first-seen order. Always returns a non-nil slice so JSON responses
// serialize as [] rather than null.
func Normalize(in []string) []string {
	out := make([]string, 0, len(in))
	seen := make(map[string]struct{}, len(in))
	for _, t := range in {
		t = strings.ToLower(strings.TrimSpace(t))
		if t == "" {
			continue
		}
		if _, ok := seen[t]; ok {
			continue
		}
		seen[t] = struct{}{}
		out = append(out, t)
	}
	return out
}

// Merge unions two tag lists through Normalize (preserving first-seen
// order, existing tags first).
func Merge(existing, extra []string) []string {
	return Normalize(append(append([]string{}, existing...), extra...))
}
