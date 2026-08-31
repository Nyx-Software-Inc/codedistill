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

package mcpmap_test

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"codedistill/internal/mcpclient"
	"codedistill/internal/mcpmap"
)

// fakeSuggester captures the prompt it was called with and returns a
// canned JSON response. Used to exercise Suggest end-to-end without
// hitting Ollama.
type fakeSuggester struct {
	response  string
	err       error
	gotPrompt string
	callCount int
}

func (f *fakeSuggester) GenerateJSON(ctx context.Context, prompt string) (string, error) {
	f.callCount++
	f.gotPrompt = prompt
	return f.response, f.err
}

func sampleCatalog() []mcpclient.Tool {
	schema := json.RawMessage(`{"type":"object"}`)
	return []mcpclient.Tool{
		{Name: "create_issue", Description: "Create a new issue in the project", InputSchema: schema},
		{Name: "update_issue", Description: "Update an existing issue's title or body", InputSchema: schema},
		{Name: "close_issue", Description: "Mark an issue as closed", InputSchema: schema},
		{Name: "delete_issue", Description: "Permanently delete an issue", InputSchema: schema},
		{Name: "list_issues", Description: "List issues in the project", InputSchema: schema},
	}
}

func TestSuggest_HappyPath(t *testing.T) {
	llm := &fakeSuggester{
		response: `{"create":"create_issue","update":"update_issue","status_change":"close_issue","delete":"delete_issue"}`,
	}
	got, err := mcpmap.Suggest(context.Background(), llm, "todo", sampleCatalog())
	if err != nil {
		t.Fatalf("Suggest: %v", err)
	}
	want := mcpmap.Mapping{
		mcpmap.OpCreate:       "create_issue",
		mcpmap.OpUpdate:       "update_issue",
		mcpmap.OpStatusChange: "close_issue",
		mcpmap.OpDelete:       "delete_issue",
	}
	for op, w := range want {
		if got[op] != w {
			t.Errorf("op %q: got %q, want %q", op, got[op], w)
		}
	}
	if llm.callCount != 1 {
		t.Errorf("expected 1 LLM call, got %d", llm.callCount)
	}
}

func TestSuggest_PromptIncludesItemTypeAndCatalog(t *testing.T) {
	llm := &fakeSuggester{response: `{}`}
	_, err := mcpmap.Suggest(context.Background(), llm, "bug", sampleCatalog())
	if err != nil {
		t.Fatalf("Suggest: %v", err)
	}
	prompt := llm.gotPrompt
	if !strings.Contains(prompt, "bug report") {
		t.Errorf("prompt should mention 'bug report' (item-type description); got:\n%s", prompt)
	}
	for _, name := range []string{"create_issue", "update_issue", "close_issue", "delete_issue"} {
		if !strings.Contains(prompt, name) {
			t.Errorf("prompt missing tool name %q", name)
		}
	}
	for _, op := range mcpmap.AllOperations {
		if !strings.Contains(prompt, string(op)) {
			t.Errorf("prompt missing operation %q", op)
		}
	}
}

func TestSuggest_PromptIsDeterministic(t *testing.T) {
	// Same catalog (in different order) and same item type → same prompt.
	cat1 := sampleCatalog()
	cat2 := []mcpclient.Tool{cat1[4], cat1[0], cat1[3], cat1[2], cat1[1]}

	llm1 := &fakeSuggester{response: `{}`}
	llm2 := &fakeSuggester{response: `{}`}
	_, _ = mcpmap.Suggest(context.Background(), llm1, "todo", cat1)
	_, _ = mcpmap.Suggest(context.Background(), llm2, "todo", cat2)

	if llm1.gotPrompt != llm2.gotPrompt {
		t.Errorf("prompt should be deterministic across catalog order; diffs:\n--- 1 ---\n%s\n--- 2 ---\n%s", llm1.gotPrompt, llm2.gotPrompt)
	}
}

func TestSuggest_MissingKeysBecomeEmpty(t *testing.T) {
	llm := &fakeSuggester{
		response: `{"create":"create_issue","update":"update_issue"}`,
	}
	got, err := mcpmap.Suggest(context.Background(), llm, "todo", sampleCatalog())
	if err != nil {
		t.Fatalf("Suggest: %v", err)
	}
	if got[mcpmap.OpStatusChange] != "" {
		t.Errorf("missing op should be empty, got %q", got[mcpmap.OpStatusChange])
	}
	if got[mcpmap.OpDelete] != "" {
		t.Errorf("missing op should be empty, got %q", got[mcpmap.OpDelete])
	}
}

func TestSuggest_ExtraKeysIgnored(t *testing.T) {
	llm := &fakeSuggester{
		response: `{"create":"create_issue","unknown_op":"foo","update":"update_issue","status_change":"","delete":""}`,
	}
	got, err := mcpmap.Suggest(context.Background(), llm, "todo", sampleCatalog())
	if err != nil {
		t.Fatalf("Suggest: %v", err)
	}
	if _, present := got[mcpmap.Operation("unknown_op")]; present {
		t.Error("extra key 'unknown_op' should not appear in mapping")
	}
	if got[mcpmap.OpCreate] != "create_issue" {
		t.Errorf("create: got %q, want create_issue", got[mcpmap.OpCreate])
	}
}

func TestSuggest_HallucinatedToolKept(t *testing.T) {
	// LLM invents a tool name that's not in the catalog. Suggest must
	// keep it verbatim — Validate is what filters bad names so the UI
	// can show "the LLM picked X, X doesn't exist, pick again."
	llm := &fakeSuggester{
		response: `{"create":"make_issue","update":"","status_change":"","delete":""}`,
	}
	got, err := mcpmap.Suggest(context.Background(), llm, "todo", sampleCatalog())
	if err != nil {
		t.Fatalf("Suggest: %v", err)
	}
	if got[mcpmap.OpCreate] != "make_issue" {
		t.Errorf("hallucinated name should be kept verbatim, got %q", got[mcpmap.OpCreate])
	}
}

func TestSuggest_MalformedJSONErrors(t *testing.T) {
	llm := &fakeSuggester{response: `not json at all`}
	_, err := mcpmap.Suggest(context.Background(), llm, "todo", sampleCatalog())
	if err == nil {
		t.Fatal("expected error for malformed JSON")
	}
	if !strings.Contains(err.Error(), "parse") {
		t.Errorf("error should mention parse failure, got: %v", err)
	}
}

func TestSuggest_LLMErrorPropagates(t *testing.T) {
	llm := &fakeSuggester{err: fmt.Errorf("ollama down")}
	_, err := mcpmap.Suggest(context.Background(), llm, "todo", sampleCatalog())
	if err == nil {
		t.Fatal("expected LLM error to propagate")
	}
	if !strings.Contains(err.Error(), "ollama down") {
		t.Errorf("error should wrap underlying LLM error, got: %v", err)
	}
}

func TestSuggest_GuardsBadInput(t *testing.T) {
	llm := &fakeSuggester{response: `{}`}
	if _, err := mcpmap.Suggest(context.Background(), nil, "todo", sampleCatalog()); err == nil {
		t.Error("expected error for nil suggester")
	}
	if _, err := mcpmap.Suggest(context.Background(), llm, "todo", nil); err == nil {
		t.Error("expected error for empty catalog")
	}
}

func TestValidate_DropsUnknownTools(t *testing.T) {
	m := mcpmap.Mapping{
		mcpmap.OpCreate:       "create_issue", // valid
		mcpmap.OpUpdate:       "make_better",  // hallucinated
		mcpmap.OpStatusChange: "close_issue",  // valid
		mcpmap.OpDelete:       "",             // unavailable
	}
	kept, errs := mcpmap.Validate(m, sampleCatalog())

	if kept[mcpmap.OpCreate] != "create_issue" {
		t.Errorf("create kept: got %q", kept[mcpmap.OpCreate])
	}
	if kept[mcpmap.OpUpdate] != "" {
		t.Errorf("update should be cleared (unknown tool), got %q", kept[mcpmap.OpUpdate])
	}
	if kept[mcpmap.OpStatusChange] != "close_issue" {
		t.Errorf("status_change kept: got %q", kept[mcpmap.OpStatusChange])
	}
	if kept[mcpmap.OpDelete] != "" {
		t.Errorf("delete should remain empty, got %q", kept[mcpmap.OpDelete])
	}
	if len(errs) != 1 {
		t.Errorf("expected 1 error, got %d: %v", len(errs), errs)
	}
}

func TestValidate_AllValidNoErrors(t *testing.T) {
	m := mcpmap.Mapping{
		mcpmap.OpCreate:       "create_issue",
		mcpmap.OpUpdate:       "update_issue",
		mcpmap.OpStatusChange: "",
		mcpmap.OpDelete:       "",
	}
	kept, errs := mcpmap.Validate(m, sampleCatalog())
	if len(errs) != 0 {
		t.Errorf("expected no errors, got: %v", errs)
	}
	if kept[mcpmap.OpCreate] != "create_issue" || kept[mcpmap.OpUpdate] != "update_issue" {
		t.Errorf("valid entries should round-trip")
	}
}

func TestAvailable_OrderedAndFiltered(t *testing.T) {
	m := mcpmap.Mapping{
		mcpmap.OpDelete:       "delete_issue",
		mcpmap.OpCreate:       "create_issue",
		mcpmap.OpStatusChange: "",
		mcpmap.OpUpdate:       "update_issue",
	}
	got := mcpmap.Available(m)
	want := []mcpmap.Operation{mcpmap.OpCreate, mcpmap.OpUpdate, mcpmap.OpDelete}
	if len(got) != len(want) {
		t.Fatalf("Available length: got %d, want %d (%v)", len(got), len(want), got)
	}
	for i, op := range want {
		if got[i] != op {
			t.Errorf("Available[%d]: got %q, want %q", i, got[i], op)
		}
	}
}

func TestAvailable_EmptyMapping(t *testing.T) {
	got := mcpmap.Available(mcpmap.Mapping{})
	if len(got) != 0 {
		t.Errorf("empty mapping should yield no available ops, got %v", got)
	}
}
