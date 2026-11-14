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
  import { tick } from 'svelte';
  import { allCommands, isEnabled, isVisible, runCommand, shortcutLabel, type Command } from '../lib/commands.svelte';

  // The command palette (Mod+K): every command the registry knows, found by
  // typing part of its name. The menus are for browsing; this is for when
  // you know what you want. Disabled commands are listed, greyed, so "why
  // can't I do X" has an answer.

  type Props = { open: boolean; onClose: () => void; menuOf: (id: string) => string };
  let { open, onClose, menuOf }: Props = $props();

  let query = $state('');
  let sel = $state(0);
  let input = $state<HTMLInputElement | null>(null);

  /** score ranks a command for the query: every query word must appear in
   *  its label, menu or description; a match at the start of the label
   *  ranks highest. -1: no match. */
  function score(c: Command, q: string): number {
    if (!q) return 0;
    const label = c.label.toLowerCase();
    const hay = `${label} ${menuOf(c.id).toLowerCase()} ${(c.description ?? '').toLowerCase()}`;
    let s = 0;
    for (const w of q.toLowerCase().split(/\s+/).filter(Boolean)) {
      const i = hay.indexOf(w);
      if (i < 0) return -1;
      s += label.startsWith(w) ? 0 : label.includes(w) ? 1 : 3;
    }
    return s;
  }
  const results = $derived.by(() => {
    const q = query.trim();
    return allCommands()
      .filter((c) => isVisible(c) && !c.id.startsWith('view.palette'))
      .map((c) => ({ c, s: score(c, q) }))
      .filter((x) => x.s >= 0)
      .sort((a, b) => a.s - b.s || Number(isEnabled(b.c)) - Number(isEnabled(a.c)) || a.c.label.localeCompare(b.c.label))
      .slice(0, 40)
      .map((x) => x.c);
  });

  $effect(() => {
    if (open) {
      query = '';
      sel = 0;
      void tick().then(() => input?.focus());
    }
  });
  $effect(() => {
    void query;
    sel = 0;
  });

  function pick(c: Command | undefined) {
    if (!c || !isEnabled(c)) return;
    onClose();
    runCommand(c.id);
  }
  function onKey(e: KeyboardEvent) {
    if (e.key === 'ArrowDown') { e.preventDefault(); sel = Math.min(sel + 1, results.length - 1); }
    else if (e.key === 'ArrowUp') { e.preventDefault(); sel = Math.max(sel - 1, 0); }
    else if (e.key === 'Enter') { e.preventDefault(); pick(results[sel]); }
    else if (e.key === 'Escape') { e.preventDefault(); onClose(); }
  }
</script>

{#if open}
  <!-- svelte-ignore a11y_click_events_have_key_events -->
  <div class="backdrop" role="presentation" onclick={onClose}>
    <!-- svelte-ignore a11y_click_events_have_key_events -->
    <div class="palette" role="dialog" tabindex="-1" aria-label="Command palette" onclick={(e) => e.stopPropagation()}>
      <input
        bind:this={input}
        bind:value={query}
        onkeydown={onKey}
        placeholder="Type a command…"
        aria-label="Search commands"
        spellcheck="false"
      />
      <div class="list" role="listbox">
        {#each results as c, i (c.id)}
          <button
            class="row"
            class:sel={i === sel}
            role="option"
            aria-selected={i === sel}
            disabled={!isEnabled(c)}
            onpointerenter={() => (sel = i)}
            onclick={() => pick(c)}
          >
            <span class="label">{c.label}</span>
            <span class="menu">{menuOf(c.id)}</span>
            <span class="keys">{shortcutLabel(c)}</span>
          </button>
        {/each}
        {#if results.length === 0}<div class="none">No command matches “{query}”.</div>{/if}
      </div>
    </div>
  </div>
{/if}

<style>
  .backdrop { position: fixed; inset: 0; z-index: 2000; background: rgba(0, 0, 0, 0.25); }
  .palette {
    position: absolute; top: 12vh; left: 50%; transform: translateX(-50%); width: min(560px, calc(100vw - 32px));
    background: var(--p-161616); border: 1px solid var(--p-3a3a3a); border-radius: 8px;
    box-shadow: 0 16px 40px rgba(0, 0, 0, 0.5); overflow: hidden;
  }
  input {
    width: 100%; box-sizing: border-box; border: 0; border-bottom: 1px solid var(--p-2a2a2a);
    background: transparent; color: var(--text); font: inherit; font-size: 0.95rem; padding: 0.7rem 0.9rem; outline: none;
  }
  .list { max-height: 50vh; overflow-y: auto; padding: 4px; display: flex; flex-direction: column; }
  .row {
    display: grid; grid-template-columns: 1fr auto auto; gap: 0.8rem; align-items: center;
    background: none; border: 0; border-radius: 4px; color: var(--text); font: inherit; font-size: 0.85rem;
    padding: 0.4rem 0.6rem; text-align: left; cursor: default;
  }
  .row.sel:not(:disabled) { background: var(--p-2d5578); }
  .row:disabled { color: var(--p-666666); }
  .menu { color: var(--text-dim); font-size: 0.72rem; }
  .keys { color: var(--text-dim); font-size: 0.72rem; min-width: 3rem; text-align: right; }
  .none { color: var(--text-dim); font-size: 0.82rem; padding: 0.6rem; }
</style>
