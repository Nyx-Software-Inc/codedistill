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

// Job statuses. "interrupted" is the one that only exists because jobs are
// persisted: a row left running when the process died. It is distinct from
// failed, because nothing went wrong with the work — the server went away —
// and a client may be able to resume it.
const (
	// JobQueued: waiting for a job slot (docs/design/job-queue.md). Nothing of
	// it has started, so there is nothing to clean up if it is cancelled.
	JobQueued      = "queued"
	JobRunning     = "running"
	JobSucceeded   = "succeeded"
	JobFailed      = "failed"
	JobCancelled   = "cancelled"
	JobInterrupted = "interrupted"

	// JobPaused: stopped by a human, and expected to continue. Distinct from
	// cancelled (abandoned) and interrupted (the process died). Only offered
	// for workflows that declare themselves resumable, because a Resume button
	// that quietly restarts from zero is worse than no button.
	//
	// Pausing is NOT instant. A model call is atomic — a ten-minute generate
	// cannot be interrupted — so a pause takes effect at the next checkpoint,
	// which for decomposition is the end of the current reading pass. The UI
	// says so rather than implying otherwise.
	JobPaused = "paused"
)

// Job priorities. The queue orders waiting jobs by priority, then by when they
// joined the line; a job that has waited long enough moves up a level, so a
// stream of high-priority jobs cannot starve a low one. Immediate starts at
// once (docs/design/job-queue.md, "Priority").
const (
	PriorityLow       = "low"
	PriorityMedium    = "medium"
	PriorityHigh      = "high"
	PriorityImmediate = "immediate"
)

// JobPriorityRank orders job priorities, higher first in line. An unknown value ranks
// as medium, the default.
func JobPriorityRank(p string) int {
	switch p {
	case PriorityLow:
		return 0
	case PriorityHigh:
		return 2
	case PriorityImmediate:
		return 3
	}
	return 1
}

// ValidJobPriority reports whether p is one of the priorities a job can have.
func ValidJobPriority(p string) bool {
	switch p {
	case PriorityLow, PriorityMedium, PriorityHigh, PriorityImmediate:
		return true
	}
	return false
}

// Job types. Open vocabulary: adding one must not need a migration.
const (
	JobDecompose = "decompose"  // a document into proposed work items
	JobArchDraft = "arch_draft" // characterise architecture components
	JobSequence  = "sequence"   // work out what has to happen before what
	// An outside agent fixes scanner findings on one branch, highest priority
	// first; CodeDistill verifies each fix (docs/design/fix-scanner-findings.md).
	JobFixFindings = "fix_findings"
)

// Job is one run of long work. It records the run, not the work: how a job
// resumes is the client's business, and its output lives in the client's own
// tables, reachable through ScopeKind/ScopeID.
type Job struct {
	ID        string `json:"id"`
	ProjectID string `json:"project_id"`
	Type      string `json:"type"`

	ScopeKind  string `json:"scope_kind,omitempty"`
	ScopeID    string `json:"scope_id,omitempty"`
	ScopeLabel string `json:"scope_label,omitempty"`

	Status string `json:"status"`

	// Phase names the current stage of a multi-stage job. Total is revisable
	// because a phased job cannot know the size of its second phase until the
	// first one ends.
	Phase string `json:"phase,omitempty"`
	Done  int    `json:"done"`
	Total int    `json:"total"` // 0 means not yet known

	Model     string `json:"model,omitempty"`
	TokensIn  int    `json:"tokens_in"`
	TokensOut int    `json:"tokens_out"`

	Error      string     `json:"error,omitempty"`
	CreatedBy  string     `json:"created_by,omitempty"`
	StartedAt  time.Time  `json:"started_at"`
	UpdatedAt  time.Time  `json:"updated_at"`
	FinishedAt *time.Time `json:"finished_at,omitempty"`

	// Branch is the git branch the job owns, for workflows that change code.
	// The Conductor cuts it from BaseSHA and hands its worktree to the agents;
	// every commit left on it is a verified result. Empty for jobs that write
	// nothing to the repository.
	Branch  string `json:"branch,omitempty"`
	BaseSHA string `json:"base_sha,omitempty"`

	// Priority orders the job in the queue: low, medium (the default), high
	// or immediate. QueuedAt is when it joined the line; nil for a job that
	// started straight away or ran before the queue existed. A queued job's
	// StartedAt is its submit time until it leaves the line.
	Priority string     `json:"priority"`
	QueuedAt *time.Time `json:"queued_at,omitempty"`
	// PausedBy says who stopped a paused or re-queued job: a person, the
	// queue (to make room for an Immediate job) or a restart. Empty once it
	// runs again.
	PausedBy string `json:"paused_by,omitempty"`
}

// Who paused a job.
const (
	PausedByPerson  = "person"  // stays paused until someone resumes it
	PausedByQueue   = "queue"   // made room for an Immediate job; resumes on its own
	PausedByRestart = "restart" // running when the server stopped; resumes on its own
)

// Active reports whether the job is still doing work.
// Active reports whether the job is still doing work. A paused job is not
// active — nothing is running — but it is not finished either, which is why
// Resumable exists separately.
func (j *Job) Active() bool { return j.Status == JobRunning }

// Pending reports whether the job is waiting or working: queued or running.
// What a one-run-at-a-time guard must count, or two runs for the same thing
// could both wait in line and then both start.
func (j *Job) Pending() bool { return j.Status == JobQueued || j.Status == JobRunning }

// Resumable reports whether this run can be continued. Paused and interrupted
// both qualify: one was stopped deliberately, the other by a process dying, and
// neither means the work was abandoned.
func (j *Job) Resumable() bool {
	return j.Status == JobPaused || j.Status == JobInterrupted
}

// ETASeconds estimates the remaining run from measured throughput so far,
// which is what architecture drafting already does and what makes a long job
// bearable. Returns 0 when there is nothing to estimate from: no total yet,
// nothing finished yet, or the job is not running.
//
// It deliberately measures elapsed-per-unit rather than assuming a fixed cost.
// On local hardware a component takes 35-45s and a document pass takes minutes;
// a hardcoded constant would be wrong for one of them and both after a model
// change.
func (j *Job) ETASeconds(now time.Time) int {
	if j.Status != JobRunning || j.Total <= 0 || j.Done <= 0 || j.Done >= j.Total {
		return 0
	}
	elapsed := now.Sub(j.StartedAt).Seconds()
	if elapsed <= 0 {
		return 0
	}
	return int(elapsed / float64(j.Done) * float64(j.Total-j.Done))
}

// Percent is progress for display, or -1 when the total is not yet known.
// Callers must render the -1 case as indeterminate rather than as zero: a bar
// pinned at 0% for four minutes reads as broken, and a bar that jumps because
// the denominator changed reads as a lie.
func (j *Job) Percent() int {
	if j.Total <= 0 {
		return -1
	}
	if j.Done >= j.Total {
		return 100
	}
	return j.Done * 100 / j.Total
}

// ValidJobStatus mirrors the CHECK constraint on jobs.status.
func ValidJobStatus(s string) bool {
	switch s {
	case JobQueued, JobRunning, JobSucceeded, JobFailed, JobCancelled, JobInterrupted, JobPaused:
		return true
	}
	return false
}
