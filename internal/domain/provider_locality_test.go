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

package domain

import "testing"

// The bug this exists for: the provider dialog offered "Runs on this machine —
// nothing leaves it" as a checkbox, and ticking it on a GEMINI provider was
// believed. The document still went to Google; the product simply stopped
// saying so, which is worse than never having warned.
func TestAHostedVendorCannotBeCalledLocal(t *testing.T) {
	for _, tc := range []struct{ protocol, endpoint string }{
		{"gemini", "https://generativelanguage.googleapis.com/v1beta"},
		{"anthropic", "https://api.anthropic.com/v1"},
		// Even pointed at loopback: there is no local Gemini, so a loopback
		// endpoint here is a proxy that forwards off the machine.
		{"gemini", "http://localhost:8080"},
		{"anthropic", "http://127.0.0.1:9000"},
	} {
		if !ProvenRemote(tc.protocol, tc.endpoint) {
			t.Errorf("%s at %s was not recognised as remote — a user could claim it never leaves the machine",
				tc.protocol, tc.endpoint)
		}
	}
}

// A hosted OpenAI-compatible service is remote too, and the protocol alone
// cannot say so: the same protocol serves LM Studio on loopback.
func TestAPublicEndpointIsRemoteWhateverTheProtocol(t *testing.T) {
	remote := []string{
		"https://api.openai.com/v1",
		"https://openrouter.ai/api/v1",
		"http://8.8.8.8:11434",
		"https://ollama.example.com",
	}
	for _, e := range remote {
		if !ProvenRemote("openai", e) {
			t.Errorf("%s was not recognised as remote", e)
		}
	}
}

// Where it is genuinely uncertain, the product must NOT assert. A loopback
// endpoint is probably local, but an SSH tunnel is indistinguishable from one —
// so the user's own answer stands rather than being overruled by URL parsing.
func TestLoopbackAndPrivateStayTheUsersCallToMake(t *testing.T) {
	uncertain := []string{
		"http://localhost:11434",
		"http://127.0.0.1:1234/v1",
		"http://[::1]:11434",
		"http://192.168.2.43:11434",
		"http://10.0.0.5:8000/v1",
		"http://my-box.local:11434",
		"", // a half-filled form is not a claim about anything
	}
	for _, e := range uncertain {
		if ProvenRemote("ollama", e) {
			t.Errorf("%q was declared provably remote; it is not provable either way, "+
				"and overruling the user here would be guessing", e)
		}
	}
}

// Userinfo and ports must not fool the host extraction — "user@evil.com" past a
// naive parser is how a remote host gets read as something else.
func TestHostExtractionIsNotFooledByShape(t *testing.T) {
	if !ProvenRemote("openai", "https://token@api.openai.com:443/v1") {
		t.Error("userinfo hid the real host")
	}
	if ProvenRemote("ollama", "http://user:pw@127.0.0.1:11434") {
		t.Error("userinfo made a loopback host unreadable")
	}
}

// Three-way reach: the LAN box with the GPU is neither "this machine" nor "the
// internet", and calling it either is a lie in one direction or the other.
func TestReachDistinguishesTheMachineFromTheNetwork(t *testing.T) {
	cases := []struct{ protocol, endpoint, want string }{
		{"ollama", "http://localhost:11434", ReachMachine},
		{"ollama", "http://127.0.0.1:11434", ReachMachine},
		{"ollama", "http://[::1]:11434", ReachMachine},
		// The case this exists for: one beefy box serving the house.
		{"ollama", "http://192.168.2.50:11434", ReachNetwork},
		{"ollama", "http://10.1.2.3:11434", ReachNetwork},
		{"ollama", "http://beefy.local:11434", ReachNetwork},
		{"ollama", "http://ollama.example.com", ReachInternet},
		{"openai", "https://api.openai.com/v1", ReachInternet},
		{"gemini", "https://generativelanguage.googleapis.com/v1beta", ReachInternet},
	}
	for _, c := range cases {
		if got := EndpointReach(c.protocol, c.endpoint); got != c.want {
			t.Errorf("%s at %s: reach %q, want %q", c.protocol, c.endpoint, got, c.want)
		}
	}
}
