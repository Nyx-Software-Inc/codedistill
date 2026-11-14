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
  // The comments at one anchor of one file, rendered under the line they
  // belong to. A thread is every comment at the same (side, lines); a reply
  // is another comment at that anchor. Each comment can be edited,
  // resolved, deleted, or promoted to a bug or todo — after which it links
  // to the item it became.
  import type { ReviewComment } from '../lib/api';
  import type { CommentAnchor, ReviewActions } from '../lib/reviewComments.svelte';

  type Props = {
    path: string;
    anchor: CommentAnchor;
    comments: ReviewComment[];
    review: ReviewActions;
  };
  let { path, anchor, comments, review }: Props = $props();

  let editing = $state<string | null>(null);
  let draft = $state('');
  let reply = $state('');
  let replying = $state(false);
  let promoting = $state<string | null>(null);
  let busy = $state(false);

  function when(iso: string): string {
    const d = new Date(iso);
    const ms = Date.now() - d.getTime();
    const m = Math.round(ms / 60000);
    if (m < 1) return 'just now';
    if (m < 60) return `${m}m ago`;
    const h = Math.round(m / 60);
    if (h < 24) return `${h}h ago`;
    const days = Math.round(h / 24);
    if (days < 14) return `${days}d ago`;
    return d.toLocaleDateString();
  }
  function startEdit(c: ReviewComment) {
    editing = c.id;
    draft = c.body;
  }
  async function run(f: () => Promise<void>) {
    if (busy) return;
    busy = true;
    try { await f(); } catch { /* the host shows review errors */ } finally { busy = false; }
  }
  async function saveEdit(id: string) {
    const b = draft.trim();
    if (!b) return;
    await run(async () => { await review.edit(id, b); editing = null; });
  }
  async function sendReply() {
    const b = reply.trim();
    if (!b) return;
    await run(async () => { await review.add(path, anchor, b); reply = ''; replying = false; });
  }
  function onKey(e: KeyboardEvent, submit: () => void, cancel: () => void) {
    if (e.key === 'Enter' && (e.ctrlKey || e.metaKey)) { e.preventDefault(); submit(); }
    if (e.key === 'Escape') { e.preventDefault(); cancel(); }
  }
  const allResolved = $derived(comments.length > 0 && comments.every((c) => !!c.resolved_at));
</script>

<div class="thread" class:resolved={allResolved}>
  {#each comments as c (c.id)}
    <div class="comment" class:done={!!c.resolved_at}>
      <div class="head">
        <span class="who">{c.author_name ?? c.author}</span>
        <span class="when" title={new Date(c.created_at).toLocaleString()}>{when(c.created_at)}</span>
        {#if c.updated_at !== c.created_at && !c.resolved_at}<span class="when">edited</span>{/if}
        {#if c.resolved_at}<span class="chip ok">resolved</span>{/if}
        {#if c.promoted_owner_type && c.promoted_owner_id}
          {@const t = c.promoted_owner_type}
          {@const id = c.promoted_owner_id}
          <button type="button" class="chip link" title="Open the item this comment became" onclick={() => review.openItem(t, id)}>
            → {t === 'bug_item' ? 'bug' : 'todo'}
          </button>
        {/if}
        <span class="actions">
          {#if editing !== c.id}
            <button type="button" class="act" disabled={busy} onclick={() => startEdit(c)}>edit</button>
            <button type="button" class="act" disabled={busy} onclick={() => void run(() => review.resolve(c.id, !c.resolved_at))}>{c.resolved_at ? 'reopen' : 'resolve'}</button>
            {#if !c.promoted_owner_id}
              {#if promoting === c.id}
                <span class="promote-menu">
                  <button type="button" class="act strong" disabled={busy} onclick={() => void run(async () => { await review.promote(c.id, 'bug'); promoting = null; })}>bug</button>
                  <button type="button" class="act strong" disabled={busy} onclick={() => void run(async () => { await review.promote(c.id, 'todo'); promoting = null; })}>todo</button>
                  <button type="button" class="act" onclick={() => (promoting = null)}>×</button>
                </span>
              {:else}
                <button type="button" class="act" disabled={busy} title="Turn this comment into a bug or todo, anchored to these lines" onclick={() => (promoting = c.id)}>promote…</button>
              {/if}
            {/if}
            <button type="button" class="act danger" disabled={busy} onclick={() => void run(() => review.remove(c.id))}>delete</button>
          {/if}
        </span>
      </div>
      {#if editing === c.id}
        <!-- svelte-ignore a11y_autofocus -->
        <textarea class="box" bind:value={draft} rows={3} autofocus
          onkeydown={(e) => onKey(e, () => void saveEdit(c.id), () => (editing = null))}></textarea>
        <div class="row">
          <button type="button" class="btn primary" disabled={busy || !draft.trim()} onclick={() => void saveEdit(c.id)}>Save</button>
          <button type="button" class="btn" disabled={busy} onclick={() => (editing = null)}>Cancel</button>
          <span class="hint">Ctrl+Enter</span>
        </div>
      {:else}
        <div class="body">{c.body}</div>
      {/if}
    </div>
  {/each}
  {#if replying}
    <!-- svelte-ignore a11y_autofocus -->
    <textarea class="box" bind:value={reply} rows={2} placeholder="Reply…" autofocus
      onkeydown={(e) => onKey(e, () => void sendReply(), () => { replying = false; reply = ''; })}></textarea>
    <div class="row">
      <button type="button" class="btn primary" disabled={busy || !reply.trim()} onclick={() => void sendReply()}>Reply</button>
      <button type="button" class="btn" disabled={busy} onclick={() => { replying = false; reply = ''; }}>Cancel</button>
      <span class="hint">Ctrl+Enter</span>
    </div>
  {:else}
    <button type="button" class="act reply" onclick={() => (replying = true)}>reply</button>
  {/if}
</div>

<style>
  .thread {
    font-family: var(--font-family, system-ui, sans-serif);
    font-size: 12px;
    line-height: 1.45;
    background: var(--p-161616);
    border-top: 1px solid var(--p-2a2a2a);
    border-bottom: 1px solid var(--p-2a2a2a);
    border-left: 3px solid var(--p-99ccff);
    padding: 6px 10px 6px 12px;
    white-space: normal;
    color: var(--p-dddddd);
  }
  .thread.resolved { border-left-color: var(--p-444444); }
  .comment { padding: 3px 0; }
  .comment + .comment { border-top: 1px dashed var(--p-2a2a2a); margin-top: 3px; padding-top: 6px; }
  .comment.done .body { color: var(--p-888888); }
  .head { display: flex; align-items: center; gap: 8px; flex-wrap: wrap; }
  .who { font-weight: 600; color: var(--p-f0f0f0); }
  .when { color: var(--p-777777); font-size: 11px; }
  .chip { font-size: 10px; padding: 0 6px; border-radius: 9px; background: var(--p-262626); color: var(--p-aaaaaa); border: none; }
  .chip.ok { color: var(--diff-add-fg); }
  .chip.link { color: var(--p-99ccff); cursor: pointer; background: var(--p-1e3a52); }
  .chip.link:hover { color: var(--p-ffffff); }
  .actions { margin-left: auto; display: inline-flex; gap: 2px; align-items: center; }
  .act {
    background: transparent; border: none; color: var(--p-888888); font-size: 11px;
    padding: 1px 5px; border-radius: 3px; cursor: pointer;
  }
  .act:hover { color: var(--p-ffffff); background: var(--p-252525); }
  .act:disabled { opacity: 0.5; cursor: default; }
  .act.strong { color: var(--p-99ccff); }
  .act.danger:hover { color: var(--p-ff8888); }
  .act.reply { margin-top: 3px; }
  .promote-menu { display: inline-flex; gap: 2px; background: var(--p-1e3a52); border-radius: 4px; padding: 0 2px; }
  .body { white-space: pre-wrap; word-break: break-word; margin-top: 2px; }
  .box {
    width: 100%; box-sizing: border-box; margin-top: 4px;
    background: var(--p-111111); border: 1px solid var(--p-333333); color: var(--p-eeeeee);
    border-radius: 4px; padding: 5px 8px; font-size: 12px; font-family: inherit; resize: vertical;
  }
  .box:focus { outline: none; border-color: var(--p-99ccff); }
  .row { display: flex; gap: 6px; align-items: center; margin-top: 4px; }
  .btn {
    font-size: 11px; padding: 3px 10px; border-radius: 4px; cursor: pointer;
    background: transparent; border: 1px solid var(--p-333333); color: var(--p-aaaaaa);
  }
  .btn.primary { background: var(--p-2d5578); border-color: var(--p-2d5578); color: var(--p-cceeff); }
  .btn:disabled { opacity: 0.5; cursor: default; }
  .hint { color: var(--p-666666); font-size: 10px; margin-left: auto; }
</style>
