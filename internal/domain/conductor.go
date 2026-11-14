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

package domain

import "time"

// Job item statuses: what happened to one thing a job worked through.
const (
	JobItemQueued  = "queued"
	JobItemRunning = "running"
	// Fixed: every check passed at the item's commit and the challenger had
	// nothing left. The commit stays on the job's branch.
	JobItemFixed = "fixed"
	// Waiting: attempted, not fixed, and handed to a person with the reason.
	// The attempt is set aside (AttemptRef) and the branch reset, so it cannot
	// get in the way of the next item.
	JobItemWaiting = "waiting"
	// Skipped: never attempted, because the item was out of scope (not from a
	// scanner, below the stop line). The reason says which.
	JobItemSkipped   = "skipped"
	JobItemCancelled = "cancelled"
)

// Phases of an item: the fixer's approach is reviewed (plan) before any code
// is written (build).
const (
	PhasePlan  = "plan"
	PhaseBuild = "build"
)

// How much an objection matters. Only blocking ones send work back; the rest
// are kept for the person.
const (
	SeverityBlocking = "blocking"
	SeverityMinor    = "minor"
	SeverityNote     = "note"
)

// What an objection is about: the change itself, or CodeDistill's own setup
// (a missing test command), which the fixer cannot act on.
const (
	AboutChange = "change"
	AboutSetup  = "setup"
)

// ValidJobItemStatus mirrors the CHECK constraint on job_items.status.
func ValidJobItemStatus(s string) bool {
	switch s {
	case JobItemQueued, JobItemRunning, JobItemFixed, JobItemWaiting, JobItemSkipped, JobItemCancelled:
		return true
	}
	return false
}

// JobItem is one item a job worked through, and what became of it.
//
// A job's row says how the run is going; this says what happened to each
// thing it touched. A scratchpad run is many outcomes under one job, and "the
// job succeeded" says nothing about which items were fixed and which are
// waiting on a person.
type JobItem struct {
	ID       string `json:"id"`
	JobID    string `json:"job_id"`
	Position int    `json:"position"`

	// The work item the outcome is recorded on, and the finding it came from.
	OwnerType string `json:"owner_type"`
	OwnerID   string `json:"owner_id"`
	FindingID string `json:"finding_id,omitempty"`
	Label     string `json:"label"`

	Status string `json:"status"`
	Reason string `json:"reason,omitempty"`

	CommitSHA  string `json:"commit_sha,omitempty"`
	AttemptRef string `json:"attempt_ref,omitempty"`
	Rounds     int    `json:"rounds"`
	Phase      string `json:"phase"`
	Checks     string `json:"checks,omitempty"` // JSON, see conductor.Checks

	ProgressNote   string     `json:"progress_note,omitempty"`
	LastProgressAt *time.Time `json:"last_progress_at,omitempty"`

	StartedAt  *time.Time `json:"started_at,omitempty"`
	FinishedAt *time.Time `json:"finished_at,omitempty"`
}

// Claim kinds.
const (
	ClaimFact       = "fact"       // about the code as it is
	ClaimAssumption = "assumption" // taken as true without checking
	ClaimDecision   = "decision"   // a choice the work made, and what it rests on
)

// Verdicts a challenger lens gives. Problem is a finding no claim covered.
const (
	VerdictHolds   = "holds"
	VerdictFalse   = "false"
	VerdictUnclear = "unclear"
	VerdictProblem = "problem"
)

// JobClaim is something an agent asserted while doing the work: precise
// enough that a challenger can rule on it.
type JobClaim struct {
	ID         string    `json:"id"`
	JobID      string    `json:"job_id"`
	JobItemID  string    `json:"job_item_id"`
	Round      int       `json:"round"`
	Phase      string    `json:"phase"`
	AuthorRole string    `json:"author_role"`
	Kind       string    `json:"kind"`
	Statement  string    `json:"statement"`
	Evidence   string    `json:"evidence,omitempty"`
	Falsifier  string    `json:"falsifier,omitempty"`
	CreatedAt  time.Time `json:"created_at"`
}

// ClaimVerdict is one lens's ruling on one claim, or (ClaimID empty, verdict
// problem) something it found that no claim covered.
type ClaimVerdict struct {
	ID        string `json:"id"`
	JobID     string `json:"job_id"`
	JobItemID string `json:"job_item_id"`
	Round     int    `json:"round"`
	Lens      string `json:"lens"`
	Phase     string `json:"phase"`
	ClaimID   string `json:"claim_id,omitempty"`
	Verdict   string `json:"verdict"`
	Severity  string `json:"severity"`
	About     string `json:"about"`
	Checked   string `json:"checked,omitempty"`
	Detail    string `json:"detail,omitempty"`
	// PointID is the point this verdict raises, when it raises one: the
	// reviewer cited it, or the Conductor matched it.
	PointID   string    `json:"point_id,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

// Kinds of point.
const (
	PointObjection = "objection" // raised as blocking, about the change, at least once
	PointNote      = "note"      // never blocking: for the person
)

// JobPoint is one thing the reviewers raised about an item, stored once
// however many times, by however many reviewers, it was raised. Each raising
// is a verdict carrying the point's id, so the count and who raised it come
// from the verdicts.
type JobPoint struct {
	ID        string    `json:"id"`
	JobID     string    `json:"job_id"`
	JobItemID string    `json:"job_item_id"`
	Kind      string    `json:"kind"`
	About     string    `json:"about"`
	Text      string    `json:"text"` // its first wording
	CreatedAt time.Time `json:"created_at"`
}

// JobTest is a test the TestBuilder wrote for an item, or an existing test it
// reviewed.
type JobTest struct {
	ID        string    `json:"id"`
	JobID     string    `json:"job_id"`
	JobItemID string    `json:"job_item_id"`
	Origin    string    `json:"origin"` // new | existing
	Name      string    `json:"name"`
	File      string    `json:"file,omitempty"`
	Purpose   string    `json:"purpose,omitempty"` // new: problem | guard
	Applies   *bool     `json:"applies,omitempty"` // existing: still applies?
	Criterion string    `json:"criterion,omitempty"`
	Reason    string    `json:"reason,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

// Test origins and purposes.
const (
	TestNew      = "new"
	TestExisting = "existing"
	TestProblem  = "problem" // must fail before the fix and pass after
	TestGuard    = "guard"   // must pass after: it guards what the fix must not break
)

// AgentRun is what one agent run consumed and produced. Pointers are what an
// agent may not report: nil is unknown, which is not zero.
type AgentRun struct {
	ID            string         `json:"id"`
	JobID         string         `json:"job_id"`
	JobItemID     string         `json:"job_item_id,omitempty"`
	Role          string         `json:"role"`
	Lens          string         `json:"lens,omitempty"`
	Round         int            `json:"round"`
	Phase         string         `json:"phase"`
	Agent         string         `json:"agent"`
	Model         string         `json:"model,omitempty"`
	Outcome       string         `json:"outcome"`
	ExitCode      *int           `json:"exit_code,omitempty"`
	StartedAt     time.Time      `json:"started_at"`
	DurationMS    int64          `json:"duration_ms"`
	SilentMS      int64          `json:"silent_ms"`
	Turns         *int           `json:"turns,omitempty"`
	TokensIn      *int64         `json:"tokens_in,omitempty"`
	TokensOut     *int64         `json:"tokens_out,omitempty"`
	CacheRead     *int64         `json:"cache_read,omitempty"`
	CacheWrite    *int64         `json:"cache_write,omitempty"`
	CostUSD       *float64       `json:"cost_usd,omitempty"`
	ToolCalls     map[string]int `json:"tool_calls,omitempty"`
	FilesChanged  *int           `json:"files_changed,omitempty"`
	LinesAdded    *int           `json:"lines_added,omitempty"`
	LinesDeleted  *int           `json:"lines_deleted,omitempty"`
	ClaimsMade    *int           `json:"claims_made,omitempty"`
	ClaimsRefuted *int           `json:"claims_refuted,omitempty"`
}
