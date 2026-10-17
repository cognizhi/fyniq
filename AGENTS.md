# AGENTS.md: coding instructions for Fyniq

These instructions are for every contributor, human or AI coding agent (Claude Code, Codex,
Copilot, Cursor, …). `CLAUDE.md` imports this file. When the instructions and the PRD disagree,
the PRD wins; fix whichever is wrong in the same change. The OpenSpec specs in `openspec/` and the
screen designs in `docs/design/screens/` are derived from the PRD: a PRD change updates them in the
same commit (`fyniq-openspec` skill; `make check` fails on drift).

## What this repository is

**Fyniq**, *Intelligence behind every number*: a web-hosted accounting, compliance, reporting
and planning platform for a Singapore start-up, on Google Cloud (`asia-southeast1`). It replaces
the `zh-sheet-accounting` Zoho Sheet workbook and keeps its accounting rules, statuses, checks and
test philosophy. The line is a promise the code keeps: every figure traces to its source, and AI
states where each number comes from (golden rules 3 and 5).

**Status: P0 (Foundation) in progress.** Besides the product spec, the design system, the agent
skills and the release tooling, the repository has the Go service skeleton, local mode and the web
shell (p0-repo-skeleton-local-mode), and the database backstop with the storage port
(p0-database-tenancy-migrations: `db/`, `internal/ports/storage`, `internal/adapters/postgres`). The rest of P0 is planned in `openspec/changes/p0-*` and
`docs/prd/plans/p0-foundation-2026-10-14.md`; P1, P2 and P3 in
`docs/prd/plans/p1-p3-build-plan-2026-10-16.md` (accepted by the founders on 16 Oct 2026; PRD-001 rev 0.25).

| Read first | Why |
|---|---|
| `docs/prd/prd-001-fyniq-platform.md` | The requirements (IDs such as `FR-GL-08`, `AI-34`, `DEV-02`) and the architecture decisions (ADR index in §12.0) |
| `docs/prd/prd-002-nemura-people.md` | **Nemura**, the optional people platform (its own image, database and sign-in; linked to Fyniq by one sign-in and the Finance \| People switch): records, leave, payroll, self-service, letters, later hiring, performance and onboarding (IDs such as `NM-LV-03`); Fyniq's side is PRD-001 §8.26 |
| `docs/prd/reviews/prd-001-review-2026-10-12.md` | Why revision 0.2 changed what it did (REV-01…40) |
| `docs/prd/reviews/prd-001-deep-review-validation-2026-10-13.md` | Why revision 0.8 changed what it did: the deep review (F01–F50, O01–O12) validated claim by claim |
| `docs/prd/reviews/prd-001-002-deep-review-validation-2026-10-14.md` | Why PRD-001 revision 0.16 and PRD-002 revision 0.2 changed what they did: the second deep review (F2, X2, N2 findings) validated against both PRDs and the official MOM, MSF, CPF Board and OpenAI pages |
| `docs/design-system/fyniq/MASTER.md` | How the UI looks and behaves; `pages/*.md` override it per page |
| `docs/design-system/nemura/MASTER.md` | Nemura's own look: professional, modern, with depth (tokens, type, components); behaviour follows Fyniq's MASTER |
| `openspec/specs/` | The PRD's requirements as OpenSpec specs, one capability per file (`openspec list --specs`); derived from the PRD, which stays the source of truth |
| `docs/design/screens/` | Every screen: the inventory, the kit and the fragments behind the Claude Design canvases (URLs in `canvases.json`) |
| `.claude/skills/fyniq-*/SKILL.md` | Step-by-step recipes for each kind of change (table below) |

## Commands

| Command | Does | Status |
|---|---|---|
| `make help` | Lists the targets | Today |
| `make check` | Every check available today: design-token contrast in four themes, release configuration, the knowledge-base lint, the OpenSpec specs against the PRD, the P1–P3 plan against the specs, the screen inventory and fragments, the review register, the scan waivers and the scan, image and infra rules on planted findings | Today (CI runs it) |
| `make hooks` | Installs the Conventional Commit check as a git `commit-msg` hook | Today: run once per clone |
| `make lint-commits` | Checks the commit messages in `RANGE` (default `origin/main..HEAD`) | Today |
| `make deps-up` / `deps-down` / `deps-logs` | PostgreSQL 18 (port 55432), Mailpit (8025) and fake GCS (4443) in Docker | Today |
| `make prototype` | Prints the path of the static UI prototype | Today |
| `make kb-index` / `kb-lint` / `kb-stale` | Regenerate the knowledge-base index; lint the compiled pages (part of `make check`); list stale pages and uncovered sources (§9.11) | Today |
| `make roadmap` / `roadmap-check` | Regenerate the change tables, coverage and counts of the P1–P3 plan (`docs/prd/plans/p1-p3-build-plan-2026-10-16.md`) from the specs and the screen inventory / check that every requirement and screen after P0 sits in exactly one planned change and the tables are current (part of `make check`); a new P2 or P3 requirement needs its ID in `scripts/roadmap.py` | Today |
| `make spec-check` / `spec-map` / `spec-stale` | OpenSpec specs valid (strict), covering every PRD anchor once and matching the PRD rows (part of `make check`); the PRD → capability map; PRD rows changed since conversion | Today |
| `make openapi` | Regenerate the API contract `contracts/openapi/fyniq.v1.yaml` from the code (huma) and the web app's typed client from it; `make test` fails while the code, the contract and the client differ, so the contract is reviewed in every pull request that changes it | Today |
| `make setup-code` | Create the one-time first-run setup code for the local database and print it once (FR-ADM-06); `POST /v1/setup` with it makes the caller the Super admin and Owner. Refused once setup is done | Today |
| `make super-admin-recovery` | The deployment's two-person recovery for a Super admin on the local database: `USER_ID=<user id> OPERATORS="<name>, <name>"` prints a one-time code, valid 24 hours (FR-ADM-09; `docs/runbooks/super-admin-recovery.md`) | Today |
| `make controls` | The control registry: one file per PRD-001 Appendix F control in `compliance/controls/`, with the PRD's text (`scripts/controls.py sync` rewrites it after a PRD change), an owner, and how each frequency is checked or when it arrives; part of `make check`, and the Go test checks every named target exists | Today |
| `make scan` | The security scans (SEC-21): gosec, Semgrep, gitleaks, grype, Checkov, licences, actionlint and zizmor; a blocking finding fails unless `compliance/scan-waivers.toml` waives it with a reason and an expiry; reports in `.data/scan/` | Today (CI runs it) |
| `make api-check` | The contract passes Spectral (NFR-API-01) and the web app's generated client is current; part of `make test`, which also runs `oasdiff` against the last release (QA-API-02) | Today |
| `make design-kit` / `design-check` / `design-status` | Regenerate the screen kit stylesheet; lint the screen inventory and fragments (part of `make check`); screens designed per canvas | Today |
| `make review-check` / `review-status` | The review register (`docs/prd/reviews/review-status.json`) names real requirements and screens and no excluded page (part of `make check`); the areas awaiting second review and the pages that show the "Needs additional verification" banner (FR-UX-41) | Today |
| `make setup` / `dev` / `up` / `down` | Install the pinned toolchain (`mise.toml`), the web dependencies and hooks; run the stack with hot reload (dependencies in Docker, services and Vite on the host); run it from the local images behind Caddy on `localhost:8080` / stop it | Today (Hydra, and a local sign-in page instead of an emulator: open `localhost:8080` and sign in) |
| `make seed` / `reset` | Write the sample company to `.data/seed/` / delete local data and the database volume | Today |
| `make seed-load` | Load the sample company into an organisation of the local database (`ORG=<organisation id>`, from `GET /api/v1/session`): its chart, the 2026 periods and its journals, posted through the engine and audited; refuses an organisation that has a chart, and checks the trial balance against the seed's own totals (QA-LOCAL-01) | Today |
| `make mutation` | Mutation tests (gremlins) on money, the rounding policies and the posting engine: at least 90% efficacy per package (QA-ENG-04); part of `make ci` and its own CI job, not of `make test` | Today |
| `make db-lint` / `sqlc` | squawk on the migrations and a check that the adapter's generated queries are current (part of `make test`) / regenerate them | Today |
| `make differential` | The full differential run (QA-ENG-02): Hypothesis searches 1,000 generated scenarios for one where the Go engine and the oracle disagree, shrinks it and saves it as a golden case; part of `make ci` and its own CI job. A fixed-seed sample and every golden case run in `make test` | Today |
| `make oracle-test` | The independent Python oracle's own tests (QA-ENG-01): hand-worked journals, the scenario schemas in `test/differential/`, Hypothesis properties (part of `make test`; `ORACLE_HYPOTHESIS_PROFILE=thorough` runs more examples) | Today |
| `make test` / `test-e2e` / `evals` / `images` / `ci` | Every merge gate offline (migrations, Go in both build flavours with the database suites in Testcontainers, so Docker must run; web typecheck; the oracle; `make check`) / the Playwright smoke test / the AI evals in replay (suites arrive in P1) / build the images (`FLAVOURS=release local`) and check them against `deploy/docker/budgets.json` and the §12.9 hardening (non-root, no shell, no local adapter in release) / what CI runs, in order, one CI job per step | Today (gates grow with each P0 change) |
| `make infra-check` | The OpenTofu code in `infra/` without a cloud account (NFR-PORT-05): `tofu fmt`, `tofu init` from the committed lock files, `tofu validate` and each module's `tofu test` on mock providers, then the rules no plan shows (no service-account or HMAC key, no authoritative IAM, no role granted to a person or group, no region outside Singapore). Downloads the providers, so it is its own CI job and not part of `make test`; Checkov scans `infra/` in `make scan`. Plans against the cloud wait for the founder's organisation (`infra/README.md`) | Today (CI runs it) |
| `make image-scan` | An SPDX SBOM (syft) and a grype scan of each release image; a critical finding fails unless waived. `scripts/local-smoke.sh` is QA-LOCAL-01: `make up` from the local images with no cloud credentials, then smoke checks through Caddy | Today (CI runs both) |
| `make image-sign` | After `make image-scan`: pushes each release image by digest to a throwaway local registry, signs it and attaches its SBOM as an attestation with cosign (a key pair made for the run), and verifies both; a wrong key and an unsigned image must fail (NFR-IMG-05, SEC-22). Evidence in `.data/images/signatures.json`. The release workflow signs keyless with GitHub OIDC and pushes to Artifact Registry (with p0-infra-opentofu) | Today (CI runs it) |

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
   in §9.6 is enforced by tests. Figures come only from read tools; know-how comes from the
   compiled knowledge base in `kb/` (§9.11). **Every change to `docs/user-guide/` is compiled,
   indexed and linted in the same commit** (`fyniq-knowledge-base` skill; `make check` fails otherwise).
6. **Closed by default.** MCP access, integration clients and AI task types are off until an
   Owner enables them with dual control. Agent-class clients are proposal-only (AI-34). Tokens are
   audience-bound and never passed through (AI-36).
7. **Every saved change is audited** in the same transaction (AUD-01), with actor,
   on-behalf-of, reason and before/after values. Logs never contain secrets, tokens, full NRIC
   or bank numbers (SEC-15). Important activities carry **evidence** (screenshots, files) under
   the evidence rules, append-only once approved (FR-DOC-05…07).
8. **Tenancy on every row.** `organisation_id` everywhere, set per transaction with
   `set_config('app.org_id', …, true)`, and `FORCE ROW LEVEL SECURITY` on every table (§11.4).
9. **Local mode always works; the cloud is an adapter.** Every cloud dependency sits behind a
   port with a local adapter, and cloud SDKs are imported only in `internal/adapters/<provider>/`
   (ADR-T8). Outside services are connectors (`connectors/`). Background work goes through the
   outbox and Cloud Tasks, so nothing has to stay running (ADR-T7). `make test` passes offline. Local adapters never reach staging or production
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
docs/user-guide/     in-app user guide (§7.9)  kb/              compiled knowledge base (§9.11)
openspec/            specs derived from the PRD  docs/design/screens/  screen inventory, kit, canvases (§7.10)
agents/              scheduled-agent skills (§9.12), e.g. agents/company-secretary/SKILL.md, agents/people-assistant/SKILL.md
cmd/nemura/, apps/nemura-web/   Nemura's own image and web app (PRD-002 ADR-N1; arrives with Nemura's P1)
connectors/          connector manifests and code (FR-INT-25)  internal/adapters/  cloud adapters (ADR-T8)
.claude/skills/      agent skills            .github/workflows/  CI and release-please
```

## Commits, pull requests and releases (PRD §12.10)

- **Conventional Commits** (REL-01): `<type>(<scope>)<!>: <imperative summary ≤ 100 chars>`.
  - Types: `feat` `fix` `perf` `refactor` `docs` `test` `build` `ci` `chore` `revert` `deps`.
  - Scopes: `ledger` `engine` `api` `web` `ui` `ai` `mcp` `oauth` `worker` `executor` `jobs` `db`
    `infra` `deploy` `contracts` `oracle` `packs` `compliance` `security` `design` `docs` `prd`
    `kb` `specs` `skills` `ci` `release` `deps`. Several are allowed: `fix(api,web): …`.
  - A breaking change gets `!` and a `BREAKING CHANGE: …` footer. Anything users, operators or
    integrators must do goes in an `Upgrade notes:` paragraph in the body (REL-03).
- Work on a branch and open a pull request. The **pull request title** must be a Conventional
  Commit, because squash merges use it. CI checks titles and commits.
- **Before production** (founder decision, 14 Oct 2026): the owner's development pull requests are
  approved by `.github/workflows/auto-approve.yml` and merge by themselves (auto-merge, squash) once
  the required checks pass. Release pull requests, the specialist reviews in `docs/prd/reviews/` and
  anything labelled `needs-human-review` still wait for a person. Before the first production deploy,
  remove the workflow, require a human approval and enforce it for admins (CTL-CHG-01).
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
   `make up` still starts (`make test` includes `make check`).

## Skills (`.claude/skills/`)

| Skill | Use for |
|---|---|
| `fyniq-accounting-domain` | Posting rules, invariants, money and rounding, the balance service, the oracle, imbalances |
| `fyniq-sg-statutory` | GST, CPF, SDL, SHG, CIT, InvoiceNow and deadline updates as effective-dated packs |
| `fyniq-feature-change` | Adding a module, entity, field, report, check, KPI or permission end to end |
| `fyniq-quality-gates` | The test pyramid, every merge gate, differential and property tests, traceability |
| `fyniq-bug-fix` | Triage → reproduce → failing test → fix → data correction by approved journal |
| `fyniq-api-integrations` | API conventions, contracts, webhooks and events, OAuth client classes, SDKs |
| `fyniq-mcp-server` | Exposing tools over MCP safely; the MCP security controls and red-team suite |
| `fyniq-ai-proposals` | AI read tools and catalogue task types, dry runs, evals, red-team cases |
| `fyniq-security-compliance` | Controls as code, evidence, ASVS, PDPA, incident response |
| `fyniq-design-system` | Tokens, themes, components, Basic/Expert mode, accessibility, charts; consulting uipro |
| `fyniq-local-dev` | Local mode, the Compose stack, local substitutes, lean images and budgets |
| `fyniq-release` | Conventional Commits, release-please, versioning matrix, PRD revisions |
| `fyniq-knowledge-base` | Compile, index and lint the knowledge base the AI assistant reads (`kb/`); run it on every change to `docs/user-guide/` |
| `fyniq-openspec` | Convert a PRD or PRD revision into OpenSpec specs and keep them in step; design every new or changed screen in Claude Design (five frames, light and dark); the OpenSpec CLI and the twelve `/opsx` workflows |

The twelve `openspec-*` skills and the `/opsx:*` commands (propose, explore, new, continue, ff,
apply, update, verify, sync, archive, bulk-archive, onboard) are generated by `openspec init` and
`openspec update`; never edit them by hand.

Design skills that ship with the repository (`ui-ux-pro-max`, also called **uipro**;
`ui-styling`; `design-system`; `design`; `brand`; `banner-design`; `slides`) support
`fyniq-design-system`. uipro is a review lens; it never overrides the PRD or MASTER.md.

## Working as an AI agent: sub-agents

Sub-agents burn tokens fast. Each one (Claude Code's Agent and Workflow tools, Codex tasks or any
parallel agent) builds its own context from scratch. While it runs, the main session sits idle,
and after five minutes its prompt cache expires, so the next turn pays to re-read everything.

- Do the work in the main session by default: conversions, screen batches, PRD revisions and
  reviews included.
- Start a sub-agent only for a self-contained task that finishes in **under about five
  minutes**, such as one broad search whose conclusion is all you need.
- Never fan out long work to save wall-clock time. If a task would really benefit from parallel
  agents, say so, give a rough cost and let the person decide.

## Safety

- Never commit secrets, `.env` files, keys or real customer, employee or bank data. Sample data is
  synthetic.
- Do not push to `main` directly once branch protection is on (CTL-CHG-01); until then, keep
  direct pushes to repository bootstrap work.
- Tasks that touch money movement, payee bank details, approvals, the audit trail, tenancy or
  the AI/MCP write path need a second human reviewer, whoever wrote the code. (Waived before
  production by the founders on 14 Oct 2026: such pull requests are auto-approved like any other
  development change; label one `needs-human-review` to hold it for a person. The rule returns
  before the first production deploy.)
