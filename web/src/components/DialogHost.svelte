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
  import { get } from 'svelte/store';
  import Modal from './Modal.svelte';
  import { dialogQueue, settleDialog, flushDialogs, type DialogRequest } from '../lib/dialog';

  const current = $derived($dialogQueue[0] ?? null);
  let inputValue = $state('');
  // Tracked by id, in a plain (non-reactive) let, and both halves of that are
  // load-bearing. Holding the request itself in $state PROXIES it, so the old
  // `current !== lastReq` compared a raw object against its own proxy — never
  // equal, always true — and the effect rewrote its own dependency on every
  // pass until Svelte's depth guard fired and took the app's reactivity down
  // with it (svelte.dev/e/state_proxy_equality_mismatch). An id is a primitive,
  // so proxying cannot corrupt the comparison, and an untracked write cannot
  // re-invalidate the effect. Do not turn this back into $state.
  let lastReqId = 0;
  $effect(() => {
    const req = current;
    if (req?.id !== lastReqId) {
      lastReqId = req?.id ?? 0;
      inputValue = req?.kind === 'prompt' ? req.initial : '';
    }
  });

  function cancelValue(req: DialogRequest | null): unknown {
    return req?.kind === 'confirm' ? false : req?.kind === 'prompt' ? null : undefined;
  }
  function okValue(req: DialogRequest | null): unknown {
    return req?.kind === 'confirm' ? true : req?.kind === 'prompt' ? inputValue : undefined;
  }
  // The queue read through the store rather than through `current`. If the app's
  // reactivity has stalled — a runaway $effect trips Svelte's update-depth guard
  // and the whole graph stops — `current` reads null while a dialog is still on
  // screen. Keying the escape hatch off the derived meant Escape silently
  // returned exactly when it was the only way out.
  function front(): DialogRequest | null {
    return current ?? get(dialogQueue)[0] ?? null;
  }
  function onKey(e: KeyboardEvent) {
    // Escape is the hard escape hatch: clear the ENTIRE queue, not just the
    // front, so a re-firing loop can't out-queue the user. Handled here (not
    // just via Modal's onClose) so one press always empties the board. Only
    // swallow the key when a dialog is actually up, so Escape still reaches
    // panels and modals underneath.
    if (e.key === 'Escape') {
      if (get(dialogQueue).length === 0) return;
      e.preventDefault();
      flushDialogs();
      return;
    }
    // Enter confirms (the prompt input also submits via its own handler).
    const req = front();
    if (!req) return;
    if (e.key === 'Enter' && req.kind !== 'prompt') {
      e.preventDefault();
      settleDialog(req, okValue(req));
    }
  }
</script>

<svelte:window onkeydown={onKey} />

{#if current}
  <!-- Bind the request ONCE per render. Every exit below settles `req`, the
       request these buttons were drawn for, instead of re-reading `current` at
       click time — a derived that reads null the moment reactivity stalls, which
       made each handler throw before it reached settleDialog and left the dialog
       on screen with no working way out. -->
  {@const req = current}
  {#key req.id}
  <Modal open={true} z={2000} title={req.title} onClose={() => settleDialog(req, cancelValue(req))} width="440px">
    <p class="msg">{req.message}</p>
    {#if req.kind === 'prompt'}
      <!-- svelte-ignore a11y_autofocus -->
      <input
        class="prompt-input"
        bind:value={inputValue}
        placeholder={req.placeholder}
        autofocus
        onkeydown={(e) => {
          if (e.key === 'Enter') {
            e.preventDefault();
            settleDialog(req, inputValue);
          }
        }}
      />
    {/if}
    <div class="actions">
      {#if req.kind !== 'alert'}
        <button class="cancel" onclick={() => settleDialog(req, cancelValue(req))}>{req.cancelLabel}</button>
      {/if}
      <button class:danger={req.danger} class="ok" onclick={() => settleDialog(req, okValue(req))}>
        {req.confirmLabel}
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
