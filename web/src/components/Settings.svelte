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
  import AppearanceSettings from './AppearanceSettings.svelte';
  import IndexingSettings from './IndexingSettings.svelte';
  import InputSettings from './InputSettings.svelte';
  import CanvasSettings from './CanvasSettings.svelte';
  import McpExportSettings from './McpExportSettings.svelte';
  import ModelProviders from './ModelProviders.svelte';
  import WorkflowSettings from './WorkflowSettings.svelte';
  import ServerAccessSettings from './ServerAccessSettings.svelte';
  import SecurityKnowledgeSettings from './SecurityKnowledgeSettings.svelte';
  import AgentSettings from './AgentSettings.svelte';

  import { can } from '../lib/session.svelte';

  type Section = 'appearance' | 'indexing' | 'input' | 'canvas' | 'models' | 'workflows' | 'agents' | 'security' | 'mcp' | 'server';

  type Props = { userId: string; /** The section to open on. */ initial?: Section };
  let { userId, initial = 'appearance' }: Props = $props();

  // Each section and who may open it. Personal ones are everyone's; the rest
  // change the installation or its workflows, and show only to those whose
  // role allows it (the server refuses the change regardless).
  const SECTIONS: { id: Section; label: string; allowed: () => boolean }[] = [
    { id: 'appearance', label: 'Appearance', allowed: () => true },
    { id: 'input', label: 'Input', allowed: () => true },
    { id: 'canvas', label: 'Canvas', allowed: () => true },
    { id: 'indexing', label: 'Indexing', allowed: () => can('administer') },
    { id: 'models', label: 'Models', allowed: () => can('administer') },
    { id: 'workflows', label: 'Workflows', allowed: () => can('design_workflows') },
    { id: 'agents', label: 'Agents', allowed: () => can('administer') || can('design_workflows') },
    { id: 'security', label: 'Security knowledge', allowed: () => can('administer') },
    { id: 'mcp', label: 'MCP Export', allowed: () => can('administer') },
    { id: 'server', label: 'Server access', allowed: () => can('administer') },
  ];
  const shown = $derived(SECTIONS.filter((x) => x.allowed()));

  let active = $state<Section>('appearance');
  $effect(() => {
    active = shown.some((x) => x.id === initial) ? initial : (shown[0]?.id ?? 'appearance');
  });
</script>

<div class="settings-shell">
  <nav class="settings-nav" aria-label="Settings sections">
    {#each shown as x (x.id)}
      <button type="button" class="nav-item" class:active={active === x.id} onclick={() => (active = x.id)}>
        {x.label}
      </button>
    {/each}
  </nav>
  <div class="settings-content">
    {#if active === 'appearance'}
      <AppearanceSettings />
    {:else if active === 'indexing'}
      <IndexingSettings />
    {:else if active === 'input'}
      <InputSettings />
    {:else if active === 'canvas'}
      <CanvasSettings />
    {:else if active === 'models'}
      <ModelProviders />
    {:else if active === 'workflows'}
      <WorkflowSettings />
    {:else if active === 'agents'}
      <AgentSettings {userId} />
    {:else if active === 'security'}
      <SecurityKnowledgeSettings {userId} />
    {:else if active === 'mcp'}
      <McpExportSettings {userId} />
    {:else if active === 'server'}
      <ServerAccessSettings />
    {/if}
  </div>
</div>

<style>
  .settings-shell {
    display: grid;
    grid-template-columns: 160px 1fr;
    gap: 16px;
    min-height: 420px;
  }
  .settings-nav {
    display: flex;
    flex-direction: column;
    gap: 2px;
    border-right: 1px solid rgba(0, 0, 0, 0.08);
    padding-right: 8px;
  }
  .nav-item {
    padding: 8px 12px;
    border: 0;
    background: transparent;
    cursor: pointer;
    text-align: left;
    font-size: 13px;
    border-radius: 6px;
    color: inherit;
  }
  .nav-item:hover { background: rgba(0, 0, 0, 0.04); }
  .nav-item.active {
    background: rgba(0, 0, 0, 0.08);
    font-weight: 600;
  }
  .settings-content { min-width: 0; }
</style>
