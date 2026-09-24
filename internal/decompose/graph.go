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
// Layer 3 — nodes. What the SYSTEM has.
//
// A deterministic fold of layer 2's moves into the deltas the document
// describes. No model runs here, and that is the point: if 2→3 needed one, the
// graph would be a second opinion about the document rather than a consequence
// of the first one, and there would be nothing to check it against.
//
// The relation set is closed and small on purpose. An ontology that can grow
// without a document forcing it to grow is how this stops being a feature and
// becomes a taxonomy project.
package decompose

import (
	"fmt"
	"sort"
	"strings"
)

// BuildGraph folds moves into nodes. Same moves, same graph, every time.
func BuildGraph(doc *Document, moves []Move) *Graph {
	g := &Graph{}
	bySentence := map[int][]int{} // introducing sentence -> node indices

	for _, m := range moves {
		if m.Role != RoleIntroduce {
			continue
		}
		idx := len(g.Nodes)
		g.Nodes = append(g.Nodes, Node{
			ID:      fmt.Sprintf("n%d", idx+1),
			Intent:  strings.TrimSpace(m.Label),
			State:   StateAsserted,
			Origin:  m.Sentence,
			Sources: []int{m.Sentence},
		})
		bySentence[m.Sentence] = append(bySentence[m.Sentence], idx)
	}

	for _, m := range moves {
		switch m.Role {
		case RoleMeta:
			g.MetaMoves++
			continue
		case RoleIntroduce:
			continue
		}

		pick, inferred := -1, false
		switch {
		case m.Target == nil && m.Role == RoleRetract:
			// NEVER guess the target of a retraction. The costs are wildly
			// asymmetric: a misplaced elaboration adds a sentence to the wrong
			// item and someone notices; a misplaced retraction DELETES a real
			// requirement and nobody does.
			//
			// Found on a three-sentence document: "Single sign-on ... is
			// deferred" arrived with no target, attached to the nearest open
			// idea — the unrelated export requirement — and silently retracted
			// it. The run reported zero proposals and no error.
			g.RetractsDropped++
			continue
		case m.Target == nil:
			// An elaboration without a named target is different, and common:
			// on a human-written document 58% of them arrived this way, so
			// dropping them discards most of the signal the layer exists to
			// find. Attach to the nearest idea opened before this sentence —
			// the common case in prose — and mark it inferred so nothing
			// downstream can claim the model asserted the link.
			pick = nearestOpenBefore(g.Nodes, m.Sentence)
			inferred = pick >= 0
		default:
			cands := bySentence[*m.Target]
			if len(cands) > 0 {
				pick = cands[0]
				if len(cands) > 1 {
					g.Ambiguous++
					pick = bestByLabel(g.Nodes, cands, m.Label)
				}
			}
		}
		if pick < 0 {
			g.Orphans++
			continue
		}

		// A retraction must plausibly name what it withdraws. The checker in
		// layer 2 only proves the target introduced SOMETHING, which pressures
		// a model into naming the nearest available idea when the thing being
		// deferred was never introduced at all.
		//
		// Measured: on a three-sentence document, "Single sign-on ... is
		// deferred to next year" named the unrelated export requirement as its
		// target and withdrew it. Proposals went from 1 corroborated to 0, with
		// no error and no warning. Requiring a single shared content word
		// between the move's label and the node's intent blocks that without
		// touching legitimate retractions, which always name their subject.
		if m.Role == RoleRetract && !sharesAWord(m.Label, g.Nodes[pick].Intent) {
			g.RetractsDropped++
			continue
		}

		n := &g.Nodes[pick]
		n.Sources = append(n.Sources, m.Sentence)
		if inferred {
			n.Inferred = true
			g.InferredLinks++
		}
		if m.Role == RoleRetract {
			// A document that defers something must not produce work for it.
			// The one-pass extractor had no way to express this at all.
			n.State = StateRetracted
		}
	}

	for i := range g.Nodes {
		g.Nodes[i].Lines = citeLines(doc, g.Nodes[i].Sources)
	}
	return g
}

// nearestOpenBefore returns the last node introduced before this sentence, or
// -1. Retracted ideas are skipped: a sentence elaborating something the
// document already withdrew is far more likely to be about whatever came next.
func nearestOpenBefore(nodes []Node, sentence int) int {
	best := -1
	for i, n := range nodes {
		if n.Origin < sentence && n.State == StateAsserted {
			if best < 0 || n.Origin >= nodes[best].Origin {
				best = i
			}
		}
	}
	return best
}

// bestByLabel picks among nodes introduced by one sentence, using the label on
// the refining move. Word overlap is crude, but always taking the first
// silently attaches detail to the wrong idea, and being crude in the open beats
// being wrong in the dark.
func bestByLabel(nodes []Node, cands []int, label string) int {
	want := words(label)
	best, bestScore := cands[0], -1
	for _, c := range cands {
		score := 0
		for w := range words(nodes[c].Intent) {
			if want[w] {
				score++
			}
		}
		if score > bestScore {
			best, bestScore = c, score
		}
	}
	return best
}

// sharesAWord reports whether two labels have any content word in common. A
// deliberately low bar: it exists to catch a retraction aimed at something it
// never mentions, not to judge how well the model phrased things.
func sharesAWord(a, b string) bool {
	wa := words(a)
	for w := range words(b) {
		if wa[w] {
			return true
		}
	}
	return false
}

func words(s string) map[string]bool {
	out := map[string]bool{}
	for _, w := range strings.Fields(strings.ToLower(s)) {
		if w = strings.Trim(w, ".,:;()\"'"); len(w) > 3 {
			out[w] = true
		}
	}
	return out
}

// citeLines turns sentence indices into merged, sorted line ranges.
func citeLines(doc *Document, sentences []int) [][2]int {
	var out [][2]int
	seen := map[int]bool{}
	for _, s := range sentences {
		if s < 0 || s >= len(doc.Sentences) || seen[s] {
			continue
		}
		seen[s] = true
		sn := doc.Sentences[s]
		out = append(out, [2]int{sn.Line1, sn.Line2})
	}
	sort.Slice(out, func(i, j int) bool { return out[i][0] < out[j][0] })

	var merged [][2]int
	for _, r := range out {
		if k := len(merged) - 1; k >= 0 && r[0] <= merged[k][1]+1 {
			if r[1] > merged[k][1] {
				merged[k][1] = r[1]
			}
			continue
		}
		merged = append(merged, r)
	}
	return merged
}

// FormatLines renders a citation the way a human reads it: "10-13,40".
func FormatLines(r [][2]int) string {
	var p []string
	for _, x := range r {
		if x[0] == x[1] {
			p = append(p, fmt.Sprintf("%d", x[0]))
		} else {
			p = append(p, fmt.Sprintf("%d-%d", x[0], x[1]))
		}
	}
	return strings.Join(p, ",")
}
