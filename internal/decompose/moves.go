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
// Layer 2 — moves. What each sentence DOES.
//
// The only layer a model drives, asked the smallest question available: for
// sentence N, which of four things is happening, and if it refines something,
// which earlier sentence. The model returns indices and short labels. It never
// returns document text, so it cannot fabricate evidence — the worst it can do
// is mislabel, which a human sees beside a citation that is correct by
// construction.
//
// A sentence carries a LIST of moves. "We will build the splat widget via the
// new XYZ UI" introduces two things; "keep export, drop SSO" both elaborates
// and retracts. One role per sentence cannot express either.
//
// Every move is checked against layer 1 before it is allowed through, and what
// fails is counted rather than swallowed.
package decompose

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// Rejected is a move the checker refused — something the model asserted that
// the document cannot support.
type Rejected struct {
	Move   Move   `json:"move"`
	Reason string `json:"reason"`
}

// movePrompt is ordered for PREFIX REUSE, not for readability.
//
// Everything identical across passes comes first — the document, then the
// instructions — and everything that varies comes last. llama.cpp reuses the
// KV cache only up to the first byte that differs from the previous prompt, so
// a varying block placed early throws away the whole prefill.
//
// Measured on two consecutive passes over a real document: with the varying
// block early, 2,092 characters were shared and each pass re-prefilled from
// scratch at 8m27s. Reordered, 22,703 characters were shared and the second
// pass prefilled in 2m10s — the same tokens, four times faster. Across five
// passes that is roughly 19 minutes instead of 57.
const movePrompt = `--- THE DOCUMENT, in full ---
%s--- END OF DOCUMENT ---

You have just read the entire document. You are analysing it one sentence at a
time.

Each sentence does one or more of these things:

  introduce  states something NEW about the system being built — a capability,
             a rule, a fact, a defect. Something not previously on the table.
  elaborate  refines, constrains, gives detail about, or justifies something an
             EARLIER sentence already introduced.
  retract    removes, defers or cancels something introduced earlier
             ("dropped for now", "no longer required", "out of scope").
  meta       navigational text saying nothing about the system itself
             ("This section describes...", "See Appendix B.", "The table below").

A single sentence can do SEVERAL of these at once. "We will build the splat
widget via the new XYZ UI" introduces TWO things. "We keep export but drop SSO"
both elaborates and retracts. Return one entry per thing happening.

Sentences sharing a paragraph number were written together. A paragraph that
opens with a claim and continues is usually one introduce followed by
elaborates — the continuation is detail about the opening claim, not a new
idea. Judge the content, but let that grouping inform you.

For each entry return:
  sentence  the sentence number
  role      introduce | elaborate | retract | meta
  label     a short name for the idea, under 60 characters. For elaborate and
            retract, name the idea being refined, not the refinement.
  target    for elaborate and retract: the number of the earlier sentence that
            introduced that idea. You can see that sentence above — use its real
            number. Omit target only when you genuinely cannot tell which idea
            is meant.

COVERAGE IS REQUIRED. Every sentence number in the list at the end must appear
at least once in your answer. A sentence you have nothing to say about is
"meta" — say so explicitly rather than leaving it out. Returning a partial list
silently discards part of the document, which is worse than calling something
meta.

Do not quote the document. Every number you return must be one shown below.

%s--- CLASSIFY THESE SENTENCES ---
%s--- END ---

Return JSON only: {"moves":[{"sentence":1,"role":"introduce","label":"..."}]}`

const gapPrompt = `You analysed a document a moment ago and left these sentences
out of your answer. Each one still does one of these things:

  introduce  states something NEW about the system being built.
  elaborate  refines or gives detail about something introduced earlier.
  retract    removes, defers or cancels something introduced earlier.
  meta       navigational text saying nothing about the system itself.

Classify EVERY sentence below. There are only a few, so none may be omitted.
If a sentence says nothing about the system, answer meta — that is a real
answer, not a failure.

For each return: sentence (the number), role, label (under 60 characters), and
for elaborate/retract a target (an earlier sentence number) when you can
identify one.

%s--- SENTENCES ---
%s--- END ---

Return JSON only: {"moves":[{"sentence":1,"role":"meta","label":"..."}]}`

// gapRetryThreshold is the share of a window that must be missing before a
// second pass is worth its minutes. Below this, the stragglers are usually
// genuinely empty lines and a retry buys nothing.
const gapRetryThreshold = 0.25

// DeriveMoves runs the model over the document in sentence windows.
//
// progress, when non-nil, is called after each window with the number of
// windows finished and the total — the job layer turns that into a bar. The
// total is known before the first model call because layer 1 is instant, which
// is what lets the reading phase show a real denominator.
// PassStore lets a caller persist each reading pass as it completes and
// recover the ones already done. Optional: with a nil store the pipeline
// behaves exactly as before, which keeps this package free of a storage
// dependency and keeps every existing test valid.
//
// It exists because holding a whole run in memory means an interruption throws
// it away. Four of five passes once finished — 24 minutes of model time — and
// none of them survived the fifth failing.
type PassStore interface {
	// Completed returns window starts already read, with the moves each found.
	// A window present with no moves finished and found nothing, which is not
	// the same as never having run.
	Completed(ctx context.Context) (map[int][]Move, error)
	// Save records one finished pass. An error must not abort the run: losing
	// the ability to resume is bad, losing the work in hand is worse.
	Save(ctx context.Context, windowStart, windowEnd int, moves []Move, rejected int) error
}

func DeriveMoves(ctx context.Context, g Generator, doc *Document, window int, progress func(done, total int)) ([]Move, []Rejected, error) {
	return DeriveMovesResumable(ctx, g, doc, window, nil, progress)
}

// DeriveMovesResumable is DeriveMoves with pass persistence.
func DeriveMovesResumable(ctx context.Context, g Generator, doc *Document, window int, store PassStore, progress func(done, total int)) ([]Move, []Rejected, error) {
	// The window bounds how many entries are ASKED FOR, not how much the model
	// may see. Measured, in both directions: at 30 the model produced 112 moves
	// over 150 sentences; asked for all 150 at once it produced ONE and the
	// prose pass collapsed to nothing. Sustained structured output is the
	// binding limit, not context.
	if window <= 0 {
		window = 30
	}
	total := (len(doc.Sentences) + window - 1) / window
	if progress != nil {
		progress(0, total)
	}

	var kept []Move
	var rejected []Rejected
	introduced := map[int]bool{}

	// Passes already on disk for this document. Recovered before anything is
	// asked of the model, and their introduces registered, so a resumed run can
	// validate targets pointing back into work it did not do itself.
	var done map[int][]Move
	if store != nil {
		var err error
		if done, err = store.Completed(ctx); err != nil {
			done = nil // resuming is an optimisation; failing to is not fatal
		}
		for _, ms := range done {
			for _, m := range ms {
				if m.Role == RoleIntroduce {
					introduced[m.Sentence] = true
				}
				kept = append(kept, m)
			}
		}
	}

	// The whole document goes in EVERY prompt, and only a slice is asked about.
	//
	// Two separate limits were conflated before. Context is not the binding
	// one: a 159-line spec is ~6,900 tokens against a 32,768-token window, so
	// the model can afford to see all of it. What the model cannot do is
	// sustain 150 structured entries in one response — asked to, it produced
	// exactly ONE move for 150 sentences and the prose pass collapsed to
	// nothing.
	//
	// So the window bounds the OUTPUT, and the context bounds nothing. A
	// sentence refining something from earlier in the document can now see that
	// sentence and cite its real number, instead of guessing from a list of
	// labels standing in for text it was never shown. That guessing is what
	// produced 24 moves rejected for refining a sentence that introduced
	// nothing, and 12 for refining a LATER one.
	fullText := renderSentences(doc.Sentences)

	for start, w := 0, 0; start < len(doc.Sentences); start, w = start+window, w+1 {
		if err := ctx.Err(); err != nil {
			return kept, rejected, err
		}
		end := start + window
		if end > len(doc.Sentences) {
			end = len(doc.Sentences)
		}

		// Already read. Its moves are in `kept` from the recovery above.
		if _, ok := done[start]; ok {
			if progress != nil {
				progress(w+1, total)
			}
			continue
		}

		keptBefore, rejectedBefore := len(kept), len(rejected)
		raw, err := callBounded(ctx, g, end-start, sprintfMovePrompt(fullText, openIdeasBlock(kept, doc, 24), renderSentences(doc.Sentences[start:end])))
		if err != nil {
			return kept, rejected, fmt.Errorf("sentences %d-%d: %w", start, end-1, err)
		}

		var batch struct {
			Moves []Move `json:"moves"`
		}
		if err := json.Unmarshal([]byte(raw), &batch); err != nil {
			// One unparseable window must not lose the whole document. Record
			// it as a rejection and keep reading — a 40-page spec that fails
			// entirely because window 19 returned bad JSON is worse than one
			// that reports a gap.
			rejected = append(rejected, Rejected{
				Move:   Move{Sentence: start},
				Reason: fmt.Sprintf("window %d-%d returned unparseable JSON", start, end-1),
			})
			if progress != nil {
				progress(w+1, total)
			}
			continue
		}

		// Two passes over the batch: register every introduce first, THEN
		// validate targets. Without this, a model listing an elaboration
		// before the introduce it points at has that elaboration rejected for
		// pointing at nothing — an ordering artefact, not a real fault.
		for _, m := range batch.Moves {
			if m.Role == RoleIntroduce && m.Sentence >= 0 && m.Sentence < len(doc.Sentences) {
				introduced[m.Sentence] = true
			}
		}
		for _, m := range batch.Moves {
			if why := CheckMove(m, len(doc.Sentences), introduced); why != "" {
				rejected = append(rejected, Rejected{m, why})
				continue
			}
			kept = append(kept, m)
		}
		// Coverage is checked, not assumed. The model is told every sentence
		// must appear, and on a real document it ignored that for half of
		// them — 62 of 127 produced no move at all, which is indistinguishable
		// from reading the document badly unless something counts it.
		//
		// So the gap is measured and re-asked. Detecting it is free and exact;
		// trusting an instruction is neither.
		missing := uncovered(kept, start, end)
		if len(missing) > 0 && float64(len(missing))/float64(end-start) >= gapRetryThreshold {
			more, moreRejected := retryGap(ctx, g, doc, missing, kept, introduced)
			kept = append(kept, more...)
			rejected = append(rejected, moreRejected...)
		}

		// Snapshot AFTER the gap retry, not before: the retry appends to kept,
		// and a slice taken earlier would both miss those moves and risk
		// pointing at a stale backing array once append reallocates.
		passMoves := append([]Move(nil), kept[keptBefore:]...)
		passRejected := len(rejected) - rejectedBefore

		// Persist BEFORE reporting progress, so a crash between the two costs a
		// redundant pass rather than a lost one.
		if store != nil {
			if err := store.Save(ctx, start, end, passMoves, passRejected); err != nil {
				// Deliberately swallowed: the moves are in hand, and refusing
				// to continue because they could not be checkpointed would
				// throw away the very work this exists to protect.
				_ = err
			}
		}

		if progress != nil {
			progress(w+1, total)
		}
	}
	return kept, rejected, nil
}

// EstimateTokens is a rough size for the reading prompt: the document plus the
// instructions around it. Four characters per token is the usual approximation
// and it is close enough to decide whether a document fits.
//
// It exists so a caller can SEE the limit rather than discover it as a
// truncated answer, which is how a context overflow presents itself: not as an
// error, but as a model that quietly read less than you gave it.
func EstimateTokens(doc *Document) int {
	chars := len(movePrompt)
	for _, s := range doc.Sentences {
		chars += len(s.Text) + len(s.Section) + 16 // number, paragraph marker
	}
	return chars / 4
}

// uncovered lists sentence indices in [start,end) that no move mentions.
func uncovered(moves []Move, start, end int) []int {
	seen := map[int]bool{}
	for _, m := range moves {
		seen[m.Sentence] = true
	}
	var out []int
	for i := start; i < end; i++ {
		if !seen[i] {
			out = append(out, i)
		}
	}
	return out
}

// retryGap re-asks for the sentences the first pass skipped. One attempt only:
// a second failure means the model has nothing to say about them, and looping
// would spend minutes to learn that twice. Errors are swallowed deliberately —
// a failed gap pass must not lose the moves the first pass DID produce.
func retryGap(ctx context.Context, g Generator, doc *Document, missing []int, kept []Move, introduced map[int]bool) ([]Move, []Rejected) {
	var ss []Sentence
	for _, i := range missing {
		ss = append(ss, doc.Sentences[i])
	}
	raw, err := callBounded(ctx, g, len(missing), fmt.Sprintf(gapPrompt,
		openIdeasBlock(kept, doc, 16), renderSentences(ss)))
	if err != nil {
		return nil, nil
	}
	var batch struct {
		Moves []Move `json:"moves"`
	}
	if err := json.Unmarshal([]byte(raw), &batch); err != nil {
		return nil, nil
	}

	inGap := map[int]bool{}
	for _, i := range missing {
		inGap[i] = true
	}
	var out []Move
	var bad []Rejected
	for _, m := range batch.Moves {
		// A gap pass may only answer about the gap. Left unguarded, a model
		// re-answering a sentence the first pass already classified would
		// double it.
		if !inGap[m.Sentence] {
			continue
		}
		if why := CheckMove(m, len(doc.Sentences), introduced); why != "" {
			bad = append(bad, Rejected{m, why})
			continue
		}
		if m.Role == RoleIntroduce {
			introduced[m.Sentence] = true
		}
		out = append(out, m)
	}
	return out, bad
}

// callBounded runs one model call under CallTimeout, so a wedged generation
// fails the call rather than the afternoon.
func callBounded(ctx context.Context, g Generator, units int, prompt string) (string, error) {
	// len/4 is the usual token approximation and it only has to be close: the
	// deadline carries 2x headroom over measured cost either way.
	cctx, cancel := context.WithTimeout(ctx, CallTimeoutFor(len(prompt)/4, units))
	defer cancel()
	return g.GenerateJSON(cctx, prompt)
}

// sprintfMovePrompt renders the reading prompt. Extracted so its cost can be
// measured directly rather than inferred from how a run failed.
func sprintfMovePrompt(full, ideas, ask string) string {
	return fmt.Sprintf(movePrompt, full, ideas, ask)
}

// CheckMove is the 1→2 seam guard. It rejects only what the document cannot
// support — never a judgement about whether the role is correct, which is not
// something code can know.
func CheckMove(m Move, sentences int, introduced map[int]bool) string {
	if m.Sentence < 0 || m.Sentence >= sentences {
		return fmt.Sprintf("sentence %d does not exist", m.Sentence)
	}
	switch m.Role {
	case RoleIntroduce, RoleMeta:
		if m.Target != nil {
			return "target on a role that cannot have one"
		}
		return ""
	case RoleElaborate, RoleRetract:
		if m.Target == nil {
			// Legal, and common: the model knows a sentence clarifies
			// something and cannot say what. Layer 3 attaches it to the
			// nearest open idea and marks the link inferred.
			return ""
		}
		switch t := *m.Target; {
		case t < 0 || t >= sentences:
			return fmt.Sprintf("target %d does not exist", t)
		case t >= m.Sentence:
			return fmt.Sprintf("target %d is not earlier than %d", t, m.Sentence)
		case !introduced[t]:
			return fmt.Sprintf("target %d never introduced anything", t)
		}
		return ""
	default:
		return fmt.Sprintf("unknown role %q", m.Role)
	}
}

func renderSentences(ss []Sentence) string {
	var b strings.Builder
	section := ""
	for _, s := range ss {
		if s.Section != section {
			section = s.Section
			fmt.Fprintf(&b, "\n[section: %s]\n", section)
		}
		fmt.Fprintf(&b, "%d. (p%d) %s\n", s.Idx, s.Para, s.Text)
	}
	return b.String()
}

// openIdeasBlock lists the most recent ideas a later sentence may point back
// at. This is the honest form of a running summary: not a paraphrase of what
// came before, but the actual set of targets that exist.
func openIdeasBlock(moves []Move, doc *Document, max int) string {
	var lines []string
	seen := map[int]bool{}
	for i := len(moves) - 1; i >= 0 && len(lines) < max; i-- {
		m := moves[i]
		if m.Role != RoleIntroduce || seen[m.Sentence] {
			continue
		}
		seen[m.Sentence] = true
		lines = append(lines, fmt.Sprintf("%d. %s", m.Sentence, m.Label))
	}
	if len(lines) == 0 {
		return ""
	}
	sort.Slice(lines, func(i, j int) bool { return len(lines[i]) < len(lines[j]) })
	sort.Strings(lines)
	return "--- IDEAS ALREADY INTRODUCED (you may target these) ---\n" +
		strings.Join(lines, "\n") + "\n\n"
}
