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
  import { confirmDialog } from '../lib/dialog';
  import * as api from '../lib/api';

  // Manage a project's custom-field definitions (backlog item #1). Embedded in
  // ProjectSettings. Add + delete; values are edited per-item in the detail modals.
  let { projectId }: { projectId: string } = $props();

  const OWNERS: { type: api.CustomFieldOwner; label: string }[] = [
    { type: 'todo_item', label: 'Todos' },
    { type: 'bug_item', label: 'Bugs' },
    { type: 'use_case_item', label: 'Use cases' },
    { type: 'knowledge_entry', label: 'KB' },
  ];

  let defs = $state<api.CustomFieldDef[]>([]);
  let loaded = $state(false);
  let loadFailed = $state(false);
  let busy = $state(false);
  let err = $state('');

  // New-field draft.
  let name = $state('');
  let fieldType = $state<api.CustomFieldType>('text');
  let optionsText = $state('');
  let applies = $state<Record<api.CustomFieldOwner, boolean>>({
    todo_item: false, bug_item: false, use_case_item: false, knowledge_entry: false,
  });

  async function load() {
    try {
      defs = await api.listCustomFieldDefs(projectId);
      loadFailed = false;
    } catch {
      // Don't render a load failure as "No custom fields yet" (audit M26).
      defs = [];
      loadFailed = true;
    }
    loaded = true;
  }
  $effect(() => {
    if (projectId) load();
  });

  function ownerLabels(d: api.CustomFieldDef): string {
    return d.applies_to
      .map((t) => OWNERS.find((o) => o.type === t)?.label ?? t)
      .join(', ');
  }

  async function add() {
    err = '';
    const appliesTo = OWNERS.filter((o) => applies[o.type]).map((o) => o.type);
    if (!name.trim()) { err = 'Name is required.'; return; }
    if (appliesTo.length === 0) { err = 'Pick at least one item type.'; return; }
    const options = fieldType === 'select'
      ? optionsText.split(/\r?\n/).map((s) => s.trim()).filter(Boolean)
      : [];
    if (fieldType === 'select' && options.length === 0) { err = 'A select field needs at least one option.'; return; }
    busy = true;
    try {
      await api.createCustomFieldDef(projectId, {
        name: name.trim(), field_type: fieldType, options, applies_to: appliesTo,
      });
      name = ''; optionsText = ''; fieldType = 'text';
      applies = { todo_item: false, bug_item: false, use_case_item: false, knowledge_entry: false };
      await load();
    } catch (e) {
      err = e instanceof Error ? e.message : 'Failed to add field.';
    } finally {
      busy = false;
    }
  }

  async function remove(d: api.CustomFieldDef) {
    if (!await confirmDialog(`Delete custom field “${d.name}” and all its values?`, { title: 'Delete custom field', confirmLabel: 'Delete', danger: true })) return;
    busy = true;
    try {
      await api.deleteCustomFieldDef(d.id);
      await load();
    } catch (e) {
      err = e instanceof Error ? e.message : 'Failed to delete.';
    } finally {
      busy = false;
    }
  }
</script>

<div class="cfa">
  {#if loaded && defs.length > 0}
    <ul class="cfa-list">
      {#each defs as d (d.id)}
        <li class="cfa-item">
          <span class="cfa-name">{d.name}</span>
          <span class="cfa-type">{d.field_type}</span>
          <span class="cfa-owners">{ownerLabels(d)}</span>
          <button class="cfa-del" onclick={() => remove(d)} disabled={busy} title="Delete field">✕</button>
        </li>
      {/each}
    </ul>
  {:else if loadFailed}
    <p class="cfa-empty cfa-err">Couldn't load custom fields — try reopening.</p>
  {:else if loaded}
    <p class="cfa-empty">No custom fields yet.</p>
  {/if}

  <div class="cfa-add">
    <input class="cfa-in" type="text" placeholder="Field name (e.g. Story Points)" bind:value={name} disabled={busy} />
    <select class="cfa-in cfa-type-sel" bind:value={fieldType} disabled={busy}>
      <option value="text">Text</option>
      <option value="number">Number</option>
      <option value="select">Select</option>
      <option value="date">Date</option>
    </select>
    {#if fieldType === 'select'}
      <textarea class="cfa-in" rows="3" placeholder={'One option per line'} bind:value={optionsText} disabled={busy}></textarea>
    {/if}
    <div class="cfa-owners-pick">
      {#each OWNERS as o (o.type)}
        <label class="cfa-chk">
          <input type="checkbox" bind:checked={applies[o.type]} disabled={busy} />
          {o.label}
        </label>
      {/each}
    </div>
    <button class="cfa-add-btn" onclick={add} disabled={busy}>+ Add field</button>
    {#if err}<span class="cfa-err">{err}</span>{/if}
  </div>
</div>

<style>
  .cfa { display: flex; flex-direction: column; gap: 10px; }
  .cfa-list { list-style: none; margin: 0; padding: 0; display: flex; flex-direction: column; gap: 4px; }
  .cfa-item {
    display: flex; align-items: center; gap: 8px;
    background: var(--p-1e1e1e); border: 1px solid var(--p-3a3a3a);
    border-radius: 4px; padding: 5px 8px;
  }
  .cfa-name { flex: 1; font-size: 12px; color: var(--p-e0e0e0); }
  .cfa-type { font-size: 11px; color: var(--p-777777); text-transform: uppercase; }
  .cfa-owners { font-size: 11px; color: var(--p-cccccc); }
  .cfa-del {
    background: none; border: none; color: var(--p-777777); cursor: pointer; font-size: 12px;
  }
  .cfa-del:hover { color: var(--p-e07a7a); }
  .cfa-empty { font-size: 12px; color: var(--p-777777); margin: 0; }
  .cfa-err { color: var(--p-ff8888); }
  .cfa-add { display: flex; flex-direction: column; gap: 6px; }
  .cfa-in {
    background: var(--p-1e1e1e); border: 1px solid var(--p-3a3a3a);
    border-radius: 4px; color: var(--p-e0e0e0); padding: 5px 7px; font-size: 12px;
  }
  .cfa-type-sel { width: 120px; }
  .cfa-owners-pick { display: flex; gap: 12px; flex-wrap: wrap; }
  .cfa-chk { display: flex; align-items: center; gap: 4px; font-size: 12px; color: var(--p-cccccc); }
  .cfa-add-btn {
    align-self: flex-start;
    background: var(--p-2a2a2a); border: 1px solid var(--p-3a3a3a);
    border-radius: 4px; color: var(--p-e0e0e0); padding: 5px 10px; font-size: 12px; cursor: pointer;
  }
  .cfa-add-btn:disabled { opacity: 0.5; }
  .cfa-err { font-size: 12px; color: var(--p-e07a7a); }
</style>
