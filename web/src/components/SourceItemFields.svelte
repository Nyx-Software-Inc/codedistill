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
  // Read-only provenance view of the immutable source capture (item-editing
  // redesign, slice 2). It shows the original text/object + what was captured
  // from it. The ONLY mutation allowed here is a *logged, non-cascading*
  // correction of the original text — fixing a typo never re-classifies the item
  // or clobbers the derived work item. Reclassification (explicit "Change type")
  // and the notes/annotations editor have moved off this tab (later slices + the
  // Log tab). See docs/design/item-editing-redesign.md.
  import * as api from '../lib/api';
  import CodeAnchorChips from './CodeAnchorChips.svelte';
  import type { ScratchpadItem } from '../lib/types';

  type Props = {
    itemId: string | undefined;
    onSaved: () => void;
    onOpenFile?: (path: string, revision: string) => void;
  };
  let { itemId, onSaved, onOpenFile }: Props = $props();

  let item = $state<ScratchpadItem | null>(null);
  let loadId = $state<string | null>(null);
  let loading = $state(false);
  let loadErr = $state('');

  // Text correction — the one allowed mutation. Opt-in edit; logged on save.
  let editing = $state(false);
  let draftText = $state('');
  let saving = $state(false);
  let saveErr = $state('');
  let savedFlash = $state(false);

  $effect(() => {
    const id = itemId;
    if (!id) {
      item = null;
      loadId = null;
      loading = false;
      return;
    }
    if (id === loadId) return;
    loadId = id;
    loading = true;
    loadErr = '';
    item = null;
    editing = false;
    // loadId is the sequence token: a fast item switch means an older getItem
    // can resolve last, so drop it unless it's still the current id (audit M20).
    api.getItem(id)
      .then((it) => { if (id !== loadId) return; item = it; })
      .catch((e) => { if (id !== loadId) return; loadErr = String(e); })
      .finally(() => { if (id === loadId) loading = false; });
  });

  function startEdit() {
    if (!item) return;
    draftText = item.content;
    saveErr = '';
    editing = true;
  }
  function cancelEdit() {
    editing = false;
  }
  async function saveCorrection() {
    if (!item || saving) return;
    if (draftText === item.content) {
      editing = false;
      return;
    }
    saving = true;
    saveErr = '';
    try {
      // correction:true is the deliberate-typo-fix signal — without it the
      // server refuses content edits on classified items (source guard).
      item = await api.updateItem(item.id, { content: draftText, correction: true });
      editing = false;
      savedFlash = true;
      setTimeout(() => (savedFlash = false), 1500);
      onSaved();
    } catch (e) {
      saveErr = String(e);
    } finally {
      saving = false;
    }
  }

  // Tags shown here are the ORIGINAL #hashtags captured with the source —
  // read-only provenance. Editable tags moved to the work record in the
  // canvas rework (C3): add/remove them on the derived item's own tab, not
  // on the frozen source.

  function fmtDate(s?: string): string {
    if (!s) return '';
    try {
      return new Date(s).toLocaleString();
    } catch {
      return s;
    }
  }
  function typeLabel(it: ScratchpadItem): string {
    return it.classification_override || it.proposed_category || 'unclassified';
  }
</script>

{#if !itemId}
  <p class="muted">Manually created — no source capture.</p>
{:else if loading}
  <p class="muted">Loading source…</p>
{:else if loadErr}
  <p class="err">Failed to load source: {loadErr}</p>
{:else if item}
  <section class="src">
    <p class="hint">
      The source is the immutable original you captured — read-only provenance.
      Edit the item's fields on the other tabs, and add updates in the Log. You
      may correct the original text itself (a typo, say); corrections are
      recorded and never re-classify the item.
    </p>

    <div class="field">
      <div class="field-head">
        <span>Original text</span>
        {#if !editing}
          <button class="link-btn" onclick={startEdit}>Correct…</button>
        {/if}
      </div>
      {#if editing}
        <textarea bind:value={draftText} rows="8"></textarea>
        {#if saveErr}<div class="err">{saveErr}</div>{/if}
        <div class="edit-actions">
          <button class="btn" onclick={cancelEdit} disabled={saving}>Cancel</button>
          <button class="btn primary" onclick={saveCorrection} disabled={saving || !draftText.trim()}>
            {saving ? 'Saving…' : 'Save correction'}
          </button>
        </div>
      {:else}
        <pre class="content-ro">{item.content}</pre>
        {#if savedFlash}<span class="saved-flash">Correction saved</span>{/if}
      {/if}
    </div>

    {#if (item.tags?.length ?? 0) > 0}
      <div class="field">
        <span>Original tags</span>
        <div class="chips ro">
          {#each item.tags ?? [] as t (t)}
            <span class="chip">#{t}</span>
          {/each}
        </div>
        <p class="sub">Captured with the source. Edit tags on the item's own tab.</p>
      </div>
    {/if}

    <CodeAnchorChips ownerType="scratchpad_item" ownerId={item.id} {onOpenFile} />

    <div class="meta-grid">
      <div><span class="k">Type</span><span class="v">{typeLabel(item)}</span></div>
      <div><span class="k">State</span><span class="v">{item.classification_state}</span></div>
      <div><span class="k">Captured</span><span class="v">{fmtDate(item.created_at)}</span></div>
      <div><span class="k">ID</span><span class="v mono">{item.id}</span></div>
    </div>
  </section>
{:else}
  <p class="muted">Source scratchpad item was deleted (orphaned).</p>
{/if}

<style>
  .src { margin-bottom: 4px; }
  .muted { color: var(--p-777777); font-size: 12px; font-style: italic; margin: 0 0 12px 0; }
  .hint {
    color: var(--p-888888); font-size: 11px; line-height: 1.5;
    margin: 0 0 12px 0; padding: 8px 10px;
    background: var(--p-141414); border: 1px solid var(--p-262626); border-radius: 4px;
  }
  .field { display: flex; flex-direction: column; gap: 4px; margin-bottom: 12px; }
  .field > span, .field-head > span {
    font-size: 10px; color: var(--p-888888);
    text-transform: uppercase; letter-spacing: 0.5px;
  }
  .field-head { display: flex; align-items: center; justify-content: space-between; }
  .link-btn {
    background: transparent; border: none; color: var(--p-99ccff);
    font-size: 11px; cursor: pointer; padding: 0; text-transform: none; letter-spacing: 0;
  }
  .link-btn:hover { color: var(--p-cceeff); text-decoration: underline; }
  .content-ro {
    background: var(--p-0a0a0a); color: var(--p-dddddd);
    border: 1px solid var(--p-262626); border-radius: 3px;
    padding: 8px; margin: 0; font-size: 12.5px; line-height: 1.5;
    white-space: pre-wrap; word-break: break-word; max-height: 320px; overflow: auto;
  }
  textarea {
    background: var(--p-0a0a0a); color: var(--p-eeeeee);
    border: 1px solid var(--p-333333); border-radius: 3px; padding: 6px 8px;
    font-size: 13px; font-family: ui-monospace, SFMono-Regular, Consolas, monospace;
    resize: vertical; min-height: 80px;
  }
  .edit-actions { display: flex; justify-content: flex-end; gap: 8px; margin-top: 6px; }
  .btn {
    padding: 4px 12px; font-size: 12px; border-radius: 3px; cursor: pointer;
    border: 1px solid var(--p-333333); background: var(--p-1a1a1a); color: var(--p-cccccc);
  }
  .btn.primary { background: var(--p-1e3a52); color: var(--p-99ccff); border-color: var(--p-2d5578); }
  .btn.primary:hover:not(:disabled) { background: var(--p-2d5578); }
  .btn:disabled { opacity: 0.5; cursor: default; }
  .saved-flash { color: var(--p-66cc66); font-size: 11px; font-style: italic; }
  .chips {
    display: flex; flex-wrap: wrap; gap: 4px; align-items: center;
    background: var(--p-0a0a0a); border: 1px solid var(--p-333333);
    padding: 4px 6px; min-height: 30px;
  }
  .chips.ro { opacity: 0.85; }
  .chip {
    display: inline-flex; align-items: center; gap: 2px;
    background: var(--p-1e3a52); color: var(--p-99ccff);
    font-size: 11px; padding: 1px 6px; border-radius: 3px;
    font-family: ui-monospace, monospace;
  }
  .sub { color: var(--p-888888); font-size: 11px; margin: 3px 0 0; font-style: italic; }
  .meta-grid {
    display: grid; grid-template-columns: repeat(2, 1fr); gap: 4px 16px;
    margin-top: 8px; padding-top: 8px; border-top: 1px solid var(--p-262626);
  }
  .meta-grid > div { display: grid; grid-template-columns: 80px 1fr; font-size: 12px; padding: 3px 0; }
  .k { color: var(--p-888888); text-transform: uppercase; font-size: 10px; letter-spacing: 0.5px; padding-top: 2px; }
  .v { color: var(--p-dddddd); font-size: 12px; }
  .mono { font-family: ui-monospace, monospace; font-size: 11px; color: var(--p-aaaaaa); }
  .err { color: var(--p-ff8888); font-size: 12px; margin-top: 6px; }
</style>
