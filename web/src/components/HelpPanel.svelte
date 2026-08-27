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
  // UC-50: subject-driven help center — a grouped topic sidebar on the left, the
  // selected topic's content on the right. Content is authored in
  // ../lib/helpTopics.ts and rendered with the in-bundle markdown converter (NOT
  // Carta — its lazy Shiki chunks don't precache in the PWA). Every topic's body
  // is shown to everyone; a non-free topic carries a tier banner so the user
  // knows what tier the feature needs (and whether their plan includes it).
  import Modal from './Modal.svelte';
  import { renderMarkdown } from '../lib/mdToHtml';
  import {
    HELP_GROUPS,
    TIER_LABEL,
    effectiveTier,
    unlocked,
    type HelpTopic,
  } from '../lib/helpTopics';
  import type { LicenseInfo } from '../lib/api';

  type Props = { open: boolean; onClose: () => void; license?: LicenseInfo | null };
  let { open, onClose, license = null }: Props = $props();

  const userTier = $derived(effectiveTier(license));
  let selectedId = $state(HELP_GROUPS[0].topics[0].id);
  const selected = $derived.by<HelpTopic>(() => {
    for (const g of HELP_GROUPS) for (const t of g.topics) if (t.id === selectedId) return t;
    return HELP_GROUPS[0].topics[0];
  });
  const bodyHtml = $derived(renderMarkdown(selected.body));
</script>

<Modal {open} title="Help" {onClose} width="980px">
  <div class="help">
    <nav class="sidebar" aria-label="Help topics">
      {#each HELP_GROUPS as g (g.id)}
        <div class="grp">{g.title}</div>
        {#each g.topics as t (t.id)}
          <button
            class="topic"
            class:on={t.id === selectedId}
            type="button"
            onclick={() => (selectedId = t.id)}
          >
            <span class="t-title">{t.title}</span>
            {#if t.tier !== 'free'}<span class="tier tier-{t.tier}">{TIER_LABEL[t.tier]}</span>{/if}
          </button>
        {/each}
      {/each}
    </nav>

    <div class="content">
      {#if selected.tier !== 'free'}
        <div class="tier-banner" class:have={unlocked(selected, userTier)}>
          {#if unlocked(selected, userTier)}
            <strong>{TIER_LABEL[selected.tier]} feature</strong> — included in your plan.
          {:else}
            <strong>{TIER_LABEL[selected.tier]} feature</strong> — requires a
            {TIER_LABEL[selected.tier]} license to use.
          {/if}
        </div>
      {/if}
      <!-- Trusted, bundled content (our own help) — safe to render as HTML. -->
      <div class="body">{@html bodyHtml}</div>
    </div>
  </div>
</Modal>

<style>
  .help {
    display: grid;
    grid-template-columns: 236px 1fr;
    height: 74vh;
    min-height: 0;
  }
  .sidebar {
    overflow-y: auto;
    border-right: 1px solid var(--p-2a2a2a);
    padding-right: 8px;
    display: flex;
    flex-direction: column;
    gap: 1px;
  }
  .grp {
    font-size: 10px;
    text-transform: uppercase;
    letter-spacing: 0.6px;
    color: var(--p-777777);
    padding: 12px 8px 4px;
  }
  .grp:first-child { padding-top: 2px; }
  .topic {
    display: flex;
    align-items: center;
    gap: 6px;
    background: transparent;
    border: none;
    color: var(--p-cccccc);
    text-align: left;
    padding: 6px 8px;
    font-size: 12.5px;
    border-radius: 5px;
    cursor: pointer;
  }
  .topic:hover { background: var(--p-1e1e1e); }
  .topic.on { background: var(--p-1a2530); color: var(--p-cfe6ff); }
  .t-title { flex: 1; }
  .tier {
    font-size: 9px;
    text-transform: uppercase;
    letter-spacing: 0.3px;
    padding: 1px 5px;
    border-radius: 8px;
    border: 1px solid transparent;
    flex-shrink: 0;
  }
  .tier-pro { color: var(--p-d4a54d); border-color: var(--p-6a5424); background: var(--p-2a2314); }
  .tier-enterprise { color: var(--p-99ccff); border-color: var(--p-2d5578); background: var(--p-16222e); }

  .content {
    overflow-y: auto;
    padding: 2px 6px 8px 18px;
    min-width: 0;
  }
  .tier-banner {
    font-size: 12px;
    color: var(--p-e0c384);
    background: var(--p-241d0f);
    border: 1px solid var(--p-6a5424);
    border-radius: 6px;
    padding: 7px 11px;
    margin: 0 0 14px;
  }
  .tier-banner strong { color: var(--p-ffe1a3); }
  .tier-banner.have {
    color: var(--p-a9d5a9);
    background: var(--p-13210f);
    border-color: var(--p-2c5a2c);
  }
  .tier-banner.have strong { color: var(--p-cdeccd); }

  .body { font-size: 13px; line-height: 1.6; color: var(--p-dddddd); }
  .body :global(h1) { font-size: 20px; color: var(--p-f0f0f0); margin: 0 0 12px; }
  .body :global(h2) {
    font-size: 15px; color: var(--p-e8e8e8); margin: 20px 0 8px;
    border-bottom: 1px solid var(--p-2a2a2a); padding-bottom: 4px;
  }
  .body :global(h3) { font-size: 13.5px; color: var(--p-cccccc); margin: 14px 0 6px; }
  .body :global(p) { margin: 8px 0; }
  .body :global(a) { color: var(--p-99ccff); }
  .body :global(strong) { color: var(--p-f0f0f0); }
  .body :global(code) {
    background: var(--p-111111); border: 1px solid var(--p-2a2a2a);
    border-radius: 3px; padding: 1px 4px; font-size: 12px;
    font-family: ui-monospace, monospace; color: var(--p-cceeff);
  }
  .body :global(ul), .body :global(ol) { margin: 8px 0; padding-left: 22px; }
  .body :global(li) { margin: 4px 0; }
  .body :global(table) { border-collapse: collapse; margin: 12px 0; width: 100%; font-size: 12px; }
  .body :global(th), .body :global(td) {
    border: 1px solid var(--p-2a2a2a); padding: 6px 9px; text-align: left; vertical-align: top;
  }
  .body :global(th) { background: var(--p-161616); color: var(--p-e0e0e0); }

  @media (max-width: 720px) {
    .help { grid-template-columns: 1fr; height: 78vh; }
    .sidebar { border-right: none; border-bottom: 1px solid var(--p-2a2a2a); max-height: 170px; }
  }
</style>
