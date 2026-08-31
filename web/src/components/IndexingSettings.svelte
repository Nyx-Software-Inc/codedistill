<!-- =============================================================================
  Copyright (c) 2026 Nyx Software, Inc.  All rights reserved.

  CodeDistill

  Property of Nyx Software, Inc., provided under a dual license: the GNU Affero General
  Public License v3.0 (see the LICENSE file) and, separately, a commercial
  license available from Nyx Software, Inc. Use outside the terms of one of those
  licenses is prohibited.

  SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-Nyx-Commercial
============================================================================= -->

<script lang="ts">
  import { onMount } from 'svelte';
  import { powerSource, batteryPause, loadBatteryPause, setBatteryPause } from '../lib/power';
  import { loadUserNum, saveUserNum } from '../lib/userSettings';

  // Setting keys mirror codedistill/internal/throttle/throttle.go.
  // The "indexing" namespace is a historical name — the same keys
  // also drive the matcher watcher's cadence.
  const KEY_MULTIPLIER = 'indexing.battery.multiplier';

  // Slider BOUNDS still mirror MinMultiplier / MaxMultiplier in throttle.go —
  // those are validation limits, not defaults, and the settings API does not
  // serve them. The DEFAULT no longer lives here: the server returns
  // throttle.DefaultMultiplier for an unset key, so the value below is only a
  // last-resort fallback for a failed request (CE-review item 43).
  const MIN_MULTIPLIER = 1;
  const MAX_MULTIPLIER = 11;
  const MULTIPLIER_FALLBACK = 5;

  let multiplier = $state(MULTIPLIER_FALLBACK);
  let loading = $state(true);

  // One shared poller for the power source (lib/power.ts) instead of this
  // component running a second 10s interval alongside PowerBadge's.
  let source = $derived($powerSource);
  let pause = $derived($batteryPause ?? false);

  async function loadAll() {
    await loadBatteryPause();
    multiplier = await loadUserNum(KEY_MULTIPLIER, MULTIPLIER_FALLBACK);
    loading = false;
  }

  onMount(() => {
    void loadAll();
  });

  function clamp(n: number, min: number, max: number) {
    if (!Number.isFinite(n)) return min;
    return Math.max(min, Math.min(max, Math.round(n)));
  }

  // Pushes into the shared store first, so PowerBadge re-renders immediately
  // rather than discovering the change on its next poll.
  function onPauseToggle(checked: boolean) {
    setBatteryPause(checked);
  }

  function onMultiplierInput(raw: number) {
    multiplier = clamp(raw, MIN_MULTIPLIER, MAX_MULTIPLIER);
    saveUserNum(KEY_MULTIPLIER, multiplier);
  }

  // Computed examples so users see what the slider does in real units.
  // Indexer base tick is 60 s; matcher base tick is 15 min. Per-call
  // pace bases are 3 s (per embed) and 1 s (per LLM judge).
  let indexerInterval = $derived(60 * multiplier);
  let matcherInterval = $derived(15 * multiplier);
  let chunkPace = $derived(multiplier > 1 ? 3 * multiplier : 0);
  let judgePace = $derived(multiplier > 1 ? 1 * multiplier : 0);

  function fmtSeconds(s: number): string {
    if (s < 60) return `${s}s`;
    const m = Math.round(s / 60);
    return m === 1 ? '1 min' : `${m} min`;
  }
</script>

<div class="indexing-settings">
  <div class="status-line" data-source={source}>
    <span class="dot" aria-hidden="true"></span>
    {#if source === 'battery'}
      On battery — throttle policy is active
    {:else if source === 'ac'}
      On AC power — background work runs at full speed
    {:else}
      Power source unknown — no throttle applied
    {/if}
  </div>

  <p class="hint">
    Background work — code indexing (chunking + embedding) and the
    implementation matcher (LLM-judging commits against open items)
    — can be slowed down or paused on battery so it doesn't drain the
    laptop during development. Settings only affect behavior on
    battery; on AC, everything runs at full speed regardless.
  </p>

  <div class="field">
    <label class="checkbox">
      <input
        type="checkbox"
        checked={pause}
        disabled={loading}
        onchange={(e) => onPauseToggle((e.currentTarget as HTMLInputElement).checked)}
      />
      Pause all background work entirely when on battery
    </label>
    <p class="hint sub">
      When enabled, both the indexer and the matcher stop between
      cycles and won't make any embed or LLM calls until you're
      plugged back in.
    </p>
  </div>

  <div class="field">
    <label for="throttle-multiplier">
      Battery throttle: <strong>{multiplier}× slower</strong>
    </label>
    <input
      id="throttle-multiplier"
      type="range"
      min={MIN_MULTIPLIER}
      max={MAX_MULTIPLIER}
      step="1"
      value={multiplier}
      disabled={loading || pause}
      oninput={(e) => onMultiplierInput((e.currentTarget as HTMLInputElement).valueAsNumber)}
    />
    <div class="scale-marks" aria-hidden="true">
      <span>1× off</span>
      <span>{MAX_MULTIPLIER}× max</span>
    </div>
    <p class="hint sub">
      Stretches every background timer by this factor when on battery.
      At {multiplier}× the indexer ticks every {fmtSeconds(indexerInterval)}
      (vs 1 min on AC) and the matcher every {matcherInterval} min
      (vs 15 min on AC){chunkPace > 0
        ? `, with ${chunkPace}s between embeds and ${judgePace}s between LLM judges`
        : ''}.
    </p>
  </div>
</div>

<style>
  .indexing-settings { display: flex; flex-direction: column; gap: 16px; }

  .status-line {
    display: flex; align-items: center; gap: 8px;
    padding: 8px 12px;
    border-radius: 6px;
    background: rgba(0, 0, 0, 0.04);
    font-size: 13px;
  }
  .status-line .dot {
    display: inline-block;
    width: 8px; height: 8px;
    border-radius: 50%;
    background: var(--p-888888);
  }
  .status-line[data-source="ac"] .dot { background: var(--p-4caf50); }
  .status-line[data-source="battery"] .dot { background: var(--p-ff9800); }

  .hint {
    color: var(--p-888888);
    font-size: 12px;
    line-height: 1.4;
    margin: 0;
  }
  .hint.sub { margin-top: 2px; }

  .field { display: flex; flex-direction: column; gap: 4px; }
  .field label { font-size: 13px; font-weight: 500; }
  .field label.checkbox { font-weight: 400; cursor: pointer; }
  .field label strong { font-weight: 600; }
  .field input[type="range"] {
    width: 100%;
    max-width: 320px;
  }
  .field input[type="checkbox"] {
    margin-right: 6px;
    vertical-align: middle;
  }
  .field input:disabled {
    opacity: 0.6;
    cursor: not-allowed;
  }

  .scale-marks {
    display: flex;
    justify-content: space-between;
    max-width: 320px;
    font-size: 11px;
    color: var(--p-888888);
    margin-top: -2px;
  }
</style>
