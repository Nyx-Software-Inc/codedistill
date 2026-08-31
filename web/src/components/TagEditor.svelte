<!--
  =============================================================================
   Copyright (c) 2026 Nyx Software, Inc.  All rights reserved.

   CodeDistill

   Property of Nyx Software, Inc., provided under a dual license: the GNU Affero General
   Public License v3.0 (see the LICENSE file) and, separately, a commercial
   license available from Nyx Software, Inc. Use outside the terms of one of those
   licenses is prohibited.

   SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-Nyx-Commercial
  =============================================================================
-->
<!--
  Reusable tag chip editor for the work record (canvas rework C3). Tags live
  on the derived item (todo/bug/use case/KB), not the immutable source — so
  this is where you add and remove them. The parent owns the array and the
  persistence: we mutate `tags` in place and call `onChange(tags)` on every
  add/remove, which the parent turns into an api.updateX({ tags }) call.

  Kept deliberately dumb (no fetching of its own) so it drops into any of the
  four detail views with a one-line binding.
-->
<script lang="ts">
  type Props = {
    tags: string[];
    onChange: (tags: string[]) => void;
    disabled?: boolean;
  };
  let { tags = $bindable(), onChange, disabled = false }: Props = $props();

  let tagInput = $state('');

  function addTag() {
    if (disabled) return;
    const t = tagInput.trim().toLowerCase().replace(/^#/, '');
    tagInput = '';
    if (!t || tags.includes(t)) return;
    tags = [...tags, t];
    onChange(tags);
  }
  function removeTag(t: string) {
    if (disabled) return;
    tags = tags.filter((x) => x !== t);
    onChange(tags);
  }
  function tagKey(e: KeyboardEvent) {
    if (e.key === 'Enter' || e.key === ',') {
      e.preventDefault();
      addTag();
    } else if (e.key === 'Backspace' && tagInput === '' && tags.length) {
      e.preventDefault();
      removeTag(tags[tags.length - 1]);
    }
  }
</script>

<div class="chips" class:disabled>
  {#each tags as t (t)}
    <span class="chip"
      >#{t}<button type="button" class="chip-x" onclick={() => removeTag(t)} aria-label="Remove tag {t}" {disabled}
        >×</button
      ></span
    >
  {/each}
  {#if !disabled}
    <input
      class="chip-input"
      type="text"
      bind:value={tagInput}
      onkeydown={tagKey}
      onblur={addTag}
      placeholder={tags.length === 0 ? 'Add tags — Enter or comma' : ''}
    />
  {/if}
</div>

<style>
  .chips {
    display: flex; flex-wrap: wrap; gap: 4px; align-items: center;
    background: var(--p-0a0a0a); border: 1px solid var(--p-333333);
    padding: 4px 6px; min-height: 30px;
  }
  .chips.disabled { opacity: 0.7; }
  .chip {
    display: inline-flex; align-items: center; gap: 2px;
    background: var(--p-1e3a52); color: var(--p-99ccff);
    font-size: 11px; padding: 1px 4px 1px 6px; border-radius: 3px;
    font-family: ui-monospace, monospace;
  }
  .chip-x {
    border: none; background: transparent; color: var(--p-99ccff);
    font-size: 12px; padding: 0 2px; line-height: 1; cursor: pointer;
  }
  .chip-x:hover { color: var(--p-ffffff); }
  .chip-x:disabled { cursor: default; }
  .chip-input {
    flex: 1; min-width: 120px; background: transparent; color: var(--p-eeeeee);
    border: none; font-size: 12px; padding: 2px 4px; outline: none;
  }
</style>
