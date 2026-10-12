# Cognizhi Finance Hub Cloud

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

**Pre-P0: specification and foundations.** No application code yet.

| Area | Where |
|---|---|
| Product requirements (revision 0.3) | [`docs/prd/prd-001-finance-hub-cloud-platform.md`](docs/prd/prd-001-finance-hub-cloud-platform.md) |
| Review that produced revision 0.2 | [`docs/prd/reviews/prd-001-review-2026-10-12.md`](docs/prd/reviews/prd-001-review-2026-10-12.md) |
| Design system (uipro-reviewed; tokens; contrast validator) | [`docs/design-system/cognizhi-finance-hub/`](docs/design-system/cognizhi-finance-hub/) |
| Clickable UI prototype | [`docs/design/prototype/index.html`](docs/design/prototype/index.html) (open in a browser; try `?mode=expert&theme=dark`) |
| User guide (source of the in-app help, §7.9) | [`docs/user-guide/`](docs/user-guide/) |
| Compiled knowledge base the AI assistant reads (§9.11) | [`kb/`](kb/) (index: [`kb/index.md`](kb/index.md)) |
| Coding instructions | [`AGENTS.md`](AGENTS.md) (and [`CLAUDE.md`](CLAUDE.md) for Claude Code) |
| Agent skills | [`.claude/skills/`](.claude/skills/) (`fh-*`) |
| Release tooling | [`release-please-config.json`](release-please-config.json), [`.github/workflows/`](.github/workflows/) |

## Quick start (today)

Needs Docker, GNU Make, Python 3 and git.

```bash
make hooks        # Conventional Commit check on every commit
make check        # design-token contrast in four themes, release configuration, knowledge-base lint
make deps-up      # PostgreSQL 18 (localhost:55432), Mailpit (localhost:8025), fake GCS (localhost:4443)
make prototype    # path of the UI prototype
make deps-down
```

From P0, `make setup && make dev` starts the whole platform with the sample company, and
`make test` runs every merge gate, with no cloud account (PRD §12.8).

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

- Use Conventional Commits (`feat(ledger): …`, `fix(web): …`, `docs(prd): …`). CI checks them.
- [release-please](https://github.com/googleapis/release-please) turns them into versions, tags,
  GitHub releases and [`CHANGELOG.md`](CHANGELOG.md). Never bump versions by hand.
- Reference requirement IDs from the PRD in pull requests and tests.

## Licence

Proprietary. © Cognizhi. All rights reserved. The design skills under `.claude/skills/` keep their
own licences where they include one.
