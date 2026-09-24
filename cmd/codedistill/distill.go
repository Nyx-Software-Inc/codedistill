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
// `codedistill distill` — a document in, proposed work items out.
//
// The command exists before the UI on purpose. Decomposition is judged by what
// it produces on a real document, and a terminal that prints the reconciliation
// and can accept in bulk gets that judgement days earlier than a review screen
// would.

package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"codedistill/internal/api"
	"codedistill/internal/decompose"
	"codedistill/internal/domain"
	"codedistill/internal/id"
	"codedistill/internal/jobrun"
	"codedistill/internal/ollama"
	"codedistill/internal/storage/sqlite"
)

func cmdDistill(dbPath, model, endpoint, modelAPI, file, scratchpadID string, acceptAll, again bool) error {
	ctx := context.Background()

	src, err := decompose.ReadDocument(file)
	if err != nil {
		return err
	}
	info, _ := os.Stat(file)
	lines := src.Lines

	store, err := sqlite.OpenDSN(dbPath)
	if err != nil {
		return err
	}
	defer store.Close()
	if err := store.Migrate(ctx); err != nil {
		return err
	}
	if err := ensureDefaults(ctx, store); err != nil {
		return fmt.Errorf("seed defaults: %w", err)
	}
	if n, err := store.InterruptStaleJobs(ctx, time.Now().UTC()); err == nil && n > 0 {
		fmt.Printf("cleared %d job(s) interrupted by an earlier run\n", n)
	}

	if scratchpadID == "" {
		scratchpadID = defaultScratchpadID
	}
	sp, err := store.GetScratchpad(ctx, scratchpadID)
	if err != nil || sp == nil {
		return fmt.Errorf("scratchpad %q not found", scratchpadID)
	}
	projectID := sp.ProjectID

	// Have we read this exact document before? Fifteen minutes is too long to
	// spend re-deriving an answer already on disk.
	label := filepath.Base(file)
	hash := decompose.HashLines(lines)
	priors, err := store.FindDecomposeRuns(ctx, projectID, hash, label)
	if err != nil {
		return err
	}
	for _, prior := range priors {
		switch {
		case prior.SourceHash == hash && !again:
			fmt.Printf("This document was already decomposed on %s (job %s).\n",
				prior.CreatedAt.Format("2 Jan 2006 15:04"), prior.JobID)
			fmt.Printf("  %d sentences, %d proposals reconciled (%d corroborated, %d table-only)\n",
				prior.Sentences, prior.Corroborated+prior.TableOnly+prior.ProseOnly,
				prior.Corroborated, prior.TableOnly)
			fmt.Println("  Same bytes, so re-reading would produce the same answer. Use -again to run anyway.")
			return nil
		case prior.SourceLabel == label && prior.SourceHash != hash:
			fmt.Printf("Note: %q was decomposed before, on %s, with different content.\n",
				label, prior.CreatedAt.Format("2 Jan 2006"))
			fmt.Printf("  This looks like a revision. Proposals are NOT yet diffed against the\n")
			fmt.Printf("  items that review created, so expect duplicates of what you accepted.\n\n")
		}
	}

	// The client's own 5-minute default sits UNDERNEATH decompose's scaled
	// per-call deadline, and the inner limit wins — a reading pass carrying the
	// whole document as context crossed it and died at exactly five minutes
	// while its own deadline said seven. Hand it a client that cannot preempt
	// the deadline the package computes.
	// The decomposer WORKER decides which model reads the document. A local 7B
	// found 27 of 37 written-down requirements in a real spec and took 46
	// minutes; pointing this at something stronger is the whole reason the
	// provider layer exists. Unbound falls back to the flags, so an install
	// with nothing configured still works.
	roleClient, prov, err := api.StepClient(ctx, store, domain.JobDecompose, 0,
		domain.WorkerDecomposer, projectID, decompose.MaxCallTimeout+time.Minute)
	if err != nil {
		return err
	}
	if roleClient != nil {
		fmt.Printf("  using %s (%s / %s)", prov.Name, prov.Protocol, prov.Model)
		if !prov.IsLocal {
			// Said out loud every time. A document leaving the machine is a
			// thing someone should never discover afterwards.
			fmt.Printf("  — this sends the document off this machine")
		}
		fmt.Println()
	}

	cl := ollama.New(ollama.WithModel(model), ollama.WithEndpoint(endpoint),
		ollama.WithProtocol(ollama.ParseProtocol(modelAPI)),
		ollama.WithHTTPClient(&http.Client{Timeout: decompose.MaxCallTimeout + time.Minute}),
		// Ollama's default is 4096 whatever the model holds, and it truncates
		// silently rather than erroring. Every reading pass carries the whole
		// document, so without this the model is handed a prompt with its own
		// instructions cut off the front and answers from the remainder.
		ollama.WithContextTokens(decompose.ContextTokens))
	// A configured role wins over the flags.
	var gen decompose.Generator = cl
	if roleClient != nil {
		gen = roleClient
	}

	if roleClient == nil && !cl.Reachable(ctx) {
		return fmt.Errorf("model at %s is not reachable — decomposition needs it (tables alone would read, work items would not)", endpoint)
	}

	// The run itself is shared with the submit dialog's path. Only the
	// REPORTING differs — a terminal rewrites a line in place, a browser polls
	// the row — so anything else living here would be a second implementation
	// of the same job, free to drift from the one people click.

	// In-place rewriting needs a terminal. Redirected to a file or a pipe, the
	// carriage returns pile up into an unreadable smear, so a non-tty gets one
	// clean line per phase instead.
	tty := isTerminal(os.Stdout)
	lastPhase := ""
	// Elapsed time on every tick, and a line per pass when not on a terminal.
	// A run that printed "reading" and then nothing was indistinguishable from
	// a dead one — a suspended laptop cost an afternoon before anyone noticed
	// the process had used zero CPU.
	began := time.Now()

	// A ticker, not just a progress callback. Progress fires at pass
	// boundaries, and reading the whole document in one pass means it fires
	// exactly twice — so a fifteen-minute call showed "0/1  0s" and then
	// nothing, which is the same silence that hid a suspended laptop for six
	// hours. Liveness cannot depend on the work reporting itself.
	var mu sync.Mutex
	lastLine := ""
	stopTick := make(chan struct{})
	if tty {
		go func() {
			tick := time.NewTicker(10 * time.Second)
			defer tick.Stop()
			for {
				select {
				case <-stopTick:
					return
				case <-tick.C:
					mu.Lock()
					if lastLine != "" {
						fmt.Printf("\r%s  %8s", lastLine, time.Since(began).Round(time.Second))
					}
					mu.Unlock()
				}
			}
		}()
	}

	out, runErr := jobrun.RunDecompose(ctx, store, jobrun.DecomposeInput{
		ProjectID: projectID, ScratchpadID: scratchpadID, Label: label,
		Lines: lines, SourceHash: hash, Format: src.Format, FileInfo: info,
		Again: again, Gen: gen, NewID: id.New,
	}, func(phase string, done, total int) {
		newPhase := phase != lastPhase
		if newPhase {
			if lastPhase != "" && tty {
				fmt.Println()
			}
			lastPhase = phase
		}
		el := time.Since(began).Round(time.Second)
		mu.Lock()
		if total > 0 {
			lastLine = fmt.Sprintf("  %-12s %3d/%-3d", phase, done, total)
		} else {
			lastLine = fmt.Sprintf("  %-12s         ", phase)
		}
		line := lastLine
		mu.Unlock()

		switch {
		case tty:
			fmt.Printf("\r%s  %8s", line, el)
		case total > 0:
			fmt.Printf("%s  %8s\n", line, el)
		case newPhase:
			fmt.Printf("%s  %8s\n", line, el)
		}
	})
	close(stopTick)
	fmt.Println()
	if runErr != nil {
		return runErr
	}
	job, run := out.Job, out.Run
	res, props := out.Detail, out.Proposal

	if out.Resumed > 0 {
		fmt.Printf("  resumed: %s were already read and were not read again\n\n", passWord(out.Resumed))
	}
	printDistillSummary(run, res, props)

	if !acceptAll {
		fmt.Printf("\nNothing has been created. Run again with -accept to turn these into items.\n")
		return nil
	}
	// The document's own scratchpad. The UI can direct the work elsewhere;
	// -accept has nowhere to ask, and putting it beside the document is the
	// behaviour that already existed.
	return acceptProposals(ctx, store, job.ID, scratchpadID)
}

// printDistillSummary reports the run as TWO independent readings and then
// their reconciliation, rather than as one merged number.
//
// The merged view was unreadable in practice: it showed "use_case 31" with no
// todos, which looked like a finding about the document but was an artefact of
// the table deciding the kind for every corroborated row. And it gave no way to
// judge the prose pass at all — whether it compressed 127 sentences correctly
// or swallowed requirements was invisible.
func printDistillSummary(run *domain.DecomposeRun, res *decompose.Result, props []*domain.DecomposeProposal) {
	rule := strings.Repeat("-", 68)

	fmt.Printf("\n%s\n  DOCUMENT\n%s\n", rule, rule)
	fmt.Printf("  %d lines -> %d sentences", res.Document.Lines, run.Sentences)
	if run.ExcludedLines > 0 {
		fmt.Printf("   (%d lines set aside as non-prose)", run.ExcludedLines)
	}
	fmt.Println()

	// --- the prose pass -------------------------------------------------
	roles := map[string]int{}
	for _, m := range res.Moves {
		roles[m.Role]++
	}
	asserted, retracted := 0, 0
	for _, n := range res.Graph.Nodes {
		if n.State == decompose.StateAsserted {
			asserted++
		} else {
			retracted++
		}
	}
	proseProposals := len(res.Recon.Corroborated) + len(res.Recon.ProseOnly)

	fmt.Printf("\n%s\n  PROSE PASS — inferred by the model\n%s\n", rule, rule)
	fmt.Printf("  %d sentences -> %d moves -> %s -> %s\n",
		run.Sentences, len(res.Moves),
		plural(len(res.Graph.Nodes), "idea"), plural(proseProposals, "proposal"))
	fmt.Printf("    moves: %d introduce, %d elaborate, %d retract, %d meta\n",
		roles[decompose.RoleIntroduce], roles[decompose.RoleElaborate],
		roles[decompose.RoleRetract], roles[decompose.RoleMeta])

	// Coverage, which is the most important number here and was invisible.
	// A sentence the model returned nothing for was neither read nor declined
	// — it was skipped, and skipping half a document is indistinguishable from
	// reading it badly unless the report says so.
	touched := map[int]bool{}
	for _, m := range res.Moves {
		touched[m.Sentence] = true
	}
	if silent := run.Sentences - len(touched); silent > 0 {
		fmt.Printf("    %d of %d sentences produced NO move at all (%d%% of the document\n",
			silent, run.Sentences, silent*100/max(run.Sentences, 1))
		fmt.Printf("      was neither read as work nor declined as meta)\n")
	}
	if retracted > 0 {
		fmt.Printf("    %d idea(s) withdrawn by the document itself\n", retracted)
	}
	if len(res.Skipped) > 0 {
		fmt.Printf("    %d idea(s) declined as not-work:\n", len(res.Skipped))
		for i, s := range res.Skipped {
			if i == 5 {
				fmt.Printf("        ... and %d more\n", len(res.Skipped)-5)
				break
			}
			fmt.Printf("        %-40s %s\n", trunc64(s.Intent, 40), s.Reason)
		}
	}

	// --- the table pass -------------------------------------------------
	if run.TablesRead > 0 || run.TablesDeclined > 0 {
		tableProposals := 0
		for _, p := range props {
			if p.Origin == domain.OriginTable {
				tableProposals++
			}
		}
		fmt.Printf("\n%s\n  TABLE PASS — read, not inferred\n%s\n", rule, rule)
		fmt.Printf("  %d record table(s) -> %d proposals", run.TablesRead, tableProposals)
		if run.TablesDeclined > 0 {
			fmt.Printf("   (%d table(s) declined: no requirement column)", run.TablesDeclined)
		}
		fmt.Println()
	}

	// --- reconciliation -------------------------------------------------
	fmt.Printf("\n%s\n  RECONCILED — %d proposals\n%s\n", rule, len(props), rule)
	fmt.Printf("    corroborated %3d   both passes found it independently\n", run.Corroborated)
	fmt.Printf("    table only   %3d   written down; the prose pass did not find it\n", run.TableOnly)
	fmt.Printf("    prose only   %3d   inferred from prose; not in any table\n", run.ProseOnly)

	kinds := map[string]int{}
	for _, p := range props {
		kinds[p.Kind]++
	}
	var ks []string
	for k := range kinds {
		ks = append(ks, k)
	}
	sort.Strings(ks)
	fmt.Printf("\n  by kind: ")
	for _, k := range ks {
		fmt.Printf("%s %d   ", k, kinds[k])
	}
	fmt.Println()
	if run.Corroborated > 0 && run.TablesRead > 0 {
		// Without this the kind tally reads as a judgement about the document.
		fmt.Printf("    (a corroborated row takes its kind and priority from the table,\n")
		fmt.Printf("     so the prose pass's own classification of those %d is not shown)\n", run.Corroborated)
	}

	if run.TableOnly > 0 {
		fmt.Printf("\n  WRITTEN DOWN BUT NOT FOUND IN THE PROSE:\n")
		for _, p := range res.Recon.TableOnly {
			fmt.Printf("    %-8s %s\n", p.ExternalRef, trunc64(p.Subject, 66))
		}
		fmt.Printf("    Matching is by word overlap, so this list can OVERSTATE: a\n")
		fmt.Printf("    requirement the prose phrased differently lands here too.\n")
	}

	printSeamHealth(run, res)
}

// printSeamHealth reports what each layer refused, grouped by cause. The old
// version printed a bare count, which said something was wrong without saying
// what — and a run that produces too little needs to distinguish "the model
// misbehaved" from "the document says less than you thought".
func printSeamHealth(run *domain.DecomposeRun, res *decompose.Result) {
	if run.RejectedMoves == 0 && run.InferredLinks == 0 &&
		res.Graph.RetractsDropped == 0 && res.UnusableProjections == 0 && res.Graph.Orphans == 0 {
		return
	}
	fmt.Printf("\n%s\n  SEAM HEALTH — what the layers refused\n%s\n", strings.Repeat("-", 68), strings.Repeat("-", 68))

	if run.RejectedMoves > 0 {
		byReason := map[string]int{}
		for _, r := range res.Rejected {
			byReason[classifyRejection(r.Reason)]++
		}
		fmt.Printf("  %d model claim(s) the document could not support:\n", run.RejectedMoves)
		var rs []string
		for k := range byReason {
			rs = append(rs, k)
		}
		sort.Strings(rs)
		for _, k := range rs {
			fmt.Printf("      %3d  %s\n", byReason[k], k)
		}
	}
	if run.InferredLinks > 0 {
		fmt.Printf("  %d elaboration(s) attached to the nearest open idea — the model\n", run.InferredLinks)
		fmt.Printf("      flagged them as clarifying something but could not say what\n")
	}
	if res.Graph.RetractsDropped > 0 {
		fmt.Printf("  %d retraction(s) DROPPED — no plausible target. Guessing would have\n", res.Graph.RetractsDropped)
		fmt.Printf("      withdrawn an unrelated requirement, so those ideas stay proposed\n")
	}
	if res.Graph.Orphans > 0 {
		fmt.Printf("  %d refinement(s) whose idea did not survive\n", res.Graph.Orphans)
	}
	if res.UnusableProjections > 0 {
		fmt.Printf("  %d sorting pass(es) named an idea that does not exist\n", res.UnusableProjections)
	}
}

// classifyRejection collapses a per-move reason into its class, so the report
// shows the shapes of failure rather than seventeen near-identical lines.
func classifyRejection(reason string) string {
	switch {
	case strings.Contains(reason, "unparseable"):
		return "a reading pass returned unusable JSON"
	case strings.Contains(reason, "does not exist"):
		return "pointed at a sentence that is not in the document"
	case strings.Contains(reason, "not earlier than"):
		return "claimed to refine a LATER sentence"
	case strings.Contains(reason, "never introduced"):
		return "refined a sentence that introduced nothing"
	case strings.Contains(reason, "cannot have one"):
		return "a new idea that also claimed to refine something"
	case strings.Contains(reason, "unknown role"):
		return "used a role outside the vocabulary"
	}
	return reason
}

// acceptProposals turns every pending proposal into a real item. Failures are
// reported per proposal and do not stop the rest: a run that aborts halfway
// leaves the user with no idea which half landed.
func acceptProposals(ctx context.Context, store *sqlite.Store, jobID, scratchpadID string) error {
	pending, err := store.ListProposals(ctx, jobID, []string{domain.ProposalPending})
	if err != nil {
		return err
	}
	created, failed := 0, 0
	for _, p := range pending {
		if _, err := decompose.Accept(ctx, store, p, scratchpadID, "", id.New, time.Now().UTC()); err != nil {
			fmt.Fprintf(os.Stderr, "  could not accept %q: %v\n", trunc64(p.Subject, 48), err)
			failed++
			continue
		}
		created++
	}
	fmt.Printf("\n  created %d item(s)", created)
	if failed > 0 {
		fmt.Printf(", %d failed", failed)
	}
	fmt.Println(" — open the app to review them.")
	return nil
}

// isTerminal reports whether w is a character device.
func isTerminal(f *os.File) bool {
	info, err := f.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}

// plural keeps the report readable: "1 ideas" is the kind of thing that makes
// a tool look unfinished.
func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// passWord avoids "5 passs": naive pluralisation is fine for "idea" and wrong
// for any noun already ending in s.
func passWord(n int) string {
	if n == 1 {
		return "1 pass"
	}
	return fmt.Sprintf("%d passes", n)
}

func plural(n int, noun string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, noun)
	}
	return fmt.Sprintf("%d %ss", n, noun)
}

func fileSize(info os.FileInfo) int64 {
	if info == nil {
		return 0
	}
	return info.Size()
}

func trunc64(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}

// cmdScratchpads lists every project and its scratchpads with their ids.
//
// It exists because `distill -scratchpad <id>` asked for an id the CLI gave no
// way to discover: the only routes were the HTTP API (which needs the server
// running and authenticated) or opening the SQLite file by hand. A command that
// requires an argument nobody can obtain is a command nobody can run.
func cmdScratchpads(dbPath string) error {
	ctx := context.Background()

	store, err := sqlite.OpenDSN(dbPath)
	if err != nil {
		return err
	}
	defer store.Close()
	if err := store.Migrate(ctx); err != nil {
		return err
	}
	if err := ensureDefaults(ctx, store); err != nil {
		return fmt.Errorf("seed defaults: %w", err)
	}

	projects, err := store.ListProjects(ctx)
	if err != nil {
		return err
	}
	if len(projects) == 0 {
		fmt.Println("no projects yet")
		return nil
	}

	for _, p := range projects {
		fmt.Printf("\n%s  (project %s)\n", p.Name, p.ID)
		pads, err := store.ListScratchpads(ctx, p.ID)
		if err != nil {
			return err
		}
		if len(pads) == 0 {
			fmt.Println("    (no scratchpads)")
			continue
		}
		for _, sp := range pads {
			marker := "  "
			if sp.ID == defaultScratchpadID {
				marker = "* " // where distill writes when -scratchpad is omitted
			}
			fmt.Printf("  %s%-24s %s\n", marker, sp.ID, sp.Name)
		}
	}
	fmt.Printf("\n* is the default: distill writes here when -scratchpad is omitted.\n")
	return nil
}
