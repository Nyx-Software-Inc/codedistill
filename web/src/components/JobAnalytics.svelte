<!--
  =============================================================================
   Copyright (c) 2026 Nyx Software, Inc.  All rights reserved.

   CodeDistill

   Property of Nyx Software, Inc., provided under a dual license: the GNU Affero General
   Public License v3.0 (see the LICENSE file) and, separately, a commercial
   license available from Nyx Software, Inc. Use outside the terms of one of those
   licenses is prohibited.

   SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-Nyx-Commercial
  =============================================================================
-->
<!--
  The analytics strip at the top of the Jobs pane (docs/design/jobs-analytics.md).
  Collapsed: one line, how full the job slots are, tokens, CPU and memory.
  Open: running vs waiting over time, tokens by job / role / model, the machine
  over the last hour, and the models loaded. Installation-wide, like the slots.
-->
<script lang="ts">
  import { onMount, onDestroy } from 'svelte';
  import * as api from '../lib/api';
  import type { AnalyticsWindow, JobsAnalytics, MachineView } from '../lib/api';

  const OPEN_KEY = 'cd.jobs.analytics.open';
  const WIN_KEY = 'cd.jobs.analytics.window';
  const remembered = (k: string) => { try { return localStorage.getItem(k); } catch { return null; } };
  const remember = (k: string, v: string) => { try { localStorage.setItem(k, v); } catch { /* not kept */ } };

  let open = $state(remembered(OPEN_KEY) === '1');
  let win = $state<AnalyticsWindow>((['1h', '24h', '7d'] as const).find((w) => w === remembered(WIN_KEY)) ?? '24h');
  let a = $state<JobsAnalytics | null>(null);
  let m = $state<MachineView | null>(null);
  let by = $state<'job' | 'role' | 'model'>('job');
  let asTable = $state(false);

  async function loadAnalytics() {
    try { a = await api.getJobsAnalytics(win); } catch { /* keep the last figures */ }
  }
  async function loadMachine() {
    try { m = await api.getMachine(); } catch { /* keep the last figures */ }
  }
  const visible = () => typeof document === 'undefined' || document.visibilityState === 'visible';
  let t1: ReturnType<typeof setInterval> | null = null;
  let t2: ReturnType<typeof setInterval> | null = null;
  onMount(() => {
    loadAnalytics();
    loadMachine();
    // The machine is sampled only while this asks: a hidden tab asks nothing.
    t1 = setInterval(() => visible() && loadAnalytics(), 15000);
    t2 = setInterval(() => visible() && loadMachine(), 5000);
  });
  onDestroy(() => { if (t1) clearInterval(t1); if (t2) clearInterval(t2); });

  function toggle() { open = !open; remember(OPEN_KEY, open ? '1' : '0'); }
  function setWin(w: AnalyticsWindow) { win = w; remember(WIN_KEY, w); loadAnalytics(); }

  // ── Formatting ────────────────────────────────────────────────────────
  function k(n: number): string {
    if (n >= 1e9) return `${(n / 1e9).toFixed(1)}B`;
    if (n >= 1e6) return `${(n / 1e6).toFixed(1)}M`;
    return n >= 1000 ? `${(n / 1000).toFixed(1)}k` : String(n);
  }
  const gb = (b: number) => `${(b / 2 ** 30).toFixed(b >= 10 * 2 ** 30 ? 0 : 1)} GB`;
  function when(iso: string): string {
    const d = new Date(iso);
    return win === '7d'
      ? d.toLocaleDateString([], { weekday: 'short' }) + ' ' + d.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })
      : d.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' });
  }
  const winLabel: Record<AnalyticsWindow, string> = { '1h': 'last hour', '24h': 'last 24 hours', '7d': 'last 7 days' };

  const last = $derived(m?.samples.length ? m.samples[m.samples.length - 1] : null);
  const cpuNow = $derived(last && last.cpu_percent >= 0 ? `${Math.round(last.cpu_percent)}%` : '—');
  const memNow = $derived(last && last.mem_total ? `${Math.round((100 * last.mem_used) / last.mem_total)}%` : '—');

  // ── Running vs waiting ────────────────────────────────────────────────
  let cw = $state(0);
  const CH = 120;
  const PAD = { l: 22, r: 8, t: 8, b: 18 };
  const tl = $derived(a?.timeline ?? []);
  const yMax = $derived(Math.max(1, a?.slots ?? 1, ...tl.map((p) => Math.max(p.running, p.queued))));
  const plotW = $derived(Math.max(0, cw - PAD.l - PAD.r));
  const xOf = (i: number) => PAD.l + (tl.length ? (i / tl.length) * plotW : 0);
  const yOf = (v: number) => PAD.t + (CH - PAD.t - PAD.b) * (1 - v / yMax);
  /** A step line: each slice is flat across its width. */
  function steps(key: 'running' | 'queued'): string {
    if (!tl.length || !plotW) return '';
    let d = '';
    tl.forEach((p, i) => {
      const y = yOf(p[key]).toFixed(1);
      d += `${i ? 'L' : 'M'}${xOf(i).toFixed(1)},${y}L${xOf(i + 1).toFixed(1)},${y}`;
    });
    return d;
  }
  const yTicks = $derived(yMax <= 4 ? Array.from({ length: yMax + 1 }, (_, i) => i) : [0, Math.round(yMax / 2), yMax]);
  let hover = $state<number | null>(null);
  function onMove(e: PointerEvent) {
    const r = (e.currentTarget as SVGElement).getBoundingClientRect();
    const i = Math.floor(((e.clientX - r.left - PAD.l) / plotW) * tl.length);
    hover = i >= 0 && i < tl.length ? i : null;
  }
  const busySlices = $derived(tl.filter((p) => p.running || p.queued));

  // ── Tokens ─────────────────────────────────────────────────────────────
  type Row = { key: string; name: string; sub?: string; total: number; title: string };
  const TOP = 6;
  const rows = $derived.by((): Row[] => {
    if (!a) return [];
    const t = a.tokens;
    const title = (c: api.TokenCounts) =>
      `${c.in.toLocaleString()} in · ${c.out.toLocaleString()} out · ${c.cache_read.toLocaleString()} read from cache · ${c.cache_write.toLocaleString()} written to cache`;
    // A run that reported no numbers is not a run that used none: leave it out.
    const all: Row[] = (
      by === 'job'
        ? t.by_job.map((j) => ({ key: j.job_id, name: j.label, sub: j.project, total: j.total, title: title(j) }))
        : by === 'role'
          ? t.by_role.map((r) => ({ key: r.role + r.lens, name: r.role, sub: r.lens || undefined, total: r.total, title: title(r) }))
          : t.by_model.map((x) => ({ key: x.model, name: x.model, total: x.total, title: title(x) }))
    ).filter((r) => r.total > 0);
    if (all.length <= TOP) return all;
    const rest = all.slice(TOP - 1);
    const other = rest.reduce((n, r) => n + r.total, 0);
    return [...all.slice(0, TOP - 1), { key: 'other', name: `${rest.length} others`, total: other, title: `${other.toLocaleString()} tokens` }];
  });
  const rowMax = $derived(Math.max(1, ...rows.map((r) => r.total)));

  // ── The machine ──────────────────────────────────────────────────────
  const SW = 120, SH = 28;
  function spark(vals: number[], max: number): string {
    const pts = vals.map((v, i) => [vals.length > 1 ? (i / (vals.length - 1)) * SW : SW, SH - 2 - (Math.max(0, v) / max) * (SH - 4)]);
    return pts.map(([x, y], i) => `${i ? 'L' : 'M'}${x.toFixed(1)},${y.toFixed(1)}`).join('');
  }
  const cpuSeries = $derived((m?.samples ?? []).filter((s) => s.cpu_percent >= 0).map((s) => s.cpu_percent));
  const memSeries = $derived((m?.samples ?? []).filter((s) => s.mem_total).map((s) => (100 * s.mem_used) / s.mem_total));
  const gpuSeries = $derived((m?.samples ?? []).filter((s) => s.gpu_percent >= 0).map((s) => s.gpu_percent));
  const onLabel = { gpu: 'on the GPU', cpu: 'on the CPU (slow)', split: 'partly on the GPU' } as const;
</script>

<section class="strip" aria-label="Job analytics">
  <button class="line" onclick={toggle} aria-expanded={open} title={open ? 'Hide analytics' : 'Show analytics'}>
    <span class="chev">{open ? '▾' : '▸'}</span>
    {#if a}
      <span title="Job slots: how many jobs run at once across every project. Change it in Settings › Agents.">
        {a.running} of {a.slots} slot{a.slots === 1 ? '' : 's'} busy{a.queued ? ` · ${a.queued} waiting` : ''}
      </span>
      <span class="sep">·</span>
      <span title="Tokens used by agents in the {winLabel[win]}">{k(a.tokens.total.total)} tokens</span>
    {:else}
      <span>Analytics</span>
    {/if}
    <span class="spacer"></span>
    <span class="mach" title="CPU and memory now">CPU {cpuNow} · Mem {memNow}</span>
  </button>

  {#if open && a}
    <div class="body">
      <!-- Running vs waiting -->
      <div class="block">
        <div class="bhead">
          <h4>Jobs</h4>
          <span class="legend">
            <span class="key run"></span>Running {a.running}
            <span class="key wait"></span>Waiting {a.queued}
            <span class="key cap"></span>{a.slots} slot{a.slots === 1 ? '' : 's'}
          </span>
          <span class="spacer"></span>
          <button class="link" onclick={() => (asTable = !asTable)}>{asTable ? 'Chart' : 'Table'}</button>
          <div class="seg" role="group" aria-label="Time window">
            {#each ['1h', '24h', '7d'] as const as w (w)}
              <button class:on={win === w} onclick={() => setWin(w)}>{w}</button>
            {/each}
          </div>
        </div>
        {#if asTable}
          {#if busySlices.length}
            <table class="tbl">
              <thead><tr><th>From</th><th>Running</th><th>Waiting</th></tr></thead>
              <tbody>
                {#each busySlices as p (p.at)}
                  <tr><td>{when(p.at)}</td><td>{p.running}</td><td>{p.queued}</td></tr>
                {/each}
              </tbody>
            </table>
          {:else}
            <p class="muted">No jobs in the {winLabel[win]}.</p>
          {/if}
        {:else}
          <div class="chart" bind:clientWidth={cw}>
            {#if cw}
              <svg width={cw} height={CH} role="img"
                   aria-label="Jobs running and waiting over the {winLabel[win]}"
                   onpointermove={onMove} onpointerleave={() => (hover = null)}>
                {#each yTicks as v (v)}
                  <line class="grid" x1={PAD.l} x2={cw - PAD.r} y1={yOf(v)} y2={yOf(v)} />
                  <text class="ax" x={PAD.l - 5} y={yOf(v) + 3} text-anchor="end">{v}</text>
                {/each}
                <line class="cap" x1={PAD.l} x2={cw - PAD.r} y1={yOf(a.slots)} y2={yOf(a.slots)} />
                <path class="s-wait" d={steps('queued')} />
                <path class="s-run" d={steps('running')} />
                {#if tl.length}
                  <text class="ax" x={PAD.l} y={CH - 4}>{when(tl[0].at)}</text>
                  <text class="ax" x={cw - PAD.r} y={CH - 4} text-anchor="end">now</text>
                {/if}
                {#if hover !== null}
                  <line class="cross" x1={xOf(hover + 0.5)} x2={xOf(hover + 0.5)} y1={PAD.t} y2={CH - PAD.b} />
                  <circle class="dot-run" cx={xOf(hover + 0.5)} cy={yOf(tl[hover].running)} r="4" />
                  <circle class="dot-wait" cx={xOf(hover + 0.5)} cy={yOf(tl[hover].queued)} r="4" />
                {/if}
              </svg>
              {#if hover !== null}
                {@const p = tl[hover]}
                <!-- Away from the newest data: left of the cursor on the right half. -->
                <div class="tip" style:left={xOf(hover + 0.5) > cw / 2 ? 'auto' : `${xOf(hover + 0.5) + 10}px`}
                     style:right={xOf(hover + 0.5) > cw / 2 ? `${cw - xOf(hover + 0.5) + 10}px` : 'auto'}>
                  <div class="tipwhen">{when(p.at)}</div>
                  <div><span class="key run"></span>Running <b>{p.running}</b></div>
                  <div><span class="key wait"></span>Waiting <b>{p.queued}</b></div>
                </div>
              {/if}
            {/if}
          </div>
        {/if}
      </div>

      <!-- Tokens -->
      <div class="block">
        <div class="bhead">
          <h4>Tokens</h4>
          <span class="hero" title="{a.tokens.total.in.toLocaleString()} in · {a.tokens.total.out.toLocaleString()} out · {a.tokens.total.cache_read.toLocaleString()} read from cache · {a.tokens.total.cache_write.toLocaleString()} written to cache">
            {k(a.tokens.total.total)}
          </span>
          <span class="muted small">
            {k(a.tokens.total.in)} in · {k(a.tokens.total.out)} out · {k(a.tokens.total.cache_read + a.tokens.total.cache_write)} cache{a.tokens.live_runs ? ` · ${a.tokens.live_runs} running now` : ''}
          </span>
          <span class="spacer"></span>
          <div class="seg" role="group" aria-label="Break tokens down by">
            <button class:on={by === 'job'} onclick={() => (by = 'job')}>Job</button>
            <button class:on={by === 'role'} onclick={() => (by = 'role')}>Agent</button>
            <button class:on={by === 'model'} onclick={() => (by = 'model')}>Model</button>
          </div>
        </div>
        {#if rows.length}
          <ul class="bars">
            {#each rows as r (r.key)}
              <li title={r.title}>
                <div class="blabel">
                  <span class="bname">{r.name}</span>{#if r.sub}<span class="muted small"> {r.sub}</span>{/if}
                  <span class="spacer"></span><span class="bval">{k(r.total)}</span>
                </div>
                <div class="track"><div class="fill" style:width="{(100 * r.total) / rowMax}%"></div></div>
              </li>
            {/each}
          </ul>
        {:else}
          <p class="muted">No agent used tokens in the {winLabel[win]}.</p>
        {/if}
      </div>

      <!-- The machine -->
      <div class="block">
        <!-- A GPU that cannot be read gets no tile: a dash beside CPU and memory
             says nothing useful. Whether the local model runs on the GPU is
             the line below, from the model server itself. Why there are no
             GPU figures stays a hover away. -->
        <div class="bhead">
          <h4 title={m && !m.gpu.readable && m.gpu.why ? `No GPU figures: ${m.gpu.why}` : undefined}>Machine</h4>
          <span class="muted small">last hour, while this is open</span>
        </div>
        <div class="tiles">
          <div class="tile">
            <div class="tname">CPU</div><div class="tval">{cpuNow}</div>
            {#if cpuSeries.length > 1}<svg class="spark" viewBox="0 0 {SW} {SH}" height={SH} preserveAspectRatio="none" aria-hidden="true"><path d={spark(cpuSeries, 100)} /></svg>{/if}
          </div>
          <div class="tile">
            <div class="tname">Memory</div>
            <div class="tval">{memNow}</div>
            {#if last && last.mem_total}<div class="muted small">{gb(last.mem_used)} of {gb(last.mem_total)}</div>{/if}
            {#if memSeries.length > 1}<svg class="spark" viewBox="0 0 {SW} {SH}" height={SH} preserveAspectRatio="none" aria-hidden="true"><path d={spark(memSeries, 100)} /></svg>{/if}
          </div>
          {#if m?.gpu.readable}
            <div class="tile">
              <div class="tname">GPU{m.gpu.name ? ` · ${m.gpu.name}` : ''}</div>
              <div class="tval">{last && last.gpu_percent >= 0 ? `${Math.round(last.gpu_percent)}%` : '—'}</div>
              {#if last?.gpu_mem_used && m.gpu.mem_total}<div class="muted small">{gb(last.gpu_mem_used)} of {gb(m.gpu.mem_total)}</div>{/if}
              {#if gpuSeries.length > 1}<svg class="spark" viewBox="0 0 {SW} {SH}" height={SH} preserveAspectRatio="none" aria-hidden="true"><path d={spark(gpuSeries, 100)} /></svg>{/if}
            </div>
          {/if}
        </div>
        {#if m}
          <p class="models">
            {#if m.models.length}
              {#each m.models as x, i (x.endpoint + x.name)}{i ? ' · ' : ''}<span class:slow={x.on === 'cpu'}>{x.name} {onLabel[x.on]}</span>{/each}
            {:else if m.models_error}
              {m.models_error}
            {:else}
              No local model loaded right now.
            {/if}
          </p>
        {/if}
      </div>
    </div>
  {/if}
</section>

<style>
  .strip {
    /* Series colors, checked for colour-blind separation against both
       themes' surfaces. Text never wears them. */
    --s-run: #3987e5;
    --s-wait: #d95926;
    border: 1px solid var(--p-333333);
    border-radius: 8px;
    font-size: 0.78rem;
    color: var(--p-aaaaaa);
  }
  :global(:root[data-theme='light']) .strip { --s-run: #2a78d6; --s-wait: #eb6834; }

  .line {
    display: flex; align-items: center; gap: 0.4rem; width: 100%;
    background: none; border: 0; color: inherit; font: inherit; text-align: left;
    padding: 0.35rem 0.6rem; cursor: pointer; white-space: nowrap; overflow: hidden;
  }
  .line > span { overflow: hidden; text-overflow: ellipsis; }
  .chev { color: var(--p-777777); flex: 0 0 auto; }
  .sep { color: var(--p-666666); }
  .spacer { flex: 1; }
  .mach { flex: 0 0 auto; }

  .body { display: flex; flex-direction: column; gap: 0.9rem; padding: 0.6rem 0.6rem 0.7rem; border-top: 1px solid var(--p-2a2a2a); }
  .seg { display: inline-flex; border: 1px solid var(--p-3a3a3a); border-radius: 5px; overflow: hidden; }
  .seg button { background: none; border: 0; color: var(--p-999999); font: inherit; font-size: 0.72rem; padding: 0.1rem 0.5rem; cursor: pointer; }
  .seg button + button { border-left: 1px solid var(--p-3a3a3a); }
  .seg button.on { background: var(--p-2a2a2a); color: var(--p-e0e0e0); }

  .block { display: flex; flex-direction: column; gap: 0.35rem; min-width: 0; }
  .bhead { display: flex; align-items: baseline; gap: 0.6rem; flex-wrap: wrap; }
  h4 { margin: 0; font-size: 0.78rem; font-weight: 600; color: var(--p-e0e0e0); }
  .legend { display: inline-flex; align-items: center; gap: 0.3rem; flex-wrap: wrap; }
  .key { display: inline-block; width: 12px; height: 2px; border-radius: 1px; vertical-align: middle; margin-left: 0.4rem; }
  .key.run { background: var(--s-run); }
  .key.wait { background: var(--s-wait); }
  .key.cap { background: var(--p-666666); height: 1px; }
  .link { background: none; border: 0; color: var(--p-999999); font: inherit; font-size: 0.72rem; cursor: pointer; text-decoration: underline; }
  .muted { color: var(--p-888888); margin: 0; }
  .small { font-size: 0.72rem; }

  .chart { position: relative; width: 100%; }
  svg { display: block; overflow: visible; }
  .grid { stroke: var(--p-2a2a2a); stroke-width: 1; }
  .cap { stroke: var(--p-666666); stroke-width: 1; }
  .ax { fill: var(--p-888888); font-size: 9px; }
  .s-run, .s-wait { fill: none; stroke-width: 2; stroke-linejoin: round; stroke-linecap: round; }
  .s-run { stroke: var(--s-run); }
  .s-wait { stroke: var(--s-wait); }
  .cross { stroke: var(--p-777777); stroke-width: 1; }
  .dot-run, .dot-wait { stroke: var(--p-1a1a1a); stroke-width: 2; }
  .dot-run { fill: var(--s-run); }
  .dot-wait { fill: var(--s-wait); }
  .tip {
    position: absolute; top: 0; pointer-events: none; min-width: 110px;
    background: var(--p-222222); border: 1px solid var(--p-3a3a3a); border-radius: 6px;
    padding: 0.3rem 0.5rem; color: var(--p-e0e0e0); font-size: 0.72rem; box-shadow: 0 2px 8px rgba(0, 0, 0, 0.3);
  }
  .tip .key { margin: 0 0.3rem 0 0; }
  .tipwhen { color: var(--p-999999); margin-bottom: 0.15rem; }
  .tbl { border-collapse: collapse; font-size: 0.72rem; max-height: 160px; }
  .tbl th, .tbl td { text-align: left; padding: 0.1rem 0.8rem 0.1rem 0; }
  .tbl th { color: var(--p-888888); font-weight: 500; }

  .hero { font-size: 1rem; font-weight: 600; color: var(--p-e0e0e0); font-variant-numeric: tabular-nums; }
  .bars { list-style: none; margin: 0; padding: 0; display: flex; flex-direction: column; gap: 0.35rem; }
  .blabel { display: flex; gap: 0.3rem; min-width: 0; }
  .bname { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; color: var(--p-cccccc, var(--p-e0e0e0)); }
  .bval { font-variant-numeric: tabular-nums; color: var(--p-e0e0e0); }
  .track { height: 6px; margin-top: 2px; }
  .fill { height: 100%; min-width: 2px; background: var(--s-run); border-radius: 0 3px 3px 0; }

  .tiles { display: grid; grid-template-columns: repeat(auto-fit, minmax(130px, 1fr)); gap: 0.5rem; }
  .tile { border: 1px solid var(--p-2a2a2a); border-radius: 6px; padding: 0.4rem 0.5rem; min-width: 0; }
  .tname { color: var(--p-888888); font-size: 0.72rem; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .tval { font-size: 1rem; font-weight: 600; color: var(--p-e0e0e0); font-variant-numeric: tabular-nums; }
  .spark { margin-top: 0.25rem; width: 100%; }
  .spark path { fill: none; stroke: var(--s-run); stroke-width: 1.5; stroke-linejoin: round; vector-effect: non-scaling-stroke; }
  .models { margin: 0; color: var(--p-999999); }
  .models .slow { color: var(--p-e0e0e0); }
</style>
