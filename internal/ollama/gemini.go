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

// Google Gemini.
//
// Two things make this the easiest adapter here, not the hardest:
//
//   - responseMimeType "application/json" is a real structured-output mode, so
//     the JSON this product depends on everywhere is a request parameter
//     rather than a prompt instruction and a hope.
//   - /v1beta/models reports inputTokenLimit, so a context window is a fact the
//     provider stated. OpenAI publishes neither.
//
// The model name is part of the PATH, not the body — models/gemini-x:generateContent
// — which is why this cannot ride the OpenAI adapter with a different base URL.

type geminiRequest struct {
	Contents         []geminiContent `json:"contents"`
	GenerationConfig geminiGenConfig `json:"generationConfig"`
}

type geminiContent struct {
	Role  string       `json:"role,omitempty"`
	Parts []geminiPart `json:"parts"`
}

type geminiPart struct {
	Text string `json:"text"`
}

type geminiGenConfig struct {
	Temperature      float64 `json:"temperature"`
	ResponseMIMEType string  `json:"responseMimeType,omitempty"`
	MaxOutputTokens  int     `json:"maxOutputTokens,omitempty"`
}

type geminiResponse struct {
	Candidates []struct {
		Content struct {
			Parts []geminiPart `json:"parts"`
		} `json:"content"`
		FinishReason string `json:"finishReason"`
	} `json:"candidates"`
	UsageMetadata struct {
		PromptTokenCount     int `json:"promptTokenCount"`
		CandidatesTokenCount int `json:"candidatesTokenCount"`
	} `json:"usageMetadata"`
	Error *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
		Status  string `json:"status"`
	} `json:"error"`
}

// geminiModelPath builds the per-model endpoint. Gemini accepts an id with or
// without the "models/" prefix depending on where it came from — the list
// endpoint returns "models/gemini-2.0-flash" while a user types
// "gemini-2.0-flash" — so both are normalised rather than one being demanded.
func geminiModelPath(endpoint, model, method string) string {
	model = strings.TrimPrefix(model, "models/")
	return strings.TrimRight(endpoint, "/") + "/models/" + model + ":" + method
}

func (c *Client) generateJSONGemini(ctx context.Context, model, prompt string) (string, error) {
	body, err := json.Marshal(geminiRequest{
		Contents: []geminiContent{{Role: "user", Parts: []geminiPart{{Text: prompt}}}},
		GenerationConfig: geminiGenConfig{
			Temperature: 0.0,
			// A real structured-output mode, not an instruction.
			ResponseMIMEType: "application/json",
		},
	})
	if err != nil {
		return "", err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		geminiModelPath(c.endpoint, model, "generateContent"), bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	// Header rather than the ?key= query parameter: a key in a URL ends up in
	// access logs, proxy logs and error messages.
	if c.apiKey != "" {
		req.Header.Set("x-goog-api-key", c.apiKey)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("gemini generate: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<20))

	var gr geminiResponse
	if err := json.Unmarshal(raw, &gr); err != nil {
		return "", fmt.Errorf("decode gemini response: %w (%.200s)", err, raw)
	}
	// Gemini reports failures in the body with a 200 in some paths, so the
	// error object is checked before the status code.
	if gr.Error != nil {
		return "", fmt.Errorf("gemini: %s (%s)", gr.Error.Message, gr.Error.Status)
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("gemini status %d: %.200s", resp.StatusCode, raw)
	}
	if len(gr.Candidates) == 0 || len(gr.Candidates[0].Content.Parts) == 0 {
		// A blocked or empty candidate is not a transport failure, and saying
		// "no content" beats returning "" and letting a JSON decode fail two
		// layers away with no clue why.
		reason := "no content returned"
		if len(gr.Candidates) > 0 && gr.Candidates[0].FinishReason != "" {
			reason = "finished early: " + gr.Candidates[0].FinishReason
		}
		return "", fmt.Errorf("gemini: %s", reason)
	}
	return gr.Candidates[0].Content.Parts[0].Text, nil
}
