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

package jobqueue

import (
	"testing"
	"time"

	"codedistill/internal/domain"
)

func TestTimeline(t *testing.T) {
	at := func(min int) time.Time { return t0.Add(time.Duration(min) * time.Minute) }
	ptr := func(x time.Time) *time.Time { return &x }
	jobs := []*domain.Job{
		// Waited 0–10, ran 10–30.
		{ID: "a", Status: domain.JobSucceeded, QueuedAt: ptr(at(0)), StartedAt: at(10), FinishedAt: ptr(at(30))},
		// Started at once (before the queue), ran 5–50, still running.
		{ID: "b", Status: domain.JobRunning, StartedAt: at(5)},
		// Waiting since 20.
		{ID: "c", Status: domain.JobQueued, QueuedAt: ptr(at(20)), StartedAt: at(20)},
		// A one-minute job inside the third slice: the peak must show it.
		{ID: "d", Status: domain.JobFailed, StartedAt: at(41), FinishedAt: ptr(at(42))},
	}
	got := Timeline(jobs, at(0), at(60), 6) // ten-minute slices
	want := []struct{ run, q int }{
		{1, 1}, // 0–10: b runs from 5; a waits
		{2, 0}, // 10–20: a and b run
		{2, 1}, // 20–30: a and b run; c waits
		{1, 1}, // 30–40: b
		{2, 1}, // 40–50: b, and d for a minute
		{1, 1}, // 50–60
	}
	for i, w := range want {
		if got[i].Running != w.run || got[i].Queued != w.q {
			t.Errorf("slice %d (%s) = %d running, %d waiting; want %d, %d",
				i, got[i].At.Format("15:04"), got[i].Running, got[i].Queued, w.run, w.q)
		}
	}
	if Timeline(jobs, at(10), at(0), 6) != nil || Timeline(jobs, at(0), at(10), 0) != nil {
		t.Error("an empty window drew something")
	}
}
