<!--
  A popped-out pane: one pane in a window of its own (docs/design/panes.md,
  "Popping a pane out of the window").

  A separate copy of the app, not a window the main one draws into. It loads
  its own data and keeps it current from its own event stream, as every pane
  already does. What it cannot do itself, such as opening an item or a file,
  it asks the main window to do over the bridge (lib/popout.svelte.ts).

  It never saves the layout. It reports its size and position, and the main
  window saves them.
-->
<script lang="ts">
  import { onMount } from 'svelte';
  import * as api from './lib/api';
  import type { Project } from './lib/types';
  import { SSEChannel, EventNames } from './lib/events';
  import { initTheme } from './lib/theme';
  import { initFonts } from './lib/fonts';
  import { initUiPrefs } from './lib/uiPrefs.svelte';
  import { LOCAL_USER } from './lib/userSettings';
  import { paneDef } from './lib/layout';
  import { listen, send, paneTaken, type Call, type SharedState } from './lib/popout.svelte';
  import type { ChangesetPick } from './components/ChangesetPicker.svelte';
  import Drawer from './components/Drawer.svelte';
  import JobMonitor from './components/JobMonitor.svelte';
  import NeedsReviewPanel from './components/NeedsReviewPanel.svelte';
  import ChangesetPane from './components/ChangesetPane.svelte';
  import DialogHost from './components/DialogHost.svelte';
  import Icon from './components/Icon.svelte';

  type Props = { pane: string; w: number; h: number };
  let { pane, w, h }: Props = $props();

  const MIME = 'application/x-codedistill-pane';
  // The key the main window keeps its active project under: the fallback when
  // this window starts before the main one (a restored session).
  const ACTIVE_PROJECT_KEY = 'ui.active_project';
  /** How long the main window has to come back (a reload) before this window
   *  closes with it. A slow reload that misses it reopens the window anyway:
   *  the main window restores every popped-out pane it does not hear from. */
  const MAIN_GONE_MS = 3000;

  const title = $derived(paneDef(pane)?.title ?? pane);

  let projectId = $state('');
  let projects = $state<Project[]>([]);
  const activeProject = $derived(projects.find((p) => p.id === projectId) ?? null);
  let changeset = $state<ChangesetPick | null>(null);
  /** False once the main window has said it closed. */
  let mainOpen = $state(true);
  /** Bumped on every change the server announces, as the main window's
   *  reviewTick is. */
  let tick = $state(0);
  let filesTick = $state(0);
  let jobsReload = $state(0);
  /** A pane's tab from another window is over this one. */
  let dropping = $state(false);

  function call(c: Call) {
    send({ t: 'call', pane, call: c });
  }

  function dock() {
    send({ t: 'dock', pane });
  }

  function applyState(s: SharedState) {
    mainOpen = true;
    if (s.projectId && s.projectId !== projectId) projectId = s.projectId;
    changeset = s.changeset;
  }

  async function loadProjects() {
    try {
      projects = await api.listProjects();
    } catch {
      // The pane shows its own error when its data fails to load.
    }
  }

  // ── Bounds ──────────────────────────────────────────────────────────────
  //
  // Reported as the inner size, which is what window.open's width and height
  // mean. There is no event for a window being moved, so the position is
  // sampled; on Wayland it never changes from what the window was told, which
  // is harmless (the desktop places the window regardless).
  let lastBounds = '';
  function reportBounds() {
    const b = { w: innerWidth, h: innerHeight, x: screenX, y: screenY };
    const k = JSON.stringify(b);
    if (k === lastBounds) return;
    lastBounds = k;
    send({ t: 'bounds', pane, b });
  }

  onMount(() => {
    document.title = `${title} — CodeDistill`;
    void initTheme();
    void initFonts();
    void initUiPrefs();

    // The size this window was meant to open at. The desktop app's Chrome
    // opens every window at its launch size, so the window corrects itself.
    if (w > 0 && h > 0 && (Math.abs(innerWidth - w) > 8 || Math.abs(innerHeight - h) > 8)) {
      try { window.resizeBy(w - innerWidth, h - innerHeight); } catch { /* not ours to size */ }
    }

    let closing: ReturnType<typeof setTimeout> | undefined;
    const stop = listen((m) => {
      switch (m.t) {
        case 'state': applyState(m.state); break;
        case 'ask': send({ t: 'hello', pane }); break;
        case 'main-hello':
          mainOpen = true;
          clearTimeout(closing);
          send({ t: 'hello', pane });
          break;
        case 'main-bye':
          // The main window closed, or is reloading. A reload says hello again
          // in a moment; a close does not, and a popped-out pane must not
          // outlive it: its window would keep the app running, unseen. Its
          // place is kept, so it opens again with the main window.
          mainOpen = false;
          clearTimeout(closing);
          closing = setTimeout(() => window.close(), MAIN_GONE_MS);
          break;
        case 'focus': if (m.pane === pane) window.focus(); break;
        case 'close': if (m.pane === pane) window.close(); break;
      }
    });
    send({ t: 'hello', pane });

    // No main window answered (it is closed, or this window was restored
    // first): fall back to the project the main window last had open.
    const fallback = setTimeout(async () => {
      if (projectId) return;
      try {
        const r = await api.getUserSetting(LOCAL_USER, ACTIVE_PROJECT_KEY);
        if (!projectId && r && typeof r.value === 'string' && r.value) projectId = r.value;
      } catch { /* the pane shows nothing until a project is known */ }
    }, 800);
    void loadProjects();

    const sse = new SSEChannel();
    const bump = () => (tick += 1);
    const unsubs = [
      sse.on(EventNames.ItemsChanged, bump),
      sse.on(EventNames.InboxChanged, bump),
      sse.on(EventNames.TodosChanged, bump),
      sse.on(EventNames.BugsChanged, bump),
      sse.on(EventNames.UseCasesChanged, bump),
      sse.on(EventNames.FilesChanged, () => (filesTick += 1)),
      sse.on(EventNames.ProjectsChanged, () => void loadProjects()),
    ];
    sse.open();

    let resizeTimer: ReturnType<typeof setTimeout> | undefined;
    const onResize = () => {
      clearTimeout(resizeTimer);
      resizeTimer = setTimeout(reportBounds, 300);
    };
    window.addEventListener('resize', onResize);
    const moveTimer = setInterval(reportBounds, 1000);
    const bye = () => send({ t: 'bye', pane });
    window.addEventListener('pagehide', bye);

    return () => {
      stop();
      clearTimeout(fallback);
      clearTimeout(closing);
      for (const u of unsubs) u();
      sse.close();
      window.removeEventListener('resize', onResize);
      clearInterval(moveTimer);
      window.removeEventListener('pagehide', bye);
    };
  });

  // ── Dragging ────────────────────────────────────────────────────────────
  //
  // The tab drags back into the main window, whose slots take it; the main
  // window then tells this one to close. This window's own dragend cannot tell
  // a drop on the main window from a drop on the desktop (both say "none"),
  // so it does nothing on its own.
  function onDragStart(e: DragEvent) {
    e.dataTransfer?.setData(MIME, pane);
    if (e.dataTransfer) e.dataTransfer.effectAllowed = 'move';
  }

  // A tab dragged here from the main window is taken and ignored: this window
  // holds one pane. Taking it stops the main window popping it out a second
  // time, which is what a drop it never hears about would otherwise mean.
  function isPaneDrag(e: DragEvent): boolean {
    return !!e.dataTransfer?.types.includes(MIME);
  }
  function onOver(e: DragEvent) {
    if (!isPaneDrag(e)) return;
    e.preventDefault();
    // Not 'none': a drop that is refused never fires, and then nothing would
    // tell the main window the tab landed here.
    if (e.dataTransfer) e.dataTransfer.dropEffect = 'move';
    dropping = true;
  }
  function onDrop(e: DragEvent) {
    if (!isPaneDrag(e)) return;
    e.preventDefault();
    dropping = false;
    const id = e.dataTransfer?.getData(MIME);
    if (id && id !== pane) paneTaken(id);
  }
</script>

<main
  class="pane-window"
  class:dropping
  ondragover={onOver}
  ondragleave={() => (dropping = false)}
  ondrop={onDrop}
  role="presentation"
>
  <header class="pane-head">
    <div class="pane-tabs">
      <span
        class="pane-tab active"
        draggable="true"
        ondragstart={onDragStart}
        role="tab"
        aria-selected="true"
        tabindex="0"
        title="Drag into the main window to put it back"
      >{title}</span>
    </div>
    <div class="pane-actions">
      <button class="dock-btn" onclick={dock} title="Put {title} back in the main window">
        <Icon name="move" size={13} /> Dock
      </button>
    </div>
  </header>

  {#if !mainOpen}
    <div class="main-closed" role="status">
      The main CodeDistill window is closed. This pane keeps working;
      opening items and files needs the main window.
      <button onclick={() => window.open(new URL('/', location.href).toString(), '_blank')}>Open it</button>
    </div>
  {/if}

  <div class="pane-body">
    <!-- Every pane is per project. Rendering one before the project is known
         asks the server for "no project" and shows the error it gets back. -->
    {#if !projectId}
      <p class="waiting">Loading…</p>
    {:else if pane === 'tree'}
      <Drawer
        {activeProject}
        filesRefreshTick={filesTick}
        onProjectImported={(p) => call({ fn: 'projectImported', projectId: p.id })}
        onOpenFile={(path) => call({ fn: 'openFile', path })}
      />
    {:else if pane === 'jobs'}
      <JobMonitor
        {projectId}
        reloadKey={jobsReload}
        onNewJob={() => call({ fn: 'newJob' })}
        onReview={(rid) => call({ fn: 'reviewJob', jobId: rid })}
        onOpenItem={(ot, oid) => call({ fn: 'openItem', ownerType: ot, ownerId: oid })}
      />
    {:else if pane === 'inbox'}
      <NeedsReviewPanel
        {projectId}
        projectName={activeProject?.name ?? ''}
        {tick}
        onOpenItem={(ot: string, oid: string) => call({ fn: 'openItem', ownerType: ot, ownerId: oid })}
        onChanged={() => call({ fn: 'changed' })}
        onOpenChangeset={(cs) => call({ fn: 'openChangeset', changeset: cs })}
      />
    {:else if pane === 'review'}
      <ChangesetPane
        {projectId}
        {changeset}
        {tick}
        onPick={(cs) => { changeset = cs; call({ fn: 'pickChangeset', changeset: cs }); }}
        onOpenFile={(p) => call({ fn: 'openFile', path: p })}
        onOpenItem={(t, id) => call({ fn: 'openItem', ownerType: t, ownerId: id })}
        onReviewChanged={() => call({ fn: 'changed' })}
      />
    {/if}
  </div>
</main>
<DialogHost />

<style>
  .pane-window {
    height: 100%;
    display: flex;
    flex-direction: column;
    background: var(--p-1c1c1c);
  }
  .pane-window.dropping { outline: 2px dashed var(--p-555555); outline-offset: -2px; }

  /* The same strip as a docked pane's (PaneFrame), so a popped-out pane looks
     like the pane it is. */
  .pane-head {
    display: flex;
    align-items: stretch;
    gap: 0.25rem;
    background: var(--p-141414);
    border-bottom: 1px solid var(--border);
    flex-shrink: 0;
  }
  .pane-tabs { display: flex; flex: 1; min-width: 0; }
  .pane-tab {
    position: relative;
    color: var(--text);
    background: var(--p-1c1c1c);
    border-right: 1px solid var(--p-222222);
    border-bottom: 1px solid var(--p-1c1c1c);
    margin-bottom: -1px;
    padding: 0.34rem 0.75rem;
    font-size: 0.78rem;
    cursor: grab;
    white-space: nowrap;
    user-select: none;
  }
  .pane-tab::before {
    content: '';
    position: absolute;
    inset: 0 0 auto 0;
    height: 2px;
    background: var(--accent);
  }
  .pane-actions { display: flex; align-items: center; padding-right: 0.3rem; }
  .dock-btn {
    display: flex;
    align-items: center;
    gap: 0.3rem;
    border: 0;
    border-radius: 3px;
    background: none;
    color: var(--p-aaaaaa);
    font: inherit;
    font-size: 0.75rem;
    padding: 0.15rem 0.45rem;
    cursor: pointer;
  }
  .dock-btn:hover { color: var(--text); background: var(--p-333333); }

  .main-closed {
    display: flex;
    align-items: center;
    gap: 0.6rem;
    padding: 0.4rem 0.7rem;
    font-size: 0.78rem;
    background: var(--p-2a2310);
    color: var(--text);
    border-bottom: 1px solid var(--border);
  }
  .main-closed button {
    border: 1px solid var(--p-555555);
    border-radius: 3px;
    background: none;
    color: var(--text);
    font: inherit;
    padding: 0.1rem 0.5rem;
    cursor: pointer;
  }

  .waiting { margin: 1rem; color: var(--p-808080); font-size: 0.8rem; }

  .pane-body {
    flex: 1;
    min-width: 0;
    min-height: 0;
    overflow: hidden;
    display: flex;
    flex-direction: column;
  }
</style>
