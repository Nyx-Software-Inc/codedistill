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

package api

import (
	"context"
	"encoding/json"
	"net/http"
	"sort"
	"strings"
	"time"

	"codedistill/internal/domain"
	"codedistill/internal/events"
	"codedistill/internal/git"
	"codedistill/internal/id"
	"codedistill/internal/risk"
	"codedistill/internal/verify"
)

// recordReviewForOwner records the human floor's approve/reject on an item's
// implementation (glass-box Phase 4, slice 4.3 — closing the loop). The decision
// becomes evidence the trust dial weighs. A write — gated by write-auth.
func (s *Server) recordReviewForOwner(ownerType string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ownerID := r.PathValue("id")
		if err := s.ensureOwnerExists(r, ownerType, ownerID); err != nil {
			writeErr(w, statusFor(err), err)
			return
		}
		var req struct {
			Decision string `json:"decision"`
			Note     string `json:"note"`
			Commit   string `json:"commit"`
		}
		if err := readJSON(r, &req); err != nil {
			writeErr(w, http.StatusBadRequest, err)
			return
		}
		decision := strings.TrimSpace(req.Decision)
		if decision != "approved" && decision != "rejected" {
			writeMsg(w, http.StatusBadRequest, "decision must be \"approved\" or \"rejected\"")
			return
		}
		d := &domain.ReviewDecision{
			ID: id.New(), OwnerType: ownerType, OwnerID: ownerID,
			Decision: decision, Note: strings.TrimSpace(req.Note),
			CommitSHA: strings.TrimSpace(req.Commit), Reviewer: "local",
			CreatedAt: time.Now().UTC(),
		}
		if d.CommitSHA == "" {
			if c, err := s.latestCommitAnchor(r.Context(), ownerType, ownerID); err == nil {
				d.CommitSHA = c
			}
		}
		if err := s.store.CreateReviewDecision(r.Context(), d); err != nil {
			writeErr(w, statusFor(err), err)
			return
		}
		// Approving the change resolves any active review flags on it — the human
		// looked and signed off, so the "needs another look" marker is satisfied.
		if decision == "approved" {
			_ = s.store.ClearReviewFlags(r.Context(), ownerType, ownerID, time.Now().UTC())
		}
		// Rejecting sends the work BACK: an item marked done that fails review
		// isn't done, so reopen it to a non-terminal status and clear the
		// completion metadata. It re-enters the active queue for rework.
		reopened := ""
		if decision == "rejected" {
			reopened = s.reopenForRework(r.Context(), ownerType, ownerID)
		}
		// Narrate the decision into the item's Log (poke-1 discipline: the Log
		// tells the item's story; a human sign-off is a chapter, not a side table).
		summary := "Review: approved — sign-off recorded"
		if decision == "rejected" {
			summary = "Review: rejected — needs rework"
			if reopened != "" {
				summary += " (reopened → " + reopened + ")"
			}
		}
		if d.Note != "" {
			summary += " · " + d.Note
		}
		s.recordEvent(r.Context(), ownerType, ownerID, "review", summary, sourceUI)
		writeJSON(w, http.StatusCreated, d)
	}
}

// latestReviewForOwner returns the item's most recent review decision, so a
// surface (the item's sign-off bar) can render a persisted "signed off" state
// instead of re-prompting every time it mounts. The response carries the
// commit the decision was made against, so the caller can tell a still-current
// sign-off from one that a later re-implementation superseded. 404 when the
// item has never been reviewed.
func (s *Server) latestReviewForOwner(ownerType string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ownerID := r.PathValue("id")
		if err := s.ensureOwnerExists(r, ownerType, ownerID); err != nil {
			writeErr(w, statusFor(err), err)
			return
		}
		d, err := s.store.LatestReviewDecision(r.Context(), ownerType, ownerID)
		if err != nil {
			writeErr(w, statusFor(err), err)
			return
		}
		writeJSON(w, http.StatusOK, d)
	}
}

// reopenForRework flips a done item back to a non-terminal status after a
// rejected review and clears its completion metadata. Returns the new status
// (for the Log), or "" if nothing changed / on error (best-effort).
func (s *Server) reopenForRework(ctx context.Context, ownerType, ownerID string) string {
	now := time.Now().UTC()
	switch ownerType {
	case ownerBugItem:
		b, err := s.store.GetBugItem(ctx, ownerID)
		if err != nil || !isTerminalBugStatus(b.Status) {
			return ""
		}
		b.Status, b.CompletedAt, b.CommitSHA, b.CommitTag = "open", nil, "", ""
		if s.store.UpdateBugItem(ctx, b) != nil {
			return ""
		}
		s.recordDerivedStatus(ctx, ownerType, ownerID, b.Status, sourceUI)
		s.bus.Publish(events.BugsChanged)
		return b.Status
	case ownerTodoItem:
		t, err := s.store.GetTodoItem(ctx, ownerID)
		if err != nil || !domain.IsTodoDone(t.Status) {
			return ""
		}
		t.Status, t.CompletedAt, t.CommitSHA, t.CommitTag = "in_progress", nil, "", ""
		if s.store.UpdateTodoItem(ctx, t) != nil {
			return ""
		}
		s.recordDerivedStatus(ctx, ownerType, ownerID, t.Status, sourceUI)
		s.bus.Publish(events.TodosChanged)
		return t.Status
	case ownerUseCaseItem:
		u, err := s.store.GetUseCaseItem(ctx, ownerID)
		if err != nil || u.Status != domain.UseCaseStatusCompleted {
			return ""
		}
		u.Status, u.ImplementationDate, u.CommitSHA, u.CommitTag = domain.UseCaseStatusInProgress, nil, "", ""
		if s.store.UpdateUseCaseItem(ctx, u) != nil {
			return ""
		}
		s.recordDerivedStatus(ctx, ownerType, ownerID, u.Status, sourceUI)
		s.bus.Publish(events.UseCasesChanged)
		return u.Status
	}
	_ = now
	return ""
}

// Review queue (glass-box Phase 4, slice 1 — attention routing). Ranks a
// project's implemented items by how much they deserve human eyes: risk =
// blast-radius (code metrics) + verification verdict + sensitivity (always-review
// zones). Advisory — it routes attention, it doesn't gate. The earned-trust dial
// (slice 4.2) will replace the fixed needs_review threshold with risk-vs-trust.

type reviewQueueEntry struct {
	OwnerType   string   `json:"owner_type"`
	ID          string   `json:"id"`
	Number      int      `json:"number"`
	Subject     string   `json:"subject"`
	Status      string   `json:"status"`
	Band        string   `json:"band"`
	Score       int      `json:"score"`
	Reasons     []string `json:"reasons"`
	NeedsReview bool     `json:"needs_review"`
	// Unanchored: the item is marked implemented but has NO recorded code
	// location (no code anchor). The intent→code throughline is broken, so it's
	// surfaced for review instead of silently dropped from the queue.
	Unanchored      bool   `json:"unanchored,omitempty"`
	InZone          bool   `json:"in_zone"`
	ComplexityBand  string `json:"complexity_band,omitempty"`
	HasVerification bool   `json:"has_verification"`
	Failing         bool   `json:"failing"`
	Refuted         bool   `json:"refuted"`
	// Reverted: a later commit reverted this item's implementation commit.
	Reverted bool `json:"reverted"`
	// Domains are the distinct top-level directories this change touched — the
	// per-domain competence map (Phase 4) judges the item by the strictest
	// (lowest-trust) area it touches, not one project-wide number.
	Domains []string `json:"domains,omitempty"`
	// Decision is the human floor's latest call: "" | "approved" | "rejected".
	Decision string `json:"decision,omitempty"`
	Note     string `json:"note,omitempty"`
	// Flagged: an active review_flag routes this item back for another look (e.g.
	// it was changed under a superseded/flawed skill version). Attention only — a
	// flag is never counted as trust evidence (it stays out of entryVerdict).
	Flagged bool `json:"flagged,omitempty"`
}

type reviewQueueResp struct {
	Items          []reviewQueueEntry `json:"items"`
	RepoConfigured bool               `json:"repo_configured"`
	// Trust is the project's earned-trust level; EscalateAtOrAbove is the
	// effective threshold after any human tightening.
	Trust             risk.Trust `json:"trust"`
	EscalateAtOrAbove string     `json:"escalate_at_or_above"`
	// DomainTrust is the per-area earned trust (Phase 4 competence map): each
	// touched top-level directory's own track record. An item escalates by the
	// strictest area it touches, so a trusted project still gets scrutiny in a
	// new corner of the codebase.
	DomainTrust map[string]risk.Trust `json:"domain_trust,omitempty"`
}

func (s *Server) reviewQueue(w http.ResponseWriter, r *http.Request) {
	pid := r.PathValue("id")
	proj, err := s.store.GetProject(r.Context(), pid)
	if err != nil {
		writeErr(w, statusFor(err), err)
		return
	}
	zones, _ := s.lookupSettingStrings(r.Context(), risk.ZonesSettingKey, pid, "")

	var repo *git.Repo
	var reverted map[string]bool
	if proj.RepoRoot != "" {
		if rp, err := git.Open(proj.RepoRoot); err == nil {
			repo = rp
			// Reverts are real-world negative evidence: scan recent history once
			// for "This reverts commit X" and match against each item's commit.
			reverted = s.cachedReverted(repo, proj.RepoRoot, time.Now().UTC())
		}
	}

	entries := []reviewQueueEntry{}
	add := func(ownerType, id string, number int, subject, status string) {
		if e, ok := s.assessItem(r.Context(), repo, proj.RepoRoot, reverted, zones, ownerType, id, number, subject, status); ok {
			entries = append(entries, e)
			return
		}
		// assessItem drops items with no usable code location. If the item was
		// positively implemented, that's a broken throughline — surface it as a
		// flag rather than letting it vanish from the queue.
		if !domain.WasImplemented(ownerType, status) {
			return
		}
		reason := "implemented, but no code location was recorded — this change can't be traced to code or verified"
		if anchors, err := s.store.ListCodeAnchors(r.Context(), ownerType, id); err == nil && len(anchors) > 0 {
			reason = "implemented, but no commit was recorded — the change can't be verified or placed in history"
		}
		// An unanchored item is still humanly sign-off-able (pure judgment — the
		// item bar offers exactly this), so read any recorded decision. Without
		// this the fallback entry always looks undecided and approving it is a
		// silent no-op.
		decision, note := "", ""
		if d, err := s.store.LatestReviewDecision(r.Context(), ownerType, id); err == nil {
			decision, note = d.Decision, d.Note
		}
		entries = append(entries, reviewQueueEntry{
			OwnerType: ownerType, ID: id, Number: number, Subject: subject, Status: status,
			Band: "Unknown", Unanchored: true, Reasons: []string{reason},
			Decision: decision, Note: note,
		})
	}
	if todos, err := s.store.ListTodoItems(r.Context(), pid); err == nil {
		for _, it := range todos {
			add(ownerTodoItem, it.ID, it.Number, it.Subject, it.Status)
		}
	}
	if bugs, err := s.store.ListBugItems(r.Context(), pid); err == nil {
		for _, it := range bugs {
			add(ownerBugItem, it.ID, it.Number, it.Subject, it.Status)
		}
	}
	if ucs, err := s.store.ListUseCaseItems(r.Context(), pid); err == nil {
		for _, it := range ucs {
			add(ownerUseCaseItem, it.ID, it.Number, it.Subject, it.Status)
		}
	}

	// Earned trust from the project's verification track record sets the
	// auto-clear-vs-escalate threshold (slice 4.2). The human may tighten it.
	// Per-domain competence map (Phase 4): the same evidence is also bucketed by
	// top-level directory, so an item escalates by the strictest area it touches.
	type tally struct{ clean, failed int }
	global := tally{}
	domEv := map[string]*tally{}
	for _, e := range entries {
		v := entryVerdict(e)
		if v == "" {
			continue
		}
		bump := func(t *tally) {
			if v == "clean" {
				t.clean++
			} else {
				t.failed++
			}
		}
		bump(&global)
		for _, d := range e.Domains {
			t := domEv[d]
			if t == nil {
				t = &tally{}
				domEv[d] = t
			}
			bump(t)
		}
	}
	trust := risk.AssessTrust(global.clean, global.failed)
	domainTrust := make(map[string]risk.Trust, len(domEv))
	for d, t := range domEv {
		domainTrust[d] = risk.AssessTrust(t.clean, t.failed)
	}
	human := ""
	if st, err := s.store.GetProjectSetting(r.Context(), pid, risk.TightenSettingKey); err == nil {
		_ = json.Unmarshal(st.Value, &human)
	}
	// strictestThreshold picks the lowest (most-escalating) earned band among the
	// domains an item touches, falling back to the project-wide trust when the
	// item has no domain. Human tightening applies on top.
	strictestThreshold := func(domains []string) risk.Band {
		band := trust.EscalateAtOrAbove
		for _, d := range domains {
			if dt, ok := domainTrust[d]; ok && risk.Rank(dt.EscalateAtOrAbove) < risk.Rank(band) {
				band = dt.EscalateAtOrAbove
			}
		}
		return risk.TightenedThreshold(band, risk.Band(human))
	}
	for i := range entries {
		// Active review flags route the item back for another look and surface
		// their reasons. A flag is attention-only — deliberately NOT fed into
		// entryVerdict above, so an unresolved suspicion never moves the trust dial.
		if flags, err := s.store.ActiveReviewFlags(r.Context(), entries[i].OwnerType, entries[i].ID); err == nil && len(flags) > 0 {
			entries[i].Flagged = true
			for _, f := range flags {
				entries[i].Reasons = append(entries[i].Reasons, f.Reason)
			}
		}
		// A reverted change always needs eyes, even if it was previously approved —
		// the revert is newer ground truth. A fresh flag likewise overrides a stale
		// approval. Those are newer NEGATIVE evidence, so they come first. Only then
		// does a human decision settle it: approved drops out of attention (this is
		// the escape hatch for an unanchored item — you sign off on judgment),
		// rejected stays as needs-rework. An undecided unanchored item still needs
		// eyes; everything else falls to the per-area threshold.
		switch {
		case entries[i].Reverted:
			entries[i].NeedsReview = true
		case entries[i].Flagged:
			entries[i].NeedsReview = true
		case entries[i].Decision == "approved":
			entries[i].NeedsReview = false
		case entries[i].Decision == "rejected":
			entries[i].NeedsReview = true
		case entries[i].Unanchored:
			// Untraceable AND unjudged — no code location and no human call yet.
			entries[i].NeedsReview = true
		default:
			entries[i].NeedsReview = risk.Escalate(risk.Band(entries[i].Band), entries[i].InZone, strictestThreshold(entries[i].Domains))
		}
	}

	// Untraceable (unanchored) items float to the very top — a missing
	// throughline is the most "we have no idea" state. Then highest risk first,
	// ties broken by raw score.
	sort.SliceStable(entries, func(i, j int) bool {
		if entries[i].Unanchored != entries[j].Unanchored {
			return entries[i].Unanchored
		}
		ri, rj := risk.Rank(risk.Band(entries[i].Band)), risk.Rank(risk.Band(entries[j].Band))
		if ri != rj {
			return ri > rj
		}
		return entries[i].Score > entries[j].Score
	})
	writeJSON(w, http.StatusOK, reviewQueueResp{
		Items: entries, RepoConfigured: repo != nil,
		Trust:             trust,
		EscalateAtOrAbove: string(risk.TightenedThreshold(trust.EscalateAtOrAbove, risk.Band(human))),
		DomainTrust:       domainTrust,
	})
}

// assessItem scores one item. ok is false when the item has no recorded
// implementation (no commit anchor) — there's no change to route attention to.
func (s *Server) assessItem(ctx context.Context, repo *git.Repo, repoRoot string, reverted map[string]bool, zones []string, ownerType, itemID string, number int, subject, status string) (reviewQueueEntry, bool) {
	anchors, err := s.store.ListCodeAnchors(ctx, ownerType, itemID)
	if err != nil || len(anchors) == 0 {
		return reviewQueueEntry{}, false
	}
	commits := map[string]bool{}
	paths := map[string]bool{}
	for _, a := range anchors {
		switch a.Kind {
		case "commit":
			if a.Revision != "" {
				commits[a.Revision] = true
			}
		case "file":
			if a.Path != "" {
				paths[a.Path] = true
			}
		}
	}
	if len(commits) == 0 {
		return reviewQueueEntry{}, false
	}

	complexityBand := ""
	if repo != nil {
		churn, hunks := 0, 0
		uniqueFiles := map[string]bool{}
		for rev := range commits {
			m, mErr := s.cachedCommitMetrics(ctx, repo, repoRoot, rev)
			if mErr != nil {
				continue
			}
			churn += m.Added + m.Deleted
			hunks += m.Hunks
			for _, p := range m.Paths {
				uniqueFiles[p] = true
				paths[p] = true
			}
		}
		complexityBand, _ = git.Complexity(churn, len(uniqueFiles), hunks)
	}

	hasV, failing, refuted := s.verificationSummary(ctx, ownerType, itemID)
	inZone := risk.MatchesZone(zones, mapKeys(paths))
	wasReverted := commitReverted(commits, reverted)
	a := risk.Score(risk.Inputs{
		ComplexityBand:       complexityBand,
		HasVerification:      hasV,
		DeterministicFailing: failing,
		ReviewRefuted:        refuted,
		InAlwaysReviewZone:   inZone,
		Reverted:             wasReverted,
	})
	decision, note := "", ""
	if d, err := s.store.LatestReviewDecision(ctx, ownerType, itemID); err == nil {
		decision, note = d.Decision, d.Note
	}
	return reviewQueueEntry{
		OwnerType: ownerType, ID: itemID, Number: number, Subject: subject, Status: status,
		Band: string(a.Band), Score: a.Score, Reasons: a.Reasons,
		InZone: inZone, ComplexityBand: complexityBand,
		HasVerification: hasV, Failing: failing, Refuted: refuted,
		Reverted: wasReverted, Domains: domainsOf(paths),
		Decision: decision, Note: note,
	}, true
}

// domainsOf returns the distinct top-level directories of an item's changed
// paths. Top-level files (no slash) belong to no domain and fall back to the
// project-wide trust. Sorted for stable output.
func domainsOf(paths map[string]bool) []string {
	seen := map[string]bool{}
	out := []string{}
	for p := range paths {
		p = strings.TrimLeft(p, "/")
		i := strings.IndexByte(p, '/')
		if i <= 0 {
			continue // top-level file → no domain
		}
		d := p[:i]
		if !seen[d] {
			seen[d] = true
			out = append(out, d)
		}
	}
	sort.Strings(out)
	return out
}

// entryVerdict reduces an entry to one piece of trust evidence: "clean",
// "failed", or "" (no evidence yet). A revert is failed above all; then a human
// decision; then the verification verdict. Mirrors the headline trust counting.
func entryVerdict(e reviewQueueEntry) string {
	switch {
	case e.Reverted:
		return "failed"
	case e.Decision == "approved":
		return "clean"
	case e.Decision == "rejected":
		return "failed"
	case e.HasVerification && (e.Failing || e.Refuted):
		return "failed"
	case e.HasVerification:
		return "clean"
	}
	return ""
}

// commitReverted reports whether any of an item's implementation commits appears
// in the reverted set. SHAs are matched by prefix in either direction, since a
// recorded anchor may be short while the revert marker names the full hash.
func commitReverted(commits map[string]bool, reverted map[string]bool) bool {
	if len(reverted) == 0 {
		return false
	}
	for c := range commits {
		for rv := range reverted {
			if c == rv || strings.HasPrefix(c, rv) || strings.HasPrefix(rv, c) {
				return true
			}
		}
	}
	return false
}

// verificationSummary reduces an item's latest verdicts (item-level + every
// criterion's) to three booleans the risk score needs. "failing" / "refuted"
// look only at the LATEST result per layer, so an old failure that's since gone
// green doesn't keep flagging the item.
func (s *Server) verificationSummary(ctx context.Context, ownerType, id string) (hasV, failing, refuted bool) {
	merge := func(results []*domain.VerificationResult) {
		var det, rev *domain.VerificationResult
		for _, r := range results { // newest-first
			// A skipped (tool-missing) or still-running result is not evidence:
			// an all-skipped verification must not count as "clean" trust.
			if r.Verdict != domain.VerifyVerdictSkipped && r.Verdict != domain.VerifyVerdictRunning {
				hasV = true
			}
			if det == nil && r.Layer == domain.VerifyLayerDeterministic && r.Verdict != domain.VerifyVerdictSkipped {
				det = r
			}
			if rev == nil && r.Layer == domain.VerifyLayerAIReview {
				rev = r
			}
		}
		if det != nil && (det.Verdict == domain.VerifyVerdictFail || det.Verdict == domain.VerifyVerdictError) {
			failing = true
		}
		if rev != nil && rev.Verdict == domain.VerifyVerdictFail {
			refuted = true
		}
	}
	if rs, err := s.store.ListVerificationResults(ctx, ownerType, id); err == nil {
		merge(rs)
	}
	if crits, err := s.store.ListAcceptanceCriteria(ctx, ownerType, id); err == nil {
		for _, c := range crits {
			if rs, err := s.store.ListVerificationResults(ctx, verify.OwnerAcceptanceCriterion, c.ID); err == nil {
				merge(rs)
			}
		}
	}
	return
}

func mapKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
