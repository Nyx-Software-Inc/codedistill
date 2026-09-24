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

// Vendors: who you are talking to, rather than how.
//
// Picking a company is the choice a user actually makes. Protocol and endpoint
// are consequences of it, and asking someone to type
// "https://api.openai.com/v1" and select "openai-compatible" is asking them to
// know our implementation.
//
// The catalog is also where honesty about coverage lives. Several vendors do
// NOT speak a protocol this product implements — Anthropic's API is
// /v1/messages with an x-api-key header, not OpenAI's shape — and listing them
// as if they worked would produce a confident failure at the first call.
// Supported=false says so at the moment of choosing.

type Vendor struct {
	ID   string `json:"id"`
	Name string `json:"name"`

	// Protocol is the adapter used to talk to it. Empty when unsupported.
	Protocol string `json:"protocol,omitempty"`

	// DefaultEndpoint pre-fills the field. Still editable: self-hosted and
	// proxied deployments are normal, and a fixed endpoint would exclude them.
	DefaultEndpoint string `json:"default_endpoint,omitempty"`

	NeedsKey bool `json:"needs_key"`

	// ListsModels reports whether asking "what models do you have" works.
	// Ollama answers; OpenAI answers with ids only.
	ListsModels bool `json:"lists_models"`

	// ReportsContext reports whether the vendor states a model's context
	// window. Where false, the number is whatever a human typed, and the UI
	// must not present it as verified.
	ReportsContext bool `json:"reports_context"`

	// Supported is false when this product cannot talk to the vendor at all.
	// Listed anyway, with a reason: "not offered" is a worse answer than "not
	// yet, and here is what to do instead".
	Supported bool   `json:"supported"`
	Note      string `json:"note,omitempty"`
}

// KnownVendors, most likely first.
//
// Deliberately NOT carrying model lists or context limits per vendor: those go
// stale silently, and a stale limit is the same class of bug as the silent
// truncation this package exists to catch. Everything here is a connection
// detail, which changes rarely and fails loudly when wrong.
var KnownVendors = []Vendor{
	{
		ID: "ollama", Name: "Ollama",
		Protocol: "ollama", DefaultEndpoint: "http://localhost:11434",
		NeedsKey: false, ListsModels: true, ReportsContext: true, Supported: true,
		// NOT "on this machine". One box with the GPU serving every other
		// machine in the house is the normal Ollama deployment, and naming the
		// vendor after the default endpoint told those users the wrong thing
		// about where their documents go. Where it runs is the ENDPOINT's
		// business, and the UI derives the answer from that.
		Note: "Runs wherever you point it: this machine, or a box on your network with the GPU in it. Reports each model's real context window.",
	},
	{
		ID: "openai", Name: "OpenAI",
		Protocol: "openai", DefaultEndpoint: "https://api.openai.com/v1",
		NeedsKey: true, ListsModels: true, ReportsContext: false, Supported: true,
		Note: "Lists model names but not context windows, so that figure is whatever you enter. OpenAI rejects an oversized prompt rather than truncating it, so a wrong number fails loudly.",
	},
	{
		ID: "openrouter", Name: "OpenRouter",
		Protocol: "openai", DefaultEndpoint: "https://openrouter.ai/api/v1",
		NeedsKey: true, ListsModels: true, ReportsContext: true, Supported: true,
		Note: "Routes to many vendors, including ones this product cannot reach directly.",
	},
	{
		ID: "groq", Name: "Groq",
		Protocol: "openai", DefaultEndpoint: "https://api.groq.com/openai/v1",
		NeedsKey: true, ListsModels: true, ReportsContext: true, Supported: true,
	},
	{
		ID: "together", Name: "Together AI",
		Protocol: "openai", DefaultEndpoint: "https://api.together.xyz/v1",
		NeedsKey: true, ListsModels: true, ReportsContext: true, Supported: true,
	},
	{
		ID: "lmstudio", Name: "LM Studio",
		Protocol: "openai", DefaultEndpoint: "http://localhost:1234/v1",
		NeedsKey: false, ListsModels: true, ReportsContext: true, Supported: true,
		Note: "This machine by default; point it at another on your network if that is where the GPU is.",
	},
	{
		ID: "vllm", Name: "vLLM (self-hosted)",
		Protocol: "openai", DefaultEndpoint: "http://localhost:8000/v1",
		NeedsKey: false, ListsModels: true, ReportsContext: true, Supported: true,
	},
	{
		ID: "custom", Name: "Other OpenAI-compatible service",
		Protocol: "openai", DefaultEndpoint: "", NeedsKey: true,
		ListsModels: true, ReportsContext: false, Supported: true,
		Note: "Anything exposing /v1/chat/completions: llama.cpp, Fireworks, DeepInfra, a gateway of your own.",
	},
	{
		ID: "anthropic", Name: "Anthropic (Claude)",
		Protocol: "anthropic", DefaultEndpoint: "https://api.anthropic.com/v1",
		NeedsKey: true, ListsModels: true, ReportsContext: false, Supported: true,
		Note: "Claude has no JSON mode, so structured output is forced with a tool call rather than asked for in the prompt. Context windows are not published, so that figure is yours — Anthropic rejects an oversized prompt rather than truncating it, so a wrong one fails loudly.",
	},
	{
		ID: "azure", Name: "Azure OpenAI",
		Protocol: "azure", DefaultEndpoint: "https://YOUR-RESOURCE.openai.azure.com",
		NeedsKey: true, ListsModels: true, ReportsContext: false, Supported: true,
		// The Model field holds a DEPLOYMENT name here, which is the one thing
		// people get wrong: it is whatever was typed when the deployment was
		// created, not "gpt-4o".
		Note: "OpenAI models under your Azure subscription and data-residency terms. Replace YOUR-RESOURCE with your resource name. The model is the DEPLOYMENT you created, not a model id — the list below shows the ones that exist. Context windows are not published, so that figure is yours.",
	},
	{
		ID: "bedrock", Name: "AWS Bedrock",
		Protocol: "bedrock", DefaultEndpoint: "https://bedrock-runtime.us-east-1.amazonaws.com",
		NeedsKey: true, ListsModels: true, ReportsContext: false, Supported: true,
		// The region is part of the ENDPOINT, which is why the default carries
		// one: change us-east-1 to wherever the models are enabled. Credentials
		// are three fields rather than a key, and the request is signed rather
		// than bearing a token — see internal/awssig.
		Note: "The same Claude and Llama models, called inside your own AWS account: your bill, your region, and optionally never leaving your VPC. Needs an access key and secret with bedrock:InvokeModel, and the model enabled under Model access in the Bedrock console. Context windows are not published, so that figure is yours.",
	},
	{
		ID: "gemini", Name: "Google Gemini",
		Protocol: "gemini", DefaultEndpoint: "https://generativelanguage.googleapis.com/v1beta",
		NeedsKey: true, ListsModels: true, ReportsContext: true, Supported: true,
		Note: "Reports each model's real input limit and has a native JSON mode, so both the context window and the structured output are facts rather than hopes.",
	},
}

// VendorByID returns a vendor, or false.
func VendorByID(id string) (Vendor, bool) {
	for _, v := range KnownVendors {
		if v.ID == id {
			return v, true
		}
	}
	return Vendor{}, false
}
