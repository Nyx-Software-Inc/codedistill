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
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"codedistill/internal/awssig"
)

// Asking Bedrock what it offers.
//
// Two hosts, which is the awkward part. Generation goes to the RUNTIME host,
// bedrock-runtime.<region>.amazonaws.com; the model list lives on the CONTROL
// PLANE, bedrock.<region>.amazonaws.com. A user configures the first, so the
// second is derived from it — asking them to type both would be asking them to
// know how AWS splits its APIs.

// bedrockCreds parses the packed credential. Kept separate from the adapter's
// copy because these two packages do not import each other, and a shared
// "credential parsing" package for one JSON unmarshal would be ceremony.
func bedrockCreds(secret string) (awssig.Credentials, error) {
	var c awssig.Credentials
	s := strings.TrimSpace(secret)
	if s == "" {
		return c, fmt.Errorf("bedrock needs AWS credentials")
	}
	if err := json.Unmarshal([]byte(s), &c); err != nil {
		return c, fmt.Errorf("bedrock credentials are not readable: expected an access key id and secret")
	}
	if !c.Valid() {
		return c, fmt.Errorf("bedrock credentials are incomplete — both an access key id and a secret are required")
	}
	return c, nil
}

// bedrockRegion answers which region to sign for: the credential says so, or
// the endpoint implies it.
func bedrockRegion(t Target, creds awssig.Credentials, host string) (string, error) {
	if creds.Region != "" {
		return creds.Region, nil
	}
	if r := awssig.RegionFromHost(host); r != "" {
		return r, nil
	}
	return "", fmt.Errorf("cannot tell which AWS region %q is in — the endpoint should look like https://bedrock-runtime.us-east-1.amazonaws.com", host)
}

// signBedrock signs a runtime request in place.
func signBedrock(req *http.Request, body []byte, t Target) error {
	creds, err := bedrockCreds(t.APIKey)
	if err != nil {
		return err
	}
	region, err := bedrockRegion(t, creds, req.URL.Host)
	if err != nil {
		return err
	}
	return awssig.Sign(req, body, creds, region, "bedrock", time.Now())
}

// controlPlaneHost turns a runtime endpoint into the control-plane one.
//
// bedrock-runtime.us-east-1.amazonaws.com -> bedrock.us-east-1.amazonaws.com
// Left alone if it does not look like the runtime host: a gateway or a
// PrivateLink name is not ours to rewrite, and a wrong guess is a confusing
// failure rather than a clear one.
func controlPlaneHost(endpoint string) string {
	return strings.Replace(endpoint, "bedrock-runtime.", "bedrock.", 1)
}

type bedrockModelList struct {
	ModelSummaries []struct {
		ModelID                    string   `json:"modelId"`
		ModelName                  string   `json:"modelName"`
		ProviderName               string   `json:"providerName"`
		InputModalities            []string `json:"inputModalities"`
		OutputModalities           []string `json:"outputModalities"`
		InferenceTypesSupported    []string `json:"inferenceTypesSupported"`
		ResponseStreamingSupported bool     `json:"responseStreamingSupported"`
	} `json:"modelSummaries"`
	Message string `json:"message"`
}

// listModelsBedrock returns the text models this account can actually call.
//
// Filtered twice, both times to avoid offering something that fails at run
// time: a model with no TEXT output cannot answer a decomposition, and one
// without ON_DEMAND inference needs a provisioned-throughput ARN rather than
// its plain id, so selecting it here would produce a confusing 400 later.
func listModelsBedrock(ctx context.Context, hc *http.Client, t Target) ([]ModelSummary, error) {
	if hc == nil {
		hc = &http.Client{Timeout: 20 * time.Second}
	}
	creds, err := bedrockCreds(t.APIKey)
	if err != nil {
		return nil, err
	}

	url := strings.TrimRight(controlPlaneHost(t.Endpoint), "/") + "/foundation-models"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")

	region, err := bedrockRegion(t, creds, req.URL.Host)
	if err != nil {
		return nil, err
	}
	// An empty body still hashes: SigV4 signs the hash of no bytes, not nothing.
	if err := awssig.Sign(req, nil, creds, region, "bedrock", time.Now()); err != nil {
		return nil, err
	}

	resp, err := hc.Do(req)
	if err != nil {
		return nil, fmt.Errorf("cannot reach %s: %w", url, err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<20))

	if resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusUnauthorized {
		// AWS's own words matter here. A 403 covers two different problems —
		// a signature it could not verify, and a credential that verified but
		// lacks the permission — and they send you to opposite places. A fixed
		// message would make a signing bug look like an IAM policy question
		// forever.
		var e bedrockModelList
		_ = json.Unmarshal(raw, &e)
		why := strings.TrimSpace(e.Message)
		if why == "" {
			why = resp.Header.Get("x-amzn-ErrorType")
		}
		return nil, fmt.Errorf("AWS rejected the request (%d) in %s: %.200s — the credentials need bedrock:ListFoundationModels and bedrock:InvokeModel",
			resp.StatusCode, region, why)
	}
	if resp.StatusCode != http.StatusOK {
		var e bedrockModelList
		_ = json.Unmarshal(raw, &e)
		msg := e.Message
		if msg == "" {
			msg = string(raw)
		}
		return nil, fmt.Errorf("bedrock returned %d: %.200s", resp.StatusCode, msg)
	}

	var list bedrockModelList
	if err := json.Unmarshal(raw, &list); err != nil {
		return nil, fmt.Errorf("decode bedrock model list: %w", err)
	}

	out := make([]ModelSummary, 0, len(list.ModelSummaries))
	for _, m := range list.ModelSummaries {
		if !contains(m.OutputModalities, "TEXT") {
			continue
		}
		if len(m.InferenceTypesSupported) > 0 && !contains(m.InferenceTypesSupported, "ON_DEMAND") {
			// Needs provisioned throughput, which is called by ARN rather than
			// by this id. Offering it would fail at the first call.
			continue
		}
		// Family carries the vendor, which is the useful distinction in a
		// Bedrock list — the ids are long and the same model appears under
		// several of them.
		out = append(out, ModelSummary{ID: m.ModelID, Family: m.ProviderName})
	}
	return out, nil
}

func contains(xs []string, want string) bool {
	for _, x := range xs {
		if strings.EqualFold(x, want) {
			return true
		}
	}
	return false
}
