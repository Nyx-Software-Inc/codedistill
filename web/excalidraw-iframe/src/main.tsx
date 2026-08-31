// =============================================================================
//  Copyright (c) 2026 Nyx Software, Inc.  All rights reserved.
//
//  CodeDistill
//
//  Property of Nyx Software, Inc., provided under a dual license: the GNU Affero General
//  Public License v3.0 (see the LICENSE file) and, separately, a commercial
//  license available from Nyx Software, Inc. Use outside the terms of one of those
//  licenses is prohibited.
//
//  SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-Nyx-Commercial
// =============================================================================
// Tell Excalidraw to fetch its runtime assets (fonts, locales,
// the vendor chunk) from the same origin instead of the default
// CDN. The assets land at /excalidraw/excalidraw-assets/ via the
// copyExcalidrawAssets vite plugin. Must be set BEFORE the
// component imports run — Excalidraw reads it during module init.
(window as unknown as { EXCALIDRAW_ASSET_PATH: string }).EXCALIDRAW_ASSET_PATH = '/excalidraw/';

import { StrictMode, useEffect, useRef, useState } from 'react';
import { createRoot } from 'react-dom/client';
import { Excalidraw, type ExcalidrawImperativeAPI } from '@excalidraw/excalidraw';
// Excalidraw 0.17 bundles its CSS inside the JS distribution — no
// separate index.css export. The styles arrive automatically when
// the component imports.

// Minimal React wrapper around Excalidraw that talks to the parent
// (the Svelte SketchEditor modal) via postMessage. Protocol:
//
//   parent → iframe:
//     { type: 'init', scene: ExcalidrawSceneJSON | null }
//       Load the given scene. null = blank canvas.
//
//     { type: 'request-save' }
//       Send back the current scene + a PNG preview.
//
//   iframe → parent:
//     { type: 'ready' }
//       Iframe loaded; parent should send 'init' next.
//
//     { type: 'save', scene: ExcalidrawSceneJSON, previewDataURL: string }
//       Reply to request-save. previewDataURL is a base64 PNG the
//       parent can either store as a blob or render inline as a
//       thumbnail. Empty string when export-to-canvas fails.
//
// Why postMessage instead of a function call: the iframe is a
// completely separate React app (avoids pulling React into the
// Svelte main bundle), so cross-frame messaging is the only
// communication channel. The protocol is tiny on purpose — two
// commands in, two events out — to keep the surface debuggable.

type Scene = {
  elements: readonly unknown[];
  appState?: Record<string, unknown>;
  files?: Record<string, unknown>;
};

function App() {
  const apiRef = useRef<ExcalidrawImperativeAPI | null>(null);
  const [initialData, setInitialData] = useState<Scene | null>(null);
  const [ready, setReady] = useState(false);

  useEffect(() => {
    function onMessage(e: MessageEvent) {
      const msg = e.data;
      if (!msg || typeof msg !== 'object') return;
      if (msg.type === 'init') {
        if (msg.scene && typeof msg.scene === 'object') {
          setInitialData(msg.scene as Scene);
        } else {
          // Trigger the Excalidraw mount with empty state.
          setInitialData({ elements: [] });
        }
      } else if (msg.type === 'request-save') {
        void handleSave();
      }
    }
    window.addEventListener('message', onMessage);
    // Announce readiness immediately so the parent knows it can
    // safely post 'init'.
    window.parent?.postMessage({ type: 'ready' }, '*');
    setReady(true);
    return () => window.removeEventListener('message', onMessage);
  }, []);

  async function handleSave() {
    const api = apiRef.current;
    if (!api) {
      window.parent?.postMessage({ type: 'save', scene: { elements: [] }, previewDataURL: '' }, '*');
      return;
    }
    const elements = api.getSceneElements();
    const appState = api.getAppState();
    const files = api.getFiles();
    let previewDataURL = '';
    try {
      // Lazy-import exportToCanvas to keep the initial bundle small.
      const mod = await import('@excalidraw/excalidraw');
      const canvas = await mod.exportToCanvas({
        elements,
        appState,
        files,
        getDimensions: () => ({ width: 512, height: 384 }),
      });
      previewDataURL = canvas.toDataURL('image/png');
    } catch {
      // Preview generation is best-effort; the save still succeeds.
    }
    window.parent?.postMessage({
      type: 'save',
      scene: { elements, appState, files },
      previewDataURL,
    }, '*');
  }

  if (!ready || !initialData) {
    return <div style={{ color: '#888', padding: 16 }}>Loading sketch editor…</div>;
  }

  return (
    <div style={{ width: '100%', height: '100%' }}>
      <Excalidraw
        excalidrawAPI={(api) => { apiRef.current = api; }}
        initialData={initialData as any}
        theme="dark"
        UIOptions={{
          // Drop bits the embedded use-case doesn't need —
          // collaboration / library / welcome don't make sense
          // inside CodeDistill.
          canvasActions: {
            loadScene: false,
            saveToActiveFile: false,
            saveAsImage: false,
          },
        }}
      />
    </div>
  );
}

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <App />
  </StrictMode>,
);
