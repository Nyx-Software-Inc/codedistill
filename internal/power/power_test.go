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

package power

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestSourceString(t *testing.T) {
	cases := []struct {
		in   Source
		want string
	}{
		{SourceUnknown, "unknown"},
		{SourceAC, "ac"},
		{SourceBattery, "battery"},
		{Source(99), "unknown"},
	}
	for _, c := range cases {
		if got := c.in.String(); got != c.want {
			t.Errorf("Source(%d).String() = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestDetectReturnsUsableSource(t *testing.T) {
	// Real Detect against the host. We don't care which value comes
	// back — only that it's not Unknown and not an error escaping the
	// "fatal=AC" mapping.
	src, _ := Detect()
	if src == SourceUnknown {
		t.Fatalf("Detect returned SourceUnknown on host; should map fatal-errors to SourceAC")
	}
}

// fakeDetector returns sources from a sequence, advancing on each call.
// The last value sticks once exhausted so long-running tests don't
// trigger extra transitions.
type fakeDetector struct {
	mu      sync.Mutex
	seq     []Source
	pos     int
	err     error
	callsAt int32
}

func (f *fakeDetector) detect() (Source, error) {
	atomic.AddInt32(&f.callsAt, 1)
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.pos >= len(f.seq) {
		return f.seq[len(f.seq)-1], f.err
	}
	s := f.seq[f.pos]
	f.pos++
	return s, f.err
}

func TestWatcherInitialSubscribeFiresBootstrap(t *testing.T) {
	fd := &fakeDetector{seq: []Source{SourceBattery}}
	w := NewWatcherFunc(fd.detect, 10*time.Millisecond, nil)

	got := make(chan Source, 4)
	w.Subscribe(func(s Source) { got <- s })

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go w.Run(ctx)

	select {
	case s := <-got:
		if s != SourceBattery {
			t.Fatalf("first event = %v, want %v", s, SourceBattery)
		}
	case <-time.After(time.Second):
		t.Fatal("subscriber never got bootstrap event")
	}
	if w.Current() != SourceBattery {
		t.Fatalf("Current() = %v, want %v", w.Current(), SourceBattery)
	}
}

func TestWatcherFiresOnTransition(t *testing.T) {
	fd := &fakeDetector{seq: []Source{SourceAC, SourceAC, SourceBattery, SourceBattery, SourceAC}}
	w := NewWatcherFunc(fd.detect, 5*time.Millisecond, nil)

	got := make(chan Source, 8)
	w.Subscribe(func(s Source) { got <- s })

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go w.Run(ctx)

	collect := func(n int) []Source {
		out := []Source{}
		deadline := time.After(time.Second)
		for len(out) < n {
			select {
			case s := <-got:
				out = append(out, s)
			case <-deadline:
				t.Fatalf("only got %d/%d events: %v", len(out), n, out)
			}
		}
		return out
	}

	events := collect(3)
	want := []Source{SourceAC, SourceBattery, SourceAC}
	for i, w := range want {
		if events[i] != w {
			t.Fatalf("event[%d] = %v, want %v (all: %v)", i, events[i], w, events)
		}
	}
}

func TestWatcherDetectorErrorDoesNotPanic(t *testing.T) {
	fd := &fakeDetector{
		seq: []Source{SourceAC},
		err: errors.New("sysfs read failed"),
	}
	w := NewWatcherFunc(fd.detect, 5*time.Millisecond, nil)
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	w.Run(ctx)
	if w.Current() != SourceAC {
		t.Fatalf("Current() = %v after error path, want %v", w.Current(), SourceAC)
	}
}

func TestSubscribeNilIsNoop(t *testing.T) {
	w := NewWatcherFunc(func() (Source, error) { return SourceAC, nil }, time.Second, nil)
	w.Subscribe(nil)
	// No assertion needed — passing nil must not crash on the next poll.
	w.poll()
}
