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
	"encoding/json"
	"fmt"
	"strings"
)

// classifyPrompt — four-class production prompt validated by the
// spike/usecase-classifier spike (89.6% on 2-class, 85.4% on 4-class).
// USE_CASE leads because the use_case-vs-todo distinction is the trickiest
// one and the prompt's leading line carries the most weight.
//
// The role/want/why extraction lives in the same one-pass JSON: the model is
// asked to populate them ONLY when category=USE_CASE AND the source explicitly
// conveys a "Role / Want / Why" structure. Casual notes that classify as
// use_case (e.g. "export feature button — next to rename") are expected to
// return empty strings for role/want/why, not invented ones. Hallucination
// resistance is asserted in the agent_test.go suite.
//
// Used when no project file tree is available (no repo_root configured).
// classifyWithFilesPrompt is the same prompt extended with auto-anchor
// suggestions when the agent has a file list to share.
const classifyPrompt = `Classify the content into exactly one of:
- USE_CASE: a persistent capability the system enables a user or role to do (often "users can…", "as a [role] I want…", "the system supports…"). Describes what the product does; survives across releases.
- TODO: a discrete unit of work — usually a verb targeting code, config, docs, or process. Describes what to do next.
- BUG: software defect, error, or broken behavior
- KB: reference info, notes, links, knowledge to keep

When (and only when) category=USE_CASE AND the content explicitly conveys an "as a [role] I want X so that Y" structure, populate role, want, and why with the extracted pieces. Leave them as empty strings ("") if the structure is not explicitly present in the content. Do not invent role/want/why for casual notes or non-use-case content.

Content:
<<<
%s
>>>

Respond as strict JSON:
{"category": "USE_CASE|TODO|BUG|KB", "reasoning": "<short>", "role": "", "want": "", "why": ""}`

// classifyWithFilesPrompt extends classifyPrompt with a project file list
// and a suggested_files field. The agent uses this when an item's project
// has a configured repo_root. We post-validate suggested_files against the
// tree we passed in so any hallucinated paths are dropped before becoming
// agent-suggested anchors.
const classifyWithFilesPrompt = `Classify the content into exactly one of:
- USE_CASE: a persistent capability the system enables a user or role to do (often "users can…", "as a [role] I want…", "the system supports…"). Describes what the product does; survives across releases.
- TODO: a discrete unit of work — usually a verb targeting code, config, docs, or process. Describes what to do next.
- BUG: software defect, error, or broken behavior
- KB: reference info, notes, links, knowledge to keep

When (and only when) category=USE_CASE AND the content explicitly conveys an "as a [role] I want X so that Y" structure, populate role, want, and why with the extracted pieces. Leave them as empty strings ("") if the structure is not explicitly present in the content. Do not invent role/want/why for casual notes or non-use-case content.

If category is TODO, BUG, or USE_CASE AND one or more of the project files below look related (the content references them, or the work obviously belongs in them), return up to 3 paths in suggested_files in descending order of relevance. Use exact path strings from the list — do NOT invent paths. Return [] when nothing obviously matches, when category is KB, or when uncertain.

Project files:
%s

Content:
<<<
%s
>>>

Respond as strict JSON:
{"category": "USE_CASE|TODO|BUG|KB", "reasoning": "<short>", "role": "", "want": "", "why": "", "suggested_files": []}`

// Classification is the agent's structured output for a single classify call.
// Role/Want/Why are populated only when Category == "USE_CASE" and the source
// conveys a user-story shape; otherwise empty.
//
// SuggestedFiles is the auto-anchor extension — paths from the project file
// tree the model thinks are related to the content. Only meaningful for
// TODO / BUG / USE_CASE; empty for KB. Validated against the tree passed in
// to Classify before reaching the agent.
type Classification struct {
	Category       string
	Reasoning      string
	Role           string
	Want           string
	Why            string
	SuggestedFiles []string
}

// ClassifyInput bundles the content with optional auto-anchor context.
// Empty FileTree means "no repo context" — the classifier falls back to the
// pre-auto-anchor prompt and SuggestedFiles in the result is always empty.
type ClassifyInput struct {
	Content  string
	FileTree []string
	// ProjectID lets the production classifier resolve a per-project model
	// (the model-provider arc). Empty → the global default model.
	ProjectID string
}

// Classifier produces a Classification for the given input.
// Implementations: OllamaClassifier (production), test doubles (unit tests).
type Classifier interface {
	Classify(ctx context.Context, in ClassifyInput) (Classification, error)
}

// GenerateJSONFunc matches the ollama.Client.GenerateJSON signature, allowing
// tests to substitute a fake without importing the ollama package.
type GenerateJSONFunc func(ctx context.Context, prompt string) (string, error)

// GenerateJSONModelFunc matches ollama.Client.GenerateJSONWithModel: a JSON
// generate against an explicit model ("" = the client's default). It's how a
// role resolves its own configured model (the model-provider arc).
type GenerateJSONModelFunc func(ctx context.Context, model, prompt string) (string, error)

// ModelForFunc resolves the model a role should use for a given project ("" =
// default). Backed by the per-project model.<role> setting.
type ModelForFunc func(projectID string) string

// OllamaClassifier is the production Classifier, backed by a JSON-mode Ollama
// generate call. When GenerateModel + ModelFor are wired it runs against the
// project's configured classifier model; otherwise it falls back to the
// model-less Generate (tests, or no per-role config).
type OllamaClassifier struct {
	Generate      GenerateJSONFunc
	GenerateModel GenerateJSONModelFunc
	ModelFor      ModelForFunc
}

// gen runs the prompt against the project's resolved model when wired, else the
// plain model-less generator.
func (o *OllamaClassifier) gen(ctx context.Context, projectID, prompt string) (string, error) {
	if o.GenerateModel != nil {
		model := ""
		if o.ModelFor != nil {
			model = o.ModelFor(projectID)
		}
		return o.GenerateModel(ctx, model, prompt)
	}
	return o.Generate(ctx, prompt)
}

type classificationResult struct {
	Category       string   `json:"category"`
	Reasoning      string   `json:"reasoning"`
	Role           string   `json:"role"`
	Want           string   `json:"want"`
	Why            string   `json:"why"`
	SuggestedFiles []string `json:"suggested_files"`
}

// maxSuggestedFiles caps the agent-suggested anchors per item. The prompt
// asks for at most 3; this is a defensive trim if the model returns more.
const maxSuggestedFiles = 3

// maxFileTreeLines caps how many paths we paste into the prompt. Solo-dev
// repos are typically well under this; larger repos get truncated alpha-
// betically. Pre-filter (e.g., by content keywords) is a future polish.
const maxFileTreeLines = 500

func (o *OllamaClassifier) Classify(ctx context.Context, in ClassifyInput) (Classification, error) {
	var prompt string
	if len(in.FileTree) > 0 {
		paths := in.FileTree
		if len(paths) > maxFileTreeLines {
			paths = paths[:maxFileTreeLines]
		}
		prompt = fmt.Sprintf(classifyWithFilesPrompt, strings.Join(paths, "\n"), in.Content)
	} else {
		prompt = fmt.Sprintf(classifyPrompt, in.Content)
	}
	raw, err := o.gen(ctx, in.ProjectID, prompt)
	if err != nil {
		return Classification{}, err
	}
	var res classificationResult
	if err := json.Unmarshal([]byte(raw), &res); err != nil {
		return Classification{}, fmt.Errorf("parse classification: %w (raw: %q)", err, raw)
	}
	cat := normalizeCategory(res.Category)
	switch cat {
	case "USE_CASE", "TODO", "BUG", "KB":
		c := Classification{
			Category:  cat,
			Reasoning: strings.TrimSpace(res.Reasoning),
		}
		// Defensive: only carry role/want/why when the model also returned
		// USE_CASE. A model that mis-fills the optional fields under the
		// wrong category should not corrupt downstream derive logic.
		if cat == "USE_CASE" {
			c.Role = strings.TrimSpace(res.Role)
			c.Want = strings.TrimSpace(res.Want)
			c.Why = strings.TrimSpace(res.Why)
		}
		// Auto-anchor: pass through whatever the model returned. Validation
		// (intersecting with the tree, dropping hallucinated paths, the
		// KB-no-anchors rule, and the per-item cap) lives in agent.Process
		// so every classifier impl gets the same guarantees uniformly.
		c.SuggestedFiles = res.SuggestedFiles
		return c, nil
	default:
		return Classification{}, fmt.Errorf("unexpected category %q (raw: %q)", cat, res.Category)
	}
}

// filterToTree keeps only paths that appear verbatim in the provided tree
// (case-sensitive — paths are repo-relative and case matters on most file
// systems). Caps the result at maxSuggestedFiles. Preserves the model's
// ordering so the most-relevant guess sits first.
func filterToTree(suggested, tree []string) []string {
	if len(suggested) == 0 || len(tree) == 0 {
		return nil
	}
	allowed := make(map[string]struct{}, len(tree))
	for _, p := range tree {
		allowed[p] = struct{}{}
	}
	out := make([]string, 0, len(suggested))
	seen := make(map[string]struct{}, len(suggested))
	for _, p := range suggested {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if _, ok := allowed[p]; !ok {
			continue
		}
		if _, dup := seen[p]; dup {
			continue
		}
		seen[p] = struct{}{}
		out = append(out, p)
		if len(out) >= maxSuggestedFiles {
			break
		}
	}
	return out
}

// normalizeCategory uppercases, trims whitespace, and strips any leading
// non-alphabetic characters. The spike observed qwen2.5:7b emitting category
// strings like ".TODO" deterministically on a small fraction of inputs;
// stripping the leading dot recovers the intended category.
func normalizeCategory(s string) string {
	s = strings.ToUpper(strings.TrimSpace(s))
	for len(s) > 0 && (s[0] < 'A' || s[0] > 'Z') {
		s = s[1:]
	}
	return s
}
