<!--
  The chrome around a slot: which pane is showing, how to switch, how to move it.

  One frame per slot, not per pane. A slot shows one pane and tabs the rest, so
  the tab strip belongs to the slot — and a drop onto it means exactly one thing,
  "put this pane here", with no "or split it" to disambiguate. That is what keeps
  drag and drop affordable later.

  Knows nothing about what a pane contains. The caller passes a snippet that
  renders the active pane, so each component keeps its own props and this file
  never grows a list of them.
-->
<script lang="ts">
  import type { Snippet } from 'svelte';
  import Icon from './Icon.svelte';
  import { insertionIndex, paneDef, SLOTS, type SlotID } from '../lib/layout';
  import {
    slotPanes, activePane, activatePane, closePane, movePane,
    draggingPane, beginDrag, endDrag,
  } from '../lib/layoutStore.svelte';

  type Props = {
    slot: SlotID;
    /** Renders the pane currently showing. Given its id so the caller can
     *  switch on it. */
    content: Snippet<[string]>;
  };
  let { slot, content }: Props = $props();

  // Read through the store on every render, never captured into a local at init.
  // A value read once at component creation is read before its async load
  // finishes and never again — the bug that cost three rounds on a preference.
  const panes = $derived.by(() => slotPanes(slot));
  const active = $derived.by(() => activePane(slot));

  let moveOpen = $state(false);

  /** Where this pane could go. Its current slot is excluded: offering a move to
   *  where it already is invites a click that does nothing. */
  const elsewhere = $derived.by(() => SLOTS.filter((s) => s !== slot));

  function titleOf(id: string): string {
    return paneDef(id)?.title ?? id;
  }

  function moveTo(to: SlotID) {
    const id = active;
    moveOpen = false;
    if (id) movePane(id, to);
  }

  // ── Drag and drop ─────────────────────────────────────────────────────────
  //
  // An input method over movePane, which already existed and was already
  // tested. Nothing here decides what a move MEANS; it only works out which
  // slot and which index the pointer is asking for.
  //
  // The pane id travels in dataTransfer as well as the shared store: the store
  // drives the drop affordances (other frames need to light up), while
  // dataTransfer is what makes the drop itself survive if anything resets the
  // store mid-drag.
  const MIME = 'application/x-codedistill-pane';

  /** Where an insertion marker is drawn, or null for none. */
  let markerAt = $state<number | null>(null);
  let strip: HTMLElement | undefined = $state();

  function onDragStart(e: DragEvent, id: string) {
    e.dataTransfer?.setData(MIME, id);
    // 'move' rather than 'copy' — the cursor should not promise a duplicate.
    if (e.dataTransfer) e.dataTransfer.effectAllowed = 'move';
    beginDrag(id);
  }

  function dragged(e: DragEvent): string | null {
    return e.dataTransfer?.getData(MIME) || draggingPane();
  }

  /** The index the pointer is asking for, from the live tab geometry. */
  function indexFor(e: DragEvent): number {
    if (!strip) return panes.length;
    const boxes = Array.from(strip.querySelectorAll('.pane-tab')).map((el) => {
      const r = el.getBoundingClientRect();
      return { start: r.left, end: r.right };
    });
    return insertionIndex(boxes, e.clientX);
  }

  function onStripOver(e: DragEvent) {
    if (!draggingPane()) return;
    // Without preventDefault the browser refuses the drop entirely — the single
    // most common reason a hand-rolled drag target silently does nothing.
    e.preventDefault();
    if (e.dataTransfer) e.dataTransfer.dropEffect = 'move';
    markerAt = indexFor(e);
  }

  function onStripDrop(e: DragEvent) {
    e.preventDefault();
    const id = dragged(e);
    const at = indexFor(e);
    markerAt = null;
    endDrag();
    if (id) movePane(id, slot, at);
  }

  /** Dropping on the body means "put it here", with no position implied. */
  function onBodyOver(e: DragEvent) {
    if (!draggingPane()) return;
    e.preventDefault();
    if (e.dataTransfer) e.dataTransfer.dropEffect = 'move';
  }

  function onBodyDrop(e: DragEvent) {
    e.preventDefault();
    const id = dragged(e);
    endDrag();
    if (id) movePane(id, slot);
  }

  // True while something is being dragged from ANOTHER slot — the signal that
  // this slot is a worthwhile target. Dragging within a slot is a reorder and
  // needs the marker, not a whole-slot highlight.
  const foreign = $derived.by(() => {
    const d = draggingPane();
    return !!d && !panes.includes(d);
  });
</script>

<div class="pane-frame">
  <header class="pane-head">
    <!-- A tab per pane, always visible even with one. The strip is where the
         title, the close button and the move menu live, so hiding it for a
         single pane would mean two different headers to maintain. -->
    <!-- tabindex -1: the drop handlers make the checker treat the strip as
         interactive, and -1 satisfies that without adding a tab stop. The tabs
         themselves are buttons and are already reachable. -->
    <div
      class="pane-tabs tab-scroll"
      class:target={foreign}
      role="tablist"
      aria-label="Panes in this slot"
      tabindex={-1}
      bind:this={strip}
      ondragover={onStripOver}
      ondragleave={() => (markerAt = null)}
      ondrop={onStripDrop}
    >
      {#each panes as id, i (id)}
        {#if markerAt === i}<span class="drop-marker" aria-hidden="true"></span>{/if}
        <button
          class="pane-tab"
          class:active={id === active}
          class:dragging={draggingPane() === id}
          role="tab"
          aria-selected={id === active}
          draggable="true"
          ondragstart={(e) => onDragStart(e, id)}
          ondragend={() => { endDrag(); markerAt = null; }}
          onclick={() => activatePane(id)}
        >{titleOf(id)}</button>
      {/each}
      {#if markerAt === panes.length}<span class="drop-marker" aria-hidden="true"></span>{/if}
    </div>

    <div class="pane-actions">
      {#if active}
        <div class="move-wrap">
          <button
            class="pane-btn"
            title="Move this pane to another edge"
            aria-haspopup="menu"
            aria-expanded={moveOpen}
            onclick={() => (moveOpen = !moveOpen)}
          ><Icon name="move" size={13} /></button>
          {#if moveOpen}
            <!-- Explicit movement, which is the point: picking a destination
                 says exactly one thing, where a drop onto an occupied slot
                 could mean "tab into" or "split". Dragging comes later as
                 another way to call the same operation. -->
            <div class="move-menu" role="menu">
              {#each elsewhere as s (s)}
                <button role="menuitem" onclick={() => moveTo(s)}>Move to {s}</button>
              {/each}
            </div>
          {/if}
        </div>
        <button
          class="pane-btn"
          title="Close {titleOf(active)}"
          aria-label="Close {titleOf(active)}"
          onclick={() => closePane(active)}
        >×</button>
      {/if}
    </div>
  </header>

  <div
    class="pane-body"
    class:target={foreign}
    ondragover={onBodyOver}
    ondrop={onBodyDrop}
    role="presentation"
  >
    {#if active}
      {@render content(active)}
    {/if}
  </div>
</div>

<style>
  .pane-frame {
    display: flex;
    flex-direction: column;
    /* min-width/height 0 is what lets the body actually scroll inside a grid
       cell. Without it a flex child refuses to shrink and its content spills
       over its neighbours — the same failure that made long documents overrun
       the editor modal. */
    min-width: 0;
    min-height: 0;
    overflow: hidden;
    background: var(--p-1c1c1c);
  }

  /* The strip is DARKER than the pane body, and the active tab is the body
     colour. That inversion is what makes a tab read as a tab: the active one
     looks continuous with the content below it and the others look like they
     are behind it. A dim label with a 2px underline — the first attempt — was
     indistinguishable from a heading, and read as one. */
  .pane-head {
    display: flex;
    align-items: stretch;
    gap: 0.25rem;
    background: var(--p-141414);
    border-bottom: 1px solid var(--border);
    flex-shrink: 0;
    min-width: 0;
  }

  .pane-tabs {
    display: flex;
    flex: 1;
    min-width: 0;
    overflow-x: auto;
    align-items: stretch;
  }

  .pane-tab {
    position: relative;
    border: 0;
    border-right: 1px solid var(--p-222222);
    background: none;
    color: var(--p-808080);
    padding: 0.34rem 0.75rem;
    font-size: 0.78rem;
    font-family: inherit;
    cursor: pointer;
    white-space: nowrap;
    /* Lines the active tab up with the body so the seam disappears. */
    margin-bottom: -1px;
  }
  .pane-tab:hover { color: var(--text); background: var(--p-1a1a1a); }
  .pane-tab.active {
    color: var(--text);
    background: var(--p-1c1c1c);
    border-bottom: 1px solid var(--p-1c1c1c);
  }
  /* An accent cap on top rather than underneath: an underline on the active tab
     would cut it off from the content it belongs to. */
  .pane-tab.active::before {
    content: '';
    position: absolute;
    inset: 0 0 auto 0;
    height: 2px;
    background: var(--accent);
  }

  .pane-actions {
    display: flex;
    align-items: center;
    gap: 0.1rem;
    padding-right: 0.2rem;
    flex-shrink: 0;
  }

  /* Was var(--p-777777) at 0.95rem with no hit area, which on the right-hand
     slot was effectively invisible. A pane control has to be findable without
     hunting for it. */
  .pane-btn {
    display: flex;
    align-items: center;
    justify-content: center;
    width: 20px;
    height: 20px;
    border: 0;
    border-radius: 3px;
    background: none;
    color: var(--p-aaaaaa);
    cursor: pointer;
    padding: 0;
    font-size: 1.05rem;
    line-height: 1;
  }
  .pane-btn:hover { color: var(--text); background: var(--p-333333); }

  .move-wrap { position: relative; display: flex; align-items: center; }
  .move-menu {
    position: absolute;
    top: 100%;
    right: 0;
    z-index: 40;
    background: var(--p-222222);
    border: 1px solid var(--p-3a3a3a);
    border-radius: 4px;
    padding: 0.15rem;
    display: flex;
    flex-direction: column;
    min-width: 8rem;
  }
  .move-menu button {
    border: 0;
    background: none;
    color: var(--text);
    text-align: left;
    padding: 0.25rem 0.5rem;
    font-size: 0.78rem;
    font-family: inherit;
    cursor: pointer;
    border-radius: 3px;
    text-transform: capitalize;
  }
  .move-menu button:hover { background: var(--p-333333); }

  /* A tab mid-drag stays visible but recedes: hiding it would make the strip
     jump and move every other tab out from under the pointer. */
  .pane-tab.dragging { opacity: 0.4; }

  .drop-marker {
    flex: none;
    width: 2px;
    margin: 2px 0;
    background: var(--accent);
    border-radius: 1px;
  }

  /* Only lit for a pane arriving from somewhere else. A reorder inside one slot
     shows the insertion marker instead — highlighting the slot you are already
     in says nothing. */
  .pane-tabs.target { background: var(--p-1b2430); }
  .pane-body.target { outline: 2px dashed var(--accent); outline-offset: -2px; }

  .pane-body {
    flex: 1;
    min-width: 0;
    min-height: 0;
    overflow: hidden;
    display: flex;
    flex-direction: column;
  }
</style>
