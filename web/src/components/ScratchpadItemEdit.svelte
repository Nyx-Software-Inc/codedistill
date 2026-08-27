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
  import Modal from './Modal.svelte';
  import * as api from '../lib/api';
  import CodeAnchorChips from './CodeAnchorChips.svelte';
  import Throughline from './Throughline.svelte';
  import type {
    ScratchpadItem,
    ClassificationOverride,
    TodoItem,
    BugItem,
    Priority,
    TodoStatus,
    Severity,
    BugStatus,
  } from '../lib/types';
  import { copyToClipboard, itemToMarkdown } from '../lib/markdown';

  type Props = {
    item: ScratchpadItem | null;
    onClose: () => void;
    onSaved: () => void;
    // UC-9: jump-to-code for file anchors (host closes; App opens canvas).
    onOpenFile?: (path: string, revision: string) => void;
  };
  let { item, onClose, onSaved, onOpenFile }: Props = $props();

  let copyStatus = $state<string>('');
  async function copyAsMarkdown() {
    if (!draft) return;
    const ok = await copyToClipboard(itemToMarkdown(draft));
    copyStatus = ok ? 'Copied as Markdown' : 'Copy failed';
    setTimeout(() => (copyStatus = ''), 1200);
  }

  // Snapshot by id so the 3s poll re-emitting an equivalent row doesn't clobber edits.
  let draft = $state<ScratchpadItem | null>(null);
  let snapId = $state<string | null>(null);
  // The override the item REALLY has — used at save time so merely
  // opening the editor never silently pins an override (UC-10).
  let originalOverride = $state<ClassificationOverride>('');
  $effect(() => {
    if (item?.id !== snapId) {
      draft = item ? { ...item } : null;
      snapId = item?.id ?? null;
      saveErr = '';
      originalOverride = (item?.classification_override ?? '') as ClassificationOverride;
      // UC-10: pre-select the classifier's determined type instead of
      // "Auto" so the segment reflects what the item actually is.
      if (draft && !draft.classification_override) {
        const cat = draft.proposed_category ?? '';
        if (cat === 'todo' || cat === 'bug' || cat === 'kb' || cat === 'use_case') {
          draft.classification_override = cat;
        }
      }
    }
  });

  let saving = $state(false);
  let saveErr = $state('');

  let tagInput = $state('');

  // Derived row — when a scratchpad item is classified as todo/bug, the
  // corresponding row in todo_items / bug_items is fetched here so its
  // category-specific fields are editable alongside the source. Save commits
  // both rows.
  type DerivedKind = 'todo' | 'bug' | null;
  let derivedKind = $state<DerivedKind>(null);
  let todoDraft = $state<TodoItem | null>(null);
  let bugDraft = $state<BugItem | null>(null);
  let bugInitialStatus = $state<BugStatus | null>(null);
  let derivedErr = $state('');
  let loadingDerived = $state(false);

  // The category to fetch by: proposed_category names the table that holds
  // derived_item_id. Override may name a different category — when the agent
  // re-derives, proposed_category catches up; until then, we render whatever
  // currently lives in the DB.
  // Request-sequence guard: switching source items fast can leave an older
  // getTodo/getBug resolving after the newer one and painting the wrong item's
  // fields in (audit M20). A superseded response is dropped.
  let derivedSeq = 0;
  $effect(() => {
    const id = item?.derived_item_id;
    const cat = item?.proposed_category;
    if (!id || (cat !== 'todo' && cat !== 'bug')) {
      derivedSeq++; // invalidate any in-flight load
      derivedKind = null;
      todoDraft = null;
      bugDraft = null;
      bugInitialStatus = null;
      derivedErr = '';
      loadingDerived = false;
      return;
    }
    const my = ++derivedSeq;
    loadingDerived = true;
    derivedErr = '';
    if (cat === 'todo') {
      api.getTodo(id)
        .then((t) => {
          if (my !== derivedSeq) return;
          todoDraft = { ...t };
          bugDraft = null;
          bugInitialStatus = null;
          derivedKind = 'todo';
        })
        .catch((e) => { if (my !== derivedSeq) return; derivedErr = String(e); derivedKind = null; })
        .finally(() => { if (my === derivedSeq) loadingDerived = false; });
    } else {
      api.getBug(id)
        .then((b) => {
          if (my !== derivedSeq) return;
          bugDraft = { ...b };
          bugInitialStatus = b.status;
          todoDraft = null;
          derivedKind = 'bug';
        })
        .catch((e) => { if (my !== derivedSeq) return; derivedErr = String(e); derivedKind = null; })
        .finally(() => { if (my === derivedSeq) loadingDerived = false; });
    }
  });

  const PRIORITIES: Priority[] = ['high', 'medium', 'low', 'none'];
  const TODO_STATUSES: TodoStatus[] = ['incomplete', 'complete'];
  const SEVERITIES: Severity[] = ['critical', 'major', 'minor', 'trivial'];
  // All statuses available always — workflow is unrestricted (any → any).
  // Ordered: active states, then fix-type terminals, then non-fix terminals.
  const ALL_BUG_STATUSES: BugStatus[] = [
    'open', 'investigating', 'in-progress',
    'fixed', 'verified', 'closed',
    'not_a_bug', 'wont_fix', 'duplicate',
  ];
  let allowedBugStatuses = $derived.by<BugStatus[]>(() => {
    const cur = bugInitialStatus ?? 'open';
    if (ALL_BUG_STATUSES.includes(cur)) return ALL_BUG_STATUSES;
    return [cur, ...ALL_BUG_STATUSES];
  });

  // Tabs: when the item has a derived Todo / Bug, the derived editor is
  // the primary view; the raw scratchpad fields live on a Source tab.
  // For items without a derived editor (KB / Use Case / unprocessed),
  // there's nothing to tab — show the source form directly.
  // Show the tab strip when the item is *classified as* todo or bug
  // (those are the kinds with a derived editor here), even before the
  // draft has loaded — the Derived tab handles the loading state.
  let hasDerivedEditor = $derived(
    derivedKind === 'todo' || derivedKind === 'bug',
  );
  let derivedLabel = $derived(
    derivedKind === 'todo' ? 'Todo' :
    derivedKind === 'bug' ? 'Bug' : 'Derived',
  );
  let activeTab = $state<'derived' | 'source' | 'throughline'>('derived');
  // Reset to derived tab whenever a new item opens.
  $effect(() => {
    void item?.id;
    activeTab = 'derived';
  });
  // First non-blank line of content — the throughline's intent label when
  // the item has no explicit name.
  function firstLine(s: string | undefined): string {
    return (s || '').split('\n').map((l) => l.trim()).find((l) => l) ?? '';
  }

  // Phase 1 limitation: only classification_override changes re-enqueue the agent.
  // Editing content alone will NOT trigger re-classification — any derived todo/bug/KB
  // stays as-is. See internal/api/items.go updateItem() for the wiring.

  // canSave: text-shaped items need non-empty content; binary items
  // (image / file — bytes live in the blob, content column is
  // deliberately empty) only need to exist. Without this, the Save
  // button stayed disabled on image/file items because the inherited
  // text-only rule `!draft.content.trim()` evaluated true.
  let canSave = $derived.by(() => {
    if (!draft) return false;
    const ct = draft.content_type;
    if (ct === 'image' || ct === 'file') return true;
    return (draft.content ?? '').trim().length > 0;
  });

  async function save() {
    if (!draft || !item || saving) return;
    saving = true;
    saveErr = '';
    // Commit a pending tag from the chip input (Enter not pressed but user hit Save).
    commitTag();
    try {
      await api.updateItem(item.id, {
        name: draft.name ?? '',
        content: draft.content,
        // If the user left the pre-selected determined type untouched
        // (and no override existed before), persist '' — selecting what
        // the classifier already decided shouldn't pin an override.
        classification_override: (originalOverride === '' &&
          (draft.classification_override ?? '') === (draft.proposed_category ?? '')
            ? ''
            : (draft.classification_override ?? '')) as ClassificationOverride,
        // A typed note appends to the item's activity log (the blob field is
        // retired) — only send one when the user actually wrote something.
        ...((draft.annotations ?? '').trim() !== '' ? { annotations: draft.annotations } : {}),
        tags: draft.tags ?? [],
      });
      if (derivedKind === 'todo' && todoDraft) {
        await api.updateTodo(todoDraft.id, {
          subject: todoDraft.subject,
          priority: todoDraft.priority,
          status: todoDraft.status,
        });
      } else if (derivedKind === 'bug' && bugDraft) {
        // Only include status when changed — server rejects no-op transitions
        // for a status already at its terminal value.
        const body: Parameters<typeof api.updateBug>[1] = {
          subject: bugDraft.subject,
          severity: bugDraft.severity,
          steps_to_reproduce: bugDraft.steps_to_reproduce ?? '',
          expected_behavior: bugDraft.expected_behavior ?? '',
          actual_behavior: bugDraft.actual_behavior ?? '',
          environment: bugDraft.environment ?? '',
          affected_component: bugDraft.affected_component ?? '',
        };
        if (bugDraft.status !== bugInitialStatus) body.status = bugDraft.status;
        await api.updateBug(bugDraft.id, body);
      }
      onSaved();
      onClose();
    } catch (e) {
      saveErr = String(e);
    } finally {
      saving = false;
    }
  }

  function cancel() { onClose(); }

  function setOverride(o: ClassificationOverride) {
    if (!draft) return;
    draft.classification_override = o;
  }

  function commitTag() {
    if (!draft) return;
    const t = tagInput.trim().toLowerCase();
    tagInput = '';
    if (!t) return;
    if (!draft.tags) draft.tags = [];
    if (draft.tags.includes(t)) return;
    draft.tags = [...draft.tags, t];
  }

  function removeTag(t: string) {
    if (!draft) return;
    draft.tags = draft.tags.filter((x) => x !== t);
  }

  function tagKey(e: KeyboardEvent) {
    if (e.key === 'Enter' || e.key === ',') {
      e.preventDefault();
      commitTag();
    } else if (e.key === 'Backspace' && tagInput === '' && draft?.tags?.length) {
      e.preventDefault();
      draft.tags = draft.tags.slice(0, -1);
    }
  }
</script>

<Modal open={item !== null} title="Edit scratchpad item" onClose={cancel}>
  {#if draft}
    <div class="modal-tabs">
      {#if hasDerivedEditor}
        <button class="modal-tab" class:active={activeTab === 'derived'} onclick={() => (activeTab = 'derived')}>{derivedLabel}</button>
      {/if}
      <button class="modal-tab" class:active={activeTab === 'source' || (activeTab === 'derived' && !hasDerivedEditor)} onclick={() => (activeTab = 'source')}>Source</button>
      <button class="modal-tab" class:active={activeTab === 'throughline'} onclick={() => (activeTab = 'throughline')}>Throughline</button>
    </div>

    {#if hasDerivedEditor && activeTab === 'derived'}
    {#if derivedKind === 'todo' && todoDraft}
      <section class="derived">
        <label class="field">
          <span>Subject</span>
          <input type="text" bind:value={todoDraft.subject} />
        </label>
        <div class="row2">
          <label class="field">
            <span>Priority</span>
            <select bind:value={todoDraft.priority}>
              {#each PRIORITIES as p}
                <option value={p}>{p}</option>
              {/each}
            </select>
          </label>
          <label class="field">
            <span>Status</span>
            <select bind:value={todoDraft.status}>
              {#each TODO_STATUSES as s}
                <option value={s}>{s}</option>
              {/each}
            </select>
          </label>
        </div>
        <CodeAnchorChips ownerType="todo_item" ownerId={todoDraft.id} {onOpenFile} />
      </section>
    {:else if derivedKind === 'bug' && bugDraft}
      <section class="derived">
        <label class="field">
          <span>Subject</span>
          <input type="text" bind:value={bugDraft.subject} />
        </label>
        <div class="row2">
          <label class="field">
            <span>Severity</span>
            <select bind:value={bugDraft.severity}>
              {#each SEVERITIES as s}
                <option value={s}>{s}</option>
              {/each}
            </select>
          </label>
          <label class="field">
            <span>Status</span>
            <select bind:value={bugDraft.status}>
              {#each allowedBugStatuses as s}
                <option value={s}>{s}</option>
              {/each}
            </select>
          </label>
        </div>
        <label class="field">
          <span>Steps to reproduce</span>
          <textarea bind:value={bugDraft.steps_to_reproduce} rows="3"></textarea>
        </label>
        <label class="field">
          <span>Expected behavior</span>
          <textarea bind:value={bugDraft.expected_behavior} rows="2"></textarea>
        </label>
        <label class="field">
          <span>Actual behavior</span>
          <textarea bind:value={bugDraft.actual_behavior} rows="2"></textarea>
        </label>
        <div class="row2">
          <label class="field">
            <span>Environment</span>
            <input type="text" bind:value={bugDraft.environment} />
          </label>
          <label class="field">
            <span>Affected component</span>
            <input type="text" bind:value={bugDraft.affected_component} />
          </label>
        </div>
        <CodeAnchorChips ownerType="bug_item" ownerId={bugDraft.id} {onOpenFile} />
      </section>
    {:else if loadingDerived}
      <p class="muted">Loading derived item…</p>
    {:else if derivedErr}
      <p class="err">Failed to load derived item: {derivedErr}</p>
    {/if}
    {/if}

    {#if activeTab === 'source' || (activeTab === 'derived' && !hasDerivedEditor)}
    <section class="form">
      <label class="field">
        <span>Name (optional)</span>
        <input
          type="text"
          bind:value={draft.name}
          placeholder="Optional label — falls back to first line of content when empty"
        />
      </label>

      <label class="field">
        <span>Content</span>
        <textarea bind:value={draft.content} rows="10"></textarea>
      </label>

      <label class="field">
        <span>Add note</span>
        <textarea
          bind:value={draft.annotations}
          rows="3"
          placeholder="Appends a note to this item's activity log on save"
        ></textarea>
      </label>

      <div class="field">
        <span>Tags</span>
        <div class="chips">
          {#each draft.tags ?? [] as t (t)}
            <span class="chip">
              #{t}
              <button
                type="button"
                class="chip-x"
                onclick={() => removeTag(t)}
                aria-label="Remove tag {t}"
              >×</button>
            </span>
          {/each}
          <input
            class="chip-input"
            type="text"
            bind:value={tagInput}
            onkeydown={tagKey}
            onblur={commitTag}
            placeholder={(draft.tags?.length ?? 0) === 0 ? 'Add tags — Enter or comma to commit' : ''}
          />
        </div>
      </div>

      <CodeAnchorChips ownerType="scratchpad_item" ownerId={draft.id} {onOpenFile} />

      <div class="field">
        <span>Classification override</span>
        <div class="seg" role="radiogroup" aria-label="Classification override">
          <button
            type="button"
            class:on={(draft.classification_override ?? '') === ''}
            onclick={() => setOverride('')}
          >Auto</button>
          <button
            type="button"
            class:on={draft.classification_override === 'todo'}
            onclick={() => setOverride('todo')}
          >Todo</button>
          <button
            type="button"
            class:on={draft.classification_override === 'bug'}
            onclick={() => setOverride('bug')}
          >Bug</button>
          <button
            type="button"
            class:on={draft.classification_override === 'kb'}
            onclick={() => setOverride('kb')}
          >KB</button>
          <button
            type="button"
            class:on={draft.classification_override === 'use_case'}
            onclick={() => setOverride('use_case')}
          >Use Case</button>
          <button
            type="button"
            class:on={draft.classification_override === 'skip'}
            onclick={() => setOverride('skip')}
          >Skip</button>
        </div>
        <span class="hint">
          Changing the override re-classifies the item. Editing content alone does not.
        </span>
      </div>

      <div class="meta-grid">
        <div><span class="k">State</span><span class="v">{draft.classification_state}</span></div>
        {#if draft.proposed_category}
          <div><span class="k">Proposed</span><span class="v">{draft.proposed_category}</span></div>
        {/if}
        <div><span class="k">ID</span><span class="v mono">{draft.id}</span></div>
      </div>

      {#if saveErr}
        <div class="err">{saveErr}</div>
      {/if}
    </section>
    {/if}

    {#if activeTab === 'throughline'}
      <section class="throughline-tab">
        <Throughline
          ownerType="scratchpad_item"
          ownerId={draft.id}
          intentTitle={draft.name || firstLine(draft.content)}
          {onOpenFile}
        />
      </section>
    {/if}
  {/if}

  {#snippet footer()}
    {#if copyStatus}
      <span class="copy-flash">{copyStatus}</span>
    {/if}
    <button type="button" class="copy-md" onclick={copyAsMarkdown} disabled={!draft}>
      Copy as Markdown
    </button>
    <button onclick={cancel} disabled={saving}>Cancel</button>
    <button class="primary" onclick={save} disabled={saving || !canSave}>
      {saving ? 'Saving…' : 'Save'}
    </button>
  {/snippet}
</Modal>

<style>
  .modal-tabs {
    display: flex;
    gap: 0;
    border-bottom: 1px solid var(--p-262626);
    margin: -4px -4px 12px;
  }
  .modal-tab {
    background: transparent;
    border: none;
    border-bottom: 2px solid transparent;
    color: var(--p-888888);
    padding: 6px 14px 5px;
    font-size: 11px;
    text-transform: uppercase;
    letter-spacing: 0.5px;
    cursor: pointer;
    border-radius: 0;
    margin-bottom: -1px;
  }
  .modal-tab:hover { color: var(--p-cccccc); }
  .modal-tab.active {
    color: var(--p-ffffff);
    border-bottom-color: var(--p-66ccff);
  }
  .form { margin-bottom: 8px; }
  .throughline-tab { max-height: 60vh; overflow-y: auto; padding: 2px; }
  .derived {
    padding-bottom: 12px;
    margin-bottom: 16px;
  }
  .row2 { display: grid; grid-template-columns: 1fr 1fr; gap: 12px; }
  .muted { color: var(--p-777777); font-size: 12px; font-style: italic; margin: 0 0 12px 0; }
  .field {
    display: flex;
    flex-direction: column;
    gap: 4px;
    margin-bottom: 12px;
  }
  .field > span {
    font-size: 10px;
    color: var(--p-888888);
    text-transform: uppercase;
    letter-spacing: 0.5px;
  }
  .field textarea, .field input {
    background: var(--p-0a0a0a);
    color: var(--p-eeeeee);
    border: 1px solid var(--p-333333);
    padding: 6px 8px;
    font-size: 13px;
    font-family: ui-monospace, SFMono-Regular, Consolas, monospace;
  }
  .field textarea {
    resize: vertical;
    min-height: 60px;
  }
  .copy-md {
    background: transparent;
    border: 1px solid var(--p-333333);
    color: var(--p-aaaaaa);
    font-size: 12px;
  }
  .copy-md:hover:not(:disabled) {
    color: var(--p-99ccff);
    border-color: var(--p-2d5578);
  }
  .copy-flash {
    color: var(--p-66cc66);
    font-size: 11px;
    font-style: italic;
    align-self: center;
    margin-right: auto;
  }
  .chips {
    display: flex;
    flex-wrap: wrap;
    gap: 4px;
    align-items: center;
    background: var(--p-0a0a0a);
    border: 1px solid var(--p-333333);
    padding: 4px 6px;
    min-height: 30px;
  }
  .chip {
    display: inline-flex;
    align-items: center;
    gap: 2px;
    background: var(--p-1e3a52);
    color: var(--p-99ccff);
    font-size: 11px;
    padding: 1px 4px 1px 6px;
    border-radius: 3px;
    font-family: ui-monospace, monospace;
  }
  .chip-x {
    border: none;
    background: transparent;
    color: var(--p-99ccff);
    font-size: 12px;
    padding: 0 2px;
    line-height: 1;
    cursor: pointer;
  }
  .chip-x:hover { color: var(--p-ffffff); }
  .chip-input {
    flex: 1;
    min-width: 120px;
    background: transparent;
    color: var(--p-eeeeee);
    border: none;
    font-size: 12px;
    padding: 2px 4px;
    outline: none;
  }
  .hint {
    color: var(--p-666666);
    font-size: 11px;
    font-style: italic;
    text-transform: none;
    letter-spacing: 0;
    margin-top: 4px;
  }
  .seg {
    display: inline-flex;
    border: 1px solid var(--p-333333);
    border-radius: 4px;
    overflow: hidden;
    background: var(--p-111111);
    width: fit-content;
  }
  .seg button {
    background: transparent;
    border: none;
    border-right: 1px solid var(--p-2a2a2a);
    color: var(--p-888888);
    padding: 4px 14px;
    font-size: 12px;
    border-radius: 0;
    letter-spacing: 0.3px;
    cursor: pointer;
  }
  .seg button:last-child { border-right: none; }
  .seg button:hover { color: var(--p-dddddd); background: var(--p-1a1a1a); }
  .seg button.on {
    background: var(--p-1e3a52);
    color: var(--p-99ccff);
  }
  .meta-grid {
    display: grid;
    grid-template-columns: repeat(2, 1fr);
    gap: 4px 16px;
    margin-top: 8px;
    padding-top: 8px;
    border-top: 1px solid var(--p-262626);
  }
  .meta-grid > div {
    display: grid;
    grid-template-columns: 80px 1fr;
    font-size: 12px;
    padding: 3px 0;
  }
  .k {
    color: var(--p-888888);
    text-transform: uppercase;
    font-size: 10px;
    letter-spacing: 0.5px;
    padding-top: 2px;
  }
  .v { color: var(--p-dddddd); font-size: 12px; }
  .mono { font-family: ui-monospace, monospace; font-size: 11px; color: var(--p-aaaaaa); }
  .err { color: var(--p-ff8888); font-size: 12px; margin-top: 6px; }
  .primary {
    background: var(--p-1e3a52);
    color: var(--p-99ccff);
    border-color: var(--p-2d5578);
  }
  .primary:hover:not(:disabled) { background: var(--p-2d5578); }
</style>
