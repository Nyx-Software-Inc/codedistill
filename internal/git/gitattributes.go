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

package git

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/go-git/go-git/v5/plumbing/format/gitattributes"
)

// linguistAttrs are the .gitattributes markers GitHub Linguist (and now we)
// treat as "not human-authored code": generated build output and vendored
// dependencies. A file with either set is excluded from the headline metrics.
var linguistAttrs = []string{"linguist-generated", "linguist-vendored"}

// loadGeneratedMatcher reads the worktree .gitattributes and returns a matcher
// for the linguist markers. We read the working-tree file (the team's current
// declaration of what's generated) rather than the version at each commit —
// that's how GitHub Linguist treats it, and it keeps the check cheap. A
// missing/unparseable file yields an empty matcher (matches nothing).
func loadGeneratedMatcher(root string) gitattributes.Matcher {
	f, err := os.Open(filepath.Join(root, ".gitattributes"))
	if err != nil {
		return gitattributes.NewMatcher(nil)
	}
	defer f.Close()
	attrs, err := gitattributes.ReadAttributes(f, nil, true)
	if err != nil {
		return gitattributes.NewMatcher(nil)
	}
	return gitattributes.NewMatcher(attrs)
}

// attrSaysGenerated reports whether .gitattributes marks the repo-relative
// path as linguist-generated or linguist-vendored. Honors explicit overrides:
// `-linguist-generated` or `linguist-generated=false` un-mark a path even if a
// broader earlier rule set it.
func attrSaysGenerated(m gitattributes.Matcher, path string) bool {
	if m == nil || path == "" {
		return false
	}
	res, matched := m.Match(strings.Split(path, "/"), linguistAttrs)
	if !matched {
		return false
	}
	for _, name := range linguistAttrs {
		if a, ok := res[name]; ok && attrIsTrue(a) {
			return true
		}
	}
	return false
}

// isExcluded decides whether a file is set aside from the headline code
// metrics: either it matches the built-in default conventions (isGenerated)
// or the repo's .gitattributes marks it linguist-generated/vendored. The
// .gitattributes matcher is loaded once per Repo and cached.
func (r *Repo) isExcluded(path string) bool {
	if isGenerated(path) {
		return true
	}
	r.genOnce.Do(func() { r.genMatcher = loadGeneratedMatcher(r.root) })
	return attrSaysGenerated(r.genMatcher, path)
}

// IsExcluded reports whether a path is a generated/vendored artifact (built-in
// conventions + .gitattributes linguist-generated/vendored). Exported so the
// code indexer skips the same files the headline metrics already set aside.
func (r *Repo) IsExcluded(path string) bool { return r.isExcluded(path) }

func attrIsTrue(a gitattributes.Attribute) bool {
	switch {
	case a.IsUnset(): // -linguist-generated
		return false
	case a.IsSet(): // linguist-generated
		return true
	case a.IsValueSet(): // linguist-generated=true|false
		return strings.EqualFold(a.Value(), "true")
	default:
		return false
	}
}
