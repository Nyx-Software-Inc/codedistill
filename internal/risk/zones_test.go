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

import "testing"

func TestMatchesZone(t *testing.T) {
	zones := []string{"auth", "db/migrations", "*.lock", "billing"}
	cases := []struct {
		path string
		want bool
	}{
		{"internal/auth/handler.go", true},     // segment
		{"auth/login.go", true},                // leading segment
		{"db/migrations/0001.sql", true},       // prefix
		{"internal/db/migrations/x.sql", true}, // multi-segment pattern matches anywhere
		{"go.sum", false},
		{"deps.lock", true},                  // glob basename
		{"internal/billing/charge.go", true}, // billing segment
		{"internal/api/server.go", false},
	}
	for _, c := range cases {
		if got := MatchesZone(zones, []string{c.path}); got != c.want {
			t.Errorf("MatchesZone(%q) = %v, want %v", c.path, got, c.want)
		}
	}
	// Empty inputs never match.
	if MatchesZone(nil, []string{"auth/x.go"}) || MatchesZone([]string{"auth"}, nil) {
		t.Error("empty zones or paths should not match")
	}
}
