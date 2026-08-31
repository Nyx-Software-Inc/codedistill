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

// Shared power state for PowerBadge and IndexingSettings.
//
// Both components previously ran their own timers: each polled getPowerSource
// every 10s (two independent pollers for one OS fact whenever Settings was
// open), and PowerBadge additionally re-read the battery-pause SETTING every
// 30s. That third timer was polling the app's own state — the badge learned
// that the user had flipped a toggle in the same browser tab by asking the
// server 2,880 times a day, and every one of those requests 404'd for anyone
// who had never opened Settings (CE-review item 43).
//
// This module replaces all three with one refcounted poller for the genuinely
// external fact, and a plain store for the local one.

import { readable, writable, get } from 'svelte/store';
import * as api from './api';
import { loadUserBool, saveUserBool } from './userSettings';

// Mirrors throttle.KeyBatteryPause. The VALUE is no longer duplicated here —
// the server serves its own default (settingDefaults in internal/api/settings.go),
// so there is one definition of what CodeDistill ships with.
export const KEY_BATTERY_PAUSE = 'indexing.battery.pause';

const SOURCE_POLL_MS = 10_000;

// powerSource polls AC-vs-battery, which is the one thing here that genuinely
// changes outside the app (you unplug the laptop). readable's start function
// runs on the FIRST subscriber and its teardown on the LAST, so any number of
// components share a single interval instead of one each.
export const powerSource = readable<api.PowerSource>('unknown', (set) => {
  let stopped = false;
  const tick = async () => {
    try {
      const r = await api.getPowerSource();
      if (!stopped) set(r.source);
    } catch {
      if (!stopped) set('unknown');
    }
  };
  void tick();
  const id = setInterval(() => void tick(), SOURCE_POLL_MS);
  return () => {
    stopped = true;
    clearInterval(id);
  };
});

// batteryPause needs no polling at all: it only changes when this app changes
// it, so the writer updates the store and every reader re-renders in the same
// tick. null means "not loaded yet" so the badge can avoid rendering a label
// derived from a value it hasn't fetched.
export const batteryPause = writable<boolean | null>(null);

let loadStarted = false;

// loadBatteryPause fetches once per page load. Idempotent, so every component
// that needs the value can call it without coordinating.
export async function loadBatteryPause(): Promise<void> {
  if (loadStarted) return;
  loadStarted = true;
  // The `false` here is a LAST-RESORT value for a failed request, not the
  // product default — since the settings API serves throttle.DefaultPause for
  // this key, a successful response always carries the real one.
  batteryPause.set(await loadUserBool(KEY_BATTERY_PAUSE, false));
}

// setBatteryPause updates every subscriber immediately, then persists. The
// optimistic order is deliberate: the toggle should feel instant, and
// saveUserBool is already fire-and-forget.
export function setBatteryPause(value: boolean): void {
  batteryPause.set(value);
  saveUserBool(KEY_BATTERY_PAUSE, value);
}

// currentBatteryPause is for non-reactive callers (tests, imperative code).
export function currentBatteryPause(): boolean {
  return get(batteryPause) ?? false;
}
