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

// Job types. Open vocabulary: adding one must not need a migration.
const (
	JobDecompose = "decompose"  // a document into proposed work items
	JobArchDraft = "arch_draft" // characterise architecture components
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
}

// Active reports whether the job is still doing work.
// Active reports whether the job is still doing work. A paused job is not
// active — nothing is running — but it is not finished either, which is why
// Resumable exists separately.
func (j *Job) Active() bool { return j.Status == JobRunning }

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
	case JobRunning, JobSucceeded, JobFailed, JobCancelled, JobInterrupted, JobPaused:
		return true
	}
	return false
}
