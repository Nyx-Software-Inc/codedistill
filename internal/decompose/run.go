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
// Orchestration — the four layers, run once over a document.
//
// This package stays free of storage and HTTP. It reports progress through a
// callback and returns a Result; persisting that, and moving the job row along,
// belongs to the caller. Keeping it that way is what lets the whole pipeline be
// tested against a fake model with no database in sight.
package decompose

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strings"
)

// Phases, in order. The names are what a user reads on a progress bar, so they
// describe what is happening to their document rather than which layer is
// executing.
const (
	PhaseReading    = "reading"     // layer 2: the expensive one
	PhaseSorting    = "sorting"     // layer 4
	PhaseReconciled = "reconciling" // tables vs prose
)

// Progress reports movement within a phase. total is 0 when it is not yet
// known — the sorting phase cannot be sized until reading has finished, and a
// bar that invents a denominator is a bar that lies.
type Progress func(phase string, done, total int)

// Options tune a run. Zero values are sensible.
type Options struct {
	// Passes, when set, checkpoints each reading pass and skips those already
	// done. Without it an interruption discards the whole run: four of five
	// passes once completed, 24 minutes of model time, and none survived.
	Passes PassStore

	Window         int     // sentences per reading pass
	Batch          int     // nodes per sorting pass
	MatchThreshold float64 // reconciliation overlap, 0 -> 0.5
	SkipTables     bool
}

// Result is everything one run learned. Proposals are candidates; nothing
// exists until a human accepts one.
type Result struct {
	Document  *Document      `json:"document"`
	Graph     *Graph         `json:"graph"`
	Moves     []Move         `json:"moves"`
	Rejected  []Rejected     `json:"rejected"`
	Proposals []Proposal     `json:"proposals"`
	Skipped   []Skipped      `json:"skipped"`
	Recon     Reconciliation `json:"reconciliation"`

	// UnusableProjections counts nodes the model named that do not exist. A run
	// producing nothing needs to distinguish that from genuinely finding no
	// work, because the two need opposite fixes.
	UnusableProjections int `json:"unusable_projections"`

	TablesRead     int    `json:"tables_read"`
	TablesDeclined int    `json:"tables_declined"`
	SourceHash     string `json:"source_hash"`
	Model          string `json:"model"`
}

// Run executes the pipeline. It is cancellable at every phase boundary and
// inside both model loops, because fifteen minutes is long enough that a user
// will change their mind.
func Run(ctx context.Context, g Generator, lines []string, opts Options, progress Progress) (*Result, error) {
	if progress == nil {
		progress = func(string, int, int) {}
	}
	res := &Result{Model: g.Model(), SourceHash: HashLines(lines)}

	// Layer 1 — instant, and the reason the reading bar has a real denominator
	// from the very first tick.
	res.Document = Segment(lines)

	// Tables are read before the model runs: no inference, so nothing to wait
	// for, and the reconciliation later has something to compare against.
	if !opts.SkipTables {
		for _, tb := range ExtractTables(lines) {
			f, ok := tb.Fields()
			if !ok {
				res.TablesDeclined++
				continue
			}
			res.TablesRead++
			res.Proposals = append(res.Proposals, tb.RecordProposals(KindForHeader(tb.Header[f.Subject]))...)
		}
	}
	fromTable := res.Proposals

	// Layer 2 — the expensive phase.
	progress(PhaseReading, 0, 0)
	moves, rejected, err := DeriveMovesResumable(ctx, g, res.Document, opts.Window, opts.Passes, func(done, total int) {
		progress(PhaseReading, done, total)
	})
	res.Moves, res.Rejected = moves, rejected
	if err != nil {
		return res, err
	}

	// Layer 3 — deterministic, instant, no progress worth reporting.
	res.Graph = BuildGraph(res.Document, moves)

	// Layer 4 — its size is only knowable now, which is exactly the case the
	// job's revisable total exists for.
	progress(PhaseSorting, 0, 0)
	fromProse, skipped, unusable, err := Project(ctx, g, res.Document, res.Graph, opts.Batch, func(done, total int) {
		progress(PhaseSorting, done, total)
	})
	res.Skipped, res.UnusableProjections = skipped, unusable
	if err != nil {
		return res, err
	}

	progress(PhaseReconciled, 0, 1)
	res.Recon = Reconcile(fromTable, fromProse, opts.MatchThreshold)
	// Corroborated rows replace their table originals so a proposal is not
	// offered twice under two origins.
	res.Proposals = append(append(append([]Proposal{},
		res.Recon.Corroborated...), res.Recon.TableOnly...), res.Recon.ProseOnly...)
	progress(PhaseReconciled, 1, 1)

	return res, nil
}

// HashLines is a document's identity: same bytes, same document. Used to
// recognise a file dropped twice, which otherwise costs another full read for
// an answer already on disk.
func HashLines(lines []string) string {
	h := sha256.New()
	h.Write([]byte(strings.Join(lines, "\n")))
	return hex.EncodeToString(h.Sum(nil))
}

// Origin reports where a proposal came from, in the vocabulary the storage
// layer uses.
func (p Proposal) Origin() string {
	if p.FromTable {
		return "table"
	}
	return "prose"
}
