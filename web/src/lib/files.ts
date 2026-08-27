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

// Tree utilities for FilesPanel. The backend returns a flat string[] of
// repo-relative paths (e.g. ["a/b.go", "a/c.go", "main.go"]); buildTree
// reshapes that into a folder hierarchy the recursive FileTree can render.

export interface TreeNode {
  // Last path segment ("b.go", "src", "main.go").
  name: string;
  // Full repo-relative path. For folders, the path *to* the folder
  // ("a", "a/b"). For files, the file path ("a/b.go"). Used as a stable
  // key in the expansion Set and as the drag payload for files.
  path: string;
  isDir: boolean;
  children: TreeNode[];
}

// buildTree groups a flat list of file paths into a folder tree, sorted
// folders-first and then alphabetical within each folder. Forward slashes
// are the only separator; the API normalizes path separators server-side.
export function buildTree(paths: string[]): TreeNode[] {
  // folderMap lets us de-duplicate intermediate folders that appear across
  // multiple files (e.g. "a/b/x.go" and "a/b/y.go" share the "a" and "a/b"
  // folder nodes). The empty-string key is the synthetic root.
  const folderMap = new Map<string, TreeNode>();
  const root: TreeNode = { name: '', path: '', isDir: true, children: [] };
  folderMap.set('', root);

  for (const p of paths) {
    if (!p) continue;
    const parts = p.split('/');
    let parentPath = '';
    for (let i = 0; i < parts.length; i++) {
      const name = parts[i];
      const isLeaf = i === parts.length - 1;
      const fullPath = parentPath === '' ? name : parentPath + '/' + name;

      if (isLeaf) {
        folderMap.get(parentPath)!.children.push({
          name,
          path: fullPath,
          isDir: false,
          children: [],
        });
      } else {
        let folder = folderMap.get(fullPath);
        if (!folder) {
          folder = { name, path: fullPath, isDir: true, children: [] };
          folderMap.set(fullPath, folder);
          folderMap.get(parentPath)!.children.push(folder);
        }
        parentPath = fullPath;
      }
    }
  }

  function sortRecursive(node: TreeNode) {
    node.children.sort((a, b) => {
      // Folders before files, then alphabetical.
      if (a.isDir !== b.isDir) return a.isDir ? -1 : 1;
      return a.name.localeCompare(b.name);
    });
    node.children.forEach(sortRecursive);
  }
  sortRecursive(root);
  return root.children;
}

// collectFolderPaths walks a tree and returns every folder path. Used to
// auto-expand all folders when a search filter is active so matches are
// always visible.
export function collectFolderPaths(nodes: TreeNode[]): Set<string> {
  const out = new Set<string>();
  function walk(ns: TreeNode[]) {
    for (const n of ns) {
      if (n.isDir) {
        out.add(n.path);
        walk(n.children);
      }
    }
  }
  walk(nodes);
  return out;
}

// iconForFile picks a single-character glyph for a file based on its
// extension. Kept minimal and monochrome to fit the existing terminal-y
// look; emoji can be swapped in later if/when we want richer visuals.
export function iconForFile(name: string): string {
  const dot = name.lastIndexOf('.');
  if (dot < 0) return '·';
  const ext = name.slice(dot + 1).toLowerCase();
  const code: Record<string, string> = {
    go: '◆',
    ts: '◇',
    tsx: '◇',
    js: '◇',
    jsx: '◇',
    svelte: '◇',
    py: '◆',
    rs: '◆',
    java: '◆',
    c: '◆',
    h: '◆',
    cc: '◆',
    cpp: '◆',
    hpp: '◆',
    rb: '◆',
    sh: '$',
    bash: '$',
    zsh: '$',
    md: '¶',
    txt: '¶',
    json: '⚙',
    yml: '⚙',
    yaml: '⚙',
    toml: '⚙',
    xml: '⚙',
    html: '◌',
    css: '◌',
    scss: '◌',
    sql: '⛁',
    png: '▣',
    jpg: '▣',
    jpeg: '▣',
    gif: '▣',
    svg: '▣',
    webp: '▣',
    pdf: '▤',
  };
  return code[ext] ?? '·';
}
