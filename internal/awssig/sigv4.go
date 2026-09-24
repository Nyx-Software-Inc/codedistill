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

// Package awssig signs requests with AWS Signature Version 4.
//
// Its own package because TWO callers need it and they are not in the same
// place: the Bedrock adapter signs a generate against the runtime host, and
// model discovery signs a list against the CONTROL-PLANE host. Leaving it in
// the adapter would have meant probing importing the adapter to ask AWS what
// models exist.
package awssig

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
)

// AWS Signature Version 4.
//
// Written here rather than taken from Amazon's official Go SDK, which would
// pull a large dependency tree — smithy, the config loader, the credential
// providers, the endpoint resolver — to sign one kind of request. This codebase
// left out WebP support rather than take an image library; a whole cloud SDK
// for an HMAC chain is the same trade, larger.
//
// (That SDK is deliberately not named here: its module path is one of the
// markers the CE leak check greps for, because the paid S3 blobstore imports
// it. Naming it in prose failed a release.)
//
// The algorithm is fully specified and does not drift, which is what makes
// hand-rolling it reasonable. What DOES bite is the detail: every step below
// that looks fussy is a step where getting it wrong produces the same opaque
// 403 "The request signature we calculated does not match", with no indication
// of which part was wrong. They are commented accordingly.

// Credentials is what Bedrock needs instead of a bearer token.
//
// Three fields, not one, which is why this arrived with a credential-shape
// change rather than just an adapter: the provider dialog and the encrypted
// secret both assumed a single string.
type Credentials struct {
	AccessKeyID     string `json:"access_key_id"`
	SecretAccessKey string `json:"secret_access_key"`
	// SessionToken is set for temporary credentials (STS, SSO, an assumed
	// role). Empty for a long-lived IAM user key. When present it must ALSO go
	// in a signed header, not only in the request.
	SessionToken string `json:"session_token,omitempty"`

	// Region overrides what the endpoint implies. Normally empty — the
	// endpoint carries it — but a proxy or a private hostname may not look
	// like AWS at all, and signing with an empty region produces a 403 that
	// blames the signature rather than the configuration.
	Region string `json:"region,omitempty"`
}

func (c Credentials) Valid() bool {
	return c.AccessKeyID != "" && c.SecretAccessKey != ""
}

// Sign signs an HTTP request in place.
//
// payload must be the exact bytes of the body: the hash of the body is part of
// what is signed, so a body read or rewritten after signing invalidates it.
func Sign(req *http.Request, payload []byte, creds Credentials, region, service string, now time.Time) error {
	if !creds.Valid() {
		return fmt.Errorf("aws credentials are incomplete — both an access key id and a secret are required")
	}
	amzDate := now.UTC().Format("20060102T150405Z")
	dateStamp := now.UTC().Format("20060102")

	// Host must be an explicit header: it is always signed, and Go otherwise
	// sets it from the URL at write time where the signer cannot see it.
	req.Header.Set("Host", req.URL.Host)
	req.Header.Set("X-Amz-Date", amzDate)
	if creds.SessionToken != "" {
		// Signed as well as sent. Setting it after signing is a 403.
		req.Header.Set("X-Amz-Security-Token", creds.SessionToken)
	}

	payloadHash := sha256Hex(payload)
	req.Header.Set("X-Amz-Content-Sha256", payloadHash)

	// ── Canonical request ───────────────────────────────────────────────────
	signedHeaders, canonicalHeaders := canonicalHeaders(req)
	canonicalRequest := strings.Join([]string{
		req.Method,
		canonicalURI(req.URL),
		canonicalQuery(req.URL),
		canonicalHeaders,
		signedHeaders,
		payloadHash,
	}, "\n")

	// ── String to sign ──────────────────────────────────────────────────────
	scope := strings.Join([]string{dateStamp, region, service, "aws4_request"}, "/")
	stringToSign := strings.Join([]string{
		"AWS4-HMAC-SHA256",
		amzDate,
		scope,
		sha256Hex([]byte(canonicalRequest)),
	}, "\n")

	// ── Signing key: a chain of HMACs, each keyed by the previous ───────────
	k := hmacSHA256([]byte("AWS4"+creds.SecretAccessKey), dateStamp)
	k = hmacSHA256(k, region)
	k = hmacSHA256(k, service)
	k = hmacSHA256(k, "aws4_request")
	signature := hex.EncodeToString(hmacSHA256(k, stringToSign))

	req.Header.Set("Authorization", fmt.Sprintf(
		"AWS4-HMAC-SHA256 Credential=%s/%s, SignedHeaders=%s, Signature=%s",
		creds.AccessKeyID, scope, signedHeaders, signature))
	return nil
}

// canonicalHeaders returns the signed-header list and the canonical block.
//
// Lowercased names, sorted, values trimmed and inner runs of whitespace
// collapsed. The collapsing matters: a header the transport normalises
// differently than the signer did produces a mismatch that looks like a bad
// secret.
func canonicalHeaders(req *http.Request) (string, string) {
	names := make([]string, 0, len(req.Header)+1)
	values := map[string]string{}
	for name, vs := range req.Header {
		l := strings.ToLower(name)
		switch l {
		case "authorization", "content-length", "user-agent":
			// Excluded: set or rewritten by the transport after signing.
			continue
		}
		names = append(names, l)
		parts := make([]string, 0, len(vs))
		for _, v := range vs {
			parts = append(parts, strings.Join(strings.Fields(v), " "))
		}
		values[l] = strings.Join(parts, ",")
	}
	if _, ok := values["host"]; !ok {
		names = append(names, "host")
		values["host"] = req.URL.Host
	}
	sort.Strings(names)

	var b strings.Builder
	for _, n := range names {
		b.WriteString(n)
		b.WriteByte(':')
		b.WriteString(values[n])
		b.WriteByte('\n')
	}
	return strings.Join(names, ";"), b.String()
}

// canonicalURI percent-encodes each path segment, leaving the separators.
//
// Bedrock model ids contain dots and colons — "anthropic.claude-3-5-sonnet-
// 20241022-v2:0" — and the colon MUST be encoded in the canonical path while
// remaining literal in the URL that is sent. Getting this wrong is the single
// most likely cause of a 403 here.
func canonicalURI(u *url.URL) string {
	p := u.EscapedPath()
	if p == "" {
		return "/"
	}
	segs := strings.Split(p, "/")
	for i, s := range segs {
		// EscapedPath leaves sub-delims and ':' alone; SigV4 wants them
		// encoded. Decode first so an already-escaped segment is not
		// double-encoded.
		if dec, err := url.PathUnescape(s); err == nil {
			s = dec
		}
		segs[i] = awsURIEncode(s, false)
	}
	return strings.Join(segs, "/")
}

func canonicalQuery(u *url.URL) string {
	q := u.Query()
	keys := make([]string, 0, len(q))
	for k := range q {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		vs := append([]string(nil), q[k]...)
		sort.Strings(vs)
		for _, v := range vs {
			parts = append(parts, awsURIEncode(k, true)+"="+awsURIEncode(v, true))
		}
	}
	return strings.Join(parts, "&")
}

// awsURIEncode is RFC 3986 encoding with AWS's exceptions: unreserved
// characters stay, everything else is percent-encoded uppercase. Slashes are
// preserved only in paths.
func awsURIEncode(s string, encodeSlash bool) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		ch := s[i]
		switch {
		case (ch >= 'A' && ch <= 'Z') || (ch >= 'a' && ch <= 'z') ||
			(ch >= '0' && ch <= '9') || ch == '-' || ch == '_' || ch == '.' || ch == '~':
			b.WriteByte(ch)
		case ch == '/' && !encodeSlash:
			b.WriteByte(ch)
		default:
			fmt.Fprintf(&b, "%%%02X", ch)
		}
	}
	return b.String()
}

func hmacSHA256(key []byte, data string) []byte {
	h := hmac.New(sha256.New, key)
	h.Write([]byte(data))
	return h.Sum(nil)
}

func sha256Hex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// regionFromHost pulls the region out of an AWS endpoint host.
//
// Derived rather than stored as another field: the region is already in the
// endpoint the user configured, and two places to say it is one place to get
// them out of step.
//
// Scans for a label SHAPED like a region rather than assuming
// service.region.amazonaws.com, because the deployment Bedrock exists for
// often does not look like that. A PrivateLink endpoint is
// "vpce-0a1b.bedrock-runtime.us-east-1.vpce.amazonaws.com" — the region is in
// there, just not where the simple form puts it.
func RegionFromHost(host string) string {
	if i := strings.Index(host, ":"); i >= 0 {
		host = host[:i]
	}
	for _, label := range strings.Split(strings.ToLower(host), ".") {
		if looksLikeRegion(label) {
			return label
		}
	}
	return ""
}

// looksLikeRegion matches the AWS region shape: a two-letter area, optional
// partition qualifier, a direction, and a number — us-east-1, eu-central-1,
// ap-southeast-2, us-gov-west-1, cn-north-1.
func looksLikeRegion(s string) bool {
	parts := strings.Split(s, "-")
	if len(parts) < 3 || len(parts) > 4 {
		return false
	}
	if len(parts[0]) != 2 {
		return false
	}
	for _, r := range parts[0] {
		if r < 'a' || r > 'z' {
			return false
		}
	}
	last := parts[len(parts)-1]
	if len(last) != 1 || last[0] < '0' || last[0] > '9' {
		return false
	}
	for _, p := range parts[1 : len(parts)-1] {
		if p == "" {
			return false
		}
		for _, r := range p {
			if r < 'a' || r > 'z' {
				return false
			}
		}
	}
	return true
}
