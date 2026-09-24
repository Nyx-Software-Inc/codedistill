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

// Package modelprobe answers "will this provider do the job", not merely "is
// something listening".
//
// Three failures in this codebase motivated every check here, and none of them
// would have been caught by a reachability ping:
//
//   - Ollama served /api/tags in one millisecond while /api/generate never
//     returned. Happened twice after a laptop suspend. The health check passed,
//     the classifier queued work, and every item hung.
//   - num_ctx defaulted to 4096 regardless of the model's real capacity, and
//     Ollama TRUNCATED longer prompts without erroring. A 7,116-token prompt
//     reported prompt_eval_count 4096 and came back with invented vocabulary,
//     because the instructions had been cut off the front. Silent since v1.
//   - A model name that is merely wrong produces a 404 at call time, minutes
//     into a job, rather than at the moment someone typed it.
//
// So a probe generates, and — when asked — proves the context window by sending
// a prompt larger than the default and reading back how much was actually read.
// That number comes from the server, not from configuration, which is the only
// way to know configuration is true.
package modelprobe

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Outcome grades a provider from unusable to proven.
type Outcome string

const (
	// Unreachable: nothing answered. Wrong host, wrong port, nothing running.
	Unreachable Outcome = "unreachable"
	// Reached but the model cannot be used: wrong name, auth rejected, or
	// generation hangs. Configured, and useless.
	ModelUnusable Outcome = "model_unusable"
	// Generation works, but proving the context window took longer than anyone
	// should wait. NOT a failure of the provider and explicitly not truncation:
	// local prefill runs about 76ms/token on modest hardware, so a 16k window
	// legitimately needs twenty minutes to demonstrate. Saying "truncates" here
	// would be a confident wrong answer, which is the exact failure this
	// package exists to prevent.
	ContextUnproven Outcome = "context_unproven"
	// Generation works. The context window was not tested.
	Working Outcome = "working"
	// Generation works AND the context window was measured at or above what
	// was asked for. The only state that justifies pointing a document
	// decomposition at it.
	Verified Outcome = "verified"
	// Generation works but the provider reads LESS than it claims. The
	// dangerous one: everything looks healthy and long prompts are silently
	// truncated.
	ContextShort Outcome = "context_short"
)

// Result is what a probe learned. Every field is observed, not assumed.
type Result struct {
	Outcome Outcome `json:"outcome"`
	// Detail is written for a human to act on: what is wrong and what to do.
	Detail string `json:"detail"`

	Reachable   bool `json:"reachable"`
	CanGenerate bool `json:"can_generate"`

	// ContextAsked is what the probe requested; ContextSeen is what the server
	// reported reading. ContextSeen < ContextAsked means truncation, which is
	// the failure that corrupted prompts here for a year without one error.
	ContextAsked int `json:"context_asked,omitempty"`
	ContextSeen  int `json:"context_seen,omitempty"`

	// Described is what the provider SAID about the model, when it says
	// anything. Ollama and Gemini answer precisely; OpenAI and Anthropic list
	// only an id. Absence is reported rather than guessed, because a stale
	// hardcoded limit is the same class of error as the truncation this
	// package exists to catch.
	Described Description `json:"described,omitempty"`

	LatencyMS int64     `json:"latency_ms"`
	Model     string    `json:"model,omitempty"`
	CheckedAt time.Time `json:"checked_at"`
}

// OK reports whether the provider can be used at all.
func (r Result) OK() bool {
	return r.Outcome == Working || r.Outcome == Verified || r.Outcome == ContextUnproven
}

// Target is the connection to probe.
type Target struct {
	Protocol string // ollama | openai
	Endpoint string
	Model    string
	APIKey   string // decrypted by the caller

	// NeedContext, when > 0, makes the probe prove the window by sending a
	// prompt larger than the provider's likely default and comparing what came
	// back. Costs a few seconds and is the only honest way to know.
	NeedContext int
}

// Probe runs the checks in cost order: cheap failures fail fast, and the
// expensive context proof only runs once generation is known to work.
func Probe(ctx context.Context, hc *http.Client, t Target, now func() time.Time) (res Result) {
	if hc == nil {
		hc = &http.Client{Timeout: 90 * time.Second}
	}
	if now == nil {
		now = time.Now
	}
	// NAMED RETURN so the deferred timing actually lands: with a local `res`
	// the defer ran after the value was copied and every probe reported 0.0s.
	res = Result{Outcome: Unreachable, Model: t.Model, CheckedAt: now().UTC()}
	start := now()
	defer func() { res.LatencyMS = now().Sub(start).Milliseconds() }()

	if strings.TrimSpace(t.Endpoint) == "" {
		res.Detail = "no endpoint configured"
		return res
	}

	// 1. Is anything there? Milliseconds when the answer is no.
	if err := reachable(ctx, hc, t); err != nil {
		res.Detail = fmt.Sprintf("cannot reach %s: %v", t.Endpoint, err)
		return res
	}
	res.Reachable = true

	// 2. Will it generate? This is the check that catches a wedged server,
	// a model name that does not exist, and a rejected key.
	seen, err := generate(ctx, hc, t, "ok", 1, 0)
	if err != nil {
		res.Outcome = ModelUnusable
		res.Detail = err.Error()
		return res
	}
	res.CanGenerate = true
	res.Outcome = Working
	res.Detail = "generation works"
	_ = seen

	if t.NeedContext <= 0 {
		return res
	}

	// ASK BEFORE MEASURING. The provider knows its own model, and answering
	// takes milliseconds where proving it empirically takes twenty minutes for
	// a 16k window. Only Ollama-style servers and Gemini answer; the rest list
	// an id and nothing else, and that is handled below rather than guessed.
	if d, err := Describe(ctx, hc, t); err == nil {
		res.Described = d
		if d.ContextLength > 0 {
			res.ContextSeen = d.ContextLength
			if d.ContextLength < t.NeedContext {
				res.Outcome = ContextShort
				res.Detail = fmt.Sprintf("%s holds %d tokens; this needs %d — longer input would be truncated",
					t.Model, d.ContextLength, t.NeedContext)
			} else {
				res.Outcome = Verified
				res.Detail = fmt.Sprintf("generation works; %s reports a %d-token window",
					t.Model, d.ContextLength)
			}
			return res
		}
	}

	// 3. The provider would not say, so measure. Expensive and a last resort:
	// this is the path that takes minutes, and it exists only for servers that
	// report no limit at all.
	res.ContextAsked = t.NeedContext
	filler := fillerPrompt(t.NeedContext)

	// The proof needs a deadline scaled to the work, not a fixed one. Prefill
	// was measured at ~76ms/token on this hardware, so a 16k window needs
	// twenty minutes to demonstrate; a flat 90s cut it off and the run was
	// graded as truncating, which was simply false.
	pctx, pcancel := context.WithTimeout(ctx, contextProofTimeout(t.NeedContext))
	seen, err = generate(pctx, hc, t, filler, 1, t.NeedContext)
	pcancel()
	if err != nil {
		if isDeadline(err, pctx) {
			// Slow is not short. The provider may be perfectly capable.
			res.Outcome = ContextUnproven
			res.Detail = fmt.Sprintf("generation works, but reading %d tokens took longer than %s — too slow to prove the window here, not evidence of truncation",
				t.NeedContext, contextProofTimeout(t.NeedContext).Round(time.Minute))
			return res
		}
		// A genuine refusal: some providers reject an oversized prompt rather
		// than truncating, which is the safe behaviour and still means it
		// will not do the job.
		res.Outcome = ContextShort
		res.Detail = fmt.Sprintf("refused a %d-token prompt: %v", t.NeedContext, err)
		return res
	}
	res.ContextSeen = seen

	switch {
	case seen == 0:
		// The server did not report token counts; the OpenAI path often does
		// not. Saying so is better than inventing a verdict.
		res.Outcome = Working
		res.Detail = "generation works; this provider does not report token counts, so the context window could not be proven"
	case seen < int(float64(t.NeedContext)*0.9):
		res.Outcome = ContextShort
		res.Detail = fmt.Sprintf("read only %d of %d tokens — longer prompts will be silently truncated", seen, t.NeedContext)
	default:
		res.Outcome = Verified
		res.Detail = fmt.Sprintf("generation works and %d tokens were read in full", seen)
	}
	return res
}

// contextProofTimeout scales with the prompt being proven. Derived from a
// measured ~76ms/token local prefill, doubled, with a floor for fast remote
// providers and a ceiling so nothing waits forever.
func contextProofTimeout(tokens int) time.Duration {
	d := time.Duration(tokens) * 160 * time.Millisecond
	if d < 90*time.Second {
		return 90 * time.Second
	}
	if d > 30*time.Minute {
		return 30 * time.Minute
	}
	return d
}

// isDeadline distinguishes "we stopped waiting" from "the provider said no".
// Conflating them is what produced a false truncation verdict.
func isDeadline(err error, ctx context.Context) bool {
	return errors.Is(err, context.DeadlineExceeded) ||
		ctx.Err() != nil ||
		strings.Contains(err.Error(), "context deadline exceeded") ||
		strings.Contains(err.Error(), "Client.Timeout")
}

func reachable(ctx context.Context, hc *http.Client, t Target) error {
	if t.Protocol == "bedrock" {
		// Bedrock has no unauthenticated liveness endpoint, and its model list
		// lives on a DIFFERENT host from generation. Asking that host is the
		// closest equivalent: it proves the credentials sign correctly and the
		// region exists, which is what reachability is being asked here.
		_, err := listModelsBedrock(ctx, hc, t)
		return err
	}
	url := strings.TrimRight(t.Endpoint, "/")
	switch t.Protocol {
	case "azure":
		url = azureDeploymentsURL(t.Endpoint)
	case "openai", "anthropic", "gemini":
		url += "/models"
	default:
		url += "/api/tags"
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	switch t.Protocol {
	case "gemini":
		geminiAuth(req, t)
	case "anthropic":
		if t.APIKey != "" {
			req.Header.Set("x-api-key", t.APIKey)
		}
		req.Header.Set("anthropic-version", "2023-06-01")
	case "azure":
		if t.APIKey != "" {
			req.Header.Set("api-key", t.APIKey)
		}
	default:
		auth(req, t)
	}
	resp, err := hc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)
	// 401/403 still proves something is listening, and the generate step will
	// produce the clearer message about credentials.
	if resp.StatusCode >= 500 {
		return fmt.Errorf("server returned %d", resp.StatusCode)
	}
	return nil
}

// generate asks for a bounded completion and returns the prompt token count the
// server reported, or 0 when it reported none.
func generate(ctx context.Context, hc *http.Client, t Target, prompt string, maxOut, numCtx int) (int, error) {
	var url string
	var body []byte
	var err error

	switch t.Protocol {
	case "openai":
		url = strings.TrimRight(t.Endpoint, "/") + "/chat/completions"
		body, err = json.Marshal(map[string]any{
			"model":      t.Model,
			"messages":   []map[string]string{{"role": "user", "content": prompt}},
			"max_tokens": maxOut,
		})
	case "anthropic":
		url = strings.TrimRight(t.Endpoint, "/") + "/messages"
		body, err = json.Marshal(map[string]any{
			"model":      t.Model,
			"max_tokens": maxOut, // required by this API, not optional
			"messages":   []map[string]string{{"role": "user", "content": prompt}},
		})
	case "azure":
		url = azureChatURL(t.Endpoint, t.Model)
		body, err = json.Marshal(map[string]any{
			"messages":   []map[string]string{{"role": "user", "content": prompt}},
			"max_tokens": maxOut,
		})
	case "bedrock":
		url = strings.TrimRight(t.Endpoint, "/") + "/model/" + t.Model + "/converse"
		body, err = json.Marshal(map[string]any{
			"messages": []map[string]any{
				{"role": "user", "content": []map[string]string{{"text": prompt}}},
			},
			"inferenceConfig": map[string]any{"maxTokens": maxOut},
		})
	case "gemini":
		url = strings.TrimRight(strings.TrimRight(t.Endpoint, "/"), "/") +
			"/models/" + strings.TrimPrefix(t.Model, "models/") + ":generateContent"
		body, err = json.Marshal(map[string]any{
			"contents":         []map[string]any{{"parts": []map[string]string{{"text": prompt}}}},
			"generationConfig": map[string]any{"maxOutputTokens": maxOut},
		})
	default:
		url = strings.TrimRight(t.Endpoint, "/") + "/api/generate"
		opts := map[string]any{"temperature": 0.0, "num_predict": maxOut}
		if numCtx > 0 {
			opts["num_ctx"] = numCtx
		}
		body, err = json.Marshal(map[string]any{
			"model": t.Model, "prompt": prompt, "stream": false, "options": opts,
		})
	}
	if err != nil {
		return 0, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	// Four vendors, four auth schemes — most of why they need separate adapters
	// rather than one with a different base URL. Bedrock is not a header at
	// all: the whole request is signed.
	switch t.Protocol {
	case "bedrock":
		if err := signBedrock(req, body, t); err != nil {
			return 0, err
		}
	case "gemini":
		geminiAuth(req, t)
	case "anthropic":
		if t.APIKey != "" {
			req.Header.Set("x-api-key", t.APIKey)
		}
		req.Header.Set("anthropic-version", "2023-06-01")
	default:
		auth(req, t)
	}

	resp, err := hc.Do(req)
	if err != nil {
		// Covers the wedged case: the deadline expiring here is exactly what a
		// server that accepts connections but never answers looks like.
		return 0, fmt.Errorf("model did not answer: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))

	switch {
	case resp.StatusCode == http.StatusUnauthorized, resp.StatusCode == http.StatusForbidden:
		return 0, fmt.Errorf("credentials rejected (%d) — check the API key", resp.StatusCode)
	case resp.StatusCode == http.StatusNotFound:
		return 0, fmt.Errorf("model %q not found at this endpoint", t.Model)
	case resp.StatusCode != http.StatusOK:
		return 0, fmt.Errorf("provider returned %d: %s", resp.StatusCode, snippet(raw))
	}

	// Token counts, where the provider reports them. Ollama uses
	// prompt_eval_count; OpenAI-compatible servers use usage.prompt_tokens.
	var env struct {
		PromptEvalCount int `json:"prompt_eval_count"` // ollama
		Usage           struct {
			PromptTokens int `json:"prompt_tokens"` // openai
			InputTokens  int `json:"input_tokens"`  // anthropic
		} `json:"usage"`
		UsageMetadata struct {
			PromptTokenCount int `json:"promptTokenCount"` // gemini
		} `json:"usageMetadata"`
		Error any `json:"error"`
	}
	if err := json.Unmarshal(raw, &env); err != nil {
		return 0, fmt.Errorf("unreadable response: %s", snippet(raw))
	}
	if env.Error != nil {
		return 0, fmt.Errorf("provider reported an error: %v", env.Error)
	}
	// Four vendors, four names for the same number.
	for _, n := range []int{
		env.PromptEvalCount, env.Usage.PromptTokens,
		env.Usage.InputTokens, env.UsageMetadata.PromptTokenCount,
	} {
		if n > 0 {
			return n, nil
		}
	}
	return 0, nil
}

func auth(req *http.Request, t Target) {
	if t.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+t.APIKey)
	}
}

// fillerPrompt builds a prompt of roughly n tokens. Numbered lines rather than
// a repeated word: some servers collapse or cache identical content, and a
// prompt that compresses would prove nothing.
func fillerPrompt(n int) string {
	var b strings.Builder
	b.WriteString("Reply with the single word: ok\n")
	for i := 0; b.Len()/4 < n; i++ {
		fmt.Fprintf(&b, "%d. reference line %d for context window measurement\n", i, i*7919%100003)
	}
	return b.String()
}

func snippet(b []byte) string {
	s := strings.TrimSpace(string(b))
	if len(s) > 180 {
		return s[:180] + "…"
	}
	return s
}
