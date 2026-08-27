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

// Package power reports the system's current power source (AC or
// battery) so background work like the code indexer can throttle itself
// when the user is on battery. Cross-platform via distatus/battery;
// machines with no battery hardware always report AC, which means
// servers and desktops naturally never trigger battery-aware throttling
// without any mode flag or build tag.
package power

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/distatus/battery"
)

// Source enumerates the current power source.
type Source int

const (
	// SourceUnknown is the zero value, used before the first poll
	// completes or when the OS APIs returned a fatal error. Callers
	// should treat Unknown as AC for policy decisions — the safer
	// default is "don't throttle".
	SourceUnknown Source = iota
	SourceAC
	SourceBattery
)

func (s Source) String() string {
	switch s {
	case SourceAC:
		return "ac"
	case SourceBattery:
		return "battery"
	default:
		return "unknown"
	}
}

// Detector is the function shape backing one Watcher. The default
// implementation is Detect; tests can inject a fake.
type Detector func() (Source, error)

// Detect performs a one-shot read of the host power source. Returns
// SourceAC when no battery hardware is present (desktops, servers,
// laptops on a dock without an internal battery). Returns the partial
// error from distatus/battery when one is non-fatal, alongside a usable
// Source — callers may log and proceed.
func Detect() (Source, error) {
	bats, err := battery.GetAll()
	// A fatal error means we couldn't read anything at all. On Linux
	// this typically means /sys/class/power_supply is missing — almost
	// always a non-laptop system, so the safe answer is AC.
	if _, fatal := err.(battery.ErrFatal); fatal {
		return SourceAC, err
	}
	if len(bats) == 0 {
		return SourceAC, nil
	}
	for _, b := range bats {
		if b == nil {
			continue
		}
		if b.State.Raw == battery.Discharging {
			return SourceBattery, err
		}
	}
	return SourceAC, err
}

// Watcher polls a Detector on an interval and fires subscribers when
// the source changes. The first poll always emits a change event from
// SourceUnknown to the detected source so subscribers can bootstrap
// their state from the watcher rather than calling Detect themselves.
type Watcher struct {
	detector Detector
	interval time.Duration
	log      *slog.Logger

	mu      sync.Mutex
	current Source
	subs    []func(Source)
}

// DefaultInterval is the polling cadence when NewWatcher is given 0.
// 30 s is long enough to be cheap (a sysfs read every 30 s is noise)
// and short enough that the indexer sees plug/unplug events within a
// single watcher tick of normal use.
const DefaultInterval = 30 * time.Second

// NewWatcher returns a Watcher backed by Detect. Pass interval=0 for
// DefaultInterval.
func NewWatcher(interval time.Duration, log *slog.Logger) *Watcher {
	return NewWatcherFunc(Detect, interval, log)
}

// NewWatcherFunc returns a Watcher backed by the supplied detector.
// Exposed for tests; production callers use NewWatcher.
func NewWatcherFunc(detector Detector, interval time.Duration, log *slog.Logger) *Watcher {
	if interval <= 0 {
		interval = DefaultInterval
	}
	if log == nil {
		log = slog.Default()
	}
	return &Watcher{
		detector: detector,
		interval: interval,
		log:      log,
		current:  SourceUnknown,
	}
}

// Current returns the most recently observed source. Returns
// SourceUnknown before the first poll completes.
func (w *Watcher) Current() Source {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.current
}

// Subscribe registers a callback fired on every source change,
// including the bootstrap transition from Unknown to the first
// detected source. Callbacks run synchronously in the watcher's
// goroutine — keep them fast and non-blocking. Order across callbacks
// is registration order; concurrent registrations are serialized
// against in-flight polls.
func (w *Watcher) Subscribe(cb func(Source)) {
	if cb == nil {
		return
	}
	w.mu.Lock()
	w.subs = append(w.subs, cb)
	w.mu.Unlock()
}

// Run blocks until ctx is done. Fires an immediate poll so subscribers
// see the starting source without waiting an interval, then polls on
// the configured cadence. Detector errors are logged at debug level —
// on systems with no battery hardware these are routine and not worth
// surfacing to the user.
func (w *Watcher) Run(ctx context.Context) {
	w.poll()
	t := time.NewTicker(w.interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			w.poll()
		}
	}
}

func (w *Watcher) poll() {
	src, err := w.detector()
	if err != nil {
		w.log.Debug("power: detector returned error", "err", err, "source", src)
	}
	w.mu.Lock()
	changed := src != w.current
	if changed {
		w.current = src
	}
	subs := append([]func(Source){}, w.subs...)
	w.mu.Unlock()
	if !changed {
		return
	}
	w.log.Info("power: source changed", "source", src)
	for _, cb := range subs {
		cb(src)
	}
}
