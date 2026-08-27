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

// Package throttle holds the battery-aware throttle policy shared by
// every background watcher that wants to back off when the user is on
// battery — today, codeindex.Watcher and matcher.Watcher.
//
// Model: one Multiplier knob (1.0 = no throttle, up to ~11.0 = ~11x
// slower) plus a separate Pause toggle ("stop entirely"). Watchers
// scale their natural cadences and per-call pacing through the
// helpers ScaleInterval and ScalePace; on AC the policy is the zero
// value and both helpers degrade to pre-throttle behavior (intervals
// unchanged, pacing zero).
package throttle

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"codedistill/internal/power"
	"codedistill/internal/storage"
)

// Setting keys in user_settings. The keys still live under the
// "indexing" namespace because that's where they were introduced;
// since v0.8.3 the same keys also drive the matcher.
const (
	KeyBatteryPause      = "indexing.battery.pause"
	KeyBatteryMultiplier = "indexing.battery.multiplier"
)

// Slider bounds — the SPA exposes positions 1..11 mapping 1:1 to
// these values. Stored as a float so future scaling tweaks (e.g.
// half-steps for finer control) don't need a key migration.
const (
	MinMultiplier     = 1.0
	MaxMultiplier     = 11.0
	DefaultMultiplier = 5.0
	DefaultPause      = false
)

// Policy is the effective throttle for the current moment, combining
// the user's stored preferences with the live power source. The zero
// value (Pause=false, Multiplier=0) is the AC behavior — ScaleInterval
// returns the base unchanged and ScalePace returns zero.
type Policy struct {
	// Pause is true when background work should stop entirely.
	// Watchers skip ticks; per-call loops bail on the next check.
	// Overrides Multiplier.
	Pause bool
	// Multiplier scales every base duration. 0 (the AC default) and
	// values <= 1 mean "no throttle." On battery, values between 1
	// and ~11 stretch the base.
	Multiplier float64
}

// ScaleInterval stretches a periodic tick interval by the policy's
// multiplier. Returns base unchanged on AC (or when the slider is at
// position 1, "no throttle"). Watchers call this to decide how long
// to sleep between ticks.
func (p Policy) ScaleInterval(base time.Duration) time.Duration {
	if p.Multiplier <= 1 || base <= 0 {
		return base
	}
	return time.Duration(float64(base) * p.Multiplier)
}

// ScalePace returns the duration to sleep between expensive per-call
// steps (an embed call, an LLM judge). Returns zero on AC so the
// natural rate is preserved. On battery, returns base * multiplier
// — small base values (1-3 s) keep modest multipliers gentle while
// aggressive multipliers genuinely throttle the per-call cadence.
func (p Policy) ScalePace(base time.Duration) time.Duration {
	if p.Multiplier <= 1 || base <= 0 {
		return 0
	}
	return time.Duration(float64(base) * p.Multiplier)
}

// ReadConfig loads the user's stored throttle preferences from
// user_settings, falling back to defaults for any missing or invalid
// keys. Returns usable values even when storage errors out so callers
// can degrade gracefully (defaults + the wrapped error).
func ReadConfig(ctx context.Context, store storage.Storage, userID string) (pause bool, multiplier float64, err error) {
	pause = DefaultPause
	multiplier = DefaultMultiplier

	if s, gerr := store.GetUserSetting(ctx, userID, KeyBatteryPause); gerr == nil {
		var v bool
		if json.Unmarshal(s.Value, &v) == nil {
			pause = v
		}
	} else if !errors.Is(gerr, storage.ErrNotFound) {
		return pause, multiplier, fmt.Errorf("read %s: %w", KeyBatteryPause, gerr)
	}

	if s, gerr := store.GetUserSetting(ctx, userID, KeyBatteryMultiplier); gerr == nil {
		var v float64
		if json.Unmarshal(s.Value, &v) == nil && v >= MinMultiplier {
			if v > MaxMultiplier {
				v = MaxMultiplier
			}
			multiplier = v
		}
	} else if !errors.Is(gerr, storage.ErrNotFound) {
		return pause, multiplier, fmt.Errorf("read %s: %w", KeyBatteryMultiplier, gerr)
	}

	return pause, multiplier, nil
}

// Reader returns the effective Policy for the current moment.
// Implementations should be fast (~ms is fine; no network I/O).
type Reader func(ctx context.Context) Policy

// Compute returns the effective Policy for the given power source. On
// AC (and on machines that report Unknown) it returns the zero policy
// so callers degrade to pre-throttle behavior.
func Compute(ctx context.Context, store storage.Storage, userID string, source power.Source, log *slog.Logger) Policy {
	if source != power.SourceBattery {
		return Policy{}
	}
	pause, mult, err := ReadConfig(ctx, store, userID)
	if err != nil && log != nil {
		log.Warn("throttle: read config failed, using defaults", "err", err)
	}
	return Policy{Pause: pause, Multiplier: mult}
}

// NewReader returns a Reader closure that consults the current power
// source and reads the user's stored config on every call. Production
// wiring in cmd/codedistill: build once with the power.Watcher's
// Current method and the storage handle, then inject into every
// throttle-aware Watcher.
func NewReader(store storage.Storage, userID string, currentSource func() power.Source, log *slog.Logger) Reader {
	if currentSource == nil {
		return func(context.Context) Policy { return Policy{} }
	}
	return func(ctx context.Context) Policy {
		return Compute(ctx, store, userID, currentSource(), log)
	}
}
