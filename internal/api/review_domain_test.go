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

package api

import (
	"reflect"
	"testing"
)

func TestDomainsOf(t *testing.T) {
	cases := []struct {
		name  string
		paths map[string]bool
		want  []string
	}{
		{"empty", map[string]bool{}, []string{}},
		{"top-level file has no domain", map[string]bool{"go.mod": true}, []string{}},
		{"single dir", map[string]bool{"internal/auth/login.go": true}, []string{"internal"}},
		{"distinct + sorted + deduped", map[string]bool{
			"web/src/app.ts":          true,
			"internal/auth/login.go":  true,
			"internal/auth/logout.go": true,
			"README.md":               true,
		}, []string{"internal", "web"}},
		{"leading slash tolerated", map[string]bool{"/cmd/main.go": true}, []string{"cmd"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := domainsOf(c.paths); !reflect.DeepEqual(got, c.want) {
				t.Errorf("domainsOf(%v) = %v, want %v", c.paths, got, c.want)
			}
		})
	}
}

func TestEntryVerdict(t *testing.T) {
	cases := []struct {
		name string
		e    reviewQueueEntry
		want string
	}{
		{"reverted dominates an approval", reviewQueueEntry{Reverted: true, Decision: "approved", HasVerification: true}, "failed"},
		{"human approve", reviewQueueEntry{Decision: "approved"}, "clean"},
		{"human reject", reviewQueueEntry{Decision: "rejected", HasVerification: true}, "failed"},
		{"failing verification", reviewQueueEntry{HasVerification: true, Failing: true}, "failed"},
		{"refuted by AI review", reviewQueueEntry{HasVerification: true, Refuted: true}, "failed"},
		{"green verification", reviewQueueEntry{HasVerification: true}, "clean"},
		{"no evidence yet", reviewQueueEntry{}, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := entryVerdict(c.e); got != c.want {
				t.Errorf("entryVerdict(%+v) = %q, want %q", c.e, got, c.want)
			}
		})
	}
}
