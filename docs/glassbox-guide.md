# CodeDistill — the Glassbox guide

## The IDE was built for the wrong person

For thirty years an IDE was where a **human** wrote software — autocomplete, a
debugger, a place to type. Every part of it assumed a person at the keyboard,
and the keyboard was the bottleneck.

When an **agent** writes the code, that assumption breaks. You don't need help
typing anymore. The bottleneck moves to **trust**: does the thing it produced
actually do what you asked, and can you see *why* it believes so?

**CodeDistill is the IDE redrawn for that world.** We kept the three letters and
changed what they stand for — not Integrated *Development* Environment but the
**Integrated Distillation Environment**. You supply intent. An agent supplies
code. CodeDistill distills the two together into something **verified and
traceable** — the agent works *behind glass, not in a black box*.

That pane of glass is the **Glassbox**: the accountability layer where every
change an agent makes is tied to the intent that asked for it, checked against
your acceptance criteria and your test suite, and routed for human review by
risk. Not a black box you hope is right — a glass box you can look straight into.

Out of the box it runs **entirely on your machine** (SQLite + a local model via
Ollama). No cloud, no code leaving the building. A team can share one
**self-hosted server** instead — same promise, still your infrastructure (see
*Working as a team* below).

---

## The loop, in one line

```
capture intent → set criteria → agent implements + records → Glassbox verifies
in isolation → risk-routed review → (optional) governance gates → done, traceable
```

Everything below is that loop in detail, then the parts that make it sharper
(the project brain, architecture map, drift, Skills).

---

## Setting up

### 1. Run it
```
codedistill serve            # web UI + API on http://127.0.0.1:8080
```

### 2. Point a project at your repo
Open a project's ⚙ (the gear in the **Project pane** — that's *project* settings;
your personal profile menu is the **avatar in the top-right**):

- Set **repo_root** to your code's path, so the Glassbox can read git, run your
  tests against real commits, and index the code.
- Under **Verification**, set the commands the box runs to check work — `test`,
  and optionally `lint / types / sast / vuln`. A non-zero exit means *failing*.
  CodeDistill **detects your language** from the repo (Go, Java, Node, Python,
  Rust, C#/.NET, Ruby, Kotlin, C/C++) and pre-fills example commands — hit
  **"Use <language> defaults"** to fill the empty fields, then edit to taste.
- Optionally set **always-review zones** (path globs like `auth`, `migrations`,
  `billing`) whose changes always escalate to you regardless of risk.

### 3. Capture intent
Paste anything into a scratchpad — a stray thought, an error, a link, a spec.
It's auto-classified into a **Todo / Bug / Use case / KB** entry. You can force a
type, group related cards, attach a **Sketch** (Excalidraw canvas) or **Doc**
(markdown + inline images) from the same composer, and add typed **custom
fields** (e.g. story points) per project.

### 4. Set the contract — acceptance criteria
On an item's **Acceptance** tab, write the criteria the work must satisfy — or
hit **"Draft with AI"** (free, local) and edit. These are the contract the agent
builds toward and the thing the Glassbox checks against. Sharp criteria here are
what make verification meaningful later.

### 5. Connect your agent over MCP
- **stdio** (Claude Desktop, etc.): point the MCP client at `codedistill mcp`.
- **HTTP**: point the agent at `http://127.0.0.1:8080/mcp` (bearer = your API
  token, in Settings → Server access).

Read tools (`read_item`, `get_project_brain`, `list_skills` / `get_skill`) are
**free** — the agent can always orient itself. The **write** tools
(`record_implementation`, `map_criterion_test`, `mark_implemented`,
`record_decision`, …) are a **Pro** feature.

### 6. The agent works the item
It reads the item, its criteria, and the project brain; implements; then calls
**`record_implementation`** (the commit + the files it touched). Optionally it
maps each criterion to a command with **`map_criterion_test`** so individual
criteria can pass or fail on their own evidence.

---

## How the Glassbox holds the work accountable

### Verification — automatic, isolated, real
`record_implementation` fires verification. The box checks out an **isolated
worktree at the exact commit** the agent recorded and runs your configured
checks *there* — never against your dirty working tree. It's a **portfolio**, not
a single gate:

- **Tests + scanners** — your `test / lint / types / sast / vuln` commands. A
  non-zero exit is a failing verdict.
- **Per-criterion mapping** — criteria mapped to commands advance to
  *satisfied* / *failed* on their own results, so "passed the suite" and "met
  this specific criterion" are tracked separately.
- **Adversarial AI review** (opt-in) — a separate, independent local model reads
  each criterion against the diff and tries to **refute** it. It's an advisory
  second opinion; it never moves criterion state on its own. (Pick a stronger,
  code-specialized reviewer model in settings — it must differ from the model
  that wrote the code: no fox guarding the henhouse.)

Verdicts land on the item's **Verification** tab.

### Review by risk — not everything, just what needs eyes
The dashboard's **review queue** ranks implemented items by **risk**: blast
radius (how much changed, how widely), the verification verdict, whether the
change touched a sensitive zone, and whether it was reverted.

The **trust dial** learns from your project's verification track record and
per-area history. Low-risk changes with a clean record **auto-clear**; the ones
that need a human get **escalated**. The machine sets a ceiling; you can always
**tighten** it, never loosen below what the evidence supports. When you
**approve or reject** from the queue, that decision feeds back as evidence — the
dial gets more accurate over time.

### Governance — turn advice into gates (Pro)
Project Settings → **Governance** promotes checks from advisory to enforcing:
`off → flag → gate → hard-block`. At **gate** and above, an item **can't be
marked complete while its verification is failing** — and the same gate binds the
**agent's** `mark_implemented`, not just the UI button. Verification, review, and
architecture each get their own enforcement level.

---

## Beyond the loop — context that keeps the agent honest

- **Project brain** — a living summary of the project (purpose, conventions, key
  decisions) the agent reads via `get_project_brain`, and writes back to with
  `record_decision`. Better context in, better code out.
- **Architecture diagram + as-built delta** — a diagram of the intended
  architecture overlaid against what's *actually* in the repo. Nodes with no
  matching code paint **red**; uncovered code areas are listed. The diagram
  can't lie about what's built.
- **Drift detection** — the dashboard's **Drift** panel surfaces where intent and
  code have diverged: criteria that no longer match the code, decisions the repo
  contradicts.
- **Skills** — reusable, named instructions (`list_skills` / `get_skill`) the
  agent can pull in for recurring tasks, so house style and gotchas travel with
  the work. Skills are **versioned** (every edit cuts an immutable version), and
  their *use* is **evidence-gated provenance**: retrievals are logged, and
  `record_implementation` can attest which skill versions produced a change —
  never blanket-assumed. When a skill version turns out flawed, **remediation**
  routes every change made under it back for review or re-verification. You can
  answer "which code did v3 of this skill touch?" — and act on it.

## Working the board
Items render in whatever lens fits: **canvas** (spatial), **list**, **calendar**
(by due date), or **Kanban** (drag across native per-type statuses — the same
governance gate applies when you drag into a "done" column). **Duplicate
detection** flags likely-related items as you go, and can group them.

---

## Working as a team (server)

The single-user desktop above runs entirely on your machine. A team shares one
**self-hosted CodeDistill server** instead: the same binary, pointed at
**Postgres** — still your infrastructure, still your local models, just
multi-user. (Desktop is untouched by any of this; no identity provider configured
means no sign-in, exactly as before.)

- **Sign in with your IdP.** Configure OpenID Connect (Google, Okta, Entra,
  Keycloak) on the server and people sign in with accounts they already have —
  no passwords for CodeDistill to store, and sessions are server-side and
  **revocable**.
- **Everyone has an identity.** Your **avatar** (top-right) opens your profile:
  edit your display name, see the license, reach Settings, sign out — and
  **Admin**, if you're an owner or admin. Hover any name on the board to see who
  it really is, and the window title always shows who the license is registered
  to (a quiet anti-theft watermark — a vanity name can't hide it).
- **Claim what you're working on.** Hit **Claim** on a todo, bug, or use case and
  it's tagged to you — pre-filled from your sign-in — so the board shows who's on
  what and the accountability trail names a **person**, not "someone". Agents act
  through **per-user API tokens**, so automated work is attributed too.
- **Seats and offboarding.** Your license carries a seat count, enforced at
  sign-in. When someone leaves, an admin **deactivates** them from the admin
  console: their seat frees up, their history stays intact. Deactivate, never
  delete — the trail is the whole point.

---

## The shape of it

```
   capture intent ─▶ acceptance criteria ─▶ agent implements + records
        ▲                                            │
        │                                            ▼
   feeds trust ◀── risk-routed review ◀── Glassbox verifies in isolation
                          │
                          ▼
              (optional) governance gates ─▶ done, fully traceable
```

That's the Glassbox: intent in, verified and traceable code out — and at every
step, you can see exactly why the machine thinks it's right.
