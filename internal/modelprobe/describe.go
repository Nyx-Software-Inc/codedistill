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

package modelprobe

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Description is what a provider says about a model when asked.
//
// Asking beats measuring. The first version of this package proved a context
// window empirically, by sending an oversized prompt and reading back how much
// was consumed — which costs twenty minutes for a 16k window on local hardware
// and produced a false "truncates" verdict when its own deadline fired.
// /api/show answers the same question exactly, in milliseconds.
//
// One distinction this does NOT collapse: ContextLength is what the MODEL
// supports. It is not what the server will use. Ollama defaults num_ctx to 4096
// regardless, and truncates silently past it — which is precisely the bug that
// corrupted prompts here for a year. So the ceiling tells you what you may ASK
// for; it is only true if the caller then sets num_ctx.
type Description struct {
	// ContextLength is the model's maximum, 0 when the provider does not say.
	ContextLength int `json:"context_length,omitempty"`

	Family        string   `json:"family,omitempty"`
	ParameterSize string   `json:"parameter_size,omitempty"`
	Quantization  string   `json:"quantization,omitempty"`
	Capabilities  []string `json:"capabilities,omitempty"`

	// Source records how this was learned, so a UI can distinguish a fact the
	// server stated from a number a human typed.
	Source string `json:"source,omitempty"` // "api/show" | "models" | ""
}

// Describe asks the provider about a model. A provider that does not answer is
// not an error: it means the caller must fall back to what it was told.
func Describe(ctx context.Context, hc *http.Client, t Target) (Description, error) {
	if hc == nil {
		hc = &http.Client{Timeout: 20 * time.Second}
	}
	switch t.Protocol {
	case "openai":
		return describeOpenAI(ctx, hc, t)
	case "gemini":
		return describeGemini(ctx, hc, t)
	case "anthropic":
		// Anthropic's /v1/models returns ids and display names, with no context
		// window. Reported as unknown rather than filled from a table that goes
		// stale — the same reasoning as OpenAI.
		return Description{Source: "models"}, nil
	case "bedrock":
		// ListFoundationModels reports modalities and inference types but not
		// context length. Unknown rather than guessed, for the same reason.
		return Description{Source: "models"}, nil
	case "azure":
		// The deployments list gives a model name per deployment but no
		// context window. Unknown rather than filled from a table.
		return Description{Source: "models"}, nil
	}
	return describeOllama(ctx, hc, t)
}

func describeOllama(ctx context.Context, hc *http.Client, t Target) (Description, error) {
	body, _ := json.Marshal(map[string]string{"name": t.Model})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		strings.TrimRight(t.Endpoint, "/")+"/api/show", bytes.NewReader(body))
	if err != nil {
		return Description{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	auth(req, t)

	resp, err := hc.Do(req)
	if err != nil {
		return Description{}, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if resp.StatusCode == http.StatusNotFound {
		return Description{}, fmt.Errorf("model %q not found at this endpoint", t.Model)
	}
	if resp.StatusCode != http.StatusOK {
		return Description{}, fmt.Errorf("provider returned %d", resp.StatusCode)
	}

	var out struct {
		ModelInfo    map[string]any `json:"model_info"`
		Capabilities []string       `json:"capabilities"`
		Details      struct {
			Family            string `json:"family"`
			ParameterSize     string `json:"parameter_size"`
			QuantizationLevel string `json:"quantization_level"`
		} `json:"details"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return Description{}, fmt.Errorf("unreadable response: %w", err)
	}

	d := Description{
		Family: out.Details.Family, ParameterSize: out.Details.ParameterSize,
		Quantization: out.Details.QuantizationLevel, Capabilities: out.Capabilities,
		Source: "api/show",
	}
	// The key is architecture-prefixed — qwen2.context_length, llama.context_length
	// — so it is found by suffix rather than by guessing the family name.
	for k, v := range out.ModelInfo {
		if strings.HasSuffix(k, ".context_length") {
			if n, ok := asInt(v); ok {
				d.ContextLength = n
			}
			break
		}
	}
	return d, nil
}

func describeOpenAI(ctx context.Context, hc *http.Client, t Target) (Description, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		strings.TrimRight(t.Endpoint, "/")+"/models", nil)
	if err != nil {
		return Description{}, err
	}
	auth(req, t)
	resp, err := hc.Do(req)
	if err != nil {
		return Description{}, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if resp.StatusCode != http.StatusOK {
		return Description{}, fmt.Errorf("provider returned %d", resp.StatusCode)
	}

	// The OpenAI schema has no context field. Servers that implement it — vLLM,
	// LM Studio, llama.cpp — add one under various names, so several are tried
	// and absence is reported honestly rather than guessed at.
	var out struct {
		Data []map[string]any `json:"data"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return Description{}, fmt.Errorf("unreadable response: %w", err)
	}
	d := Description{Source: "models"}
	for _, m := range out.Data {
		if id, _ := m["id"].(string); id != t.Model {
			continue
		}
		for _, key := range []string{"context_length", "max_model_len", "max_context_length", "context_window"} {
			if n, ok := asInt(m[key]); ok && n > 0 {
				d.ContextLength = n
				return d, nil
			}
		}
		return d, nil
	}
	return d, fmt.Errorf("model %q not listed at this endpoint", t.Model)
}

// describeGemini reads inputTokenLimit, which Gemini publishes per model. It
// is the only cloud vendor here that states a context window, so it is the only
// one where the probe can VERIFY rather than take a typed number on trust.
func describeGemini(ctx context.Context, hc *http.Client, t Target) (Description, error) {
	name := strings.TrimPrefix(t.Model, "models/")
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		strings.TrimRight(t.Endpoint, "/")+"/models/"+name, nil)
	if err != nil {
		return Description{}, err
	}
	geminiAuth(req, t)
	resp, err := hc.Do(req)
	if err != nil {
		return Description{}, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if resp.StatusCode == http.StatusNotFound {
		return Description{}, fmt.Errorf("model %q not found at this endpoint", t.Model)
	}
	if resp.StatusCode != http.StatusOK {
		return Description{}, fmt.Errorf("provider returned %d", resp.StatusCode)
	}
	var out struct {
		Name                       string   `json:"name"`
		DisplayName                string   `json:"displayName"`
		InputTokenLimit            int      `json:"inputTokenLimit"`
		SupportedGenerationMethods []string `json:"supportedGenerationMethods"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return Description{}, fmt.Errorf("unreadable response: %w", err)
	}
	return Description{
		ContextLength: out.InputTokenLimit,
		Family:        out.DisplayName,
		Capabilities:  out.SupportedGenerationMethods,
		Source:        "models",
	}, nil
}

// geminiAuth uses the header rather than ?key=, so a credential does not end up
// in access logs, proxy logs and error messages.
func geminiAuth(req *http.Request, t Target) {
	if t.APIKey != "" {
		req.Header.Set("x-goog-api-key", t.APIKey)
	}
}

// supportsGenerate filters out embedding-only models.
func supportsGenerate(methods []string) bool {
	if len(methods) == 0 {
		return true // said nothing; do not hide it
	}
	for _, m := range methods {
		if strings.Contains(m, "generateContent") {
			return true
		}
	}
	return false
}

func asInt(v any) (int, bool) {
	switch n := v.(type) {
	case float64:
		return int(n), true
	case int:
		return n, true
	case json.Number:
		i, err := n.Int64()
		return int(i), err == nil
	}
	return 0, false
}

// ModelSummary is one model a provider offers.
type ModelSummary struct {
	ID            string `json:"id"`
	ContextLength int    `json:"context_length,omitempty"` // 0 = the provider did not say
	ParameterSize string `json:"parameter_size,omitempty"`
	Family        string `json:"family,omitempty"`
}

// ListModels asks a provider what it offers.
//
// Discovery matters more than it looks: a provider is configured by typing a
// model string exactly right, and "qwen2.5:7b" versus "qwen2.5-7b" fails at
// call time with a 404 rather than at the moment of typing. Offering the real
// list removes a whole class of mistake.
//
// Context lengths are NOT fetched here. Ollama reports them only per-model via
// /api/show, and describing twenty models to populate a dropdown would mean
// twenty round trips on every keystroke. The chosen model is described once
// it is chosen.
func ListModels(ctx context.Context, hc *http.Client, t Target) ([]ModelSummary, error) {
	if hc == nil {
		hc = &http.Client{Timeout: 20 * time.Second}
	}
	if t.Protocol == "bedrock" {
		// A different host, a signed request and a different response shape —
		// too little in common to fold into the generic path below.
		return listModelsBedrock(ctx, hc, t)
	}
	url := strings.TrimRight(t.Endpoint, "/")
	switch t.Protocol {
	case "azure":
		// DEPLOYMENTS, not models: what a caller may actually name is what
		// someone created in this resource, and the two lists differ.
		url = azureDeploymentsURL(t.Endpoint)
	case "openai", "anthropic", "gemini":
		url += "/models"
	default:
		url += "/api/tags"
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	switch t.Protocol {
	case "gemini":
		geminiAuth(req, t)
	case "anthropic":
		req.Header.Set("x-api-key", t.APIKey)
		req.Header.Set("anthropic-version", "2023-06-01")
	case "azure":
		req.Header.Set("api-key", t.APIKey)
	default:
		auth(req, t)
	}
	resp, err := hc.Do(req)
	if err != nil {
		return nil, fmt.Errorf("cannot reach %s: %w", t.Endpoint, err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return nil, fmt.Errorf("credentials rejected (%d) — check the API key", resp.StatusCode)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("provider returned %d", resp.StatusCode)
	}

	if t.Protocol == "gemini" {
		// Gemini lists models WITH their input limits, so a picker can show
		// the real window beside each name instead of asking per model.
		var out struct {
			Models []struct {
				Name                       string   `json:"name"`
				DisplayName                string   `json:"displayName"`
				InputTokenLimit            int      `json:"inputTokenLimit"`
				SupportedGenerationMethods []string `json:"supportedGenerationMethods"`
			} `json:"models"`
		}
		if err := json.Unmarshal(raw, &out); err != nil {
			return nil, fmt.Errorf("unreadable response: %w", err)
		}
		var models []ModelSummary
		for _, m := range out.Models {
			// Embedding-only models cannot answer a prompt; offering them in a
			// generation picker is a guaranteed failure one step later.
			if !supportsGenerate(m.SupportedGenerationMethods) {
				continue
			}
			models = append(models, ModelSummary{
				ID:            strings.TrimPrefix(m.Name, "models/"),
				ContextLength: m.InputTokenLimit,
				Family:        m.DisplayName,
			})
		}
		return models, nil
	}

	if t.Protocol == "anthropic" {
		var out struct {
			Data []struct {
				ID          string `json:"id"`
				DisplayName string `json:"display_name"`
			} `json:"data"`
		}
		if err := json.Unmarshal(raw, &out); err != nil {
			return nil, fmt.Errorf("unreadable response: %w", err)
		}
		var models []ModelSummary
		for _, m := range out.Data {
			// No context length: Anthropic does not publish it here.
			models = append(models, ModelSummary{ID: m.ID, Family: m.DisplayName})
		}
		return models, nil
	}

	if t.Protocol == "openai" {
		var out struct {
			Data []map[string]any `json:"data"`
		}
		if err := json.Unmarshal(raw, &out); err != nil {
			return nil, fmt.Errorf("unreadable response: %w", err)
		}
		var models []ModelSummary
		for _, m := range out.Data {
			id, _ := m["id"].(string)
			if id == "" {
				continue
			}
			s := ModelSummary{ID: id}
			for _, key := range []string{"context_length", "max_model_len", "max_context_length"} {
				if n, ok := asInt(m[key]); ok && n > 0 {
					s.ContextLength = n
					break
				}
			}
			models = append(models, s)
		}
		return models, nil
	}

	var out struct {
		Models []struct {
			Name    string `json:"name"`
			Details struct {
				Family        string `json:"family"`
				ParameterSize string `json:"parameter_size"`
			} `json:"details"`
		} `json:"models"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("unreadable response: %w", err)
	}
	var models []ModelSummary
	for _, m := range out.Models {
		models = append(models, ModelSummary{
			ID: m.Name, Family: m.Details.Family, ParameterSize: m.Details.ParameterSize,
		})
	}
	return models, nil
}
