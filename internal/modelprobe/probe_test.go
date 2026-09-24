// =============================================================================
//
//	Copyright (c) 2026 Nyx Software, Inc.  All rights reserved.
//
//	CodeDistill
//
//	Property of Nyx Software, Inc., provided under a dual license: the GNU Affero General
//	Public License v3.0 (see the LICENSE file) and, separately, a commercial
//	license available from Nyx Software, Inc. Use outside the terms of one of those
//	licenses is prohibited.
//
//	SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-Nyx-Commercial
//
// =============================================================================
// Each test here is a failure this codebase actually had. None of them would
// have been caught by a reachability ping.
package modelprobe

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// ollamaServer fakes an Ollama that honours num_ctx up to cap, reporting the
// truncated count exactly as the real one does.
func ollamaServer(t *testing.T, cap int) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/tags" {
			w.Write([]byte(`{"models":[]}`))
			return
		}
		var req struct {
			Prompt  string         `json:"prompt"`
			Options map[string]any `json:"options"`
		}
		json.NewDecoder(r.Body).Decode(&req)
		want := len(req.Prompt) / 4
		limit := cap
		if nc, ok := req.Options["num_ctx"].(float64); ok && int(nc) < limit {
			limit = int(nc)
		}
		if want > limit {
			want = limit // exactly what Ollama does: truncate, do not error
		}
		json.NewEncoder(w).Encode(map[string]any{
			"response": "ok", "done": true, "prompt_eval_count": want,
		})
	}))
}

// The bug that corrupted every oversized prompt since v1: a provider that
// accepts a long prompt, answers confidently, and read only part of it.
func TestSilentTruncationIsCaught(t *testing.T) {
	srv := ollamaServer(t, 4096)
	defer srv.Close()

	got := Probe(context.Background(), srv.Client(), Target{
		Protocol: "ollama", Endpoint: srv.URL, Model: "m", NeedContext: 16384,
	}, nil)

	if got.Outcome != ContextShort {
		t.Fatalf("outcome = %q, want context_short — a 4k provider passed a 16k requirement", got.Outcome)
	}
	if got.ContextSeen != 4096 || got.ContextAsked != 16384 {
		t.Errorf("seen/asked = %d/%d, want 4096/16384", got.ContextSeen, got.ContextAsked)
	}
	if !strings.Contains(got.Detail, "truncated") {
		t.Errorf("detail does not say what will happen: %q", got.Detail)
	}
	// Reachable and generating: this is why up/down is not enough.
	if !got.Reachable || !got.CanGenerate {
		t.Error("the provider IS reachable and DOES generate; that is the point")
	}
}

func TestAdequateContextVerifies(t *testing.T) {
	srv := ollamaServer(t, 32768)
	defer srv.Close()

	got := Probe(context.Background(), srv.Client(), Target{
		Protocol: "ollama", Endpoint: srv.URL, Model: "m", NeedContext: 16384,
	}, nil)
	if got.Outcome != Verified {
		t.Fatalf("outcome = %q (%s), want verified", got.Outcome, got.Detail)
	}
	if !got.OK() {
		t.Error("a verified provider reported not OK")
	}
}

// Observed twice after a laptop suspend: /api/tags in a millisecond,
// /api/generate never returns.
func TestWedgedModelIsCaught(t *testing.T) {
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/tags" {
			w.Write([]byte(`{"models":[]}`))
			return
		}
		<-release
	}))
	defer func() { close(release); srv.Close() }()

	hc := &http.Client{Timeout: 1500 * time.Millisecond}
	got := Probe(context.Background(), hc, Target{Protocol: "ollama", Endpoint: srv.URL, Model: "m"}, nil)

	if got.Outcome != ModelUnusable {
		t.Fatalf("outcome = %q, want model_unusable", got.Outcome)
	}
	if !got.Reachable {
		t.Error("it WAS reachable — that is exactly the trap")
	}
	if got.CanGenerate {
		t.Error("reported able to generate while generation hangs")
	}
}

// A wrong model name should fail when someone types it, not minutes into a job.
func TestWrongModelNameIsCaughtAtConfigTime(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/tags" {
			w.Write([]byte(`{"models":[]}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	got := Probe(context.Background(), srv.Client(), Target{
		Protocol: "ollama", Endpoint: srv.URL, Model: "typo:7b",
	}, nil)
	if got.Outcome != ModelUnusable {
		t.Fatalf("outcome = %q", got.Outcome)
	}
	if !strings.Contains(got.Detail, "typo:7b") {
		t.Errorf("detail does not name the model: %q", got.Detail)
	}
}

func TestRejectedCredentialsSaySo(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/models") {
			w.Write([]byte(`{"data":[]}`))
			return
		}
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	got := Probe(context.Background(), srv.Client(), Target{
		Protocol: "openai", Endpoint: srv.URL, Model: "big", APIKey: "wrong",
	}, nil)
	if got.Outcome != ModelUnusable || !strings.Contains(got.Detail, "API key") {
		t.Fatalf("outcome = %q, detail = %q", got.Outcome, got.Detail)
	}
}

func TestNothingListening(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	url := srv.URL
	srv.Close()

	got := Probe(context.Background(), nil, Target{Protocol: "ollama", Endpoint: url, Model: "m"}, nil)
	if got.Outcome != Unreachable || got.Reachable {
		t.Fatalf("outcome = %q, reachable = %v", got.Outcome, got.Reachable)
	}
}

// An OpenAI-compatible server that reports usage must verify like any other.
func TestOpenAIPathIsProbed(t *testing.T) {
	var sawAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/models") {
			w.Write([]byte(`{"data":[]}`))
			return
		}
		sawAuth = r.Header.Get("Authorization")
		var req struct {
			Messages []struct{ Content string } `json:"messages"`
		}
		json.NewDecoder(r.Body).Decode(&req)
		n := 0
		if len(req.Messages) > 0 {
			n = len(req.Messages[0].Content) / 4
		}
		fmt.Fprintf(w, `{"choices":[{"message":{"content":"ok"}}],"usage":{"prompt_tokens":%d}}`, n)
	}))
	defer srv.Close()

	got := Probe(context.Background(), srv.Client(), Target{
		Protocol: "openai", Endpoint: srv.URL, Model: "big",
		APIKey: "sk-test", NeedContext: 8000,
	}, nil)
	if got.Outcome != Verified {
		t.Fatalf("outcome = %q (%s)", got.Outcome, got.Detail)
	}
	if sawAuth != "Bearer sk-test" {
		t.Errorf("key not sent: %q", sawAuth)
	}
}

// A provider that reports no counts must not be graded as verified OR as
// broken. Inventing a verdict is how the original bug hid.
func TestUnreportedTokenCountsAreAdmitted(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/models") {
			w.Write([]byte(`{"data":[]}`))
			return
		}
		w.Write([]byte(`{"choices":[{"message":{"content":"ok"}}]}`))
	}))
	defer srv.Close()

	got := Probe(context.Background(), srv.Client(), Target{
		Protocol: "openai", Endpoint: srv.URL, Model: "big", NeedContext: 8000,
	}, nil)
	if got.Outcome != Working {
		t.Fatalf("outcome = %q, want working (honest uncertainty)", got.Outcome)
	}
	if !strings.Contains(got.Detail, "could not be proven") {
		t.Errorf("detail claims more than it knows: %q", got.Detail)
	}
}

// The filler must not compress, or a server that caches identical content
// would report a small count and look broken.
func TestFillerIsNotRepetitive(t *testing.T) {
	p := fillerPrompt(2000)
	lines := strings.Split(strings.TrimSpace(p), "\n")
	seen := map[string]bool{}
	for _, l := range lines {
		if seen[l] {
			t.Fatalf("filler repeats a line: %q", l)
		}
		seen[l] = true
	}
	if got := len(p) / 4; got < 1800 {
		t.Errorf("filler is ~%d tokens, want ~2000", got)
	}
}

// A slow provider is not a truncating one. The first real run against local
// Ollama graded a 16k proof as "context_short" because the probe's own 90s
// timeout fired mid-prefill — at a measured ~76ms/token, 16,384 tokens
// legitimately needs twenty minutes. Reporting truncation there is a confident
// wrong answer, which is the exact failure this package exists to prevent.
func TestSlowProviderIsNotReportedAsTruncating(t *testing.T) {
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/tags" {
			w.Write([]byte(`{"models":[]}`))
			return
		}
		var req struct {
			Prompt string `json:"prompt"`
		}
		json.NewDecoder(r.Body).Decode(&req)
		if len(req.Prompt) > 100 { // the context proof: never finishes in time
			<-release
			return
		}
		w.Write([]byte(`{"response":"ok","done":true,"prompt_eval_count":2}`))
	}))
	defer func() { close(release); srv.Close() }()

	hc := &http.Client{Timeout: 1200 * time.Millisecond}
	got := Probe(context.Background(), hc, Target{
		Protocol: "ollama", Endpoint: srv.URL, Model: "m", NeedContext: 16384,
	}, nil)

	if got.Outcome == ContextShort {
		t.Fatalf("a timeout was reported as truncation: %q", got.Detail)
	}
	if got.Outcome != ContextUnproven {
		t.Fatalf("outcome = %q, want context_unproven", got.Outcome)
	}
	if !strings.Contains(got.Detail, "not evidence of truncation") {
		t.Errorf("detail does not disclaim truncation: %q", got.Detail)
	}
	// Unproven is still usable: the provider generates.
	if !got.OK() || !got.CanGenerate {
		t.Error("a slow-but-working provider was graded unusable")
	}
}

// The deferred timing must reach the caller. It did not: with a local variable
// the defer ran after the return value was copied, and every probe reported
// 0.0s — including against a real provider that took seconds.
func TestLatencyIsActuallyReported(t *testing.T) {
	srv := ollamaServer(t, 32768)
	defer srv.Close()

	// An injected clock, not the wall clock: against an in-process fake the
	// whole probe takes under a millisecond, so a real timing would round to 0
	// and the test would fail on a correct implementation.
	base := time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)
	calls := 0
	clock := func() time.Time {
		calls++
		return base.Add(time.Duration(calls) * 750 * time.Millisecond)
	}

	got := Probe(context.Background(), srv.Client(), Target{
		Protocol: "ollama", Endpoint: srv.URL, Model: "m",
	}, clock)
	if got.LatencyMS <= 0 {
		t.Fatalf("latency = %dms; the deferred timing never reached the caller", got.LatencyMS)
	}
}

// The proof deadline must scale with the prompt, or a fixed one cuts off a
// legitimate large-window provider.
func TestContextProofTimeoutScales(t *testing.T) {
	small, large := contextProofTimeout(1000), contextProofTimeout(16384)
	if large <= small {
		t.Fatalf("16k got %v, 1k got %v — the deadline does not scale", large, small)
	}
	if large < 20*time.Minute {
		t.Errorf("16k gets %v; at ~76ms/token it needs ~20min", large)
	}
	if contextProofTimeout(10_000_000) > time.Hour {
		t.Errorf("no ceiling: %v", contextProofTimeout(10_000_000))
	}
}

// Asking beats measuring. A provider that reports its own context length must
// be believed instantly rather than proven over twenty minutes — and a model
// too small for the job must be caught at config time, not after a long run.
func TestDescribedWindowIsUsedInsteadOfMeasuring(t *testing.T) {
	var generates int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/tags":
			w.Write([]byte(`{"models":[]}`))
		case "/api/show":
			// Architecture-prefixed, exactly as Ollama returns it.
			w.Write([]byte(`{"model_info":{"qwen2.context_length":32768,"qwen2.block_count":28},
				"details":{"family":"qwen2","parameter_size":"7.6B","quantization_level":"Q4_K_M"},
				"capabilities":["completion","tools"]}`))
		case "/api/generate":
			generates++
			w.Write([]byte(`{"response":"ok","done":true,"prompt_eval_count":2}`))
		}
	}))
	defer srv.Close()

	got := Probe(context.Background(), srv.Client(), Target{
		Protocol: "ollama", Endpoint: srv.URL, Model: "qwen2.5:7b", NeedContext: 16384,
	}, nil)

	if got.Outcome != Verified {
		t.Fatalf("outcome = %q (%s), want verified from the reported window", got.Outcome, got.Detail)
	}
	if got.ContextSeen != 32768 {
		t.Errorf("context = %d, want the reported 32768", got.ContextSeen)
	}
	// One cheap generate, and NOT the expensive oversized proof.
	if generates != 1 {
		t.Errorf("made %d generate calls; the reported window should make the proof unnecessary", generates)
	}
	if got.Described.ParameterSize != "7.6B" || got.Described.Source != "api/show" {
		t.Errorf("model details lost: %+v", got.Described)
	}
}

func TestDescribedWindowTooSmallIsCaughtAtConfigTime(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/tags":
			w.Write([]byte(`{"models":[]}`))
		case "/api/show":
			w.Write([]byte(`{"model_info":{"llama.context_length":4096}}`))
		default:
			w.Write([]byte(`{"response":"ok","done":true,"prompt_eval_count":2}`))
		}
	}))
	defer srv.Close()

	got := Probe(context.Background(), srv.Client(), Target{
		Protocol: "ollama", Endpoint: srv.URL, Model: "small", NeedContext: 16384,
	}, nil)
	if got.Outcome != ContextShort {
		t.Fatalf("outcome = %q, want context_short", got.Outcome)
	}
	if !strings.Contains(got.Detail, "4096") || !strings.Contains(got.Detail, "16384") {
		t.Errorf("detail does not give both numbers: %q", got.Detail)
	}
}

// OpenAI and Anthropic list an id and nothing else. Absence must be admitted,
// not filled in from a hardcoded table that silently goes stale.
func TestUnreportedWindowFallsBackToMeasuring(t *testing.T) {
	var sawBigPrompt bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/tags" {
			w.Write([]byte(`{"models":[]}`))
			return
		}
		if r.URL.Path == "/api/show" {
			w.Write([]byte(`{"model_info":{},"details":{}}`)) // says nothing
			return
		}
		var req struct {
			Prompt string `json:"prompt"`
		}
		json.NewDecoder(r.Body).Decode(&req)
		n := len(req.Prompt) / 4
		if n > 1000 {
			sawBigPrompt = true
		}
		fmt.Fprintf(w, `{"response":"ok","done":true,"prompt_eval_count":%d}`, n)
	}))
	defer srv.Close()

	got := Probe(context.Background(), srv.Client(), Target{
		Protocol: "ollama", Endpoint: srv.URL, Model: "mystery", NeedContext: 8000,
	}, nil)
	if !sawBigPrompt {
		t.Fatal("a provider that reports nothing was not measured")
	}
	if got.Outcome != Verified {
		t.Fatalf("outcome = %q (%s)", got.Outcome, got.Detail)
	}
}
