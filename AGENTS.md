# AGENTS.md: coding instructions for Cognizhi Finance Hub Cloud

These instructions are for every contributor, human or AI coding agent (Claude Code, Codex,
Copilot, Cursor, …). `CLAUDE.md` imports this file. When the instructions and the PRD disagree,
the PRD wins; fix whichever is wrong in the same change.

## What this repository is

A web-hosted accounting, compliance, reporting and planning platform for a Singapore start-up,
on Google Cloud (`asia-southeast1`). It replaces the `zh-sheet-accounting` Zoho Sheet workbook
and keeps its accounting rules, statuses, checks and test philosophy.

**Status: pre-P0.** The repository holds the product spec, the design system, the agent
skills, the local dependency stack and the release tooling. No application code exists yet.
P0 (Foundation) builds the skeleton described in PRD §12 and §17.3.

| Read first | Why |
|---|---|
| `docs/prd/prd-001-finance-hub-cloud-platform.md` | The requirements (IDs such as `FR-GL-08`, `AI-34`, `DEV-02`) and the architecture decisions (ADR index in §12.0) |
| `docs/prd/reviews/prd-001-review-2026-10-12.md` | Why revision 0.2 changed what it did (REV-01…40) |
| `docs/design-system/cognizhi-finance-hub/MASTER.md` | How the UI looks and behaves; `pages/*.md` override it per page |
| `.claude/skills/fh-*/SKILL.md` | Step-by-step recipes for each kind of change (table below) |

## Commands

| Command | Does | Status |
|---|---|---|
| `make help` | Lists the targets | Today |
| `make check` | Every check available today: design-token contrast in four themes, release configuration | Today (CI runs it) |
| `make hooks` | Installs the Conventional Commit check as a git `commit-msg` hook | Today: run once per clone |
| `make lint-commits` | Checks the commit messages in `RANGE` (default `origin/main..HEAD`) | Today |
| `make deps-up` / `deps-down` / `deps-logs` | PostgreSQL 18 (port 55432), Mailpit (8025) and fake GCS (4443) in Docker | Today |
| `make prototype` | Prints the path of the static UI prototype | Today |
| `make setup`, `dev`, `up`, `down`, `seed`, `reset`, `test`, `test-e2e`, `evals`, `images`, `ci` | The local-mode command contract (PRD §12.8): the full stack and every merge gate, offline | **Arrives in P0** |

Never invent a command in docs or scripts. If you need a new one, add it to the Makefile with a
`## description`, and to this table.

## Golden rules

1. **No floating-point money, anywhere** (ENG-10…14). Use exact decimals with a currency;
   `NUMERIC(20,4)` with the currency's scale in the database; decimal **strings** in JSON;
   integer minor units or a decimal library in TypeScript. Every rounding goes through a named
   policy (`RND-*`, `ALLOC-*`, §10.4).
2. **The database is the backstop.** Balanced entries, immutable posted history, locked periods
   and tenant isolation are enforced by constraints, triggers, grants and row-level security
   (INV-01…13). Never weaken one to make a test pass. Correct posted history by **reversal**,
   never by update or delete.
3. **Two implementations must agree.** The Go engine and the independent Python oracle
   (`oracle/`, written from the rules, not translated from the engine) must match to the cent.
   Every visible number is verified by a test, and every ✖/⚠ message is triggered by a fixture
   (QA-ENG-02, QA-RPT-01/02).
4. **API-first.** Every capability is an operation in `contracts/openapi/` before it is a screen.
   The web app calls only the generated client: no private endpoints (QA-API-03). Inside `/v1`,
   changes are additive (FR-INT-06), errors are RFC 9457 with stable codes, writes accept
   `Idempotency-Key`, and versioned records use `ETag` / `If-Match` (§8.18.1).
5. **AI proposes; humans decide.** AI services and MCP clients hold no write credential. The only
   AI write path is a proposal from the approved catalogue, approved by a named human in the web
   app. Never add a write, approve, export or admin tool to the AI or MCP surface; the never-list
   in §9.6 is enforced by tests.
6. **Closed by default.** MCP access, integration clients and AI task types are off until an
   Owner enables them with dual control. Agent-class clients are proposal-only (AI-34). Tokens are
   audience-bound and never passed through (AI-36).
7. **Every saved change is audited** in the same transaction (AUD-01), with actor,
   on-behalf-of, reason and before/after values. Logs never contain secrets, tokens, full NRIC
   or bank numbers (SEC-15). Important activities carry **evidence** (screenshots, files) under
   the evidence rules, append-only once approved (FR-DOC-05…07).
8. **Tenancy on every row.** `organisation_id` everywhere, set per transaction with
   `set_config('app.org_id', …, true)`, and `FORCE ROW LEVEL SECURITY` on every table (§11.4).
9. **Local mode always works.** Every cloud dependency sits behind an interface with a local
   adapter. `make test` passes offline. Local adapters never reach staging or production
   (DEV-07, SEC-16). Never copy production data to a laptop (DEV-08).
10. **Lean, hardened images.** Static Go binaries on `distroless/static:nonroot`, non-root,
    read-only root filesystem, no shell, base images pinned by digest, within the size budget
    (§12.9).
11. **UI rules** (MASTER.md): semantic tokens only; brand teal for actions, **violet only for
    AI content**; Lucide icons with text labels (no emoji as icons); every feature tagged
    `basic` or `expert`, and safety information never hidden in Basic mode (§7.8); WCAG 2.2 AA;
    no optimistic updates of ledger figures; the clipboard gets ASCII minus and plain decimals.
    Every feature has **help**: a help key with tooltip text, a page in `docs/user-guide/` and,
    on the first-run path, a tour step (§7.9, QA-UI-05).
12. **Statutory values come from official sources**, as effective-dated packs with a source URL
    and pinned tests (§10.6). A `[verify]` tag in the PRD means: confirm it before you build on it.
13. **Plain words.** Every user-facing message names the field, says what is wrong and how to
    fix it, and has an error code (ENG-23).
14. **Trace to requirements.** Reference requirement IDs in tests (`// req: FR-GL-08`) and in
    pull requests. Missing `M` requirements fail the phase gate (QA-TRACE-01).

## Repository layout (PRD §12.4)

```
apps/web/            React SPA (P0)          contracts/       OpenAPI, AsyncAPI, AI tool schemas (P0)
cmd/<service>/       Go entry points (P0)    internal/        Go domain, ledger, adapters (P0)
db/migrations/       goose SQL (P0)          oracle/          Python reference ledger (P0)
packs/               statutory packs (P1)    compliance/      control registry as code (P0)
deploy/local/        Compose stack           deploy/docker/   Dockerfiles (P0)
infra/               OpenTofu (P0)           scripts/         hooks and repo scripts
docs/prd/            PRD, reviews, research  docs/design-system/, docs/design/  UI system and prototype
docs/user-guide/     in-app user guide (§7.9)
.claude/skills/      agent skills            .github/workflows/  CI and release-please
```

## Commits, pull requests and releases (PRD §12.10)

- **Conventional Commits** (REL-01): `<type>(<scope>)<!>: <imperative summary ≤ 100 chars>`.
  - Types: `feat` `fix` `perf` `refactor` `docs` `test` `build` `ci` `chore` `revert` `deps`.
  - Scopes: `ledger` `engine` `api` `web` `ui` `ai` `mcp` `oauth` `worker` `executor` `jobs` `db`
    `infra` `deploy` `contracts` `oracle` `packs` `compliance` `security` `design` `docs` `prd`
    `skills` `ci` `release` `deps`. Several are allowed: `fix(api,web): …`.
  - A breaking change gets `!` and a `BREAKING CHANGE: …` footer. Anything users, operators or
    integrators must do goes in an `Upgrade notes:` paragraph in the body (REL-03).
- Work on a branch and open a pull request. The **pull request title** must be a Conventional
  Commit, because squash merges use it. CI checks titles and commits.
- **release-please** owns `VERSION`, `.release-please-manifest.json`, `CHANGELOG.md`, the tags
  and the GitHub releases. **Never edit them by hand** (REL-02). While the version is `0.x`, a
  `feat` bumps the minor version.
- Commit trailers and AI attribution follow the tool's own rules and the repository settings.

## Definition of done (PRD §16.3)

1. The requirement ID is referenced; acceptance criteria were written before the code.
2. Tests at the right levels, including a regression test for every bug; all gates in §16.2 are
   green; coverage stays at 100% where required.
3. Security checklist and threat model updated if money movement, personal data, permissions,
   the API surface or the AI/MCP surface changed.
4. Audit events are emitted and verified for every new state change.
5. Docs are updated (PRD with a `(rev 0.N)` tag, user guide in `docs/user-guide/` with its help
   keys, API reference, runbook, skills).
6. Conventional Commit with upgrade notes where needed. `make test` passes offline and
   `make up` still starts (from P0). Today, `make check` passes.

## Skills (`.claude/skills/`)

| Skill | Use for |
|---|---|
| `fh-accounting-domain` | Posting rules, invariants, money and rounding, the balance service, the oracle, imbalances |
| `fh-sg-statutory` | GST, CPF, SDL, SHG, CIT, InvoiceNow and deadline updates as effective-dated packs |
| `fh-feature-change` | Adding a module, entity, field, report, check, KPI or permission end to end |
| `fh-quality-gates` | The test pyramid, every merge gate, differential and property tests, traceability |
| `fh-bug-fix` | Triage → reproduce → failing test → fix → data correction by approved journal |
| `fh-api-integrations` | API conventions, contracts, webhooks and events, OAuth client classes, SDKs |
| `fh-mcp-server` | Exposing tools over MCP safely; the MCP security controls and red-team suite |
| `fh-ai-proposals` | AI read tools and catalogue task types, dry runs, evals, red-team cases |
| `fh-security-compliance` | Controls as code, evidence, ASVS, PDPA, incident response |
| `fh-design-system` | Tokens, themes, components, Basic/Expert mode, accessibility, charts; consulting uipro |
| `fh-local-dev` | Local mode, the Compose stack, local substitutes, lean images and budgets |
| `fh-release` | Conventional Commits, release-please, versioning matrix, PRD revisions |

Design skills that ship with the repository (`ui-ux-pro-max`, also called **uipro**;
`ui-styling`; `design-system`; `design`; `brand`; `banner-design`; `slides`) support
`fh-design-system`. uipro is a review lens; it never overrides the PRD or MASTER.md.

## Safety

- Never commit secrets, `.env` files, keys or real customer, employee or bank data. Sample data is
  synthetic.
- Do not push to `main` directly once branch protection is on (CTL-CHG-01); until then, keep
  direct pushes to repository bootstrap work.
- Tasks that touch money movement, payee bank details, approvals, the audit trail, tenancy or
  the AI/MCP write path need a second human reviewer, whoever wrote the code.
