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

package ollama

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// Anthropic Messages API.
//
// Three differences from the OpenAI shape, each of which breaks a naive reuse:
//
//   - Auth is x-api-key, not Authorization: Bearer.
//   - anthropic-version is a REQUIRED header; omitting it is a 400.
//   - max_tokens is REQUIRED, not optional.
//
// And the one that actually matters here: THERE IS NO JSON MODE. Everything in
// this product depends on structured output, and Claude has no
// response_format equivalent. Asking politely in the prompt works most of the
// time, which is the worst property a parser dependency can have.
//
// So JSON is forced with a TOOL. A single tool is declared whose input schema
// is "an object", and tool_choice pins it, which makes the model emit a
// structured argument object rather than prose that resembles one. The
// arguments are the answer. This is the reliable construction, not a clever
// one — a prefilled "{" assistant turn also works and fails differently, and
// "fails differently" is not a property worth designing around.

const anthropicVersion = "2023-06-01"

// anthropicJSONTool is the forcing function. The schema is deliberately open:
// this package cannot know what shape a caller wants, and the point is to move
// the model onto the structured-output path at all, not to validate here.
var anthropicJSONTool = map[string]any{
	"name":        "emit_json",
	"description": "Return the answer as a JSON object.",
	"input_schema": map[string]any{
		"type":                 "object",
		"additionalProperties": true,
	},
}

type anthropicRequest struct {
	Model       string             `json:"model"`
	MaxTokens   int                `json:"max_tokens"`
	Messages    []anthropicMessage `json:"messages"`
	Tools       []map[string]any   `json:"tools,omitempty"`
	ToolChoice  map[string]any     `json:"tool_choice,omitempty"`
	Temperature float64            `json:"temperature"`
}

type anthropicMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type anthropicResponse struct {
	Content []struct {
		Type  string          `json:"type"`
		Text  string          `json:"text"`
		Name  string          `json:"name"`
		Input json.RawMessage `json:"input"`
	} `json:"content"`
	StopReason string `json:"stop_reason"`
	Usage      struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
	} `json:"usage"`
	Error *struct {
		Type    string `json:"type"`
		Message string `json:"message"`
	} `json:"error"`
}

// anthropicMaxTokens bounds the answer. Required by the API, and generous
// because decomposition asks for a hundred-plus structured entries in one
// response — a small cap here would truncate the answer, which is the same
// silent-shortfall failure this codebase already paid for once.
const anthropicMaxTokens = 8192

func (c *Client) generateJSONAnthropic(ctx context.Context, model, prompt string) (string, error) {
	body, err := json.Marshal(anthropicRequest{
		Model:     model,
		MaxTokens: anthropicMaxTokens,
		Messages:  []anthropicMessage{{Role: "user", Content: prompt}},
		Tools:     []map[string]any{anthropicJSONTool},
		// Pinning the tool is what turns "usually JSON" into "structured".
		ToolChoice:  map[string]any{"type": "tool", "name": "emit_json"},
		Temperature: 0.0,
	})
	if err != nil {
		return "", err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		strings.TrimRight(c.endpoint, "/")+"/messages", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("anthropic-version", anthropicVersion)
	if c.apiKey != "" {
		// x-api-key, NOT Authorization: Bearer. A Bearer token here is a 401
		// with a message that does not say why.
		req.Header.Set("x-api-key", c.apiKey)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("anthropic generate: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<20))

	var ar anthropicResponse
	if err := json.Unmarshal(raw, &ar); err != nil {
		return "", fmt.Errorf("decode anthropic response: %w (%.200s)", err, raw)
	}
	if ar.Error != nil {
		return "", fmt.Errorf("anthropic: %s (%s)", ar.Error.Message, ar.Error.Type)
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("anthropic status %d: %.200s", resp.StatusCode, raw)
	}

	// The tool's arguments ARE the answer.
	for _, block := range ar.Content {
		if block.Type == "tool_use" && len(block.Input) > 0 {
			return string(block.Input), nil
		}
	}
	// Falling back to a text block rather than failing: a model that answered
	// in prose despite the forced tool has still said something, and letting
	// the caller's decoder report the real problem beats a generic error here.
	for _, block := range ar.Content {
		if block.Type == "text" && strings.TrimSpace(block.Text) != "" {
			return block.Text, nil
		}
	}
	if ar.StopReason == "max_tokens" {
		// Named explicitly: a truncated structured answer decodes as malformed
		// JSON three layers away, with nothing pointing at the cause.
		return "", fmt.Errorf("anthropic: answer hit the %d-token limit and was cut off", anthropicMaxTokens)
	}
	return "", fmt.Errorf("anthropic: no usable content (stop reason %q)", ar.StopReason)
}
