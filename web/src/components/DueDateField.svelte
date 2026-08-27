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
  // One due-date control for every item type that carries a due date
  // (todo/bug/use case). Native <input type="date"> — the browser's calendar
  // picker: accessible, keyboard-friendly, zero dependencies — plus an
  // explicit clear button, since clearing a native date input is otherwise
  // an awkward select-and-delete.
  type Props = {
    // RFC3339 or ISO date string from the item, undefined/'' = none.
    value: string | undefined;
    onChange: (v: string | undefined) => void;
    label?: string;
  };
  let { value, onChange, label = 'Due date' }: Props = $props();
</script>

<label class="field due-field">
  <span>{label}</span>
  <div class="due-row">
    <input
      type="date"
      value={value ? value.slice(0, 10) : ''}
      onchange={(e) => onChange((e.currentTarget as HTMLInputElement).value || undefined)}
    />
    {#if value}
      <button type="button" class="due-clear" title="Clear due date" onclick={() => onChange(undefined)}>×</button>
    {/if}
  </div>
</label>

<style>
  .due-field { display: flex; flex-direction: column; gap: 4px; }
  .due-field > span {
    font-size: 10px;
    color: var(--p-888888);
    text-transform: uppercase;
    letter-spacing: 0.5px;
  }
  .due-row { display: flex; align-items: center; gap: 6px; }
  .due-row input[type='date'] {
    background: var(--p-0a0a0a);
    color: var(--p-eeeeee);
    border: 1px solid var(--p-333333);
    padding: 6px 8px;
    font-size: 13px;
    font-family: inherit;
    color-scheme: dark;
    flex: 1;
  }
  :global([data-theme='light']) .due-row input[type='date'] { color-scheme: light; }
  .due-clear {
    background: transparent;
    border: none;
    color: var(--p-666666);
    cursor: pointer;
    font-size: 15px;
    line-height: 1;
    padding: 0 4px;
  }
  .due-clear:hover { color: var(--p-ff8888); }
</style>
