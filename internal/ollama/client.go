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

// Package ollama is a thin HTTP client for the local Ollama server.
// Scope for Phase 1: a single GenerateJSON method that requests strict-JSON output
// at temperature 0. Streaming, chat-style, and logprob variants can be added later.
package ollama

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

const (
	DefaultEndpoint   = "http://localhost:11434"
	DefaultModel      = "qwen2.5:7b"
	DefaultEmbedModel = "nomic-embed-text"
	// Headroom for cold-start (Ollama loading the 7B from disk can take
	// 30–60s) plus the larger auto-anchor prompt. Hot subsequent calls
	// finish in ~1–3s; this ceiling only matters when something is wrong.
	DefaultTimeout = 5 * time.Minute
)

// Protocol is the wire API a model server speaks. Both are LOCAL/self-hosted —
// no cloud vendors. "ollama" is the native Ollama API; "openai" is the
// OpenAI-compatible API that vLLM / LM Studio / llama.cpp / an internal model
// server expose, so an enterprise can point at its own server with no extra code.
type Protocol string

const (
	ProtocolOllama    Protocol = "ollama"
	ProtocolOpenAI    Protocol = "openai"
	ProtocolAnthropic Protocol = "anthropic"
	ProtocolGemini    Protocol = "gemini"
	ProtocolBedrock   Protocol = "bedrock"
	ProtocolAzure     Protocol = "azure"
)

// ParseProtocol normalizes a string to a Protocol, defaulting to Ollama.
//
// Defaulting rather than erroring on an unknown value is deliberate: this is
// read from configuration, and a typo should land on the local model rather
// than take the process down.
func ParseProtocol(s string) Protocol {
	switch Protocol(s) {
	case ProtocolOpenAI:
		return ProtocolOpenAI
	case ProtocolAnthropic:
		return ProtocolAnthropic
	case ProtocolGemini:
		return ProtocolGemini
	case ProtocolBedrock:
		return ProtocolBedrock
	case ProtocolAzure:
		return ProtocolAzure
	}
	return ProtocolOllama
}

// WithAPIKey sets the credential. Sent as Authorization: Bearer for OpenAI,
// x-api-key for Anthropic, x-goog-api-key for Gemini — three different headers,
// which is most of why these cannot share one adapter.
func WithAPIKey(k string) Option { return func(c *Client) { c.apiKey = k } }

type Client struct {
	endpoint string
	model    string
	proto    Protocol
	http     *http.Client
	numCtx   int // 0 = leave to the server default (4096, and it truncates)
	// apiKey carries whatever the protocol authenticates with: a bearer token,
	// an x-api-key, an x-goog-api-key — or, for Bedrock, a JSON object holding
	// an AWS access key id, secret and optional session token. Packed into the
	// one field because it is the one that is encrypted at rest and never
	// returned by the API; half a credential in a plaintext column would sit
	// outside that guarantee.
	apiKey string
}

type Option func(*Client)

func WithEndpoint(e string) Option         { return func(c *Client) { c.endpoint = e } }
func WithModel(m string) Option            { return func(c *Client) { c.model = m } }
func WithHTTPClient(h *http.Client) Option { return func(c *Client) { c.http = h } }

// WithContextTokens sets num_ctx — how much of a prompt the model is allowed to
// see.
//
// This is not a tuning knob, it is a correctness setting. Ollama's default is
// 4096 REGARDLESS of what the model supports (qwen2.5:7b holds 32768), and it
// does not error when a prompt exceeds it: it silently truncates from the
// front and answers from what is left. Measured: a 7,116-token prompt was
// accepted, reported prompt_eval_count of exactly 4096, and came back with
// invented role names — because the instructions at the top had been cut away
// and only the data at the bottom survived.
//
// Any caller sending more than ~4k tokens must set this or be quietly lied to.
func WithContextTokens(n int) Option { return func(c *Client) { c.numCtx = n } }

// WithProtocol selects the wire API (ProtocolOllama default, or ProtocolOpenAI
// for a self-hosted OpenAI-compatible server).
func WithProtocol(p Protocol) Option { return func(c *Client) { c.proto = p } }

func New(opts ...Option) *Client {
	c := &Client{
		endpoint: DefaultEndpoint,
		model:    DefaultModel,
		proto:    ProtocolOllama,
		http:     &http.Client{Timeout: DefaultTimeout},
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

func (c *Client) Model() string { return c.model }

// options builds the per-request option map, carrying num_ctx when the caller
// asked for one. Omitted otherwise, so existing behaviour is unchanged for
// callers whose prompts already fit.
func (c *Client) options() map[string]interface{} {
	o := map[string]interface{}{"temperature": 0.0}
	if c.numCtx > 0 {
		o["num_ctx"] = c.numCtx
	}
	return o
}

// Reachable reports whether the model server is responding (a quick GET to
// /api/tags). Used by the classifier reconciliation sweep to avoid re-enqueueing
// work while Ollama is down (e.g. laptop suspended). Uses a short timeout so a
// dead endpoint doesn't hang the sweep.
// Reachable reports whether the model server will actually ANSWER.
//
// It used to call GET /api/tags and return true on 200. That endpoint is served
// from metadata and stays green when inference is dead — observed twice in one
// day after a laptop suspend, where /api/tags answered in 1ms while
// /api/generate never returned. A health check that passes while the thing it
// guards is broken is worse than no health check: the agent's reconcile sweep
// sees a healthy model, queues work, and every item hangs until it times out.
//
// So it exercises generation, with the smallest prompt that proves the path
// works end to end. Bounded tightly: a health check that can itself hang is the
// same bug one level up.
func (c *Client) Reachable(ctx context.Context) bool {
	ctx, cancel := context.WithTimeout(ctx, healthTimeout)
	defer cancel()

	// Still check the cheap endpoint first. A server that is genuinely down
	// fails here in milliseconds, and there is no reason to make the common
	// case pay for a generation.
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.endpoint+"/api/tags", nil)
	if err != nil {
		return false
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return false
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return false
	}
	return c.canGenerate(ctx)
}

// healthTimeout bounds the probe. Generous enough for a cold model load (which
// takes seconds, not minutes) and far short of any real call's deadline.
const healthTimeout = 45 * time.Second

// canGenerate asks for one token. num_predict caps the answer so the probe
// costs a load and a single step rather than a full response.
func (c *Client) canGenerate(ctx context.Context) bool {
	body, err := json.Marshal(generateRequest{
		Model:  c.model,
		Prompt: "ok",
		Stream: false,
		Options: map[string]interface{}{
			"temperature": 0.0,
			"num_predict": 1,
		},
	})
	if err != nil {
		return false
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint+"/api/generate", bytes.NewReader(body))
	if err != nil {
		return false
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return false // includes the deadline expiring, which is the wedged case
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)
	return resp.StatusCode == http.StatusOK
}

type generateRequest struct {
	Model   string                 `json:"model"`
	Prompt  string                 `json:"prompt"`
	Format  string                 `json:"format,omitempty"`
	Stream  bool                   `json:"stream"`
	Options map[string]interface{} `json:"options,omitempty"`
}

type generateResponse struct {
	Response string `json:"response"`
	Done     bool   `json:"done"`
}

// GenerateJSON sends prompt to Ollama with format=json and temperature=0
// using the client's configured model, and returns the raw JSON string.
func (c *Client) GenerateJSON(ctx context.Context, prompt string) (string, error) {
	return c.GenerateJSONWithModel(ctx, "", prompt)
}

// GenerateJSONWithModel is GenerateJSON against an explicit model — used by the
// adversarial reviewer so a project can run review on a stronger local model
// than the high-frequency classifier. An empty model falls back to the client's
// configured default.
func (c *Client) GenerateJSONWithModel(ctx context.Context, model, prompt string) (string, error) {
	if model == "" {
		model = c.model
	}
	switch c.proto {
	case ProtocolOpenAI:
		return c.generateJSONOpenAI(ctx, model, prompt)
	case ProtocolAnthropic:
		return c.generateJSONAnthropic(ctx, model, prompt)
	case ProtocolGemini:
		return c.generateJSONGemini(ctx, model, prompt)
	case ProtocolBedrock:
		return c.generateJSONBedrock(ctx, model, prompt)
	case ProtocolAzure:
		return c.generateJSONAzure(ctx, model, prompt)
	}
	body, err := json.Marshal(generateRequest{
		Model:   model,
		Prompt:  prompt,
		Format:  "json",
		Stream:  false,
		Options: c.options(),
	})
	if err != nil {
		return "", err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint+"/api/generate", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("ollama generate: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("ollama status %d: %s", resp.StatusCode, string(b))
	}

	var gr generateResponse
	if err := json.NewDecoder(resp.Body).Decode(&gr); err != nil {
		return "", fmt.Errorf("decode ollama response: %w", err)
	}
	return gr.Response, nil
}

// --- OpenAI-compatible protocol (self-hosted vLLM / LM Studio / llama.cpp) ---

type chatRequest struct {
	Model          string            `json:"model"`
	Messages       []chatMessage     `json:"messages"`
	Temperature    float64           `json:"temperature"`
	Stream         bool              `json:"stream"`
	ResponseFormat map[string]string `json:"response_format,omitempty"`
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatResponse struct {
	Choices []struct {
		Message chatMessage `json:"message"`
	} `json:"choices"`
}

// generateJSONOpenAI runs a JSON-mode completion against an OpenAI-compatible
// endpoint (POST {endpoint}/v1/chat/completions). response_format json_object
// asks the server for strict JSON (vLLM/LM Studio honor it; servers that don't
// still return the JSON the prompt asks for).
// setAuth puts the credential where the protocol expects it.
//
// This did not exist, and the OpenAI path set no auth header at all — so every
// HOSTED OpenAI-compatible provider (OpenAI itself, OpenRouter, Groq, Together)
// would have answered 401 at the first call. It went unnoticed because the
// vendors people actually ran locally, LM Studio and vLLM, need no key.
//
// One function rather than a line per call site: the next adapter that forgets
// is the same bug again.
func (c *Client) setAuth(req *http.Request) {
	if c.apiKey == "" {
		return
	}
	switch c.proto {
	case ProtocolAzure:
		// api-key, NOT Authorization: Bearer. Azure rejects the bearer form.
		req.Header.Set("api-key", c.apiKey)
	case ProtocolAnthropic:
		req.Header.Set("x-api-key", c.apiKey)
	case ProtocolGemini:
		req.Header.Set("x-goog-api-key", c.apiKey)
	default:
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}
}

func (c *Client) generateJSONOpenAI(ctx context.Context, model, prompt string) (string, error) {
	body, err := json.Marshal(chatRequest{
		Model:          model,
		Messages:       []chatMessage{{Role: "user", Content: prompt}},
		Temperature:    0.0,
		Stream:         false,
		ResponseFormat: map[string]string{"type": "json_object"},
	})
	if err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint+"/v1/chat/completions", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	c.setAuth(req)
	resp, err := c.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("openai generate: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("openai status %d: %s", resp.StatusCode, string(b))
	}
	var cr chatResponse
	if err := json.NewDecoder(resp.Body).Decode(&cr); err != nil {
		return "", fmt.Errorf("decode openai response: %w", err)
	}
	if len(cr.Choices) == 0 {
		return "", fmt.Errorf("openai response had no choices")
	}
	return cr.Choices[0].Message.Content, nil
}

type modelsResponse struct {
	Data []struct {
		ID string `json:"id"`
	} `json:"data"`
}

// listModelsOpenAI returns model IDs from an OpenAI-compatible server
// (GET {endpoint}/v1/models).
func (c *Client) listModelsOpenAI(ctx context.Context) ([]string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.endpoint+"/v1/models", nil)
	if err != nil {
		return nil, err
	}
	c.setAuth(req)
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("openai models: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("openai models status %d: %s", resp.StatusCode, string(b))
	}
	var mr modelsResponse
	if err := json.NewDecoder(resp.Body).Decode(&mr); err != nil {
		return nil, fmt.Errorf("decode openai models: %w", err)
	}
	out := make([]string, 0, len(mr.Data))
	for _, m := range mr.Data {
		if m.ID != "" {
			out = append(out, m.ID)
		}
	}
	return out, nil
}

type tagsResponse struct {
	Models []struct {
		Name string `json:"name"`
	} `json:"models"`
}

// ListModels returns the names of the models the configured server exposes
// (Ollama GET /api/tags, or OpenAI-compatible GET /v1/models), so the UI can
// offer a model picker. Never returns nil on success.
func (c *Client) ListModels(ctx context.Context) ([]string, error) {
	if c.proto == ProtocolOpenAI {
		return c.listModelsOpenAI(ctx)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.endpoint+"/api/tags", nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("ollama tags: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("ollama tags status %d: %s", resp.StatusCode, string(b))
	}
	var tr tagsResponse
	if err := json.NewDecoder(resp.Body).Decode(&tr); err != nil {
		return nil, fmt.Errorf("decode ollama tags: %w", err)
	}
	out := make([]string, 0, len(tr.Models))
	for _, m := range tr.Models {
		if m.Name != "" {
			out = append(out, m.Name)
		}
	}
	return out, nil
}

// Generate sends prompt to Ollama and returns the model's free-form
// text response. No format=json — used by the Q&A endpoint where a
// natural-language answer with embedded citation markers is what we
// want. Temperature defaults to 0 for reproducibility on identical
// retrievals.
func (c *Client) Generate(ctx context.Context, prompt string) (string, error) {
	body, err := json.Marshal(generateRequest{
		Model:   c.model,
		Prompt:  prompt,
		Stream:  false,
		Options: c.options(),
	})
	if err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint+"/api/generate", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("ollama generate: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("ollama status %d: %s", resp.StatusCode, string(b))
	}
	var gr generateResponse
	if err := json.NewDecoder(resp.Body).Decode(&gr); err != nil {
		return "", fmt.Errorf("decode ollama response: %w", err)
	}
	return gr.Response, nil
}

type embedRequest struct {
	Model string `json:"model"`
	Input string `json:"input"`
}

type embedResponse struct {
	Embeddings [][]float32 `json:"embeddings"`
}

// Embed produces a single embedding vector for the given input string using
// the client's configured model. Production wiring constructs a separate
// Client with WithModel(DefaultEmbedModel) for this purpose so the
// classifier and embedder don't share a model setting.
//
// Returns an empty slice + nil error when the input is whitespace-only —
// the caller can skip the storage write rather than persist a meaningless
// vector. Errors propagate verbatim; the agent treats embed failures as
// non-fatal and just logs them.
func (c *Client) Embed(ctx context.Context, input string) ([]float32, error) {
	if input == "" {
		return nil, nil
	}
	body, err := json.Marshal(embedRequest{Model: c.model, Input: input})
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint+"/api/embed", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("ollama embed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("ollama embed status %d: %s", resp.StatusCode, string(b))
	}
	var er embedResponse
	if err := json.NewDecoder(resp.Body).Decode(&er); err != nil {
		return nil, fmt.Errorf("decode ollama embed response: %w", err)
	}
	if len(er.Embeddings) == 0 {
		return nil, fmt.Errorf("ollama embed returned no vectors")
	}
	return er.Embeddings[0], nil
}
