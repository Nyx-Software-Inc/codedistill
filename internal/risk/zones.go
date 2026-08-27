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

package risk

import (
	"path"
	"strings"
)

// ZonesSettingKey is the project-scoped list of always-review path patterns —
// the hand-tagged critical areas (auth/billing/migrations) that always escalate.
const ZonesSettingKey = "review.always_review_zones"

// MatchesZone reports whether any changed path falls in any always-review zone.
// A pattern containing glob chars (* ? [) is matched with path.Match against the
// full path and the basename; a plain pattern matches as a path segment or a
// path prefix — so "auth" hits internal/auth/x.go and "db/migrations" hits its
// whole subtree. Empty patterns are ignored.
func MatchesZone(zones, paths []string) bool {
	for _, z := range zones {
		z = strings.Trim(strings.TrimSpace(z), "/")
		if z == "" {
			continue
		}
		for _, p := range paths {
			if matchZone(z, strings.TrimSpace(p)) {
				return true
			}
		}
	}
	return false
}

func matchZone(pattern, p string) bool {
	if p == "" {
		return false
	}
	if strings.ContainsAny(pattern, "*?[") {
		if ok, _ := path.Match(pattern, p); ok {
			return true
		}
		ok, _ := path.Match(pattern, path.Base(p))
		return ok
	}
	if p == pattern || strings.HasPrefix(p, pattern+"/") || strings.HasSuffix(p, "/"+pattern) || strings.Contains(p, "/"+pattern+"/") {
		return true
	}
	for _, s := range strings.Split(p, "/") {
		if s == pattern {
			return true
		}
	}
	return false
}
