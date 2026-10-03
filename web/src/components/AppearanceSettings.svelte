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
  import * as api from '../lib/api';
  import { currentThemeMode, setThemeMode, type ThemeMode } from '../lib/theme';
  import { currentFonts, setItemFont, setCodeFont, DEFAULT_ITEM_SIZE, DEFAULT_CODE_SIZE } from '../lib/fonts';
  import { uiPrefs, setActivityWave, setItemNumberPrefix, type ItemNumberPrefix,
           setSortProjectsAlphabetically, setDefaultCanvasView,
           CANVAS_VIEWS, type CanvasView } from '../lib/uiPrefs.svelte';

  // Appearance tab (UC-7): three-way theme mode. The choice applies
  // instantly (no reload) and persists as the ui.theme user setting.

  // Alternative scratchpad views are a paid feature (canvas_views), and the
  // canvas is forced when it is off — so a free install must not be offered a
  // choice the gate will then ignore. Null means "not yet known", treated as
  // licensed to avoid flashing a disabled control at a paying user while the
  // fetch is in flight; the server enforces regardless.
  let license = $state<api.LicenseInfo | null>(null);
  const viewsLicensed = $derived(license === null || license.features.includes('canvas_views'));

  let mode = $state<ThemeMode>('dark');

  // User-selectable fonts. Combo boxes (datalist) = curated suggestions + type
  // your own. Empty family = the surface default. All system fonts, no bundling.
  let itemFamily = $state('');
  let itemSize = $state(DEFAULT_ITEM_SIZE);
  let codeFamily = $state('');
  let codeSize = $state(DEFAULT_CODE_SIZE);

  const SIZES = [10, 11, 12, 13, 14, 15, 16, 18, 20];
  // Scratchpad items: any family.
  const ITEM_FONTS = ['system-ui', 'Inter', 'Helvetica Neue', 'Arial', 'Georgia', 'Times New Roman', 'ui-monospace'];
  // Code surfaces: monospace only.
  const CODE_FONTS = ['ui-monospace', 'SF Mono', 'Menlo', 'Consolas', 'JetBrains Mono', 'Fira Code', 'Cascadia Code', 'Source Code Pro', 'IBM Plex Mono', 'Roboto Mono'];

  onMount(() => {
    void api.getLicense().then((l) => (license = l)).catch(() => { license = null; });
    mode = currentThemeMode();
    const f = currentFonts();
    itemFamily = f.itemFamily;
    itemSize = f.itemSize;
    codeFamily = f.codeFamily;
    codeSize = f.codeSize;
  });

  function applyItem() { setItemFont(itemFamily.trim(), itemSize); }
  function applyCode() { setCodeFont(codeFamily.trim(), codeSize); }

  const MODES: { m: ThemeMode; label: string; hint: string }[] = [
    { m: 'dark', label: 'Dark', hint: 'The classic CodeDistill look (default)' },
    { m: 'light', label: 'Light', hint: 'Light palette — same hues, flipped lightness' },
    { m: 'system', label: 'System', hint: 'Follow the operating system preference, live' },
  ];

  function pick(m: ThemeMode) {
    mode = m;
    setThemeMode(m);
  }

  // UC-65: how a work item's number prefixes its title in the item views.
  // Labels double as live samples of each mode.
  const PREFIX_MODES: { mode: ItemNumberPrefix; label: string; hint: string }[] = [
    { mode: 'off', label: 'Off', hint: 'No number prefix' },
    { mode: 'number', label: '#95', hint: 'Just the number' },
    { mode: 'bracket', label: '[95]', hint: 'Bracketed number' },
    { mode: 'typed', label: 'BUG-95', hint: 'Type + number' },
  ];
</script>

<div class="appearance">
  <p class="hint">Color theme for the whole app. Applies immediately.</p>
  <div class="seg" role="radiogroup" aria-label="Theme mode">
    {#each MODES as { m, label, hint } (m)}
      <button
        type="button"
        class:on={mode === m}
        onclick={() => pick(m)}
        aria-pressed={mode === m}
        title={hint}
      >{label}</button>
    {/each}
  </div>

  <div class="fld">
    <div class="fld-label">Scratchpad item font</div>
    <p class="hint">Applies to item content on the canvas. Leave the name blank for the default; type any installed font or pick a suggestion.</p>
    <div class="fld-row">
      <input class="font-in" list="item-fonts" placeholder="Default" bind:value={itemFamily} onchange={applyItem} aria-label="Scratchpad item font family" />
      <datalist id="item-fonts">{#each ITEM_FONTS as f (f)}<option value={f}></option>{/each}</datalist>
      <select class="size-in" bind:value={itemSize} onchange={applyItem} aria-label="Scratchpad item font size">
        {#each SIZES as s (s)}<option value={s}>{s}px</option>{/each}
      </select>
    </div>
  </div>

  <div class="fld">
    <div class="fld-label">Code font <span class="fld-note">file tree + code viewer · monospace</span></div>
    <p class="hint">A monospace font for code surfaces. Uninstalled fonts fall back to the default monospace stack.</p>
    <div class="fld-row">
      <input class="font-in" list="code-fonts" placeholder="Default monospace" bind:value={codeFamily} onchange={applyCode} aria-label="Code font family" />
      <datalist id="code-fonts">{#each CODE_FONTS as f (f)}<option value={f}></option>{/each}</datalist>
      <select class="size-in" bind:value={codeSize} onchange={applyCode} aria-label="Code font size">
        {#each SIZES as s (s)}<option value={s}>{s}px</option>{/each}
      </select>
    </div>
  </div>

  <div class="fld">
    <div class="fld-label">Background activity indicator</div>
    <p class="hint">The animated bar that sweeps across the top of the window while indexing and other background work runs. Buttons show their own busy states either way.</p>
    <label class="check-row">
      <input
        type="checkbox"
        checked={uiPrefs.activityWave}
        onchange={(e) => setActivityWave((e.currentTarget as HTMLInputElement).checked)}
      />
      <span>Show the activity bar</span>
    </label>
  </div>

  <div class="fld">
    <div class="fld-label">Project order</div>
    <p class="hint">Project lists read in the order you created them, which keeps recent work near where you left it. Sort them by name once there are enough that finding one matters more than remembering the last.</p>
    <label class="check-row">
      <input
        type="checkbox"
        checked={uiPrefs.sortProjectsAlphabetically}
        onchange={(e) => setSortProjectsAlphabetically((e.currentTarget as HTMLInputElement).checked)}
      />
      <span>Sort projects alphabetically</span>
    </label>
  </div>

  <div class="fld">
    <div class="fld-label">Default scratchpad view</div>
    <p class="hint">
      Which view a scratchpad opens in. The canvas is what the product is for; the
      others are different readings of the same items. Switching view on a pad
      still affects only that visit.
      {#if !viewsLicensed}
        <strong class="gated">List, Kanban and Calendar are part of Pro — the canvas is always available.</strong>
      {/if}
    </p>
    <select
      value={uiPrefs.defaultCanvasView}
      disabled={!viewsLicensed}
      onchange={(e) => setDefaultCanvasView((e.currentTarget as HTMLSelectElement).value as CanvasView)}
    >
      {#each CANVAS_VIEWS as v (v)}
        <option value={v}>{v === 'canvas' ? 'Canvas (default)' : v.charAt(0).toUpperCase() + v.slice(1)}</option>
      {/each}
    </select>
  </div>

  <div class="fld">
    <div class="fld-label">Item number prefix <span class="fld-note">canvas · list · calendar</span></div>
    <p class="hint">Show a work item's number before its title in the item views. Applies instantly; the type badge is still shown separately.</p>
    <div class="seg" role="radiogroup" aria-label="Item number prefix">
      {#each PREFIX_MODES as pm (pm.mode)}
        <button
          type="button"
          class:on={uiPrefs.itemNumberPrefix === pm.mode}
          onclick={() => setItemNumberPrefix(pm.mode)}
          aria-pressed={uiPrefs.itemNumberPrefix === pm.mode}
          title={pm.hint}
        >{pm.label}</button>
      {/each}
    </div>
  </div>
</div>

<style>
  .appearance { display: flex; flex-direction: column; gap: 12px; }
  .hint {
    color: var(--p-888888);
    font-size: 12px;
    line-height: 1.4;
    margin: 0;
  }
  .fld { display: flex; flex-direction: column; gap: 6px; padding-top: 6px; border-top: 1px solid var(--p-262626); }
  .check-row {
    display: flex;
    align-items: center;
    gap: 8px;
    font-size: 12px;
    color: var(--p-cccccc);
    cursor: pointer;
    width: fit-content;
  }
  .check-row input[type="checkbox"] { accent-color: var(--p-66ccff); cursor: pointer; }
  .fld-label { font-size: 12px; font-weight: 600; color: var(--p-cccccc); }
  .fld-note { font-weight: 400; color: var(--p-777777); font-size: 11px; margin-left: 4px; }
  .fld-row { display: flex; gap: 8px; }
  .font-in {
    flex: 1; min-width: 0;
    background: var(--p-0a0a0a); border: 1px solid var(--p-333333); border-radius: 4px;
    color: var(--p-e0e0e0); padding: 6px 8px; font-size: 12px;
  }
  .size-in {
    background: var(--p-0a0a0a); border: 1px solid var(--p-333333); border-radius: 4px;
    color: var(--p-e0e0e0); padding: 6px 8px; font-size: 12px; cursor: pointer;
  }
  .seg { display: flex; gap: 4px; }
  .seg button {
    background: var(--p-1a1a1a);
    border: 1px solid var(--p-333333);
    color: var(--p-bbbbbb);
    padding: 6px 16px;
    font-size: 12px;
    border-radius: 4px;
    cursor: pointer;
  }
  .seg button:hover { color: var(--p-ffffff); }
  .seg button.on {
    background: var(--p-1e3a52);
    border-color: var(--p-2d5578);
    color: var(--p-99ccff);
  }
</style>
