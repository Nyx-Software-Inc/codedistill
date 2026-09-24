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
	"net/url"
	"strings"
)

// Azure URLs.
//
// Everything Azure-shaped is here rather than inline, because all of it is the
// same trap: api-version is required, the path names a DEPLOYMENT rather than a
// model, and the endpoint is written with or without its /openai suffix
// depending on which page of the portal you copied it from.

const azureProbeAPIVersion = "2024-10-21"

// azureBase normalises an endpoint to ".../openai" and returns the api-version
// the user pinned, if they pinned one.
func azureBase(endpoint string) (*url.URL, string) {
	u, err := url.Parse(strings.TrimRight(endpoint, "/"))
	if err != nil {
		return nil, azureProbeAPIVersion
	}
	version := u.Query().Get("api-version")
	if version == "" {
		version = azureProbeAPIVersion
	}
	u.RawQuery = ""
	p := strings.TrimSuffix(u.Path, "/")
	if !strings.HasSuffix(p, "/openai") {
		p += "/openai"
	}
	u.Path = p
	return u, version
}

// azureDeploymentsURL lists what this resource actually has.
//
// Deployments, not models: a caller may only name something that was deployed,
// and the names are chosen by whoever deployed them. Listing models would offer
// ids that 404.
func azureDeploymentsURL(endpoint string) string {
	u, version := azureBase(endpoint)
	if u == nil {
		return endpoint
	}
	u.Path += "/deployments"
	q := url.Values{}
	q.Set("api-version", version)
	u.RawQuery = q.Encode()
	return u.String()
}

func azureChatURL(endpoint, deployment string) string {
	u, version := azureBase(endpoint)
	if u == nil {
		return endpoint
	}
	u.Path += "/deployments/" + deployment + "/chat/completions"
	q := url.Values{}
	q.Set("api-version", version)
	u.RawQuery = q.Encode()
	return u.String()
}
