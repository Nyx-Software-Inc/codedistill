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

// Lazy Shiki wrapper. Loading the highlighter is async + heavy (~1MB); we
// instantiate it once per page and reuse. Languages are loaded on demand.
//
// The tiny surface: highlight(code, lang) -> HTML. Missing/unknown languages
// fall back to plain <pre><code>{escaped code}</code></pre>.

import type { Highlighter, BundledLanguage } from 'shiki';

let loading: Promise<Highlighter> | null = null;

async function loadHighlighter(): Promise<Highlighter> {
  if (!loading) {
    loading = import('shiki').then((m) =>
      m.createHighlighter({
        // Both themes loaded; codeToHtml emits dual-theme CSS vars and
        // app.css picks the active one via [data-theme].
        themes: ['github-dark', 'github-light'],
        // Preload nothing — languages load on demand via loadLanguage().
        langs: [],
      }),
    );
  }
  return loading;
}

// Map a file extension to a Shiki language ID. Returns null for extensions
// we don't want to try (binary, unknown, etc.).
export function langFromPath(path: string): BundledLanguage | null {
  const dot = path.lastIndexOf('.');
  if (dot < 0) return null;
  const ext = path.slice(dot + 1).toLowerCase();
  const map: Record<string, BundledLanguage> = {
    go: 'go',
    ts: 'typescript',
    tsx: 'tsx',
    js: 'javascript',
    jsx: 'jsx',
    py: 'python',
    rs: 'rust',
    c: 'c',
    cc: 'cpp',
    cpp: 'cpp',
    h: 'c',
    hpp: 'cpp',
    java: 'java',
    rb: 'ruby',
    sh: 'bash',
    bash: 'bash',
    zsh: 'bash',
    md: 'markdown',
    json: 'json',
    yml: 'yaml',
    yaml: 'yaml',
    xml: 'xml',
    html: 'html',
    css: 'css',
    sql: 'sql',
    svelte: 'svelte',
    toml: 'toml',
  };
  return map[ext] ?? null;
}

function escapeHTML(s: string): string {
  return s
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;');
}

// highlight returns highlighted HTML. `lang` can be null for plain-text rendering.
// The return always renders as a <pre><code> block with line structure preserved
// so the caller can overlay a line-number gutter.
export async function highlight(code: string, lang: BundledLanguage | null): Promise<string> {
  if (!lang) {
    return `<pre class="plain"><code>${escapeHTML(code)}</code></pre>`;
  }
  try {
    const hl = await loadHighlighter();
    // loadLanguage is safe to call for already-loaded langs.
    await hl.loadLanguage(lang);
    return hl.codeToHtml(code, {
      lang,
      themes: { dark: 'github-dark', light: 'github-light' },
      defaultColor: false,
    });
  } catch (e) {
    console.warn('shiki highlight failed; falling back to plain', e);
    return `<pre class="plain"><code>${escapeHTML(code)}</code></pre>`;
  }
}

// highlightLines highlights a whole text and returns one HTML fragment per
// line, in order — what a diff renderer needs, where every line is its own
// row with its own background. One highlighter pass, split afterwards, so a
// 2,000-line file costs the same as it does in the plain viewer. The input is
// joined with '\n' (no trailing newline) so the count matches lines.length.
export async function highlightLines(lines: string[], lang: BundledLanguage | null): Promise<string[]> {
  if (lines.length === 0) return [];
  const plain = () => lines.map((l) => escapeHTML(l));
  if (!lang) return plain();
  try {
    const hl = await loadHighlighter();
    await hl.loadLanguage(lang);
    const html = hl.codeToHtml(lines.join('\n'), {
      lang,
      themes: { dark: 'github-dark', light: 'github-light' },
      defaultColor: false,
    });
    const open = html.indexOf('<code>');
    const close = html.lastIndexOf('</code>');
    if (open < 0 || close < 0) return plain();
    const parts = html.slice(open + '<code>'.length, close).split('\n');
    if (parts.length !== lines.length) return plain();
    return parts;
  } catch (e) {
    console.warn('shiki highlightLines failed; falling back to plain', e);
    return plain();
  }
}
