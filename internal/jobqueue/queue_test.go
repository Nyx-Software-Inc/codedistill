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
	"context"
	"errors"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"codedistill/internal/domain"
)

var t0 = time.Date(2026, 11, 23, 9, 0, 0, 0, time.UTC)

func queued(id, prio string, joinedAgo time.Duration) *domain.Job {
	at := t0.Add(-joinedAgo)
	return &domain.Job{ID: id, Type: "work", Status: domain.JobQueued, Priority: prio, StartedAt: at, QueuedAt: &at}
}

func ids(js []*domain.Job) string {
	var s []string
	for _, j := range js {
		s = append(s, j.ID)
	}
	return strings.Join(s, ",")
}

func TestOrderByPriorityThenArrival(t *testing.T) {
	js := []*domain.Job{
		queued("m-new", "medium", time.Minute),
		queued("low", "low", 2*time.Hour),
		queued("high", "high", time.Minute),
		queued("m-old", "medium", 30*time.Minute),
		queued("unset", "", 10*time.Minute), // before the queue: medium
	}
	if got := ids(Order(js, t0, DefaultAging, nil)); got != "high,m-old,unset,m-new,low" {
		t.Fatalf("order = %s", got)
	}
	if Position(js, "low", t0, DefaultAging, nil) != 5 || Position(js, "nope", t0, DefaultAging, nil) != 0 {
		t.Error("position")
	}
}

// A job moves up a level for each aging interval waited, never past High, so a
// stream of High jobs cannot starve a Low one.
func TestAgingMovesWaitingJobsUp(t *testing.T) {
	js := []*domain.Job{
		queued("high-fresh", "high", time.Minute),
		queued("low-6h", "low", 6*time.Hour+time.Minute),
		queued("med-3h", "medium", 3*time.Hour+time.Minute),
		queued("low-3h", "low", 3*time.Hour+time.Minute),
	}
	if got := ids(Order(js, t0, DefaultAging, nil)); got != "low-6h,med-3h,high-fresh,low-3h" {
		t.Fatalf("order = %s", got)
	}
	if r := EffectiveRank(queued("ancient", "low", 100*time.Hour), t0, DefaultAging); r != domain.JobPriorityRank(domain.PriorityHigh) {
		t.Errorf("aging passed High: %d", r)
	}
	if got := ids(Order(js, t0, 0, nil)); got != "high-fresh,med-3h,low-6h,low-3h" {
		t.Errorf("no aging: %s", got)
	}
	// A workflow whose priority is fixed does not move up by waiting.
	js[1].Type = "nightly"
	fixed := func(typ string) bool { return typ == "nightly" }
	if got := ids(Order(js, t0, DefaultAging, fixed)); got != "med-3h,high-fresh,low-3h,low-6h" {
		t.Errorf("fixed priority: %s", got)
	}
}

func TestSuggest(t *testing.T) {
	const gb = 1 << 30
	cases := []struct {
		mem   uint64
		cores int
		want  int
	}{
		{31 * gb, 22, 3}, // Rich's laptop
		{16 * gb, 8, 2},
		{128 * gb, 64, 16},
		{128 * gb, 8, 2}, // cores are the limit
		{4 * gb, 2, 1},   // never below one
		{0, 16, 1},       // memory unknown
	}
	for _, c := range cases {
		if got := Suggest(c.mem, c.cores); got != c.want {
			t.Errorf("Suggest(%d GB, %d cores) = %d, want %d", c.mem/gb, c.cores, got, c.want)
		}
	}
	if m, c := Machine(); c < 1 || Suggest(m, c) < 1 {
		t.Errorf("Machine() = %d, %d", m, c)
	}
}

// fakeStore is the jobs table, in memory.
type fakeStore struct {
	mu   sync.Mutex
	jobs map[string]*domain.Job
}

func (f *fakeStore) ListQueuedJobs(context.Context) ([]*domain.Job, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []*domain.Job
	for _, j := range f.jobs {
		if j.Status == domain.JobQueued {
			c := *j
			out = append(out, &c)
		}
	}
	return out, nil
}
func (f *fakeStore) CountRunningJobs(context.Context) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, j := range f.jobs {
		if j.Status == domain.JobRunning {
			n++
		}
	}
	return n, nil
}
func (f *fakeStore) StartQueuedJob(_ context.Context, id string, at time.Time) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	j := f.jobs[id]
	if j == nil || j.Status != domain.JobQueued {
		return false, nil
	}
	j.Status, j.StartedAt = domain.JobRunning, at
	return true, nil
}
func (f *fakeStore) FinishJob(_ context.Context, id, status, msg string, _ time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if j := f.jobs[id]; j != nil && (j.Status == domain.JobRunning || j.Status == domain.JobQueued) {
		j.Status, j.Error = status, msg
	}
	return nil
}
func (f *fakeStore) ListRunningJobs(context.Context) ([]*domain.Job, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []*domain.Job
	for _, j := range f.jobs {
		if j.Status == domain.JobRunning {
			c := *j
			out = append(out, &c)
		}
	}
	return out, nil
}
func (f *fakeStore) GetJob(_ context.Context, id string) (*domain.Job, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if j := f.jobs[id]; j != nil {
		c := *j
		return &c, nil
	}
	return nil, nil
}
func (f *fakeStore) RequeueJob(_ context.Context, id, by string, _ time.Time) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	j := f.jobs[id]
	if j == nil || (j.Status != domain.JobPaused && j.Status != domain.JobInterrupted) {
		return false, nil
	}
	j.Status, j.PausedBy = domain.JobQueued, by
	return true, nil
}
func (f *fakeStore) pause(id, by string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	j := f.jobs[id]
	if j == nil || j.Status != domain.JobRunning {
		return false
	}
	j.Status, j.PausedBy = domain.JobPaused, by
	return true
}
func (f *fakeStore) status(id string) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.jobs[id].Status + " " + f.jobs[id].Error
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// Never more running than there are slots; each ending starts the next, in
// priority order; and the slots count jobs from every project.
func TestSchedulerKeepsToTheSlots(t *testing.T) {
	st := &fakeStore{jobs: map[string]*domain.Job{}}
	for _, j := range []*domain.Job{
		queued("a", "medium", 5*time.Minute), queued("b", "medium", 4*time.Minute),
		queued("c", "high", time.Minute), queued("d", "low", 10*time.Minute),
	} {
		st.jobs[j.ID] = j
	}
	// One job already running elsewhere (another project, or the command line)
	// holds a slot too.
	st.jobs["cli"] = &domain.Job{ID: "cli", Status: domain.JobRunning}

	release := map[string]chan struct{}{}
	var mu sync.Mutex
	var started []string
	for _, id := range []string{"a", "b", "c", "d"} {
		release[id] = make(chan struct{})
	}
	q := New(st, Config{Slots: func(context.Context) int { return 3 }, Now: func() time.Time { return t0 }})
	q.Register("work", func(ctx context.Context, j *domain.Job) error {
		mu.Lock()
		started = append(started, j.ID)
		mu.Unlock()
		<-release[j.ID]
		return st.FinishJob(ctx, j.ID, domain.JobSucceeded, "", t0)
	})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { q.Run(ctx); close(done) }()
	// The jobs started together run in their own goroutines, so the order they
	// record in is theirs; compare sets for those.
	got := func() string {
		mu.Lock()
		defer mu.Unlock()
		s := append([]string(nil), started...)
		sort.Strings(s)
		return strings.Join(s, ",")
	}

	waitFor(t, "two to start", func() bool { return got() == "a,c" })
	time.Sleep(50 * time.Millisecond)
	if got() != "a,c" {
		t.Fatalf("more started than the slots allow: %s", got())
	}
	st.FinishJob(ctx, "cli", domain.JobSucceeded, "", t0)
	q.Kick() // the command line's job ended in another process
	waitFor(t, "b", func() bool { return got() == "a,b,c" })
	close(release["c"])
	waitFor(t, "d after c ended", func() bool { return got() == "a,b,c,d" })
	for _, id := range []string{"a", "b", "d"} {
		close(release[id])
	}
	waitFor(t, "all done", func() bool { return strings.HasPrefix(st.status("d"), domain.JobSucceeded) })
	cancel()
	<-done
}

// A job its runner could not get going fails with the reason; a job with no
// runner here fails rather than waiting forever.
func TestSchedulerFailsWhatCannotRun(t *testing.T) {
	st := &fakeStore{jobs: map[string]*domain.Job{
		"broken": queued("broken", "medium", time.Minute),
		"alien":  {ID: "alien", Type: "unknown", Status: domain.JobQueued, Priority: "medium", StartedAt: t0},
		"panics": queued("panics", "low", time.Minute),
	}}
	q := New(st, Config{Slots: func(context.Context) int { return 5 }, Now: func() time.Time { return t0 }})
	q.Register("work", func(_ context.Context, j *domain.Job) error {
		if j.ID == "panics" {
			panic("boom")
		}
		return errors.New("the scratchpad no longer exists")
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go q.Run(ctx)
	waitFor(t, "failures", func() bool {
		return st.status("broken") == "failed the scratchpad no longer exists" &&
			st.status("alien") == "failed this kind of job cannot run here" &&
			st.status("panics") == "failed the job stopped unexpectedly"
	})
}

func TestChooseVictim(t *testing.T) {
	at := func(id, prio, typ string, ago time.Duration) *domain.Job {
		return &domain.Job{ID: id, Type: typ, Status: domain.JobRunning, Priority: prio, StartedAt: t0.Add(-ago)}
	}
	can := func(typ string) bool { return typ == "fix" }
	running := []*domain.Job{
		at("high-fix", "high", "fix", time.Minute),
		at("low-old", "low", "fix", 2*time.Hour),
		at("low-new", "low", "fix", 10*time.Minute),
		at("low-cannot", "low", "draft", time.Minute),
		at("imm", "immediate", "fix", time.Second),
	}
	if v := ChooseVictim(running, can); v == nil || v.ID != "low-new" {
		t.Fatalf("victim = %+v; want the lowest priority that started last", v)
	}
	if v := ChooseVictim(running[3:], can); v != nil {
		t.Errorf("paused %s: never an Immediate job, nor one that cannot pause", v.ID)
	}
}

// An Immediate job with every slot busy pauses the lowest-priority running job
// that can pause and starts at once; the paused job comes back to the line
// once its work has stopped, and resumes when a slot frees.
func TestImmediateMakesRoom(t *testing.T) {
	st := &fakeStore{jobs: map[string]*domain.Job{}}
	st.jobs["low"] = queued("low", "low", 10*time.Minute)
	st.jobs["high"] = queued("high", "high", 9*time.Minute)

	var mu sync.Mutex
	var started []string
	stop := map[string]chan struct{}{}
	cancels := map[string]context.CancelFunc{}
	q := New(st, Config{
		Slots:    func(context.Context) int { return 2 },
		Now:      func() time.Time { return t0 },
		CanPause: func(string) bool { return true },
		Pause: func(_ context.Context, j *domain.Job) error {
			if !st.pause(j.ID, domain.PausedByQueue) {
				return errors.New("not running")
			}
			mu.Lock()
			cancels[j.ID]()
			mu.Unlock()
			return nil
		},
	})
	q.Register("work", func(ctx context.Context, j *domain.Job) error {
		ctx, cancel := context.WithCancel(ctx)
		mu.Lock()
		started = append(started, j.ID)
		cancels[j.ID] = cancel
		ch := stop[j.ID]
		if ch == nil {
			ch = make(chan struct{})
			stop[j.ID] = ch
		}
		mu.Unlock()
		select {
		case <-ch:
			return st.FinishJob(ctx, j.ID, domain.JobSucceeded, "", t0)
		case <-ctx.Done():
			return errors.New("paused") // the runner's own word for it; the queue looks at the row
		}
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go q.Run(ctx)
	got := func() string { mu.Lock(); defer mu.Unlock(); return strings.Join(started, ",") }
	waitFor(t, "both to start", func() bool { return len(strings.Split(got(), ",")) == 2 })

	st.mu.Lock()
	st.jobs["now"] = queued("now", domain.PriorityImmediate, 0)
	st.mu.Unlock()
	q.Kick()
	waitFor(t, "the Immediate job to start", func() bool { return strings.HasSuffix(got(), ",now") })
	waitFor(t, "low back in line", func() bool { return st.status("low") == domain.JobQueued+" " })
	if st.status("high") != domain.JobRunning+" " {
		t.Errorf("high = %s; the low job should have made room", st.status("high"))
	}
	if j, _ := st.GetJob(ctx, "low"); j.PausedBy != domain.PausedByQueue {
		t.Errorf("low paused by %q", j.PausedBy)
	}

	// A second Immediate job pauses high; a third finds only Immediate jobs
	// running and waits, first in line.
	st.mu.Lock()
	st.jobs["now2"] = queued("now2", domain.PriorityImmediate, 0)
	st.mu.Unlock()
	q.Kick()
	waitFor(t, "high paused for the second", func() bool { return st.status("high") == domain.JobQueued+" " })
	st.mu.Lock()
	st.jobs["now3"] = queued("now3", domain.PriorityImmediate, 0)
	st.mu.Unlock()
	q.Kick()
	time.Sleep(100 * time.Millisecond)
	if st.status("now3") != domain.JobQueued+" " {
		t.Fatalf("now3 = %s; nothing could make room", st.status("now3"))
	}

	// When an Immediate job ends, the next Immediate takes its slot, then the
	// paused jobs resume, high first.
	mu.Lock()
	close(stop["now"])
	mu.Unlock()
	waitFor(t, "now3 next", func() bool { return strings.HasSuffix(got(), ",now3") })
	mu.Lock()
	stop["high"] = make(chan struct{})
	close(stop["now2"])
	mu.Unlock()
	waitFor(t, "high resumes", func() bool { return strings.HasSuffix(got(), ",high") })
	if j, _ := st.GetJob(ctx, "high"); j.Status != domain.JobRunning {
		t.Errorf("high = %+v", j)
	}
}
