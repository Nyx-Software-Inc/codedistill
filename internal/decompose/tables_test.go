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
	"strings"
	"testing"
)

const ucTable = `
# Use cases

| Use Case # | Use Case Statement | Description | Priority |
| ---------- | ------------------ | ----------- | -------- |
| UC-01 | As a user, I want notes pre-populated at the start of the day. | Notes are created from the calendar. | P0 |
| UC-07 | As a user, I want the option to record the meetings I am in. | Recording runs while you type. | P0 |
| UC-18 | As a user, I want to search across all of my notes. | Full text search. | P1 |
`

func TestRecordTableIsReadNotInferred(t *testing.T) {
	tables := ExtractTables(strings.Split(strings.TrimPrefix(ucTable, "\n"), "\n"))
	if len(tables) != 1 {
		t.Fatalf("got %d tables, want 1", len(tables))
	}
	f, ok := tables[0].Fields()
	if !ok {
		t.Fatal("a use case table was not recognised as records")
	}
	if f.Subject != 1 || f.Body != 2 || f.Priority != 3 || f.Ref != 0 {
		t.Fatalf("column mapping = %+v", f)
	}

	props := tables[0].RecordProposals(KindForHeader(tables[0].Header[f.Subject]))
	if len(props) != 3 {
		t.Fatalf("got %d proposals, want 3", len(props))
	}
	p := props[1]
	if p.Kind != "use_case" {
		t.Errorf("kind = %q, want use_case", p.Kind)
	}
	if p.ExternalRef != "UC-07" || p.Priority != "P0" {
		t.Errorf("row fields lost: ref=%q priority=%q", p.ExternalRef, p.Priority)
	}
	if !p.FromTable {
		t.Error("proposal not marked as read from a table — the review surface cannot rank it")
	}
	// Provenance is the ROW, not the whole table: a citation pointing at 124
	// lines is not a citation.
	if len(p.Lines) != 1 || p.Lines[0][0] != p.Lines[0][1] {
		t.Errorf("citation = %v, want a single row line", p.Lines)
	}
	if p.Lines[0][0] != 6 {
		t.Errorf("cited line %d, want 6 (the UC-07 row)", p.Lines[0][0])
	}
}

// Turning a layout or comparison table into thirty work items is worse than
// ignoring it, so detection has to decline anything without a subject column.
func TestNonRecordTablesAreDeclined(t *testing.T) {
	for name, src := range map[string]string{
		"comparison matrix": "| | Community | Pro |\n| --- | --- | --- |\n| Canvas | yes | yes |\n",
		"layout table":      "| left | right |\n| --- | --- |\n| a | b |\n",
	} {
		t.Run(name, func(t *testing.T) {
			tables := ExtractTables(strings.Split(src, "\n"))
			if len(tables) != 1 {
				t.Fatalf("got %d tables", len(tables))
			}
			if _, ok := tables[0].Fields(); ok {
				t.Fatalf("a %s was treated as requirement records", name)
			}
			if got := tables[0].RecordProposals("todo"); got != nil {
				t.Fatalf("produced %d proposals from a %s", len(got), name)
			}
		})
	}
}

// The whole point of reading a document twice: where the two accounts disagree
// is the answer to "what did it miss", which coverage alone cannot give.
func TestReconcileSortsIntoThreeHonestBuckets(t *testing.T) {
	fromTable := []Proposal{
		{Subject: "As a user, I want to search across all of my notes.", Kind: "use_case", FromTable: true},
		{Subject: "As a user, I want the option to record the meetings I am in.", Kind: "use_case", FromTable: true},
		{Subject: "As a user, I want notes pre-populated at the start of the day.", Kind: "use_case", FromTable: true},
	}
	fromProse := []Proposal{
		{Subject: "Search across all notes", Kind: "use_case"},
		{Subject: "Enable meeting recording", Kind: "use_case"},
		{Subject: "Archive recordings and meetings", Kind: "use_case"},
	}

	r := Reconcile(fromTable, fromProse, 0.5)

	if len(r.Corroborated) != 2 {
		t.Fatalf("corroborated %d, want 2: %+v", len(r.Corroborated), subjects(r.Corroborated))
	}
	for _, p := range r.Corroborated {
		if !p.Corroborated {
			t.Errorf("%q not flagged corroborated", p.Subject)
		}
	}
	// The pre-population row has no prose match — that is a real miss, and
	// surfacing it is the feature.
	if len(r.TableOnly) != 1 || !strings.Contains(r.TableOnly[0].Subject, "pre-populated") {
		t.Fatalf("table-only = %v, want the pre-population row", subjects(r.TableOnly))
	}
	if len(r.ProseOnly) != 1 || r.ProseOnly[0].Subject != "Archive recordings and meetings" {
		t.Fatalf("prose-only = %v", subjects(r.ProseOnly))
	}
}

// One prose proposal must not corroborate two different table rows.
func TestReconcileDoesNotDoubleClaim(t *testing.T) {
	fromTable := []Proposal{
		{Subject: "As a user, I want to delete a meeting note."},
		{Subject: "As a user, I want to archive a meeting note."},
	}
	fromProse := []Proposal{{Subject: "Delete meeting notes"}}

	r := Reconcile(fromTable, fromProse, 0.5)
	if len(r.Corroborated) != 1 {
		t.Fatalf("corroborated %d, want exactly 1: %v", len(r.Corroborated), subjects(r.Corroborated))
	}
	if len(r.TableOnly) != 1 {
		t.Fatalf("table-only %d, want 1: %v", len(r.TableOnly), subjects(r.TableOnly))
	}
}

// The failure that killed the embedding matcher: on a document where every
// sentence is about notes and meetings, unrelated requirements must not pair.
func TestReconcileRejectsSameDomainNoise(t *testing.T) {
	fromTable := []Proposal{{Subject: "As a user, I want notes pre-populated at the start of the day."}}
	fromProse := []Proposal{{Subject: "Delete meeting notes"}}

	r := Reconcile(fromTable, fromProse, 0.5)
	if len(r.Corroborated) != 0 {
		t.Fatalf("paired two unrelated note requirements: %v", subjects(r.Corroborated))
	}
}

func subjects(ps []Proposal) []string {
	var out []string
	for _, p := range ps {
		out = append(out, p.Subject)
	}
	return out
}
