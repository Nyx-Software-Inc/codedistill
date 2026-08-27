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
  import {
    loadTypeColors,
    saveTypeColors,
    DEFAULT_TYPE_COLORS,
    type TypeColorsConfig,
  } from '../lib/typeColors';
  import {
    loadDueDateColors,
    saveDueDateColors,
    DEFAULT_DUE_DATE_COLORS,
    type DueDateColorsConfig,
  } from '../lib/dueDateColors';

  // Canvas behavior settings. Restack layout itself moved onto the
  // Restack button's ▾ menu (remembered per scratchpad); this panel
  // holds the knobs that aren't per-pad.

  // UC-18: per-type card colors.
  const TYPE_LABELS: [string, string][] = [
    ['todo', 'Todos'],
    ['bug', 'Bugs'],
    ['kb', 'KB'],
    ['use_case', 'Use cases'],
    ['unclassified', 'Unclassified'],
  ];
  let tc = $state<TypeColorsConfig>({ ...DEFAULT_TYPE_COLORS, colors: { ...DEFAULT_TYPE_COLORS.colors } });
  // UC-48: due-date header coloring toggle.
  let dd = $state<DueDateColorsConfig>({ ...DEFAULT_DUE_DATE_COLORS });
  onMount(async () => {
    tc = await loadTypeColors();
    dd = await loadDueDateColors();
  });
  function ddToggle(checked: boolean) {
    dd = { ...dd, enabled: checked };
    saveDueDateColors(dd);
  }
  function tcToggle(checked: boolean) {
    tc = { ...tc, enabled: checked };
    saveTypeColors(tc);
  }
  function tcColor(type: string, value: string) {
    tc = { ...tc, colors: { ...tc.colors, [type]: value } };
    saveTypeColors(tc);
  }
  function tcReset() {
    tc = { ...tc, colors: { ...DEFAULT_TYPE_COLORS.colors } };
    saveTypeColors(tc);
  }
</script>

<div class="canvas-settings">
  <div class="field">
    <label class="checkbox">
      <input
        type="checkbox"
        checked={tc.enabled}
        onchange={(e) => tcToggle((e.currentTarget as HTMLInputElement).checked)}
      />
      Color cards by item type
    </label>
    <p class="hint sub">
      Tints each card's background by its classified type (~30% blend,
      so text stays readable in both themes). Groups get a fixed gray.
      Changes apply when you close settings.
    </p>
    {#if tc.enabled}
      <div class="color-grid">
        {#each TYPE_LABELS as [type, label] (type)}
          <label class="color-row">
            <input
              type="color"
              value={tc.colors[type] ?? '#5a5a5a'}
              onchange={(e) => tcColor(type, (e.currentTarget as HTMLInputElement).value)}
            />
            <span>{label}</span>
          </label>
        {/each}
        <button class="reset" onclick={tcReset}>Reset to defaults</button>
      </div>
    {/if}
  </div>

  <div class="field">
    <label class="checkbox">
      <input
        type="checkbox"
        checked={dd.enabled}
        onchange={(e) => ddToggle((e.currentTarget as HTMLInputElement).checked)}
      />
      Color item headers by due date
    </label>
    <p class="hint sub">
      Tints a card's header bar yellow when its due date is within 48 hours,
      and red within 24 hours or once overdue. Uses the item's todo due date.
      Changes apply when you close settings.
    </p>
  </div>

  <p class="hint">
    Restack layout is chosen on the canvas: the ▾ next to
    <strong>▤ Restack</strong> offers <em>Tidy</em>, <em>Fit to
    content</em>, and <em>2 / 3 columns</em>; each scratchpad remembers
    its last choice. New text cards are sized to their content
    automatically.
  </p>
</div>

<style>
  .canvas-settings { display: flex; flex-direction: column; gap: 14px; }
  .hint {
    color: var(--p-888888);
    font-size: 12px;
    line-height: 1.5;
    margin: 0;
  }
  .hint strong, .hint em { color: var(--p-aaaaaa); }
  .hint.sub { margin-top: 2px; }
  .field { display: flex; flex-direction: column; gap: 4px; }
  .color-grid {
    display: flex;
    flex-wrap: wrap;
    gap: 10px;
    align-items: center;
    margin-top: 6px;
  }
  .color-row {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    color: var(--p-bbbbbb);
    font-size: 12px;
    cursor: pointer;
  }
  .color-row input[type='color'] {
    width: 26px;
    height: 20px;
    padding: 0;
    border: 1px solid var(--p-333333);
    border-radius: 3px;
    background: transparent;
    cursor: pointer;
  }
  .reset {
    background: transparent;
    border: 1px solid var(--p-333333);
    color: var(--p-999999);
    font-size: 11px;
    padding: 3px 10px;
    border-radius: 3px;
    cursor: pointer;
  }
  .reset:hover { color: var(--p-dddddd); }
  .checkbox {
    display: flex;
    align-items: center;
    gap: 8px;
    color: var(--p-dddddd);
    font-size: 13px;
    cursor: pointer;
  }
</style>
