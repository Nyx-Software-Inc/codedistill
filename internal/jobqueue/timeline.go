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
	"time"

	"codedistill/internal/domain"
)

// TimelinePoint is one slice of time: the most jobs running, and the most
// waiting, at any moment within it.
type TimelinePoint struct {
	At      time.Time `json:"at"` // the slice's start
	Running int       `json:"running"`
	Queued  int       `json:"queued"`
}

// Timeline rebuilds how many jobs were running and waiting over [from, to),
// in n equal slices, from the jobs' own times (docs/design/jobs-analytics.md).
//
// A slice reports the peak, not a sample at its edge: a job that ran for a
// minute inside a two-hour slice still shows. What the rows cannot say is a
// job's past pauses: a job paused and resumed is seen as waiting from when it
// first joined the line until its latest start, then running.
func Timeline(jobs []*domain.Job, from, to time.Time, n int) []TimelinePoint {
	if n < 1 || !to.After(from) {
		return nil
	}
	var running, queued []span
	for _, j := range jobs {
		end := to
		if j.FinishedAt != nil {
			end = *j.FinishedAt
		}
		switch j.Status {
		case domain.JobQueued:
			if j.QueuedAt != nil {
				queued = append(queued, span{*j.QueuedAt, to})
			}
		default:
			if j.QueuedAt != nil && j.StartedAt.After(*j.QueuedAt) {
				queued = append(queued, span{*j.QueuedAt, j.StartedAt})
			}
			if j.Status == domain.JobRunning || j.FinishedAt != nil {
				running = append(running, span{j.StartedAt, end})
			}
		}
	}
	step := to.Sub(from) / time.Duration(n)
	out := make([]TimelinePoint, n)
	for i := range out {
		a := from.Add(time.Duration(i) * step)
		b := a.Add(step)
		if i == n-1 {
			b = to
		}
		out[i] = TimelinePoint{At: a, Running: peak(running, a, b), Queued: peak(queued, a, b)}
	}
	return out
}

// span is a stretch of time a job spent running, or waiting.
type span struct{ start, end time.Time }

// peak is the most spans overlapping at any moment of [a, b). The count can
// only rise where a span starts, so it is enough to count at a and at every
// start inside the slice.
func peak(spans []span, a, b time.Time) int {
	at := func(t time.Time) int {
		n := 0
		for _, s := range spans {
			if !s.start.After(t) && s.end.After(t) {
				n++
			}
		}
		return n
	}
	best := at(a)
	for _, s := range spans {
		if s.start.After(a) && s.start.Before(b) {
			if c := at(s.start); c > best {
				best = c
			}
		}
	}
	return best
}
