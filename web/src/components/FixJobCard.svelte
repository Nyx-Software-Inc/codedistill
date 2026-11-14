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
  import { onMount, onDestroy } from 'svelte';
  import * as api from '../lib/api';
  import type { FixJobItem, AgentRun } from '../lib/api';

  // The expanded card of a Fix scanner findings run: what each item came to,
  // how CodeDistill checked it, and what each agent run took.
  //
  // It answers two questions in order. First, "what do I have to do?": an item
  // waiting on you says why, in full, and links to the item; the branch to
  // merge is one copy away. Then, "can I trust it?": every check, the
  // TestBuilder's tests, the reviewers' notes, and the agents' cost, so a fix
  // is judged on evidence rather than on "succeeded".

  type Props = {
    jobId: string;
    running: boolean;
    /** Open an item's own view, where its notes and activity live. */
    onOpenItem?: (ownerType: string, ownerId: string) => void;
  };
  let { jobId, running, onOpenItem }: Props = $props();

  let items = $state<FixJobItem[]>([]);
  let branch = $state('');
  let error = $state('');
  let loaded = $state(false);
  let openItem = $state<string | null>(null);
  let copied = $state('');
  let timer: ReturnType<typeof setInterval> | undefined;

  async function load() {
    try {
      const r = await api.getFixJobItems(jobId);
      items = r.items ?? [];
      branch = r.job?.branch ?? '';
      error = '';
      // The item in hand is the one worth looking at while a run goes on.
      if (openItem === null) {
        const pick = items.find((i) => i.status === 'running') ?? items.find((i) => i.status === 'waiting');
        if (pick) openItem = pick.id;
      }
    } catch (e) {
      error = e instanceof Error ? e.message : String(e);
    } finally {
      loaded = true;
    }
  }

  onMount(() => {
    void load();
    timer = setInterval(() => { if (running) void load(); }, 5000);
  });
  onDestroy(() => { if (timer) clearInterval(timer); });

  // One more load when the run ends, so the last item's outcome shows without
  // waiting on a poll that has stopped.
  let wasRunning: boolean | null = null;
  $effect(() => {
    if (wasRunning === true && !running) void load();
    wasRunning = running;
  });

  const fixed = $derived(items.filter((i) => i.status === 'fixed').length);
  const waiting = $derived(items.filter((i) => i.status === 'waiting').length);
  const labels = $derived([...new Set(items.flatMap((i) => i.labels ?? []))]);

  async function copy(what: string, text: string) {
    try {
      await navigator.clipboard.writeText(text);
      copied = what;
      setTimeout(() => { if (copied === what) copied = ''; }, 1500);
    } catch {
      copied = '';
    }
  }

  const ICON: Record<string, string> = {
    fixed: '✓', waiting: '⚠', running: '⏳', queued: '·', skipped: '–', cancelled: '×',
  };
  const STATUS: Record<string, string> = {
    fixed: 'fixed', waiting: 'waiting on you', running: 'working', queued: 'queued',
    skipped: 'skipped', cancelled: 'cancelled',
  };
  const CHECK: Record<string, string> = {
    finding_gone: 'finding gone', nothing_new: 'nothing new', tests: 'tests',
    tests_fail_before: 'new tests failed before the fix', claims: 'claims recorded',
  };
  const ROLE: Record<string, string> = { solutioner: 'fixer', challenger: 'reviewer', test_builder: 'TestBuilder' };

  function who(r: AgentRun): string {
    const role = r.lens ? `${r.lens} reviewer` : (ROLE[r.role] ?? r.role);
    return `${role} · ${r.phase} r${r.round}`;
  }
  function secs(ms: number): string {
    const s = Math.round(ms / 1000);
    return s < 60 ? `${s}s` : `${Math.floor(s / 60)}m ${s % 60}s`;
  }
  function k(n?: number): string {
    if (n == null) return '—';
    return n >= 1000 ? `${(n / 1000).toFixed(1)}k` : String(n);
  }
  function phaseLine(i: FixJobItem): string {
    const what = i.phase === 'plan' ? 'planning' : 'building';
    return `${what} · round ${i.rounds}`;
  }
  function testsLine(i: FixJobItem): string {
    const parts: string[] = [];
    if (i.tests.new) parts.push(`${i.tests.new} new (${i.tests.problem} aimed at the problem, ${i.tests.guard} guarding)`);
    const n = i.tests.obsolete.length;
    if (n) parts.push(`${n} existing no longer appl${n === 1 ? 'ies' : 'y'}: ${i.tests.obsolete.join(', ')}`);
    return parts.join(' · ');
  }
  /** Silent for 10 minutes or more: flagged, never stopped for it. */
  function silent(i: FixJobItem): boolean {
    if (i.status !== 'running' || !i.last_progress_at) return false;
    return Date.now() - new Date(i.last_progress_at).getTime() > 10 * 60 * 1000;
  }
</script>

<div class="fixcard">
  {#if branch}
    <div class="branch">
      <span class="lbl">Branch</span>
      <code>{branch}</code>
      <button class="mini" onclick={() => copy('branch', branch)}>{copied === 'branch' ? 'copied' : 'copy'}</button>
      <!-- Merging is offered once there is something finished to merge. -->
      {#if !running && fixed > 0}
        <code class="merge">git merge {branch}</code>
        <button class="mini" onclick={() => copy('merge', `git merge ${branch}`)}>{copied === 'merge' ? 'copied' : 'copy'}</button>
      {/if}
    </div>
  {/if}
  <div class="summary">
    {fixed} fixed · {waiting} waiting on you · {items.length} item{items.length === 1 ? '' : 's'}
    {#each labels as l}<span class="label" class:warn={l === 'uncontained'}>{l}</span>{/each}
  </div>
  {#if error}<p class="err" role="alert">{error}</p>{/if}
  {#if loaded && items.length === 0 && !error}<p class="muted">No items yet.</p>{/if}

  {#each items as i (i.id)}
    <div class="item {i.status}">
      <button class="irow" onclick={() => (openItem = openItem === i.id ? null : i.id)}>
        <span class="ic">{ICON[i.status] ?? '·'}</span>
        <span class="ilabel" title={i.label}>{i.label}</span>
        <span class="ist">
          {#if i.status === 'running'}{phaseLine(i)}{:else}{STATUS[i.status] ?? i.status}{i.rounds > 0 ? ` · ${i.rounds} round${i.rounds === 1 ? '' : 's'}` : ''}{/if}
        </span>
      </button>
      {#if i.status === 'running' && i.progress_note}
        <div class="progress" class:silent={silent(i)}>
          {#if silent(i)}silent for a while — last: {/if}{i.progress_note}
        </div>
      {/if}
      {#if i.reason && i.status !== 'fixed'}<p class="reason">{i.reason}</p>{/if}

      {#if openItem === i.id && i.status !== 'skipped' && i.status !== 'queued'}
        <div class="body">
          {#if i.checks.length}
            <div class="checks">
              {#each i.checks as c}
                <span class="chk" class:ok={c.pass} class:na={c.na} class:bad={!c.pass && !c.na} title={c.detail}>
                  {c.pass ? '✓' : c.na ? '–' : '✗'} {CHECK[c.name] ?? c.name}
                </span>
              {/each}
            </div>
          {/if}
          {#if i.tests.new || i.tests.obsolete.length}
            <div class="line">
              <span class="lbl">Tests</span>
              {testsLine(i)}
            </div>
          {/if}
          {#if i.notes}
            <div class="line">
              <span class="lbl">Notes</span>
              {i.notes} for you{i.top_note ? ` — most raised${(i.top_note_count ?? 0) > 1 ? ` (${i.top_note_count}×)` : ''}:` : ''}
              {#if i.top_note}<span class="top" title={i.top_note}>{i.top_note}</span>{/if}
            </div>
          {/if}
          {#if i.commit_sha}
            <div class="line"><span class="lbl">Commit</span><code>{i.commit_sha.slice(0, 10)}</code></div>
          {/if}
          {#if i.attempt_ref}
            <div class="line"><span class="lbl">Set aside</span><code>{i.attempt_ref}</code></div>
          {/if}
          {#if i.runs.length}
            <div class="runs-wrap"><table class="runs">
              <thead><tr><th>Agent</th><th>Model</th><th>Outcome</th><th>Time</th><th>Turns</th><th>Tokens out</th></tr></thead>
              <tbody>
                {#each i.runs as r (r.id)}
                  <tr>
                    <td>{who(r)}</td>
                    <td class="mono">{r.model || r.agent || '—'}</td>
                    <td>{r.outcome}</td>
                    <td>{secs(r.duration_ms)}</td>
                    <td>{r.turns ?? '—'}</td>
                    <td>{k(r.tokens_out)}</td>
                  </tr>
                {/each}
              </tbody>
            </table></div>
          {/if}
          {#if onOpenItem}
            <button class="mini open" onclick={() => onOpenItem?.(i.owner_type, i.owner_id)}>Open the item</button>
          {/if}
        </div>
      {/if}
    </div>
  {/each}
</div>

<style>
  .fixcard { padding: 0.2rem 0.7rem 0.7rem 2.3rem; display: flex; flex-direction: column; gap: 0.45rem; font-size: 0.8rem; }
  .branch { display: flex; flex-wrap: wrap; align-items: center; gap: 0.35rem; }
  .branch code { font-family: ui-monospace, monospace; font-size: 0.76rem; color: var(--text); }
  .branch .merge { color: var(--text-dim); margin-left: 0.5rem; }
  .lbl { color: var(--text-dim); margin-right: 0.35rem; }
  .mini { border: 1px solid var(--p-3a3a3a); background: var(--p-2a2a2a); color: var(--text-dim);
          border-radius: 3px; font: inherit; font-size: 0.7rem; padding: 0 0.35rem; cursor: pointer; }
  .mini:hover { color: var(--text); }
  .summary { color: var(--text-dim); display: flex; flex-wrap: wrap; gap: 0.4rem; align-items: center; }
  .label { border: 1px solid var(--p-3a3a3a); border-radius: 3px; padding: 0 0.3rem; font-size: 0.7rem; }
  .label.warn { color: var(--warn, #e0a030); border-color: var(--warn, #e0a030); }
  .err { color: var(--danger, #f88); }
  .muted { color: var(--text-dim); }
  .item { border-top: 1px solid var(--p-2a2a2a); padding-top: 0.35rem; }
  .irow { display: flex; align-items: baseline; gap: 0.45rem; width: 100%; background: none; border: 0;
          color: var(--text); font: inherit; text-align: left; cursor: pointer; padding: 0; }
  .ic { flex: 0 0 1rem; text-align: center; }
  .item.fixed .ic { color: var(--ok, #4caf72); }
  .item.waiting .ic { color: var(--warn, #e0a030); }
  .item.running .ic { color: var(--accent); }
  .item.skipped, .item.cancelled { color: var(--text-dim); }
  .ilabel { flex: 1 1 auto; min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap;
            font-family: ui-monospace, monospace; font-size: 0.76rem; }
  .ist { flex: 0 0 auto; color: var(--text-dim); font-size: 0.74rem; }
  .item.waiting .ist { color: var(--warn, #e0a030); }
  .progress { margin: 0.15rem 0 0 1.45rem; color: var(--text-dim); font-size: 0.74rem;
              overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .progress.silent { color: var(--warn, #e0a030); }
  .reason { margin: 0.2rem 0 0 1.45rem; color: var(--text-dim); font-size: 0.75rem; white-space: pre-wrap; overflow-wrap: anywhere; }
  .item.waiting .reason { color: var(--text); }
  .body { margin: 0.35rem 0 0.2rem 1.45rem; display: flex; flex-direction: column; gap: 0.35rem; }
  .checks { display: flex; flex-wrap: wrap; gap: 0.3rem; }
  .chk { border: 1px solid var(--p-3a3a3a); border-radius: 3px; padding: 0 0.3rem; font-size: 0.72rem; cursor: help; }
  .chk.ok { color: var(--ok, #4caf72); }
  .chk.bad { color: var(--danger, #e74c3c); border-color: var(--danger, #e74c3c); }
  .chk.na { color: var(--text-dim); }
  .line { color: var(--text); overflow-wrap: anywhere; }
  .line code { font-family: ui-monospace, monospace; font-size: 0.74rem; }
  .top { color: var(--text-dim); display: -webkit-box; -webkit-line-clamp: 2; line-clamp: 2;
         -webkit-box-orient: vertical; overflow: hidden; }
  .runs-wrap { overflow-x: auto; }
  .runs { border-collapse: collapse; font-size: 0.72rem; width: 100%; max-width: 40rem; }
  .runs th { text-align: left; color: var(--text-dim); font-weight: normal; padding: 0.1rem 0.6rem 0.1rem 0; }
  .runs td { padding: 0.1rem 0.6rem 0.1rem 0; white-space: nowrap; }
  .mono { font-family: ui-monospace, monospace; }
  .open { align-self: flex-start; }
</style>
