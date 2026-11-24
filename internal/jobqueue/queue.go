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

// Package jobqueue decides which jobs run (docs/design/job-queue.md).
//
// A job is submitted as a row with status queued. The scheduler starts queued
// jobs while there are free job slots, highest priority first, and starts the
// next whenever one ends. The line is the jobs table itself, so it survives a
// restart with nothing to rebuild, and the slots are shared by every project:
// counting running rows counts the whole installation.
//
// It decides only which jobs run, never what a job does inside: that is the
// job's runner (and the Conductor's, for fix jobs). Plain code with fixed
// rules, like the verifier.
package jobqueue

import (
	"context"
	"log/slog"
	"runtime"
	"sort"
	"sync"
	"time"

	"github.com/shirou/gopsutil/v4/mem"

	"codedistill/internal/domain"
)

// Store is what the scheduler needs of the database.
type Store interface {
	ListQueuedJobs(ctx context.Context) ([]*domain.Job, error)
	ListRunningJobs(ctx context.Context) ([]*domain.Job, error)
	CountRunningJobs(ctx context.Context) (int, error)
	StartQueuedJob(ctx context.Context, id string, at time.Time) (bool, error)
	FinishJob(ctx context.Context, id, status, errMsg string, at time.Time) error
	GetJob(ctx context.Context, id string) (*domain.Job, error)
	RequeueJob(ctx context.Context, id, by string, at time.Time) (bool, error)
}

// Runner does one kind of job, from its row (and the parameters saved with it)
// to its end. It is called once the job has left the line and is running, and
// returns when the job is over, having recorded how it ended. An error it
// returns, for a job it never got going, fails the job with that error.
type Runner func(ctx context.Context, job *domain.Job) error

// Config is how a scheduler finds its limits. Both are read each time the
// queue is looked at, so a changed setting applies to the next job.
type Config struct {
	// Slots is how many jobs may run at once.
	Slots func(ctx context.Context) int
	// Aging is how long a job waits before it moves up a priority level.
	Aging func(ctx context.Context) time.Duration
	Log   *slog.Logger
	// Now is the clock; tests set it.
	Now func() time.Time

	// CanPause reports whether a job's workflow can pause and resume where it
	// stopped: only those are paused to make room for an Immediate job.
	CanPause func(jobType string) bool
	// FixedPriority reports whether a job's workflow keeps the priority its
	// jobs were given: waiting does not raise it.
	FixedPriority func(jobType string) bool
	// Pause pauses a running job to make room (paused by the queue) and stops
	// its work at the next checkpoint. Its runner then returns, and the queue
	// puts it back in line.
	Pause func(ctx context.Context, j *domain.Job) error
}

// DefaultAging is how long a job waits before moving up a level. Not an hour:
// some jobs run for a day or two, and hourly aging would put every waiting job
// at High within one (Rich, 2026-11-23: "every 2-4 hours").
const DefaultAging = 3 * time.Hour

// Poll is how often the queue is looked at with nothing to prompt it: for
// aging, and for slots freed by a job that ended in another process.
const Poll = 30 * time.Second

// Scheduler starts queued jobs as slots free.
type Scheduler struct {
	store   Store
	cfg     Config
	kick    chan struct{}
	mu      sync.Mutex
	runners map[string]Runner
	wg      sync.WaitGroup
}

// New returns a scheduler; Run starts it.
func New(store Store, cfg Config) *Scheduler {
	if cfg.Log == nil {
		cfg.Log = slog.Default()
	}
	if cfg.Now == nil {
		cfg.Now = func() time.Time { return time.Now().UTC() }
	}
	if cfg.Aging == nil {
		cfg.Aging = func(context.Context) time.Duration { return DefaultAging }
	}
	if cfg.Slots == nil {
		cfg.Slots = func(context.Context) int { return Suggest(Machine()) }
	}
	if cfg.CanPause == nil {
		cfg.CanPause = func(string) bool { return false }
	}
	if cfg.FixedPriority == nil {
		cfg.FixedPriority = func(string) bool { return false }
	}
	return &Scheduler{store: store, cfg: cfg, kick: make(chan struct{}, 1), runners: map[string]Runner{}}
}

// Register names the runner for one job type.
func (q *Scheduler) Register(jobType string, r Runner) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.runners[jobType] = r
}

// Kick asks the scheduler to look at the line now: a job was submitted, ended,
// or a setting changed. Never blocks.
func (q *Scheduler) Kick() {
	select {
	case q.kick <- struct{}{}:
	default:
	}
}

// Run starts queued jobs until ctx ends, and then waits for the ones it
// started to return.
func (q *Scheduler) Run(ctx context.Context) {
	t := time.NewTicker(Poll)
	defer t.Stop()
	for {
		q.fill(ctx)
		select {
		case <-ctx.Done():
			q.wg.Wait()
			return
		case <-q.kick:
		case <-t.C:
		}
	}
}

// fill starts as many jobs as there are free slots, first making room for any
// Immediate job waiting.
func (q *Scheduler) fill(ctx context.Context) {
	free := q.cfg.Slots(ctx)
	running, err := q.store.ListRunningJobs(ctx)
	if err != nil {
		q.cfg.Log.Warn("job queue: could not list running jobs", "err", err)
		return
	}
	free -= len(running)
	queued, err := q.store.ListQueuedJobs(ctx)
	if err != nil {
		q.cfg.Log.Warn("job queue: could not read the line", "err", err)
		return
	}
	now := q.cfg.Now()
	line := Order(queued, now, q.cfg.Aging(ctx), q.cfg.FixedPriority)
	// Immediate jobs are first in line. Each one without a free slot pauses a
	// running job to make one: the paused job stops at its next checkpoint and
	// comes back to the line, while the Immediate job starts now.
	for _, j := range line {
		if free > 0 || j.Priority != domain.PriorityImmediate || q.cfg.Pause == nil {
			break
		}
		victim := ChooseVictim(running, q.cfg.CanPause)
		if victim == nil {
			break // nothing can pause: it waits, first in line
		}
		if err := q.cfg.Pause(ctx, victim); err != nil {
			q.cfg.Log.Warn("job queue: could not pause a job to make room", "job", victim.ID, "err", err)
			break
		}
		q.cfg.Log.Info("job queue: paused a job to make room for an Immediate one", "paused", victim.ID, "for", j.ID)
		running = without(running, victim.ID)
		free++
	}
	if free <= 0 {
		return
	}
	for _, j := range line {
		if free == 0 {
			return
		}
		q.mu.Lock()
		run := q.runners[j.Type]
		q.mu.Unlock()
		if run == nil {
			// Nothing here can run it (a job type this edition lacks). Say so
			// rather than leave it waiting forever.
			_ = q.store.FinishJob(ctx, j.ID, domain.JobFailed, "this kind of job cannot run here", now)
			continue
		}
		ok, err := q.store.StartQueuedJob(ctx, j.ID, now)
		if err != nil {
			q.cfg.Log.Warn("job queue: could not start a job", "job", j.ID, "err", err)
			continue
		}
		if !ok {
			continue // cancelled meanwhile, or started by another process
		}
		free--
		j.Status, j.StartedAt = domain.JobRunning, now
		q.wg.Add(1)
		go q.start(ctx, j, run)
	}
}

func (q *Scheduler) start(ctx context.Context, j *domain.Job, run Runner) {
	defer q.wg.Done()
	// The slot is free once the job is over, whatever happened.
	defer q.Kick()
	defer func() {
		if r := recover(); r != nil {
			q.cfg.Log.Error("job queue: a job's runner panicked", "job", j.ID, "panic", r)
			_ = q.store.FinishJob(context.WithoutCancel(ctx), j.ID, domain.JobFailed, "the job stopped unexpectedly", q.cfg.Now())
		}
	}()
	err := run(ctx, j)
	bg := context.WithoutCancel(ctx)
	// Paused to make room: back in line now its work has stopped, to resume
	// when a slot frees. Not before, or it could start again while its last
	// run was still winding down.
	if cur, gerr := q.store.GetJob(bg, j.ID); gerr == nil && cur != nil &&
		cur.Status == domain.JobPaused && cur.PausedBy == domain.PausedByQueue {
		if _, rerr := q.store.RequeueJob(bg, j.ID, domain.PausedByQueue, q.cfg.Now()); rerr != nil {
			q.cfg.Log.Warn("job queue: could not put a paused job back in line", "job", j.ID, "err", rerr)
		}
		return
	}
	if err != nil {
		q.cfg.Log.Warn("job queue: a job could not run", "job", j.ID, "type", j.Type, "err", err)
		// Only touches a job still running: one the runner already finished
		// keeps the ending it recorded.
		_ = q.store.FinishJob(bg, j.ID, domain.JobFailed, err.Error(), q.cfg.Now())
	}
}

// ChooseVictim picks the running job to pause for an Immediate one: the
// lowest priority that can pause, and among equals the one that started most
// recently, which loses the least work. Never an Immediate job. Nil when no
// running job can pause.
func ChooseVictim(running []*domain.Job, canPause func(string) bool) *domain.Job {
	var best *domain.Job
	for _, j := range running {
		if j.Priority == domain.PriorityImmediate || !canPause(j.Type) {
			continue
		}
		if best == nil {
			best = j
			continue
		}
		rj, rb := domain.JobPriorityRank(j.Priority), domain.JobPriorityRank(best.Priority)
		if rj < rb || (rj == rb && j.StartedAt.After(best.StartedAt)) {
			best = j
		}
	}
	return best
}

func without(js []*domain.Job, id string) []*domain.Job {
	out := js[:0:0]
	for _, j := range js {
		if j.ID != id {
			out = append(out, j)
		}
	}
	return out
}

// Order puts waiting jobs in the order they will start: by priority, a job
// moving up a level for every `aging` it has waited (never above High; only a
// person makes a job Immediate, and never for a workflow whose priority is
// fixed), then by when it joined the line. fixed may be nil.
func Order(jobs []*domain.Job, now time.Time, aging time.Duration, fixed func(jobType string) bool) []*domain.Job {
	out := append([]*domain.Job(nil), jobs...)
	rank := make(map[string]int, len(out))
	for _, j := range out {
		if fixed != nil && fixed(j.Type) {
			rank[j.ID] = domain.JobPriorityRank(j.Priority)
			continue
		}
		rank[j.ID] = EffectiveRank(j, now, aging)
	}
	sort.SliceStable(out, func(a, b int) bool {
		ra, rb := rank[out[a].ID], rank[out[b].ID]
		if ra != rb {
			return ra > rb
		}
		return joined(out[a]).Before(joined(out[b]))
	})
	return out
}

// EffectiveRank is a waiting job's priority after aging. A job paused to make
// room, or stopped by a restart, was running: it goes to the front of its
// priority, which its early place in line already gives it.
func EffectiveRank(j *domain.Job, now time.Time, aging time.Duration) int {
	base := domain.JobPriorityRank(j.Priority)
	high := domain.JobPriorityRank(domain.PriorityHigh)
	if base >= high || aging <= 0 {
		return base
	}
	up := int(now.Sub(joined(j)) / aging)
	if base+up > high {
		return high
	}
	return base + up
}

// Position is where a waiting job stands in the line, counting from 1; 0 when
// it is not in it.
func Position(jobs []*domain.Job, id string, now time.Time, aging time.Duration, fixed func(jobType string) bool) int {
	for i, j := range Order(jobs, now, aging, fixed) {
		if j.ID == id {
			return i + 1
		}
	}
	return 0
}

func joined(j *domain.Job) time.Time {
	if j.QueuedAt != nil {
		return *j.QueuedAt
	}
	return j.StartedAt
}

// ── The machine's suggestion ────────────────────────────────────────────────

// Per slot. A fix job at its peak is a clone of the repository, two or three
// agent processes and the project's test suite: about 6–8 GB, and the cores to
// run the tests (docs/design/job-queue.md).
const (
	memPerSlot   = 8 << 30
	coresPerSlot = 4
)

// Suggest is how many job slots a machine with this much memory and these
// many cores should have: one per 8 GB and per 4 cores, whichever gives
// fewer, and at least one.
func Suggest(memBytes uint64, cores int) int {
	n := int(memBytes / memPerSlot)
	if c := cores / coresPerSlot; c < n {
		n = c
	}
	if n < 1 {
		return 1
	}
	return n
}

// Machine reports this machine's memory and cores. Memory is 0 when it cannot
// be read, which Suggest turns into one slot.
func Machine() (memBytes uint64, cores int) {
	if v, err := mem.VirtualMemory(); err == nil {
		memBytes = v.Total
	}
	return memBytes, runtime.NumCPU()
}
