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
  // Inline SVG icon set. Used wherever a Unicode glyph would render
  // inconsistently across platforms (most notably ✏ U+270F, which macOS
  // forces into emoji presentation and renders blank in some font stacks).
  // Stroke uses currentColor so the icon inherits the surrounding text color.

  type IconName =
    | 'pencil' | 'edit' | 'copy' | 'download'
    | 'workspace' | 'project' | 'scratchpad'
    | 'anchor' | 'sparkle' | 'link'
    | 'move'
    // Header nav + canvas-toolbar icons (signature-hue set).
    | 'menu' | 'dashboard' | 'check' | 'jobs' | 'architecture' | 'datamodel' | 'analysis'
    | 'ask' | 'help' | 'search' | 'lists'
    | 'restack' | 'canvas' | 'listview' | 'calendar' | 'kanban'
    | 'duplicates' | 'archived' | 'export' | 'upload';

  type Props = {
    name: IconName;
    size?: number;
  };
  let { name, size = 14 }: Props = $props();
</script>

{#if name === 'pencil'}
  <svg
    viewBox="0 0 16 16"
    width={size}
    height={size}
    fill="none"
    stroke="currentColor"
    stroke-width="1.4"
    stroke-linecap="round"
    stroke-linejoin="round"
    aria-hidden="true"
  >
    <path d="M10.5 2.5 L13.5 5.5 L5 14 L2 14 L2 11 Z" />
    <path d="M9 4 L12 7" />
  </svg>
{:else if name === 'edit'}
  <svg
    viewBox="0 0 16 16"
    width={size}
    height={size}
    fill="none"
    stroke="currentColor"
    stroke-width="1.4"
    stroke-linecap="round"
    stroke-linejoin="round"
    aria-hidden="true"
  >
    <path d="M10.5 2.5 L13.5 5.5 L5 14 L2 14 L2 11 Z" />
    <path d="M9 4 L12 7" />
  </svg>
{:else if name === 'copy'}
  <svg
    viewBox="0 0 16 16"
    width={size}
    height={size}
    fill="none"
    stroke="currentColor"
    stroke-width="1.4"
    stroke-linecap="round"
    stroke-linejoin="round"
    aria-hidden="true"
  >
    <rect x="5.5" y="5.5" width="8.5" height="8.5" rx="1" />
    <path d="M3 10.5 L3 3 L10.5 3" />
  </svg>
{:else if name === 'download'}
  <svg
    viewBox="0 0 16 16"
    width={size}
    height={size}
    fill="none"
    stroke="currentColor"
    stroke-width="1.4"
    stroke-linecap="round"
    stroke-linejoin="round"
    aria-hidden="true"
  >
    <path d="M8 2 L8 11" />
    <path d="M4.5 8 L8 11.5 L11.5 8" />
    <path d="M3 13.5 L13 13.5" />
  </svg>
{:else if name === 'workspace'}
  <svg
    viewBox="0 0 16 16"
    width={size}
    height={size}
    fill="none"
    stroke="currentColor"
    stroke-width="1.4"
    stroke-linecap="round"
    stroke-linejoin="round"
    aria-hidden="true"
  >
    <rect x="2.5" y="2.5" width="4.5" height="4.5" rx="0.5" />
    <rect x="9" y="2.5" width="4.5" height="4.5" rx="0.5" />
    <rect x="2.5" y="9" width="4.5" height="4.5" rx="0.5" />
    <rect x="9" y="9" width="4.5" height="4.5" rx="0.5" />
  </svg>
{:else if name === 'project'}
  <svg
    viewBox="0 0 16 16"
    width={size}
    height={size}
    fill="none"
    stroke="currentColor"
    stroke-width="1.4"
    stroke-linecap="round"
    stroke-linejoin="round"
    aria-hidden="true"
  >
    <path d="M2 5 L2 12.5 L14 12.5 L14 6 L7.5 6 L6 4.5 L2 4.5 Z" />
  </svg>
{:else if name === 'scratchpad'}
  <svg
    viewBox="0 0 16 16"
    width={size}
    height={size}
    fill="none"
    stroke="currentColor"
    stroke-width="1.4"
    stroke-linecap="round"
    stroke-linejoin="round"
    aria-hidden="true"
  >
    <path d="M3.5 2 L12.5 2 L12.5 14 L3.5 14 Z" />
    <path d="M5.5 5.5 L10.5 5.5" />
    <path d="M5.5 8 L10.5 8" />
    <path d="M5.5 10.5 L8.5 10.5" />
  </svg>
{:else if name === 'anchor'}
  <svg
    viewBox="0 0 16 16"
    width={size}
    height={size}
    fill="none"
    stroke="currentColor"
    stroke-width="1.4"
    stroke-linecap="round"
    stroke-linejoin="round"
    aria-hidden="true"
  >
    <circle cx="8" cy="3.5" r="1.5" />
    <path d="M8 5 L8 13" />
    <path d="M5.5 7.5 L10.5 7.5" />
    <path d="M3 10.5 C3 12 5 13.5 8 13.5 C11 13.5 13 12 13 10.5" />
  </svg>
{:else if name === 'sparkle'}
  <svg
    viewBox="0 0 16 16"
    width={size}
    height={size}
    fill="none"
    stroke="currentColor"
    stroke-width="1.4"
    stroke-linecap="round"
    stroke-linejoin="round"
    aria-hidden="true"
  >
    <path d="M8 2 L8.8 6.4 L13 7.2 L8.8 8 L8 12.4 L7.2 8 L3 7.2 L7.2 6.4 Z" />
    <path d="M12 12 L12.4 13.4 L13.8 13.8 L12.4 14.2 L12 15.6 L11.6 14.2 L10.2 13.8 L11.6 13.4 Z" />
  </svg>
{:else if name === 'link'}
  <svg
    viewBox="0 0 16 16"
    width={size}
    height={size}
    fill="none"
    stroke="currentColor"
    stroke-width="1.4"
    stroke-linecap="round"
    stroke-linejoin="round"
    aria-hidden="true"
  >
    <path d="M6.5 9.5 L9.5 6.5" />
    <path d="M5 8 L3.5 9.5 C2.5 10.5 2.5 12 3.5 13 C4.5 14 6 14 7 13 L8.5 11.5" />
    <path d="M11 8 L12.5 6.5 C13.5 5.5 13.5 4 12.5 3 C11.5 2 10 2 9 3 L7.5 4.5" />
  </svg>
{:else if name === 'move'}
  <svg
    viewBox="0 0 16 16"
    width={size}
    height={size}
    fill="none"
    stroke="currentColor"
    stroke-width="1.4"
    stroke-linecap="round"
    stroke-linejoin="round"
    aria-hidden="true"
  >
    <path d="M2 8 L11 8" />
    <path d="M8 5 L11 8 L8 11" />
    <path d="M13.5 3.5 L13.5 12.5" />
  </svg>
{:else if name === 'menu'}
  <svg viewBox="0 0 24 24" width={size} height={size} fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><line x1="3" y1="6" x2="21" y2="6" /><line x1="3" y1="12" x2="21" y2="12" /><line x1="3" y1="18" x2="21" y2="18" /></svg>
{:else if name === 'dashboard'}
  <svg viewBox="0 0 24 24" width={size} height={size} fill="none" stroke="currentColor" stroke-width="2" stroke-linejoin="round" aria-hidden="true"><rect x="3" y="3" width="7" height="7" rx="1" /><rect x="14" y="3" width="7" height="7" rx="1" /><rect x="3" y="14" width="7" height="7" rx="1" /><rect x="14" y="14" width="7" height="7" rx="1" /></svg>
{:else if name === 'jobs'}
  <svg viewBox="0 0 24 24" width={size} height={size} fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="M3 12h3.5l2-6 3.5 12 3-9 2 3H21" /></svg>
{:else if name === 'check'}
  <svg viewBox="0 0 24 24" width={size} height={size} fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="M22 11.1V12a10 10 0 1 1-5.93-9.14" /><path d="M9 11l3 3L22 4" /></svg>
{:else if name === 'architecture'}
  <svg viewBox="0 0 24 24" width={size} height={size} fill="none" stroke="currentColor" stroke-width="2" stroke-linejoin="round" aria-hidden="true"><rect x="3" y="3" width="6" height="6" rx="1" /><rect x="15" y="15" width="6" height="6" rx="1" /><path d="M9 6h4a3 3 0 0 1 3 3v6" /></svg>
{:else if name === 'datamodel'}
  <svg viewBox="0 0 24 24" width={size} height={size} fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><ellipse cx="12" cy="5" rx="8" ry="3" /><path d="M4 5v14c0 1.7 3.6 3 8 3s8-1.3 8-3V5" /><path d="M4 12c0 1.7 3.6 3 8 3s8-1.3 8-3" /></svg>
{:else if name === 'analysis'}
  <svg viewBox="0 0 24 24" width={size} height={size} fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><line x1="4" y1="20" x2="20" y2="20" /><rect x="5" y="11" width="3" height="6" /><rect x="10.5" y="7" width="3" height="10" /><rect x="16" y="4" width="3" height="13" /></svg>
{:else if name === 'ask'}
  <svg viewBox="0 0 24 24" width={size} height={size} fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="M21 11.5a8.5 8.5 0 0 1-12.5 7.5L3 21l1.9-5.5A8.5 8.5 0 1 1 21 11.5z" /></svg>
{:else if name === 'help'}
  <svg viewBox="0 0 24 24" width={size} height={size} fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><circle cx="12" cy="12" r="9" /><path d="M9.4 9.2a2.6 2.6 0 1 1 3.7 2.4c-.9.4-1.4 1-1.4 2" /><line x1="12" y1="17" x2="12.01" y2="17" /></svg>
{:else if name === 'search'}
  <svg viewBox="0 0 24 24" width={size} height={size} fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" aria-hidden="true"><circle cx="11" cy="11" r="7" /><line x1="21" y1="21" x2="16.5" y2="16.5" /></svg>
{:else if name === 'lists'}
  <svg viewBox="0 0 24 24" width={size} height={size} fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" aria-hidden="true"><line x1="8" y1="6" x2="20" y2="6" /><line x1="8" y1="12" x2="20" y2="12" /><line x1="8" y1="18" x2="20" y2="18" /><circle cx="4" cy="6" r="1" /><circle cx="4" cy="12" r="1" /><circle cx="4" cy="18" r="1" /></svg>
{:else if name === 'restack'}
  <svg viewBox="0 0 24 24" width={size} height={size} fill="none" stroke="currentColor" stroke-width="2" stroke-linejoin="round" aria-hidden="true"><path d="M12 3 3 8l9 5 9-5-9-5z" /><path d="M3 13l9 5 9-5" /></svg>
{:else if name === 'canvas'}
  <svg viewBox="0 0 24 24" width={size} height={size} fill="none" stroke="currentColor" stroke-width="2" stroke-linejoin="round" aria-hidden="true"><rect x="3" y="3" width="18" height="18" rx="2" /><line x1="3" y1="9" x2="21" y2="9" /><line x1="9" y1="9" x2="9" y2="21" /></svg>
{:else if name === 'listview'}
  <svg viewBox="0 0 24 24" width={size} height={size} fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" aria-hidden="true"><line x1="4" y1="6" x2="20" y2="6" /><line x1="4" y1="12" x2="20" y2="12" /><line x1="4" y1="18" x2="20" y2="18" /></svg>
{:else if name === 'calendar'}
  <svg viewBox="0 0 24 24" width={size} height={size} fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><rect x="3" y="4" width="18" height="17" rx="2" /><line x1="3" y1="9" x2="21" y2="9" /><line x1="8" y1="2" x2="8" y2="6" /><line x1="16" y1="2" x2="16" y2="6" /></svg>
{:else if name === 'kanban'}
  <svg viewBox="0 0 24 24" width={size} height={size} fill="none" stroke="currentColor" stroke-width="2" stroke-linejoin="round" aria-hidden="true"><rect x="3" y="3" width="5" height="18" rx="1" /><rect x="9.5" y="3" width="5" height="11" rx="1" /><rect x="16" y="3" width="5" height="14" rx="1" /></svg>
{:else if name === 'duplicates'}
  <svg viewBox="0 0 24 24" width={size} height={size} fill="none" stroke="currentColor" stroke-width="2" stroke-linejoin="round" aria-hidden="true"><rect x="9" y="9" width="11" height="11" rx="2" /><path d="M5 15H4a2 2 0 0 1-2-2V4a2 2 0 0 1 2-2h9a2 2 0 0 1 2 2v1" /></svg>
{:else if name === 'archived'}
  <svg viewBox="0 0 24 24" width={size} height={size} fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><rect x="3" y="4" width="18" height="4" rx="1" /><path d="M5 8v11a2 2 0 0 0 2 2h10a2 2 0 0 0 2-2V8" /><line x1="10" y1="12" x2="14" y2="12" /></svg>
{:else if name === 'export'}
  <svg viewBox="0 0 24 24" width={size} height={size} fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4" /><polyline points="17 8 12 3 7 8" /><line x1="12" y1="3" x2="12" y2="15" /></svg>
{:else if name === 'upload'}
  <svg viewBox="0 0 24 24" width={size} height={size} fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="M16 16l-4-4-4 4" /><path d="M12 12v9" /><path d="M20 16.5A5 5 0 0 0 18 7h-1.3A8 8 0 1 0 4 15.3" /></svg>
{/if}
