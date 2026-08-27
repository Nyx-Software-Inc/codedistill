/* =============================================================================
 *  Copyright (c) 2026 Nyx Software, Inc.  All rights reserved.
 *
 *  CodeDistill
 *
 *  Property of Nyx Software, Inc., provided under a dual license: the GNU Affero General
 *  Public License v3.0 (see the LICENSE file) and, separately, a commercial
 *  license available from Nyx Software, Inc. Use outside the terms of one of those
 *  licenses is prohibited.
 *
 *  SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-Nyx-Commercial
 * ============================================================================= */

// Markdown formatters for clipboard copy. Two flavors:
//
//   plainContent(item)   — just the raw content. The default copy.
//   itemToMarkdown(item) — content + name/tags/state/annotations as
//                          Markdown frontmatter-ish header. The Shift-click
//                          / "Copy as Markdown" copy. Pastes well into PRs,
//                          Slack, GitHub issues, Linear.
//
// Format (locked in project_scratchpad_item_ux.md):
//   # <name or first-line excerpt>
//   **Tags:** #foo #bar
//   **State:** classified → bug
//   **Notes:** <annotations>
//
//   <content>
//
// Sections are omitted when empty so a bare item with no metadata copies as
// just `# heading\n\ncontent`.
import type { ScratchpadItem } from './types';

// firstLineOrTruncate mirrors the Go agent.firstLineOrTruncate used to
// derive the heading when no explicit name is set.
function firstLineOrTruncate(s: string, max = 120): string {
  const t = s.trim();
  const newline = t.indexOf('\n');
  const head = newline >= 0 ? t.slice(0, newline) : t;
  return head.length > max ? head.slice(0, max) + '…' : head;
}

export function itemHeading(item: ScratchpadItem): string {
  const name = item.name?.trim();
  if (name) return name;
  return firstLineOrTruncate(item.content) || '(empty)';
}

export function plainContent(item: ScratchpadItem): string {
  return item.content;
}

export function itemToMarkdown(item: ScratchpadItem): string {
  const lines: string[] = [];
  lines.push(`# ${itemHeading(item)}`);

  if (item.tags && item.tags.length > 0) {
    lines.push(`**Tags:** ${item.tags.map((t) => `#${t}`).join(' ')}`);
  }

  // State line: only meaningful when classified or has a proposed/override.
  const stateBits: string[] = [];
  if (item.classification_state) stateBits.push(item.classification_state);
  if (item.proposed_category) stateBits.push(`→ ${item.proposed_category}`);
  if (item.classification_override) stateBits.push(`(override: ${item.classification_override})`);
  if (stateBits.length > 0) {
    lines.push(`**State:** ${stateBits.join(' ')}`);
  }

  if (item.annotations && item.annotations.trim()) {
    lines.push(`**Notes:** ${item.annotations.trim()}`);
  }

  lines.push(''); // blank line before content
  lines.push(item.content);

  return lines.join('\n');
}

// copyToClipboard wraps navigator.clipboard.writeText with a fallback to the
// document.execCommand path for older browsers / non-secure contexts.
// Returns true on success.
export async function copyToClipboard(text: string): Promise<boolean> {
  if (typeof navigator !== 'undefined' && navigator.clipboard?.writeText) {
    try {
      await navigator.clipboard.writeText(text);
      return true;
    } catch {
      // fall through to legacy path
    }
  }
  // Legacy fallback: textarea + execCommand. Removed once browsers
  // universally support clipboard API in non-secure contexts (already true
  // for localhost which is our primary target).
  try {
    const ta = document.createElement('textarea');
    ta.value = text;
    ta.style.position = 'fixed';
    ta.style.opacity = '0';
    document.body.appendChild(ta);
    ta.select();
    const ok = document.execCommand('copy');
    document.body.removeChild(ta);
    return ok;
  } catch {
    return false;
  }
}
