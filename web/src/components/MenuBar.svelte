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
    commandsById, isEnabled, isVisible, runCommand, shortcutLabel, type Command,
  } from '../lib/commands.svelte';

  // The menubar (docs/design/menubar.md): a low strip of menus rendered from
  // the command registry. Each menu states its own order as command ids, '-'
  // for a separator; an id nothing registered is simply absent, so a menu
  // gains a command when its owner registers it. Keyboard: F10 (or Alt alone)
  // focuses the bar; arrows move; Enter runs; Escape closes. Alt+letters stay
  // the browser's.

  export type Menu = { label: string; items: string[] };
  /** compact folds every menu into one button whose list carries each
   *  menu's name as a heading: what a narrow window gets instead of menus
   *  that do not fit. */
  type Props = { menus: Menu[]; compact?: boolean };
  let { menus: given, compact = false }: Props = $props();
  const menus = $derived(compact ? [{ label: '☰', items: given.flatMap((m) => ['#' + m.label, ...m.items, '-']) }] : given);

  let open = $state(-1); // which menu is open; -1 none
  let focusRow = $state(-1); // highlighted row in the open menu
  let barEl: HTMLDivElement;
  const btns: HTMLButtonElement[] = [];

  type Row = { sep: true; heading?: string } | { sep: false; cmd: Command };
  function rowsOf(m: Menu): Row[] {
    const out: Row[] = [];
    for (const id of m.items) {
      if (id.startsWith('#')) {
        // A heading (compact mode): the menu its rows came from.
        while (out.length && out[out.length - 1].sep) out.pop();
        out.push({ sep: true, heading: id.slice(1) });
        continue;
      }
      if (id === '-') {
        if (out.length && !out[out.length - 1].sep) out.push({ sep: true });
        continue;
      }
      const [cmd] = commandsById([id]);
      if (cmd && isVisible(cmd)) out.push({ sep: false, cmd });
    }
    while (out.length && out[out.length - 1].sep) out.pop();
    // A heading left with no rows under it says nothing.
    return out.filter((r, i) => !(r.sep && r.heading && (i + 1 >= out.length || out[i + 1].sep)));
  }
  const menuRows = $derived(menus.map(rowsOf));

  function openMenu(i: number, row = -1) {
    open = i;
    focusRow = row;
  }
  function close(refocus = false) {
    const was = open;
    open = -1;
    focusRow = -1;
    if (refocus && was >= 0) btns[was]?.focus();
  }
  function run(cmd: Command) {
    if (!isEnabled(cmd)) return;
    close();
    runCommand(cmd.id);
  }
  function step(dir: 1 | -1) {
    const rows = menuRows[open] ?? [];
    for (let n = 0, i = focusRow; n < rows.length; n++) {
      i = (i + dir + rows.length) % rows.length;
      const r = rows[i];
      if (!r.sep && isEnabled(r.cmd)) {
        focusRow = i;
        return;
      }
    }
  }
  function onBarKey(e: KeyboardEvent) {
    if (open < 0 && !btns.includes(document.activeElement as HTMLButtonElement)) return;
    const cur = open >= 0 ? open : btns.indexOf(document.activeElement as HTMLButtonElement);
    switch (e.key) {
      case 'ArrowRight':
      case 'ArrowLeft': {
        e.preventDefault();
        const next = (cur + (e.key === 'ArrowRight' ? 1 : -1) + menus.length) % menus.length;
        btns[next]?.focus();
        if (open >= 0) openMenu(next);
        break;
      }
      case 'ArrowDown':
        e.preventDefault();
        if (open < 0) openMenu(cur);
        step(1);
        break;
      case 'ArrowUp':
        e.preventDefault();
        if (open < 0) openMenu(cur);
        step(-1);
        break;
      case 'Enter':
      case ' ': {
        if (open < 0) return; // the button's own click opens it
        e.preventDefault();
        const r = menuRows[open]?.[focusRow];
        if (r && !r.sep) run(r.cmd);
        break;
      }
      case 'Escape':
        e.preventDefault();
        close(true);
        break;
    }
  }

  onMount(() => {
    let altAlone = false;
    const down = (e: KeyboardEvent) => {
      if (e.key === 'F10' && !e.ctrlKey && !e.metaKey && !e.altKey && !e.shiftKey) {
        e.preventDefault();
        btns[0]?.focus();
        return;
      }
      altAlone = e.key === 'Alt';
    };
    const up = (e: KeyboardEvent) => {
      // Alt pressed and released on its own focuses the bar, as on a desktop.
      if (e.key === 'Alt' && altAlone) {
        e.preventDefault();
        if (barEl.contains(document.activeElement)) (document.activeElement as HTMLElement).blur();
        else btns[0]?.focus();
      }
      altAlone = false;
    };
    const outside = (e: PointerEvent) => {
      if (open >= 0 && !barEl.contains(e.target as Node)) close();
    };
    window.addEventListener('keydown', down);
    window.addEventListener('keyup', up);
    window.addEventListener('pointerdown', outside);
    return () => {
      window.removeEventListener('keydown', down);
      window.removeEventListener('keyup', up);
      window.removeEventListener('pointerdown', outside);
    };
  });
</script>

<!-- svelte-ignore a11y_no_static_element_interactions -->
<div class="menubar" role="menubar" tabindex="-1" bind:this={barEl} onkeydown={onBarKey}>
  {#each menus as m, i (m.label)}
    <div class="menu">
      <button
        bind:this={btns[i]}
        class="menu-btn"
        class:open={open === i}
        role="menuitem"
        aria-haspopup="menu"
        aria-expanded={open === i}
        aria-label={compact ? 'Menu' : undefined}
        title={compact ? 'Menu' : undefined}
        onclick={() => (open === i ? close() : openMenu(i))}
        onpointerenter={() => { if (open >= 0 && open !== i) openMenu(i); }}
      >{m.label}</button>
      {#if open === i}
        <div class="dropdown" role="menu" aria-label={m.label}>
          {#each menuRows[i] as r, j}
            {#if r.sep && r.heading}
              <div class="heading" role="presentation">{r.heading}</div>
            {:else if r.sep}
              <div class="sep" role="separator"></div>
            {:else}
              {@const on = r.cmd.active?.() ?? false}
              {@const n = r.cmd.badge?.() ?? 0}
              <button
                class="row"
                class:focus={focusRow === j}
                role="menuitemcheckbox"
                aria-checked={on}
                disabled={!isEnabled(r.cmd)}
                title={r.cmd.description ?? ''}
                onpointerenter={() => (focusRow = j)}
                onclick={() => run(r.cmd)}
              >
                <span class="tick" aria-hidden="true">{on ? '✓' : ''}</span>
                <span class="label">{r.cmd.label}</span>
                {#if n > 0}<span class="badge">{n}</span>{/if}
                <span class="keys">{shortcutLabel(r.cmd)}</span>
              </button>
            {/if}
          {/each}
          {#if menuRows[i].length === 0}<div class="empty">Nothing here yet</div>{/if}
        </div>
      {/if}
    </div>
  {/each}
</div>

<style>
  .menubar { display: flex; align-items: stretch; height: 100%; gap: 1px; }
  .menu { position: relative; display: flex; }
  .menu-btn {
    background: none; border: 0; border-radius: 4px; color: var(--text-dim);
    font: inherit; font-size: 0.8rem; padding: 0 0.55rem; cursor: default; height: 100%;
  }
  .menu-btn:hover, .menu-btn.open, .menu-btn:focus-visible { background: var(--p-2a2a2a); color: var(--text); outline: none; }
  .dropdown {
    position: absolute; top: calc(100% + 2px); left: 0; z-index: 1000; min-width: 16rem;
    background: var(--p-161616); border: 1px solid var(--p-333333); border-radius: 6px;
    box-shadow: 0 8px 24px rgba(0, 0, 0, 0.45); padding: 4px; display: flex; flex-direction: column;
  }
  .row {
    display: grid; grid-template-columns: 1rem 1fr auto auto; align-items: center; gap: 0.5rem;
    background: none; border: 0; border-radius: 4px; color: var(--text); font: inherit; font-size: 0.8rem;
    padding: 0.3rem 0.6rem 0.3rem 0.4rem; text-align: left; cursor: default;
  }
  .row.focus:not(:disabled) { background: var(--p-2d5578); }
  .row:disabled { color: var(--p-666666); }
  .tick { color: var(--accent); font-size: 0.75rem; text-align: center; }
  .keys { color: var(--text-dim); font-size: 0.72rem; padding-left: 1.2rem; }
  .badge { background: var(--accent); color: #fff; border-radius: 8px; font-size: 0.66rem; padding: 0 0.35rem; }
  .sep { height: 1px; background: var(--p-2a2a2a); margin: 4px 0.4rem; }
  .empty { color: var(--text-dim); font-size: 0.78rem; padding: 0.3rem 0.6rem; }
  .heading {
    color: var(--text-dim); font-size: 0.68rem; text-transform: uppercase; letter-spacing: 0.06em;
    padding: 0.45rem 0.6rem 0.15rem 1.9rem;
  }
  .heading:first-child { padding-top: 0.2rem; }
  .dropdown { max-height: calc(100vh - 60px); overflow-y: auto; }
</style>
