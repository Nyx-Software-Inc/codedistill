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

// Package decompose turns one document into many proposed work items.
//
// The existing classifier is one capture, one item. This is one document, many
// items, which is decomposition rather than classification and needs a
// different shape.
//
// It runs in four layers, and the split is the whole design:
//
//	1  document -> sentences      code    exact line spans, no model
//	2  sentences -> moves         model   what each sentence DOES
//	3  moves -> nodes             code    a deterministic fold into a graph
//	4  nodes -> proposals         model   kind and subject only
//
// The first attempt asked a model to go from document straight to work items
// and to quote the text justifying each one. Measured against a real document,
// 30% of those citations landed on the text they claimed. The failures were not
// dishonesty — the model restyled quotes and miscounted line numbers — but the
// distinction cost a day to establish and would have cost a user their trust.
//
// So the model here never emits document text. It emits INDICES into sentences
// that layer 1 already cut, and short labels. Citations are computed before the
// model runs, which means a fabricated one cannot be expressed rather than
// having to be detected. Labels are generated, but a bad label is a clumsy
// subject beside a correct citation — cosmetic, not evidentiary.
package decompose

import (
	"context"
	"time"
)

// A model call's deadline has to scale with the work it was asked for.
//
// A flat 4-minute cap looked reasonable against 30-sentence windows and then
// killed the first whole-document read at exactly four minutes. The mistake was
// assuming latency is about the document: it is about the OUTPUT. A pass asked
// to classify 150 sentences must generate 150 entries, and observed throughput
// is roughly six seconds per entry, so that call needs a quarter of an hour
// whatever the prompt size.
//
// Generous on purpose. Too short kills legitimate work, which is what just
// happened; too long only delays noticing a wedged model, which the progress
// line now makes visible anyway.
// A call costs two things, and the first version of this counted only one.
//
//	PREFILL     reading the prompt. Measured at ~76ms/token cold on this
//	            hardware (6,660 tokens in 8m27s), roughly a quarter of that when
//	            the prefix is reused. Leaving it out is why a pass with a
//	            "7-minute" budget died against an 11m22s reality: the deadline
//	            counted the 30 entries it would write and not the 7,000 tokens
//	            it had to read first.
//	GENERATION  writing the answer, ~6s/entry observed.
//
// Both terms carry 2x headroom. Too short kills legitimate work, which has
// happened four times now; too long only delays noticing a wedged model, and
// the health check catches that case directly.
const (
	callBaseTimeout  = 60 * time.Second
	callPerPromptTok = 160 * time.Millisecond
	callPerUnit      = 12 * time.Second
	callMaxTimeout   = 45 * time.Minute
)

// ContextTokens is how much prompt the model must be allowed to see.
//
// Ollama defaults to 4096 whatever the model supports and truncates anything
// longer WITHOUT erroring — a 7,116-token prompt came back reporting
// prompt_eval_count 4096 and invented role names, because the instructions at
// the top had been cut away. Since every reading pass carries the whole
// document, the window has to be sized for the document, not the slice.
//
// 16384 fits qwen2.5:7b (32768) with room for the answer, and covers documents
// several times the size of a typical spec. Larger costs memory on the serving
// side for prompts that do not need it.
const ContextTokens = 16384

// MaxCallTimeout is the longest this package will wait for one call. Exported
// so a caller can size its http client above it: a transport timeout below this
// silently preempts the deadline computed here, which is how a pass with a
// seven-minute budget died at five.
const MaxCallTimeout = callMaxTimeout

// CallTimeoutFor bounds one model call: promptTokens to read, n entries to
// write.
func CallTimeoutFor(promptTokens, n int) time.Duration {
	d := callBaseTimeout +
		time.Duration(promptTokens)*callPerPromptTok +
		time.Duration(n)*callPerUnit
	if d > callMaxTimeout {
		return callMaxTimeout
	}
	return d
}

// Generator is the narrow slice of the model client this package needs.
// Satisfied by *ollama.Client.
type Generator interface {
	GenerateJSON(ctx context.Context, prompt string) (string, error)
	Model() string
}

// Sentence is the atomic unit of PROVENANCE — not of meaning. One sentence can
// carry several ideas ("we will build the splat widget via the new XYZ UI"),
// which is layer 2's problem, not layer 1's.
type Sentence struct {
	Idx     int    `json:"idx"`
	Text    string `json:"text"`
	Line1   int    `json:"line1"`
	Line2   int    `json:"line2"`
	Section string `json:"section"` // nearest preceding heading

	// Para groups sentences that were written together — one paragraph, one
	// bullet. A bulleted lead-in followed by two sentences IS "introduce,
	// elaborate, elaborate" written in markup, and the spike stripped that out
	// and then asked a 7B model to re-infer it from bare text. On the
	// LLM-written document only 11% of moves came back as elaborations; three
	// sentences of one tenet were each called a new idea.
	Para int `json:"para"`
}

// Excluded is a region layer 1 would not segment. Recorded rather than dropped
// so a run can say what it did not look at — a table row has no contiguous
// sentence, and the first spike produced a citation pointing at nothing by
// pretending otherwise.
type Excluded struct {
	Kind  string `json:"kind"` // "code" | "table"
	Line1 int    `json:"line1"`
	Line2 int    `json:"line2"`
}

// Document is layer 1's output.
type Document struct {
	Sentences []Sentence `json:"sentences"`
	Excluded  []Excluded `json:"excluded"`
	Headings  int        `json:"headings"`
	Lines     int        `json:"lines"`
}

// Move roles.
const (
	RoleIntroduce = "introduce" // states something new about the system
	RoleElaborate = "elaborate" // refines something introduced earlier
	RoleRetract   = "retract"   // removes or defers something introduced earlier
	RoleMeta      = "meta"      // navigational text saying nothing about the system
)

// Move is one thing a sentence does. A sentence may do several: "we keep export
// but drop SSO" both elaborates and retracts, and one role per sentence cannot
// say so.
type Move struct {
	Sentence int    `json:"sentence"`
	Role     string `json:"role"`
	Label    string `json:"label"`
	Target   *int   `json:"target,omitempty"` // sentence that introduced the refined idea

	// Inferred records that the model flagged a refinement without naming what
	// it refines, and layer 3 attached it to the nearest open idea. On a real
	// human-written document 58% of detected elaborations arrived this way, so
	// discarding them throws away most of the signal — but the graph must not
	// claim a link the model never made.
	Inferred bool `json:"inferred,omitempty"`
}

// Node is one delta to the system the document describes.
type Node struct {
	ID       string   `json:"id"`
	Intent   string   `json:"intent"`
	State    string   `json:"state"` // "asserted" | "retracted"
	Origin   int      `json:"origin"`
	Sources  []int    `json:"sources"`
	Lines    [][2]int `json:"lines"` // citation, from layer 1
	Inferred bool     `json:"inferred,omitempty"`
}

const (
	StateAsserted  = "asserted"
	StateRetracted = "retracted"
)

// Graph is layer 3's output, with the seam health it observed.
type Graph struct {
	Nodes []Node `json:"nodes"`

	// Orphans counts refinements whose idea did not survive; Ambiguous counts
	// targets that hit a sentence introducing more than one thing. Reported
	// rather than swallowed: the risk in a layered pipeline lives at the seams.
	Orphans       int `json:"orphans"`
	Ambiguous     int `json:"ambiguous"`
	MetaMoves     int `json:"meta_moves"`
	InferredLinks int `json:"inferred_links"`

	// RetractsDropped counts retractions that were refused: either the model
	// named no target, or it named one sharing not a single word with what it
	// claimed to withdraw. Both are dropped rather than guessed, because a
	// misplaced retraction deletes a real requirement and nobody notices, while
	// a retraction left unapplied leaves an idea proposed that a human can see
	// and reject. Visible and wrong beats invisible and gone.
	RetractsDropped int `json:"retracts_dropped"`
}

// Proposal is a candidate work item. Not an item: nothing exists until a human
// accepts it.
type Proposal struct {
	NodeID  string   `json:"node_id"`
	Kind    string   `json:"kind"` // todo | bug | kb | use_case
	Subject string   `json:"subject"`
	Lines   [][2]int `json:"lines"`
	Sources []int    `json:"sources"`

	// FromTable marks a proposal READ from a record table rather than inferred
	// from prose. It is the stronger evidence of the two — the author wrote the
	// requirement down as a requirement — and the review surface should rank it
	// accordingly.
	FromTable   bool   `json:"from_table,omitempty"`
	ExternalRef string `json:"external_ref,omitempty"` // the author's own id, e.g. UC-07
	Priority    string `json:"priority,omitempty"`     // as written, not normalised
	Body        string `json:"body,omitempty"`

	// Corroborated records that the prose pipeline independently found this
	// same requirement. Two independent readings agreeing is the strongest
	// signal available, and it is what lets a reviewer accept in bulk.
	Corroborated bool `json:"corroborated,omitempty"`
}

// Reconciliation is what a document says about itself when read twice — once as
// prose, once as records. The buckets are the honest answer to "what did it
// miss", which coverage alone cannot give.
type Reconciliation struct {
	Corroborated []Proposal `json:"corroborated"` // in the table AND found in prose
	TableOnly    []Proposal `json:"table_only"`   // written down, prose extraction missed it
	ProseOnly    []Proposal `json:"prose_only"`   // found in prose, not in the table
}
