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
  import { alertDialog } from '../lib/dialog';
  import * as api from '../lib/api';
  import type { Project } from '../lib/types';
  import FilesPanel from './FilesPanel.svelte';
  import Icon from './Icon.svelte';
  import ProjectSettings from './ProjectSettings.svelte';

  // The drawer's only surface after Stage B. Project create / rename /
  // delete / select moved to the header Picker, so this pane only owns
  // per-project actions (Export, Import) plus the Files tree.

  type Props = {
    activeProject: Project | null;
    onImported: (p: Project) => void;
    onOpenFile: (path: string) => void;
    filesRefreshTick?: number;
  };
  let { activeProject, onImported, onOpenFile, filesRefreshTick = 0 }: Props = $props();

  let fileInput = $state<HTMLInputElement | undefined>(undefined);
  let importing = $state(false);
  let exporting = $state(false);
  let settingsOpen = $state(false);

  function openImport() {
    fileInput?.click();
  }

  async function onFilePicked(e: Event) {
    const target = e.currentTarget as HTMLInputElement;
    const file = target.files?.[0];
    if (!file) return;
    importing = true;
    try {
      const res = await api.importBundle(file);
      const c = res.counts;
      onImported(res.project);
      void alertDialog(
        `Imported "${res.project.name}":\n` +
        `  ${c.scratchpads} scratchpad${c.scratchpads === 1 ? '' : 's'}, ` +
        `${c.items} item${c.items === 1 ? '' : 's'}\n` +
        `  ${c.todos} todo${c.todos === 1 ? '' : 's'}, ` +
        `${c.bugs} bug${c.bugs === 1 ? '' : 's'}, ` +
        `${c.kb} KB ${c.kb === 1 ? 'entry' : 'entries'}\n` +
        `  ${c.anchors} code anchor${c.anchors === 1 ? '' : 's'}`
      );
    } catch (err) {
      void alertDialog(`Import failed: ${err}`);
    } finally {
      importing = false;
      target.value = '';
    }
  }

  async function exportActive() {
    if (!activeProject || exporting) return;
    exporting = true;
    try {
      await api.exportProject(activeProject.id);
    } catch (e) {
      void alertDialog(String(e));
    } finally {
      exporting = false;
    }
  }
</script>

<div class="pane">
  <div class="head">
    <span class="title">Project</span>
    <button
      class="head-btn"
      onclick={exportActive}
      disabled={!activeProject || exporting}
      title={exporting ? 'Exporting…' : 'Export this project as a .zip bundle'}
    ><Icon name="download" size={13} /> {exporting ? 'Exporting…' : 'Export'}</button>
    <button
      class="head-btn"
      onclick={openImport}
      disabled={importing}
      title="Import a previously-exported .zip bundle as a new project"
    >{importing ? 'Importing…' : '↓ Import'}</button>
    <button
      class="head-btn"
      onclick={() => (settingsOpen = true)}
      disabled={!activeProject}
      title={activeProject ? `Settings for ${activeProject.name} (upload limits, allowed MIME types)` : 'Select a project to edit its settings'}
      aria-label="Project settings"
    >⚙</button>
    <input
      type="file"
      accept=".zip,application/zip"
      bind:this={fileInput}
      onchange={onFilePicked}
      style="display: none;"
      aria-hidden="true"
    />
  </div>

  <ProjectSettings
    open={settingsOpen}
    project={activeProject}
    onClose={() => (settingsOpen = false)}
  />

  <div class="files">
    {#if activeProject}
      <FilesPanel projectId={activeProject.id} onFileClick={onOpenFile} refreshTick={filesRefreshTick} />
    {:else}
      <div class="empty">No project selected.</div>
    {/if}
  </div>
</div>

<style>
  .pane {
    display: flex;
    flex-direction: column;
    height: 100%;
    min-height: 0;
  }
  .head {
    display: flex;
    align-items: center;
    gap: 6px;
    padding: 8px 8px 6px;
    border-bottom: 1px solid var(--p-262626);
    flex-shrink: 0;
  }
  .title {
    flex: 1;
    font-size: 11px;
    color: var(--p-888888);
    text-transform: uppercase;
    letter-spacing: 0.5px;
  }
  .head-btn {
    background: transparent;
    border: 1px solid var(--p-333333);
    color: var(--p-aaaaaa);
    font-size: 11px;
    padding: 3px 8px;
    border-radius: 3px;
    cursor: pointer;
    display: inline-flex;
    align-items: center;
    gap: 4px;
  }
  .head-btn:hover:not(:disabled) { color: var(--p-99ccff); border-color: var(--p-2d5578); }
  .head-btn:disabled { opacity: 0.5; cursor: default; }
  .files {
    flex: 1;
    overflow: auto;
    min-height: 0;
    padding: 4px 6px;
  }
  .empty {
    color: var(--p-666666);
    font-size: 12px;
    font-style: italic;
    text-align: center;
    padding: 16px 8px;
  }
</style>
