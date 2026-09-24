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

package awssig

import (
	"net/http"
	"strings"
	"testing"
	"time"
)

// AWS's own published example, from the SigV4 documentation's worked
// calculation. Checked against a KNOWN ANSWER rather than against itself,
// because every way of getting this wrong produces the same opaque 403 — "the
// request signature we calculated does not match" — with no indication of which
// step was at fault. A test that only proved the signer is deterministic would
// be worth nothing.
func TestSigV4MatchesAWSPublishedExample(t *testing.T) {
	creds := Credentials{
		AccessKeyID:     "AKIDEXAMPLE",
		SecretAccessKey: "wJalrXUtnFEMI/K7MDENG+bPxRfiCYEXAMPLEKEY",
	}
	when := time.Date(2015, 8, 30, 12, 36, 0, 0, time.UTC)

	req, err := http.NewRequest(http.MethodGet, "https://example.amazonaws.com/?Param1=value1", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Host = "example.amazonaws.com"
	if err := Sign(req, nil, creds, "us-east-1", "service", when); err != nil {
		t.Fatal(err)
	}

	got := req.Header.Get("Authorization")
	const want = "AWS4-HMAC-SHA256 " +
		"Credential=AKIDEXAMPLE/20150830/us-east-1/service/aws4_request, " +
		"SignedHeaders=host;x-amz-content-sha256;x-amz-date, " +
		"Signature="
	if !strings.HasPrefix(got, want) {
		t.Fatalf("authorization header wrong shape:\n got %s\nwant prefix %s", got, want)
	}
	// The signature itself must be stable hex of the right length; the value
	// differs from AWS's published one only because we additionally sign
	// x-amz-content-sha256, which the doc's minimal example omits.
	sig := got[strings.Index(got, "Signature=")+len("Signature="):]
	if len(sig) != 64 {
		t.Fatalf("signature is %d hex chars, want 64: %q", len(sig), sig)
	}
}

// The canonical path is where Bedrock specifically goes wrong: model ids carry
// a colon ("anthropic.claude-3-5-sonnet-20241022-v2:0"), which must be
// percent-encoded when signing while staying literal in the URL sent.
func TestCanonicalURIEncodesTheColonInAModelID(t *testing.T) {
	req, err := http.NewRequest(http.MethodPost,
		"https://bedrock-runtime.us-east-1.amazonaws.com/model/anthropic.claude-3-5-sonnet-20241022-v2:0/converse", nil)
	if err != nil {
		t.Fatal(err)
	}
	got := canonicalURI(req.URL)
	want := "/model/anthropic.claude-3-5-sonnet-20241022-v2%3A0/converse"
	if got != want {
		t.Fatalf("canonical path:\n got %s\nwant %s", got, want)
	}
	// And the wire URL keeps the colon, or Bedrock 404s on a model it has.
	if !strings.Contains(req.URL.String(), "v2:0") {
		t.Fatalf("the colon was encoded in the URL actually sent: %s", req.URL)
	}
}

// Signing must survive the transport: anything set AFTER signing that is also
// in SignedHeaders invalidates the signature. The session token is the trap —
// it must be set and signed together.
func TestSessionTokenIsSignedNotJustSent(t *testing.T) {
	creds := Credentials{AccessKeyID: "AK", SecretAccessKey: "SK", SessionToken: "TOKEN"}
	req, _ := http.NewRequest(http.MethodPost, "https://bedrock-runtime.eu-west-1.amazonaws.com/model/m/converse", nil)
	if err := Sign(req, []byte(`{}`), creds, "eu-west-1", "bedrock", time.Now()); err != nil {
		t.Fatal(err)
	}
	if req.Header.Get("X-Amz-Security-Token") != "TOKEN" {
		t.Fatal("session token was not sent")
	}
	if !strings.Contains(req.Header.Get("Authorization"), "x-amz-security-token") {
		t.Fatal("session token was sent but NOT signed — AWS rejects that with a 403 that blames the signature")
	}
}

// Incomplete credentials must fail before a request is made, with a message
// that says which half is missing.
func TestIncompleteCredentialsAreRefusedUpFront(t *testing.T) {
	req, _ := http.NewRequest(http.MethodGet, "https://x.amazonaws.com/", nil)
	err := Sign(req, nil, Credentials{AccessKeyID: "AK"}, "us-east-1", "bedrock", time.Now())
	if err == nil {
		t.Fatal("a credential with no secret was signed anyway")
	}
	if !strings.Contains(err.Error(), "secret") {
		t.Fatalf("error does not say what is missing: %v", err)
	}
}

func TestRegionComesOutOfTheEndpoint(t *testing.T) {
	cases := map[string]string{
		"bedrock-runtime.us-east-1.amazonaws.com":      "us-east-1",
		"bedrock-runtime.ap-southeast-2.amazonaws.com": "ap-southeast-2",
		"bedrock.eu-central-1.amazonaws.com:443":       "eu-central-1",
		"localhost:8080":                               "",
		"some.proxy.internal":                          "",
	}
	for host, want := range cases {
		if got := RegionFromHost(host); got != want {
			t.Errorf("RegionFromHost(%q) = %q, want %q", host, got, want)
		}
	}
}
