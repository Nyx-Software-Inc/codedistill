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
  import * as api from '../lib/api';

  // Renders + edits a single item's custom-field values (backlog item #1).
  // owner is the type ('bug_item' etc.); id is the item id. Saves on blur/change.
  let { owner, id }: { owner: api.CustomFieldOwner; id: string } = $props();

  let fields = $state<api.CustomFieldView[]>([]);
  let loaded = $state(false);
  let loadFailed = $state(false);
  let savingId = $state('');
  let errId = $state('');

  const prefix = $derived(api.customFieldPrefix[owner]);

  async function load() {
    try {
      fields = await api.listItemCustomFields(prefix, id);
      loadFailed = false;
    } catch {
      // A load failure must not masquerade as "no custom fields" (audit M26).
      fields = [];
      loadFailed = true;
    }
    loaded = true;
  }
  $effect(() => {
    if (id) load();
  });

  async function save(f: api.CustomFieldView, value: string) {
    if (value === f.value) return;
    savingId = f.id;
    errId = '';
    try {
      await api.setItemCustomField(prefix, id, f.id, value);
      f.value = value;
    } catch {
      errId = f.id;
    } finally {
      savingId = '';
    }
  }
</script>

{#if loaded && fields.length > 0}
  <div class="cf">
    <span class="cf-head">Custom fields</span>
    {#each fields as f (f.id)}
      <label class="cf-row" class:err={errId === f.id}>
        <span class="cf-name">{f.name}</span>
        {#if f.field_type === 'select'}
          <select
            class="cf-input"
            value={f.value}
            onchange={(e) => save(f, (e.target as HTMLSelectElement).value)}
            disabled={savingId === f.id}
          >
            <option value="">—</option>
            {#each f.options as o (o)}<option value={o}>{o}</option>{/each}
          </select>
        {:else if f.field_type === 'number'}
          <input
            class="cf-input"
            type="number"
            value={f.value}
            onblur={(e) => save(f, (e.target as HTMLInputElement).value.trim())}
            disabled={savingId === f.id}
          />
        {:else if f.field_type === 'date'}
          <input
            class="cf-input"
            type="date"
            value={f.value}
            onchange={(e) => save(f, (e.target as HTMLInputElement).value)}
            disabled={savingId === f.id}
          />
        {:else}
          <input
            class="cf-input"
            type="text"
            value={f.value}
            onblur={(e) => save(f, (e.target as HTMLInputElement).value.trim())}
            disabled={savingId === f.id}
          />
        {/if}
      </label>
    {/each}
  </div>
{:else if loadFailed}
  <div class="cf"><span class="cf-err">Couldn't load custom fields — try reopening.</span></div>
{/if}

<style>
  .cf-err {
    font-size: 11px;
    color: var(--p-ff8888);
  }
  .cf {
    display: flex;
    flex-direction: column;
    gap: 6px;
    margin-top: 4px;
  }
  .cf-head {
    font-size: 11px;
    text-transform: uppercase;
    letter-spacing: 0.04em;
    color: var(--p-777777);
  }
  .cf-row {
    display: flex;
    align-items: center;
    gap: 8px;
  }
  .cf-name {
    flex: 0 0 38%;
    font-size: 12px;
    color: var(--p-cccccc);
  }
  .cf-input {
    flex: 1;
    min-width: 0;
    background: var(--p-1e1e1e);
    border: 1px solid var(--p-3a3a3a);
    border-radius: 4px;
    color: var(--p-e0e0e0);
    padding: 4px 6px;
    font-size: 12px;
  }
  .cf-row.err .cf-input {
    border-color: var(--p-e07a7a);
  }
</style>
