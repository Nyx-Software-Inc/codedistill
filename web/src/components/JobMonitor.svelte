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
  import type { Job, JobDetail, Workflow } from '../lib/api';

  // The job monitor.
  //
  // The question this exists to answer is "should I be worried?" — not "what
  // happened", which is history. At minute 25 of a 40-minute decomposition you
  // want to know whether this is normal or stuck, and everything here is
  // arranged around that.
  //
  // Which is why the typical-run comparison is not decoration: "12 minutes"
  // carries no verdict until you know the usual run is 34. It is the one
  // number that turns a progress bar into something actionable.

  type Props = {
    projectId: string;
    onClose?: () => void;
    /** Opens the submission dialog. Starting a job belongs next to the list of
     *  running ones — that is where someone is standing when they want another. */
    onNewJob?: () => void;
    /** Bumped by the submitter. A new run should appear at once rather than
     *  after the next poll — several seconds of nothing reads as a failure. */
    reloadKey?: number;
    /** Open the review for a finished decompose. A run whose proposals have
     *  nowhere to be accepted is a job that did not finish. */
    onReview?: (jobId: string) => void;
  };
  let { projectId, onClose, onNewJob, reloadKey = 0, onReview }: Props = $props();

  type Tab = 'running' | 'history';
  let tab = $state<Tab>('running');

  let jobs = $state<Job[]>([]);
  let workflows = $state<Workflow[]>([]);
  let detail = $state<JobDetail | null>(null);
  let openJob = $state<string | null>(null);
  let error = $state('');
  let loaded = $state(false);
  let note = $state('');

  // Poll rather than stream. A job changes phase every few minutes, so a
  // five-second poll is indistinguishable from live and needs no connection to
  // keep alive across a laptop sleeping — which this product has learned about
  // the hard way.
  let timer: ReturnType<typeof setInterval> | null = null;

  const running = $derived(jobs.filter((j) => j.status === 'running' || j.status === 'paused'));
  const finished = $derived(jobs.filter((j) => j.status !== 'running' && j.status !== 'paused'));
  const shown = $derived(tab === 'running' ? running : finished);

  async function load() {
    try {
      // Workflows come along for the NAME: a run's type is an id, and
      // "decompose" on a card is worse than "Decompose a document".
      const [js, wfs] = await Promise.all([
        api.listJobs(projectId, undefined, 100),
        api.listWorkflows(),
      ]);
      jobs = js ?? [];
      workflows = wfs ?? [];
      error = '';
      if (openJob) detail = await api.getJob(openJob);
    } catch (e) {
      error = e instanceof Error ? e.message : String(e);
    } finally {
      loaded = true;
    }
  }

  onMount(() => {
    load();
    timer = setInterval(load, 5000);
  });

  // A submission reloads at once rather than waiting for the next poll: several
  // seconds of nothing happening after pressing Start reads as a failure.
  let lastKey = 0;
  $effect(() => {
    if (reloadKey !== lastKey) {
      lastKey = reloadKey;
      tab = 'running';
      void load();
    }
  });
  onDestroy(() => {
    if (timer) clearInterval(timer);
  });

  async function open(id: string) {
    openJob = openJob === id ? null : id;
    detail = null;
    if (openJob) {
      try {
        detail = await api.getJob(openJob);
      } catch (e) {
        error = e instanceof Error ? e.message : String(e);
      }
    }
  }

  async function act(kind: 'pause' | 'cancel', j: Job) {
    note = '';
    try {
      if (kind === 'pause') {
        const r = await api.pauseJob(j.id);
        // Say what actually happens. A model call is atomic, so the stop lands
        // at the next checkpoint and the button must not imply otherwise.
        note = r.note;
      } else {
        await api.cancelJob(j.id);
      }
      await load();
    } catch (e) {
      error = e instanceof Error ? e.message : String(e);
    }
  }

  function workflowOf(j: Job): Workflow | undefined {
    return workflows.find((w) => w.id === j.type);
  }

  function dur(sec: number): string {
    if (sec < 60) return `${sec}s`;
    const m = Math.floor(sec / 60);
    if (m < 60) return `${m}m`;
    return `${Math.floor(m / 60)}h ${m % 60}m`;
  }

  /** The verdict, not the number. Elapsed alone says nothing; elapsed against
   *  the usual says whether to worry. */
  function pace(j: Job, medianSec: number): { label: string; cls: string } | null {
    if (!medianSec || j.status !== 'running') return null;
    const r = j.elapsed_seconds / medianSec;
    if (r < 1.2) return { label: `typical: ${dur(medianSec)}`, cls: 'ok' };
    if (r < 2) return { label: `slower than usual (${dur(medianSec)})`, cls: 'warn' };
    return { label: `far over the usual ${dur(medianSec)}`, cls: 'bad' };
  }

  function medianFor(j: Job): number {
    // The whole-run norm is the sum of its phases' medians: each phase is
    // measured separately, so a run's expected cost is their total.
    if (!detail || detail.job.id !== j.id) return 0;
    return detail.norms.reduce((n, x) => n + x.median_sec, 0);
  }

  /** Today shows a clock; anything older shows a date. The whole point is that
   *  a run from last week must not look like one from five minutes ago. */
  function startedLabel(j: Job): string {
    const d = new Date(j.started_at);
    if (Number.isNaN(d.getTime())) return '';
    const now = new Date();
    const time = d.toLocaleTimeString(undefined, { hour: '2-digit', minute: '2-digit' });
    if (d.toDateString() === now.toDateString()) return `today ${time}`;
    const yesterday = new Date(now);
    yesterday.setDate(now.getDate() - 1);
    if (d.toDateString() === yesterday.toDateString()) return `yesterday ${time}`;
    return `${d.toLocaleDateString(undefined, { day: 'numeric', month: 'short' })} ${time}`;
  }

  const STATUS_LABEL: Record<string, string> = {
    running: 'running', paused: 'paused', succeeded: 'succeeded',
    failed: 'failed', cancelled: 'cancelled', interrupted: 'interrupted',
  };
</script>

<div class="monitor">
  <header>
    <h2>Jobs</h2>
    <nav class="tabs">
      <button class:active={tab === 'running'} onclick={() => (tab = 'running')}>
        Running{running.length ? ` (${running.length})` : ''}
      </button>
      <button class:active={tab === 'history'} onclick={() => (tab = 'history')}>History</button>
    </nav>
    <span class="spacer"></span>
    {#if onNewJob}
      <button class="newjob" onclick={onNewJob} title="Start a job">+ New</button>
    {/if}
    {#if onClose}
      <button class="closebtn" onclick={onClose} aria-label="Close jobs">×</button>
    {/if}
  </header>

  {#if error}<p class="err" role="alert">{error}</p>{/if}
  {#if note}<p class="note">{note}</p>{/if}

  <!-- ── Runs ─────────────────────────────────────────────────────────── -->
  {#if !loaded}
    <p class="muted">Loading…</p>
  {:else if shown.length === 0}
    <p class="muted">
      {tab === 'running' ? 'Nothing running.' : 'No runs yet.'}
    </p>
  {:else}
    {#each shown as j (j.id)}
      {@const wf = workflowOf(j)}
      {@const p = pace(j, medianFor(j))}
      <div class="job" class:open={openJob === j.id}>
        <!-- One card per run, with a visible edge. A flat list of rows made two
             runs nine days apart read as one job duplicated. -->
        <button class="jobrow" onclick={() => open(j.id)}>
          <span class="chev">{openJob === j.id ? '▾' : '▸'}</span>
          <span class="scope" title={j.scope_label || j.scope_id || j.type}>
            {j.scope_label || j.scope_id || j.type}
          </span>
          <span class="st {j.status}">{STATUS_LABEL[j.status] ?? j.status}</span>
        </button>

        <!-- WHEN. Absent, a run from last week is indistinguishable from a
             duplicate of today's — which is exactly how one got reported as a
             phantom second job. -->
        <div class="when">
          <span title={j.started_at}>{startedLabel(j)}</span>
          <span class="sep">·</span>
          <span>{j.status === 'running' ? `${dur(j.elapsed_seconds)} so far` : `took ${dur(j.elapsed_seconds)}`}</span>
          {#if j.eta_seconds > 0}<span class="sep">·</span><span>ETA {dur(j.eta_seconds)}</span>{/if}
          {#if j.status === 'running' && j.percent >= 0}
            <span class="sep">·</span><span>{j.done}/{j.total}{j.phase ? ` ${j.phase}` : ''}</span>
          {:else if j.status === 'running' && j.phase}
            <span class="sep">·</span><span>{j.phase}</span>
          {/if}
        </div>

        <!-- WHAT IT PRODUCED. A decompose that "succeeded" has not finished
             doing anything useful until someone has judged what it found. -->
        {#if j.proposals}
          <div class="yield">
            <strong>{j.proposals}</strong> proposal{j.proposals === 1 ? '' : 's'}
            {#if j.awaiting_review}
              <span class="awaiting">{j.awaiting_review} awaiting your review</span>
            {:else}
              <span class="muted">all reviewed</span>
            {/if}
          </div>
        {/if}

        <div class="meta">
          {#if j.provider_name}
            <span class="who">{j.worker_type} → {j.provider_name}</span>
            {#if j.provider_local === false}
              <span class="tag remote">off this machine</span>
            {/if}
          {:else if j.model}
            <span class="who">{j.model}</span>
          {/if}
          {#if p}<span class="pace {p.cls}">{p.label}</span>{/if}
          {#if j.tokens_in > 0}
            <span>{(j.tokens_in / 1000).toFixed(0)}k in / {(j.tokens_out / 1000).toFixed(0)}k out</span>
          {/if}
        </div>

        <div class="acts">
          {#if j.awaiting_review && onReview}
            <button class="btn primary" onclick={() => onReview(j.id)}>Review {j.awaiting_review}</button>
          {:else if j.status === 'succeeded' && j.type === 'decompose' && onReview}
            <button class="btn" onclick={() => onReview(j.id)}>See proposals</button>
          {/if}
          {#if j.status === 'running'}
            {#if wf?.resumable}
              <button class="btn" onclick={() => act('pause', j)}>Pause</button>
            {/if}
            <button class="btn danger" onclick={() => act('cancel', j)}>Cancel</button>
          {:else if j.status === 'paused'}
            <button class="btn" onclick={() => act('cancel', j)}>Cancel</button>
          {/if}
        </div>

        {#if j.error}<p class="joberr">{j.error}</p>{/if}

        {#if openJob === j.id && detail && detail.job.id === j.id}
          <div class="phases">
            {#each detail.phases as ph (ph.id)}
              {@const norm = detail.norms.find((n) => n.phase === ph.phase)}
              <div class="ph">
                <span class="phname">
                  {ph.phase}
                  {#if ph.worker_type}<span class="muted"> · {ph.worker_type}</span>{/if}
                  {#if ph.round}<span class="muted"> · round {ph.round}</span>{/if}
                </span>
                <span class="phbar">
                  <!-- Bar against the norm, not against 100%: the useful
                       comparison is this run versus the usual one. -->
                  {#if norm?.median_sec}
                    <span
                      class="fill"
                      class:over={ph.seconds > norm.median_sec * 1.2}
                      style={`width:${Math.min(100, (ph.seconds / norm.median_sec) * 100)}%`}
                    ></span>
                  {/if}
                </span>
                <span class="phtime">{dur(ph.seconds)}{ph.running ? '…' : ''}</span>
                <span class="phnorm">
                  {#if norm?.median_sec}usually {dur(norm.median_sec)} (n={norm.runs}){/if}
                </span>
                {#if ph.error}<span class="pace bad">{ph.error}</span>{/if}
              </div>
            {/each}
            {#if detail.phases.length === 0}
              <p class="muted">No phases recorded for this run.</p>
            {/if}
          </div>
        {/if}
      </div>
    {/each}
  {/if}
</div>

<style>
  .monitor { display: flex; flex-direction: column; gap: 1rem; padding: 1rem; }
  header { display: flex; align-items: baseline; justify-content: space-between; gap: 1rem; }
  h2 { margin: 0; font-size: 1.05rem; }
  .tabs { display: flex; gap: 0.25rem; }
  .tabs button { background: none; border: 1px solid transparent; border-radius: 6px;
                 padding: 0.25rem 0.6rem; color: var(--text-dim); font: inherit;
                 font-size: 0.85rem; cursor: pointer; }
  .tabs button.active { border-color: var(--border); color: var(--text); }
  .muted { color: var(--text-dim); font-size: 0.85rem; }
  .err { color: var(--danger, #f88); font-size: 0.85rem; }
  .note { color: #d69b30; font-size: 0.85rem; }

  .spacer { flex: 1; }
  .sm.review { border-color: var(--accent); color: var(--accent); }
  .newjob {
    border: 1px solid var(--p-3a3a3a); background: var(--p-2a2a2a);
    color: var(--text); border-radius: 4px; padding: 0.15rem 0.5rem;
    font-size: 0.78rem; cursor: pointer;
  }
  .newjob:hover { background: var(--p-333333); }
  .closebtn {
    border: 0; background: none; color: var(--p-888888);
    font-size: 1.1rem; line-height: 1; cursor: pointer; padding: 0 0.2rem;
  }
  .closebtn:hover { color: var(--text); }
  /* One card per run. The old flat rows gave two runs nine days apart the
     same visual weight as two lines of the same job. */
  .job {
    border: 1px solid var(--border);
    border-radius: 8px;
    margin-bottom: 0.5rem;
    padding: 0.1rem 0 0.45rem;
    background: var(--p-1c1c1c);
    /* min-width:0 on a flex/grid child is what actually stops long content
       pushing the panel wider than its column and clipping the right edge. */
    min-width: 0;
    overflow: hidden;
  }
  .job.open { border-color: var(--p-4a4a4a); }

  .jobrow {
    display: flex; align-items: center; gap: 0.45rem;
    width: 100%; min-width: 0;
    background: none; border: 0; color: var(--text);
    padding: 0.45rem 0.6rem 0.2rem; cursor: pointer; text-align: left;
    font-size: 0.86rem;
  }
  .chev { color: var(--p-777777); flex: 0 0 auto; }
  /* The label truncates; everything after it keeps its place. Previously the
     row was laid out so a long filename pushed the status off the edge. */
  .scope { flex: 1 1 auto; min-width: 0; overflow: hidden;
           text-overflow: ellipsis; white-space: nowrap; }
  .st {
    flex: 0 0 auto; font-size: 0.68rem; text-transform: uppercase;
    letter-spacing: 0.04em; border: 1px solid currentColor;
    border-radius: 3px; padding: 0 0.3rem; color: var(--p-888888);
  }
  .st.running   { color: var(--accent); }
  .st.succeeded { color: var(--ok, #4caf72); }
  .st.failed    { color: var(--danger, #e74c3c); }
  .st.paused    { color: var(--warn, #e0a030); }

  .when, .meta {
    display: flex; flex-wrap: wrap; align-items: center; gap: 0.3rem;
    padding: 0 0.6rem 0 2rem; min-width: 0;
    font-size: 0.75rem; color: var(--text-dim);
  }
  .when { padding-top: 0.05rem; }
  .meta { padding-top: 0.2rem; }
  .sep { color: var(--p-555555); }
  .who { font-family: var(--mono, ui-monospace); }

  .yield {
    padding: 0.25rem 0.6rem 0 2rem;
    font-size: 0.78rem; color: var(--text-dim);
  }
  .yield strong { color: var(--text); }
  /* The whole point of the panel for a finished decompose: 49 things found and
     none of them are work until someone says so. */
  .awaiting { margin-left: 0.4rem; color: var(--warn, #e0a030); }

  .acts {
    display: flex; flex-wrap: wrap; gap: 0.35rem;
    padding: 0.45rem 0.6rem 0 2rem;
  }
  /* Actual buttons. Pause and Cancel read as plain text before, which is a
     poor thing for a control that stops a forty-minute run. */
  .btn {
    border: 1px solid var(--p-3a3a3a); background: var(--p-2a2a2a);
    color: var(--text); border-radius: 4px;
    padding: 0.2rem 0.6rem; font-size: 0.76rem;
    font-family: inherit; cursor: pointer;
  }
  .btn:hover { background: var(--p-333333); }
  .btn.primary { background: var(--accent); border-color: var(--accent); color: #fff; }
  .btn.primary:hover { filter: brightness(1.12); }
  .btn.danger { color: var(--danger, #e74c3c); border-color: var(--p-4a3030); }
  .btn.danger:hover { background: var(--p-3a2020); }

  .tag.remote { color: var(--warn, #e0a030); border: 1px solid currentColor;
                border-radius: 3px; padding: 0 0.25rem; font-size: 0.66rem; }
  .pace.ok { color: var(--p-777777); }
  .pace.warn { color: var(--warn, #e0a030); }
  .pace.bad { color: var(--danger, #e74c3c); }

  .joberr { margin: 0 0.7rem 0.5rem 2.3rem; font-size: 0.8rem; color: var(--danger, #f88); }
  .sm { font-size: 0.75rem; padding: 0.15rem 0.5rem; border: 1px solid var(--border);
        border-radius: 5px; background: none; color: inherit; cursor: pointer; }
  .sm.danger { color: var(--danger, #f88); }

  .phases { padding: 0.2rem 0.7rem 0.7rem 2.3rem; display: flex; flex-direction: column; gap: 0.3rem; }
  .ph { display: grid; grid-template-columns: 11rem 8rem 4rem 1fr; gap: 0.6rem;
        align-items: center; font-size: 0.78rem; }
  .phname { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .phbar { height: 6px; background: var(--border); border-radius: 3px; overflow: hidden; }
  .fill { display: block; height: 100%; background: #4caf7d; }
  .fill.over { background: #d69b30; }
  .phtime { font-variant-numeric: tabular-nums; }
  .phnorm { color: var(--text-dim); }

  @media (max-width: 800px) {
    .jobrow { grid-template-columns: 1.2rem 1fr auto; }
    .phase, .prog, .eta { display: none; }
    .ph { grid-template-columns: 1fr 4rem; }
    .phbar, .phnorm { display: none; }
  }
</style>
