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

package agent

import (
	"context"
	"strings"
	"testing"
)

func TestCriteriaDrafter_ParseDedupCap(t *testing.T) {
	// Model returns whitespace, a case-insensitive dupe, an empty, and more
	// than the cap. Expect: trimmed, deduped, capped at maxDraftCriteria.
	d := &OllamaCriteriaDrafter{Generate: func(_ context.Context, _ string) (string, error) {
		return `{"criteria":["  Alpha  ","Beta","beta","","Gamma","Delta","Epsilon","Zeta","Eta"]}`, nil
	}}
	out, err := d.Draft(context.Background(), CriteriaInput{Category: "USE_CASE", Subject: "x"})
	if err != nil {
		t.Fatalf("draft: %v", err)
	}
	if len(out) != maxDraftCriteria {
		t.Fatalf("len = %d, want %d (capped): %v", len(out), maxDraftCriteria, out)
	}
	if out[0] != "Alpha" {
		t.Errorf("first = %q, want trimmed \"Alpha\"", out[0])
	}
	// case-insensitive dedup dropped the second "beta"
	betas := 0
	for _, c := range out {
		if strings.EqualFold(c, "beta") {
			betas++
		}
	}
	if betas != 1 {
		t.Errorf("expected exactly 1 beta after dedup, got %d", betas)
	}
}

func TestCriteriaDrafter_BadJSON(t *testing.T) {
	d := &OllamaCriteriaDrafter{Generate: func(_ context.Context, _ string) (string, error) {
		return `not json`, nil
	}}
	if _, err := d.Draft(context.Background(), CriteriaInput{Category: "BUG", Subject: "x"}); err == nil {
		t.Error("expected a parse error on non-JSON output")
	}
}

func TestCriteriaInput_RoleWantWhyInPrompt(t *testing.T) {
	var captured string
	d := &OllamaCriteriaDrafter{Generate: func(_ context.Context, prompt string) (string, error) {
		captured = prompt
		return `{"criteria":["ok"]}`, nil
	}}
	_, _ = d.Draft(context.Background(), CriteriaInput{
		Category: "USE_CASE", Subject: "export", Detail: "d",
		Role: "developer", Want: "export a zip", Why: "backups",
	})
	if !strings.Contains(captured, "developer") || !strings.Contains(captured, "export a zip") {
		t.Errorf("role/want not threaded into prompt:\n%s", captured)
	}
}
