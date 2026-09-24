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
	"net/url"
	"strings"
)

// Azure OpenAI.
//
// The same models and the same request body as OpenAI — and it is still not
// the OpenAI adapter with a different base URL, for three reasons that each
// produce a different failure:
//
//   - Auth is the `api-key` header. A Bearer token is a 401.
//   - The path names a DEPLOYMENT, not a model. "gpt-4o" is what the model is;
//     the deployment is whatever the person who created it typed, which may be
//     "gpt4o-prod" or "default". Sending a model id where a deployment name
//     belongs is a 404 that reads like the model does not exist.
//   - api-version is a REQUIRED query parameter. Omitting it is a 400.
//
// The enterprise argument is the same as Bedrock's: an organisation on an
// Azure commitment, with data-residency terms already negotiated, can use these
// models when it cannot send anything to api.openai.com.

// azureAPIVersion is the data-plane version this adapter speaks.
//
// Pinned rather than "latest": Azure's versions change request and response
// shapes, and a deployment that silently moved under us is the kind of failure
// that shows up as malformed JSON three layers away. Overridable per provider
// by putting ?api-version= on the endpoint.
const azureAPIVersion = "2024-10-21"

func (c *Client) generateJSONAzure(ctx context.Context, deployment, prompt string) (string, error) {
	body, err := json.Marshal(chatRequest{
		// Azure takes the model from the URL, not the body. Sent anyway
		// because some gateway proxies in front of it expect the field, and it
		// is ignored where it is not.
		Model:          deployment,
		Messages:       []chatMessage{{Role: "user", Content: prompt}},
		Temperature:    0.0,
		Stream:         false,
		ResponseFormat: map[string]string{"type": "json_object"},
	})
	if err != nil {
		return "", err
	}

	u, err := azureURL(c.endpoint, deployment, "chat/completions")
	if err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	c.setAuth(req)

	resp, err := c.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("azure generate: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<20))

	if resp.StatusCode != http.StatusOK {
		if resp.StatusCode == http.StatusNotFound {
			// Named, because this is THE Azure mistake and the raw message does
			// not explain it.
			return "", fmt.Errorf("azure 404 for deployment %q — on Azure this names a DEPLOYMENT you created, not a model id like \"gpt-4o\": %.200s",
				deployment, raw)
		}
		return "", fmt.Errorf("azure status %d: %.300s", resp.StatusCode, raw)
	}

	var cr chatResponse
	if err := json.Unmarshal(raw, &cr); err != nil {
		return "", fmt.Errorf("decode azure response: %w (%.200s)", err, raw)
	}
	if len(cr.Choices) == 0 {
		return "", fmt.Errorf("azure response had no choices")
	}
	return cr.Choices[0].Message.Content, nil
}

// azureURL builds a data-plane URL, preserving an api-version the user pinned
// on the endpoint themselves.
func azureURL(endpoint, deployment, op string) (string, error) {
	base := strings.TrimRight(endpoint, "/")
	u, err := url.Parse(base)
	if err != nil {
		return "", fmt.Errorf("azure endpoint is not a URL: %w", err)
	}
	version := u.Query().Get("api-version")
	if version == "" {
		version = azureAPIVersion
	}
	u.RawQuery = ""
	// Tolerate an endpoint given with or without the /openai suffix: both
	// appear in Azure's own documentation and portal.
	p := strings.TrimSuffix(u.Path, "/")
	if !strings.HasSuffix(p, "/openai") {
		p += "/openai"
	}
	u.Path = p + "/deployments/" + deployment + "/" + op
	q := url.Values{}
	q.Set("api-version", version)
	u.RawQuery = q.Encode()
	return u.String(), nil
}
