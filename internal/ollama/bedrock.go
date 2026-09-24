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
	"time"

	"codedistill/internal/awssig"
)

// AWS Bedrock, via the Converse API.
//
// The reason this exists is not model access — the same Claude models are a
// direct API call away. It is that an enterprise with an AWS commitment and a
// "nothing leaves our account" policy cannot make that call, and can make this
// one: same models, their account, their bill, optionally never leaving their
// VPC over PrivateLink.
//
// Two things make it unlike every other adapter here:
//
//   - Auth is a SIGNED REQUEST, not a header. See sigv4.go.
//   - The credential is three fields, not one. It rides in the same encrypted
//     secret as every other provider's key, as JSON, so nothing about storage
//     or encryption had to change to carry it.
//
// CONVERSE, not InvokeModel. InvokeModel takes each vendor's native body —
// Anthropic's shape for Claude, Meta's for Llama — which would mean a payload
// zoo keyed by model id prefix, and a new one every time AWS adds a vendor.
// Converse is one request and one response across all of them, and it supports
// tool use, which is how JSON gets forced here exactly as it does for Anthropic
// direct.

// bedrockMaxTokens bounds the answer, for the same reason as Anthropic's:
// decomposition asks for a hundred-plus structured entries in one response, and
// a small cap truncates it into malformed JSON three layers away.
const bedrockMaxTokens = 8192

type bedrockRequest struct {
	Messages        []bedrockMessage `json:"messages"`
	InferenceConfig bedrockInference `json:"inferenceConfig"`
	ToolConfig      *bedrockTools    `json:"toolConfig,omitempty"`
}

type bedrockMessage struct {
	Role    string           `json:"role"`
	Content []bedrockContent `json:"content"`
}

type bedrockContent struct {
	Text string `json:"text,omitempty"`
}

type bedrockInference struct {
	MaxTokens   int     `json:"maxTokens"`
	Temperature float64 `json:"temperature"`
}

type bedrockTools struct {
	Tools      []map[string]any `json:"tools"`
	ToolChoice map[string]any   `json:"toolChoice,omitempty"`
}

// bedrockJSONTool mirrors the Anthropic forcing function: one tool whose input
// schema is "an object", pinned by toolChoice, so the model emits a structured
// argument rather than prose that resembles one.
var bedrockJSONTool = map[string]any{
	"toolSpec": map[string]any{
		"name":        "emit_json",
		"description": "Return the answer as a JSON object.",
		"inputSchema": map[string]any{
			"json": map[string]any{
				"type":                 "object",
				"additionalProperties": true,
			},
		},
	},
}

type bedrockResponse struct {
	Output struct {
		Message struct {
			Role    string `json:"role"`
			Content []struct {
				Text    string `json:"text"`
				ToolUse *struct {
					Name  string          `json:"name"`
					Input json.RawMessage `json:"input"`
				} `json:"toolUse"`
			} `json:"content"`
		} `json:"message"`
	} `json:"output"`
	StopReason string `json:"stopReason"`
	Usage      struct {
		InputTokens  int `json:"inputTokens"`
		OutputTokens int `json:"outputTokens"`
	} `json:"usage"`
	// Bedrock reports errors as a bare message with a non-200 status and an
	// x-amzn-ErrorType header, rather than a typed body like Anthropic's.
	Message string `json:"message"`
}

// bedrockCreds reads the three-field credential out of the client's single
// encrypted secret.
//
// Packed as JSON rather than given its own columns: the secret is already
// encrypted at rest and never returned by the API, and adding plaintext columns
// for an access key id would have put half a credential outside that guarantee.
func bedrockCreds(secret string) (awssig.Credentials, error) {
	var c awssig.Credentials
	s := strings.TrimSpace(secret)
	if s == "" {
		return c, fmt.Errorf("bedrock needs AWS credentials — set them on the provider")
	}
	if err := json.Unmarshal([]byte(s), &c); err != nil {
		return c, fmt.Errorf("bedrock credentials are not readable: expected an access key id and secret")
	}
	if !c.Valid() {
		return c, fmt.Errorf("bedrock credentials are incomplete — both an access key id and a secret are required")
	}
	return c, nil
}

func (c *Client) generateJSONBedrock(ctx context.Context, model, prompt string) (string, error) {
	creds, err := bedrockCreds(c.apiKey)
	if err != nil {
		return "", err
	}

	// Tool-forced first. Not every model on Bedrock supports a pinned tool —
	// some Llama and Mistral variants reject toolConfig outright — so a
	// rejection falls back to asking, once, and says so in the error if that
	// also fails. Silently degrading from "structured" to "usually JSON" is the
	// failure mode this whole design exists to avoid, so it is not silent.
	out, err := c.bedrockConverse(ctx, model, prompt, creds, true)
	if err != nil && isBedrockToolRefusal(err) {
		out, err = c.bedrockConverse(ctx, model, prompt, creds, false)
		if err != nil {
			return "", fmt.Errorf("%w (this model also refused forced tool use, so structured output cannot be guaranteed for it)", err)
		}
	}
	return out, err
}

func (c *Client) bedrockConverse(ctx context.Context, model, prompt string, creds awssig.Credentials, forceTool bool) (string, error) {
	reqBody := bedrockRequest{
		Messages: []bedrockMessage{{
			Role:    "user",
			Content: []bedrockContent{{Text: prompt}},
		}},
		InferenceConfig: bedrockInference{MaxTokens: bedrockMaxTokens, Temperature: 0},
	}
	if forceTool {
		reqBody.ToolConfig = &bedrockTools{
			Tools:      []map[string]any{bedrockJSONTool},
			ToolChoice: map[string]any{"tool": map[string]any{"name": "emit_json"}},
		}
	}
	body, err := json.Marshal(reqBody)
	if err != nil {
		return "", err
	}

	// The model id goes in the PATH, and Bedrock ids contain a colon
	// ("...-v2:0"). See canonicalURI: that colon must be percent-encoded when
	// signing while staying literal on the wire, which is the likeliest source
	// of a 403 here.
	url := strings.TrimRight(c.endpoint, "/") + "/model/" + model + "/converse"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	// The credential may state the region outright; otherwise the endpoint
	// implies it.
	region := creds.Region
	if region == "" {
		region = awssig.RegionFromHost(req.URL.Host)
	}
	if region == "" {
		return "", fmt.Errorf("cannot tell which AWS region %q is in — the endpoint should look like https://bedrock-runtime.us-east-1.amazonaws.com, or set %q in the credentials", req.URL.Host, "region")
	}
	if err := awssig.Sign(req, body, creds, region, "bedrock", time.Now()); err != nil {
		return "", err
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("bedrock generate: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<20))

	if resp.StatusCode != http.StatusOK {
		var e bedrockResponse
		_ = json.Unmarshal(raw, &e)
		kind := resp.Header.Get("x-amzn-ErrorType")
		msg := e.Message
		if msg == "" {
			msg = string(raw)
		}
		if resp.StatusCode == http.StatusForbidden {
			// Named explicitly: a signing bug and a permissions problem produce
			// the same status, and the difference is where you go looking.
			return "", fmt.Errorf("bedrock 403 (%s): %.300s — either the credentials lack bedrock:InvokeModel, or the model is not enabled in this region's model access", kind, msg)
		}
		return "", fmt.Errorf("bedrock status %d (%s): %.300s", resp.StatusCode, kind, msg)
	}

	var br bedrockResponse
	if err := json.Unmarshal(raw, &br); err != nil {
		return "", fmt.Errorf("decode bedrock response: %w (%.200s)", err, raw)
	}

	// The tool's arguments ARE the answer.
	for _, block := range br.Output.Message.Content {
		if block.ToolUse != nil && len(block.ToolUse.Input) > 0 {
			return string(block.ToolUse.Input), nil
		}
	}
	for _, block := range br.Output.Message.Content {
		if strings.TrimSpace(block.Text) != "" {
			return block.Text, nil
		}
	}
	if br.StopReason == "max_tokens" {
		return "", fmt.Errorf("bedrock: answer hit the %d-token limit and was cut off", bedrockMaxTokens)
	}
	return "", fmt.Errorf("bedrock: no usable content (stop reason %q)", br.StopReason)
}

// isBedrockToolRefusal spots a model that cannot be pinned to a tool, as
// opposed to a real failure.
//
// Matched on text because Bedrock reports it as a generic ValidationException;
// there is no code that distinguishes it. Deliberately narrow — a broader match
// would swallow genuine validation errors and retry them pointlessly.
func isBedrockToolRefusal(err error) bool {
	s := strings.ToLower(err.Error())
	if !strings.Contains(s, "validation") && !strings.Contains(s, "400") {
		return false
	}
	return strings.Contains(s, "tool")
}
