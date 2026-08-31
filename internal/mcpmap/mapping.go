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

// Package mcpmap maps CodeDistill item operations (create / update /
// status_change / delete) to specific tools on a remote MCP server.
//
// The mapping is the bridge between CodeDistill's uniform item-type
// model and a destination's bespoke tool surface — Linear calls it
// "create_issue", Jira calls it "createIssue", GitHub calls it
// "create_an_issue", and we don't want to hard-code that.
//
// Two paths produce a Mapping:
//  1. Suggest — feed the destination's catalog (from
//     mcpclient.ListTools) to an LLM (Ollama) and ask it which tool
//     implements each operation. Cheap, often-right, occasionally
//     wrong. Empty string = "I don't know."
//  2. Manual override — the user picks the tool for an operation in
//     the settings UI. Wins over the LLM suggestion on collision.
//
// Operations with an empty mapping are *unavailable* for that
// destination — the export queue worker drops or fails the op rather
// than guessing. Better to surface "delete is not configured" than to
// pick the wrong tool and trash a real ticket.
package mcpmap

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"codedistill/internal/mcpclient"
)

// Operation is one of the four CodeDistill→destination operations
// the export pipeline supports. The set is closed (full CRUD plus
// status_change as a separate op so destinations can route a "close
// ticket" to a dedicated transition tool rather than a generic update).
type Operation string

const (
	OpCreate       Operation = "create"
	OpUpdate       Operation = "update"
	OpStatusChange Operation = "status_change"
	OpDelete       Operation = "delete"
)

// AllOperations is the canonical iteration order. Used by both the
// prompt builder (so the LLM sees a stable list) and the worker.
var AllOperations = []Operation{OpCreate, OpUpdate, OpStatusChange, OpDelete}

// Mapping is op → remote tool name. Empty string = "unavailable for
// this destination" — the worker treats those operations as a no-op
// and surfaces the gap in the UI rather than retrying.
//
// Stored as JSON inside user_settings.mcp.export.<type>.mapping.
type Mapping map[Operation]string

// Suggester is the narrow LLM interface this package needs. Defined
// here (rather than imported from internal/ollama) so the package is
// independently testable with a fake. *ollama.Client satisfies it.
type Suggester interface {
	GenerateJSON(ctx context.Context, prompt string) (string, error)
}

// Suggest asks the LLM to map operations to tools from catalog. itemType
// is "todo" | "bug" | "kb" | "use_case" — used to shape the prompt with
// item-specific guidance.
//
// Robustness:
//   - missing keys in the LLM response → "" (unavailable)
//   - extra keys in the response → ignored
//   - tool names that aren't in the catalog → kept verbatim; the caller
//     should run Validate to surface those as errors. Suggest itself
//     doesn't drop them so the UI can show "the LLM thought it was X
//     but X isn't a real tool — pick again."
//   - parse failure → returned as an error; caller should fall back to
//     manual mapping
func Suggest(ctx context.Context, llm Suggester, itemType string, catalog []mcpclient.Tool) (Mapping, error) {
	if llm == nil {
		return nil, fmt.Errorf("mcpmap: nil suggester")
	}
	if len(catalog) == 0 {
		return nil, fmt.Errorf("mcpmap: empty catalog")
	}

	prompt := buildPrompt(itemType, catalog)
	raw, err := llm.GenerateJSON(ctx, prompt)
	if err != nil {
		return nil, fmt.Errorf("mcpmap: suggest: %w", err)
	}
	return parseSuggestion(raw)
}

// Validate splits m into (kept, errs). kept contains entries whose
// tool name appears in catalog (or is empty). errs contains one error
// per entry whose tool name isn't in the catalog — typically a stale
// mapping after the destination's tool surface changed, or an LLM
// hallucination that survived to here.
//
// kept is always usable; the caller can persist it and surface errs to
// the user as warnings.
func Validate(m Mapping, catalog []mcpclient.Tool) (kept Mapping, errs []error) {
	known := make(map[string]struct{}, len(catalog))
	for _, t := range catalog {
		known[t.Name] = struct{}{}
	}
	kept = make(Mapping, len(m))
	for _, op := range AllOperations {
		name, ok := m[op]
		if !ok || name == "" {
			kept[op] = ""
			continue
		}
		if _, hit := known[name]; !hit {
			errs = append(errs, fmt.Errorf("op %q maps to unknown tool %q", op, name))
			kept[op] = ""
			continue
		}
		kept[op] = name
	}
	return kept, errs
}

// Available returns the operations m provides (non-empty mapping), in
// AllOperations order. Used by the worker to skip enqueueing for
// unavailable ops and by the UI to render which ops are wired up.
func Available(m Mapping) []Operation {
	out := make([]Operation, 0, len(AllOperations))
	for _, op := range AllOperations {
		if m[op] != "" {
			out = append(out, op)
		}
	}
	return out
}

// buildPrompt constructs the LLM prompt. Kept private so the prompt
// can evolve without breaking callers; tested via Suggest's behavior.
//
// The prompt is intentionally short — qwen2.5:7b (the default) handles
// this fine and a longer prompt costs latency on the worker path.
// Catalog tool descriptions are truncated to keep the context stable
// across destinations with verbose schemas.
func buildPrompt(itemType string, catalog []mcpclient.Tool) string {
	var b strings.Builder

	b.WriteString("You are mapping CodeDistill operations to tools on a remote MCP server.\n\n")
	b.WriteString("Item type: ")
	b.WriteString(itemTypeDescription(itemType))
	b.WriteString("\n\nFor each of the four operations below, pick the single tool from the catalog that best implements it.\n")
	b.WriteString("If no tool fits, use an empty string. Do not invent tool names.\n\n")

	b.WriteString("Operations:\n")
	for _, op := range AllOperations {
		b.WriteString("  - ")
		b.WriteString(string(op))
		b.WriteString(": ")
		b.WriteString(opDescription(op, itemType))
		b.WriteString("\n")
	}

	b.WriteString("\nAvailable tools:\n")
	// Sort catalog by name so the prompt is deterministic across calls
	// — same catalog, same prompt, same suggestion (with temperature=0).
	sorted := make([]mcpclient.Tool, len(catalog))
	copy(sorted, catalog)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Name < sorted[j].Name })
	for _, t := range sorted {
		desc := strings.TrimSpace(t.Description)
		if len(desc) > 200 {
			desc = desc[:200] + "..."
		}
		fmt.Fprintf(&b, "  - %s: %s\n", t.Name, desc)
	}

	b.WriteString("\nRespond with JSON of the form:\n")
	b.WriteString(`{"create": "tool_name", "update": "tool_name", "status_change": "tool_name", "delete": "tool_name"}`)
	b.WriteString("\nUse exactly these four keys. Use \"\" for any operation with no fitting tool.\n")
	return b.String()
}

func itemTypeDescription(itemType string) string {
	switch itemType {
	case "todo":
		return "a todo (a task to be done; has a subject, optional description, status open/done)"
	case "bug":
		return "a bug report (a defect; has a subject, description, status open/fixed)"
	case "kb":
		return "a knowledge-base entry (a note or piece of documentation; has a title and body)"
	case "use_case":
		return "a use case (a product capability described as role/want/why; has status proposed/implemented)"
	default:
		return fmt.Sprintf("an item of type %q", itemType)
	}
}

func opDescription(op Operation, itemType string) string {
	switch op {
	case OpCreate:
		return fmt.Sprintf("create a new %s in the destination", itemType)
	case OpUpdate:
		return fmt.Sprintf("update an existing %s's title / description / fields", itemType)
	case OpStatusChange:
		return fmt.Sprintf("change a %s's status (e.g. open → closed/done)", itemType)
	case OpDelete:
		return fmt.Sprintf("delete an existing %s", itemType)
	default:
		return string(op)
	}
}

// parseSuggestion decodes the LLM's JSON response into a Mapping. The
// LLM is prompted to produce {"create": ..., "update": ..., ...}; we
// accept any JSON object with string values and pick out our four keys.
// Extra keys are silently ignored; missing keys default to "".
func parseSuggestion(raw string) (Mapping, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, fmt.Errorf("mcpmap: empty LLM response")
	}
	var obj map[string]any
	if err := json.Unmarshal([]byte(raw), &obj); err != nil {
		return nil, fmt.Errorf("mcpmap: parse LLM response: %w (raw: %q)", err, raw)
	}
	out := make(Mapping, len(AllOperations))
	for _, op := range AllOperations {
		v, ok := obj[string(op)]
		if !ok {
			out[op] = ""
			continue
		}
		// Tolerate both string and null; anything else → "".
		s, _ := v.(string)
		out[op] = s
	}
	return out, nil
}
