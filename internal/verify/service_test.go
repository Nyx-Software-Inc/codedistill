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
	"sync"
	"testing"
	"time"

	"codedistill/internal/domain"
)

// fakeStore implements verify.Store for a single todo_item owned by project
// "p1" whose repo + command are configured by the test. Results are cloned on
// write so the test can read terminal state without racing the run goroutine.
type fakeStore struct {
	repoRoot string
	commands map[string]string // per-kind setting key → command
	aiReview bool              // verify.ai_review

	mu       sync.Mutex
	results  map[string]domain.VerificationResult
	criteria map[string]*domain.AcceptanceCriterion // seeded; mutated by advance
}

// newFakeStore seeds the test (verify.test_command) command; add scanners with
// withCheck.
func newFakeStore(repoRoot, command string) *fakeStore {
	cmds := map[string]string{}
	if command != "" {
		cmds[CommandSettingKey] = command
	}
	return &fakeStore{
		repoRoot: repoRoot, commands: cmds,
		results:  map[string]domain.VerificationResult{},
		criteria: map[string]*domain.AcceptanceCriterion{},
	}
}

func (f *fakeStore) withCheck(settingKey, command string) *fakeStore {
	f.commands[settingKey] = command
	return f
}

func (f *fakeStore) ListAcceptanceCriteria(_ context.Context, _, ownerID string) ([]*domain.AcceptanceCriterion, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := []*domain.AcceptanceCriterion{}
	for _, c := range f.criteria {
		if c.OwnerID == ownerID {
			cp := *c
			out = append(out, &cp)
		}
	}
	return out, nil
}

func (f *fakeStore) UpdateAcceptanceCriterion(_ context.Context, c *domain.AcceptanceCriterion) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	cp := *c
	f.criteria[c.ID] = &cp
	return nil
}

func (f *fakeStore) criterion(id string) domain.AcceptanceCriterion {
	f.mu.Lock()
	defer f.mu.Unlock()
	return *f.criteria[id]
}

func (f *fakeStore) GetTodoItem(_ context.Context, _ string) (*domain.TodoItem, error) {
	return &domain.TodoItem{ProjectID: "p1"}, nil
}
func (f *fakeStore) GetBugItem(_ context.Context, _ string) (*domain.BugItem, error) {
	return nil, errors.New("n/a")
}
func (f *fakeStore) GetUseCaseItem(_ context.Context, _ string) (*domain.UseCaseItem, error) {
	return nil, errors.New("n/a")
}
func (f *fakeStore) GetKnowledgeEntry(_ context.Context, _ string) (*domain.KnowledgeEntry, error) {
	return nil, errors.New("n/a")
}
func (f *fakeStore) GetScratchpadItem(_ context.Context, _ string) (*domain.ScratchpadItem, error) {
	return nil, errors.New("n/a")
}
func (f *fakeStore) GetScratchpad(_ context.Context, _ string) (*domain.Scratchpad, error) {
	return nil, errors.New("n/a")
}
func (f *fakeStore) GetProject(_ context.Context, _ string) (*domain.Project, error) {
	return &domain.Project{RepoRoot: f.repoRoot}, nil
}
func (f *fakeStore) GetProjectSetting(_ context.Context, _, key string) (*domain.ProjectSetting, error) {
	if key == AIReviewSettingKey {
		if !f.aiReview {
			return nil, errors.New("not found")
		}
		v, _ := json.Marshal(true)
		return &domain.ProjectSetting{Key: key, Value: v}, nil
	}
	cmd, ok := f.commands[key]
	if !ok || cmd == "" {
		return nil, errors.New("not found")
	}
	v, _ := json.Marshal(cmd)
	return &domain.ProjectSetting{Key: key, Value: v}, nil
}

func (f *fakeStore) reviewResult(ownerID string) (domain.VerificationResult, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, r := range f.results {
		if r.OwnerID == ownerID && r.Layer == domain.VerifyLayerAIReview {
			return r, true
		}
	}
	return domain.VerificationResult{}, false
}

// fakeReviewer is a deterministic stand-in for the adversarial AI reviewer that
// records the model it was asked to run on.
type fakeReviewer struct {
	refute bool
	err    error

	mu       sync.Mutex
	gotModel string
}

func (r *fakeReviewer) Review(_ context.Context, in ReviewInput) (ReviewVerdict, error) {
	r.mu.Lock()
	r.gotModel = in.Model
	r.mu.Unlock()
	if r.err != nil {
		return ReviewVerdict{}, r.err
	}
	return ReviewVerdict{Refuted: r.refute, Explanation: "stub explanation"}, nil
}

func (r *fakeReviewer) model() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.gotModel
}
func (f *fakeStore) CreateVerificationResult(_ context.Context, v *domain.VerificationResult) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.results[v.ID] = *v
	return nil
}
func (f *fakeStore) UpdateVerificationResult(_ context.Context, v *domain.VerificationResult) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.results[v.ID] = *v
	return nil
}
func (f *fakeStore) get(id string) domain.VerificationResult {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.results[id]
}

// waitVerdict polls the fake store until the row leaves "running" or times out.
func waitVerdict(t *testing.T, f *fakeStore, id string) domain.VerificationResult {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if r := f.get(id); r.Verdict != "" && r.Verdict != domain.VerifyVerdictRunning {
			return r
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("verification %s did not resolve", id)
	return domain.VerificationResult{}
}

func TestServiceTriggerPass(t *testing.T) {
	root, sha := initRepo(t)
	svc := NewService(newFakeStore(root, "exit 0"), nil)
	fs := svc.store.(*fakeStore)

	rows, err := svc.Trigger(context.Background(), "todo_item", "t1", sha)
	if err != nil {
		t.Fatalf("trigger: %v", err)
	}
	if len(rows) != 1 || rows[0].Verdict != domain.VerifyVerdictRunning {
		t.Fatalf("want 1 running row, got %+v", rows)
	}
	got := waitVerdict(t, fs, rows[0].ID)
	if got.Verdict != domain.VerifyVerdictPass {
		t.Errorf("final verdict = %q, want pass (%+v)", got.Verdict, got)
	}
	if got.CommitSHA != sha || got.CheckName != "exit 0" {
		t.Errorf("metadata not persisted: %+v", got)
	}
}

func TestServiceTriggerAdvancesCriteria(t *testing.T) {
	root, sha := initRepo(t)
	fs := newFakeStore(root, "exit 0")
	fs.criteria["okc"] = &domain.AcceptanceCriterion{
		ID: "okc", OwnerType: "todo_item", OwnerID: "t1", State: "accepted", TestCommand: "exit 0",
	}
	fs.criteria["badc"] = &domain.AcceptanceCriterion{
		ID: "badc", OwnerType: "todo_item", OwnerID: "t1", State: "accepted", TestCommand: "exit 1",
	}
	// Not ratified — a passing command must NOT advance it.
	fs.criteria["propc"] = &domain.AcceptanceCriterion{
		ID: "propc", OwnerType: "todo_item", OwnerID: "t1", State: "proposed", TestCommand: "exit 0",
	}
	svc := NewService(fs, nil)

	rows, err := svc.Trigger(context.Background(), "todo_item", "t1", sha)
	if err != nil {
		t.Fatalf("trigger: %v", err)
	}
	waitVerdict(t, fs, rows[0].ID) // item-level done implies the batch ran

	// Criteria advance after the item row; poll until settled.
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if fs.criterion("okc").State == "satisfied" && fs.criterion("badc").State == "failed" {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if got := fs.criterion("okc"); got.State != "satisfied" || got.SatisfiedBy != sha {
		t.Errorf("okc = %+v, want satisfied + satisfied_by=%s", got, sha)
	}
	if got := fs.criterion("badc"); got.State != "failed" || got.SatisfiedBy != "" {
		t.Errorf("badc = %+v, want failed + empty satisfied_by", got)
	}
	if got := fs.criterion("propc"); got.State != "proposed" {
		t.Errorf("propc advanced to %q — proposed criteria must not be touched", got.State)
	}
}

// Test + scanners each run as their own kind in one batch.
func TestServiceTriggerScanners(t *testing.T) {
	root, sha := initRepo(t)
	fs := newFakeStore(root, "exit 0").
		withCheck("verify.lint_command", "exit 0").
		withCheck("verify.sast_command", "exit 1")
	svc := NewService(fs, nil)

	rows, err := svc.Trigger(context.Background(), "todo_item", "t1", sha)
	if err != nil {
		t.Fatalf("trigger: %v", err)
	}
	if len(rows) != 3 {
		t.Fatalf("want 3 checks (test+lint+sast), got %d", len(rows))
	}
	byKind := map[string]domain.VerificationResult{}
	for _, r := range rows {
		byKind[r.Kind] = waitVerdict(t, fs, r.ID)
	}
	if byKind["test"].Verdict != domain.VerifyVerdictPass {
		t.Errorf("test verdict = %q, want pass", byKind["test"].Verdict)
	}
	if byKind["lint"].Verdict != domain.VerifyVerdictPass {
		t.Errorf("lint verdict = %q, want pass", byKind["lint"].Verdict)
	}
	if byKind["sast"].Verdict != domain.VerifyVerdictFail {
		t.Errorf("sast verdict = %q, want fail", byKind["sast"].Verdict)
	}
}

// AI review records an advisory ai-review verdict per ratified criterion and
// never moves criterion state.
func TestServiceTriggerAIReview(t *testing.T) {
	root, sha := initRepo(t)
	fs := newFakeStore(root, "exit 0")
	fs.aiReview = true
	fs.criteria["okc"] = &domain.AcceptanceCriterion{
		ID: "okc", OwnerType: "todo_item", OwnerID: "t1", State: "accepted", // no test command
	}
	fs.commands[ReviewerModelSettingKey] = "qwen2.5-coder:32b" // project-selected reviewer model
	reviewer := &fakeReviewer{refute: true}
	svc := NewService(fs, nil).WithReviewer(reviewer)

	rows, err := svc.Trigger(context.Background(), "todo_item", "t1", sha)
	if err != nil {
		t.Fatalf("trigger: %v", err)
	}
	waitVerdict(t, fs, rows[0].ID) // item-level done; AI review runs after

	deadline := time.Now().Add(10 * time.Second)
	var rev domain.VerificationResult
	for time.Now().Before(deadline) {
		if r, ok := fs.reviewResult("okc"); ok && r.Verdict != domain.VerifyVerdictRunning {
			rev = r
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if rev.ID == "" {
		t.Fatal("no ai-review result recorded for criterion")
	}
	if rev.Layer != domain.VerifyLayerAIReview || rev.Verdict != domain.VerifyVerdictFail {
		t.Errorf("review = %+v, want layer ai-review + verdict fail (refuted)", rev)
	}
	if rev.Output != "stub explanation" {
		t.Errorf("explanation not persisted: %q", rev.Output)
	}
	// Advisory only — criterion state must be untouched.
	if got := fs.criterion("okc"); got.State != "accepted" {
		t.Errorf("AI review moved criterion state to %q — must stay accepted", got.State)
	}
	// The project-selected reviewer model is threaded to the reviewer.
	if reviewer.model() != "qwen2.5-coder:32b" {
		t.Errorf("reviewer model = %q, want qwen2.5-coder:32b", reviewer.model())
	}
}

func TestServiceTriggerNoCommand(t *testing.T) {
	root, sha := initRepo(t)
	svc := NewService(newFakeStore(root, ""), nil)
	if _, err := svc.Trigger(context.Background(), "todo_item", "t1", sha); !errors.Is(err, ErrNoCommand) {
		t.Fatalf("err = %v, want ErrNoCommand", err)
	}
}

func TestServiceTriggerNoRepo(t *testing.T) {
	svc := NewService(newFakeStore("", "exit 0"), nil)
	if _, err := svc.Trigger(context.Background(), "todo_item", "t1", "abc1234"); !errors.Is(err, ErrNoRepo) {
		t.Fatalf("err = %v, want ErrNoRepo", err)
	}
}

func (f *fakeStore) RecordItemEvent(_ context.Context, _ *domain.ItemEvent) error { return nil }

func (f *fakeStore) FailStaleRunningVerifications(_ context.Context, now time.Time) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for id, r := range f.results {
		if r.Verdict == domain.VerifyVerdictRunning {
			r.Verdict = domain.VerifyVerdictError
			r.Summary = "interrupted"
			r.UpdatedAt = now
			f.results[id] = r
			n++
		}
	}
	return n, nil
}
