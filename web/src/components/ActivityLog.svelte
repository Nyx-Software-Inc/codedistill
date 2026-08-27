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
  // Activity log tab (item-editing redesign, slice 1). A work item's append-only
  // timeline: markdown `note` entries you (or the agent) add, interleaved with
  // system events (status changes, verification, …). Append-only — you post a
  // correction, you don't rewrite history. Notes render as pre-wrapped text for
  // now; rich markdown rendering is a later polish.
  import * as api from '../lib/api';
  import type { ItemEvent } from '../lib/api';

  // sourceItemId (when the work item was derived from a capture) lets us show
  // the MERGED timeline — capture events + work-item events — via the existing
  // lineage endpoint. Notes are always appended to the work item.
  // refreshTick: bump from a parent to force a reload when an action elsewhere
  // (a review sign-off, a status change) has written a new Log entry, so the
  // open Log tab reflects it immediately instead of on the next tab switch.
  type Props = { ownerType: string; ownerId: string; sourceItemId?: string; refreshTick?: number };
  let { ownerType, ownerId, sourceItemId, refreshTick = 0 }: Props = $props();

  let events = $state<ItemEvent[]>([]);
  let loading = $state(false);
  let err = $state('');
  let loadedFor = $state('');
  let loadedTick = $state(-1);
  let draft = $state('');
  let posting = $state(false);

  $effect(() => {
    if (ownerId && (ownerId !== loadedFor || refreshTick !== loadedTick)) {
      loadedFor = ownerId;
      loadedTick = refreshTick;
      void load();
    }
  });

  async function load() {
    loading = true;
    err = '';
    try {
      events = sourceItemId
        ? await api.getItemLineage(sourceItemId)
        : await api.getActivityLog(ownerType, ownerId);
    } catch (e) {
      err = String(e);
    } finally {
      loading = false;
    }
  }

  // Newest-first for display (the API returns oldest-first).
  const ordered = $derived([...events].reverse());

  async function post() {
    const text = draft.trim();
    if (!text || posting) return;
    posting = true;
    err = '';
    try {
      const e = await api.appendNote(ownerType, ownerId, text);
      events = [...events, e];
      draft = '';
    } catch (e) {
      err = String(e);
    } finally {
      posting = false;
    }
  }

  function onKey(e: KeyboardEvent) {
    if ((e.ctrlKey || e.metaKey) && e.key === 'Enter') {
      e.preventDefault();
      post();
    }
  }

  function fmt(iso: string): string {
    try {
      return new Date(iso).toLocaleString();
    } catch {
      return iso;
    }
  }
  const kindLabel = (k: string) => k.replace(/[-_]/g, ' ');
  const sourceLabel = (s: string) => (s === 'agent' ? 'agent' : s === 'ui' ? 'you' : s);
</script>

<div class="log">
  <div class="composer">
    <textarea
      bind:value={draft}
      rows="3"
      placeholder="Add a note… (markdown; Ctrl/⌘+Enter to post)"
      onkeydown={onKey}
    ></textarea>
    <div class="composer-actions">
      {#if err}<span class="err">{err}</span>{/if}
      <button class="btn primary" disabled={posting || !draft.trim()} onclick={post}>
        {posting ? 'Posting…' : 'Add note'}
      </button>
    </div>
  </div>

  {#if loading && events.length === 0}
    <p class="muted">Loading…</p>
  {:else if ordered.length === 0}
    <p class="muted">No activity yet. Notes you add — and system events (status changes,
      verification, reclassification) — show up here, newest first.</p>
  {:else}
    <ul class="entries">
      {#each ordered as e (e.id)}
        <li class="entry" class:note={e.kind === 'note'}>
          <div class="entry-head">
            {#if e.kind === 'note'}
              <span class="badge note-badge">note</span>
            {:else}
              <span class="badge sys-badge">{kindLabel(e.kind)}</span>
            {/if}
            <span class="meta">{sourceLabel(e.source)} · {fmt(e.created_at)}</span>
          </div>
          {#if e.kind === 'note'}
            <div class="body">{e.body || e.summary}</div>
          {:else}
            <div class="summary">{e.summary}</div>
          {/if}
        </li>
      {/each}
    </ul>
  {/if}
</div>

<style>
  .log { display: flex; flex-direction: column; gap: 12px; }
  .composer { display: flex; flex-direction: column; gap: 6px; }
  .composer textarea {
    background: var(--p-0a0a0a); color: var(--p-eeeeee);
    border: 1px solid var(--p-333333); border-radius: 4px; padding: 8px;
    font-size: 13px; font-family: inherit; resize: vertical; min-height: 56px;
  }
  .composer-actions { display: flex; align-items: center; justify-content: flex-end; gap: 10px; }
  .btn {
    padding: 4px 12px; font-size: 12px; border-radius: 3px; cursor: pointer;
    border: 1px solid var(--p-333333); background: var(--p-1a1a1a); color: var(--p-cccccc);
  }
  .btn.primary { background: var(--p-1e3a52); color: var(--p-99ccff); border-color: var(--p-2d5578); }
  .btn.primary:hover:not(:disabled) { background: var(--p-2d5578); }
  .btn:disabled { opacity: 0.5; cursor: default; }
  .muted { color: var(--p-777777); font-size: 12px; font-style: italic; }
  .err { color: var(--p-ff8888); font-size: 12px; }
  .entries { list-style: none; margin: 0; padding: 0; display: flex; flex-direction: column; gap: 8px; }
  .entry {
    border: 1px solid var(--p-262626); border-radius: 6px; padding: 8px 10px;
    background: var(--p-121212);
  }
  .entry.note { border-left: 3px solid var(--p-2d5578); }
  .entry-head { display: flex; align-items: center; gap: 8px; margin-bottom: 4px; }
  .badge {
    font-size: 9px; text-transform: uppercase; letter-spacing: 0.4px;
    padding: 1px 6px; border-radius: 3px; white-space: nowrap;
  }
  .note-badge { background: var(--p-1e3a52); color: var(--p-99ccff); }
  .sys-badge { background: var(--p-2a2a2a); color: var(--p-999999); }
  .meta { font-size: 11px; color: var(--p-777777); }
  .body { font-size: 13px; color: var(--p-e0e0e0); white-space: pre-wrap; line-height: 1.5; }
  .summary { font-size: 12px; color: var(--p-bbbbbb); }
</style>
