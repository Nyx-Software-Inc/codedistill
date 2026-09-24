<!--
  Reviewing what a decomposition proposed.

  A run produces proposals, never items. Until this existed the only way to turn
  one into work was the CLI's -accept, which meant the UI could start a
  forty-minute job and then had nowhere to put the answer.

  Batch, not click-per-row. Forty proposals reviewed one button at a time is
  forty round trips and no way to change your mind before committing; here you
  work through the list, then apply once. Three buckets rather than two — a
  linked proposal creates nothing but is not a rejection either, its citation
  attaches to work that already exists.
-->
<script lang="ts">
  import * as api from '../lib/api';
  import type { DecomposeRunDetail, DecomposeProposal, Decision } from '../lib/api';
  import Modal from './Modal.svelte';

  type Props = {
    jobId: string | null;
    projectId: string;
    onClose: () => void;
    /** Fired after a successful apply, so the lists behind this refresh. */
    onApplied?: () => void;
  };
  let { jobId, projectId, onClose, onApplied }: Props = $props();

  let detail = $state<DecomposeRunDetail | null>(null);
  let loading = $state(false);
  let error = $state('');
  let applying = $state(false);
  let result = $state<api.BatchResult | null>(null);

  /** Per-proposal intent, by id. Absent means "create it" — the default is to
   *  accept what the run found, because a reviewer who agrees with everything
   *  should not have to click forty times to say so. */
  type Intent = { verdict: 'create' | 'off' | 'link'; itemId?: string; reason?: string };
  let intent = $state<Record<string, Intent>>({});

  /** Which citation is expanded. The quoted text is the evidence, and a
   *  reviewer deciding without reading it is guessing. */
  let openCite = $state<string | null>(null);

  /** Existing work to link against, loaded lazily — only when someone actually
   *  reaches for "link instead". */
  let existing = $state<Record<string, { id: string; label: string }[]>>({});
  let linkingFor = $state<string | null>(null);

  const pending = $derived((detail?.proposals ?? []).filter((p) => p.status === 'pending'));
  const decided = $derived((detail?.proposals ?? []).filter((p) => p.status !== 'pending'));

  const counts = $derived.by(() => {
    let create = 0, off = 0, linked = 0;
    for (const p of pending) {
      const v = intent[p.id]?.verdict ?? 'create';
      if (v === 'create') create++;
      else if (v === 'link') linked++;
      else off++;
    }
    return { create, off, linked };
  });

  /** Grouped by kind, in the order work is usually thought about rather than
   *  alphabetically: what the system should do, then what to build, then what
   *  is broken, then what was learned. */
  const KIND_ORDER = ['use_case', 'todo', 'bug', 'kb'];
  const KIND_LABEL: Record<string, string> = {
    use_case: 'Use cases', todo: 'To do', bug: 'Bugs', kb: 'Knowledge',
  };
  const groups = $derived.by(() => {
    const by = new Map<string, DecomposeProposal[]>();
    for (const p of pending) {
      if (!by.has(p.kind)) by.set(p.kind, []);
      by.get(p.kind)!.push(p);
    }
    return [...by.entries()].sort(
      (a, b) => (KIND_ORDER.indexOf(a[0]) + 99 * +(KIND_ORDER.indexOf(a[0]) < 0))
              - (KIND_ORDER.indexOf(b[0]) + 99 * +(KIND_ORDER.indexOf(b[0]) < 0)),
    );
  });

  /** Document coverage: which lines produced something. The honest counterpart
   *  to a proposal list — it shows what the run did NOT find anything in, which
   *  a list of findings can never show. */
  const coverage = $derived.by(() => {
    const lines = detail?.source_lines?.length ?? 0;
    if (!lines) return [];
    const BUCKETS = Math.min(60, lines);
    const hit = new Array(BUCKETS).fill(0);
    for (const p of detail?.proposals ?? []) {
      for (const [a, b] of p.lines ?? []) {
        for (let l = a; l <= b; l++) {
          const i = Math.min(BUCKETS - 1, Math.floor(((l - 1) / lines) * BUCKETS));
          if (i >= 0) hit[i]++;
        }
      }
    }
    return hit;
  });

  $effect(() => {
    const j = jobId;
    if (!j) { detail = null; return; }
    void (async () => {
      loading = true; error = ''; result = null; intent = {}; openCite = null;
      try {
        detail = await api.getDecomposeRun(j);
      } catch (e) {
        error = e instanceof Error ? e.message : String(e);
      } finally {
        loading = false;
      }
    })();
  });

  function set(id: string, patch: Partial<Intent>) {
    const cur = intent[id] ?? { verdict: 'create' as const };
    intent[id] = { ...cur, ...patch };
  }

  function quoted(p: DecomposeProposal): string {
    const lines = detail?.source_lines;
    if (!lines) return '';
    const out: string[] = [];
    for (const [a, b] of p.lines ?? []) {
      for (let l = a; l <= b && l <= lines.length; l++) {
        if (l >= 1) out.push(`${l}  ${lines[l - 1]}`);
      }
    }
    return out.join('\n');
  }

  async function loadExisting(kind: string) {
    if (existing[kind]) return;
    try {
      const rows =
        kind === 'todo' ? (await api.listTodos(projectId)).map((t) => ({ id: t.id, label: t.subject }))
        : kind === 'bug' ? (await api.listBugs(projectId)).map((b) => ({ id: b.id, label: b.subject }))
        : kind === 'use_case' ? (await api.listUseCases(projectId)).map((u) => ({ id: u.id, label: u.subject }))
        : [];
      existing[kind] = rows;
    } catch {
      existing[kind] = [];
    }
  }

  async function apply() {
    if (!detail || !jobId) return;
    applying = true; error = '';
    try {
      const decisions: Decision[] = pending.map((p) => {
        const i = intent[p.id] ?? { verdict: 'create' as const };
        if (i.verdict === 'link') return { id: p.id, status: 'linked', item_id: i.itemId };
        if (i.verdict === 'off') return { id: p.id, status: 'rejected', reason: i.reason };
        return { id: p.id, status: 'accepted' };
      });
      result = await api.decideProposals(jobId, decisions);
      detail = await api.getDecomposeRun(jobId);
      intent = {};
      onApplied?.();
    } catch (e) {
      error = e instanceof Error ? e.message : String(e);
    } finally {
      applying = false;
    }
  }
</script>

<Modal open={!!jobId} onClose={onClose} title="Review proposals" width="58rem">
  {#if loading}
    <p class="muted">Loading…</p>
  {:else if error}
    <p class="err" role="alert">{error}</p>
  {:else if detail}
    <header class="runhead">
      <div>
        <strong>{detail.run.source_label}</strong>
        <span class="sub">
          {detail.run.sentences} sentences · {detail.run.total} proposals
          {#if detail.run.model} · read by {detail.run.model}{/if}
        </span>
      </div>
      {#if detail.run.pending === 0}
        <span class="done">all reviewed</span>
      {/if}
    </header>

    {#if coverage.length}
      <!-- What the run found nothing in. A findings list cannot show this, and
           silence about an unread section looks identical to a section with
           nothing in it. -->
      <div class="coverage" aria-label="Document coverage">
        {#each coverage as n, i}
          <div
            class="cov"
            class:hit={n > 0}
            title={`${Math.round((i / coverage.length) * 100)}% through the document — ${n > 0 ? `${n} citation(s)` : 'nothing proposed from here'}`}
          ></div>
        {/each}
      </div>
      <p class="covnote">
        Each block is a slice of the document. Dark blocks produced nothing — worth a
        glance before accepting, because that is where a miss hides.
      </p>
    {:else if !detail.source_lines}
      <p class="warn">
        The source document has been deleted, so citations cannot be shown. The
        proposals are still valid work.
      </p>
    {/if}

    {#if result}
      <p class="ok" role="status">
        Created {result.accepted}{result.linked ? `, linked ${result.linked}` : ''}{result.rejected ? `, set aside ${result.rejected}` : ''}.
        {#if result.failed.length}
          <span class="warn">{result.failed.length} failed — see below.</span>
        {/if}
      </p>
      {#each result.failed as f (f.id)}
        <p class="err">{f.subject ?? f.id}: {f.error}</p>
      {/each}
    {/if}

    {#each groups as [kind, items] (kind)}
      <section class="group">
        <h3>{KIND_LABEL[kind] ?? kind} <span class="count">{items.length}</span></h3>
        {#each items as p (p.id)}
          {@const iv = intent[p.id]?.verdict ?? 'create'}
          <article class="row" class:off={iv === 'off'} class:linked={iv === 'link'}>
            <label class="tick">
              <input
                type="checkbox"
                checked={iv === 'create'}
                disabled={applying || iv === 'link'}
                onchange={(e) => set(p.id, { verdict: e.currentTarget.checked ? 'create' : 'off' })}
              />
            </label>
            <div class="body">
              <div class="subject">
                {p.subject}
                {#if p.corroborated}
                  <span class="badge corr" title="Both the prose pass and a table found this independently">corroborated</span>
                {/if}
                {#if p.priority}<span class="badge">{p.priority}</span>{/if}
                {#if p.external_ref}<span class="badge">{p.external_ref}</span>{/if}
              </div>
              {#if p.body}<p class="text">{p.body}</p>{/if}

              <div class="rowactions">
                {#if p.lines?.length}
                  <button
                    class="cite"
                    onclick={() => (openCite = openCite === p.id ? null : p.id)}
                  >
                    {p.lines.map(([a, b]) => (a === b ? `${a}` : `${a}–${b}`)).join(', ')}
                  </button>
                {:else}
                  <span class="nocite" title="No citation — this should not happen; citations are computed before any model runs">uncited</span>
                {/if}

                {#if iv === 'link'}
                  <span class="linknote">
                    Creates nothing — the citation attaches to
                    <code>{intent[p.id]?.itemId ?? '—'}</code>
                  </span>
                  <button class="linkbtn" onclick={() => set(p.id, { verdict: 'create', itemId: undefined })}>undo</button>
                {:else}
                  <button
                    class="linkbtn"
                    onclick={() => { linkingFor = linkingFor === p.id ? null : p.id; void loadExisting(p.kind); }}
                  >link to existing…</button>
                {/if}

                {#if iv === 'off'}
                  <input
                    class="reason"
                    placeholder="why not? (remembered, so it is not re-proposed blindly)"
                    value={intent[p.id]?.reason ?? ''}
                    oninput={(e) => set(p.id, { reason: e.currentTarget.value })}
                  />
                {/if}
              </div>

              {#if linkingFor === p.id && iv !== 'link'}
                <div class="linkpick">
                  <select
                    onchange={(e) => {
                      const v = e.currentTarget.value;
                      if (v) { set(p.id, { verdict: 'link', itemId: v }); linkingFor = null; }
                    }}
                  >
                    <option value="">— pick existing {KIND_LABEL[p.kind] ?? p.kind} —</option>
                    {#each existing[p.kind] ?? [] as x (x.id)}
                      <option value={x.id}>{x.label}</option>
                    {/each}
                  </select>
                  {#if (existing[p.kind] ?? []).length === 0}
                    <span class="help">Nothing of this kind exists yet to link to.</span>
                  {/if}
                </div>
              {/if}

              {#if openCite === p.id}
                <pre class="quote">{quoted(p)}</pre>
              {/if}
            </div>
          </article>
        {/each}
      </section>
    {/each}

    {#if decided.length}
      <details class="already">
        <summary>{decided.length} already decided</summary>
        {#each decided as p (p.id)}
          <div class="decidedrow">
            <span class="dstatus {p.status}">{p.status}</span>
            <span>{p.subject}</span>
            {#if p.reject_reason}<span class="why">— {p.reject_reason}</span>{/if}
          </div>
        {/each}
      </details>
    {/if}

    {#if pending.length}
      <footer class="bar">
        <span class="tally">
          <strong>{counts.create}</strong> to create
          {#if counts.linked}· <strong>{counts.linked}</strong> linked{/if}
          {#if counts.off}· <strong>{counts.off}</strong> set aside{/if}
        </span>
        <button onclick={onClose} disabled={applying}>Close</button>
        <button class="primary" onclick={apply} disabled={applying}>
          {applying ? 'Applying…' : 'Apply'}
        </button>
      </footer>
    {:else}
      <footer class="bar">
        <span class="tally muted">Nothing left to review.</span>
        <button class="primary" onclick={onClose}>Close</button>
      </footer>
    {/if}
  {/if}
</Modal>

<style>
  .runhead { display: flex; justify-content: space-between; align-items: baseline; margin-bottom: 0.6rem; }
  .sub { display: block; font-size: 0.78rem; color: var(--text-dim); }
  .done { font-size: 0.75rem; color: var(--ok, #4caf72); }

  .coverage { display: flex; gap: 2px; height: 16px; margin: 0.4rem 0 0.2rem; }
  .cov { flex: 1; background: var(--p-2a2a2a); border-radius: 1px; }
  .cov.hit { background: var(--accent); }
  .covnote { margin: 0 0 0.9rem; font-size: 0.72rem; color: var(--p-777777); }

  .group { margin-bottom: 1rem; }
  .group h3 { margin: 0 0 0.4rem; font-size: 0.85rem; text-transform: uppercase; letter-spacing: 0.04em; color: var(--text-dim); }
  .count { color: var(--p-666666); }

  .row {
    display: flex; gap: 0.6rem; padding: 0.5rem 0.6rem;
    border: 1px solid var(--border); border-radius: 6px; margin-bottom: 0.35rem;
  }
  .row.off { opacity: 0.55; }
  .row.linked { border-color: var(--accent); }
  .tick input { margin-top: 0.2rem; }
  .body { flex: 1; min-width: 0; }
  .subject { font-size: 0.88rem; }
  .text { margin: 0.2rem 0 0.35rem; font-size: 0.8rem; color: var(--text-dim); }

  .badge {
    font-size: 0.66rem; text-transform: uppercase; letter-spacing: 0.03em;
    border: 1px solid var(--p-3a3a3a); border-radius: 3px;
    padding: 0 0.28rem; margin-left: 0.35rem; color: var(--p-888888);
  }
  .badge.corr { color: var(--ok, #4caf72); border-color: var(--ok, #4caf72); }

  .rowactions { display: flex; align-items: center; gap: 0.5rem; flex-wrap: wrap; }
  .cite, .linkbtn {
    border: 1px solid var(--p-3a3a3a); background: var(--p-222222);
    color: var(--text-dim); border-radius: 3px; padding: 0 0.35rem;
    font-size: 0.72rem; font-family: var(--mono, ui-monospace); cursor: pointer;
  }
  .cite:hover, .linkbtn:hover { color: var(--text); }
  .nocite { font-size: 0.72rem; color: var(--danger, #e74c3c); }
  .linknote { font-size: 0.72rem; color: var(--accent); }
  .reason {
    flex: 1; min-width: 12rem;
    background: var(--p-1e1e1e); color: var(--text);
    border: 1px solid var(--p-3a3a3a); border-radius: 3px;
    padding: 0.15rem 0.4rem; font-size: 0.75rem;
  }
  .linkpick { margin-top: 0.35rem; }
  .linkpick select {
    background: var(--p-1e1e1e); color: var(--text);
    border: 1px solid var(--p-3a3a3a); border-radius: 4px;
    padding: 0.2rem 0.4rem; font-size: 0.78rem; max-width: 26rem;
  }
  .quote {
    margin: 0.4rem 0 0; padding: 0.4rem 0.5rem; overflow-x: auto;
    background: var(--p-1a1a1a); border-left: 2px solid var(--accent);
    font-family: var(--mono, ui-monospace); font-size: 0.72rem;
    white-space: pre; color: var(--text-dim);
  }

  .already { margin: 0.6rem 0; font-size: 0.8rem; color: var(--text-dim); }
  .already summary { cursor: pointer; }
  .decidedrow { display: flex; gap: 0.5rem; padding: 0.15rem 0; }
  .dstatus { font-size: 0.68rem; text-transform: uppercase; min-width: 5rem; }
  .dstatus.accepted { color: var(--ok, #4caf72); }
  .dstatus.rejected { color: var(--p-777777); }
  .dstatus.linked { color: var(--accent); }
  .why { color: var(--p-666666); }

  .bar {
    position: sticky; bottom: 0; display: flex; align-items: center;
    gap: 0.5rem; padding-top: 0.6rem; margin-top: 0.4rem;
    border-top: 1px solid var(--border); background: var(--p-1e1e1e);
  }
  .tally { flex: 1; font-size: 0.82rem; }
  .bar button {
    border: 1px solid var(--p-3a3a3a); background: var(--p-2a2a2a);
    color: var(--text); border-radius: 4px; padding: 0.35rem 0.9rem;
    font-size: 0.85rem; cursor: pointer;
  }
  .bar .primary { background: var(--accent); border-color: var(--accent); color: #fff; }
  .bar button:disabled { opacity: 0.5; cursor: default; }

  .help { font-size: 0.72rem; color: var(--p-777777); }
  .muted { color: var(--text-dim); font-size: 0.85rem; }
  .err { color: var(--danger, #e74c3c); font-size: 0.8rem; }
  .ok { color: var(--ok, #4caf72); font-size: 0.82rem; }
  .warn { color: var(--warn, #e0a030); font-size: 0.8rem; }
</style>
