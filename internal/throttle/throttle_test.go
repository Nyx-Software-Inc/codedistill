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

package throttle

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"codedistill/internal/domain"
	"codedistill/internal/power"
	"codedistill/internal/storage/sqlite"
)

func newStore(t *testing.T) *sqlite.Store {
	t.Helper()
	store, err := sqlite.Open(":memory:")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { store.Close() })
	if err := store.Migrate(context.Background()); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return store
}

func setSetting(t *testing.T, store *sqlite.Store, key string, value any) {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	err = store.SetUserSetting(context.Background(), &domain.UserSetting{
		UserID: "local", Key: key, Value: raw,
	})
	if err != nil {
		t.Fatalf("set %s: %v", key, err)
	}
}

func TestPolicyZeroValueIsACBehavior(t *testing.T) {
	p := Policy{}
	if got := p.ScaleInterval(60 * time.Second); got != 60*time.Second {
		t.Errorf("ScaleInterval(60s) = %v, want 60s on AC", got)
	}
	if got := p.ScalePace(3 * time.Second); got != 0 {
		t.Errorf("ScalePace(3s) = %v, want 0 on AC", got)
	}
}

func TestPolicyMultiplierOneIsNoThrottle(t *testing.T) {
	p := Policy{Multiplier: 1.0}
	if got := p.ScaleInterval(60 * time.Second); got != 60*time.Second {
		t.Errorf("ScaleInterval(60s) at mult=1 = %v, want 60s", got)
	}
	if got := p.ScalePace(3 * time.Second); got != 0 {
		t.Errorf("ScalePace(3s) at mult=1 = %v, want 0", got)
	}
}

func TestPolicyMultiplierStretchesInterval(t *testing.T) {
	p := Policy{Multiplier: 5.0}
	if got := p.ScaleInterval(60 * time.Second); got != 5*60*time.Second {
		t.Errorf("ScaleInterval(60s) at mult=5 = %v, want 300s", got)
	}
	if got := p.ScalePace(3 * time.Second); got != 15*time.Second {
		t.Errorf("ScalePace(3s) at mult=5 = %v, want 15s", got)
	}
}

func TestPolicyZeroBaseDurationStaysZero(t *testing.T) {
	p := Policy{Multiplier: 5.0}
	if got := p.ScaleInterval(0); got != 0 {
		t.Errorf("ScaleInterval(0) = %v, want 0", got)
	}
	if got := p.ScalePace(0); got != 0 {
		t.Errorf("ScalePace(0) = %v, want 0", got)
	}
}

func TestReadConfigDefaults(t *testing.T) {
	store := newStore(t)
	pause, mult, err := ReadConfig(context.Background(), store, "local")
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if pause != DefaultPause || mult != DefaultMultiplier {
		t.Errorf("defaults mismatch: pause=%v mult=%v want %v %v", pause, mult, DefaultPause, DefaultMultiplier)
	}
}

func TestReadConfigStoredValues(t *testing.T) {
	store := newStore(t)
	setSetting(t, store, KeyBatteryPause, true)
	setSetting(t, store, KeyBatteryMultiplier, 7.5)

	pause, mult, err := ReadConfig(context.Background(), store, "local")
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !pause {
		t.Error("Pause = false, want true")
	}
	if mult != 7.5 {
		t.Errorf("Multiplier = %v, want 7.5", mult)
	}
}

func TestReadConfigClampsAboveMax(t *testing.T) {
	store := newStore(t)
	setSetting(t, store, KeyBatteryMultiplier, 99.0)
	_, mult, err := ReadConfig(context.Background(), store, "local")
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if mult != MaxMultiplier {
		t.Errorf("Multiplier = %v, want clamped to %v", mult, MaxMultiplier)
	}
}

func TestReadConfigIgnoresMalformed(t *testing.T) {
	store := newStore(t)
	setSetting(t, store, KeyBatteryPause, "yes")
	setSetting(t, store, KeyBatteryMultiplier, -3.0)

	pause, mult, err := ReadConfig(context.Background(), store, "local")
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if pause != DefaultPause {
		t.Errorf("Pause kept malformed value: %v", pause)
	}
	if mult != DefaultMultiplier {
		t.Errorf("Multiplier kept negative value: %v", mult)
	}
}

func TestComputeACReturnsZero(t *testing.T) {
	store := newStore(t)
	setSetting(t, store, KeyBatteryPause, true)
	setSetting(t, store, KeyBatteryMultiplier, 7.0)

	p := Compute(context.Background(), store, "local", power.SourceAC, nil)
	if p.Pause || p.Multiplier != 0 {
		t.Errorf("AC compute = %+v, want zero", p)
	}
}

func TestComputeBatteryUsesConfig(t *testing.T) {
	store := newStore(t)
	setSetting(t, store, KeyBatteryMultiplier, 8.0)

	p := Compute(context.Background(), store, "local", power.SourceBattery, nil)
	if p.Multiplier != 8.0 {
		t.Errorf("Battery Multiplier = %v, want 8.0", p.Multiplier)
	}
}

func TestNewReaderTracksSource(t *testing.T) {
	store := newStore(t)
	setSetting(t, store, KeyBatteryMultiplier, 4.0)

	source := power.SourceAC
	reader := NewReader(store, "local", func() power.Source { return source }, nil)

	if got := reader(context.Background()); got.Multiplier != 0 {
		t.Errorf("AC: Multiplier = %v, want 0", got.Multiplier)
	}
	source = power.SourceBattery
	if got := reader(context.Background()); got.Multiplier != 4.0 {
		t.Errorf("Battery: Multiplier = %v, want 4.0", got.Multiplier)
	}
}

func TestNewReaderNilSourceIsNoop(t *testing.T) {
	store := newStore(t)
	reader := NewReader(store, "local", nil, nil)
	if got := reader(context.Background()); got.Pause || got.Multiplier != 0 {
		t.Errorf("nil-source returned non-zero: %+v", got)
	}
}
