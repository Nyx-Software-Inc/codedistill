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
  // Renders the front of the dialog queue (lib/dialog.ts) — the app-themed
  // stand-in for the native alert/confirm/prompt popups. Mounted once in App.
  import Modal from './Modal.svelte';
  import { dialogQueue, settleDialog, flushDialogs, type DialogRequest } from '../lib/dialog';

  const current = $derived($dialogQueue[0] ?? null);
  let inputValue = $state('');
  let lastReq = $state<DialogRequest | null>(null);
  $effect(() => {
    if (current !== lastReq) {
      lastReq = current;
      inputValue = current?.kind === 'prompt' ? current.initial : '';
    }
  });

  function cancelValue(req: DialogRequest): unknown {
    return req.kind === 'confirm' ? false : req.kind === 'prompt' ? null : undefined;
  }
  function okValue(req: DialogRequest): unknown {
    return req.kind === 'confirm' ? true : req.kind === 'prompt' ? inputValue : undefined;
  }
  function onKey(e: KeyboardEvent) {
    if (!current) return;
    // Escape is the hard escape hatch: clear the ENTIRE queue, not just the
    // front, so a re-firing loop can't out-queue the user. Handled here (not
    // just via Modal's onClose) so one press always empties the board.
    if (e.key === 'Escape') {
      e.preventDefault();
      flushDialogs();
      return;
    }
    // Enter confirms (the prompt input also submits via its own handler).
    if (e.key === 'Enter' && current.kind !== 'prompt') {
      e.preventDefault();
      settleDialog(current, okValue(current));
    }
  }
</script>

<svelte:window onkeydown={onKey} />

{#if current}
  {#key current.id}
  <Modal open={true} z={2000} title={current.title} onClose={() => settleDialog(current, cancelValue(current))} width="440px">
    <p class="msg">{current.message}</p>
    {#if current.kind === 'prompt'}
      <!-- svelte-ignore a11y_autofocus -->
      <input
        class="prompt-input"
        bind:value={inputValue}
        placeholder={current.placeholder}
        autofocus
        onkeydown={(e) => {
          if (e.key === 'Enter') {
            e.preventDefault();
            settleDialog(current, inputValue);
          }
        }}
      />
    {/if}
    <div class="actions">
      {#if current.kind !== 'alert'}
        <button class="cancel" onclick={() => settleDialog(current, cancelValue(current))}>{current.cancelLabel}</button>
      {/if}
      <button class:danger={current.danger} class="ok" onclick={() => settleDialog(current, okValue(current))}>
        {current.confirmLabel}
      </button>
    </div>
  </Modal>
  {/key}
{/if}

<style>
  .msg {
    margin: 0 0 14px 0;
    font-size: 13px;
    color: var(--p-dddddd);
    line-height: 1.5;
    white-space: pre-wrap;
    word-break: break-word;
  }
  .prompt-input {
    width: 100%;
    background: var(--p-111111);
    border: 1px solid var(--p-333333);
    color: var(--p-eeeeee);
    border-radius: 4px;
    padding: 6px 9px;
    font-size: 13px;
    margin-bottom: 14px;
  }
  .prompt-input:focus { outline: none; border-color: var(--p-2d5578); }
  .actions {
    display: flex;
    gap: 8px;
    justify-content: flex-end;
  }
  .cancel { background: transparent; }
  .danger {
    background: var(--p-5a1515);
    color: var(--p-ffdddd);
    border-color: var(--p-732525);
  }
  .danger:hover { background: var(--p-732525); }
</style>
