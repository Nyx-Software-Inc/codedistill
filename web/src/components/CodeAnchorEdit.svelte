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
  import type {
    CodeAnchor,
    CodeAnchorOwnerType,
    CodeAnchorKind,
  } from '../lib/types';
  import type { CodeAnchorInput } from '../lib/api';

  type Props = {
    open: boolean;
    ownerType: CodeAnchorOwnerType;
    ownerId: string;
    // Non-null anchor = edit mode. Null = create mode.
    anchor: CodeAnchor | null;
    onClose: () => void;
    onSaved: () => void;
  };
  let { open, ownerType, ownerId, anchor, onClose, onSaved }: Props = $props();

  // Form fields. Reset whenever the modal opens or the edit-target switches.
  let kind = $state<CodeAnchorKind>('file');
  let path = $state('');
  let lineStart = $state<number | ''>('');
  let lineEnd = $state<number | ''>('');
  let revision = $state('');
  let url = $state('');
  let label = $state('');
  let saving = $state(false);
  let saveErr = $state('');

  let snapKey = $state('');
  $effect(() => {
    const key = open ? (anchor?.id ?? 'new') : '';
    if (key !== snapKey) {
      snapKey = key;
      if (anchor) {
        kind = anchor.kind;
        path = anchor.path ?? '';
        lineStart = anchor.line_start ?? '';
        lineEnd = anchor.line_end ?? '';
        revision = anchor.revision ?? '';
        url = anchor.url ?? '';
        label = anchor.label ?? '';
      } else {
        kind = 'file';
        path = '';
        lineStart = '';
        lineEnd = '';
        revision = '';
        url = '';
        label = '';
      }
      saveErr = '';
    }
  });

  function buildBody(): CodeAnchorInput {
    const body: CodeAnchorInput = { kind };
    if (label) body.label = label;
    if (kind === 'file') {
      body.path = path.trim();
      if (lineStart !== '') body.line_start = Number(lineStart);
      if (lineEnd !== '') body.line_end = Number(lineEnd);
      if (revision) body.revision = revision.trim();
    } else if (kind === 'commit') {
      body.revision = revision.trim();
      if (url) body.url = url.trim();
    } else if (kind === 'pr') {
      body.url = url.trim();
    }
    return body;
  }

  async function save() {
    if (saving) return;
    saving = true;
    saveErr = '';
    try {
      const body = buildBody();
      if (anchor) {
        await api.updateCodeAnchor(anchor.id, body);
      } else {
        await api.createCodeAnchor(ownerType, ownerId, body);
      }
      onSaved();
      onClose();
    } catch (e) {
      saveErr = String(e);
    } finally {
      saving = false;
    }
  }

  function canSave(): boolean {
    if (saving) return false;
    if (kind === 'file') return path.trim() !== '';
    if (kind === 'commit') return /^[0-9a-fA-F]{7,40}$/.test(revision.trim());
    if (kind === 'pr') return url.trim() !== '';
    return false;
  }
</script>

<Modal {open} title={anchor ? 'Edit code link' : 'Add code link'} onClose={onClose}>
  <section class="form">
    <div class="field">
      <span>Kind</span>
      <div class="seg" role="radiogroup" aria-label="Anchor kind">
        <button type="button" class:on={kind === 'file'} onclick={() => (kind = 'file')} disabled={!!anchor}>File</button>
        <button type="button" class:on={kind === 'commit'} onclick={() => (kind = 'commit')} disabled={!!anchor}>Commit</button>
        <button type="button" class:on={kind === 'pr'} onclick={() => (kind = 'pr')} disabled={!!anchor}>PR</button>
      </div>
      {#if anchor}
        <span class="hint">Kind is fixed after creation — delete and recreate to change it.</span>
      {/if}
    </div>

    {#if kind === 'file'}
      <label class="field">
        <span>Path</span>
        <input type="text" bind:value={path} placeholder="internal/api/scratchpads.go" />
      </label>
      <div class="row2">
        <label class="field">
          <span>Line start</span>
          <input type="number" bind:value={lineStart} min="0" />
        </label>
        <label class="field">
          <span>Line end</span>
          <input type="number" bind:value={lineEnd} min="0" />
        </label>
      </div>
      <label class="field">
        <span>Revision (optional SHA)</span>
        <input type="text" bind:value={revision} placeholder="a1b2c3d" />
      </label>
    {:else if kind === 'commit'}
      <label class="field">
        <span>SHA</span>
        <input type="text" bind:value={revision} placeholder="a1b2c3d (7–40 hex chars)" />
      </label>
      <label class="field">
        <span>URL (optional)</span>
        <input type="text" bind:value={url} placeholder="https://github.com/owner/repo/commit/..." />
      </label>
    {:else if kind === 'pr'}
      <label class="field">
        <span>URL</span>
        <input type="text" bind:value={url} placeholder="https://github.com/owner/repo/pull/123" />
      </label>
    {/if}

    <label class="field">
      <span>Label (optional)</span>
      <input type="text" bind:value={label} placeholder="short human-readable name" />
    </label>

    {#if saveErr}
      <div class="err">{saveErr}</div>
    {/if}
  </section>

  {#snippet footer()}
    <button onclick={onClose} disabled={saving}>Cancel</button>
    <button class="primary" onclick={save} disabled={!canSave()}>
      {saving ? 'Saving…' : 'Save'}
    </button>
  {/snippet}
</Modal>

<style>
  .form { margin-bottom: 8px; }
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
  .field input {
    background: var(--p-0a0a0a);
    color: var(--p-eeeeee);
    border: 1px solid var(--p-333333);
    padding: 6px 8px;
    font-size: 13px;
    font-family: ui-monospace, SFMono-Regular, Consolas, monospace;
  }
  .row2 { display: grid; grid-template-columns: 1fr 1fr; gap: 12px; }
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
  .seg button:hover:not(:disabled) { color: var(--p-dddddd); background: var(--p-1a1a1a); }
  .seg button.on { background: var(--p-1e3a52); color: var(--p-99ccff); }
  .seg button:disabled { opacity: 0.5; cursor: not-allowed; }
  .err { color: var(--p-ff8888); font-size: 12px; margin-top: 6px; }
  .primary {
    background: var(--p-1e3a52);
    color: var(--p-99ccff);
    border-color: var(--p-2d5578);
  }
  .primary:hover:not(:disabled) { background: var(--p-2d5578); }
</style>
