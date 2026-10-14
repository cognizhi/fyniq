# Fyniq

*Intelligence behind every number.*

> [!WARNING]
> **Work in progress: under construction, not ready for use.** Fyniq is being built in the open,
> phase by phase. Today the repository holds the specification and the first foundations (P0); the
> accounting, payroll and AI features described below are planned, not built. Do not use it for real
> books, payroll or tax filings. Statutory rules still await review by an accountant or tax agent,
> and every sample company in the repository is synthetic.

A web-hosted accounting, compliance, reporting and planning platform for a Singapore start-up,
on Google Cloud. Built for a lean founding team, with the controls a growing company needs.

- **Record** in plain words; bank feeds, receipt capture and e-invoices do most of the typing.
- **Account** with a double-entry engine whose invariants the database enforces.
- **Report** live, with drill-down from any number to its source and its full history.
- **Plan** with budgets, driver-based forecasts, 13-week cash and runway.
- **Control** with approvals, segregation of duties, period locks and a tamper-evident audit
  trail.
- **Ask and delegate to AI**: answers cite the books; AI may propose changes, and a named human
  approves each one.
- **Open by design, closed by default**: an API-first platform and a secure MCP server, so other
  systems and AI agents can integrate under the same controls as our own UI.
- **Basic and Expert modes**: founders see the essentials; accountants see every lever; one
  toggle switches between them.

## Status

**P0 (Foundation) in progress.** What exists today:

- The product specification and its OpenSpec specs; the design system, the screen designs and a
  clickable prototype; the user guide and the compiled knowledge base the AI assistant will read.
- The Go service skeleton and local mode: every service starts and answers health checks, with the
  `make` commands, the dev container and lean, hardened images.
- The database backstop: migrations, roles per service, row-level security on every table, the
  ledger's invariants enforced by PostgreSQL, the storage port, and the attack and race suites that
  prove them.

Next in P0: money and the posting engine, the independent Python oracle, the audit chain, background
work, the API conventions, CI images and releases, infrastructure, identity and controls as code
([plan](docs/prd/plans/p0-foundation-2026-10-14.md)).

| Area | Where |
|---|---|
| Product requirements (revision 0.22) | [`docs/prd/prd-001-fyniq-platform.md`](docs/prd/prd-001-fyniq-platform.md) |
| Nemura, the optional people platform (revision 0.6) | [`docs/prd/prd-002-nemura-people.md`](docs/prd/prd-002-nemura-people.md) |
| Requirements as OpenSpec specs, and the P0 changes | [`openspec/`](openspec/) |
| Design system (uipro-reviewed; tokens; contrast validator) | [`docs/design-system/fyniq/`](docs/design-system/fyniq/) |
| Screen designs | [`docs/design/screens/`](docs/design/screens/) |
| Clickable UI prototype | [`docs/design/prototype/index.html`](docs/design/prototype/index.html) (open in a browser; try `?mode=expert&theme=dark`) |
| User guide (source of the in-app help, §7.9) | [`docs/user-guide/`](docs/user-guide/) |
| Compiled knowledge base the AI assistant reads (§9.11) | [`kb/`](kb/) (index: [`kb/index.md`](kb/index.md)) |
| Database: migrations, roles, invariants, error codes | [`db/`](db/README.md) |
| Coding instructions | [`AGENTS.md`](AGENTS.md) (and [`CLAUDE.md`](CLAUDE.md) for Claude Code) |
| Agent skills | [`.claude/skills/`](.claude/skills/) (`fyniq-*`) |
| Release tooling | [`release-please-config.json`](release-please-config.json), [`.github/workflows/`](.github/workflows/) |

## Quick start

Needs Docker, GNU Make, git and [mise](https://mise.jdx.dev/) (it installs the pinned toolchain
in `mise.toml`), or the dev container in `.devcontainer/`. No cloud account is needed.

```bash
make setup        # the pinned toolchain, web dependencies, git hooks and a local .env
make dev          # PostgreSQL, Mailpit and fake GCS in Docker; migrations; the services and web app with hot reload
make test         # every merge gate, offline (the database suites run PostgreSQL 18 in Docker)
make up           # or: the stack from the hardened local images, behind Caddy on http://localhost:8080
make down
make help         # every target
```

## Architecture in one paragraph

Go services on Cloud Run that scale to zero, with background work through an outbox and Cloud Tasks; a React single-page app; PostgreSQL 18 on Cloud SQL as the system of
record, with the ledger invariants enforced in the database; an append-only, hash-chained audit
trail anchored to locked storage; Claude on Google Cloud's Agent Platform behind our own tool
layer, with an approval-gated proposal layer as the only AI write path; Ory Hydra as the OAuth 2.1
server for integrations and MCP clients; outside services as installable connectors; every cloud
service behind a port, so AWS and Azure stay open; OpenTofu for infrastructure; lean distroless
images.
Details and the decision records are in PRD §11–§12.

## Contributing

Read [`AGENTS.md`](AGENTS.md). In short:

- This is an early, fast-moving project: open an issue before a large pull request.
- Use Conventional Commits (`feat(ledger): …`, `fix(web): …`, `docs(prd): …`). CI checks them.
- [release-please](https://github.com/googleapis/release-please) turns them into versions, tags,
  GitHub releases and [`CHANGELOG.md`](CHANGELOG.md). Never bump versions by hand.
- Reference requirement IDs from the PRD in pull requests and tests.

## Licence

[MIT](LICENSE) © 2026 Cognizhi. The vendored design skills under `.claude/skills/` and the files
OpenSpec generates keep their own licences; see [`THIRD_PARTY_NOTICES.md`](THIRD_PARTY_NOTICES.md).
