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
  import type {
    DashboardIndexHealth,
    DashboardLifecycle,
    DashboardThroughput,
    DayCount,
    ReviewQueueResponse,
    DriftResponse,
  } from '../lib/api';

  // Right-side slide-in panel — project metrics dashboard.
  //
  // Panel #1 (throughput + open backlog): diverging stacked bars per
  // day — created above the baseline, completed below, both
  // color-segmented by item type (todos/bugs/use_cases), with an
  // open-count chip row above the chart.
  // Panel #2 (velocity): weekly closed-item sparklines, last 8 weeks,
  // derived client-side from the same throughput fetch.
  // Panel #3 (indexing health): embedding coverage, code-chunk totals,
  // anchor provenance counts, dedup flags.
  // Panel #4 (time to close): median/p90 per type.
  // Panel #5 (per branch): closed items attributed to git branches.
  //
  // Sized to match the existing slide-out panels (Matches/Search/Ask).
  // Hand-rolled inline SVG, no chart-lib dependency.

  type Props = {
    open: boolean;
    projectId: string;
    projectName?: string;
    onClose: () => void;
    // The dashboard shows STATS; the review WORK happens in Needs Review.
    onOpenNeedsReview?: () => void;
  };
  let { open, projectId, projectName = '', onClose, onOpenNeedsReview }: Props = $props();

  const WINDOW_DAYS = 30;
  // One throughput fetch feeds both the 30-day chart and the 8-week
  // velocity sparklines. 56 days covers 7 full prior weeks plus the
  // current week even when today is Sunday.
  const FETCH_DAYS = 56;
  const WEEKS = 8;
  // Type slugs match the JSON keys returned by the API.
  const TYPES = ['todos', 'bugs', 'use_cases'] as const;
  type TypeSlug = (typeof TYPES)[number];

  // Per-item-type accent colors, kept consistent with the rest of the app
  // so the dashboard feels visually unified.
  const TYPE_COLORS: Record<TypeSlug, string> = {
    todos: '#9cf',
    bugs: '#f88',
    use_cases: '#fc9',
  };
  const TYPE_LABELS: Record<TypeSlug, string> = {
    todos: 'Todos',
    bugs: 'Bugs',
    use_cases: 'Use cases',
  };

  let data = $state<DashboardThroughput | null>(null);
  let health = $state<DashboardIndexHealth | null>(null);
  let lifecycle = $state<DashboardLifecycle | null>(null);
  let queue = $state<ReviewQueueResponse | null>(null);
  let drift = $state<DriftResponse | null>(null);
  let loading = $state(false);
  let err = $state('');
  let sideErr = $state(''); // index-health / lifecycle fetch failures

  async function load() {
    if (!projectId) {
      data = null;
      health = null;
      lifecycle = null;
      queue = null;
      drift = null;
      return;
    }
    loading = true;
    err = '';
    sideErr = '';
    const [t, h, l, q, d] = await Promise.allSettled([
      api.getDashboardThroughput(projectId, FETCH_DAYS),
      api.getDashboardIndexHealth(projectId),
      api.getDashboardLifecycle(projectId),
      api.getReviewQueue(projectId),
      api.getDrift(projectId),
    ]);
    queue = q.status === 'fulfilled' ? q.value : null;
    drift = d.status === 'fulfilled' ? d.value : null;
    if (t.status === 'fulfilled') data = t.value;
    else {
      data = null;
      err = String(t.reason);
    }
    if (h.status === 'fulfilled') health = h.value;
    else {
      health = null;
      sideErr = String(h.reason);
    }
    if (l.status === 'fulfilled') lifecycle = l.value;
    else {
      lifecycle = null;
      sideErr = String(l.reason);
    }
    loading = false;
  }

  // Review actions moved to the Needs Review surface + the items themselves
  // (Rich's beef #1: a dashboard you READ; a queue you WORK).

  function onKey(e: KeyboardEvent) {
    if (e.key === 'Escape' && open) onClose();
  }
  onMount(() => document.addEventListener('keydown', onKey));
  onDestroy(() => document.removeEventListener('keydown', onKey));

  // Reload whenever the panel opens or the active project changes.
  let lastKey = '';
  $effect(() => {
    const key = open ? projectId : '';
    if (key !== lastKey) {
      lastKey = key;
      if (key) void load();
    }
  });

  // Build a calendar of WINDOW_DAYS YYYY-MM-DD strings ending today
  // (local time). The API returns only non-zero days; we lay out the
  // full window and fill gaps with zeros so the bar chart has a
  // stable x-axis.
  function buildCalendar(days: number): string[] {
    const out: string[] = [];
    const now = new Date();
    for (let i = days - 1; i >= 0; i--) {
      const d = new Date(now.getFullYear(), now.getMonth(), now.getDate() - i);
      out.push(
        d.getFullYear() +
          '-' +
          String(d.getMonth() + 1).padStart(2, '0') +
          '-' +
          String(d.getDate()).padStart(2, '0'),
      );
    }
    return out;
  }
  function indexSeries(series: DayCount[]): Record<string, number> {
    const m: Record<string, number> = {};
    for (const dc of series) m[dc.date] = dc.count;
    return m;
  }

  // Derived chart model. Computed inside the SVG block below, but
  // pulled out so the hover tooltip can read the same buckets.
  let calendar = $derived(buildCalendar(WINDOW_DAYS));
  let createdIdx = $derived.by(() => {
    const out: Record<TypeSlug, Record<string, number>> = {
      todos: {}, bugs: {}, use_cases: {},
    };
    if (!data) return out;
    for (const t of TYPES) out[t] = indexSeries(data.created_by_day[t] ?? []);
    return out;
  });
  let completedIdx = $derived.by(() => {
    const out: Record<TypeSlug, Record<string, number>> = {
      todos: {}, bugs: {}, use_cases: {},
    };
    if (!data) return out;
    for (const t of TYPES) out[t] = indexSeries(data.completed_by_day[t] ?? []);
    return out;
  });
  let maxTotal = $derived.by(() => {
    let m = 1; // floor of 1 so an all-zeros chart still renders
    for (const date of calendar) {
      let up = 0, down = 0;
      for (const t of TYPES) {
        up += createdIdx[t][date] ?? 0;
        down += completedIdx[t][date] ?? 0;
      }
      m = Math.max(m, up, down);
    }
    return m;
  });

  // SVG geometry. Width adapts to the panel; height is fixed per axis.
  const CHART_W = 420;
  const HALF_H = 90;       // pixels above + below baseline
  const CHART_H = HALF_H * 2 + 24; // include axis label margin
  const BASELINE = HALF_H;
  let barW = $derived((CHART_W - 8) / WINDOW_DAYS - 1.5);

  function dayX(i: number): number {
    return 4 + i * ((CHART_W - 8) / WINDOW_DAYS);
  }
  function scaleH(count: number): number {
    return (count / maxTotal) * (HALF_H - 4);
  }

  // Hover tooltip state.
  let hoverDate = $state<string | null>(null);
  function tooltipFor(date: string): string {
    const lines: string[] = [date];
    for (const t of TYPES) {
      const c = createdIdx[t][date] ?? 0;
      const d = completedIdx[t][date] ?? 0;
      if (c || d) lines.push(`${TYPE_LABELS[t]}: +${c} / -${d}`);
    }
    if (lines.length === 1) lines.push('No activity');
    return lines.join('\n');
  }

  // Friendly short labels for the x-axis: month tick on the 1st of
  // each month, day tick every 7 days.
  function xLabel(date: string, i: number): string | null {
    const parts = date.split('-'); // YYYY-MM-DD
    const day = parts[2];
    if (day === '01') {
      const m = parseInt(parts[1], 10);
      return ['Jan','Feb','Mar','Apr','May','Jun','Jul','Aug','Sep','Oct','Nov','Dec'][m - 1];
    }
    if (i === 0 || (WINDOW_DAYS - 1 - i) % 7 === 0) return day;
    return null;
  }

  // ----- Panel #2: velocity sparklines -----

  function fmtLocalDate(d: Date): string {
    return (
      d.getFullYear() +
      '-' +
      String(d.getMonth() + 1).padStart(2, '0') +
      '-' +
      String(d.getDate()).padStart(2, '0')
    );
  }

  // n week-buckets of YYYY-MM-DD strings, oldest first; weeks start
  // Monday, the last bucket is the current (possibly partial) week.
  function buildWeeks(n: number): string[][] {
    const now = new Date();
    const dow = (now.getDay() + 6) % 7; // 0 = Monday
    const monday = new Date(now.getFullYear(), now.getMonth(), now.getDate() - dow);
    const out: string[][] = [];
    for (let w = n - 1; w >= 0; w--) {
      const days: string[] = [];
      for (let d = 0; d < 7; d++) {
        days.push(
          fmtLocalDate(
            new Date(monday.getFullYear(), monday.getMonth(), monday.getDate() - w * 7 + d),
          ),
        );
      }
      out.push(days);
    }
    return out;
  }

  let weeks = $derived(buildWeeks(WEEKS));
  let weeklyClosed = $derived.by(() => {
    const out: Record<TypeSlug, number[]> = { todos: [], bugs: [], use_cases: [] };
    for (const t of TYPES) {
      out[t] = weeks.map((days) =>
        days.reduce((sum, d) => sum + (completedIdx[t][d] ?? 0), 0),
      );
    }
    return out;
  });

  const SPARK_W = 120;
  const SPARK_H = 28;
  function sparkX(i: number, n: number): number {
    return 3 + (i * (SPARK_W - 6)) / Math.max(1, n - 1);
  }
  function sparkY(v: number, series: number[]): number {
    const max = Math.max(1, ...series);
    return SPARK_H - 3 - (v / max) * (SPARK_H - 6);
  }
  function sparkPoints(series: number[]): string {
    return series.map((v, i) => `${sparkX(i, series.length)},${sparkY(v, series)}`).join(' ');
  }
  function weekLabel(i: number): string {
    return `wk of ${weeks[i][0].slice(5)}`; // MM-DD of that week's Monday
  }

  // ----- Panel #4: time to close -----

  function fmtHours(h: number): string {
    if (h < 1) return `${Math.round(h * 60)}m`;
    if (h < 48) return `${h.toFixed(h < 10 ? 1 : 0)}h`;
    return `${(h / 24).toFixed(1)}d`;
  }

  let ttcMax = $derived.by(() => {
    let m = 1;
    if (!lifecycle) return m;
    for (const t of TYPES) m = Math.max(m, lifecycle.time_to_close[t]?.p90_hours ?? 0);
    return m;
  });
  let hasTtc = $derived(
    !!lifecycle && TYPES.some((t) => (lifecycle!.time_to_close[t]?.count ?? 0) > 0),
  );
  function ttcWidth(hours: number): number {
    return Math.max(2, (hours / ttcMax) * 100);
  }

  // ----- Panel #5: per-branch breakdown -----

  type BranchDisplayRow = {
    label: string;
    counts: Record<string, number>;
    total: number;
    muted: boolean;
  };
  let branchRows = $derived.by((): BranchDisplayRow[] => {
    if (!lifecycle) return [];
    const rows: BranchDisplayRow[] = lifecycle.branches.map((b) => ({
      label: b.branch,
      counts: b.counts,
      total: b.total,
      muted: false,
    }));
    const pseudo = (label: string, counts: Record<string, number>) => {
      const total = Object.values(counts).reduce((a, b) => a + b, 0);
      if (total > 0) rows.push({ label, counts, total, muted: true });
    };
    pseudo('(unknown commit)', lifecycle.unresolved);
    pseudo('(no commit)', lifecycle.no_commit);
    return rows;
  });
  let branchMax = $derived(Math.max(1, ...branchRows.map((r) => r.total)));

  // ----- Panel #3: indexing health -----

  const COVERAGE_ROWS: [string, string][] = [
    ['items', 'Items'],
    ['todos', 'Todos'],
    ['bugs', 'Bugs'],
    ['kb', 'KB'],
    ['use_cases', 'Use cases'],
  ];
  const PROVENANCE_LABELS: Record<string, string> = {
    'user-set': 'user-set',
    'url-detected': 'URL-detected',
    'file-dropped': 'file-dropped',
    'agent-suggested': 'agent (reverse)',
  };

  function pct(part: number, total: number): number {
    return total > 0 ? Math.round((part / total) * 100) : 0;
  }
  function fmtBytes(b: number): string {
    if (b < 1024) return `${b} B`;
    if (b < 1024 * 1024) return `${(b / 1024).toFixed(1)} KB`;
    return `${(b / (1024 * 1024)).toFixed(1)} MB`;
  }
  let anchorBreakdown = $derived.by(() => {
    if (!health) return '';
    const parts = Object.entries(health.anchors_by_provenance)
      .filter(([, n]) => n > 0)
      .sort((a, b) => b[1] - a[1])
      .map(([prov, n]) => `${PROVENANCE_LABELS[prov] ?? prov} ${n}`);
    return parts.length ? ` (${parts.join(' · ')})` : '';
  });
</script>

<aside class="dash" class:open aria-hidden={!open} aria-label="Dashboard panel">
  <header class="head">
    <div class="title-wrap">
      <span class="title">Dashboard</span>
      {#if projectName}
        <span class="subtitle" title="Project scope — rolls up every scratchpad in this project">
          {projectName} · all scratchpads
        </span>
      {/if}
    </div>
    <button
      type="button"
      class="x"
      onclick={onClose}
      aria-label="Close dashboard"
      title="Close (Esc)"
    >×</button>
  </header>

  <div class="content">
    {#if err}
      <div class="err">{err}</div>
    {/if}
    {#if loading && !data && !health && !lifecycle}
      <div class="muted">Loading…</div>
    {:else}
      {#if data}
      <section class="open-counts">
        <div class="section-label">Open right now</div>
        <div class="chip-row">
          {#each TYPES as t (t)}
            <div class="chip" style:--accent={TYPE_COLORS[t]}>
              <span class="chip-count">{data.open[t] ?? 0}</span>
              <span class="chip-label">{TYPE_LABELS[t]}</span>
            </div>
          {/each}
        </div>
      </section>

      <section class="chart-block">
        <div class="section-label">
          <span>Throughput · last {WINDOW_DAYS} days</span>
          <span class="legend">
            {#each TYPES as t (t)}
              <span class="legend-item">
                <span class="swatch" style:background={TYPE_COLORS[t]}></span>
                {TYPE_LABELS[t]}
              </span>
            {/each}
          </span>
        </div>
        <div class="section-hint">
          Created stack above the line, completed stack below — taller above = more queued, taller below = more shipped.
        </div>
        <svg
          class="chart"
          viewBox="0 0 {CHART_W} {CHART_H}"
          preserveAspectRatio="none"
          aria-label="Diverging bar chart of created and completed items per day"
        >
          <!-- Baseline -->
          <line
            x1="0" y1={BASELINE}
            x2={CHART_W} y2={BASELINE}
            stroke="#333" stroke-width="1"
          />
          <!-- In-chart directional labels — placed at the left edge so
               the "above = created / below = completed" key sits right
               next to the bars instead of in a separate legend row. -->
          <text x="4" y="10" fill="#888" font-size="9">↑ Created</text>
          <text x="4" y={CHART_H - 14} fill="#888" font-size="9">↓ Completed</text>
          {#each calendar as date, i (date)}
            {@const x = dayX(i)}
            {@const label = xLabel(date, i)}
            <!-- Created stack (above baseline) -->
            {@const c1 = createdIdx.todos[date] ?? 0}
            {@const c2 = createdIdx.bugs[date] ?? 0}
            {@const c3 = createdIdx.use_cases[date] ?? 0}
            {@const h1 = scaleH(c1)}
            {@const h2 = scaleH(c2)}
            {@const h3 = scaleH(c3)}
            {#if c1 > 0}
              <rect x={x} y={BASELINE - h1} width={barW} height={h1}
                    fill={TYPE_COLORS.todos} />
            {/if}
            {#if c2 > 0}
              <rect x={x} y={BASELINE - h1 - h2} width={barW} height={h2}
                    fill={TYPE_COLORS.bugs} />
            {/if}
            {#if c3 > 0}
              <rect x={x} y={BASELINE - h1 - h2 - h3} width={barW} height={h3}
                    fill={TYPE_COLORS.use_cases} />
            {/if}
            <!-- Completed stack (below baseline) -->
            {@const d1 = completedIdx.todos[date] ?? 0}
            {@const d2 = completedIdx.bugs[date] ?? 0}
            {@const d3 = completedIdx.use_cases[date] ?? 0}
            {@const dh1 = scaleH(d1)}
            {@const dh2 = scaleH(d2)}
            {@const dh3 = scaleH(d3)}
            {#if d1 > 0}
              <rect x={x} y={BASELINE} width={barW} height={dh1}
                    fill={TYPE_COLORS.todos} opacity="0.55" />
            {/if}
            {#if d2 > 0}
              <rect x={x} y={BASELINE + dh1} width={barW} height={dh2}
                    fill={TYPE_COLORS.bugs} opacity="0.55" />
            {/if}
            {#if d3 > 0}
              <rect x={x} y={BASELINE + dh1 + dh2} width={barW} height={dh3}
                    fill={TYPE_COLORS.use_cases} opacity="0.55" />
            {/if}
            <!-- Invisible hover hit-target spanning the full column -->
            <rect
              x={x - 0.75} y="0"
              width={barW + 1.5} height={CHART_H}
              fill="transparent"
              onmouseenter={() => (hoverDate = date)}
              onmouseleave={() => (hoverDate = null)}
              role="img"
              aria-label={tooltipFor(date)}
            ><title>{tooltipFor(date)}</title></rect>
            {#if label}
              <text
                x={x + barW / 2}
                y={CHART_H - 4}
                text-anchor="middle"
                fill="#666"
                font-size="9"
              >{label}</text>
            {/if}
          {/each}
        </svg>
        <div class="axis-legend">
          <span class="muted-small">y-axis scale: {maxTotal} max/day</span>
        </div>
        {#if hoverDate}
          <div class="hover-line">{tooltipFor(hoverDate).replace(/\n/g, ' · ')}</div>
        {/if}
      </section>

      <!-- Panel #2: velocity sparklines -->
      <section class="chart-block">
        <div class="section-label"><span>Velocity · weekly closed · last {WEEKS} weeks</span></div>
        <div class="spark-row">
          {#each TYPES as t (t)}
            {@const series = weeklyClosed[t]}
            {@const total = series.reduce((a, b) => a + b, 0)}
            <div class="spark-card" style:--accent={TYPE_COLORS[t]}>
              <div class="spark-head">
                <span class="spark-label">{TYPE_LABELS[t]}</span>
                <span class="spark-total" title="Closed in the last {WEEKS} weeks">{total}</span>
              </div>
              <svg
                class="spark"
                viewBox="0 0 {SPARK_W} {SPARK_H}"
                aria-label="Weekly closed {TYPE_LABELS[t]} sparkline"
              >
                <polyline
                  points={sparkPoints(series)}
                  fill="none"
                  stroke={TYPE_COLORS[t]}
                  stroke-width="1.5"
                  stroke-linejoin="round"
                />
                {#each series as v, i (i)}
                  <circle cx={sparkX(i, series.length)} cy={sparkY(v, series)} r="1.6" fill={TYPE_COLORS[t]}>
                    <title>{weekLabel(i)}: {v} closed</title>
                  </circle>
                {/each}
              </svg>
            </div>
          {/each}
        </div>
      </section>
      {/if}

      {#if queue}
        <section class="rq">
          <div class="section-label">Review & trust</div>
          <div class="trust trust-{queue.trust.tier}" title={queue.trust.explanation}>
            <span class="trust-tier">Trust: {queue.trust.tier}</span>
            <span class="trust-meta">cleanly verified {queue.trust.clean}/{queue.trust.total} · escalating {queue.escalate_at_or_above}+</span>
          </div>
          <div class="rq-stats">
            <span class="rq-stat"><strong>{queue.items.filter((i) => i.needs_review && !i.decision).length}</strong> awaiting sign-off</span>
            <span class="rq-stat"><strong>{queue.items.filter((i) => !i.needs_review).length}</strong> auto-cleared on earned trust</span>
            {#if onOpenNeedsReview && queue.items.some((i) => i.needs_review && !i.decision)}
              <button class="rq-open" onclick={onOpenNeedsReview}>Open Needs review →</button>
            {/if}
          </div>
          {#if queue.items.length === 0}
            <p class="muted small">No implemented items yet — risk routing appears once the agent records work.</p>
          {/if}
        </section>
      {/if}

      {#if drift && (drift.untraced.length || drift.unbuilt.length || drift.diverged.length)}
        <section class="drift">
          <div class="section-label">Drift</div>
          {#if drift.untraced.length}
            <div class="drift-group">
              <div class="drift-head untraced">Untraced code <span class="drift-n">{drift.untraced.length}</span></div>
              <p class="drift-hint">Commits with no linked intent{drift.commits_scanned ? ` (of ${drift.commits_scanned} since you started tracking)` : ''} — the #1 AI-dev smell.</p>
              <ul class="drift-list">
                {#each drift.untraced.slice(0, 12) as c (c.sha)}
                  <li class="drift-row"><span class="drift-sha">{c.short_sha}</span><span class="drift-txt" title={c.subject}>{c.subject}</span></li>
                {/each}
                {#if drift.untraced.length > 12}<li class="drift-more">+{drift.untraced.length - 12} more</li>{/if}
              </ul>
            </div>
          {/if}
          {#if drift.unbuilt.length}
            <div class="drift-group">
              <div class="drift-head unbuilt">Unbuilt intent <span class="drift-n">{drift.unbuilt.length}</span></div>
              <p class="drift-hint">Marked done, but no implementation was recorded.</p>
              <ul class="drift-list">
                {#each drift.unbuilt as it (it.owner_type + it.id)}
                  <li class="drift-row"><span class="drift-num">#{it.number}</span><span class="drift-txt" title={it.reason}>{it.subject}</span></li>
                {/each}
              </ul>
            </div>
          {/if}
          {#if drift.diverged.length}
            <div class="drift-group">
              <div class="drift-head diverged">Diverged <span class="drift-n">{drift.diverged.length}</span></div>
              <p class="drift-hint">Built, but the implementation no longer matches its contract.</p>
              <ul class="drift-list">
                {#each drift.diverged as it (it.owner_type + it.id)}
                  <li class="drift-row"><span class="drift-num">#{it.number}</span><span class="drift-txt" title={it.reason}>{it.subject}</span></li>
                {/each}
              </ul>
            </div>
          {/if}
        </section>
      {:else if drift && drift.repo_configured}
        <section class="drift">
          <div class="section-label">Drift</div>
          <p class="muted small">No drift — every recorded change traces to intent, and built items match their contracts.</p>
        </section>
      {/if}

      {#if sideErr}
        <div class="err">{sideErr}</div>
      {/if}

      {#if lifecycle}
        <!-- Panel #4: time to close -->
        <section class="chart-block">
          <div class="section-label"><span>Time to close</span></div>
          {#if !hasTtc}
            <div class="muted-inline">No closed items yet.</div>
          {:else}
            <div class="ttc-rows">
              {#each TYPES as t (t)}
                {@const st = lifecycle.time_to_close[t]}
                {#if st && st.count > 0}
                  <div class="ttc-row" style:--accent={TYPE_COLORS[t]}>
                    <div class="ttc-head">
                      <span class="ttc-label">{TYPE_LABELS[t]}</span>
                      <span class="ttc-n">{st.count} closed</span>
                    </div>
                    <div class="ttc-bar" title="Median time from created to closed">
                      <span class="bar" style:width="{ttcWidth(st.median_hours)}%"></span>
                      <span class="val">median {fmtHours(st.median_hours)}</span>
                    </div>
                    <div class="ttc-bar p90" title="90th percentile time from created to closed">
                      <span class="bar" style:width="{ttcWidth(st.p90_hours)}%"></span>
                      <span class="val">p90 {fmtHours(st.p90_hours)}</span>
                    </div>
                  </div>
                {/if}
              {/each}
            </div>
          {/if}
        </section>

        <!-- Panel #5: per-branch breakdown -->
        <section class="chart-block">
          <div class="section-label"><span>Closed per branch</span></div>
          {#if !lifecycle.repo_available}
            <div class="muted-inline">
              Set the project's repo root to attribute closed items to branches.
            </div>
          {:else if branchRows.length === 0}
            <div class="muted-inline">No closed items with commits yet.</div>
          {:else}
            <div class="section-hint">
              Branch attribution is heuristic — work on merged or deleted branches folds into
              {lifecycle.default_branch || 'the default branch'}.
            </div>
            <div class="branch-rows">
              {#each branchRows as row (row.label)}
                <div class="branch-row" class:muted-row={row.muted}>
                  <span class="branch-name" title={row.label}>{row.label}</span>
                  <div class="branch-bar">
                    {#each TYPES as t (t)}
                      {@const n = row.counts[t] ?? 0}
                      {#if n > 0}
                        <span
                          class="seg"
                          style:background={TYPE_COLORS[t]}
                          style:width="{(n / branchMax) * 100}%"
                          title="{TYPE_LABELS[t]}: {n}"
                        ></span>
                      {/if}
                    {/each}
                  </div>
                  <span class="branch-total">{row.total}</span>
                </div>
              {/each}
            </div>
          {/if}
        </section>
      {/if}

      {#if health}
        <!-- Panel #3: indexing health -->
        <section class="chart-block">
          <div class="section-label"><span>Indexing health</span></div>
          <div class="cov-rows">
            {#each COVERAGE_ROWS as [slug, label] (slug)}
              {@const cc = health.coverage[slug] ?? { embedded: 0, total: 0 }}
              <div class="cov-row">
                <span class="cov-label">{label}</span>
                <div class="cov-bar" title="Embedding coverage: {cc.embedded} of {cc.total}">
                  <span class="bar" style:width="{pct(cc.embedded, cc.total)}%"></span>
                </div>
                <span class="cov-val">
                  {cc.total ? `${cc.embedded}/${cc.total} · ${pct(cc.embedded, cc.total)}%` : '—'}
                </span>
              </div>
            {/each}
          </div>
          <div class="health-lines">
            <div class="health-line">
              <span class="hl-key">Code chunks</span>
              <span class="hl-val">
                {health.chunk_count} · {fmtBytes(health.chunk_bytes)}{health.chunk_unembedded
                  ? ` · ${health.chunk_unembedded} pending`
                  : ''}{health.chunk_failed ? ` · ${health.chunk_failed} failed` : ''}
              </span>
            </div>
            <div class="health-line">
              <span class="hl-key">Anchors</span>
              <span class="hl-val">{health.anchor_total}{anchorBreakdown}</span>
            </div>
            <div class="health-line">
              <span class="hl-key">Dedup flags</span>
              <span class="hl-val">
                {health.dedup_flagged} of {health.coverage['items']?.total ?? 0} items
                ({pct(health.dedup_flagged, health.coverage['items']?.total ?? 0)}%)
              </span>
            </div>
          </div>
        </section>
      {/if}
    {/if}
  </div>
</aside>

<style>
  .dash {
    position: fixed;
    top: 0;
    right: 0;
    bottom: 0;
    width: 460px;
    max-width: 90vw;
    background: var(--p-0d0d0d);
    border-left: 1px solid var(--p-333333);
    display: flex;
    flex-direction: column;
    transform: translateX(100%);
    transition: transform 220ms ease;
    z-index: 200;
    box-shadow: -8px 0 24px rgba(0, 0, 0, 0.45);
    pointer-events: none;
  }
  .dash.open {
    transform: translateX(0);
    pointer-events: auto;
  }
  .head {
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 8px 10px;
    background: var(--p-111111);
    border-bottom: 1px solid var(--p-262626);
    flex-shrink: 0;
  }
  .title-wrap {
    flex: 1;
    display: flex;
    flex-direction: column;
    gap: 1px;
    min-width: 0;
  }
  .title {
    font-size: 12px;
    color: var(--p-dddddd);
    text-transform: uppercase;
    letter-spacing: 0.5px;
  }
  .subtitle {
    font-size: 10px;
    color: var(--p-888888);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .x {
    background: transparent;
    border: none;
    color: var(--p-888888);
    font-size: 18px;
    line-height: 1;
    padding: 2px 8px;
    border-radius: 3px;
    cursor: pointer;
  }
  .x:hover { color: var(--p-ffffff); background: var(--p-1a1a1a); }
  .content {
    flex: 1;
    overflow-y: auto;
    min-height: 0;
    padding: 12px;
  }
  .err {
    color: var(--p-ff8888);
    font-size: 12px;
    padding: 8px;
    background: var(--p-2a1414);
    border: 1px solid var(--p-4a2020);
    border-radius: 3px;
    margin-bottom: 8px;
  }
  .muted {
    color: var(--p-777777);
    font-size: 12px;
    font-style: italic;
    padding: 12px;
  }
  .muted-small { color: var(--p-666666); font-size: 10px; }
  .muted.small { padding: 4px 0; }
  .rq { margin-top: 14px; }
  .rq-stats { display: flex; align-items: center; gap: 14px; margin-top: 8px; font-size: 12px; color: var(--p-aaaaaa); }
  .rq-stat strong { color: var(--p-e8e8e8); font-size: 14px; margin-right: 3px; }
  .rq-open {
    margin-left: auto; font-size: 11.5px; padding: 3px 10px; border-radius: 4px; cursor: pointer;
    background: var(--p-1a2530); color: var(--p-99ccff); border: 1px solid var(--p-2d5578);
  }
  .rq-open:hover { background: var(--p-2d5578); color: var(--p-cceeff); }
  .rq-need {
    text-transform: none;
    letter-spacing: 0;
    color: var(--p-e07a7a);
    font-size: 11px;
  }
  .trust {
    display: flex;
    align-items: baseline;
    gap: 8px;
    margin-bottom: 6px;
    padding: 4px 8px;
    border-radius: 4px;
    background: var(--p-141414);
    border-left: 2px solid var(--p-555555);
  }
  .trust-New { border-left-color: var(--p-888888); }
  .trust-Building { border-left-color: var(--p-d4a54d); }
  .trust-Earned { border-left-color: var(--p-66cc66); }
  .trust-tier { font-size: 12px; font-weight: 600; color: var(--p-dddddd); }
  .trust-meta { font-size: 11px; color: var(--p-888888); }
  .rq-list { list-style: none; margin: 0; padding: 0; display: flex; flex-direction: column; gap: 3px; }
  .rq-row.cleared { opacity: 0.5; }
  .rq-row {
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 3px 6px;
    border-radius: 4px;
    background: var(--p-141414);
    border: 1px solid var(--p-1f1f1f);
  }
  .rq-row.flagged { border-color: var(--p-4a2020); }
  .rq-band {
    flex: none;
    width: 58px;
    text-align: center;
    font-size: 10px;
    font-weight: 600;
    padding: 1px 0;
    border-radius: 3px;
    border: 1px solid currentColor;
  }
  .rq-band.Low { color: var(--p-888888); }
  .rq-band.Medium { color: var(--p-d4a54d); }
  .rq-band.High { color: var(--p-e07a7a); }
  .rq-band.Critical { color: var(--p-ff8888); }
  .rq-band.Unknown { color: var(--p-d4a54d); }
  .rq-item {
    flex: 1;
    min-width: 0;
    font-size: 12px;
    color: var(--p-cccccc);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .rq-num { color: var(--p-777777); margin-right: 5px; }
  .rq-domain { color: var(--p-777777); margin-left: 6px; font-size: 11px; font-style: italic; }
  .rq-flags { display: inline-flex; gap: 4px; flex: none; }
  .rq-flag {
    font-size: 9px;
    padding: 0 4px;
    border-radius: 3px;
    border: 1px solid var(--p-333333);
    color: var(--p-999999);
  }
  .rq-flag.zone { color: var(--p-d4a54d); }
  .rq-flag.bad { color: var(--p-e07a7a); border-color: var(--p-4a2020); }
  .rq-flag.warn { color: var(--p-d4a54d); }
  .rq-flag.ok { color: var(--p-66cc66); }
  .rq-actions { display: inline-flex; gap: 2px; flex: none; }
  .rq-act {
    background: none;
    border: 1px solid var(--p-333333);
    border-radius: 3px;
    cursor: pointer;
    font-size: 10px;
    line-height: 1;
    padding: 2px 5px;
    color: var(--p-888888);
  }
  .rq-act:hover:not(:disabled) { color: var(--p-ffffff); }
  .rq-act.ok:hover:not(:disabled) { color: var(--p-66cc66); }
  .rq-act.no:hover:not(:disabled) { color: var(--p-e07a7a); border-color: var(--p-4a2020); }
  .rq-act:disabled { opacity: 0.5; cursor: default; }
  .drift { margin-top: 14px; }
  .drift-group { margin-bottom: 10px; }
  .drift-head {
    font-size: 12px;
    font-weight: 600;
    color: var(--p-dddddd);
    display: flex;
    align-items: center;
    gap: 6px;
  }
  .drift-head.untraced { color: var(--p-e07a7a); }
  .drift-head.unbuilt { color: var(--p-d4a54d); }
  .drift-head.diverged { color: var(--p-e07a7a); }
  .drift-n {
    font-size: 10px;
    font-weight: 600;
    color: var(--p-aaaaaa);
    background: var(--p-1a1a1a);
    border: 1px solid var(--p-333333);
    border-radius: 8px;
    padding: 0 6px;
  }
  .drift-hint { margin: 2px 0 4px; font-size: 11px; color: var(--p-888888); }
  .drift-list { list-style: none; margin: 0; padding: 0; display: flex; flex-direction: column; gap: 2px; }
  .drift-row { display: flex; align-items: center; gap: 8px; font-size: 12px; }
  .drift-sha { font-family: var(--font-mono, monospace); font-size: 11px; color: var(--p-99ccff); flex: none; }
  .drift-num { color: var(--p-777777); flex: none; }
  .drift-txt { color: var(--p-cccccc); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .drift-more { font-size: 11px; color: var(--p-777777); font-style: italic; }
  .section-label {
    color: var(--p-aaaaaa);
    font-size: 11px;
    text-transform: uppercase;
    letter-spacing: 0.5px;
    margin-bottom: 4px;
    display: flex;
    align-items: center;
    gap: 8px;
  }
  .section-hint {
    color: var(--p-888888);
    font-size: 11px;
    line-height: 1.4;
    margin-bottom: 6px;
  }
  .open-counts {
    margin-bottom: 16px;
  }
  .chip-row {
    display: flex;
    gap: 8px;
    flex-wrap: wrap;
  }
  .chip {
    background: var(--p-161616);
    border: 1px solid var(--p-2a2a2a);
    border-left: 3px solid var(--accent);
    border-radius: 3px;
    padding: 6px 10px;
    display: flex;
    flex-direction: column;
    align-items: flex-start;
    min-width: 80px;
  }
  .chip-count {
    color: var(--accent);
    font-size: 18px;
    font-weight: 600;
    font-variant-numeric: tabular-nums;
    line-height: 1.1;
  }
  .chip-label {
    color: var(--p-888888);
    font-size: 10px;
    text-transform: uppercase;
    letter-spacing: 0.5px;
  }
  .chart-block {
    margin-top: 4px;
  }
  .legend {
    margin-left: auto;
    display: flex;
    gap: 8px;
    text-transform: none;
    letter-spacing: 0;
    font-size: 10px;
    color: var(--p-888888);
  }
  .legend-item {
    display: inline-flex;
    align-items: center;
    gap: 4px;
  }
  .swatch {
    width: 8px;
    height: 8px;
    border-radius: 1px;
    display: inline-block;
  }
  .chart {
    width: 100%;
    height: auto;
    display: block;
    background: var(--p-0a0a0a);
    border: 1px solid var(--p-1f1f1f);
    border-radius: 3px;
  }
  .axis-legend {
    display: flex;
    justify-content: space-between;
    align-items: baseline;
    margin-top: 4px;
    font-size: 10px;
    color: var(--p-888888);
  }
  .hover-line {
    margin-top: 6px;
    font-size: 11px;
    color: var(--p-cccccc);
    background: var(--p-161616);
    border: 1px solid var(--p-262626);
    border-radius: 3px;
    padding: 4px 8px;
    font-variant-numeric: tabular-nums;
  }
  .chart-block + .chart-block {
    margin-top: 18px;
  }
  .muted-inline {
    color: var(--p-777777);
    font-size: 11px;
    font-style: italic;
  }

  /* Panel #2: velocity sparklines */
  .spark-row {
    display: flex;
    gap: 8px;
  }
  .spark-card {
    flex: 1;
    min-width: 0;
    background: var(--p-161616);
    border: 1px solid var(--p-2a2a2a);
    border-left: 3px solid var(--accent);
    border-radius: 3px;
    padding: 6px 8px;
  }
  .spark-head {
    display: flex;
    align-items: baseline;
    justify-content: space-between;
    margin-bottom: 4px;
  }
  .spark-label {
    color: var(--p-888888);
    font-size: 10px;
    text-transform: uppercase;
    letter-spacing: 0.5px;
  }
  .spark-total {
    color: var(--accent);
    font-size: 14px;
    font-weight: 600;
    font-variant-numeric: tabular-nums;
  }
  .spark {
    width: 100%;
    height: auto;
    display: block;
  }

  /* Panel #4: time to close */
  .ttc-rows {
    display: flex;
    flex-direction: column;
    gap: 10px;
  }
  .ttc-head {
    display: flex;
    align-items: baseline;
    gap: 8px;
    margin-bottom: 3px;
  }
  .ttc-label {
    color: var(--accent);
    font-size: 11px;
    text-transform: uppercase;
    letter-spacing: 0.5px;
  }
  .ttc-n {
    color: var(--p-777777);
    font-size: 10px;
    font-variant-numeric: tabular-nums;
  }
  .ttc-bar {
    display: flex;
    align-items: center;
    gap: 6px;
    margin-top: 2px;
  }
  .ttc-bar .bar {
    height: 6px;
    background: var(--accent);
    border-radius: 2px;
    flex-shrink: 0;
  }
  .ttc-bar.p90 .bar {
    opacity: 0.45;
  }
  .ttc-bar .val {
    color: var(--p-999999);
    font-size: 10px;
    white-space: nowrap;
    font-variant-numeric: tabular-nums;
  }

  /* Panel #5: per-branch breakdown */
  .branch-rows {
    display: flex;
    flex-direction: column;
    gap: 6px;
  }
  .branch-row {
    display: grid;
    grid-template-columns: 130px 1fr 28px;
    align-items: center;
    gap: 8px;
  }
  .branch-row.muted-row .branch-name {
    color: var(--p-666666);
    font-style: italic;
  }
  .branch-row.muted-row .seg {
    opacity: 0.4;
  }
  .branch-name {
    color: var(--p-bbbbbb);
    font-size: 11px;
    font-family: ui-monospace, monospace;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .branch-bar {
    display: flex;
    height: 10px;
    background: var(--p-0a0a0a);
    border: 1px solid var(--p-1f1f1f);
    border-radius: 2px;
    overflow: hidden;
  }
  .branch-bar .seg {
    height: 100%;
  }
  .branch-total {
    color: var(--p-999999);
    font-size: 10px;
    text-align: right;
    font-variant-numeric: tabular-nums;
  }

  /* Panel #3: indexing health */
  .cov-rows {
    display: flex;
    flex-direction: column;
    gap: 4px;
    margin-bottom: 10px;
  }
  .cov-row {
    display: grid;
    grid-template-columns: 70px 1fr 90px;
    align-items: center;
    gap: 8px;
  }
  .cov-label {
    color: var(--p-888888);
    font-size: 10px;
    text-transform: uppercase;
    letter-spacing: 0.5px;
  }
  .cov-bar {
    height: 6px;
    background: var(--p-0a0a0a);
    border: 1px solid var(--p-1f1f1f);
    border-radius: 2px;
    overflow: hidden;
  }
  .cov-bar .bar {
    display: block;
    height: 100%;
    background: var(--p-66aa99);
  }
  .cov-val {
    color: var(--p-999999);
    font-size: 10px;
    text-align: right;
    font-variant-numeric: tabular-nums;
    white-space: nowrap;
  }
  .health-lines {
    display: flex;
    flex-direction: column;
    gap: 3px;
  }
  .health-line {
    display: flex;
    gap: 8px;
    font-size: 11px;
  }
  .hl-key {
    color: var(--p-888888);
    min-width: 80px;
  }
  .hl-val {
    color: var(--p-bbbbbb);
    font-variant-numeric: tabular-nums;
  }
</style>
