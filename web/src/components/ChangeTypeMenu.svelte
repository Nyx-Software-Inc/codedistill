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
  // Explicit "Change type" action on a work-item modal (item-editing redesign,
  // slice 4). Reclassification is a deliberate, consequence-aware re-derive —
  // NOT the silent dropdown that slice 2 removed from the Source tab. It sets the
  // source item's classification_override, which re-derives the work item under
  // the new type; the backend carries the activity log over and drops
  // type-specific fields. See docs/design/item-editing-redesign.md §7.
  import * as api from '../lib/api';

  type Kind = 'todo' | 'bug' | 'use_case' | 'kb';
  type Props = { sourceItemId?: string; currentType: Kind; onDone: () => void };
  let { sourceItemId, currentType, onDone }: Props = $props();

  let open = $state(false);
  let pending = $state<Kind | null>(null);
  let busy = $state(false);
  let err = $state('');

  const LABEL: Record<Kind, string> = { todo: 'Todo', bug: 'Bug', use_case: 'Use Case', kb: 'KB' };
  const others = (): Kind[] => (['todo', 'bug', 'use_case', 'kb'] as Kind[]).filter((k) => k !== currentType);

  function dropped(cur: Kind): string {
    if (cur === 'use_case') return 'its role / want / why and acceptance criteria';
    if (cur === 'bug') return 'its severity and repro details';
    if (cur === 'kb') return 'its KB-specific fields';
    return 'its todo-specific fields';
  }

  async function confirm() {
    if (!sourceItemId || !pending || busy) return;
    busy = true;
    err = '';
    try {
      await api.updateItem(sourceItemId, { classification_override: pending });
      open = false;
      pending = null;
      onDone();
    } catch (e) {
      err = String(e);
    } finally {
      busy = false;
    }
  }
</script>

{#if !sourceItemId}
  <button class="ct-btn" disabled title="Manually created — no source item to reclassify from.">Change type…</button>
{:else}
  <div class="ct">
    <button class="ct-btn" onclick={() => { open = !open; pending = null; err = ''; }} aria-expanded={open}>
      Change type…
    </button>
    {#if open}
      <div class="ct-pop">
        {#if !pending}
          <div class="ct-head">Reclassify as…</div>
          {#each others() as k (k)}
            <button class="ct-item" onclick={() => (pending = k)}>{LABEL[k]}</button>
          {/each}
        {:else}
          <div class="ct-warn">
            <p>Change this <strong>{LABEL[currentType]}</strong> to a <strong>{LABEL[pending]}</strong>?</p>
            <ul>
              <li>Its notes/log, tags, code links, and due date <em>carry over</em>.</li>
              <li>{dropped(currentType)} <em>will not</em> — they don't apply to a {LABEL[pending]}.</li>
            </ul>
            {#if err}<div class="ct-err">{err}</div>{/if}
            <div class="ct-actions">
              <button class="ct-back" onclick={() => (pending = null)} disabled={busy}>Back</button>
              <button class="ct-confirm" onclick={confirm} disabled={busy}>
                {busy ? 'Changing…' : `Change to ${LABEL[pending]}`}
              </button>
            </div>
          </div>
        {/if}
      </div>
    {/if}
  </div>
{/if}

<style>
  .ct { position: relative; display: inline-block; }
  .ct-btn {
    background: transparent; border: 1px solid var(--p-333333); color: var(--p-aaaaaa);
    font-size: 12px; padding: 4px 10px; border-radius: 4px; cursor: pointer;
  }
  .ct-btn:hover:not(:disabled) { color: var(--p-e7b86c); border-color: var(--p-5a3e1a); }
  .ct-btn:disabled { opacity: 0.5; cursor: default; }
  .ct-pop {
    position: absolute; bottom: calc(100% + 6px); left: 0; z-index: 40;
    min-width: 240px; background: var(--p-161616); border: 1px solid var(--p-333333);
    border-radius: 6px; box-shadow: 0 8px 24px rgba(0, 0, 0, 0.55); padding: 6px;
  }
  .ct-head {
    font-size: 10px; text-transform: uppercase; letter-spacing: 0.5px;
    color: var(--p-888888); padding: 4px 8px;
  }
  .ct-item {
    display: block; width: 100%; text-align: left; background: transparent; border: none;
    color: var(--p-dddddd); font-size: 13px; padding: 6px 8px; border-radius: 3px; cursor: pointer;
  }
  .ct-item:hover { background: var(--p-252525); color: var(--p-ffffff); }
  .ct-warn { padding: 8px; font-size: 12px; color: var(--p-cccccc); line-height: 1.5; }
  .ct-warn p { margin: 0 0 6px; }
  .ct-warn ul { margin: 0 0 8px; padding-left: 18px; }
  .ct-warn em { color: var(--p-e7b86c); font-style: normal; }
  .ct-warn strong { color: var(--p-ffffff); }
  .ct-err { color: var(--p-ff8888); font-size: 12px; margin-bottom: 6px; }
  .ct-actions { display: flex; justify-content: flex-end; gap: 8px; }
  .ct-back {
    background: transparent; border: 1px solid var(--p-333333); color: var(--p-999999);
    font-size: 12px; padding: 4px 10px; border-radius: 4px; cursor: pointer;
  }
  .ct-confirm {
    background: var(--p-5a3e1a); border: 1px solid var(--p-6a5424); color: var(--p-ffe1a3);
    font-size: 12px; padding: 4px 12px; border-radius: 4px; cursor: pointer;
  }
  .ct-confirm:hover:not(:disabled) { background: var(--p-6a5424); color: var(--p-ffffff); }
  .ct-confirm:disabled { opacity: 0.6; cursor: default; }
</style>
