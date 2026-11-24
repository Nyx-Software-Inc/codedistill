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

// Package agentloop is CodeDistill's own agent loop: the built-in connector
// (docs/design/agent-loop.md). It gives a model the role's brief and tools,
// runs what the model asks for, sends back the results, and repeats until
// the model stops asking, the step limit is reached, or the run is cancelled.
//
// It knows nothing of providers (the model is anything with Chat) or of the
// Conductor (CodeDistill's own tools arrive as a Remote): the worker command
// wires those in, and a test wires in a scripted model.
package agentloop

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"codedistill/internal/ollama"
)

// DefaultStepLimit is how many model turns one run may take
// (conductor.step_limit overrides it).
const DefaultStepLimit = 100

// Model is what the loop talks to: one turn in, one turn out.
type Model interface {
	Chat(ctx context.Context, r ollama.ChatRequest) (*ollama.ChatResponse, error)
}

// Remote is CodeDistill's own tools, reached over the run's endpoint.
type Remote interface {
	Tools() []ollama.ToolSpec
	Call(ctx context.Context, name string, args json.RawMessage) (text string, isError bool, err error)
}

// Config is one run.
type Config struct {
	Dir     string // the checkout; every path is inside it
	Brief   string // the role's brief, the first user message
	CanEdit bool   // edit_file and write_file are offered
	Model   Model
	Remote  Remote // may be nil (run by hand)

	StepLimit     int // model turns; 0: DefaultStepLimit
	ContextTokens int // the model's context size; 0: assume 128k
	// OnTool is told of each tool call as it starts (progress notes).
	OnTool func(name string, args json.RawMessage)
	// OnTurn is told the run's totals after each model turn (live token
	// counts in the job card).
	OnTurn func(turns int, usage ollama.ChatUsage)
	Log    io.Writer // the run's log; nil: discarded
}

// Result is how a run ended and what it cost.
type Result struct {
	Turns     int
	Usage     ollama.ChatUsage
	ToolCalls map[string]int
	Text      string // the model's last words
	Stopped   string // "done" | "step limit" | "cancelled"
}

// systemPrompt frames every run; the role's own instructions are the brief.
const systemPrompt = `You are a coding agent working inside CodeDistill on one task, described in the first message. You work in a checkout of the project; every path you use is relative to it.

Use the tools to read and search the code, run commands (builds, tests, scanners), and make changes. Read before you edit: edit_file replaces text that must match the file exactly, including indentation, so copy it from what read_file showed (without the line numbers). Prefer small, exact edits to rewriting whole files.

When a tool reports an error, read it and correct course. Do not repeat a call that failed the same way.

Do not push, switch branches, rewrite history or change git configuration. Do not try to reach outside the checkout.

When the task is done, and you have recorded what the task asks you to record with CodeDistill's tools, stop calling tools and reply with a short summary of what you did.`

// nudge is said when a model stops calling tools right after one failed.
const nudge = "Your last tool call failed and you have not made another. If the task is finished, reply with your summary. If it is not, continue now by calling a tool; do not only describe what you will do."

// maxNudges bounds how often one run is nudged, so a model that has truly
// finished is not kept going.
const maxNudges = 3

// Run runs the loop to its end.
func Run(ctx context.Context, cfg Config) (*Result, error) {
	if cfg.StepLimit <= 0 {
		cfg.StepLimit = DefaultStepLimit
	}
	if cfg.ContextTokens <= 0 {
		cfg.ContextTokens = 128_000
	}
	log := cfg.Log
	if log == nil {
		log = io.Discard
	}
	local := newLocalTools(cfg.Dir, cfg.CanEdit)
	tools := local.specs()
	remote := map[string]bool{}
	if cfg.Remote != nil {
		for _, t := range cfg.Remote.Tools() {
			if local.has(t.Name) {
				continue // ours win; the endpoint's names never collide today
			}
			remote[t.Name] = true
			tools = append(tools, t)
		}
	}

	res := &Result{ToolCalls: map[string]int{}}
	msgs := []ollama.ChatMessage{{Role: "user", Text: cfg.Brief}}
	lastFailed, nudges := false, 0
	for {
		if ctx.Err() != nil {
			res.Stopped = "cancelled"
			return res, nil
		}
		if res.Turns >= cfg.StepLimit {
			res.Stopped = "step limit"
			fmt.Fprintf(log, "[built-in] stopped: the step limit (%d turns) was reached\n", cfg.StepLimit)
			return res, nil
		}
		trimContext(msgs, cfg.ContextTokens)
		resp, err := chatWithRetry(ctx, cfg.Model, ollama.ChatRequest{System: systemPrompt, Messages: msgs, Tools: tools}, log)
		if err != nil {
			if ctx.Err() != nil {
				res.Stopped = "cancelled"
				return res, nil
			}
			return res, err
		}
		res.Turns++
		res.Usage.In += resp.Usage.In
		res.Usage.Out += resp.Usage.Out
		res.Usage.CacheRead += resp.Usage.CacheRead
		res.Usage.CacheWrite += resp.Usage.CacheWrite
		if cfg.OnTurn != nil {
			cfg.OnTurn(res.Turns, res.Usage)
		}
		if strings.TrimSpace(resp.Text) != "" {
			res.Text = resp.Text
			fmt.Fprintf(log, "[turn %d] %s\n", res.Turns, resp.Text)
		}
		msgs = append(msgs, ollama.ChatMessage{Role: "assistant", Text: resp.Text, ToolCalls: resp.ToolCalls})
		if len(resp.ToolCalls) == 0 {
			// A model that answers a failed call with what it means to do,
			// and no call, has usually not finished: ask once.
			if lastFailed && nudges < maxNudges {
				nudges++
				lastFailed = false
				fmt.Fprintf(log, "[built-in] the last tool call failed and no call followed; asking whether it is done\n")
				msgs = append(msgs, ollama.ChatMessage{Role: "user", Text: nudge})
				continue
			}
			res.Stopped = "done"
			return res, nil
		}
		lastFailed = false

		results := make([]ollama.ToolResult, 0, len(resp.ToolCalls))
		for _, tc := range resp.ToolCalls {
			res.ToolCalls[tc.Name]++
			if cfg.OnTool != nil {
				cfg.OnTool(tc.Name, tc.Args)
			}
			fmt.Fprintf(log, "[turn %d] %s %s\n", res.Turns, tc.Name, clip(string(tc.Args), 300))
			var out string
			var isErr bool
			switch {
			case local.has(tc.Name):
				out, isErr = local.call(ctx, tc.Name, tc.Args)
			case remote[tc.Name]:
				var err error
				out, isErr, err = cfg.Remote.Call(ctx, tc.Name, tc.Args)
				if err != nil {
					out, isErr = "the call failed: "+err.Error(), true
				}
			default:
				out, isErr = fmt.Sprintf("there is no tool called %q", tc.Name), true
			}
			out = capOutput(out)
			if isErr {
				fmt.Fprintf(log, "    error: %s\n", clip(out, 300))
			}
			results = append(results, ollama.ToolResult{CallID: tc.ID, Name: tc.Name, Content: out, IsError: isErr})
			lastFailed = lastFailed || isErr
		}
		msgs = append(msgs, ollama.ChatMessage{Role: "tool", ToolResults: results})
	}
}

// chatWithRetry rides out a provider's brief refusals (rate limits,
// overload, a dropped connection): three more tries, backing off. A request
// the provider rejects outright fails at once.
func chatWithRetry(ctx context.Context, m Model, r ollama.ChatRequest, log io.Writer) (*ollama.ChatResponse, error) {
	wait := 5 * time.Second
	for attempt := 0; ; attempt++ {
		resp, err := m.Chat(ctx, r)
		if err == nil || attempt == 3 || !transient(err) {
			return resp, err
		}
		fmt.Fprintf(log, "[built-in] the model did not answer (%v); trying again in %s\n", err, wait)
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(wait):
		}
		wait *= 3
	}
}

func transient(err error) bool {
	if errors.Is(err, context.Canceled) {
		return false
	}
	s := strings.ToLower(err.Error())
	for _, k := range []string{"status 429", "status 500", "status 502", "status 503", "status 504", "status 529",
		"overloaded", "rate limit", "rate_limit", "connection reset", "eof", "timeout", "unavailable"} {
		if strings.Contains(s, k) {
			return true
		}
	}
	return false
}

// outputCap bounds one tool result, so one noisy command cannot fill the
// model's context.
const outputCap = 16 << 10

// capOutput keeps a result under the cap: the head and the tail (where a
// command's verdict is), with a line saying how much was cut.
func capOutput(s string) string {
	if len(s) <= outputCap {
		return s
	}
	head, tail := s[:outputCap/4], s[len(s)-outputCap*3/4:]
	return fmt.Sprintf("%s\n… [%d bytes cut; narrow the command or read a smaller part] …\n%s", head, len(s)-len(head)-len(tail), tail)
}

func clip(s string, n int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	if len(s) > n {
		return s[:n] + "…"
	}
	return s
}

// trimContext keeps the conversation inside the model's context: when it
// nears the limit, the oldest tool results are replaced by a stub saying
// what they were, newest last. The brief and the model's own words stay.
// Tokens are estimated at four characters each, which is close for code and
// English, and the margin (three quarters of the context) absorbs the rest.
func trimContext(msgs []ollama.ChatMessage, contextTokens int) {
	budget := contextTokens * 3 / 4 * 4 // in characters
	size := len(systemPrompt)
	for _, m := range msgs {
		size += messageSize(m)
	}
	// The last tool turn is what the model is about to read: never trimmed.
	for i := 0; i < len(msgs)-1 && size > budget; i++ {
		if msgs[i].Role != "tool" {
			continue
		}
		for j, tr := range msgs[i].ToolResults {
			if len(tr.Content) <= 200 {
				continue
			}
			stub := fmt.Sprintf("[the result of this %s call was dropped to save room; call it again if you still need it]", tr.Name)
			size -= len(tr.Content) - len(stub)
			msgs[i].ToolResults[j].Content = stub
		}
	}
}

func messageSize(m ollama.ChatMessage) int {
	n := len(m.Text)
	for _, tc := range m.ToolCalls {
		n += len(tc.Name) + len(tc.Args)
	}
	for _, tr := range m.ToolResults {
		n += len(tr.Content)
	}
	return n
}
