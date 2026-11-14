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
	"errors"
	"net/http"
	"strings"
	"time"

	"codedistill/internal/domain"
	"codedistill/internal/events"
	codegit "codedistill/internal/git"
	"codedistill/internal/id"
	"codedistill/internal/storage"
)

// Reviews — the review process on a changeset (docs/design/code-review.md,
// slice 5). A review opens on request; approvals and change requests are
// review_decisions rows under owner_type "review"; the status is derived from
// the latest decision of each reviewer. When a review becomes approved, every
// item anchored to a commit it covers gets an item-level approval, so the
// trust dial and the existing human-floor gate see it the way they see any
// sign-off; while it is open, the governance review policy at gate or above
// blocks completing those items.

const ownerReview = "review"

type reviewDecisionView struct {
	domain.ReviewDecision
	ReviewerName string `json:"reviewer_name,omitempty"`
	// Counts is false for the author's own approval in a multi-user workspace,
	// and for a decision the same reviewer later replaced (Superseded).
	Counts     bool `json:"counts"`
	Superseded bool `json:"superseded"`
}

type reviewView struct {
	Review    domain.Review        `json:"review"`
	Changeset *domain.Changeset    `json:"changeset,omitempty"`
	Decisions []reviewDecisionView `json:"decisions"`
	Approvals int                  `json:"approvals"`
	// IsAuthor: the acting user authored the head commit (multi-user only).
	IsAuthor bool `json:"is_author"`
	// Mine: the acting user's latest decision, if any.
	Mine string `json:"mine,omitempty"`
	// Comments: unresolved review comments on the changeset.
	OpenComments int    `json:"open_comments"`
	Label        string `json:"label"`
}

func (s *Server) multiUser() bool { return s.auth != nil }

// userEmail resolves a user id's email for the author exclusion; "" when unknown.
func (s *Server) userEmail(r *http.Request, uid string) string {
	if u, err := s.store.GetUser(r.Context(), uid); err == nil {
		return strings.ToLower(strings.TrimSpace(u.Email))
	}
	return ""
}

// reviewOutcome reduces decisions to the review's live status: each
// reviewer's latest decision counts once; in a multi-user workspace the
// author's approval does not count. Any standing rejection means changes
// are requested; enough counted approvals means approved; otherwise open.
func reviewOutcome(rv *domain.Review, decisions []domain.ReviewDecision, multiUser bool, emailOf func(string) string) (status string, approvals int, counts map[string]bool) {
	counts = map[string]bool{}
	latest := map[string]domain.ReviewDecision{}
	for _, d := range decisions { // newest first
		if _, seen := latest[d.Reviewer]; !seen {
			latest[d.Reviewer] = d
		}
	}
	rejected := false
	for who, d := range latest {
		c := true
		if multiUser && rv.AuthorEmail != "" && emailOf(who) == strings.ToLower(rv.AuthorEmail) {
			c = false
		}
		counts[d.ID] = c
		if !c {
			continue
		}
		switch d.Decision {
		case "rejected":
			rejected = true
		case "approved":
			approvals++
		}
	}
	switch {
	case rejected:
		return domain.ReviewChangesRequested, approvals, counts
	case approvals >= rv.RequiredApprovals:
		return domain.ReviewApproved, approvals, counts
	default:
		return domain.ReviewOpen, approvals, counts
	}
}

func (s *Server) buildReviewView(r *http.Request, rv *domain.Review, cs *domain.Changeset) (*reviewView, error) {
	ds, err := s.store.ListReviewDecisions(r.Context(), ownerReview, rv.ID)
	if err != nil {
		return nil, err
	}
	emails := map[string]string{}
	emailOf := func(uid string) string {
		if e, ok := emails[uid]; ok {
			return e
		}
		e := s.userEmail(r, uid)
		emails[uid] = e
		return e
	}
	_, approvals, counts := reviewOutcome(rv, ds, s.multiUser(), emailOf)
	me := s.currentUser(r)
	v := &reviewView{Review: *rv, Changeset: cs, Decisions: []reviewDecisionView{}, Approvals: approvals}
	for _, d := range ds {
		name := d.Reviewer
		if u, err := s.store.GetUser(r.Context(), d.Reviewer); err == nil && u.DisplayName != "" {
			name = u.DisplayName
		}
		if d.Reviewer == me && v.Mine == "" {
			v.Mine = d.Decision
		}
		c, latest := counts[d.ID]
		v.Decisions = append(v.Decisions, reviewDecisionView{ReviewDecision: d, ReviewerName: name, Counts: c, Superseded: !latest})
	}
	v.IsAuthor = s.multiUser() && rv.AuthorEmail != "" && emailOf(me) == strings.ToLower(rv.AuthorEmail)
	if cs != nil {
		v.Label = changesetLabel(cs)
		if comments, err := s.store.ListReviewComments(r.Context(), cs.ID); err == nil {
			for _, c := range comments {
				if c.ResolvedAt == nil {
					v.OpenComments++
				}
			}
		}
	}
	return v, nil
}

// openReview: POST /projects/{pid}/changesets/{cid}/review — request a review.
// 201 when opened, 200 when one already exists.
func (s *Server) openReview(w http.ResponseWriter, r *http.Request) {
	cs, ok := s.changesetOf(w, r)
	if !ok {
		return
	}
	if rv, err := s.store.GetReviewByChangeset(r.Context(), cs.ID); err == nil {
		v, err := s.buildReviewView(r, rv, cs)
		if err != nil {
			writeErr(w, http.StatusInternalServerError, err)
			return
		}
		writeJSON(w, http.StatusOK, v)
		return
	}
	repo, ok := s.openProjectRepo(w, r)
	if !ok {
		return
	}
	// What the review covers, and who wrote it. Captured now so the gate is a
	// join and the author exclusion needs no git at decision time.
	author := ""
	shas := []string{}
	if cs.HeadSHA != codegit.Working {
		commits, err := repo.CommitsBetween(cs.BaseSHA, cs.HeadSHA, 2000)
		if err != nil {
			writeErr(w, gitStatus(err), err)
			return
		}
		for _, c := range commits {
			shas = append(shas, c.SHA)
		}
		if len(commits) > 0 {
			author = commits[0].Email
		}
	} else {
		// The working copy has no commit author: whoever asked is the author.
		author = s.userEmail(r, s.currentUser(r))
	}
	required := 1
	if s.multiUser() {
		required = 2
	}
	now := time.Now().UTC()
	rv := &domain.Review{
		ID: id.New(), ChangesetID: cs.ID, ProjectID: cs.ProjectID, Status: domain.ReviewOpen,
		RequiredApprovals: required, AuthorEmail: strings.ToLower(author), CreatedAt: now, UpdatedAt: now,
	}
	if err := s.store.CreateReview(r.Context(), rv); err != nil {
		writeErr(w, statusFor(err), err)
		return
	}
	if err := s.store.SetChangesetCommits(r.Context(), cs.ID, shas); err != nil {
		writeErr(w, statusFor(err), err)
		return
	}
	v, err := s.buildReviewView(r, rv, cs)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusCreated, v)
}

// getChangesetReview: GET /projects/{pid}/changesets/{cid}/review — 404 when
// no review has been requested.
func (s *Server) getChangesetReview(w http.ResponseWriter, r *http.Request) {
	cs, ok := s.changesetOf(w, r)
	if !ok {
		return
	}
	rv, err := s.store.GetReviewByChangeset(r.Context(), cs.ID)
	if err != nil {
		writeErr(w, statusFor(err), err)
		return
	}
	v, err := s.buildReviewView(r, rv, cs)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, v)
}

func (s *Server) reviewOf(w http.ResponseWriter, r *http.Request) (*domain.Review, *domain.Changeset, bool) {
	rv, err := s.store.GetReview(r.Context(), r.PathValue("rid"))
	if err != nil {
		writeErr(w, statusFor(err), err)
		return nil, nil, false
	}
	if rv.ProjectID != r.PathValue("pid") {
		writeMsg(w, http.StatusNotFound, "review not found in this project")
		return nil, nil, false
	}
	cs, err := s.store.GetChangeset(r.Context(), rv.ChangesetID)
	if err != nil {
		writeErr(w, statusFor(err), err)
		return nil, nil, false
	}
	return rv, cs, true
}

// decideReview: POST /projects/{pid}/reviews/{rid}/decide {decision, note}.
func (s *Server) decideReview(w http.ResponseWriter, r *http.Request) {
	rv, cs, ok := s.reviewOf(w, r)
	if !ok {
		return
	}
	if rv.Status == domain.ReviewMerged || rv.Status == domain.ReviewAbandoned {
		writeMsg(w, http.StatusConflict, "review is "+rv.Status)
		return
	}
	var req struct {
		Decision string `json:"decision"`
		Note     string `json:"note"`
	}
	if err := readJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	decision := strings.TrimSpace(req.Decision)
	if decision != "approved" && decision != "rejected" {
		writeMsg(w, http.StatusBadRequest, `decision must be "approved" or "rejected"`)
		return
	}
	ctx := r.Context()
	me := s.currentUser(r)
	now := time.Now().UTC()
	d := &domain.ReviewDecision{
		ID: id.New(), OwnerType: ownerReview, OwnerID: rv.ID, Decision: decision,
		CommitSHA: cs.HeadSHA, Reviewer: me, Note: strings.TrimSpace(req.Note), CreatedAt: now,
	}
	if cs.HeadSHA == codegit.Working {
		d.CommitSHA = ""
	}
	if err := s.store.CreateReviewDecision(ctx, d); err != nil {
		writeErr(w, statusFor(err), err)
		return
	}
	ds, err := s.store.ListReviewDecisions(ctx, ownerReview, rv.ID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	was := rv.Status
	status, _, _ := reviewOutcome(rv, ds, s.multiUser(), func(uid string) string { return s.userEmail(r, uid) })
	if status != was {
		rv.Status = status
		rv.UpdatedAt = now
		if err := s.store.UpdateReview(ctx, rv); err != nil {
			writeErr(w, statusFor(err), err)
			return
		}
		if status == domain.ReviewApproved {
			s.propagateApproval(r, rv, cs, d)
		}
	}
	s.bus.Publish(events.ItemsChanged)
	v, err := s.buildReviewView(r, rv, cs)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusCreated, v)
}

// propagateApproval records an item-level approval on every item anchored to
// a commit the changeset covers — "a human read the code" is what the item
// review was standing in for — so the trust dial counts it and the
// human-floor gate opens. Items already approved for this head are skipped.
func (s *Server) propagateApproval(r *http.Request, rv *domain.Review, cs *domain.Changeset, by *domain.ReviewDecision) {
	ctx := r.Context()
	shas, err := s.store.ListChangesetCommits(ctx, cs.ID)
	if err != nil || len(shas) == 0 {
		return
	}
	refs, err := s.store.ItemsByCommits(ctx, cs.ProjectID, shas)
	if err != nil {
		return
	}
	label := changesetLabel(cs)
	for _, ref := range refs {
		if prev, err := s.store.LatestReviewDecision(ctx, ref.OwnerType, ref.OwnerID); err == nil &&
			prev.Decision == "approved" && prev.CommitSHA == cs.HeadSHA {
			continue
		}
		now := time.Now().UTC()
		d := &domain.ReviewDecision{
			ID: id.New(), OwnerType: ref.OwnerType, OwnerID: ref.OwnerID, Decision: "approved",
			CommitSHA: cs.HeadSHA, Reviewer: by.Reviewer, Note: "via code review of " + label, CreatedAt: now,
		}
		if err := s.store.CreateReviewDecision(ctx, d); err != nil {
			s.log.Warn("review: propagate approval failed", "owner", ref.OwnerID, "err", err)
			continue
		}
		_ = s.store.ClearReviewFlags(ctx, ref.OwnerType, ref.OwnerID, now)
		s.recordEvent(ctx, ref.OwnerType, ref.OwnerID, "review", "Review: approved — code review of "+label, sourceUI)
	}
}

// setReviewStatus: POST /projects/{pid}/reviews/{rid}/status {status} —
// the by-hand transitions: merged (recorded, not performed) and abandoned
// from a live review; open to reopen an abandoned one.
func (s *Server) setReviewStatus(w http.ResponseWriter, r *http.Request) {
	rv, cs, ok := s.reviewOf(w, r)
	if !ok {
		return
	}
	var req struct {
		Status string `json:"status"`
	}
	if err := readJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	next := strings.TrimSpace(req.Status)
	live := rv.Status == domain.ReviewOpen || rv.Status == domain.ReviewChangesRequested || rv.Status == domain.ReviewApproved
	switch {
	case next == domain.ReviewMerged && rv.Status == domain.ReviewApproved:
	case next == domain.ReviewAbandoned && live:
	case next == domain.ReviewOpen && rv.Status == domain.ReviewAbandoned:
		// Reopening re-derives from the decisions on record.
		if ds, err := s.store.ListReviewDecisions(r.Context(), ownerReview, rv.ID); err == nil {
			next, _, _ = reviewOutcome(rv, ds, s.multiUser(), func(uid string) string { return s.userEmail(r, uid) })
		}
	default:
		writeMsg(w, http.StatusConflict, "cannot go from "+rv.Status+" to "+next)
		return
	}
	rv.Status = next
	rv.UpdatedAt = time.Now().UTC()
	if err := s.store.UpdateReview(r.Context(), rv); err != nil {
		writeErr(w, statusFor(err), err)
		return
	}
	s.bus.Publish(events.ItemsChanged)
	v, err := s.buildReviewView(r, rv, cs)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, v)
}

type reviewListResponse struct {
	Reviews []reviewView `json:"reviews"`
}

// listReviews: GET /projects/{pid}/reviews?live=1 — every review, or only the
// live ones (open, changes requested, approved but not yet merged), which is
// what Needs review shows.
func (s *Server) listReviews(w http.ResponseWriter, r *http.Request) {
	pid := r.PathValue("pid")
	if _, err := s.store.GetProject(r.Context(), pid); err != nil {
		writeErr(w, statusFor(err), err)
		return
	}
	var statuses []string
	if r.URL.Query().Get("live") == "1" {
		statuses = []string{domain.ReviewOpen, domain.ReviewChangesRequested, domain.ReviewApproved}
	}
	list, err := s.store.ListReviews(r.Context(), pid, statuses)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	out := reviewListResponse{Reviews: []reviewView{}}
	for i := range list {
		cs, err := s.store.GetChangeset(r.Context(), list[i].ChangesetID)
		if err != nil && !errors.Is(err, storage.ErrNotFound) {
			writeErr(w, http.StatusInternalServerError, err)
			return
		}
		v, err := s.buildReviewView(r, &list[i], cs)
		if err != nil {
			writeErr(w, http.StatusInternalServerError, err)
			return
		}
		out.Reviews = append(out.Reviews, *v)
	}
	writeJSON(w, http.StatusOK, out)
}
