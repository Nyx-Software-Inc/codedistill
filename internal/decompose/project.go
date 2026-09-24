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
// Layer 4 — proposals. The projection into the four kinds CodeDistill has.
//
// The cheap, repeatable end of the pipeline, and the whole argument for the
// layers above it. Reading a document costs minutes of model time; projecting
// the graph costs seconds. Getting a kind wrong is a re-projection, not a
// re-read.
//
// Retracted nodes are not projected: a document saying "SSO is deferred" must
// not produce an SSO use case.
package decompose

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// KindNone is the outcome that stops the junk. Not every idea in a document is
// work — a title, a table header, a sentence describing the document itself.
// With only four kinds to choose from the model must call one of them
// something, and a measured run turned "Product Description" into a knowledge
// entry and "Define priorities" (a table header) into a todo. Giving it a way
// to decline is cheaper and more honest than filtering afterwards.
const KindNone = "none"

// Skipped is a node the projection declined to turn into work, kept so a run
// can show what it chose not to create rather than silently dropping it.
type Skipped struct {
	NodeID string   `json:"node_id"`
	Intent string   `json:"intent"`
	Reason string   `json:"reason"`
	Lines  [][2]int `json:"lines"`
}

const projectPrompt = `You are labelling units of work for a software project tracker.

Each unit below is an idea taken from a specification, with the sentences
describing it. Assign each one exactly one kind:

  use_case  a capability from the user's point of view — something a person can
            do with the system once it is built.
  todo      a concrete engineering task, change, or piece of work to perform.
  bug       something described as broken, incorrect, or contradicting intent.
  kb        a durable fact, rule, convention or decision worth recalling, which
            is not itself work to be done.
  none      NOT WORK AT ALL. The document's own title or subtitle, a section
            heading, a table header, a note about the document rather than the
            system, or an idea too vague to act on. Choosing none is correct and
            expected — a document contains furniture as well as requirements,
            and labelling furniture as work produces items nobody wants.

For each unit return:
  node     the unit's id, exactly as given
  kind     use_case | todo | bug | kb | none
  subject  a short imperative label under 80 characters (omit for none)
  reason   for none ONLY: a few words on why this is not work

Return JSON only: {"items":[{"node":"n1","kind":"use_case","subject":"..."}]}

--- UNITS ---
%s--- END ---`

// Project turns asserted nodes into proposals. Nodes are batched so this is one
// model call per batch rather than one per node.
//
// progress, when non-nil, reports batches finished out of the total. The total
// is only knowable here — not when the job started — because it depends on how
// many ideas layers 2 and 3 found.
func Project(ctx context.Context, g Generator, doc *Document, graph *Graph, batch int, progress func(done, total int)) ([]Proposal, []Skipped, int, error) {
	if batch <= 0 {
		batch = 12
	}
	var live []Node
	for _, n := range graph.Nodes {
		if n.State == StateAsserted {
			live = append(live, n)
		}
	}
	byID := map[string]Node{}
	for _, n := range live {
		byID[n.ID] = n
	}

	total := (len(live) + batch - 1) / batch
	if progress != nil {
		progress(0, total)
	}

	var proposals []Proposal
	var skipped []Skipped
	// Counted, not swallowed. A run that produces nothing must be able to say
	// whether the model named nodes that do not exist or genuinely found no
	// work — those need opposite fixes.
	unusable := 0
	for start, b := 0, 0; start < len(live); start, b = start+batch, b+1 {
		if err := ctx.Err(); err != nil {
			return proposals, skipped, unusable, err
		}
		end := start + batch
		if end > len(live) {
			end = len(live)
		}

		raw, err := callBounded(ctx, g, end-start, fmt.Sprintf(projectPrompt, renderUnits(doc, live[start:end])))
		if err != nil {
			return proposals, skipped, unusable, fmt.Errorf("nodes %d-%d: %w", start, end-1, err)
		}

		var out struct {
			Items []struct {
				Node    string `json:"node"`
				Kind    string `json:"kind"`
				Subject string `json:"subject"`
				Reason  string `json:"reason"`
			} `json:"items"`
		}
		if err := json.Unmarshal([]byte(raw), &out); err != nil {
			return proposals, skipped, unusable, fmt.Errorf("nodes %d-%d decode: %w", start, end-1, err)
		}

		if len(out.Items) == 0 {
			// Nodes went in and nothing came back. Distinct from declining
			// them: the model either ignored the schema or returned an empty
			// list, and a run reporting zero proposals with zero declines and
			// zero errors is otherwise indistinguishable from "the document
			// contained no work".
			unusable += end - start
		}
		for _, it := range out.Items {
			n, ok := byID[it.Node]
			if !ok {
				// A node id not in this graph has nothing to attach a citation
				// to, so it cannot be shown — but it is reported.
				unusable++
				continue
			}
			switch {
			case it.Kind == KindNone:
				skipped = append(skipped, Skipped{
					NodeID: n.ID, Intent: n.Intent, Lines: n.Lines,
					Reason: strings.TrimSpace(it.Reason),
				})
			case ValidKind(it.Kind):
				subject := strings.TrimSpace(it.Subject)
				if subject == "" {
					subject = n.Intent // never ship a nameless proposal
				}
				proposals = append(proposals, Proposal{
					NodeID: n.ID, Kind: it.Kind, Subject: subject,
					Lines: n.Lines, Sources: n.Sources,
				})
			}
		}
		if progress != nil {
			progress(b+1, total)
		}
	}
	return proposals, skipped, unusable, nil
}

// ValidKind reports whether k is one of the four item kinds. KindNone is
// deliberately not one of them: it means "do not create anything".
func ValidKind(k string) bool {
	switch k {
	case "use_case", "todo", "bug", "kb":
		return true
	}
	return false
}

func renderUnits(doc *Document, nodes []Node) string {
	var b strings.Builder
	for _, n := range nodes {
		fmt.Fprintf(&b, "\n[%s] %s\n", n.ID, n.Intent)
		for _, s := range n.Sources {
			if s >= 0 && s < len(doc.Sentences) {
				fmt.Fprintf(&b, "    %s\n", doc.Sentences[s].Text)
			}
		}
	}
	return b.String()
}
