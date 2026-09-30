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

package decompose

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// Layer 5: what has to happen before what.
//
// A SWEEP, not a pass. Layers 2 and 4 work in batches because they ask about
// one slice of the document at a time; dependency is a relationship BETWEEN
// slices, and a batched view cannot see an edge whose ends are thirty sentences
// apart. Since a whole decomposition of a 150-sentence spec costs under a
// minute on a hosted model, one more full-document call is the cheap half of
// the only design that can work.
//
// The output is proposals, like everything else here: forty-nine items with
// edges nobody ratified would be a plan asserted by a model, which is the thing
// this product exists not to do.

// SeqItem is anything that can be ordered.
//
// Four fields, because that is all the sweep ever needed. It was written
// against Proposal and took a *Document it never read — which hid the fact
// that ordering has nothing to do with documents. Items typed by hand, derived
// by the classifier or imported over MCP are exactly as orderable as items a
// decomposition proposed, and the only reason they could not be ordered was
// that this function insisted on the wrong argument.
type SeqItem struct {
	ID      string // caller's own id; echoed back on each edge
	Ref     string // the author's reference (UC-07) when there is one
	Kind    string
	Subject string
	Body    string
}

// Edge is a proposed ordering between two items.
type Edge struct {
	// From depends on To. To happens first. These are the caller's own ids,
	// resolved from the labels the model was shown.
	From string `json:"from"`
	To   string `json:"to"`

	// The labels as shown, kept for the review screen: "UC-7 waits on UC-3"
	// reads as something a person can check, an id pair does not.
	FromRef string `json:"from_ref,omitempty"`
	ToRef   string `json:"to_ref,omitempty"`

	// Kind is "blocks" or "informs". Both offered deliberately: given only
	// "blocks", a model asked for dependencies claims everything blocks
	// everything, and a graph where all 49 items block each other carries
	// exactly as much information as no graph.
	Kind string `json:"kind"`

	// Why, in the document's words, so a reviewer can check rather than trust.
	Rationale string `json:"why"`
}

// sequencePrompt asks for order, and works hard at not being agreed with.
//
// The failure mode here is not fabrication — the identifiers are supplied and
// checked on the way back — it is EAGERNESS. Asked "which of these depend on
// each other", a model will relate everything to everything, and a complete
// graph is indistinguishable from no graph. Hence: the default answer is no
// edge, the weaker relationship is offered as an escape hatch, and each claim
// must carry its reason.
//
// The varying block goes LAST so the fixed instructions stay a reusable prompt
// prefix — the same reason the reading prompt is ordered that way, worth 8m27s
// to 2m10s on a local model.
const sequencePrompt = `You are ordering work derived from a specification.

Below is a numbered list of work items. Say which ones must be done before
which others.

Answer with JSON: {"edges":[{"from":"UC-7","to":"UC-3","kind":"blocks","why":"..."}]}

  from   the item that has to WAIT
  to     the item that has to happen FIRST
  kind   "blocks"  - from cannot be started until to is done
         "informs" - from is easier once to exists, but is not blocked
  why    one short sentence, grounded in what the items say

Rules:
- MOST PAIRS HAVE NO RELATIONSHIP. Returning few edges is the correct answer
  for most documents. An empty list is a valid answer.
- Only claim "blocks" when starting the work is genuinely impossible first —
  it needs data the other creates, or a screen the other builds.
- Two items touching the same feature are NOT dependent. Sharing a subject is
  not an ordering.
- Never invent an identifier. Use only the ones listed.
- Do not create cycles.

Items:
%s`

// Sequence asks for the ordering over every accepted proposal, in one call.
//
// Returns only edges whose endpoints both exist, are not self-edges, and do not
// duplicate one already returned. Everything discarded is counted rather than
// hidden: a model that names items that do not exist is a model to distrust,
// and that is only visible if the number is reported.
func Sequence(ctx context.Context, g Generator, items []SeqItem) ([]Edge, int, error) {
	if len(items) < 2 {
		// Nothing to order. Not an error, and not worth a model call.
		return nil, 0, nil
	}

	// Stable handles. The author's own reference when the document supplies one
	// (UC-07 and such), because a model echoes a meaningful id far more
	// reliably than an opaque one; otherwise a positional label.
	label := make([]string, len(items))
	byLabel := map[string]int{}
	var b strings.Builder
	for i, p := range items {
		l := strings.TrimSpace(p.Ref)
		if l == "" || byLabel[l] != 0 {
			l = fmt.Sprintf("%s-%d", kindTag(p.Kind), i+1)
		}
		label[i] = l
		byLabel[l] = i + 1 // 1-based; 0 means absent
		fmt.Fprintf(&b, "%s [%s] %s\n", l, p.Kind, strings.TrimSpace(p.Subject))
		if body := strings.TrimSpace(p.Body); body != "" {
			fmt.Fprintf(&b, "    %s\n", firstSentence(body))
		}
	}

	raw, err := callBounded(ctx, g, len(items), fmt.Sprintf(sequencePrompt, b.String()))
	if err != nil {
		return nil, 0, err
	}

	var out struct {
		Edges []Edge `json:"edges"`
	}
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return nil, 0, fmt.Errorf("sequence: %w", err)
	}

	seen := map[string]bool{}
	edges := make([]Edge, 0, len(out.Edges))
	rejected := 0
	for _, e := range out.Edges {
		from, to := byLabel[strings.TrimSpace(e.From)], byLabel[strings.TrimSpace(e.To)]
		switch {
		case from == 0 || to == 0:
			// An identifier that was not on the list. The one failure that
			// would let an edge point at nothing.
			rejected++
			continue
		case from == to:
			rejected++
			continue
		}
		key := fmt.Sprintf("%d>%d", from, to)
		if seen[key] {
			rejected++
			continue
		}
		// The reverse of an edge already claimed is a two-item cycle, and the
		// shortest kind to catch here rather than in whatever renders the graph.
		if seen[fmt.Sprintf("%d>%d", to, from)] {
			rejected++
			continue
		}
		seen[key] = true

		kind := strings.ToLower(strings.TrimSpace(e.Kind))
		if kind != "informs" {
			kind = "blocks"
		}
		edges = append(edges, Edge{
			From:      items[from-1].ID,
			To:        items[to-1].ID,
			FromRef:   label[from-1],
			ToRef:     label[to-1],
			Kind:      kind,
			Rationale: strings.TrimSpace(e.Rationale),
		})
	}
	return edges, rejected, nil
}

func kindTag(kind string) string {
	switch kind {
	case "use_case":
		return "UC"
	case "todo":
		return "TD"
	case "bug":
		return "BG"
	case "kb":
		return "KB"
	}
	return "IT"
}

// firstSentence keeps the prompt small. The whole body of forty-nine items
// would crowd out the instructions, and one sentence is enough to tell two
// items apart — which is all the ordering question needs.
func firstSentence(s string) string {
	if i := strings.IndexAny(s, ".\n"); i > 0 && i < 160 {
		return s[:i+1]
	}
	if len(s) > 160 {
		return s[:160] + "…"
	}
	return s
}
