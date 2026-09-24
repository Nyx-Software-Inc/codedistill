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

  type Section = 'appearance' | 'indexing' | 'input' | 'canvas' | 'models' | 'workflows' | 'mcp' | 'server';

  type Props = { userId: string };
  let { userId }: Props = $props();

  let active = $state<Section>('appearance');
</script>

<div class="settings-shell">
  <nav class="settings-nav" aria-label="Settings sections">
    <button
      type="button"
      class="nav-item"
      class:active={active === 'appearance'}
      onclick={() => (active = 'appearance')}
    >
      Appearance
    </button>
    <button
      type="button"
      class="nav-item"
      class:active={active === 'indexing'}
      onclick={() => (active = 'indexing')}
    >
      Indexing
    </button>
    <button
      type="button"
      class="nav-item"
      class:active={active === 'input'}
      onclick={() => (active = 'input')}
    >
      Input
    </button>
    <button
      type="button"
      class="nav-item"
      class:active={active === 'canvas'}
      onclick={() => (active = 'canvas')}
    >
      Canvas
    </button>
    <button
      type="button"
      class="nav-item"
      class:active={active === 'models'}
      onclick={() => (active = 'models')}
    >
      Models
    </button>
    <button
      type="button"
      class="nav-item"
      class:active={active === 'workflows'}
      onclick={() => (active = 'workflows')}
    >
      Workflows
    </button>
    <button
      type="button"
      class="nav-item"
      class:active={active === 'mcp'}
      onclick={() => (active = 'mcp')}
    >
      MCP Export
    </button>
    <button
      type="button"
      class="nav-item"
      class:active={active === 'server'}
      onclick={() => (active = 'server')}
    >
      Server access
    </button>
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
