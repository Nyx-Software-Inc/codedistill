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
// Layers 1 and 3 run no model, so they are ordinary code and get ordinary
// tests. That is most of the argument for splitting the pipeline up: the
// one-pass version had nothing to unit-test, only a percentage to squint at.
package decompose

import (
	"strings"
	"testing"
)

func ptr(i int) *int { return &i }

func lines(s string) []string { return strings.Split(strings.TrimPrefix(s, "\n"), "\n") }

func TestSegmentTracksLineProvenance(t *testing.T) {
	doc := Segment(lines(`
# Export

Users must be able to download a project archive. It must not
require contacting support.

| Pipeline | Studio → sign | Feed → validate |
| -------- | ------------- | --------------- |

## Limits

` + "```go" + `
func nope() {} // not prose
` + "```" + `

Invite tokens expire after 72 hours, e.g. three days.
`))

	if len(doc.Sentences) != 3 {
		for i, s := range doc.Sentences {
			t.Logf("  %d: L%d-%d %q", i, s.Line1, s.Line2, s.Text)
		}
		t.Fatalf("got %d sentences, want 3", len(doc.Sentences))
	}

	// The citation guarantee lives or dies here: a sentence must know the
	// lines it came from, including when it wraps.
	// Each sentence gets ITS OWN span, not the paragraph's. The first ends on
	// line 3; only the second reaches line 4.
	if got := doc.Sentences[0]; got.Line1 != 3 || got.Line2 != 3 {
		t.Errorf("sentence 0 spans L%d-%d, want L3-3", got.Line1, got.Line2)
	}
	if got := doc.Sentences[1]; got.Line1 != 3 || got.Line2 != 4 {
		t.Errorf("sentence 1 spans L%d-%d, want L3-4", got.Line1, got.Line2)
	}
	if doc.Sentences[2].Section != "Limits" {
		t.Errorf("section = %q, want Limits", doc.Sentences[2].Section)
	}

	// "e.g." must not end a sentence, or every abbreviation fragments an item.
	if n := len(strings.Fields(doc.Sentences[2].Text)); n < 8 {
		t.Errorf("abbreviation split the sentence: %q", doc.Sentences[2].Text)
	}

	// Tables and code are excluded and COUNTED, never silently dropped.
	kinds := map[string]bool{}
	for _, e := range doc.Excluded {
		kinds[e.Kind] = true
	}
	if !kinds["table"] || !kinds["code"] {
		t.Errorf("excluded = %+v, want both a table and a code region", doc.Excluded)
	}
	for _, e := range doc.Excluded {
		for _, s := range doc.Sentences {
			if s.Line1 >= e.Line1 && s.Line1 <= e.Line2 {
				t.Errorf("sentence %q came from excluded %s at L%d-%d",
					s.Text, e.Kind, e.Line1, e.Line2)
			}
		}
	}
}

func TestSegmentSplitsListItems(t *testing.T) {
	doc := Segment(lines(`
- Rate-limit the export endpoint
- Cap attachment size at 25 MB
- Backfill archive_state
`))
	if len(doc.Sentences) != 3 {
		t.Fatalf("got %d sentences, want 3 — bullets glued together", len(doc.Sentences))
	}
	if doc.Sentences[1].Line1 != 2 {
		t.Errorf("second bullet at L%d, want L2", doc.Sentences[1].Line1)
	}
}

// --- layer 3 ---------------------------------------------------------------

func fakeDoc(n int) *Document {
	d := &Document{}
	for i := 0; i < n; i++ {
		d.Sentences = append(d.Sentences, Sentence{Idx: i, Text: "s", Line1: i + 1, Line2: i + 1})
	}
	return d
}

func TestGraphFoldsMovesIntoNodes(t *testing.T) {
	doc := fakeDoc(10)
	g := BuildGraph(doc, []Move{
		{Sentence: 0, Role: RoleIntroduce, Label: "splat widget"},
		{Sentence: 0, Role: RoleIntroduce, Label: "XYZ UI"}, // two ideas, one sentence
		{Sentence: 1, Role: RoleElaborate, Label: "splat widget", Target: ptr(0)},
		{Sentence: 2, Role: RoleMeta, Label: "section intro"},
		{Sentence: 3, Role: RoleIntroduce, Label: "single sign-on"},
		{Sentence: 4, Role: RoleRetract, Label: "single sign-on", Target: ptr(3)},
		{Sentence: 5, Role: RoleElaborate, Label: "ghost", Target: ptr(9)}, // never introduced
	})

	if len(g.Nodes) != 3 {
		t.Fatalf("got %d nodes, want 3", len(g.Nodes))
	}
	// One sentence introducing two things must produce two nodes, both citing
	// that sentence. Citing the same span twice is correct, not a duplicate.
	if g.Nodes[0].Origin != 0 || g.Nodes[1].Origin != 0 {
		t.Errorf("first two nodes should both originate at sentence 0")
	}

	// The elaboration must land on the splat widget, not on XYZ UI.
	if len(g.Nodes[0].Sources) != 2 {
		t.Errorf("splat widget sources = %v, want the elaboration attached", g.Nodes[0].Sources)
	}
	if len(g.Nodes[1].Sources) != 1 {
		t.Errorf("XYZ UI picked up an elaboration it should not have: %v", g.Nodes[1].Sources)
	}
	if g.Ambiguous != 1 {
		t.Errorf("ambiguous = %d, want 1 — the target hit a two-node sentence", g.Ambiguous)
	}

	// Retraction is the case the one-pass extractor could not express at all.
	if g.Nodes[2].State != "retracted" {
		t.Errorf("SSO node state = %q, want retracted", g.Nodes[2].State)
	}
	if g.Orphans != 1 {
		t.Errorf("orphans = %d, want 1", g.Orphans)
	}
	if g.MetaMoves != 1 {
		t.Errorf("meta = %d, want 1", g.MetaMoves)
	}
}

// 58% of the elaborations a real document produced arrived with no target: the
// model knew a sentence was clarifying something and could not say what.
// Dropping those discards most of the signal; attaching them silently would
// claim a link the model never made.
func TestTargetlessElaborationAttachesToNearestOpenIdea(t *testing.T) {
	doc := fakeDoc(10)
	g := BuildGraph(doc, []Move{
		{Sentence: 0, Role: RoleIntroduce, Label: "edge-first"},
		{Sentence: 1, Role: RoleElaborate, Label: "network touchpoints"}, // no target
		{Sentence: 4, Role: RoleIntroduce, Label: "everything-as-data"},
		{Sentence: 5, Role: RoleElaborate, Label: "no vertical vocabulary"}, // no target
	})

	if len(g.Nodes) != 2 {
		t.Fatalf("got %d nodes, want 2 — an untargeted elaboration must not open an idea", len(g.Nodes))
	}
	if len(g.Nodes[0].Sources) != 2 || g.Nodes[0].Sources[1] != 1 {
		t.Errorf("sentence 1 did not attach to the idea before it: %v", g.Nodes[0].Sources)
	}
	if len(g.Nodes[1].Sources) != 2 || g.Nodes[1].Sources[1] != 5 {
		t.Errorf("sentence 5 attached to the wrong idea: %v", g.Nodes[1].Sources)
	}
	if g.InferredLinks != 2 {
		t.Errorf("inferred links = %d, want 2", g.InferredLinks)
	}
	if !g.Nodes[0].Inferred || !g.Nodes[1].Inferred {
		t.Error("nodes do not record that their link was inferred, so the UI cannot say so")
	}
	if g.Orphans != 0 {
		t.Errorf("orphans = %d, want 0", g.Orphans)
	}

	// With nothing open before it, there is nothing honest to attach to.
	lone := BuildGraph(fakeDoc(5), []Move{{Sentence: 0, Role: RoleElaborate, Label: "floating"}})
	if len(lone.Nodes) != 0 || lone.Orphans != 1 {
		t.Errorf("a leading elaboration should orphan, got %d nodes / %d orphans", len(lone.Nodes), lone.Orphans)
	}
}

// A retracted idea must not absorb later detail: prose that elaborates after a
// deferral is almost always about whatever came next.
func TestInferredAttachmentSkipsRetractedIdeas(t *testing.T) {
	doc := fakeDoc(10)
	g := BuildGraph(doc, []Move{
		{Sentence: 0, Role: RoleIntroduce, Label: "export"},
		{Sentence: 1, Role: RoleIntroduce, Label: "single sign-on"},
		{Sentence: 2, Role: RoleRetract, Label: "single sign-on", Target: ptr(1)},
		{Sentence: 3, Role: RoleElaborate, Label: "archive contents"}, // no target
	})
	if g.Nodes[1].State != StateRetracted {
		t.Fatalf("SSO state = %q, want retracted", g.Nodes[1].State)
	}
	if len(g.Nodes[1].Sources) != 2 {
		t.Errorf("the retracted idea absorbed a later elaboration: %v", g.Nodes[1].Sources)
	}
	if len(g.Nodes[0].Sources) != 2 || g.Nodes[0].Sources[1] != 3 {
		t.Errorf("elaboration did not fall back to the open idea: %v", g.Nodes[0].Sources)
	}
}

func TestCitationsComeFromLayerOne(t *testing.T) {
	doc := fakeDoc(10)
	doc.Sentences[0].Line1, doc.Sentences[0].Line2 = 10, 12
	doc.Sentences[1].Line1, doc.Sentences[1].Line2 = 13, 13
	doc.Sentences[7].Line1, doc.Sentences[7].Line2 = 40, 40

	g := BuildGraph(doc, []Move{
		{Sentence: 0, Role: RoleIntroduce, Label: "a"},
		{Sentence: 1, Role: RoleElaborate, Label: "a"},
		{Sentence: 7, Role: RoleElaborate, Label: "a"},
	})
	got := FormatLines(g.Nodes[0].Lines)
	// 10-12 and 13 are adjacent and merge; 40 stays separate.
	if got != "10-13,40" {
		t.Fatalf("citation = %q, want \"10-13,40\"", got)
	}
}

// A retraction whose target the model could not name must NOT be guessed.
// Found on a three-sentence document: "Single sign-on ... is deferred" arrived
// with no target, was attached to the nearest open idea — an unrelated export
// requirement — and silently retracted it. The run reported zero proposals and
// no error, which is the worst possible way to be wrong.
func TestUnplacedRetractionNeverGuesses(t *testing.T) {
	doc := fakeDoc(6)
	g := BuildGraph(doc, []Move{
		{Sentence: 0, Role: RoleIntroduce, Label: "export a widget as a zip"},
		{Sentence: 1, Role: RoleElaborate, Label: "must include attachments"}, // no target: guessed, fine
		{Sentence: 2, Role: RoleRetract, Label: "single sign-on"},             // no target: must NOT be guessed
	})

	if len(g.Nodes) != 1 {
		t.Fatalf("got %d nodes, want 1", len(g.Nodes))
	}
	if g.Nodes[0].State != StateAsserted {
		t.Fatal("an unrelated requirement was retracted by a guess — the run would report zero proposals and no error")
	}
	if g.RetractsDropped != 1 {
		t.Errorf("unplaced retracts = %d, want 1 — it must be reported, not merely dropped", g.RetractsDropped)
	}
	// The elaboration guess is still allowed: its worst case is cosmetic.
	if len(g.Nodes[0].Sources) != 2 || !g.Nodes[0].Inferred {
		t.Errorf("the elaboration guess was lost too: %+v", g.Nodes[0])
	}
}

// A retraction aimed at something it never mentions must be refused. Layer 2
// only proves the target introduced SOMETHING, which pressures a model into
// naming the nearest available idea when the thing being deferred was never
// introduced at all.
func TestMisdirectedRetractionIsRefused(t *testing.T) {
	doc := fakeDoc(6)
	g := BuildGraph(doc, []Move{
		{Sentence: 0, Role: RoleIntroduce, Label: "export a widget as a zip"},
		{Sentence: 2, Role: RoleRetract, Label: "single sign-on", Target: ptr(0)},
	})
	if g.Nodes[0].State != StateAsserted {
		t.Fatal("an unrelated requirement was withdrawn by a retraction that never mentions it")
	}
	if g.RetractsDropped != 1 {
		t.Errorf("dropped = %d, want 1", g.RetractsDropped)
	}

	// A genuine retraction names its subject and must still work.
	ok := BuildGraph(fakeDoc(6), []Move{
		{Sentence: 0, Role: RoleIntroduce, Label: "single sign-on via Okta"},
		{Sentence: 2, Role: RoleRetract, Label: "single sign-on", Target: ptr(0)},
	})
	if ok.Nodes[0].State != StateRetracted {
		t.Error("a legitimate retraction was refused")
	}
	if ok.RetractsDropped != 0 {
		t.Errorf("dropped = %d on a legitimate retraction", ok.RetractsDropped)
	}
}
