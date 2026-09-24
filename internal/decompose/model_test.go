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
// Layers 2 and 4 are model-driven, so these tests drive a fake model. What is
// being tested is not whether a 7B classifies well — that is measured, not
// asserted — but whether the code around it holds when the model misbehaves,
// which is the part that must never lose a document.
package decompose

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

type fakeModel struct {
	replies  []string
	calls    int
	err      error
	errAfter int // fail once this many calls have succeeded; 0 = never
}

func (f *fakeModel) Model() string { return "fake" }
func (f *fakeModel) GenerateJSON(ctx context.Context, prompt string) (string, error) {
	if f.err != nil {
		return "", f.err
	}
	i := f.calls
	f.calls++
	if f.errAfter > 0 && i >= f.errAfter {
		return "", errors.New("model died mid-run")
	}
	if i < len(f.replies) {
		return f.replies[i], nil
	}
	return `{"moves":[]}`, nil
}

func docOf(texts ...string) *Document {
	d := &Document{Lines: len(texts)}
	for i, tx := range texts {
		d.Sentences = append(d.Sentences, Sentence{Idx: i, Text: tx, Line1: i + 1, Line2: i + 1, Para: i/2 + 1})
	}
	return d
}

func TestCheckMoveRejectsUnsupportableClaims(t *testing.T) {
	introduced := map[int]bool{1: true}
	for _, tc := range []struct {
		name string
		m    Move
		bad  bool
	}{
		{"introduce", Move{Sentence: 2, Role: RoleIntroduce}, false},
		{"elaborate with a real target", Move{Sentence: 3, Role: RoleElaborate, Target: ptr(1)}, false},
		{"elaborate with no target is legal", Move{Sentence: 3, Role: RoleElaborate}, false},
		{"sentence out of range", Move{Sentence: 99, Role: RoleIntroduce}, true},
		{"negative sentence", Move{Sentence: -1, Role: RoleIntroduce}, true},
		{"unknown role", Move{Sentence: 2, Role: "ponder"}, true},
		{"target out of range", Move{Sentence: 3, Role: RoleElaborate, Target: ptr(99)}, true},
		{"target is later", Move{Sentence: 1, Role: RoleElaborate, Target: ptr(3)}, true},
		{"target introduced nothing", Move{Sentence: 4, Role: RoleElaborate, Target: ptr(2)}, true},
		{"introduce carrying a target", Move{Sentence: 2, Role: RoleIntroduce, Target: ptr(1)}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			why := CheckMove(tc.m, 5, introduced)
			if tc.bad != (why != "") {
				t.Fatalf("CheckMove = %q, bad=%v", why, tc.bad)
			}
		})
	}
}

// A model listing an elaboration before the introduce it points at is an
// ordering artefact, not a fault in the document. The spike rejected those.
func TestMovesRegisterIntroducesBeforeValidatingTargets(t *testing.T) {
	doc := docOf("Export exists.", "It must include attachments.")
	m := &fakeModel{replies: []string{
		`{"moves":[
			{"sentence":1,"role":"elaborate","label":"export","target":0},
			{"sentence":0,"role":"introduce","label":"export"}
		]}`,
	}}
	kept, rejected, err := DeriveMoves(context.Background(), m, doc, 30, nil)
	if err != nil {
		t.Fatalf("DeriveMoves: %v", err)
	}
	if len(rejected) != 0 {
		t.Fatalf("rejected %+v — out-of-order listing is not a fault", rejected)
	}
	if len(kept) != 2 {
		t.Fatalf("kept %d moves, want 2", len(kept))
	}
}

// One bad window must not lose the document. A 40-page spec that fails
// entirely because window 19 returned broken JSON is worse than one reporting
// a gap.
func TestOneUnparseableWindowDoesNotLoseTheRest(t *testing.T) {
	doc := docOf("A.", "B.", "C.", "D.")
	m := &fakeModel{replies: []string{
		`not json at all`,
		`{"moves":[{"sentence":2,"role":"introduce","label":"C"}]}`,
	}}
	kept, rejected, err := DeriveMoves(context.Background(), m, doc, 2, nil)
	if err != nil {
		t.Fatalf("DeriveMoves: %v", err)
	}
	if len(kept) != 1 || kept[0].Sentence != 2 {
		t.Fatalf("second window lost: %+v", kept)
	}
	if len(rejected) != 1 || !strings.Contains(rejected[0].Reason, "unparseable") {
		t.Fatalf("the gap was not reported: %+v", rejected)
	}
}

func TestMovesReportProgressWithAKnownTotal(t *testing.T) {
	doc := docOf("A.", "B.", "C.", "D.", "E.")
	var seen [][2]int
	_, _, err := DeriveMoves(context.Background(), &fakeModel{}, doc, 2, func(d, tot int) {
		seen = append(seen, [2]int{d, tot})
	})
	if err != nil {
		t.Fatalf("DeriveMoves: %v", err)
	}
	// The denominator is known before the first model call — layer 1 is
	// instant — so the reading bar is determinate from the start.
	want := [][2]int{{0, 3}, {1, 3}, {2, 3}, {3, 3}}
	if len(seen) != len(want) {
		t.Fatalf("progress = %v, want %v", seen, want)
	}
	for i := range want {
		if seen[i] != want[i] {
			t.Fatalf("progress = %v, want %v", seen, want)
		}
	}
}

func TestMovesPropagateAModelFailure(t *testing.T) {
	doc := docOf("A.")
	_, _, err := DeriveMoves(context.Background(), &fakeModel{err: errors.New("ollama unreachable")}, doc, 30, nil)
	if err == nil {
		t.Fatal("a dead model was reported as a successful empty read")
	}
}

// Not every idea is work. Without a way to decline, a measured run turned the
// document's own title into a knowledge entry and a table header into a todo.
func TestProjectionCanDeclineToCreateWork(t *testing.T) {
	doc := docOf("Stenographer", "Users can search all notes.")
	graph := BuildGraph(doc, []Move{
		{Sentence: 0, Role: RoleIntroduce, Label: "Product Description"},
		{Sentence: 1, Role: RoleIntroduce, Label: "search across notes"},
	})
	m := &fakeModel{replies: []string{
		`{"items":[
			{"node":"n1","kind":"none","reason":"the document's own title"},
			{"node":"n2","kind":"use_case","subject":"Search across all notes"}
		]}`,
	}}
	props, skipped, _, err := Project(context.Background(), m, doc, graph, 12, nil)
	if err != nil {
		t.Fatalf("Project: %v", err)
	}
	if len(props) != 1 || props[0].Kind != "use_case" {
		t.Fatalf("proposals = %+v, want one use case", props)
	}
	if len(skipped) != 1 || skipped[0].NodeID != "n1" {
		t.Fatalf("skipped = %+v, want n1 recorded", skipped)
	}
	// Declining is shown, not silent: the run can say what it chose not to make.
	if skipped[0].Reason == "" || len(skipped[0].Lines) == 0 {
		t.Errorf("a declined node lost its reason or its citation: %+v", skipped[0])
	}
}

func TestProjectionSkipsRetractedNodesAndUnknownIds(t *testing.T) {
	doc := docOf("SSO is required.", "SSO is deferred to next year.", "Export exists.")
	graph := BuildGraph(doc, []Move{
		{Sentence: 0, Role: RoleIntroduce, Label: "single sign-on"},
		{Sentence: 1, Role: RoleRetract, Label: "single sign-on", Target: ptr(0)},
		{Sentence: 2, Role: RoleIntroduce, Label: "export"},
	})
	m := &fakeModel{replies: []string{
		`{"items":[
			{"node":"n1","kind":"use_case","subject":"Single sign-on"},
			{"node":"n2","kind":"todo","subject":"Export"},
			{"node":"n99","kind":"bug","subject":"from nowhere"}
		]}`,
	}}
	props, _, _, err := Project(context.Background(), m, doc, graph, 12, nil)
	if err != nil {
		t.Fatalf("Project: %v", err)
	}
	// n1 is retracted so it is never offered to the model at all; n99 has no
	// node, so nothing could carry its citation.
	if len(props) != 1 || props[0].NodeID != "n2" {
		t.Fatalf("proposals = %+v, want only the export node", props)
	}
}

func TestProposalNeverShipsNameless(t *testing.T) {
	doc := docOf("Users can export a project.")
	graph := BuildGraph(doc, []Move{{Sentence: 0, Role: RoleIntroduce, Label: "project export"}})
	m := &fakeModel{replies: []string{`{"items":[{"node":"n1","kind":"use_case","subject":"  "}]}`}}
	props, _, _, err := Project(context.Background(), m, doc, graph, 12, nil)
	if err != nil {
		t.Fatalf("Project: %v", err)
	}
	if len(props) != 1 || props[0].Subject != "project export" {
		t.Fatalf("empty subject not backfilled from the node: %+v", props)
	}
}

// On a real 127-sentence document the model returned moves for only 65
// sentences. The prompt described what sentences do but never required an entry
// per sentence, so a partial list was a valid answer — and half the document
// was silently skipped. Detecting the gap is free and exact; trusting an
// instruction is neither.
func TestSkippedSentencesAreReAsked(t *testing.T) {
	doc := docOf("A.", "B.", "C.", "D.", "E.", "F.", "G.", "H.")
	m := &fakeModel{replies: []string{
		// First pass covers two of eight.
		`{"moves":[
			{"sentence":0,"role":"introduce","label":"a"},
			{"sentence":1,"role":"elaborate","label":"a","target":0}
		]}`,
		// The gap pass is asked about the other six.
		`{"moves":[
			{"sentence":2,"role":"meta","label":"heading"},
			{"sentence":3,"role":"introduce","label":"d"},
			{"sentence":4,"role":"meta","label":"heading"},
			{"sentence":5,"role":"introduce","label":"f"},
			{"sentence":6,"role":"meta","label":"heading"},
			{"sentence":7,"role":"meta","label":"heading"}
		]}`,
	}}

	kept, _, err := DeriveMoves(context.Background(), m, doc, 8, nil)
	if err != nil {
		t.Fatalf("DeriveMoves: %v", err)
	}
	if m.calls != 2 {
		t.Fatalf("made %d model calls, want 2 (one pass + one gap pass)", m.calls)
	}
	if got := len(uncovered(kept, 0, 8)); got != 0 {
		t.Fatalf("%d sentences still uncovered after the gap pass", got)
	}
}

// A gap pass may only answer about the gap. Re-answering a sentence the first
// pass already classified would double it.
func TestGapPassCannotReAnswerCoveredSentences(t *testing.T) {
	doc := docOf("A.", "B.", "C.", "D.")
	m := &fakeModel{replies: []string{
		`{"moves":[{"sentence":0,"role":"introduce","label":"a"}]}`,
		`{"moves":[
			{"sentence":0,"role":"introduce","label":"a again"},
			{"sentence":1,"role":"meta","label":"x"},
			{"sentence":2,"role":"meta","label":"y"},
			{"sentence":3,"role":"meta","label":"z"}
		]}`,
	}}
	kept, _, err := DeriveMoves(context.Background(), m, doc, 4, nil)
	if err != nil {
		t.Fatalf("DeriveMoves: %v", err)
	}
	n := 0
	for _, mv := range kept {
		if mv.Sentence == 0 {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("sentence 0 carries %d moves, want 1 — the gap pass duplicated it", n)
	}
}

// A small gap is not worth minutes: the stragglers are usually blank or
// near-blank lines and a retry buys nothing.
func TestSmallGapIsNotWorthARetry(t *testing.T) {
	doc := docOf("A.", "B.", "C.", "D.", "E.", "F.", "G.", "H.")
	var reply strings.Builder
	reply.WriteString(`{"moves":[`)
	for i := 0; i < 7; i++ { // 7 of 8 covered: a 12.5% gap
		if i > 0 {
			reply.WriteString(",")
		}
		fmt.Fprintf(&reply, `{"sentence":%d,"role":"meta","label":"x"}`, i)
	}
	reply.WriteString(`]}`)

	m := &fakeModel{replies: []string{reply.String()}}
	if _, _, err := DeriveMoves(context.Background(), m, doc, 8, nil); err != nil {
		t.Fatalf("DeriveMoves: %v", err)
	}
	if m.calls != 1 {
		t.Errorf("made %d calls, want 1 — a one-sentence gap does not justify a retry", m.calls)
	}
}

// A failed gap pass must never lose the moves the first pass produced.
func TestGapFailureKeepsTheFirstPass(t *testing.T) {
	doc := docOf("A.", "B.", "C.", "D.")
	m := &fakeModel{replies: []string{
		`{"moves":[{"sentence":0,"role":"introduce","label":"a"}]}`,
		`not json`,
	}}
	kept, _, err := DeriveMoves(context.Background(), m, doc, 4, nil)
	if err != nil {
		t.Fatalf("DeriveMoves: %v", err)
	}
	if len(kept) != 1 {
		t.Fatalf("kept %d moves, want the 1 from the first pass", len(kept))
	}
}

// A flat deadline killed the first whole-document read at exactly four minutes.
// Latency is not about the document, it is about the OUTPUT: a pass asked to
// classify 150 sentences must generate 150 entries.
func TestCallDeadlineScalesWithWorkAsked(t *testing.T) {
	small, large := CallTimeoutFor(7000, 30), CallTimeoutFor(7000, 150)
	if large <= small {
		t.Fatalf("150 entries got %v, 30 got %v — the deadline does not scale", large, small)
	}
	// The window size that used to work must still fit, with headroom.
	if small < 3*time.Minute {
		t.Errorf("a 30-entry pass gets %v; observed runs took ~3min", small)
	}
	// And the case that failed must now fit.
	if large < 15*time.Minute {
		t.Errorf("a 150-entry pass gets %v; at ~6s/entry it needs ~15min", large)
	}
	// A wedged model must still lose eventually.
	if CallTimeoutFor(100000, 100000) > time.Hour {
		t.Errorf("no upper bound: %v", CallTimeoutFor(100000, 100000))
	}
}

// Two limits were conflated: what the model may SEE and how much it can be
// asked to PRODUCE. Context is not binding (a real spec is ~6,900 tokens
// against a 32,768 window), but sustained structured output is — asked for all
// 150 entries at once, the model returned one. So every prompt carries the
// whole document and asks about a slice.
func TestReadingPromptCarriesWholeDocumentAndAsksForASlice(t *testing.T) {
	doc := docOf("A.", "B.", "C.", "D.", "E.", "F.")
	var prompts []string
	m := &recordingModel{onPrompt: func(p string) { prompts = append(prompts, p) }}

	if _, _, err := DeriveMoves(context.Background(), m, doc, 2, nil); err != nil {
		t.Fatalf("DeriveMoves: %v", err)
	}
	if len(prompts) == 0 {
		t.Fatal("no prompts captured")
	}

	first := prompts[0]
	// Every sentence is visible, including ones this pass is not asked about,
	// so a refinement can cite the real number of its target.
	for _, want := range []string{"A.", "B.", "C.", "D.", "E.", "F."} {
		if !strings.Contains(first, want) {
			t.Errorf("sentence %q missing from the context block", want)
		}
	}
	if !strings.Contains(first, "THE DOCUMENT, in full") || !strings.Contains(first, "CLASSIFY THESE") {
		t.Fatal("prompt does not separate what may be seen from what is asked about")
	}
	// The ask is narrow: the slice appears after the classify marker.
	ask := first[strings.Index(first, "CLASSIFY THESE SENTENCES"):]
	if strings.Contains(ask, "E.") || strings.Contains(ask, "F.") {
		t.Errorf("the first pass was asked about sentences outside its window:\n%s", ask)
	}
}

type recordingModel struct {
	onPrompt func(string)
	reply    string
	calls    int
}

func (r *recordingModel) Model() string { return "recording" }
func (r *recordingModel) GenerateJSON(ctx context.Context, prompt string) (string, error) {
	r.calls++
	if r.onPrompt != nil {
		r.onPrompt(prompt)
	}
	if r.reply != "" {
		return r.reply, nil
	}
	return `{"moves":[]}`, nil
}

// Prompt ORDER is load-bearing, not cosmetic: llama.cpp reuses the KV cache
// only up to the first byte that differs from the previous prompt. Measured,
// with the varying block early only 2,092 chars were shared and every pass
// re-prefilled at 8m27s; with it last, 22,703 chars were shared and the second
// pass took 2m10s. Anything that varies must stay AFTER the document.
func TestPromptPutsEverythingThatVariesLast(t *testing.T) {
	doc := docOf("A.", "B.", "C.", "D.", "E.", "F.")
	// Full coverage, so the gap-retry pass never fires. Without this the second
	// captured prompt is the GAP prompt, which legitimately shares nothing with
	// the reading prompt and compares as an instant divergence.
	// INTRODUCE moves, not meta: the "ideas already introduced" block only
	// lists introduces, so a fixture of metas leaves that block empty in every
	// pass and the ordering under test is never exercised at all.
	covered := `{"moves":[` +
		`{"sentence":0,"role":"introduce","label":"alpha"},{"sentence":1,"role":"introduce","label":"beta"},` +
		`{"sentence":2,"role":"introduce","label":"gamma"},{"sentence":3,"role":"introduce","label":"delta"},` +
		`{"sentence":4,"role":"introduce","label":"epsilon"},{"sentence":5,"role":"introduce","label":"zeta"}]}`
	var prompts []string
	m := &recordingModel{reply: covered, onPrompt: func(p string) { prompts = append(prompts, p) }}
	if _, _, err := DeriveMoves(context.Background(), m, doc, 2, nil); err != nil {
		t.Fatalf("DeriveMoves: %v", err)
	}
	if len(prompts) < 2 {
		t.Fatalf("need two reading passes to compare, got %d", len(prompts))
	}
	// Precondition: the block really does differ between passes, or this test
	// would pass for the wrong reason.
	if prompts[0] == prompts[1] {
		t.Fatal("the two prompts are identical; nothing varies, so ordering is untested")
	}

	// The document must sit inside the shared prefix of consecutive passes.
	shared := 0
	for shared < len(prompts[0]) && shared < len(prompts[1]) && prompts[0][shared] == prompts[1][shared] {
		shared++
	}
	docEnd := strings.Index(prompts[0], "END OF DOCUMENT")
	if docEnd < 0 {
		t.Fatal("no document block")
	}
	if shared < docEnd {
		t.Fatalf("consecutive prompts diverge at %d, before the document ends at %d — "+
			"something that varies was placed early and the prefill cannot be reused", shared, docEnd)
	}
	// And the instructions too: they are identical every pass.
	if ins := strings.Index(prompts[0], "COVERAGE IS REQUIRED"); ins >= 0 && shared < ins {
		t.Errorf("prompts diverge at %d, before the instructions at %d", shared, ins)
	}
}

// fakePassStore is an in-memory PassStore for testing resume.
type fakePassStore struct {
	done   map[int][]Move
	saved  []int
	failOn int // window start whose Save fails; -1 for none
}

func newFakePassStore() *fakePassStore {
	return &fakePassStore{done: map[int][]Move{}, failOn: -1}
}
func (f *fakePassStore) Completed(ctx context.Context) (map[int][]Move, error) {
	out := map[int][]Move{}
	for k, v := range f.done {
		out[k] = v
	}
	return out, nil
}
func (f *fakePassStore) Save(ctx context.Context, start, end int, moves []Move, rejected int) error {
	f.saved = append(f.saved, start)
	if start == f.failOn {
		return errors.New("disk full")
	}
	f.done[start] = moves
	return nil
}

// The failure this exists for: four of five reading passes completed, 24
// minutes of model time, and every one was discarded because nothing was
// written until the run finished.
func TestInterruptedRunResumesFromWhatWasRead(t *testing.T) {
	doc := docOf("A.", "B.", "C.", "D.", "E.", "F.")
	store := newFakePassStore()

	// First attempt: window 0 succeeds, window 2 kills the run.
	m := &fakeModel{replies: []string{
		`{"moves":[{"sentence":0,"role":"introduce","label":"alpha"},{"sentence":1,"role":"meta","label":"x"}]}`,
	}}
	m.errAfter = 1
	if _, _, err := DeriveMovesResumable(context.Background(), m, doc, 2, store, nil); err == nil {
		t.Fatal("expected the second window to fail")
	}
	if len(store.done) != 1 {
		t.Fatalf("first window not checkpointed: %+v", store.done)
	}

	// Second attempt: the completed window must not be re-read.
	m2 := &fakeModel{replies: []string{
		`{"moves":[{"sentence":2,"role":"introduce","label":"beta"},{"sentence":3,"role":"meta","label":"x"}]}`,
		`{"moves":[{"sentence":4,"role":"introduce","label":"gamma"},{"sentence":5,"role":"meta","label":"x"}]}`,
	}}
	kept, _, err := DeriveMovesResumable(context.Background(), m2, doc, 2, store, nil)
	if err != nil {
		t.Fatalf("resume: %v", err)
	}
	if m2.calls != 2 {
		t.Errorf("made %d calls on resume, want 2 — the finished window was re-read", m2.calls)
	}
	// Every window's moves are present, including the one from the first run.
	seen := map[int]bool{}
	for _, mv := range kept {
		seen[mv.Sentence] = true
	}
	for i := 0; i < 6; i++ {
		if !seen[i] {
			t.Errorf("sentence %d missing after resume; work from the first run was lost", i)
		}
	}
}

// A pass that finished and found nothing must not be re-read: "ran and found
// nothing" and "never ran" are different facts and only one costs minutes.
func TestEmptyPassIsStillRecordedAsDone(t *testing.T) {
	doc := docOf("A.", "B.")
	store := newFakePassStore()
	m := &fakeModel{replies: []string{`{"moves":[]}`, `{"moves":[]}`}}
	if _, _, err := DeriveMovesResumable(context.Background(), m, doc, 2, store, nil); err != nil {
		t.Fatalf("DeriveMovesResumable: %v", err)
	}
	if _, ok := store.done[0]; !ok {
		t.Fatal("an empty pass was not recorded, so it would be paid for twice")
	}

	m2 := &fakeModel{}
	if _, _, err := DeriveMovesResumable(context.Background(), m2, doc, 2, store, nil); err != nil {
		t.Fatal(err)
	}
	if m2.calls != 0 {
		t.Errorf("re-read %d pass(es) that had already finished", m2.calls)
	}
}

// Failing to checkpoint must not lose the work in hand. Losing resumability is
// bad; throwing away the moves it exists to protect is worse.
func TestCheckpointFailureDoesNotAbortTheRun(t *testing.T) {
	doc := docOf("A.", "B.", "C.", "D.")
	store := newFakePassStore()
	store.failOn = 0
	m := &fakeModel{replies: []string{
		`{"moves":[{"sentence":0,"role":"introduce","label":"a"},{"sentence":1,"role":"meta","label":"x"}]}`,
		`{"moves":[{"sentence":2,"role":"introduce","label":"b"},{"sentence":3,"role":"meta","label":"x"}]}`,
	}}
	kept, _, err := DeriveMovesResumable(context.Background(), m, doc, 2, store, nil)
	if err != nil {
		t.Fatalf("a failed checkpoint aborted the run: %v", err)
	}
	if len(kept) != 4 {
		t.Fatalf("kept %d moves, want 4 — work was discarded because it could not be saved", len(kept))
	}
}

// With no store the pipeline must behave exactly as before.
func TestNilPassStoreChangesNothing(t *testing.T) {
	doc := docOf("A.", "B.")
	m := &fakeModel{replies: []string{`{"moves":[{"sentence":0,"role":"introduce","label":"a"},{"sentence":1,"role":"meta","label":"x"}]}`}}
	kept, _, err := DeriveMovesResumable(context.Background(), m, doc, 2, nil, nil)
	if err != nil || len(kept) != 2 {
		t.Fatalf("kept %d, err %v", len(kept), err)
	}
}
