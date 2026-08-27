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

package tags

import (
	"reflect"
	"testing"
)

func TestExtract(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		{"check #hash_tags and #tag-verify", []string{"hash_tags", "tag-verify"}},
		{"# Markdown heading, issue #123, no tags", []string{}},
		{"dup #Ui and #ui", []string{"Ui", "ui"}}, // Normalize dedupes, Extract doesn't
		{"none here", []string{}},
	}
	for _, c := range cases {
		if got := Extract(c.in); !reflect.DeepEqual(got, c.want) {
			t.Errorf("Extract(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

func TestNormalizeAndMerge(t *testing.T) {
	if got := Normalize([]string{" Ui ", "ui", "", "API"}); !reflect.DeepEqual(got, []string{"ui", "api"}) {
		t.Errorf("Normalize = %v", got)
	}
	if got := Merge([]string{"existing"}, []string{"Existing", "new"}); !reflect.DeepEqual(got, []string{"existing", "new"}) {
		t.Errorf("Merge = %v", got)
	}
	if got := Normalize(nil); got == nil || len(got) != 0 {
		t.Errorf("Normalize(nil) = %v, want empty non-nil", got)
	}
}
