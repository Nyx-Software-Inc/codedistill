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
	"strings"
	"testing"
)

type fakeGen struct {
	reply  string
	prompt string
}

func (f *fakeGen) GenerateJSON(_ context.Context, prompt string) (string, error) {
	f.prompt = prompt
	return f.reply, nil
}
func (f *fakeGen) Model() string { return "fake" }

func threeItems() []SeqItem {
	return []SeqItem{
		{ID: "i1", Kind: "use_case", Subject: "Record audio", Ref: "UC-1"},
		{ID: "i2", Kind: "use_case", Subject: "Rename a recording", Ref: "UC-2"},
		{ID: "i3", Kind: "use_case", Subject: "Export a transcript", Ref: "UC-3"},
	}
}

// An edge naming something that was never offered would point at nothing. The
// identifiers are supplied in the prompt, so this is checkable rather than a
// matter of trust — the same construction as citations.
func TestSequenceDropsEdgesNamingUnknownItems(t *testing.T) {
	g := &fakeGen{reply: `{"edges":[
	  {"from":"UC-2","to":"UC-1","kind":"blocks","why":"nothing to rename until something is recorded"},
	  {"from":"UC-9","to":"UC-1","kind":"blocks","why":"invented"},
	  {"from":"UC-3","to":"UC-42","kind":"informs","why":"also invented"}
	]}`}
	edges, rejected, err := Sequence(context.Background(), g, threeItems())
	if err != nil {
		t.Fatal(err)
	}
	if len(edges) != 1 {
		t.Fatalf("kept %d edges, want 1: %+v", len(edges), edges)
	}
	if rejected != 2 {
		t.Fatalf("rejected %d, want 2 — a model naming items that do not exist must be countable", rejected)
	}
	if edges[0].FromRef != "UC-2" || edges[0].ToRef != "UC-1" {
		t.Fatalf("wrong edge survived: %+v", edges[0])
	}
}

// Self-edges make a graph unorderable; two-item cycles do the same thing more
// quietly. Both are cheaper to catch here than in whatever tries to render them.
func TestSequenceRefusesSelfEdgesAndCycles(t *testing.T) {
	g := &fakeGen{reply: `{"edges":[
	  {"from":"UC-1","to":"UC-1","kind":"blocks","why":"itself"},
	  {"from":"UC-2","to":"UC-3","kind":"blocks","why":"a"},
	  {"from":"UC-3","to":"UC-2","kind":"blocks","why":"the other way round"},
	  {"from":"UC-2","to":"UC-3","kind":"blocks","why":"said twice"}
	]}`}
	edges, rejected, err := Sequence(context.Background(), g, threeItems())
	if err != nil {
		t.Fatal(err)
	}
	if len(edges) != 1 {
		t.Fatalf("kept %d edges, want 1 (self, reverse and duplicate all dropped): %+v", len(edges), edges)
	}
	if rejected != 3 {
		t.Fatalf("rejected %d, want 3", rejected)
	}
}

// "informs" exists so a model has somewhere to put a weak relationship. If it
// were silently promoted to "blocks", the escape hatch would be pointless and
// everything would block everything.
func TestSequenceKeepsTheWeakerRelationship(t *testing.T) {
	g := &fakeGen{reply: `{"edges":[
	  {"from":"UC-3","to":"UC-2","kind":"informs","why":"easier once renaming exists"},
	  {"from":"UC-2","to":"UC-1","kind":"nonsense","why":"unrecognised kind"}
	]}`}
	edges, _, err := Sequence(context.Background(), g, threeItems())
	if err != nil {
		t.Fatal(err)
	}
	if len(edges) != 2 {
		t.Fatalf("want 2 edges, got %d", len(edges))
	}
	if edges[0].Kind != "informs" {
		t.Fatalf("the weak relationship was promoted to %q", edges[0].Kind)
	}
	if edges[1].Kind != "blocks" {
		t.Fatalf("an unrecognised kind should fall back to blocks, got %q", edges[1].Kind)
	}
}

// Fewer than two proposals cannot have an ordering, and must not cost a call.
func TestSequenceDoesNotCallTheModelWithNothingToOrder(t *testing.T) {
	g := &fakeGen{reply: `{"edges":[]}`}
	edges, _, err := Sequence(context.Background(), g, []SeqItem{{ID: "i1", Subject: "only one"}})
	if err != nil || len(edges) != 0 {
		t.Fatalf("edges=%v err=%v", edges, err)
	}
	if g.prompt != "" {
		t.Fatal("the model was called to order a single item")
	}
}

// The instructions must precede the varying block, so the fixed prefix stays
// reusable — worth 8m27s to 2m10s on a local model when this was measured for
// the reading prompt.
func TestSequencePromptPutsTheItemsLast(t *testing.T) {
	g := &fakeGen{reply: `{"edges":[]}`}
	if _, _, err := Sequence(context.Background(), g, threeItems()); err != nil {
		t.Fatal(err)
	}
	rules := strings.Index(g.prompt, "MOST PAIRS HAVE NO RELATIONSHIP")
	items := strings.Index(g.prompt, "UC-1 [use_case]")
	if rules < 0 || items < 0 {
		t.Fatalf("prompt missing its parts:\n%s", g.prompt)
	}
	if rules > items {
		t.Fatal("the varying item list precedes the fixed instructions, which defeats prefix reuse")
	}
}
