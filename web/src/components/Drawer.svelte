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
  import type { Project } from '../lib/types';
  import ProjectPane from './ProjectPane.svelte';

  // Drawer collapsed to a single surface (Stage B): Project pane only.
  // The major-tab strip + Workspace / Scratchpad sub-routes are gone —
  // their functionality moved to header pickers (project + scratchpad
  // CRUD), the canvas filter chips (item categorization, Stage C), and
  // the Lists slide-out (derived items, Stage E). Only the resize +
  // hide-toggle behaviour from Stage 1 survives.

  type Props = {
    activeProject: Project | null;
    onProjectImported: (p: Project) => void;
    // Click on a file row → open in the code canvas (App owns the canvas).
    onOpenFile: (path: string) => void;
    // Forwarded to FilesPanel — bumps when the server says the repo moved.
    filesRefreshTick?: number;
  };
  let { activeProject, onProjectImported, onOpenFile, filesRefreshTick = 0 }: Props = $props();
</script>

<div class="drawer">
  <ProjectPane
    {activeProject}
    onImported={onProjectImported}
    {onOpenFile}
    {filesRefreshTick}
  />
</div>

<style>
  .drawer {
    display: flex;
    flex-direction: column;
    height: 100%;
    min-height: 0;
    background: var(--p-0d0d0d);
  }
</style>
