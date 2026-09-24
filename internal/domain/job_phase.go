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

// JobPhase is one stage of a run, with its own timing.
//
// jobs.phase holds only the CURRENT phase and overwrites it, which answers
// "what is it doing" and destroys "what did it do". A decomposition spends
// most of its time reading and a fraction sorting; without these rows a
// finished run cannot say so.
type JobPhase struct {
	ID      string `json:"id"`
	JobID   string `json:"job_id"`
	Ordinal int    `json:"ordinal"`
	Phase   string `json:"phase"`

	// WorkerType matters once a workflow has interacting agents: a solutioner
	// and a challenger take turns, and knowing which one spent the time is the
	// useful shape.
	WorkerType string `json:"worker_type,omitempty"`

	// Round counts the exchange between interacting agents. 0 for a
	// single-pass phase.
	Round int `json:"round,omitempty"`

	Done      int `json:"done"`
	Total     int `json:"total"`
	TokensIn  int `json:"tokens_in"`
	TokensOut int `json:"tokens_out"`

	Error      string     `json:"error,omitempty"`
	StartedAt  time.Time  `json:"started_at"`
	FinishedAt *time.Time `json:"finished_at,omitempty"`
}

// Running reports whether this phase is still going.
func (p *JobPhase) Running() bool { return p.FinishedAt == nil }

// Seconds is how long the phase took, or has taken so far.
func (p *JobPhase) Seconds(now time.Time) int {
	end := now
	if p.FinishedAt != nil {
		end = *p.FinishedAt
	}
	d := int(end.Sub(p.StartedAt).Seconds())
	if d < 0 {
		return 0
	}
	return d
}

// PhaseNorm is what a phase usually costs, across past runs of a workflow.
//
// It exists because elapsed time alone carries no verdict: "12 minutes" means
// nothing until you know the usual run is 34. The median rather than the mean,
// because one 46-minute outlier should not move the bar a user judges against.
type PhaseNorm struct {
	Phase     string `json:"phase"`
	MedianSec int    `json:"median_sec"`
	Runs      int    `json:"runs"`
}
