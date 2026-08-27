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

// UC-50: the in-app help center content — subject-driven, grouped, tier-aware.
// Each topic's `body` is markdown, rendered by the in-bundle converter. `tier`
// gates the body: a topic above the user's tier still shows in the sidebar, but
// its pane shows an upgrade notice instead of the content.

export type HelpTier = 'free' | 'pro' | 'enterprise';

export interface HelpTopic {
  id: string;
  title: string;
  tier: HelpTier;
  body: string;
}

export interface HelpGroup {
  id: string;
  title: string;
  topics: HelpTopic[];
}

export const TIER_LABEL: Record<HelpTier, string> = {
  free: 'Free',
  pro: 'Pro',
  enterprise: 'Enterprise',
};

export const HELP_GROUPS: HelpGroup[] = [
  {
    id: 'orientation',
    title: 'Orientation',
    topics: [
      {
        id: 'welcome',
        title: 'Welcome & the three surfaces',
        tier: 'free',
        body: `
# Welcome to CodeDistill

CodeDistill is where the **intent** behind your work lives — captured, classified,
and (when you want it) held accountable all the way to the code that satisfies it.
Think of it as an Integrated *Distillation* Environment: a place that distills
messy notes, requests, and ideas into tracked, actionable, verifiable work.

## The three surfaces

Everything you do happens across three areas that fit together:

- **Project view** — the left rail. It anchors a *project* to a repository on
  disk (its \`repo_root\`) and holds the project's files and scratchpads; the
  header carries the tools (Dashboard, Needs review, Search, Analyze…).
  Pick what you're working on here.
- **Canvas** — the center. A spatial scratchpad where you drop notes, code,
  links, and files as cards, arrange them, group them, and watch them get
  classified into work items. Most of your day-to-day capture happens here.
- **Code window** — opens when you follow a code anchor. It shows the file and
  line range an item is tied to, so you can jump from *intent* (an item) straight
  to the *code* that implements it — and back.

The flow between them: capture on the **canvas** → items get classified and
tracked → each item can be anchored to code you view in the **code window** →
the **project view** ties it all to one repo and gives you the dashboards.

New here? Read **"What is Glassbox?"** next, then **"The loop."**
`,
      },
      {
        id: 'what-is-glassbox',
        title: 'What is Glassbox?',
        tier: 'free',
        body: `
# What is "Glassbox"?

Most tools are a **black box**: work goes in, code comes out, and the connection
between the two lives only in someone's head. When an AI agent does the work,
that gap gets worse — you can't easily answer "did this actually get done, and
where?"

**Glassbox** is CodeDistill's answer: make the whole path *visible and
accountable*.

- **Intent is a first-class thing.** A todo, bug, or use case isn't just a title
  — it can carry **acceptance criteria** (a checkable definition of done).
- **Work is traced to code.** When something is implemented, CodeDistill records
  the commit and the exact files/lines — a **provenance** trail from intent →
  code → commit (we call the visualization *Throughline*).
- **Done is proven, not asserted.** The verification layer can actually *run*
  your checks in an isolated checkout at the recorded commit and report a real
  verdict — instead of trusting "it's done."
- **Attention goes where it's earned.** Risk scoring and a review queue surface
  the changes that need human eyes, and per-project *trust* adjusts how much gets
  auto-cleared over time.

You don't have to use all of it. The point is: the box is glass. You can always
see what an item is, whether it's really done, and where it lives in the code.
`,
      },
      {
        id: 'the-loop',
        title: 'The loop',
        tier: 'free',
        body: `
# The loop, in one line

**Capture → Classify → Work → Verify** — and back again.

1. **Capture.** Drop anything onto the canvas — a note, an error, a link, a file,
   a half-formed idea. Nothing is lost; it lands as a card.
2. **Classify.** A local model reads each card and proposes what it is — a
   **todo**, **bug**, **knowledge (KB)** entry, or **use case** — and shows the
   proposal on the card (and in **Needs review**) for you to accept or re-route.
   One click confirms.
3. **Work.** The item is now tracked, with a status lifecycle and (optionally)
   acceptance criteria. You — or an AI agent connected over MCP — do the work.
4. **Verify.** Record where you implemented it (commit + files); verification
   runs your checks at that commit and marks criteria satisfied or failed.
   Risky work then waits for **your sign-off** — right on the item, or from the
   Needs review panel — and that decision feeds the project's earned trust.

The strength is that each step leaves a trace, so the loop is *auditable*, not
just fast. Start small: capture a few things and accept their classifications.
`,
      },
    ],
  },
  {
    id: 'canvas',
    title: 'The canvas & items',
    topics: [
      {
        id: 'scratchpad-canvas',
        title: 'The scratchpad canvas',
        tier: 'free',
        body: `
# The scratchpad canvas

A **scratchpad** is a named board inside a project; the **canvas** is its spatial
view. Drop things anywhere; position is yours to control.

- **Capture** — type or paste into the composer at the bottom and hit *Send*
  (or Ctrl/Cmd+Enter). Drop OS files onto the canvas to upload them; drop a URL
  to drop it into the composer so you can *Summarize* or just *Send* it.
- **Arrange** — drag cards around. Use **Restack** (the ▾ menu) for *Tidy*,
  *Fit to content*, or *2 / 3 columns* layouts; each pad remembers its choice.
- **Group** — select cards and group them into a container.
- **Views** — the same items can be shown as a **canvas**, a **list**, a
  **kanban** board, or a **calendar** (by due date). See *Views & due dates*.
- **Scope** — scratchpads are per project. Cross-scope roll-ups live in the
  Lists drawer and the Dashboard, not on a single pad.

## Archiving vs deleting
Two very different ways to clear a card off the board:

- **Archive** *(reversible)* — takes the item **off the canvas** but *keeps* it:
  it stays stored and **searchable**, and a **"Show archived"** toggle brings it
  back so you can un-archive. Use this for things you're done with but might want
  to find again later. Archived items give up their canvas space — Restack
  compacts as if they're gone, and un-archiving places the card at the next free
  row (never on top of anything).
- **Delete** *(permanent)* — removes the item for good, after a confirm. Use it
  only for genuine junk or mistakes; deletion can't be undone.

Rule of thumb: **archive by default, delete rarely.**

Tip: hover a card to see a brief of what it's about; a colored header bar can
flag due dates (turn it on in Canvas settings).
`,
      },
      {
        id: 'item-types',
        title: 'Item types & properties',
        tier: 'free',
        body: `
# Item types & their properties

Two things live on a canvas: **content cards** you author, and the **work items**
they get classified into.

## Content cards (what you drop)
- **Note** — free text / markdown. The default.
- **Code snippet** — paste code; kept verbatim.
- **Link** — a URL. Auto-enriched with the page's title/description/image
  (OpenGraph), or *Summarize* it into an AI summary + the link.
- **Image / File** — dropped from your OS; stored as a blob.
- **Sketch / Doc** — richer composite cards.
- **Group** — a container that holds and moves other cards together.

## Work items (what a card becomes)
Once classified, a card derives one of:
- **Todo** — something to do. Has priority, status, an optional **due date**, and
  acceptance criteria.
- **Bug** — a defect. Has severity, a status lifecycle, reproduction/expected/
  actual fields, and a due date.
- **Knowledge (KB)** — reference material worth keeping.
- **Use case** — a capability the product should enable ("As a … I want …"),
  with role/want/why and a target release.

Common properties across work items: a **number** (shown as \`#12\` / \`UC-3\`),
status, **code anchors** (links to files/lines), and a full **history** of
lineage events (who changed what, and via what — UI, agent, or MCP).

## The item detail — five tabs, read left to right
Opening a work item shows tabs in the order the work happens:

1. **Todo / Bug / Use case** — the editable fields (subject, status, severity/
   priority, due date, reproduction…). This is where you *work the record*.
2. **Acceptance** — the checkable definition of done (see *Acceptance criteria*).
   You set this up **before** the work.
3. **Provenance** — the throughline: intent → commit → the exact code that
   satisfied it. It's **empty until an implementation is recorded** — that's
   not broken, it's waiting. Once work lands, click a code line to jump into it.
4. **Verification** — the real, reproducible pass/fail from running your checks
   at the recorded commit (see *Verification*).
5. **Log** — an append-only timeline of everything: status changes, code
   locations linked, verification runs, review decisions, notes. The Log owns
   the **when**; Provenance owns the **what/where** (no overlap).

The left tabs are *setup* (what it is, what "done" means); the right three are
*evidence* that fills in as work happens. That's the reading order.

## The immutable source
A card's **original captured text** is frozen — it's provenance. On a work
item's source view you'll see it read-only; editing it is refused so a status
update or note can't quietly rewrite what you originally said. Genuine typo fix?
Use **Correct…** — a deliberate, logged edit. Everything else (progress,
updates) belongs in the **Log**.
`,
      },
      {
        id: 'categorization',
        title: 'How items are categorized',
        tier: 'free',
        body: `
# How items get categorized

You don't have to file things by hand. A **local model** (Ollama by default)
reads each new card and proposes a category.

- **Classification** runs automatically when you Send a card. The card shows a
  *pending-review* state while the model thinks (a busy indicator confirms your
  click registered), then a suggestion appears.
- **Needs review** (the ✓ button in the header, with a count badge) collects
  everything awaiting your judgment — captures pending classification AND
  implemented items awaiting sign-off. Classification can also be accepted
  right on the card's banner.
- **Force a type** at capture time with the override pills (Todo/Bug/KB/Skip) if
  you already know what it is.
- **Nothing auto-commits.** Classification is a *suggestion*; the item is only
  tracked once you accept. This keeps you in control and the board honest.

Because classification is local, your content never leaves the machine for this
step (see *Model providers* to opt into a remote model deliberately).

## Capture-time extraction — fire and forget
Classification also pre-fills fields **from what your text explicitly says**,
deterministically (no model guessing):

- **Due dates** — "…by Friday", "due tomorrow", "in 2 weeks".
- **Priority** (todos) — "URGENT", "asap", "p1" → high; "no rush",
  "nice to have", "someday" → low.
- **Severity** (bugs) — "crash", "data loss", "security" → critical;
  "typo", "cosmetic" → trivial.

The rules are deliberately strict: only **explicit signals** fire ("the button
doesn't work" sets nothing), negations never fire as their positive ("not
urgent" ≠ urgent), and conflicting signals leave the field at its default —
wrong pre-filled metadata is worse than none. Everything stays editable on the
work record.

Tip: user-story phrasing ("As a … I want …") is recognized and its role/want/why
are pulled out, so accepting a use case pre-fills the structured fields. Inline
**#hashtags** become tags — on your captures and on agent (MCP) captures alike.
`,
      },
      {
        id: 'views',
        title: 'Views & due dates',
        tier: 'free',
        body: `
# Views & due dates

The same items render four ways — switch with the view toggle:

- **Canvas** — spatial cards you arrange yourself.
- **List** — a compact, scannable list; groups expand to show their contents.
- **Kanban** — columns by status; drag to change state.
- **Calendar** — items placed on a month/week grid by **due date**.

## Due dates
Todos, bugs, and use cases can carry a due date. In the **Calendar** view,
items without one sit in a "No due date" tray at the bottom — **drag one onto a
day** to assign it (end of that day), or drag a dated item to another day to
reschedule. Optionally, a card's header bar tints **yellow** within 48h and
**red** within 24h/overdue (toggle in Canvas settings).
`,
      },
      {
        id: 'search',
        title: 'Search',
        tier: 'free',
        body: `
# Search

Find items by meaning, not just keywords. CodeDistill embeds your items locally
and searches by **semantic similarity**, so "auth is flaky" can surface a bug
titled "intermittent 401 on token refresh."

- Open **Search** from the project tools.
- Results span the scratchpad's items (and derived todos/bugs/KB/use cases).
- Embeddings are computed and stored **locally** and always stay local — even if
  you opt a *generative* model to a remote provider, the embedder does not move
  (switching embedders would invalidate every stored vector).
`,
      },
    ],
  },
  {
    id: 'contract',
    title: 'Working the contract',
    topics: [
      {
        id: 'acceptance-criteria',
        title: 'Acceptance criteria',
        tier: 'free',
        body: `
# Acceptance criteria — the definition of done

An item's **acceptance criteria** are checkable statements that must all be true
for it to count as done. They turn a vague title into a contract.

- Each criterion is a short, testable claim ("Returns 400 on an empty body").
- Criteria can be **mapped to a test** so the verification layer can mark them
  satisfied or failed automatically. *Unmapped* criteria stay a human judgment.

## Two ways criteria appear
- **AI-proposed** — when a card is classified, the model drafts criteria from
  it. These start **proposed**; you **ratify** the good ones (proposed →
  accepted) and dismiss the rest. Only *accepted* criteria are the real bar —
  the box won't verify against something you haven't committed to.
- **Your own** — type a criterion in plain English in the **Add** field and
  press Add (or Enter). Because you authored it, it lands **already accepted**
  (no ratify step) and is marked *user-authored*. That's the difference: a
  machine's draft needs your sign-off; your own is the bar by definition.

Write criteria *before* the work when you can — they keep both you and any AI
agent aimed at the same target, and they're what makes "done" provable later.
`,
      },
      {
        id: 'verification',
        title: 'Verification',
        tier: 'pro',
        body: `
# Verification — automatic, isolated, real

Verification runs your project's checks against the **exact commit** an item was
implemented at, in an **isolated git worktree**, and reports a real verdict —
instead of trusting "it's done."

- Configure a **test command** for the project. When an item records its
  implementation (commit + files), the box checks out that commit in a throwaway
  worktree and runs the command with a timeout and output cap.
- The result is a **pass / fail / skipped / error** verdict per check, with the
  captured output — not a self-report. **Skipped** means the tool isn't
  installed (it couldn't run) — deliberately *not* a red fail, so a missing
  scanner never makes a genuine pass look broken.
- **Language-agnostic by design.** It runs *your* configured commands in the
  worktree — \`go test ./...\` for Go, \`gradle test\` / \`detekt\` for Kotlin,
  \`pytest\` for Python. It doesn't know or care what language; it runs what you
  told it to (Settings → Verification).
- **Criterion mapping** ties individual acceptance criteria to specific checks,
  so passing advances "satisfied" and failing flags "failed."
- A portfolio of layers stacks on top: scanners, and an opt-in **adversarial AI
  review** that tries to *refute* each criterion against the diff (advisory —
  it never silently moves a criterion's state).

Verification and human sign-off are **separate gates**: the box produces the
evidence automatically; *you* judge it (see *Review by risk & Trust*). The
sign-off prompt on an item only appears **after** verification has run — you
judge evidence, so there has to be evidence.

The point: "done" becomes evidence, reproducible at the recorded commit.
`,
      },
      {
        id: 'review-trust',
        title: 'Review by risk & Trust',
        tier: 'pro',
        body: `
# Review by risk & earned trust

Not every change needs your eyes — but the risky ones do. CodeDistill routes
**attention** instead of asking you to review everything.

- **Risk scoring** ranks changes; **always-review zones** force review for
  sensitive areas no matter what.
- **Earned trust** is per-project: as a project builds a verification track
  record, more low-risk work auto-clears and less gets escalated. There's a
  machine ceiling, and you can tighten (never loosen) it by hand.
- **Sign-off happens on the item**: an item an agent marked done shows an
  "awaiting your sign-off" bar on its detail. It appears **after verification
  has run** — you judge evidence, so there's evidence to judge. **Approve**
  closes it and feeds trust; **Needs rework** sends it back — the item
  **reopens** to a non-terminal status (bug → open, todo/use-case → in
  progress) and re-enters the active queue. **Needs review** (header ✓ button)
  is the queue view of everything waiting on you; the **Dashboard** shows the
  stats.
- **The loop closes:** every approve/reject you make is recorded on the item's
  Log and feeds back as evidence into the trust model.

The whole dial lives in **project Settings → Trust**: the earned tier, your
minimum review level, always-review zones, and enforcement — one place.

The result is a system that pays attention where it's earned — and stops nagging
you where it isn't.
`,
      },
      {
        id: 'settings',
        title: 'Settings',
        tier: 'free',
        body: `
# Settings

Settings cascade across **three scopes**, most specific wins:

1. **Scratchpad** — settings for one board.
2. **Project** — defaults for the whole project.
3. **User** — your personal defaults across projects.

A value set at a narrower scope overrides the broader one. Canvas ergonomics
(restack layout, auto-tidy, type colors, due-date header coloring) are remembered
too — some per-scratchpad and device-local, some in your user settings so they
follow you across devices.
`,
      },
    ],
  },
  {
    id: 'glassbox',
    title: 'Glass-box accountability',
    topics: [
      {
        id: 'provenance',
        title: 'Provenance & Throughline',
        tier: 'free',
        body: `
# Provenance & Throughline

Every item can carry **code anchors** — pointers to the files and line ranges
that relate to it — with a recorded *provenance* (how the anchor was created:
you added it, an agent recorded it, or it was auto-detected from a permalink).

**Throughline** is the visualization of that chain: **intent → code → commit.**
Open an item and you can see where it lives in the code, jump to the file (the
code window), and read its **history** — a lineage log of every change and its
source (UI / agent / MCP). When an item is completed, the commit and file ranges
are captured, so the trail is concrete, not remembered.

This is the backbone of the glass box: you can always answer "what implements
this, and when did it land?"
`,
      },
      {
        id: 'datamodel',
        title: 'Data-model (ER) diagram',
        tier: 'free',
        body: `
# Data-model diagram

An **entity-relationship diagram drawn deterministically from your SQL** — no
model in the loop, so it can't invent tables. CodeDistill finds your schema
(a canonical \`schema.sql\`, or migrations replayed in apply order), and renders
the final state: tables, columns, keys.

- **Solid lines** are declared \`FOREIGN KEY\`s. **Dashed lines** are \`*_id\`
  naming-convention guesses — honestly labeled as inferred, with a toggle to
  hide them.
- Click a table to expand all its columns; **Keys only** keeps big schemas
  scannable.

## Provenance — every element shows its source
Hover a **table** to see which file \`CREATE\`d it and which later migrations
altered it; hover a **column** to see the exact file that introduced it. When
your schema spans multiple directories, tables are **color-tinted by their
defining directory** (with a legend) — foundational vs experimental areas are
visible at a glance.

Export the diagram as **PNG** or a portable **mermaid** doc, or publish it
straight onto a scratchpad.
`,
      },
      {
        id: 'drift',
        title: 'Drift detection',
        tier: 'free',
        body: `
# Drift detection

Keeps the map honest as the code changes underneath it. The Dashboard's **Drift**
panel flags when the code an item is anchored to has **moved or changed** since
the anchor was recorded — so stale links *surface* instead of silently rotting.

It's about catching the exact moment your recorded intent-to-code links no longer
point where they should, so you can re-anchor or re-check before the trail goes
cold.
`,
      },
      {
        id: 'architecture',
        title: 'Architecture diagram',
        tier: 'free',
        body: `
# Architecture diagram

A map of your project's **components** and how they relate, drawn from the
**real repository structure** — every component directory becomes a node, and
edges are derived **structurally** from real imports, never invented. You
**ratify** what's right (proposed → ratified), so the picture is yours, not the
LLM's guess.

## Drafting: structure now, intelligence streams in
Hit **Draft** and the full skeleton — every component, every edge — appears
**immediately** with honest path-derived names. A background job then
*characterizes* each component (name, kind, description) one model call at a
time, and the diagram visibly upgrades as results land:

- The progress line shows **n/N and a time estimate**; **Cancel** any time.
- The job runs **on the server** — close the panel (or the app) and come back
  to a further-along diagram. After a restart, **Resume** picks up exactly
  where it left off; nothing is lost or redone.
- Clicking Draft never deletes work. **Re-draft** (a separate, confirmed
  action) is the only way to start over.
- Click the **✨ button on a container** to characterize that directory first.

## Containers — big systems stay readable
Nodes cluster into **collapsible containers** by top-level directory (the
hierarchy your code already has). A collapsed container is one box —
"core/ · 13 components" — with its members' edges bundled into single counted
lines. Large diagrams open collapsed for a clean overview; expand what you're
looking at. Exports always carry the full diagram.

## The as-built overlay — it can't lie
The diagram is continuously checked against the **real repo tree**:

- Components (nodes) with **no matching code** get painted **red**.
- Code areas the diagram **doesn't cover** are listed out.

So the diagram reflects what *actually exists*, not what you wish existed —
the glass-box view of your system's shape, kept honest by the code itself.

*(No repository connected? Draft falls back to a conceptual sketch from the
project brain and use cases.)*
`,
      },
      {
        id: 'governance',
        title: 'Governance & gates',
        tier: 'pro',
        body: `
# Governance — turn advice into gates

By default, CodeDistill's checks are **advisory**. Governance lets you promote
them to **enforcement** on a dial:

**off → flag → gate → hard-block.**

- **flag** — surface the issue, don't stop anything.
- **gate** — block *completion* of an item while it's failing (e.g. verification
  is red, or a high-severity security finding is open) — with a justified-dismissal
  escape hatch where appropriate.
- **hard-block** — the strictest setting.

Enforcement is unified across the verification and analysis layers, and applies
to agent actions too (an agent can't mark an item done through the gate any more
than you can via the UI). The free tier is clamped to advisory; raising the dial
is a paid capability.
`,
      },
    ],
  },
  {
    id: 'analysis',
    title: 'Code analysis',
    topics: [
      {
        id: 'code-analysis',
        title: 'Code Analysis & Scan',
        tier: 'free',
        body: `
# Code Analysis — the findings inbox

**Analyze** is a findings inbox fed by the SAST ecosystem via **SARIF** — one
normalized format, so CodeDistill rides established scanners rather than a
home-grown engine.

- **Scan** (free) — a built-in convenience button orchestrates the free SARIF
  tools you have installed (e.g. gosec, staticcheck, semgrep, detekt for Kotlin)
  and shows the results. It is **never silently empty**: a tool that times out,
  is missing, or errors is reported as a note, not a falsely-clean zero.
- Findings are **stateful** by SARIF fingerprint: new / open / resolved as the
  code changes — not naive dedup.
- **Select findings** and push them to a scratchpad, where they're validated and
  categorized as bugs, todos, etc. — then they leave the findings page.

Install more scanners to widen coverage; CodeDistill parses whatever SARIF they
emit.
`,
      },
      {
        id: 'findings-ingest',
        title: 'Findings ingest & routing',
        tier: 'enterprise',
        body: `
# External findings ingest

Beyond the local Scan button, Enterprise adds ways to *feed* findings in from
your pipeline and route them automatically:

- **Ingest** SARIF from CI, an API call, a watched directory, or over MCP.
- **Auto-route** findings by threshold + target scratchpad, so severity-N issues
  land where the right people triage them.
- Combined with **Governance**, a security-severity finding at or above your
  threshold can **gate completion** until it's fixed or justifiably dismissed.

This turns CodeDistill's findings inbox into the hub your existing scanners
report into.
`,
      },
    ],
  },
  {
    id: 'agents',
    title: 'Agents & integrations',
    topics: [
      {
        id: 'mcp',
        title: 'MCP — connect your agent',
        tier: 'free',
        body: `
# MCP — connect your agent

CodeDistill speaks **MCP** (Model Context Protocol), so an AI coding agent can
pull your intent and (when licensed) update it — without you copy-pasting.

- **Reads are free** on every install — the funnel by design. Any agent can list
  projects/scratchpads, read items and their acceptance criteria, get the project
  brain, and see commit changes. This is how an agent knows *what to build*.
- **Writes are a paid feature.** With MCP licensed, the agent can \`complete_item\`
  / \`update_item\`, record implementations with code anchors, map criteria to
  tests, ingest findings, and more — closing the loop from the agent side.
- **Transports:** a stdio server (\`codedistill mcp\`) for local agents, and an
  HTTP \`/mcp\` endpoint on \`codedistill serve\`. Write tools over HTTP require
  the API token; on an unlicensed build the write tools aren't registered at all.
- **Capture parity:** items an agent captures behave like yours — classified the
  same way, inline **#hashtags** surfaced as tags, due dates and explicit
  priority/severity signals extracted at capture.

The upshot: your agent reads the contract, does the work, and writes back the
provenance — all through one protocol.
`,
      },
      {
        id: 'mcp-export',
        title: 'MCP export — push to your tools',
        tier: 'pro',
        body: `
# MCP export — push your items to other tools

MCP works **both ways**. The previous topic is CodeDistill as a *server* (an
agent uses it). This is CodeDistill as an MCP *client*: push your items **out**
to another tool's MCP server — a ticketing system, an issue tracker, a todo app —
so the work you capture here mirrors into the tool your team already lives in.

- **Configure a destination.** Point CodeDistill at an external MCP server and
  map your item types and operations to its tools. A **mapping wizard** helps,
  with LLM-assisted suggestions for which local field maps to which remote
  tool/argument (todo → the tracker's "create issue", etc.).
- **Automatic push on write.** When you create or update a todo/bug/use case that
  has a mapped destination, a hook **enqueues a push** to that server and flips
  the item's sync status. A **status transition routes specially** — completing
  an item can call the destination's "close" tool rather than a generic update.
- **Best-effort and tracked.** The local write always wins; a failed push is
  logged and retried on the next edit. Each item remembers its remote counterpart
  (\`remote_id\`) so updates land on the right ticket.
- **Off by default.** Nothing leaves CodeDistill until you configure a destination.

So the full picture: agents **read and write** your intent through CodeDistill's
own MCP server, and CodeDistill in turn **mirrors** that intent **out** to your
existing tools through *their* MCP servers. CodeDistill sits in the middle as the
source of truth, speaking MCP in both directions.
`,
      },
      {
        id: 'skills',
        title: 'Skills',
        tier: 'pro',
        body: `
# Skills — reusable agent instructions, shared by the team

Every capable LLM has some notion of "skills" or custom instructions. The problem
is that they're usually **tied to one tool's configuration** and live on one
person's machine.

CodeDistill's Skills are different in two ways that matter for a team:

- **Configuration-independent.** A Skill is a reusable instruction set delivered
  **over MCP**, not baked into a particular client's config. Any agent that
  connects — whichever tool it is — can discover skills (\`list_skills\`) and
  fetch one (\`get_skill\`). You author it once; it isn't trapped in one editor's
  settings.
- **Immediately available across the team.** Because skills live in CodeDistill
  (the shared source of truth), everyone pointed at the project gets the same
  skills the moment they're published — no per-person setup, no drift between
  teammates' local configs.

So instead of "here's the prompt, paste it into your tool," it's "the skill is in
the project — your agent already has it."
`,
      },
      {
        id: 'model-providers',
        title: 'Model providers',
        tier: 'free',
        body: `
# Model providers — local by default

The generative work (classification, match judging, mapping suggestions, code
analysis triage/review) runs on a **local model** out of the box — **Ollama**,
privacy-by-default, nothing leaves your machine.

- **Local-first.** Ollama with a sensible default model; hardware-aware defaults.
- **Pluggable, opt-in remote.** You can point the generative roles at a remote,
  OpenAI-compatible endpoint (your own API key) when you want frontier-model
  quality — with an explicit consent step that names what content would leave the
  machine. Local stays the default; remote is never silent.
- **Embeddings always stay local.** The embedder does not move to a remote
  provider — switching it would invalidate every stored vector.
- **Reviewer independence.** The adversarial reviewer can be a *different* model
  from the one that did the work (fox vs. henhouse), and review is advisory, so a
  weak reviewer is never fatal.

Background analysis with a remote provider warns about per-scan API cost.
`,
      },
    ],
  },
  {
    id: 'dedup',
    title: 'Duplicate intelligence',
    topics: [
      {
        id: 'dedup',
        title: 'Dedup',
        tier: 'pro',
        body: `
# Duplicate intelligence (Dedup)

As your board grows, the same idea shows up twice. Dedup finds likely duplicates
by **meaning** (using the same local embeddings that power search), so near-copies
surface even when the wording differs.

- An on-demand **Dedup scan** flags possible duplicate pairs within a scratchpad.
- When the agent flags a possible duplicate at classify time, the card offers to
  open the other item or dismiss the suggestion ("not a duplicate").
- Boilerplate is stripped before comparison — two unrelated user stories that
  both open with "As a user, I would like …" won't be judged duplicates just for
  sharing the template.

Dedup is a paid capability; on the free build the scan is gated.
`,
      },
    ],
  },
  {
    id: 'teams',
    title: 'Teams (server)',
    topics: [
      {
        id: 'multi-user',
        title: 'Multi-user server',
        tier: 'enterprise',
        body: `
# Working as a team (server)

The desktop app is single-user on SQLite. The **server** configuration runs
multi-user on Postgres, with a shared workspace and real identity.

- **Sign-in** via OIDC; revocable database-backed sessions; per-user API tokens
  for agents.
- **Seats** = active members. Lifecycle is deactivate-not-delete, with a
  last-owner guard and a break-glass path.
- **Roles & admin.** An admin console shows the member roster; roles gate
  administrative actions.
- **Attribution.** Changes are attributed to the acting user, and the history/
  Throughline shows who did what (and whether via UI, agent, or MCP).

Everything glass-box — verification, review, trust, governance — works the same;
it just spans a team now.
`,
      },
      {
        id: 'profiles',
        title: 'Profiles',
        tier: 'enterprise',
        body: `
# Profiles

Click the **avatar** (top-right) for your profile menu — a Gmail-style bubble.

- **Edit name** — update your display name inline.
- **License** — see the licensed holder/edition at a glance.
- **Settings / Admin** — jump to settings, and (for owners/admins in multi-user)
  the admin console.
- **Sign out** (multi-user).

The window title separately watermarks the LICENSE holder as an anti-theft cue.
`,
      },
    ],
  },
  {
    id: 'licensing',
    title: 'Licensing',
    topics: [
      {
        id: 'tiers',
        title: 'Tiers & activating a license',
        tier: 'free',
        body: `
# Tiers & licensing

CodeDistill is **open-core**. The community edition is free and AGPL-licensed;
paid capabilities are unlocked by a commercial license.

- **Free** — capture, classify, the canvas and all views, search, acceptance
  criteria, local models, **MCP reads**, the local **Scan**. The whole loop's
  core.
- **Pro** — **MCP writes** (agents act, not just read), **code anchors**,
  **duplicate intelligence**, and the **List/Calendar/Kanban** views.
- **Enterprise** — everything in Pro, plus the multi-user server
  (auth/admin/profiles), S3-compatible storage, external findings ingest &
  auto-routing, and **governance enforcement** (raising the dial past
  advisory).

## Activating — redeem your purchase code
Buying from the store emails you a **claim code** (\`cdk_…\`). In the app, open
the **profile menu → 🔑 Redeem purchase code…**, paste it, and the license is
issued for this machine and installed — restart to apply. Renewed your
subscription? **⟳ Refresh license** re-issues against the same code. Each
distinct machine you redeem on uses one of your purchased seats.

Enterprise (hand-issued) licenses install as a file instead:
\`codedistill license install <file>\` (verified before it's installed).

Licenses are signed and **machine-locked**, so they can't be copied across
systems — \`codedistill license fingerprint\` prints this machine's identity if
support asks. You'll get a heads-up banner in the last 30 days before expiry;
in a multi-user setup, admins see the renew action.

*Community-edition builds physically lack the paid code*, so a license unlocks
nothing there — install the official build from codedistill.dev, then redeem.

Not sure what you have? The profile menu and the About/license view show your
current edition.
`,
      },
    ],
  },
  {
    id: 'patterns',
    title: 'Usage patterns',
    topics: [
      {
        id: 'pattern-manager',
        title: 'As a manager — todo & KB',
        tier: 'free',
        body: `
# Pattern: the manager's second brain

You don't have to be shipping code to get value. Used as a **personal todo
manager and knowledge base**, CodeDistill shines because *capture is frictionless*
and *nothing gets lost*.

- **Capture everything** onto one scratchpad during the day — action items,
  decisions, links, meeting notes. Send and move on.
- Let classification sort them: action items become **todos** (give the important
  ones a **due date** and watch them on the **Calendar**); durable references
  become **KB** entries you can *search* semantically later.
- Use **List** or **Kanban** view to work the todos; the **Inbox** to triage the
  day's captures in one pass.
- Skip the code side entirely — anchors, verification, and MCP are there when you
  want them, invisible when you don't.

The win: one low-friction inbox for your whole work life, with a searchable memory
attached.
`,
      },
      {
        id: 'pattern-developer',
        title: 'As a developer — the coding loop',
        tier: 'free',
        body: `
# Pattern: the developer's accountable loop

When you're building, run the full **Glassbox** loop:

1. **Point the project at your repo** (\`repo_root\`) so anchors and verification
   have something to resolve against.
2. **Capture intent** as todos/bugs/use cases, and write **acceptance criteria**
   — the contract for done.
3. **Connect your agent over MCP.** It reads the item and criteria (reads are
   free), does the work, and — with MCP writes — records the implementation
   (commit + files) back onto the item.
4. **Verify.** The box runs your test command at the recorded commit in an
   isolated worktree; criteria flip to satisfied/failed on real evidence.
5. **Review what's risky.** The review queue surfaces changes that need eyes;
   trust auto-clears the rest; governance can *gate* completion while checks are
   red.
6. **Watch for drift.** Stale anchors and architecture gaps surface on the
   Dashboard as the code moves.

The payoff: your agent moves fast, and every step it takes is traceable and
provable — the box stays glass.
`,
      },
    ],
  },
];

// Rank tiers so a topic at or below the user's tier shows its body.
const RANK: Record<HelpTier, number> = { free: 0, pro: 1, enterprise: 2 };

/** effectiveTier maps the running license to a coarse help tier. */
export function effectiveTier(license: {
  oss_build?: boolean;
  state?: string;
  edition?: string;
} | null): HelpTier {
  if (!license || license.oss_build || license.state !== 'valid') return 'free';
  const ed = (license.edition ?? '').toLowerCase();
  if (ed.includes('enterprise')) return 'enterprise';
  if (ed.includes('pro') || ed.includes('team')) return 'pro';
  return 'pro'; // a valid paid license we can't classify → treat as Pro
}

/** unlocked reports whether a topic's body is available at the user's tier. */
export function unlocked(topic: HelpTopic, userTier: HelpTier): boolean {
  return RANK[topic.tier] <= RANK[userTier];
}
