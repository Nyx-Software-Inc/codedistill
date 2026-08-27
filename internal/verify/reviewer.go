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

package verify

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// ReviewInput is what the adversarial reviewer judges: a single acceptance
// criterion (the contract), the item intent for context, and the actual change.
// Model selects the local model to review with (empty → the app default); a
// project can point review at a stronger code model than the classifier uses.
type ReviewInput struct {
	Criterion string
	Intent    string
	Diff      string
	Model     string
}

// ReviewVerdict is the reviewer's call. Refuted=true means it could NOT confirm
// the criterion is satisfied by the change (the adversarial default when in
// doubt); Explanation justifies the call and is shown to the human.
type ReviewVerdict struct {
	Refuted     bool
	Explanation string
}

// Reviewer is the independent AI-review layer of the verification portfolio
// (glass-box Phase 3, principle #6 tier 2): a DIFFERENT model from the one that
// wrote the code, prompted to REFUTE that a change satisfies a criterion. It is
// advisory — never the only guard — and never moves criterion state. Optional on
// the Service; nil disables the layer.
type Reviewer interface {
	Review(ctx context.Context, in ReviewInput) (ReviewVerdict, error)
}

// GenerateFunc is a JSON-mode LLM generate call against a chosen local model —
// matches ollama.Client.GenerateJSONWithModel (empty model → the app default).
type GenerateFunc func(ctx context.Context, model, prompt string) (string, error)

const reviewPrompt = `You are an INDEPENDENT, ADVERSARIAL reviewer. Your job is to REFUTE the claim that a code change satisfies a specific acceptance criterion. Treat the change as guilty until proven innocent: actively look for any way the criterion is NOT fully met by this diff. Do not give the benefit of the doubt — if the diff does not clearly and verifiably satisfy the criterion, or you cannot tell from what is shown, you MUST refute it.

ACCEPTANCE CRITERION (what must be true):
<<<
%s
>>>

ITEM INTENT (context only):
<<<
%s
>>>

THE CHANGE (unified diff at the implementing commit):
<<<
%s
>>>

Decide whether the diff CONCLUSIVELY satisfies the criterion.
- refuted = true if it is not fully, verifiably satisfied by this diff (or you can't tell from it).
- refuted = false ONLY if the diff clearly and completely satisfies the criterion.
Explain in 1-3 sentences, citing specifics from the diff.

Respond as strict JSON: {"refuted": true|false, "explanation": "<text>"}`

type ollamaReviewer struct{ gen GenerateFunc }

// NewOllamaReviewer builds a Reviewer backed by a JSON-mode generate call (the
// same local-Ollama generate the classifier/criteria-drafter use). Local and
// free — and a different model than the agent that wrote the code.
func NewOllamaReviewer(gen GenerateFunc) Reviewer { return &ollamaReviewer{gen: gen} }

func (r *ollamaReviewer) Review(ctx context.Context, in ReviewInput) (ReviewVerdict, error) {
	diff := strings.TrimSpace(in.Diff)
	if diff == "" {
		diff = "(no diff available)"
	}
	raw, err := r.gen(ctx, in.Model, fmt.Sprintf(reviewPrompt, in.Criterion, in.Intent, diff))
	if err != nil {
		return ReviewVerdict{}, err
	}
	var res struct {
		Refuted     bool   `json:"refuted"`
		Explanation string `json:"explanation"`
	}
	if err := json.Unmarshal([]byte(raw), &res); err != nil {
		return ReviewVerdict{}, fmt.Errorf("parse review: %w (raw: %q)", err, raw)
	}
	return ReviewVerdict{Refuted: res.Refuted, Explanation: strings.TrimSpace(res.Explanation)}, nil
}
