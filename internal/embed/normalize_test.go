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

import "testing"

func TestNormalizeItemText(t *testing.T) {
	cases := []struct{ in, want string }{
		{
			"As a CodeDistill user, I would like for Code Distill to do a thorough Code Analysis",
			"for Code Distill to do a thorough Code Analysis",
		},
		{
			"As a CodeDistill user, I want the option to replace the bundled local Ollama model",
			"the option to replace the bundled local Ollama model",
		},
		{
			"As a codedistill user, I would like the canvas to update to colocate grouped items automatically",
			"the canvas to update to colocate grouped items automatically",
		},
		{
			"As the creator of CodeDistill, I would like to have a small little utility",
			"have a small little utility",
		},
		{
			// no desire verb after the role clause — keep the rest verbatim
			"As a Code Distill user, when I click the claim button it should fill in my name",
			"when I click the claim button it should fill in my name",
		},
		{
			// not a user story — untouched
			"Fix the off-by-one in the pager",
			"Fix the off-by-one in the pager",
		},
	}
	for _, c := range cases {
		if got := NormalizeItemText(c.in); got != c.want {
			t.Errorf("NormalizeItemText(%q)\n  = %q\n want %q", c.in, got, c.want)
		}
	}
}

// Two unrelated stories must share far less text after normalization than before
// — that drop is exactly what pulls their cosine below the advisory cutoff.
func TestNormalizeRemovesSharedBoilerplate(t *testing.T) {
	a := NormalizeItemText("As a CodeDistill user, I would like a desktop widget showing my todo list")
	b := NormalizeItemText("As a CodeDistill user, I would like license expiry notifications")
	if wordsOverlap(a, b) > 0 {
		t.Errorf("residuals still overlap: %q vs %q", a, b)
	}
}

func wordsOverlap(a, b string) int {
	set := map[string]bool{}
	for _, w := range splitWords(a) {
		set[w] = true
	}
	n := 0
	for _, w := range splitWords(b) {
		if set[w] {
			n++
		}
	}
	return n
}

func splitWords(s string) []string {
	var out []string
	cur := ""
	for _, r := range s {
		if r == ' ' {
			if cur != "" {
				out = append(out, cur)
				cur = ""
			}
		} else {
			cur += string(r)
		}
	}
	if cur != "" {
		out = append(out, cur)
	}
	return out
}
