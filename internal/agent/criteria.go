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

package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"codedistill/internal/domain"
)

// CriteriaDrafter drafts acceptance criteria (a "definition of done") for an
// item's intent. Backed by the same local-Ollama JSON generate the classifier
// uses, so it's a free local-compute assist. Optional on the Agent — nil means
// no AI drafting (criteria can still be hand-authored).
type CriteriaDrafter interface {
	Draft(ctx context.Context, in CriteriaInput) ([]string, error)
}

// CriteriaInput is the intent the drafter reasons about.
type CriteriaInput struct {
	Category        string // USE_CASE | BUG | TODO
	Subject         string
	Detail          string // the source paste / description
	Role, Want, Why string // use cases only
	// ProjectID resolves the per-project criteria model (model-provider arc).
	ProjectID string
}

// maxDraftCriteria caps how many the model may return (defensive trim).
const maxDraftCriteria = 6

const criteriaPrompt = `You are drafting ACCEPTANCE CRITERIA — the testable "definition of done" — for one item of work. Each criterion is a single, observable, independently-checkable statement of what must be TRUE for this to be considered done. Write what a reviewer or a test could verify, not how to implement it.

Rules:
- 2 to 5 criteria. Fewer is fine for a small item; never pad.
- Each is one concrete, checkable outcome (not a list, not a paragraph).
- Prefer observable behavior over implementation detail.
- For a BUG, include that the reported defect no longer reproduces AND that the correct behavior holds.
- No numbering or bullet characters in the text.

Item type: %s
Subject: %s
%s
Details:
<<<
%s
>>>

Respond as strict JSON:
{"criteria": ["<criterion>", "<criterion>", ...]}`

func (d *OllamaCriteriaDrafter) Draft(ctx context.Context, in CriteriaInput) ([]string, error) {
	rww := ""
	if strings.TrimSpace(in.Role+in.Want+in.Why) != "" {
		rww = fmt.Sprintf("Role: %s\nWant: %s\nWhy: %s\n", in.Role, in.Want, in.Why)
	}
	prompt := fmt.Sprintf(criteriaPrompt, in.Category, in.Subject, rww, in.Detail)
	raw, err := d.gen(ctx, in.ProjectID, prompt)
	if err != nil {
		return nil, err
	}
	var res struct {
		Criteria []string `json:"criteria"`
	}
	if err := json.Unmarshal([]byte(raw), &res); err != nil {
		return nil, fmt.Errorf("parse criteria: %w (raw: %q)", err, raw)
	}
	out := make([]string, 0, len(res.Criteria))
	seen := map[string]bool{}
	for _, c := range res.Criteria {
		c = strings.TrimSpace(c)
		if c == "" || seen[strings.ToLower(c)] {
			continue
		}
		seen[strings.ToLower(c)] = true
		out = append(out, c)
		if len(out) >= maxDraftCriteria {
			break
		}
	}
	return out, nil
}

// OllamaCriteriaDrafter is the production drafter, backed by a JSON-mode Ollama
// generate call. Like OllamaClassifier, it resolves a per-project model when
// GenerateModel + ModelFor are wired, else falls back to the model-less Generate.
type OllamaCriteriaDrafter struct {
	Generate      GenerateJSONFunc
	GenerateModel GenerateJSONModelFunc
	ModelFor      ModelForFunc
}

func (d *OllamaCriteriaDrafter) gen(ctx context.Context, projectID, prompt string) (string, error) {
	if d.GenerateModel != nil {
		model := ""
		if d.ModelFor != nil {
			model = d.ModelFor(projectID)
		}
		return d.GenerateModel(ctx, model, prompt)
	}
	return d.Generate(ctx, prompt)
}

// autoDraftCriteria best-effort generates + persists AI-proposed criteria for a
// freshly-derived use_case/bug. No-op without a drafter or for categories we
// don't auto-draft (TODO is opt-in via the on-demand path). Failures are
// logged, never fatal — criteria are an enrichment, not part of the derive
// contract.
func (a *Agent) autoDraftCriteria(ctx context.Context, projectID, category, ownerType, ownerID, detail string, result Classification) {
	if a.criteria == nil || (category != "USE_CASE" && category != "BUG") {
		return
	}
	in := CriteriaInput{
		Category: category,
		Subject:  firstLineOrTruncate(detail, 250),
		Detail:   detail,
		Role:     result.Role, Want: result.Want, Why: result.Why,
		ProjectID: projectID,
	}
	texts, err := a.criteria.Draft(ctx, in)
	if err != nil {
		a.log.Warn("criteria: auto-draft failed", "owner", ownerID, "err", err)
		return
	}
	a.persistCriteria(ctx, ownerType, ownerID, 0, texts)
}

// DraftCriteriaForItem generates + persists AI-proposed criteria for an
// existing derived item (the "Draft with AI" action). Appends after any
// existing criteria and returns the new rows. Errors when no drafter is wired.
func (a *Agent) DraftCriteriaForItem(ctx context.Context, ownerType, ownerID string) ([]*domain.AcceptanceCriterion, error) {
	if a.criteria == nil {
		return nil, fmt.Errorf("AI drafting is unavailable (no model configured)")
	}
	in, err := a.criteriaInputFor(ctx, ownerType, ownerID)
	if err != nil {
		return nil, err
	}
	texts, err := a.criteria.Draft(ctx, in)
	if err != nil {
		return nil, err
	}
	base, err := a.store.NextAcceptanceCriterionPosition(ctx, ownerType, ownerID)
	if err != nil {
		return nil, err
	}
	return a.persistCriteria(ctx, ownerType, ownerID, base, texts), nil
}

func (a *Agent) persistCriteria(ctx context.Context, ownerType, ownerID string, basePos int, texts []string) []*domain.AcceptanceCriterion {
	now := a.now()
	out := []*domain.AcceptanceCriterion{}
	for i, t := range texts {
		t = strings.TrimSpace(t)
		if t == "" {
			continue
		}
		c := &domain.AcceptanceCriterion{
			ID: a.newID(), OwnerType: ownerType, OwnerID: ownerID,
			Position: basePos + i, Text: t, VerificationKind: "unspecified",
			State: "proposed", Provenance: "ai-proposed",
			CreatedAt: now, UpdatedAt: now,
		}
		if err := a.store.CreateAcceptanceCriterion(ctx, c); err != nil {
			a.log.Warn("criteria: persist failed", "owner", ownerID, "err", err)
			continue
		}
		out = append(out, c)
	}
	return out
}

// criteriaInputFor assembles the drafter input from a derived item, pulling the
// original source paste for richer detail when available.
func (a *Agent) criteriaInputFor(ctx context.Context, ownerType, ownerID string) (CriteriaInput, error) {
	in := CriteriaInput{}
	var sourceID string
	switch ownerType {
	case "use_case_item":
		uc, err := a.store.GetUseCaseItem(ctx, ownerID)
		if err != nil {
			return in, err
		}
		in.Category, in.Subject, in.Detail = "USE_CASE", uc.Subject, uc.Description
		in.Role, in.Want, in.Why = uc.Role, uc.Want, uc.Why
		in.ProjectID = uc.ProjectID
		sourceID = uc.SourceItemID
	case "bug_item":
		b, err := a.store.GetBugItem(ctx, ownerID)
		if err != nil {
			return in, err
		}
		in.Category, in.Subject = "BUG", b.Subject
		in.Detail = strings.TrimSpace(strings.Join([]string{b.ExpectedBehavior, b.ActualBehavior}, "\n"))
		in.ProjectID = b.ProjectID
		sourceID = b.SourceItemID
	case "todo_item":
		t, err := a.store.GetTodoItem(ctx, ownerID)
		if err != nil {
			return in, err
		}
		in.Category, in.Subject, in.Detail = "TODO", t.Subject, ""
		in.ProjectID = t.ProjectID
		sourceID = t.SourceItemID
	default:
		return in, fmt.Errorf("acceptance criteria not supported for owner_type %q", ownerType)
	}
	// Fall back to / enrich with the original paste.
	if strings.TrimSpace(in.Detail) == "" && sourceID != "" {
		if src, err := a.store.GetScratchpadItem(ctx, sourceID); err == nil {
			in.Detail = src.Content
		}
	}
	if strings.TrimSpace(in.Detail) == "" {
		in.Detail = in.Subject
	}
	return in, nil
}
