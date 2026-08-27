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
  // Glass-box Phase 3: the deterministic layer of the verification portfolio.
  // The box runs the project's configured check in an isolated worktree at the
  // item's recorded commit and records a pass/fail/error verdict — the trust
  // signal. Runs fire automatically when the agent records an implementation;
  // the Re-run button triggers one manually against the latest recorded commit.
  import * as api from '../lib/api';
  import type { CodeAnchorOwnerType, VerificationResponse } from '../lib/types';

  type Props = {
    ownerType: CodeAnchorOwnerType;
    ownerId: string;
  };
  let { ownerType, ownerId }: Props = $props();

  let data = $state<VerificationResponse | null>(null);
  let loading = $state(false);
  let loadErr = $state('');
  let actErr = $state('');
  let loadedKey = $state('');
  let triggering = $state(false);
  let expanded = $state<Record<string, boolean>>({});
  // Whether the item has a recorded implementation commit. Without one we can't
  // check out "the commit it was built at" — but we can offer an explicit run
  // against the project's current HEAD (caveated), rather than dead-ending.
  let hasCommit = $state(false);

  const results = $derived(data?.results ?? []);
  const criteria = $derived(data?.criteria ?? []);

  // Group results into RUNS instead of one flat, ever-growing list. Every row
  // in a single "Run verification" shares the exact created_at the trigger
  // stamped, so grouping by created_at reconstructs each run. Newest first;
  // the latest run is expanded, older runs collapse under it.
  type Run = { key: string; commit: string; results: typeof results; pass: number; fail: number; skip: number; run: number };
  const runs = $derived.by<Run[]>(() => {
    const byTime = new Map<string, typeof results>();
    for (const r of results) {
      const arr = byTime.get(r.created_at) ?? [];
      arr.push(r);
      byTime.set(r.created_at, arr);
    }
    const out: Run[] = [...byTime.entries()].map(([created, rs]) => ({
      key: created,
      commit: rs[0]?.commit_sha ?? '',
      results: rs,
      pass: rs.filter((r) => r.verdict === 'pass').length,
      fail: rs.filter((r) => r.verdict === 'fail' || r.verdict === 'error').length,
      skip: rs.filter((r) => r.verdict === 'skipped').length,
      run: rs.filter((r) => r.verdict === 'running').length,
    }));
    out.sort((a, b) => b.key.localeCompare(a.key)); // newest first
    return out;
  });
  // Accordion: one run open at a time; the latest is open by default. Empty
  // string means the user explicitly closed the latest (nothing open).
  let openRun = $state<string | null>(null);
  const activeRunKey = $derived(openRun ?? runs[0]?.key ?? null);
  function toggleRun(key: string) { openRun = activeRunKey === key ? '' : key; }
  function runVerdict(r: Run): string {
    if (r.run > 0) return 'running';
    if (r.fail > 0) return 'fail';
    if (r.pass > 0) return 'pass';
    return 'skipped';
  }
  const running = $derived(
    results.some((r) => r.verdict === 'running') ||
      criteria.some((c) => c.result?.verdict === 'running'),
  );

  $effect(() => {
    const key = `${ownerType}:${ownerId}`;
    if (!ownerId || key === loadedKey) return;
    loadedKey = key;
    void reload();
  });

  // Poll while a run is in flight so the verdict lands without a manual refresh.
  $effect(() => {
    if (!running) return;
    const t = setInterval(() => void reload(true), 2000);
    return () => clearInterval(t);
  });

  async function reload(quiet = false) {
    if (!quiet) loading = true;
    loadErr = '';
    try {
      const [v, anchors] = await Promise.all([
        api.getVerification(ownerType, ownerId),
        api.listCodeAnchors(ownerType, ownerId),
      ]);
      data = v;
      hasCommit = (anchors ?? []).some((a) => a.kind === 'commit' && !!a.revision);
    } catch (e) {
      loadErr = String(e);
    } finally {
      loading = false;
    }
  }

  async function rerun(useHead = false) {
    if (triggering || running) return;
    triggering = true;
    actErr = '';
    try {
      await api.triggerVerification(ownerType, ownerId, useHead);
      await reload(true);
    } catch (e) {
      // 409 (already running) just means a run is in flight — reload and poll.
      actErr = String(e);
      await reload(true);
    } finally {
      triggering = false;
    }
  }

  const verdictLabel: Record<string, string> = {
    running: 'Running…',
    pass: 'Passing',
    fail: 'Failing',
    error: 'Error',
    skipped: 'Skipped',
  };

  const kindLabel: Record<string, string> = {
    test: 'Tests',
    lint: 'Lint',
    types: 'Types',
    sast: 'SAST',
    vuln: 'Vuln',
  };

  const shortSha = (s?: string) => (s ? s.slice(0, 7) : '');
  const dur = (ms: number) => (ms < 1000 ? `${ms}ms` : `${(ms / 1000).toFixed(1)}s`);
  const when = (iso: string) => new Date(iso).toLocaleString();
  const toggle = (id: string) => (expanded[id] = !expanded[id]);
</script>

<section class="vf">
  {#if loading && !data}
    <p class="muted">Loading…</p>
  {:else if data && !data.available}
    <div class="empty">
      <p class="empty-head">Verification isn't available on this server.</p>
      <p class="sub">The deterministic layer runs only in <code>serve</code> mode.</p>
    </div>
  {:else if data && data.checks.length === 0}
    <div class="empty">
      <p class="empty-head">No checks configured.</p>
      <p class="sub">
        Set one or more check commands for this project in project Settings →
        Verification — a test suite (<code>go test ./...</code>) and scanners
        (lint, types, SAST, dependency-vuln). The box runs them in an isolated
        checkout at the commit each item was implemented at and records a
        pass/fail verdict per check — the trust signal.
      </p>
    </div>
  {:else if data}
    {#if loadErr}<p class="err">{loadErr}</p>{/if}
    <div class="head">
      <div class="checks-bar">
        {#each data.checks as c (c.kind)}
          <span class="kind {c.kind}" title={c.command}>{kindLabel[c.kind] ?? c.kind}</span>
        {/each}
      </div>
      {#if hasCommit}
        <button class="rerun" disabled={triggering || running} onclick={() => rerun(false)}>
          {running ? 'Running…' : triggering ? 'Starting…' : '↻ Re-run'}
        </button>
      {:else}
        <button class="rerun" disabled={triggering || running} onclick={() => rerun(true)}
          title="No implementation commit recorded — run against the project's current commit (HEAD)">
          {running ? 'Running…' : triggering ? 'Starting…' : '▶ Run against current commit'}
        </button>
      {/if}
    </div>
    {#if !hasCommit}
      <p class="head-note">
        No implementation commit is recorded for this item, so there's nothing to
        check out "as built." A run here verifies the checks against the project's
        <strong>current commit (HEAD)</strong> — the code as it is now, which may have
        drifted since this was built. The run records the exact commit it tested.
      </p>
    {/if}
    {#if actErr}<p class="err">{actErr}</p>{/if}
    {#if !data.repo_configured}
      <p class="warn">No repo is configured for this project — runs will error until one is set.</p>
    {/if}

    {#if results.length === 0}
      <div class="empty">
        <p class="empty-head">No runs yet.</p>
        <p class="sub">
          Verification runs automatically when the agent records an implementation
          for this item.{hasCommit ? ' You can also run it now against the recorded commit.' : ' Use the button above to run against the current commit.'}
        </p>
      </div>
    {:else}
      {#each runs as rn, i (rn.key)}
        {@const rv = runVerdict(rn)}
        <div class="run">
          <button class="run-head {rv}" onclick={() => toggleRun(rn.key)}>
            <span class="dot {rv}" class:pulse={rv === 'running'}></span>
            <span class="run-title">
              {i === 0 ? 'Latest run' : 'Run'} · {when(rn.key)}
              {#if rn.commit}<span class="sha" title={rn.commit}>{shortSha(rn.commit)}</span>{/if}
            </span>
            <span class="run-summary">
              {rn.pass} passed{#if rn.fail} · <span class="bad">{rn.fail} failed</span>{/if}{#if rn.skip} · {rn.skip} skipped{/if}{#if rn.run} · {rn.run} running{/if}
            </span>
            <span class="caret">{activeRunKey === rn.key ? '▾' : '▸'}</span>
          </button>
          {#if activeRunKey === rn.key}
          <ul class="list">
            {#each rn.results as r (r.id)}
              <li class="row {r.verdict}">
                <button class="line" onclick={() => toggle(r.id)} title="Show output">
                  <span class="dot {r.verdict}" class:pulse={r.verdict === 'running'}></span>
                  <span class="kind {r.kind}">{kindLabel[r.kind] ?? r.kind}</span>
                  <span class="verdict">{verdictLabel[r.verdict] ?? r.verdict}</span>
                  <span class="summary">{r.summary}</span>
                  <span class="meta">
                    {#if r.verdict !== 'running'}<span class="d">{dur(r.duration_ms)}</span>{/if}
                    <span class="caret">{expanded[r.id] ? '▾' : '▸'}</span>
                  </span>
                </button>
                {#if expanded[r.id]}
                  <div class="detail">
                    <div class="detail-meta">
                      <code class="cmd-inline">{r.check_name}</code>
                    </div>
                    {#if r.output}
                      <pre class="output">{r.output}</pre>
                    {:else if r.verdict === 'pass'}
                      <p class="muted">No issues — the check passed with no output.</p>
                    {:else if r.verdict === 'skipped'}
                      <p class="muted">The tool isn’t installed, so this check didn’t run.</p>
                    {:else}
                      <p class="muted">No output captured.</p>
                    {/if}
                  </div>
                {/if}
              </li>
            {/each}
          </ul>
          {/if}
        </div>
      {/each}
    {/if}

    {#if criteria.length > 0}
      <div class="subhead">Per-criterion</div>
      <ul class="list">
        {#each criteria as c (c.criterion_id)}
          <li class="row">
            <button class="line" onclick={() => toggle(c.criterion_id)} title={c.text}>
              <span class="summary">{c.text}</span>
              <span class="meta">
                {#if c.result}
                  <span class="chip" title="Tests: {verdictLabel[c.result.verdict] ?? c.result.verdict}">
                    <span class="dot {c.result.verdict}" class:pulse={c.result.verdict === 'running'}></span>Tests
                  </span>
                {/if}
                {#if c.review}
                  <span class="chip" title="AI review: {verdictLabel[c.review.verdict] ?? c.review.verdict}">
                    <span class="dot {c.review.verdict}" class:pulse={c.review.verdict === 'running'}></span>AI
                  </span>
                {/if}
                <span class="caret">{expanded[c.criterion_id] ? '▾' : '▸'}</span>
              </span>
            </button>
            {#if expanded[c.criterion_id]}
              <div class="detail">
                {#if c.result}
                  <div class="detail-meta">
                    <code class="cmd-inline">{c.command}</code>
                    <span class="ts">Tests: {verdictLabel[c.result.verdict] ?? c.result.verdict} · {when(c.result.created_at)}</span>
                  </div>
                  {#if c.result.output}<pre class="output">{c.result.output}</pre>{/if}
                {/if}
                {#if c.review}
                  <div class="review">
                    <span class="rlabel">
                      <span class="dot {c.review.verdict}"></span>AI review — {verdictLabel[c.review.verdict] ?? c.review.verdict}
                    </span>
                    {#if c.review.output}<p class="explain">{c.review.output}</p>{/if}
                  </div>
                {/if}
                {#if !c.result && !c.review}
                  <p class="muted">Not run yet — verification runs when the agent records an implementation, or hit Re-run.</p>
                {/if}
              </div>
            {/if}
          </li>
        {/each}
      </ul>
    {/if}
  {/if}
</section>

<style>
  .vf { padding: 4px 2px; }
  .muted { color: var(--p-777777); font-size: 12px; font-style: italic; }
  .err { color: var(--p-ff8888); font-size: 12px; margin: 4px 0; }
  .warn { color: var(--p-d4a54d); font-size: 12px; margin: 4px 0; }
  .head-note {
    font-size: 11.5px; line-height: 1.5; color: var(--p-cfe6ff);
    background: var(--p-16222e); border: 1px solid var(--p-2d5578);
    border-radius: 6px; padding: 8px 12px; margin: 0 0 10px;
  }
  .head { display: flex; align-items: center; justify-content: space-between; gap: 8px; margin-bottom: 8px; }
  .checks-bar { display: flex; flex-wrap: wrap; gap: 4px; min-width: 0; }
  .kind {
    flex: none;
    font-size: 9px;
    font-weight: 600;
    text-transform: uppercase;
    letter-spacing: 0.3px;
    color: var(--p-aaaaaa);
    background: var(--p-1a1a1a);
    border: 1px solid var(--p-333333);
    border-radius: 3px;
    padding: 1px 5px;
  }
  .kind.test { color: var(--p-99ccff); border-color: var(--p-2d5578); }
  .kind.lint { color: var(--p-d4a54d); }
  .kind.types { color: var(--p-66cc66); }
  .kind.sast { color: var(--p-e07a7a); }
  .kind.vuln { color: var(--p-d4a54d); }
  .cmd {
    font-family: var(--font-mono, monospace);
    font-size: 12px;
    color: var(--p-bbbbbb);
    background: var(--p-141414);
    border: 1px solid var(--p-262626);
    border-radius: 4px;
    padding: 3px 8px;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .rerun {
    flex: none;
    background: var(--p-1a2530);
    color: var(--p-99ccff);
    border: 1px solid var(--p-2d5578);
    border-radius: 4px;
    padding: 4px 12px;
    font-size: 12px;
    cursor: pointer;
  }
  .rerun:hover:not(:disabled) { background: var(--p-2d5578); color: var(--p-cceeff); }
  .rerun:disabled { opacity: 0.6; cursor: default; }
  .run { margin-bottom: 6px; }
  .run-head {
    display: flex; align-items: center; gap: 8px; width: 100%; text-align: left;
    background: var(--p-161616); border: 1px solid var(--p-2a2a2a); border-left-width: 2px;
    border-radius: 5px; padding: 7px 10px; cursor: pointer; color: var(--p-dddddd); font-size: 12px;
  }
  .run-head:hover { background: var(--p-1c1c1c); }
  .run-head.pass { border-left-color: var(--p-66cc66); }
  .run-head.fail { border-left-color: var(--p-e07a7a); }
  .run-head.skipped { border-left-color: var(--p-777777); }
  .run-head.running { border-left-color: var(--p-d4a54d); }
  .run-title { flex: 1; display: inline-flex; align-items: center; gap: 6px; color: var(--p-e0e0e0); }
  .run-summary { flex: none; font-size: 11px; color: var(--p-999999); }
  .run-summary .bad { color: var(--p-e07a7a); }
  .run .list { margin: 4px 0 0 14px; }
  .list { list-style: none; margin: 0; padding: 0; display: flex; flex-direction: column; gap: 4px; }
  .row {
    background: var(--p-141414);
    border: 1px solid var(--p-262626);
    border-radius: 4px;
    overflow: hidden;
  }
  .row.pass { border-left: 2px solid var(--p-66cc66); }
  .row.fail, .row.error { border-left: 2px solid var(--p-e07a7a); }
  .row.running { border-left: 2px solid var(--p-d4a54d); }
  .row.skipped { border-left: 2px solid var(--p-777777); opacity: 0.85; }
  .line {
    width: 100%;
    display: flex;
    align-items: center;
    gap: 8px;
    background: none;
    border: none;
    cursor: pointer;
    padding: 6px 8px;
    text-align: left;
    color: inherit;
  }
  .line:hover { background: var(--p-1a1a1a); }
  .dot { width: 8px; height: 8px; border-radius: 50%; flex: none; background: var(--p-555555); }
  .dot.pass { background: var(--p-66cc66); }
  .dot.fail, .dot.error { background: var(--p-e07a7a); }
  .dot.running { background: var(--p-d4a54d); }
  .dot.skipped { background: var(--p-777777); }
  .dot.pulse { animation: pulse 1.2s ease-in-out infinite; }
  @keyframes pulse { 0%, 100% { opacity: 1; } 50% { opacity: 0.3; } }
  .verdict { font-size: 12px; font-weight: 600; color: var(--p-dddddd); flex: none; }
  .summary {
    flex: 1; font-size: 12px; color: var(--p-999999);
    overflow: hidden; text-overflow: ellipsis; white-space: nowrap;
  }
  .meta { display: inline-flex; align-items: center; gap: 8px; flex: none; font-size: 11px; color: var(--p-777777); }
  .sha { font-family: var(--font-mono, monospace); color: var(--p-99ccff); }
  .caret { color: var(--p-666666); }
  .subhead {
    margin: 12px 0 6px;
    font-size: 11px;
    text-transform: uppercase;
    letter-spacing: 0.5px;
    color: var(--p-888888);
  }
  .cmd-inline {
    font-family: var(--font-mono, monospace);
    font-size: 10px;
    color: var(--p-99ccff);
  }
  .chip {
    display: inline-flex;
    align-items: center;
    gap: 4px;
    flex: none;
    font-size: 10px;
    color: var(--p-aaaaaa);
    background: var(--p-1a1a1a);
    border: 1px solid var(--p-262626);
    border-radius: 3px;
    padding: 1px 5px;
  }
  .review { margin-top: 6px; border-top: 1px solid var(--p-262626); padding-top: 6px; }
  .rlabel { display: inline-flex; align-items: center; gap: 6px; font-size: 11px; font-weight: 600; color: var(--p-cccccc); }
  .explain { margin: 4px 0 0; font-size: 12px; line-height: 1.5; color: var(--p-bbbbbb); white-space: pre-wrap; }
  .detail { border-top: 1px solid var(--p-262626); padding: 6px 8px; }
  .detail-meta { display: flex; justify-content: space-between; font-size: 10px; color: var(--p-666666); margin-bottom: 4px; }
  .output {
    margin: 0;
    max-height: 280px;
    overflow: auto;
    font-family: var(--font-mono, monospace);
    font-size: 11px;
    line-height: 1.4;
    color: var(--p-bbbbbb);
    background: var(--p-0a0a0a);
    border-radius: 3px;
    padding: 6px 8px;
    white-space: pre-wrap;
    word-break: break-word;
  }
  .empty { text-align: center; padding: 14px 12px; }
  .empty-head { color: var(--p-bbbbbb); font-size: 13px; margin: 0 0 4px; }
  .sub { font-size: 12px; line-height: 1.5; color: var(--p-777777); margin: 0 auto; max-width: 380px; }
  .sub code {
    font-family: var(--font-mono, monospace);
    font-size: 11px;
    color: var(--p-99ccff);
    background: var(--p-141414);
    padding: 0 3px;
    border-radius: 3px;
  }
</style>
