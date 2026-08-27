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
	"errors"
	"net/http"
	"strings"

	"codedistill/internal/domain"
	"codedistill/internal/git"
	"codedistill/internal/verify"
)

// Verification (glass-box Phase 3): the deterministic layer of the verification
// portfolio. The GET lists an item's verification results (the trust signal);
// the POST manually re-runs the project's configured check against the item's
// latest recorded commit. The auto-run path lives in MCP record_implementation.

// Verifier is the verification service the manual-trigger endpoint drives.
// Satisfied by *verify.Service; kept as an interface so the api package isn't
// bound to the concrete type. nil → the endpoint returns 503.
type Verifier interface {
	Trigger(ctx context.Context, ownerType, ownerID, commitSHA string) ([]*domain.VerificationResult, error)
}

type verificationResp struct {
	Results []*domain.VerificationResult `json:"results"`
	// Checks are the project's configured item-level checks (test + scanners),
	// each kind+command. Empty → the UI shows a "configure a check" empty state.
	Checks []verify.ConfiguredCheck `json:"checks"`
	// RepoConfigured reports whether the project has a repo to check out.
	RepoConfigured bool `json:"repo_configured"`
	// Available reports whether this server can run verification at all
	// (the verifier is wired). False on builds/modes without it.
	Available bool `json:"available"`
	// Criteria are the item's mapped acceptance criteria (those with a test
	// command), each with its latest per-criterion verdict (slice 2). Never nil.
	Criteria []criterionVerification `json:"criteria"`
}

// criterionVerification pairs a criterion with its latest deterministic verdict
// (Result, from its mapped test) and its latest adversarial AI-review verdict
// (Review) so the Verification tab can show both next to the item-level suite.
type criterionVerification struct {
	CriterionID string                     `json:"criterion_id"`
	Text        string                     `json:"text"`
	Command     string                     `json:"command"`
	State       string                     `json:"state"`
	Result      *domain.VerificationResult `json:"result"`
	Review      *domain.VerificationResult `json:"review"`
}

// ollamaModels lists the installed local models for the reviewer-model picker.
// A free read; returns an empty list (not an error) when no lister is wired or
// Ollama is unreachable, so the settings UI degrades to a plain text field.
func (s *Server) ollamaModels(w http.ResponseWriter, r *http.Request) {
	models := []string{}
	if s.models != nil {
		if got, err := s.models(r.Context()); err == nil {
			models = got
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"models": models})
}

// verificationForOwner factors over the owner-type verification list endpoints.
// Free read — no feature gate, mirroring code-metrics + acceptance-criteria.
func (s *Server) verificationForOwner(ownerType string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ownerID := r.PathValue("id")
		if err := s.ensureOwnerExists(r, ownerType, ownerID); err != nil {
			writeErr(w, statusFor(err), err)
			return
		}
		results, err := s.store.ListVerificationResults(r.Context(), ownerType, ownerID)
		if err != nil {
			writeErr(w, http.StatusInternalServerError, err)
			return
		}
		checks, repoConfigured := s.verificationContext(r.Context(), ownerType, ownerID)
		writeJSON(w, http.StatusOK, verificationResp{
			Results:        results,
			Checks:         checks,
			RepoConfigured: repoConfigured,
			Available:      s.verifier != nil,
			Criteria:       s.criterionVerifications(r.Context(), ownerType, ownerID),
		})
	}
}

// criterionVerifications returns the item's mapped criteria (those with a test
// command) each paired with its latest per-criterion verdict. Best-effort: a
// read error yields an empty list rather than failing the whole response.
func (s *Server) criterionVerifications(ctx context.Context, ownerType, ownerID string) []criterionVerification {
	out := []criterionVerification{}
	crits, err := s.store.ListAcceptanceCriteria(ctx, ownerType, ownerID)
	if err != nil {
		return out
	}
	for _, c := range crits {
		// Latest deterministic + latest ai-review result for this criterion.
		var test, review *domain.VerificationResult
		if rs, err := s.store.ListVerificationResults(ctx, verify.OwnerAcceptanceCriterion, c.ID); err == nil {
			for _, r := range rs { // newest-first
				if test == nil && r.Layer == domain.VerifyLayerDeterministic {
					test = r
				}
				if review == nil && r.Layer == domain.VerifyLayerAIReview {
					review = r
				}
			}
		}
		// Show a criterion that's either mapped to a test or has any verdict.
		if strings.TrimSpace(c.TestCommand) == "" && test == nil && review == nil {
			continue
		}
		out = append(out, criterionVerification{
			CriterionID: c.ID,
			Text:        c.Text,
			Command:     c.TestCommand,
			State:       c.State,
			Result:      test,
			Review:      review,
		})
	}
	return out
}

// triggerVerificationForOwner factors over the owner-type manual-run endpoints.
// Runs the project's configured check against the item's latest recorded commit
// and returns the "running" row (202) to poll. A write — gated by write-auth.
func (s *Server) triggerVerificationForOwner(ownerType string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ownerID := r.PathValue("id")
		if err := s.ensureOwnerExists(r, ownerType, ownerID); err != nil {
			writeErr(w, statusFor(err), err)
			return
		}
		if s.verifier == nil {
			writeErr(w, http.StatusServiceUnavailable, errors.New("verification is not available on this server"))
			return
		}
		commit, err := s.latestCommitAnchor(r.Context(), ownerType, ownerID)
		if err != nil {
			writeErr(w, http.StatusInternalServerError, err)
			return
		}
		if commit == "" {
			// No recorded implementation commit. Rather than hard-block (older
			// items predate the commit fields, and not everyone captures them),
			// allow an EXPLICIT run against the project's current HEAD when the
			// caller opts in with ?at=head. The result records HEAD as the
			// commit it tested against (VerificationResult.CommitSHA) — we do NOT
			// touch the item's implementation commit, which stays unrecorded.
			if r.URL.Query().Get("at") != "head" {
				writeErr(w, http.StatusBadRequest, errors.New("no recorded commit to verify — run against the current commit, or record an implementation first"))
				return
			}
			pid, perr := s.ownerProjectID(r.Context(), ownerType, ownerID)
			if perr != nil {
				writeErr(w, statusFor(perr), perr)
				return
			}
			head, herr := git.HeadSHAForProject(r.Context(), s.store, pid)
			if herr != nil || head == "" {
				writeErr(w, http.StatusBadRequest, errors.New("could not resolve the project's current commit (no repo, or an empty history)"))
				return
			}
			commit = head
		}
		rows, err := s.verifier.Trigger(r.Context(), ownerType, ownerID, commit)
		switch {
		case errors.Is(err, verify.ErrNoCommand):
			writeErr(w, http.StatusBadRequest, errors.New("configure at least one check command (e.g. verify.test_command) for this project first"))
		case errors.Is(err, verify.ErrNoRepo):
			writeErr(w, http.StatusBadRequest, errors.New("this project has no repo configured"))
		case errors.Is(err, verify.ErrAlreadyRunning):
			writeErr(w, http.StatusConflict, err)
		case err != nil:
			writeErr(w, http.StatusInternalServerError, err)
		default:
			writeJSON(w, http.StatusAccepted, rows)
		}
	}
}

// latestCommitAnchor returns the newest commit-kind anchor's revision for the
// item, or "" when none has been recorded.
func (s *Server) latestCommitAnchor(ctx context.Context, ownerType, ownerID string) (string, error) {
	anchors, err := s.store.ListCodeAnchors(ctx, ownerType, ownerID)
	if err != nil {
		return "", err
	}
	var best *domain.CodeAnchor
	for _, a := range anchors {
		if a.Kind != "commit" || a.Revision == "" {
			continue
		}
		if best == nil || a.CreatedAt.After(best.CreatedAt) {
			best = a
		}
	}
	if best == nil {
		return "", nil
	}
	return best.Revision, nil
}

// verificationContext resolves the project's configured item-level checks (test
// + scanners) and whether a repo is set, for the list response's empty state.
// Best-effort: a resolution error yields (nil, false) rather than failing the read.
func (s *Server) verificationContext(ctx context.Context, ownerType, ownerID string) (checks []verify.ConfiguredCheck, repoConfigured bool) {
	checks = []verify.ConfiguredCheck{}
	pid, err := s.ownerProjectID(ctx, ownerType, ownerID)
	if err != nil {
		return checks, false
	}
	if proj, err := s.store.GetProject(ctx, pid); err == nil {
		repoConfigured = proj.RepoRoot != ""
	}
	for _, spec := range verify.CheckSpecs() {
		st, err := s.store.GetProjectSetting(ctx, pid, spec.SettingKey)
		if err != nil {
			continue
		}
		var cmd string
		if json.Unmarshal(st.Value, &cmd) == nil && strings.TrimSpace(cmd) != "" {
			checks = append(checks, verify.ConfiguredCheck{Kind: spec.Kind, Command: strings.TrimSpace(cmd)})
		}
	}
	return checks, repoConfigured
}
