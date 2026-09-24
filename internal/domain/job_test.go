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

import (
	"testing"
	"time"
)

func TestPercentDistinguishesUnknownFromZero(t *testing.T) {
	// A phased job does not know its total until the first phase ends. If that
	// renders as 0% the user watches a dead bar for four minutes and assumes it
	// hung; -1 is the signal to render indeterminate instead.
	if got := (&Job{Total: 0, Done: 0}).Percent(); got != -1 {
		t.Errorf("unknown total gave %d%%, want -1", got)
	}
	if got := (&Job{Total: 31, Done: 0}).Percent(); got != 0 {
		t.Errorf("known total with no progress gave %d%%, want 0", got)
	}
	if got := (&Job{Total: 31, Done: 12}).Percent(); got != 38 {
		t.Errorf("12/31 gave %d%%, want 38", got)
	}
	// Total is revisable downward at a phase boundary; the bar must clamp
	// rather than report 140%.
	if got := (&Job{Total: 10, Done: 14}).Percent(); got != 100 {
		t.Errorf("overshoot gave %d%%, want 100", got)
	}
}

func TestETAMeasuresRatherThanAssumes(t *testing.T) {
	start := time.Date(2026, 9, 14, 12, 0, 0, 0, time.UTC)
	now := start.Add(4 * time.Minute) // 4 passes in 4 minutes = 60s each

	j := &Job{Status: JobRunning, StartedAt: start, Done: 4, Total: 10}
	if got := j.ETASeconds(now); got != 360 { // 6 remaining x 60s
		t.Errorf("ETA = %ds, want 360", got)
	}

	// Nothing to measure from, and nothing to estimate: all must be silent
	// rather than guess.
	for name, job := range map[string]*Job{
		"no progress yet": {Status: JobRunning, StartedAt: start, Done: 0, Total: 10},
		"total unknown":   {Status: JobRunning, StartedAt: start, Done: 3, Total: 0},
		"already done":    {Status: JobRunning, StartedAt: start, Done: 10, Total: 10},
		"not running":     {Status: JobSucceeded, StartedAt: start, Done: 4, Total: 10},
	} {
		if got := job.ETASeconds(now); got != 0 {
			t.Errorf("%s: ETA = %d, want 0", name, got)
		}
	}
}
