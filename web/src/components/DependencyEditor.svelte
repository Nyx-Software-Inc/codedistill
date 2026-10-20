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
  // "Blocked by" — the only way to draw a dependency by hand.
  //
  // The data model, the readiness API and the blocker chips in all three views
  // were already built, and nothing could create an edge: the sole producer was
  // the decompose sweep, which managed two edges across forty-five items. So
  // every project had an empty graph and the chips were correct and invisible.
  //
  // Self-fetching on purpose. It mounts in four detail modals, and a component
  // that needs five props wired four times gets wired wrong once.
  import * as api from '../lib/api';
  import { canAdd, blockersOf, dependentsOf, type DepEdge } from '../lib/depGraph';
  import { alertDialog } from '../lib/dialog';

  type Candidate = { kind: string; id: string; label: string };

  type Props = {
    projectId: string;
    /** This item: 'todo' | 'bug' | 'kb' | 'use_case'. */
    kind: string;
    id: string;
    /** Fired after an edge is added or removed, so the canvas chips refresh. */
    onChanged?: () => void;
  };
  let { projectId, kind, id, onChanged }: Props = $props();

  let edges = $state<DepEdge[]>([]);
  let raw = $state<api.ItemDependency[]>([]);
  let candidates = $state<Candidate[]>([]);
  let loading = $state(false);
  let err = $state('');
  let adding = $state(false);
  let picked = $state('');
  let filter = $state('');
  let loadedFor = $state('');

  // Reload when the modal is pointed at a different item. Read inside the
  // effect so both values are subscribed — a value captured at init would be
  // the previous item's forever.
  $effect(() => {
    const key = `${projectId}:${kind}:${id}`;
    if (projectId && id && loadedFor !== key) {
      loadedFor = key;
      void load();
    }
  });

  async function load() {
    loading = true;
    err = '';
    try {
      const [deps, todos, bugs, ucs, kbs] = await Promise.all([
        api.listDependencies(projectId),
        api.listTodos(projectId),
        api.listBugs(projectId),
        api.listUseCases(projectId),
        api.listKB(projectId),
      ]);
      raw = deps;
      edges = deps as unknown as DepEdge[];
      const c: Candidate[] = [];
      for (const t of todos) c.push({ kind: 'todo', id: t.id, label: `TODO-${t.number} ${t.subject}` });
      for (const b of bugs) c.push({ kind: 'bug', id: b.id, label: `BUG-${b.number} ${b.subject}` });
      for (const u of ucs) c.push({ kind: 'use_case', id: u.id, label: `UC-${u.number} ${u.subject}` });
      for (const k of kbs) c.push({ kind: 'kb', id: k.id, label: `KB ${k.title}` });
      candidates = c;
    } catch (e) {
      err = String(e);
    } finally {
      loading = false;
    }
  }

  const myBlockers = $derived.by(() => blockersOf(edges, kind, id));
  const myDependents = $derived.by(() => dependentsOf(edges, kind, id));

  const labelFor = (k: string, i: string) =>
    candidates.find((c) => c.kind === k && c.id === i)?.label ?? `${k} ${i.slice(0, 8)}`;

  /** Candidates with a verdict attached, so a choice that would close a loop is
   *  DISABLED with a reason rather than offered and then refused by the server.
   *  Nothing rejects a cycle server-side today. */
  const offered = $derived.by(() => {
    const q = filter.trim().toLowerCase();
    return candidates
      .filter((c) => !q || c.label.toLowerCase().includes(q))
      .map((c) => ({ ...c, verdict: canAdd(edges, kind, id, c.kind, c.id) }))
      .slice(0, 200);
  });

  async function add() {
    const c = offered.find((o) => `${o.kind}:${o.id}` === picked);
    if (!c || !c.verdict.ok) return;
    adding = true;
    try {
      await api.createDependency({
        project_id: projectId,
        from_kind: kind, from_id: id,
        to_kind: c.kind, to_id: c.id,
        kind: 'blocks',
      });
      picked = '';
      filter = '';
      await load();
      onChanged?.();
    } catch (e) {
      void alertDialog(`Could not add the blocker: ${e}`);
    } finally {
      adding = false;
    }
  }

  async function remove(e: DepEdge) {
    const row = raw.find((r) =>
      r.from_kind === e.from_kind && r.from_id === e.from_id &&
      r.to_kind === e.to_kind && r.to_id === e.to_id);
    if (!row) return;
    try {
      await api.deleteDependency(row.id);
      await load();
      onChanged?.();
    } catch (err2) {
      void alertDialog(`Could not remove it: ${err2}`);
    }
  }
</script>

<section class="deps">
  <div class="head">
    <strong>Blocked by</strong>
    <span class="count">{myBlockers.length}</span>
    <span class="hint">what has to happen before this can start</span>
  </div>

  {#if err}<p class="err">{err}</p>{/if}

  {#if myBlockers.length === 0}
    <p class="empty">Nothing is blocking this.</p>
  {:else}
    <ul class="list">
      {#each myBlockers as b (b.to_kind + b.to_id)}
        <li>
          <span class="k {b.to_kind}">{b.to_kind.replace('_', ' ')}</span>
          <span class="lbl">{labelFor(b.to_kind, b.to_id)}</span>
          {#if b.status === 'advisory'}
            <span class="prop" title="Derived by a decompose run and in force without anyone ratifying it — advisory, because a wrong edge is noticed the moment you look at the item">advisory</span>
          {/if}
          <button class="x" title="Remove this blocker" onclick={() => remove(b)}>×</button>
        </li>
      {/each}
    </ul>
  {/if}

  <div class="add">
    <input
      type="text"
      placeholder="Filter items…"
      bind:value={filter}
      disabled={loading}
    />
    <select bind:value={picked} disabled={loading || adding}>
      <option value="">Add a blocker…</option>
      {#each offered as o (o.kind + o.id)}
        <option
          value={`${o.kind}:${o.id}`}
          disabled={!o.verdict.ok}
          title={o.verdict.ok ? '' : o.verdict.reason}
        >{o.verdict.ok ? o.label : `${o.label} — ${o.verdict.reason}`}</option>
      {/each}
    </select>
    <button class="go" disabled={!picked || adding} onclick={add}>
      {adding ? 'Adding…' : 'Add'}
    </button>
  </div>

  {#if myDependents.length}
    <div class="head sub">
      <strong>Blocks</strong>
      <span class="count">{myDependents.length}</span>
      <span class="hint">waiting on this one</span>
    </div>
    <ul class="list quiet">
      {#each myDependents as d (d.from_kind + d.from_id)}
        <li>
          <span class="k {d.from_kind}">{d.from_kind.replace('_', ' ')}</span>
          <span class="lbl">{labelFor(d.from_kind, d.from_id)}</span>
        </li>
      {/each}
    </ul>
  {/if}
</section>

<style>
  .deps { margin-top: 14px; }
  .head {
    display: flex; align-items: baseline; gap: 8px;
    font-size: 13px; color: var(--p-e8e8e8); margin-bottom: 6px;
  }
  .head.sub { margin-top: 14px; }
  .count {
    background: var(--p-1e3a52); color: var(--p-99ccff); border-radius: 9px;
    font-size: 11px; padding: 0 7px; line-height: 17px;
  }
  .hint { color: var(--p-777777); font-size: 11px; }
  .err { color: var(--p-ff8888); font-size: 12px; }
  .empty {
    color: var(--p-777777); font-size: 12px; font-style: italic;
    background: var(--p-121212); border: 1px dashed var(--p-2a2a2a);
    border-radius: 6px; padding: 8px 10px; margin: 0;
  }

  .list { list-style: none; margin: 0; padding: 0; display: flex; flex-direction: column; gap: 4px; }
  .list li {
    display: flex; align-items: center; gap: 8px;
    background: var(--p-141414); border: 1px solid var(--p-262626);
    border-radius: 6px; padding: 5px 8px;
  }
  .list.quiet li { background: none; border-color: var(--p-1f1f1f); }
  .lbl { flex: 1; min-width: 0; font-size: 12.5px; color: var(--p-dddddd);
         overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .k {
    flex: none; font-size: 10px; text-transform: uppercase; letter-spacing: 0.4px;
    border: 1px solid var(--p-333333); border-radius: 3px; padding: 1px 6px;
    color: var(--p-aaaaaa);
  }
  .k.todo { color: var(--p-8fbede); border-color: var(--p-2c4a63); }
  .k.bug { color: var(--p-e09a9a); border-color: var(--p-5a2f2f); }
  .k.kb { color: var(--p-93c7a9); border-color: var(--p-2e5240); }
  .k.use_case { color: var(--p-b9a6e8); border-color: var(--p-433863); }
  .prop {
    flex: none; font-size: 10px; color: var(--p-d9b96a);
    background: color-mix(in srgb, #d4a54d 12%, transparent);
    border-radius: 3px; padding: 1px 5px;
  }
  .x {
    flex: none; border: 0; background: none; color: var(--p-888888);
    cursor: pointer; font-size: 14px; line-height: 1; padding: 0 2px;
  }
  .x:hover { color: var(--p-ff8888); }

  .add { display: flex; gap: 6px; margin-top: 8px; }
  .add input { flex: 0 0 140px; min-width: 0; font-size: 12px; }
  .add select { flex: 1; min-width: 0; font-size: 12px; }
  .add .go { flex: none; font-size: 12px; padding: 3px 11px; }
</style>
