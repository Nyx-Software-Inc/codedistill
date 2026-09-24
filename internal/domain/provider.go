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

import (
	"net"
	"strings"
	"time"
)

// Worker types: the jobs CodeDistill needs a model for.
//
// A WORKFLOW is a kind of job — decompose a document, draft an architecture —
// and it runs WORKERS. Each worker has a type, and each type is served by a
// configured model, so "decompose this BRD on Gemini" is a worker type pointed
// at a provider.
//
// Deliberately not called a "role": that already means a member's role (owner,
// admin, member) and a sentence's role in decomposition (introduce, elaborate,
// retract, meta). A third meaning was one too many.
//
// Separate from providers because they change independently: swapping which
// model classifies should not mean re-entering an endpoint and a key.
const (
	// WorkerClassifier reads one capture at a time. Genuinely within a local 7B,
	// and the highest-frequency call in the product — the reason local-first
	// remains the sensible default here.
	WorkerClassifier = "classifier"

	// WorkerDecomposer reads a whole document. Measured on a real 159-line spec:
	// a correctly configured local 7B found 27 of 37 written-down requirements
	// and took 46 minutes. A different class of job from classification.
	WorkerDecomposer = "decomposer"

	// WorkerReviewer is the adversarial verification reviewer. Supersedes the
	// verify.reviewer_model setting, which could only name a model on the one
	// configured endpoint.
	WorkerReviewer = "reviewer"

	// WorkerSolutioner and WorkerChallenger are the epistemic pair: one proposes,
	// one attacks. They SHOULD resolve to different providers — a challenger
	// running the same weights as the solutioner shares its blind spots, which
	// is the fox guarding the henhouse.
	WorkerSolutioner = "solutioner"
	WorkerChallenger = "challenger"
)

// WorkerTypes in a stable order, for UI and validation.
var WorkerTypes = []string{
	WorkerClassifier, WorkerDecomposer, WorkerReviewer, WorkerSolutioner, WorkerChallenger,
}

// ValidWorkerType reports whether t is a worker type CodeDistill itself knows
// how to run.
//
// It is NOT a constraint on what may be stored. The column is open, because a
// user-defined workflow can name a worker type this build never heard of, and
// requiring a schema change for that is the difference between a feature and a
// request. This guards CodeDistill's own code against a typo.
func ValidWorkerType(t string) bool {
	for _, x := range WorkerTypes {
		if x == t {
			return true
		}
	}
	return false
}

// ModelProvider is one configured connection to a model.
type ModelProvider struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Protocol string `json:"protocol"` // ollama | openai
	Endpoint string `json:"endpoint"`
	Model    string `json:"model"`

	// APIKey is encrypted at rest. It is NEVER serialised outward: the wire
	// form carries HasAPIKey instead, so a key cannot leak through an API
	// response, a log line, or a support bundle.
	APIKey    string `json:"-"`
	HasAPIKey bool   `json:"has_api_key"`

	// ContextTokens is how much prompt this provider will actually read.
	// Correctness, not tuning: Ollama truncates silently past its own default
	// and answers confidently from the remainder.
	ContextTokens int `json:"context_tokens"`

	// IsLocal records whether data leaves the machine. Stored rather than
	// inferred from the endpoint, because "localhost" is not a reliable
	// signal — an SSH tunnel looks local and is not — and a user deserves a
	// truthful answer that does not depend on URL parsing.
	IsLocal bool `json:"is_local"`

	Enabled   bool       `json:"enabled"`
	LastOKAt  *time.Time `json:"last_ok_at,omitempty"`
	LastError string     `json:"last_error,omitempty"`
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
}

// Protocols with an adapter. The database column is deliberately open — a
// CHECK there cost a table rebuild to add two vendors and would cost one for
// every future adapter, for a value whose correctness is a property of the Go
// code rather than of the data. A protocol is valid only if something can
// actually speak it, and SQLite cannot know that.
//
// Adding an adapter means adding it here, and the vendor-catalog test fails if
// a vendor names a protocol this list does not carry.
var Protocols = []string{"ollama", "openai", "anthropic", "gemini", "bedrock", "azure"}

// ValidProtocol reports whether an adapter exists for p.
func ValidProtocol(p string) bool {
	for _, x := range Protocols {
		if x == p {
			return true
		}
	}
	return false
}

// WorkerBinding is one worker type pointed at one provider.
type WorkerBinding struct {
	WorkerType string    `json:"worker_type"`
	ProjectID  string    `json:"project_id,omitempty"` // empty = the global default
	ProviderID string    `json:"provider_id"`
	UpdatedAt  time.Time `json:"updated_at"`
}

// EpistemicPairShared reports whether the solutioner and challenger workers
// resolve to the same provider, which defeats the purpose of having two.
//
// A warning rather than an error: a user experimenting with one provider should
// not be blocked, only told. The check is on the provider — two roles on the
// same weights share the same blind spots however they are prompted.
func EpistemicPairShared(bindings map[string]string) bool {
	s, sok := bindings[WorkerSolutioner]
	c, cok := bindings[WorkerChallenger]
	return sok && cok && s == c
}

// ProvenRemote reports whether a request to this provider demonstrably leaves
// the machine.
//
// Deliberately NOT the inverse of IsLocal. The two questions have different
// evidentiary standards, and collapsing them is what let a user tick "nothing
// leaves this machine" on a Gemini provider and be believed:
//
//   - Anthropic and Gemini are hosted services with no local implementation.
//     A request to one leaves the machine. Certain.
//   - An endpoint whose host is not a loopback or private address is reachable
//     only over the network by definition. Certain.
//   - A LOOPBACK endpoint is probably local — but an SSH tunnel looks exactly
//     like one and is not. Uncertain, which is why that case is still asked
//     rather than asserted, as the IsLocal comment above says.
//
// DNS is deliberately not consulted. Resolving a name would make a nameserver
// the authority on a privacy label, and a slow or poisoned one would decide it.
// Only literals and well-known names are read.
func ProvenRemote(protocol, endpoint string) bool {
	switch protocol {
	case "anthropic", "gemini":
		// No local implementation exists. Whatever the endpoint says.
		return true
	}
	host := endpointHost(endpoint)
	if host == "" {
		// Nothing to judge. Treated as unproven rather than remote: an empty
		// endpoint fails at call time anyway, and guessing "remote" here would
		// label a half-filled form.
		return false
	}
	return !hostIsLocalOrPrivate(host)
}

// endpointHost pulls the hostname out of an endpoint without requiring it to be
// a well-formed URL — the field is user-entered and often is not.
func endpointHost(endpoint string) string {
	s := strings.TrimSpace(endpoint)
	if s == "" {
		return ""
	}
	if i := strings.Index(s, "://"); i >= 0 {
		s = s[i+3:]
	}
	if i := strings.IndexAny(s, "/?#"); i >= 0 {
		s = s[:i]
	}
	if i := strings.LastIndex(s, "@"); i >= 0 { // strip userinfo
		s = s[i+1:]
	}
	if h, _, err := net.SplitHostPort(s); err == nil {
		s = h
	}
	return strings.Trim(strings.ToLower(s), "[]")
}

func hostIsLocalOrPrivate(host string) bool {
	if host == "localhost" || strings.HasSuffix(host, ".localhost") ||
		strings.HasSuffix(host, ".local") || strings.HasSuffix(host, ".internal") {
		return true
	}
	ip := net.ParseIP(host)
	if ip == nil {
		// A name we do not recognise. Not proven local, and not resolved on
		// purpose — see above.
		return false
	}
	return ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsUnspecified()
}

// Reach values: where a request to a provider actually goes.
//
// Three, not two. "Stays on this machine" and "goes to the box with the GPU in
// the next room" are different claims, and collapsing them is what produced a
// vendor called "Ollama (on this machine)" that told LAN users the wrong thing
// about where their documents went. The third is the internet.
const (
	ReachMachine  = "machine"
	ReachNetwork  = "network"
	ReachInternet = "internet"
)

// EndpointReach classifies where a provider's requests go.
//
// Same evidence rules as ProvenRemote — no DNS, literals and well-known names
// only — just reported with the distinction kept rather than flattened.
func EndpointReach(protocol, endpoint string) string {
	switch protocol {
	case "anthropic", "gemini":
		return ReachInternet
	}
	host := endpointHost(endpoint)
	if host == "" {
		return ReachInternet // nothing to judge; assume the cautious answer
	}
	if host == "localhost" || strings.HasSuffix(host, ".localhost") {
		return ReachMachine
	}
	if ip := net.ParseIP(host); ip != nil {
		switch {
		case ip.IsLoopback() || ip.IsUnspecified():
			return ReachMachine
		case ip.IsPrivate() || ip.IsLinkLocalUnicast():
			return ReachNetwork
		}
		return ReachInternet
	}
	if strings.HasSuffix(host, ".local") || strings.HasSuffix(host, ".internal") {
		return ReachNetwork
	}
	return ReachInternet
}
