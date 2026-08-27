# CodeDistill — Community Edition

Every developer keeps notes on their projects — scratch files, a doc folder, a running list of "things to fix." The trouble is they live *beside* the code instead of *in* it, so they drift out of sync and lose their relevance over time. And as more and more software gets written **through LLMs**, that gap turns expensive: the *intent* behind a change — why it was made, what it was meant to satisfy — is exactly what a model (and the next human) needs, and exactly what evaporates into a side file. CodeDistill is the IDE rethought for that world: you capture intent, an agent turns it into code, and the two are **distilled together** into something that stays **verified and traceable** instead of drifting apart.

In practice: paste anything — stack traces, ideas, half-finished thoughts — onto a scratchpad canvas, and a local LLM sorts it into todos, bugs, knowledge-base entries, and use cases, each anchored to the project it belongs to. Your data never leaves your machine: classification runs against your own [Ollama](https://ollama.com) instance, and storage is a single SQLite file.

## Community vs. commercial

This is the **Community Edition** (AGPL-3.0): the full scratchpad + canvas experience with AI classification, acceptance criteria, code metrics, dashboards, theming, an installable PWA, MCP **reads**, and a **baseline code scan** (runs your installed analyzers and shows the findings, which you can push onto a scratchpad). The commercial edition adds **MCP write tools, code anchors, duplicate intelligence, and alternative views (List/Calendar/Kanban)** — and at **Enterprise**, **multi-user workspaces, S3 storage, the analysis pipeline (ingest your CI/security tools' SARIF + auto-route findings), and a governance gate that blocks completion on a high-severity security finding.** See https://codedistill.dev for the commercial editions.

## Quick start

Requirements: Go 1.25+, a running Ollama with `qwen2.5:7b` and `nomic-embed-text` pulled. (Node 20+ only if you rebuild the UI.)

    ./build.sh          # builds ./codedistill (community edition)
    ./codedistill serve # UI on http://localhost:8080

## License

GNU Affero General Public License v3.0 — see [LICENSE](LICENSE). The commercial edition is licensed separately and is not covered by this repository's license.

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md) — contributions require a CLA.
