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
	ProtocolOllama Protocol = "ollama"
	ProtocolOpenAI Protocol = "openai"
)

// ParseProtocol normalizes a string to a Protocol, defaulting to Ollama.
func ParseProtocol(s string) Protocol {
	if Protocol(s) == ProtocolOpenAI {
		return ProtocolOpenAI
	}
	return ProtocolOllama
}

type Client struct {
	endpoint string
	model    string
	proto    Protocol
	http     *http.Client
}

type Option func(*Client)

func WithEndpoint(e string) Option         { return func(c *Client) { c.endpoint = e } }
func WithModel(m string) Option            { return func(c *Client) { c.model = m } }
func WithHTTPClient(h *http.Client) Option { return func(c *Client) { c.http = h } }

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

// Reachable reports whether the model server is responding (a quick GET to
// /api/tags). Used by the classifier reconciliation sweep to avoid re-enqueueing
// work while Ollama is down (e.g. laptop suspended). Uses a short timeout so a
// dead endpoint doesn't hang the sweep.
func (c *Client) Reachable(ctx context.Context) bool {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.endpoint+"/api/tags", nil)
	if err != nil {
		return false
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return false
	}
	_ = resp.Body.Close()
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
	if c.proto == ProtocolOpenAI {
		return c.generateJSONOpenAI(ctx, model, prompt)
	}
	body, err := json.Marshal(generateRequest{
		Model:  model,
		Prompt: prompt,
		Format: "json",
		Stream: false,
		Options: map[string]interface{}{
			"temperature": 0.0,
		},
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
		Model:  c.model,
		Prompt: prompt,
		Stream: false,
		Options: map[string]interface{}{
			"temperature": 0.0,
		},
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
