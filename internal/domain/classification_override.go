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

import "strings"

// ClassificationOverrides are the accepted values for
// ScratchpadItem.ClassificationOverride, matching the CHECK constraint in
// migration 0027.
//
// This list lives in Go because it previously existed ONLY in SQL, so nothing
// validated it before the insert: `-override banana` reached the database and
// came back as a raw two-line "CHECK constraint failed … (275)". Worse, the two
// places that described the list in Go — the -override flag help and the
// classification_override field comment in internal/api — both said
// "todo|bug|kb|skip" and had missed use_case since v0.7.0. The error message a
// user saw was more accurate than the documentation above it.
//
// Order is the order to show a human: the four real categories, then the escape
// hatch (CE-review item 24).
var ClassificationOverrides = []string{"todo", "bug", "kb", "use_case", "skip"}

// ValidClassificationOverride reports whether s is an accepted override. The
// empty string is valid and means "no override" — the column is nullable.
func ValidClassificationOverride(s string) bool {
	if s == "" {
		return true
	}
	for _, v := range ClassificationOverrides {
		if s == v {
			return true
		}
	}
	return false
}

// ClassificationOverrideList renders the accepted values for help text and error
// messages, so a caller cannot drift from the list the way the flag help did.
func ClassificationOverrideList() string {
	return strings.Join(ClassificationOverrides, "|")
}
