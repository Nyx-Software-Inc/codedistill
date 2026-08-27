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

// A tiny, dependency-free markdown→HTML renderer for OUR OWN trusted docs (the
// Glassbox guide). Deliberately NOT a general engine: the heavyweight editor
// markdown stack (Carta + Shiki) splits into lazy chunks that aren't in the PWA
// precache, so rendering the guide failed silently in the installed app. This
// runs synchronously, entirely in the main bundle — works offline and in the
// PWA. Covers the subset the guide uses: headings, bold/italic, inline + fenced
// code, links, ordered/unordered lists, tables, blockquotes, and rules.

function escapeHtml(s: string): string {
  return s.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;');
}

// Inline formatting on already-escaped text. Code spans are pulled out first
// (replaced with an improbable @@C<idx>@@ token) so their contents aren't
// reformatted and so the token never collides with real digits in the prose.
function inline(s: string): string {
  const codes: string[] = [];
  s = s.replace(/`([^`]+)`/g, (_m, c: string) => `@@C${codes.push(c) - 1}@@`);
  s = s.replace(/\[([^\]]+)\]\(([^)]+)\)/g, (_m, t: string, u: string) => `<a href="${u}" target="_blank" rel="noreferrer">${t}</a>`);
  s = s.replace(/\*\*([^*]+)\*\*/g, '<strong>$1</strong>');
  s = s.replace(/(^|[^*])\*([^*\n]+)\*(?!\*)/g, '$1<em>$2</em>');
  s = s.replace(/@@C(\d+)@@/g, (_m, i: string) => `<code>${codes[Number(i)]}</code>`);
  return s;
}

function splitRow(line: string): string[] {
  let s = line.trim();
  if (s.startsWith('|')) s = s.slice(1);
  if (s.endsWith('|')) s = s.slice(0, -1);
  return s.split('|').map((c) => c.trim());
}

export function renderMarkdown(md: string): string {
  const lines = md.replace(/\r\n/g, '\n').split('\n');
  const out: string[] = [];
  let para: string[] = [];
  let i = 0;
  const flushPara = () => {
    if (para.length) {
      out.push(`<p>${inline(escapeHtml(para.join(' ')))}</p>`);
      para = [];
    }
  };
  while (i < lines.length) {
    const line = lines[i];

    if (/^```/.test(line)) {
      flushPara();
      i++;
      const code: string[] = [];
      while (i < lines.length && !/^```/.test(lines[i])) code.push(lines[i++]);
      i++; // closing fence
      out.push(`<pre><code>${escapeHtml(code.join('\n'))}</code></pre>`);
      continue;
    }

    const h = /^(#{1,6})\s+(.*)$/.exec(line);
    if (h) {
      flushPara();
      out.push(`<h${h[1].length}>${inline(escapeHtml(h[2]))}</h${h[1].length}>`);
      i++;
      continue;
    }

    if (/^(-{3,}|\*{3,}|_{3,})\s*$/.test(line)) {
      flushPara();
      out.push('<hr>');
      i++;
      continue;
    }

    // Table: a row with pipes followed by a |---|---| separator line.
    if (/\|/.test(line) && i + 1 < lines.length && /^\s*\|?[\s:|-]*-[\s:|-]*\|?\s*$/.test(lines[i + 1])) {
      flushPara();
      const header = splitRow(line);
      i += 2;
      const rows: string[][] = [];
      while (i < lines.length && lines[i].includes('|') && lines[i].trim() !== '') rows.push(splitRow(lines[i++]));
      let t = '<table><thead><tr>' + header.map((c) => `<th>${inline(escapeHtml(c))}</th>`).join('') + '</tr></thead><tbody>';
      for (const r of rows) t += '<tr>' + r.map((c) => `<td>${inline(escapeHtml(c))}</td>`).join('') + '</tr>';
      out.push(t + '</tbody></table>');
      continue;
    }

    if (/^\s*[-*]\s+/.test(line)) {
      flushPara();
      const items: string[] = [];
      while (i < lines.length && /^\s*[-*]\s+/.test(lines[i])) items.push(lines[i++].replace(/^\s*[-*]\s+/, ''));
      out.push('<ul>' + items.map((it) => `<li>${inline(escapeHtml(it))}</li>`).join('') + '</ul>');
      continue;
    }

    if (/^\s*\d+\.\s+/.test(line)) {
      flushPara();
      const items: string[] = [];
      while (i < lines.length && /^\s*\d+\.\s+/.test(lines[i])) items.push(lines[i++].replace(/^\s*\d+\.\s+/, ''));
      out.push('<ol>' + items.map((it) => `<li>${inline(escapeHtml(it))}</li>`).join('') + '</ol>');
      continue;
    }

    if (/^>\s?/.test(line)) {
      flushPara();
      const q: string[] = [];
      while (i < lines.length && /^>\s?/.test(lines[i])) q.push(lines[i++].replace(/^>\s?/, ''));
      out.push(`<blockquote>${inline(escapeHtml(q.join(' ')))}</blockquote>`);
      continue;
    }

    if (line.trim() === '') {
      flushPara();
      i++;
      continue;
    }

    para.push(line);
    i++;
  }
  flushPara();
  return out.join('\n');
}
