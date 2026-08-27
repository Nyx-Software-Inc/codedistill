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

package verify

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"codedistill/internal/domain"
	codegit "codedistill/internal/git"
	"codedistill/internal/id"
)

// CommandSettingKey is the project-scoped setting that holds the test command
// (a JSON string, e.g. "go test ./..."). It's the test member of CheckSpecs.
const CommandSettingKey = "verify.test_command"

// CheckSpec is one configurable item-level deterministic check: the per-kind
// project setting that holds its command, and the kind it runs as.
type CheckSpec struct {
	SettingKey string
	Kind       string
}

// checkSpecs is the fixed set of item-level deterministic checks a project can
// configure (glass-box Phase 3) — the test suite plus the scanners (slice 3).
// Each is an optional per-kind command setting; a non-empty value runs as that
// kind in the shared worktree. Slice order is the run + display order.
var checkSpecs = []CheckSpec{
	{CommandSettingKey, domain.VerifyKindTest},
	{"verify.lint_command", domain.VerifyKindLint},
	{"verify.types_command", domain.VerifyKindTypes},
	{"verify.sast_command", domain.VerifyKindSAST},
	{"verify.vuln_command", domain.VerifyKindVuln},
}

// CheckSpecs returns the configurable check specs so the API and settings UI can
// read/write the same per-kind setting keys without duplicating the list.
func CheckSpecs() []CheckSpec { return checkSpecs }

// ConfiguredCheck is a check a project has actually configured (a non-empty
// command at a kind), ready to run.
type ConfiguredCheck struct {
	Kind    string `json:"kind"`
	Command string `json:"command"`
}

// Trigger outcomes the caller distinguishes. ErrNoCommand / ErrNoRepo mean the
// project isn't set up for verification — callers treat them as "skip", not a
// failure. ErrAlreadyRunning means a run for the same item is in flight.
var (
	ErrNoCommand      = errors.New("no verification command configured for this project")
	ErrNoRepo         = errors.New("project has no repo configured")
	ErrAlreadyRunning = errors.New("a verification run is already in flight for this item")
)

// Store is the slice of storage.Storage the verification service needs:
// owner→project resolution, the command setting, and result persistence.
// storage.Storage satisfies it structurally.
type Store interface {
	GetTodoItem(ctx context.Context, id string) (*domain.TodoItem, error)
	GetBugItem(ctx context.Context, id string) (*domain.BugItem, error)
	GetUseCaseItem(ctx context.Context, id string) (*domain.UseCaseItem, error)
	GetKnowledgeEntry(ctx context.Context, id string) (*domain.KnowledgeEntry, error)
	GetScratchpadItem(ctx context.Context, id string) (*domain.ScratchpadItem, error)
	GetScratchpad(ctx context.Context, id string) (*domain.Scratchpad, error)
	GetProject(ctx context.Context, id string) (*domain.Project, error)
	GetProjectSetting(ctx context.Context, projectID, key string) (*domain.ProjectSetting, error)
	CreateVerificationResult(ctx context.Context, v *domain.VerificationResult) error
	UpdateVerificationResult(ctx context.Context, v *domain.VerificationResult) error
	// Per-criterion mapping (slice 2): list the item's criteria, advance the
	// ones whose test command ran.
	ListAcceptanceCriteria(ctx context.Context, ownerType, ownerID string) ([]*domain.AcceptanceCriterion, error)
	UpdateAcceptanceCriterion(ctx context.Context, c *domain.AcceptanceCriterion) error
	// RecordItemEvent narrates the verification lifecycle into the item's
	// activity log — the Log tab tells the loop's story, not just its edits.
	RecordItemEvent(ctx context.Context, e *domain.ItemEvent) error
	// FailStaleRunningVerifications finalizes rows left at "running" (the
	// process died mid-run) to error. Called once at startup — see
	// RecoverStaleRuns (audit H9).
	FailStaleRunningVerifications(ctx context.Context, now time.Time) (int, error)
}

// RecoverStaleRuns finalizes verification rows orphaned at "running" by a
// crash/restart — the in-flight run goroutine that owned them is gone, so they
// would otherwise show "running…" forever. Call once at serve startup.
func (s *Service) RecoverStaleRuns(ctx context.Context) {
	n, err := s.store.FailStaleRunningVerifications(ctx, time.Now().UTC())
	if err != nil {
		s.logf("verify: recover stale runs: %v", err)
		return
	}
	if n > 0 {
		s.logf("verify: finalized %d stale 'running' verification(s) from a prior run", n)
	}
}

// OwnerAcceptanceCriterion is the owner_type for a verification_result that
// belongs to a single acceptance criterion (vs. the whole item).
const OwnerAcceptanceCriterion = "acceptance_criterion"

// AIReviewSettingKey is the project-scoped boolean that enables the adversarial
// AI-review layer (slice 4). Off by default — opt-in, since it spends local LLM
// compute per criterion on every run.
const AIReviewSettingKey = "verify.ai_review"

// reviewDiffMaxBytes caps the diff fed to the reviewer so a large commit can't
// blow the model's context (or crowd out the actual change).
const reviewDiffMaxBytes = 32 * 1024

// reviewTimeout bounds one adversarial-review model call. Advisory layer, so a
// generous-but-finite cap: better a skipped review than a wedged run.
const reviewTimeout = 3 * time.Minute

// ReviewerModelSettingKey is the project-scoped local model the adversarial
// reviewer runs on (empty → the app default). Lets a project review on a
// stronger code model than the high-frequency classifier.
const ReviewerModelSettingKey = "verify.reviewer_model"

// Service runs the deterministic verification layer: it resolves an item to its
// project's repo + check command, records a "running" result, and asynchronously
// runs the check (in an isolated worktree at the recorded commit) and resolves
// the result to its terminal verdict. Safe for concurrent use; one run per item
// at a time (an in-memory in-flight guard).
type Service struct {
	store    Store
	logf     func(format string, args ...any)
	reviewer Reviewer // optional adversarial AI-review layer; nil disables it

	mu       sync.Mutex
	inflight map[string]struct{}
}

// NewService binds the service to its store. logf may be nil (logging becomes a
// no-op); it reports only background-goroutine failures the caller can't see.
func NewService(store Store, logf func(string, ...any)) *Service {
	if logf == nil {
		logf = func(string, ...any) {}
	}
	return &Service{store: store, logf: logf, inflight: map[string]struct{}{}}
}

// WithReviewer attaches the adversarial AI-review layer. When set and a project
// has verify.ai_review on, each ratified criterion also gets an independent
// refute-review (advisory — it never moves criterion state). Returns the receiver.
func (s *Service) WithReviewer(r Reviewer) *Service {
	s.reviewer = r
	return s
}

// Trigger resolves the project's repo + configured item-level checks (test +
// scanners), records a "running" verification_result per check, and runs them all
// asynchronously in one shared worktree — returning the running rows so a caller
// can display them immediately and poll for verdicts. The async run uses a
// background context so it survives the request that started it.
func (s *Service) Trigger(ctx context.Context, ownerType, ownerID, commitSHA string) ([]*domain.VerificationResult, error) {
	repoRoot, checks, aiReview, reviewerModel, err := s.resolve(ctx, ownerType, ownerID)
	if err != nil {
		return nil, err
	}
	if len(checks) == 0 {
		return nil, ErrNoCommand
	}
	if repoRoot == "" {
		return nil, ErrNoRepo
	}

	key := ownerType + "/" + ownerID
	s.mu.Lock()
	if _, busy := s.inflight[key]; busy {
		s.mu.Unlock()
		return nil, ErrAlreadyRunning
	}
	s.inflight[key] = struct{}{}
	s.mu.Unlock()

	now := time.Now().UTC()
	rows := make([]*domain.VerificationResult, 0, len(checks))
	var createErr error
	for _, c := range checks {
		vr := &domain.VerificationResult{
			ID:         id.New(),
			OwnerType:  ownerType,
			OwnerID:    ownerID,
			Layer:      domain.VerifyLayerDeterministic,
			Kind:       c.Kind,
			CheckName:  c.Command,
			Verdict:    domain.VerifyVerdictRunning,
			CommitSHA:  commitSHA,
			Summary:    "running…",
			ProducedBy: domain.VerifyProducedByBox,
			CreatedAt:  now,
			UpdatedAt:  now,
		}
		if err := s.store.CreateVerificationResult(ctx, vr); err != nil {
			// Best-effort: skip a row we couldn't persist rather than abort the batch.
			s.logf("verify: create %s result: %v", c.Kind, err)
			createErr = err
			continue
		}
		rows = append(rows, vr)
	}
	if len(rows) == 0 {
		s.release(key)
		// A storage failure is NOT a "no command configured" — reporting it as
		// ErrNoCommand made a DB outage look like a config gap (audit H9).
		if createErr != nil {
			return nil, fmt.Errorf("could not record verification: %w", createErr)
		}
		return nil, ErrNoCommand
	}

	go s.run(key, rows, repoRoot, commitSHA, aiReview, reviewerModel)
	return rows, nil
}

// run executes every item-level check (test + scanners) plus every mapped,
// ratified criterion in a single shared worktree, resolves each result row to its
// terminal verdict, and advances criterion state. Uses a background context so it
// outlives the triggering request. rows is non-empty (guaranteed by Trigger).
func (s *Service) run(key string, rows []*domain.VerificationResult, repoRoot, commitSHA string, aiReview bool, reviewerModel string) {
	defer s.release(key)
	ctx := context.Background()
	ownerType, ownerID := rows[0].OwnerType, rows[0].OwnerID

	// One checkout for the whole batch. If it fails (e.g. the commit doesn't
	// exist), every pre-created row carries the error and we stop — no
	// per-criterion rows are minted for a checkout that never happened.
	sess, err := Open(ctx, repoRoot, commitSHA)
	if err != nil {
		for _, vr := range rows {
			s.finalize(ctx, vr, Outcome{Verdict: domain.VerifyVerdictError, Summary: err.Error()})
		}
		s.narrate(ctx, ownerType, ownerID,
			fmt.Sprintf("Verification at %s errored: %s", short(commitSHA), err.Error()))
		return
	}
	defer sess.Close()

	// Item-level checks — the test suite (slice 1) and scanners (slice 3).
	passed, failed, skipped := 0, 0, 0
	for _, vr := range rows {
		s.finalize(ctx, vr, sess.Run(ctx, vr.CheckName, 0, 0))
		switch vr.Verdict {
		case domain.VerifyVerdictPass:
			passed++
		case domain.VerifyVerdictSkipped:
			skipped++ // tool not installed — neither pass nor fail
		default:
			failed++
		}
	}

	// Per-criterion checks (slice 2): each ratified criterion that maps to a
	// command. A pass advances it to satisfied (+satisfied_by), a fail to failed.
	crits, err := s.store.ListAcceptanceCriteria(ctx, ownerType, ownerID)
	if err != nil {
		s.logf("verify: list criteria for %s: %v", key, err)
		return
	}
	critSatisfied, critFailed := 0, 0
	for _, c := range crits {
		if strings.TrimSpace(c.TestCommand) == "" || !ratified(c.State) {
			continue
		}
		now := time.Now().UTC()
		cvr := &domain.VerificationResult{
			ID:         id.New(),
			OwnerType:  OwnerAcceptanceCriterion,
			OwnerID:    c.ID,
			Layer:      domain.VerifyLayerDeterministic,
			Kind:       domain.VerifyKindTest,
			CheckName:  c.TestCommand,
			Verdict:    domain.VerifyVerdictRunning,
			CommitSHA:  commitSHA,
			Summary:    "running…",
			ProducedBy: domain.VerifyProducedByBox,
			CreatedAt:  now,
			UpdatedAt:  now,
		}
		if err := s.store.CreateVerificationResult(ctx, cvr); err != nil {
			s.logf("verify: create criterion result for %s: %v", c.ID, err)
			continue
		}
		out := sess.Run(ctx, c.TestCommand, 0, 0)
		s.finalize(ctx, cvr, out)
		s.advanceCriterion(ctx, c, out, commitSHA)
		if out.Verdict == domain.VerifyVerdictPass {
			critSatisfied++
		} else {
			critFailed++
		}
	}

	// Narrate the batch into the item's Log: the loop's state, visible where
	// the item's story is read (poke-1: silence read as "fine").
	summary := fmt.Sprintf("Verification at %s: %d/%d checks passed", short(commitSHA), passed, passed+failed)
	if skipped > 0 {
		summary += fmt.Sprintf(" · %d skipped (tool not installed)", skipped)
	}
	if critSatisfied+critFailed > 0 {
		summary += fmt.Sprintf(" · criteria: %d satisfied, %d failed", critSatisfied, critFailed)
	}
	s.narrate(ctx, ownerType, ownerID, summary)

	// Independent AI review (slice 4): an adversarial second opinion per ratified
	// criterion, reusing the same criteria list. Advisory — it records an
	// ai-review verdict but never moves criterion state ("never the only guard").
	if aiReview && s.reviewer != nil {
		s.reviewCriteria(ctx, crits, ownerType, ownerID, repoRoot, commitSHA, reviewerModel)
	}
}

// reviewCriteria runs the adversarial reviewer over each ratified criterion and
// records an advisory ai-review result. Best-effort: if the diff can't be read,
// the whole pass is skipped (logged) rather than recording meaningless verdicts.
func (s *Service) reviewCriteria(ctx context.Context, crits []*domain.AcceptanceCriterion, ownerType, ownerID, repoRoot, commitSHA, reviewerModel string) {
	repo, err := codegit.Open(repoRoot)
	if err != nil {
		s.logf("verify: open repo for AI review: %v", err)
		return
	}
	diff, err := repo.CommitDiff(ctx, commitSHA, reviewDiffMaxBytes)
	if err != nil {
		s.logf("verify: diff for AI review %s: %v", commitSHA, err)
		return
	}
	intent := s.itemIntent(ctx, ownerType, ownerID)
	for _, c := range crits {
		if !ratified(c.State) {
			continue
		}
		now := time.Now().UTC()
		rvr := &domain.VerificationResult{
			ID:         id.New(),
			OwnerType:  OwnerAcceptanceCriterion,
			OwnerID:    c.ID,
			Layer:      domain.VerifyLayerAIReview,
			Kind:       "review",
			CheckName:  "AI review",
			Verdict:    domain.VerifyVerdictRunning,
			CommitSHA:  commitSHA,
			Summary:    "reviewing…",
			ProducedBy: domain.VerifyProducedByAI,
			CreatedAt:  now,
			UpdatedAt:  now,
		}
		if err := s.store.CreateVerificationResult(ctx, rvr); err != nil {
			s.logf("verify: create review result for %s: %v", c.ID, err)
			continue
		}
		// Bound the model call: a hung Ollama request must not wedge the run
		// goroutine forever, which — because the inflight key is released only
		// when run() returns — would make every later Trigger for this item
		// return ErrAlreadyRunning until a restart (audit H9).
		rctx, cancel := context.WithTimeout(ctx, reviewTimeout)
		v, rerr := s.reviewer.Review(rctx, ReviewInput{Criterion: c.Text, Intent: intent, Diff: diff, Model: reviewerModel})
		cancel()
		s.finalize(ctx, rvr, reviewOutcome(v, rerr))
	}
}

// reviewOutcome maps a ReviewVerdict to an Outcome: refuted → fail, upheld →
// pass, model error → error. The explanation rides in Output.
func reviewOutcome(v ReviewVerdict, err error) Outcome {
	if err != nil {
		return Outcome{Verdict: domain.VerifyVerdictError, Summary: "review failed: " + err.Error()}
	}
	if v.Refuted {
		return Outcome{Verdict: domain.VerifyVerdictFail, Summary: "refuted", Output: v.Explanation}
	}
	return Outcome{Verdict: domain.VerifyVerdictPass, Summary: "upheld — could not refute", Output: v.Explanation}
}

// itemIntent assembles a short intent string (subject + detail) for the reviewer's
// context. Empty for owner types without an intent.
func (s *Service) itemIntent(ctx context.Context, ownerType, ownerID string) string {
	switch ownerType {
	case "todo_item":
		if it, err := s.store.GetTodoItem(ctx, ownerID); err == nil {
			return joinIntent(it.Subject)
		}
	case "bug_item":
		if it, err := s.store.GetBugItem(ctx, ownerID); err == nil {
			return joinIntent(it.Subject, it.ExpectedBehavior, it.ActualBehavior)
		}
	case "use_case_item":
		if it, err := s.store.GetUseCaseItem(ctx, ownerID); err == nil {
			return joinIntent(it.Subject, it.Description)
		}
	}
	return ""
}

func joinIntent(parts ...string) string {
	kept := make([]string, 0, len(parts))
	for _, p := range parts {
		if t := strings.TrimSpace(p); t != "" {
			kept = append(kept, t)
		}
	}
	return strings.Join(kept, "\n")
}

// finalize writes an Outcome onto a result row (running → terminal verdict).
func (s *Service) finalize(ctx context.Context, vr *domain.VerificationResult, out Outcome) {
	vr.Verdict = out.Verdict
	vr.ExitCode = out.ExitCode
	vr.Summary = out.Summary
	vr.Output = out.Output
	vr.DurationMS = out.Duration.Milliseconds()
	vr.UpdatedAt = time.Now().UTC()
	if err := s.store.UpdateVerificationResult(ctx, vr); err != nil {
		s.logf("verify: persist result %s: %v", vr.ID, err)
	}
}

// advanceCriterion moves a criterion's state to match a deterministic verdict:
// pass → satisfied (+satisfied_by = commit), fail → failed (clears satisfied_by).
// An error/inconclusive verdict leaves the criterion untouched.
func (s *Service) advanceCriterion(ctx context.Context, c *domain.AcceptanceCriterion, out Outcome, commitSHA string) {
	var state, satisfiedBy string
	switch out.Verdict {
	case domain.VerifyVerdictPass:
		state, satisfiedBy = "satisfied", commitSHA
	case domain.VerifyVerdictFail:
		state, satisfiedBy = "failed", ""
	default:
		return
	}
	if c.State == state && c.SatisfiedBy == satisfiedBy {
		return
	}
	c.State = state
	c.SatisfiedBy = satisfiedBy
	c.UpdatedAt = time.Now().UTC()
	if err := s.store.UpdateAcceptanceCriterion(ctx, c); err != nil {
		s.logf("verify: advance criterion %s: %v", c.ID, err)
	}
}

// ratified reports whether a criterion is a committed contract the box may move
// on verdicts. proposed (not yet accepted) and rejected (dismissed) are left
// alone — you ratify at the acceptance-criteria altitude before the box acts.
func ratified(state string) bool {
	return state == "accepted" || state == "satisfied" || state == "failed"
}

// narrate appends a system event to the item's activity log. Best-effort —
// verification's verdicts are the record; the narration is the story.
func (s *Service) narrate(ctx context.Context, ownerType, ownerID, summary string) {
	err := s.store.RecordItemEvent(ctx, &domain.ItemEvent{
		ID: id.New(), OwnerType: ownerType, OwnerID: ownerID,
		Kind: "verification", Summary: summary, Source: "system", CreatedAt: time.Now().UTC(),
	})
	if err != nil {
		s.logf("verify: narrate %s/%s: %v", ownerType, ownerID, err)
	}
}

// short is a display-length commit SHA.
func short(sha string) string {
	if len(sha) > 7 {
		return sha[:7]
	}
	return sha
}

func (s *Service) release(key string) {
	s.mu.Lock()
	delete(s.inflight, key)
	s.mu.Unlock()
}

// resolve maps an item to its project's repo root, configured item-level checks,
// and whether the AI-review layer is enabled.
func (s *Service) resolve(ctx context.Context, ownerType, ownerID string) (repoRoot string, checks []ConfiguredCheck, aiReview bool, reviewerModel string, err error) {
	projectID, err := s.ownerProjectID(ctx, ownerType, ownerID)
	if err != nil {
		return "", nil, false, "", err
	}
	proj, err := s.store.GetProject(ctx, projectID)
	if err != nil {
		return "", nil, false, "", err
	}
	// reviewer_model is a plain JSON-string setting, same shape as a check command.
	return proj.RepoRoot, s.checksForProject(ctx, projectID), s.aiReviewEnabled(ctx, projectID),
		s.projectCommand(ctx, projectID, ReviewerModelSettingKey), nil
}

// aiReviewEnabled reads the verify.ai_review boolean for the project (default false).
func (s *Service) aiReviewEnabled(ctx context.Context, projectID string) bool {
	st, err := s.store.GetProjectSetting(ctx, projectID, AIReviewSettingKey)
	if err != nil {
		return false
	}
	var on bool
	return json.Unmarshal(st.Value, &on) == nil && on
}

// checksForProject returns the project's configured item-level checks (the test
// suite + any scanners), in CheckSpecs order. Missing rows / unparseable JSON
// are skipped silently — a project opts in by setting a command.
func (s *Service) checksForProject(ctx context.Context, projectID string) []ConfiguredCheck {
	out := []ConfiguredCheck{}
	for _, spec := range checkSpecs {
		if cmd := s.projectCommand(ctx, projectID, spec.SettingKey); cmd != "" {
			out = append(out, ConfiguredCheck{Kind: spec.Kind, Command: cmd})
		}
	}
	return out
}

// projectCommand reads a per-kind command setting. Missing row or unparseable
// JSON → "" (that check is simply unconfigured), never an error.
func (s *Service) projectCommand(ctx context.Context, projectID, key string) string {
	st, err := s.store.GetProjectSetting(ctx, projectID, key)
	if err != nil {
		return ""
	}
	var cmd string
	if json.Unmarshal(st.Value, &cmd) != nil {
		return ""
	}
	return strings.TrimSpace(cmd)
}

// ownerProjectID resolves the owning project across owner types — the same
// addressing code anchors and acceptance criteria use.
func (s *Service) ownerProjectID(ctx context.Context, ownerType, ownerID string) (string, error) {
	switch ownerType {
	case "todo_item":
		it, err := s.store.GetTodoItem(ctx, ownerID)
		if err != nil {
			return "", err
		}
		return it.ProjectID, nil
	case "bug_item":
		it, err := s.store.GetBugItem(ctx, ownerID)
		if err != nil {
			return "", err
		}
		return it.ProjectID, nil
	case "use_case_item":
		it, err := s.store.GetUseCaseItem(ctx, ownerID)
		if err != nil {
			return "", err
		}
		return it.ProjectID, nil
	case "knowledge_entry":
		it, err := s.store.GetKnowledgeEntry(ctx, ownerID)
		if err != nil {
			return "", err
		}
		return it.ProjectID, nil
	case "scratchpad_item":
		it, err := s.store.GetScratchpadItem(ctx, ownerID)
		if err != nil {
			return "", err
		}
		sp, err := s.store.GetScratchpad(ctx, it.ScratchpadID)
		if err != nil {
			return "", err
		}
		return sp.ProjectID, nil
	default:
		return "", errors.New("unknown owner_type " + ownerType)
	}
}
