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
	"testing"
)

// The classifier resolves its model per project via ModelFor and runs against
// it through GenerateModel (the model-provider arc).
func TestClassifierResolvesPerProjectModel(t *testing.T) {
	var gotModel string
	c := &OllamaClassifier{
		GenerateModel: func(_ context.Context, model, _ string) (string, error) {
			gotModel = model
			return `{"category":"TODO","reasoning":"x"}`, nil
		},
		ModelFor: func(projectID string) string {
			if projectID == "p1" {
				return "qwen2.5:14b"
			}
			return ""
		},
	}
	if _, err := c.Classify(context.Background(), ClassifyInput{Content: "do x", ProjectID: "p1"}); err != nil {
		t.Fatalf("classify: %v", err)
	}
	if gotModel != "qwen2.5:14b" {
		t.Errorf("resolved model = %q, want qwen2.5:14b", gotModel)
	}
}

// With no per-role wiring the classifier uses the model-less Generate (the
// path existing call sites + tests rely on).
func TestClassifierFallsBackToGenerate(t *testing.T) {
	called := false
	c := &OllamaClassifier{
		Generate: func(context.Context, string) (string, error) {
			called = true
			return `{"category":"BUG","reasoning":"x"}`, nil
		},
	}
	if _, err := c.Classify(context.Background(), ClassifyInput{Content: "boom"}); err != nil {
		t.Fatalf("classify: %v", err)
	}
	if !called {
		t.Error("expected fallback to model-less Generate")
	}
}

// The criteria drafter resolves its model per project the same way.
func TestCriteriaDrafterResolvesPerProjectModel(t *testing.T) {
	var gotModel string
	d := &OllamaCriteriaDrafter{
		GenerateModel: func(_ context.Context, model, _ string) (string, error) {
			gotModel = model
			return `{"criteria":["does the thing"]}`, nil
		},
		ModelFor: func(projectID string) string {
			if projectID == "p1" {
				return "qwen2.5-coder:32b"
			}
			return ""
		},
	}
	if _, err := d.Draft(context.Background(), CriteriaInput{Category: "BUG", Subject: "s", Detail: "d", ProjectID: "p1"}); err != nil {
		t.Fatalf("draft: %v", err)
	}
	if gotModel != "qwen2.5-coder:32b" {
		t.Errorf("resolved model = %q, want qwen2.5-coder:32b", gotModel)
	}
}
