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

package embed

import (
	"regexp"
	"strings"
)

// User-story scaffolding strippers. Anchored at the start so they only touch a
// leading "As a <role>, I would like to ..." preamble — never content further in.
var (
	storyRole   = regexp.MustCompile(`(?i)^\s*as (?:an?|the)\s+[^,]{1,60},\s*`)
	storyDesire = regexp.MustCompile(`(?i)^\s*i(?:\s+would|\s*'d)?\s+(?:like|want|need|wish|prefer)(?:\s+to)?\s+`)
)

// NormalizeItemText strips the boilerplate user-story scaffolding ("As a <role>,
// I would like to …") from the front of an item's text before it is embedded, so
// two otherwise-unrelated stories aren't judged near-duplicates merely for
// sharing the template (backlog bug 7123ac1a — "every 'As a CodeDistill user…'
// gets linked as a possible match"). Non-story text is returned unchanged, and if
// stripping would empty the text the original is kept (never embed nothing).
func NormalizeItemText(s string) string {
	out := storyRole.ReplaceAllString(s, "")
	out = storyDesire.ReplaceAllString(out, "")
	if strings.TrimSpace(out) == "" {
		return s
	}
	return out
}
