# Design

## Context

What exists (checked on `main`, 18 Nov 2026):

- **Services.** One Go module, `github.com/cognizhi/fyniq`. Each `cmd/<service>` is its own binary and
  image. All use `internal/app` (start-up checks, the clock, the HTTP shell with `/healthz`, `/readyz`,
  `/version`, and `healthcheck` as the first argument, because distroless has no shell).
  `deploy/docker/Dockerfile.go-service` builds any of them from `--build-arg SERVICE=<name>`.
  `internal/runtime` refuses a release binary outside staging and production and a local binary there.
- **Image rules.** `GO_IMAGES` in the Makefile, `deploy/docker/budgets.json` and the table of PRD-001 §12.9
  are tied together by `TestImageBudgetsAreThePRDs` (the image name matches `[a-z]+`).
  `scripts/images.py` reads the budgets, so a new budget puts the image into the checks, the scan, the
  signing, the reproducibility run and the release publish. Compose services must be hardened (read-only,
  user 65532, no capabilities) and must not share the data volume (only api, worker, executor do).
  `.dockerignore` whitelists paths: a new top-level path needs a line there and, if the build copies it,
  one in the Dockerfile. #557 (the scanner service) is the template for a new image.
- **Database.** One PostgreSQL 18 database `fyniq`, with group roles `fyniq_*`, goose migrations in
  `db/migrations/` embedded by `db/embed.go`, the runner in `internal/adapters/postgres/migrate.go` (its goose
  table is `fyniq_migrations.goose_db_version`, and it loads Fyniq's statutory packs afterwards), local
  bootstrap in `bootstrap_local.go` (it also creates the `hydra` database for Ory Hydra, the precedent for a
  second database on the same server), schema and grants golden files, squawk over `db/migrations/*.sql`,
  sqlc over the same directory. `archtest` lets only `internal/adapters/postgres` (and sub-packages) import
  pgx, goose and `database/sql`; only `store.go` there may begin or end a transaction.
- **Identity.** Ory Hydra (opaque tokens, PKCE only) with its own database; the API serves Hydra's login and
  consent pages at `/api/auth/…`; the `SignIn` port has a local adapter (a sign-in page) and an Identity
  Platform adapter; sessions are HTTP-only cookies; passkeys are WebAuthn.
- **Audit.** `internal/domain/audit` defines the AUD-01 event, its canonical JSON, masking (SEC-15) and
  sealing of personal values (§11.8). The database chains events per organisation under the chain head's lock
  (`db/migrations/000006`), and `contracts/audit/` publishes the format.
- **Nemura so far.** Specs under `openspec/specs/people/`, 37 screens designed in
  `docs/design/screens/` (the canvas `nemura-*`), `docs/design-system/nemura/` (MASTER.md and
  `tokens.css`), and no code, no contract and no user guide.

## Goals / Non-Goals

**Goals:**
- Nemura deploys, signs people in and keeps its own audited, tenant-isolated data with **no Fyniq running**
  (NM-ARC-01), and a later Nemura change adds a record type, a rule or a screen without touching plumbing.
- Every slice ends with `make test` green and the image checks passing, and Fyniq's own tests and image
  checks are not weakened for it.
- The boundaries of ADR-N4 are enforced by tests, not by habit: neither app writes the other's tables, and
  Nemura's code does not reach into Fyniq's ledger or API packages.

**Non-Goals:**
- Records, leave, packs, payroll, letters (the other Nemura changes), the people assistant's runtime and
  tools, the cross-app link, its events and the single sign-in across both apps
  (`p1-nemura-fyniq-link`), and grievances (NM-SEC-06, P2).
- Cloud deployment (the cloud gate); the OpenTofu code is written and validated offline here, never applied.
- Selling Nemura on its own (PRD-002 question 8: internal for now). The standalone design stays.

## Already decided (not reopened)

| Decision | Where | Effect here |
|---|---|---|
| One image `nemura` (Go API and worker, static web app), its own PostgreSQL database, same monorepo, `cmd/nemura/` and `apps/nemura-web/` | ADR-N1 | D2, D4, D11 |
| Standalone sign-in through the same identity provider, one Identity Platform tenant per organisation; a linked organisation shares Fyniq's tenant | ADR-N3, question 2 (16 Oct 2026) | D7; sharing the tenant is `p1-nemura-fyniq-link` |
| Nemura's own instance of the same authorisation-server build as its token issuer, so standalone needs nothing from Fyniq | question 11 | D7 |
| One passkey relying-party ID across both apps (the apps sit under one registrable domain) | question 10 | D7; the registry shared across apps is the link change's |
| Nemura's own design system; the same device classes, accessibility rules and AI marking | ADR-N6 | D11 |
| Everything Nemura P1 is built after Fyniq's go-live and before the first paid person; none of it is in Fyniq's launch subset | question 12 | D1 |
| Nemura is internal for now; whether it is sold, and at what price, is decided after six months of live use | question 8 | O1 and O2 are sized for an internal product |

## Decisions

- **D1. Dormant by construction.** The skeleton is in `main` but changes nothing Fyniq does or releases to
  production.
  - It runs only under the compose profile `people`. `make up` and `scripts/local-smoke.sh` start the `core`
    profile and are unchanged. `PEOPLE=yes make up` adds it.
  - No infra applies, no hostname, no tenant. The image is built, checked, scanned and signed with the others
    (D14) so its supply chain is proven early; it is deployed nowhere until the cloud gate.
  - Cost to Fyniq's pipeline: one more image per flavour and platform in the `images` jobs and the daily
    reproducibility run (about 1 to 2 minutes each). Accepted; see O4 for the release train.

- **D2. One binary, one image, the role chosen at start (NM-ARC-02, NM-ARC-06).** `cmd/nemura` runs the role
  named by `NEMURA_ROLE`: `api`, `worker`, `executor`, `assistant` or `migrate`. An unset or unknown role, or
  one not built yet, refuses to start with a stable code (`NEMURA-ROLE-01`) and a plain fix.
  - Compose and Cloud Run set the role per service, so each service has its own identity, database role,
    environment and scaling (NM-ARC-06) from one image and one digest.
  - The probe stays `/app healthcheck`, and `--version` too, as for every image; the role is environment, not
    an argument, so no probe or Dockerfile changes.
  - `/version` names the service `nemura-<role>`.
  - Settings that belong to the deployment keep their Fyniq names (`FYNIQ_ENV`, `FYNIQ_BUSINESS_DATE`, `PORT`),
    so `internal/runtime` and `internal/clock` are reused unchanged. Nemura's own settings are `NEMURA_*`
    (`NEMURA_ROLE`, `NEMURA_DATABASE_URL`, `NEMURA_PUBLIC_ORIGIN`, …).
  - Alternatives: one image per role like Fyniq (rejected: PRD-002 says one image; five budgets and five scans
    for one codebase); a role as the first argument (rejected: breaks `healthcheck`/`--version` and the shared
    compose probe).
  - Budget: the table of PRD-001 §12.9 gains `nemura` at ≤ 30 MB compressed, the same as the services, with
    the web app embedded (D11) and revised by a reviewed pull request if letters and payslips need more (the PDF
    path decides, in `p1-nemura-self-service-letters`).

- **D3. One Go module with enforced boundaries (ADR-N1, NM-ARC-05 "shared rules, not shared code paths").**
  - Layout: `cmd/nemura`; `internal/nemura/<area>` for domain, HTTP and jobs; `internal/adapters/postgres/nemura`
    for SQL (a sub-package of the PostgreSQL adapter, so the SQL rules already allow it); `db/nemura/` for
    migrations; `contracts/openapi/nemura/`; `apps/nemura-web`.
  - New archtest rules, written in the first slice and tightened as packages arrive:
    1. Code under `internal/nemura` and `cmd/nemura` imports only an allow-list of **shared infrastructure**
       (`internal/app`, `httpserver`, `runtime`, `clock`, `telemetry`, `logging`, `egress`, `version`,
       `internal/ports` for ports Nemura also needs: clock, queue, signer, blob store, `internal/domain/money`
       and `rounding`, and the audit event form in `internal/domain/audit`). Never the ledger, the API, the
       storage port of Fyniq, Fyniq's domain packages or its adapters' stores.
    2. Nothing outside `internal/nemura`, `cmd/nemura` and the shared allow-list imports `internal/nemura`.
    3. Only `internal/adapters/postgres/nemura` holds Nemura's SQL, and a second unit-of-work file there is
       the only place that begins or ends its transactions (the existing rule gains `nemura/store.go`).
    4. No `float32` or `float64` in money code of Nemura (the existing money rule extended to its packages).
  - Alternative: a second Go module (`nemura/go.mod`) with `go.work`. Rejected for now: the shared packages
    live under `internal/`, so they would have to move to an exported shared module first. ADR-N1 keeps "a
    separate repository once Nemura has its own team" open; the allow-list is exactly the list that would
    become that shared module. See O5.

- **D4. Its own database, migrations and roles (NM-ARC-02, golden rules 2 and 8).**
  - A database `nemura`, never Fyniq's tables or database. Roles are cluster-wide in PostgreSQL, so Nemura's are
    named `nemura_*`: the migrator `nemura_migrator` (owns every object), and the group roles `nemura_api`,
    `nemura_worker`, `nemura_executor`, `nemura_assistant` (inserts proposals only, later) and `nemura_readonly`.
    `CONNECT` on each database is revoked from `PUBLIC` and granted only to its own roles, and a test proves a
    `fyniq_*` role cannot connect to `nemura` and the reverse.
  - Migrations are a second stream, `db/nemura/migrations/NNNNNN_name.sql`, numbered from 000001, embedded by
    `db/nemura/embed.go`, applied by `NEMURA_ROLE=migrate`. They never use Fyniq's numbers, so the ranges the
    other changes claim in `db/migrations/` are untouched. A change that needs a Fyniq-side migration (the link)
    uses Fyniq's stream under its own range.
  - The runner is the existing goose provider, parameterised by a migration set (embedded files, directory, table
    `nemura_migrations.goose_db_version`), not a copy; `Migrate` keeps loading Fyniq's packs, Nemura's set loads
    none until its packs exist.
  - Tenancy as Fyniq: `organisation_id` first in every key and index, composite foreign keys, `ENABLE` and
    `FORCE ROW LEVEL SECURITY` with one policy on `nemura.current_org()`, set per transaction with
    `set_config('app.org_id', …, true)`; unset, it matches no row. Global tables have no grant and are read
    through views.
  - Local: the same Postgres container, with the `nemura` database and roles created by the local bootstrap
    beside `hydra` (`nemura_hydra` for Nemura's issuer, D7). The password is the local-only one.
  - Tooling: `make db-lint` lints both streams; `make sqlc` generates the Nemura queries from its own config;
    the schema and grants golden files, the catalogue, attack, tenant-isolation and grant suites are Nemura's
    own, in `internal/adapters/postgres/nemura/`. Reuse the existing test harness through `pgtest`.

- **D5. The audit chain, the same format (AUD-01, NM-SEC-01, NM-ARC-05).**
  - The unit of work writes an event for every saved change in the same transaction; the database chains them
    per organisation, `hash = SHA-256(prev_hash ‖ body)`, under the chain head's lock, as `db/migrations/000006`.
  - Event form, canonical JSON, masking and sealing are the shared `internal/domain/audit` code, with a Nemura
    schema id so a verifier knows which application wrote it. Reading restricted data is an event too (NM-SEC-02,
    AUD-05).
  - Verification, anchoring and the nightly check run through a Nemura storage port, with the anchors in a bucket
    of Nemura's own (the same signer and blob ports, a Nemura prefix and key).
  - Personal values in an event are sealed per person and financial year as PRD-001 §11.8 does for Fyniq; the
    local key adapter serves local mode and Cloud KMS the cloud (`[live-verify]`).
  - Events emitted by this change (AUD-01): `nemura.org.created`; `nemura.member.invited`,
    `nemura.member.joined`, `nemura.member.deactivated`; `nemura.role.requested`, `nemura.role.granted`,
    `nemura.role.revoked` (effective-dated); `nemura.approval.requested`, `.approved`, `.refused`, `.expired`;
    `nemura.session.started`, `.ended`, `.revoked`; `nemura.passkey.registered`, `.removed`;
    `nemura.client.created`, `.revoked`; `nemura.restricted.read` (one per read of a restricted field, with the
    right used and the reason); `nemura.access_request.opened`, `.reviewed`, `.answered`;
    `nemura.retention.reviewed`, `.deleted` (with the proof); `nemura.setting.changed`. None carries a secret, a
    token, an assertion's private material, a full NRIC or FIN, or a bank number (SEC-15).

- **D6. The outbox and the worker (ADR-T7, NM-NFR-03).** Background work goes through Nemura's own outbox table
  and a queue (the shared queue port: Cloud Tasks in the cloud, the local queue in local mode), so nothing has
  to stay running and Cloud Run scales to zero (NM-NFR-01). Tasks carry an audience-bound token for the
  `nemura-worker` service and never a pay or bank detail. The hand-off to Fyniq when Fyniq is down is the same
  outbox with a different consumer (the link change); this change proves that a queued item survives a stopped
  consumer. The scheduler tick calls a Nemura endpoint the same way Fyniq's does.

- **D7. Standalone identity and the token issuer (NM-ARC-01, NM-ARC-06, ADR-N3).**
  - Nemura runs its own Ory Hydra (same pinned image and settings: opaque tokens, PKCE only, access tokens at
    most 60 minutes) on its own database `nemura_hydra`, with its own migrate job. The API serves Hydra's login
    and consent pages at `/api/auth/…` of Nemura's origin. API clients and the integration client that the link
    change registers in Fyniq are Nemura's own clients, audience-bound.
  - The `SignIn` port is reused: the local adapter is a sign-in page; the Identity Platform adapter takes the
    tenant per organisation (`[live-verify]` against the real service; tested against its fake).
  - Sessions: an HTTP-only, `__Host-` cookie on Nemura's own origin, never Fyniq's; a session carries the
    identity provider's issuer, subject and tenant, and people are matched on those three, never on email alone
    (NM-INT-12), so the link change can map people across apps without a schema change.
  - Passkeys: WebAuthn with the relying-party ID from `NEMURA_RP_ID` (locally `localhost`, which accepts both
    apps' ports; in the cloud the shared registrable domain, O3). A step-up is required for an approval (NM-PPL-04
    later) and for role changes.
  - Local origins: Fyniq stays on `localhost:8080`; Nemura gets `localhost:${NEMURA_HTTP_PORT:-8180}` through its
    own Caddy site (own origin, as NM-INT-12 needs), with the issuer at `/oauth/` on it.

- **D8. Roles and the permission matrix as data (NM-ROLE-01…05, NM-SEC-02).**
  - Appendix B of PRD-002 is transcribed once into `internal/nemura/authz` (roles, areas, per-field and per-action
    rights, scope: own, reports, assigned, organisation). `TestTheGrantsAreAppendixB` compares the table with the
    PRD's rows and fails on drift, as Fyniq's does for Appendix D; an area not in the table denies.
  - The API checks the table. The database backs it for restricted tables: row-level policies read the acting
    person and roles set per transaction (`app.person_id`, `app.roles`), so a missed check in the API cannot read
    what the role may not (golden rule 2). Restricted fields are masked by default; a read releases the value only
    with the right and always writes `nemura.restricted.read`.
  - Line managers (NM-ROLE-03) come from an effective-dated `reporting_line` table created here as the scope
    predicate's source; `p1-nemura-records-packs` edits it through the people record (NM-PPL-01).
  - Segregation (NM-ROLE-04, NM-ROLE-05) is evaluated at action time from the history (who prepared, who changed,
    who approved) by the approvals primitive (D9). In a team of one, the Owner is the second person (PRD-001 §6.4).
    Granting Payroll administrator, signatory or Owner rights needs the Owner and a second approver; the
    Administrator who configures roles cannot grant them to themselves (PRD-001 SOD-6).
  - A role in Nemura never grants a right in Fyniq and the reverse: Nemura's role table is its own and no code reads
    Fyniq's.

- **D9. One dual-control primitive.** `approval_request` (object type and id, the version and digest approved,
  requester, the rights an approver must hold, expiry, the approving person and their passkey assertion, the
  outcome), with the rule that the approver is neither the requester nor the subject. Role grants use it here;
  the payroll switch (NM-FEAT-01), bank-detail changes (NM-PPL-04), pay runs (NM-PAY) and the Fyniq link
  (NM-ARC-04) reuse it. The version and digest are recorded from the start so the cross-app approval envelope
  (NM-INT-06) is a format on top, not a migration.

- **D10. API-first with Fyniq's conventions (NM-ARC-03, golden rule 4).** The HTTP layer uses the same
  framework (huma on `net/http`). `make openapi` writes `contracts/openapi/nemura/nemura.v1.yaml`, its JSON
  schemas and the generated client for `apps/nemura-web` beside Fyniq's; `make test` fails while any differ from
  the code. RFC 9457 errors with stable codes in a catalogue (`NM_*`, such as `NM_FEATURE_REQUIRED` and
  `NM_POLICY_BELOW_STATUTE` that PRD-002 already names, plus the platform's: `NM_ROLE_REQUIRED`,
  `NM_APPROVAL_SELF`, `NM_APPROVAL_EXPIRED`, `NM_RESTRICTED_FIELD`, `NM_TENANT_UNKNOWN` and so on), each with a
  test that triggers it. Writes accept `Idempotency-Key`; versioned records use `ETag` and `If-Match`; changes
  inside `/v1` are additive. `make api-check` runs Spectral and `oasdiff` against the last release on both
  contracts. The paths are served under Nemura's own origin (`/api/v1/…`); no private endpoints.

- **D11. Nemura's web app (NM-UX-01…06, NM-NFR-02, NM-NFR-04, NM-ARC-02).**
  - `apps/nemura-web`: the same stack as `apps/web` (React, Vite, TanStack Router and Query, Base UI under a
    strict CSP, Tailwind from tokens, Vitest with axe), built on `docs/design-system/nemura/tokens.css`. Its
    typed client is generated from Nemura's contract, and `archtest` already forbids a hand-written network call
    in any `apps/*/src`.
  - It keeps its own feature registry (route, action, mode tag, help key, guide page) and the same test that
    fails on a missing tag, help key or page. Employees get one simple mode; the people team's pages show more.
  - Help: `docs/user-guide/nemura/`, compiled into `kb/` in the same commit (golden rule 5). Tooltips in
    `src/i18n/en-SG.json`, never in code.
  - The banner of NM-UX-06 reads `docs/prd/reviews/review-status.json`; self-service and sign-in pages never
    carry it.
  - Delivery: the static build is embedded in the binary (`go:embed`, a committed placeholder so `go build`
    works without a web build), served by the `api` role with the headers of `deploy/docker/security-headers.inc`.
    The Dockerfile gets a web build stage used only by the `nemura` target. In `make dev` Vite serves the app and
    proxies `/api` as for Fyniq. Production needs no second origin or nginx (PRD-002 NM-ARC-02: "a static Go binary
    with the web app").
  - No shared UI package with `apps/web` yet. The semantic token names match, so a later extraction is
    mechanical; sharing earlier would couple two looks the PRD wants distinct (ADR-N6).
  - Localisation (NM-NFR-04): every string through the i18n catalogue; dates, numbers and money formatted by the
    worksite's locale (`en-SG`, S$); no string concatenation that blocks another language. Money is a decimal
    string, never a float (golden rule 1).
  - Print (NM-UX-05): a print stylesheet and a PDF view on the company's letterhead, readable in black and white.
    This change builds the document frame and proves it on My pay's layout; payslip and letter content arrive
    with their changes, and the PDF engine is chosen with letters (the standalone profile needs it as a library,
    NM-ARC-06).
  - Performance (NM-NFR-02): My home and My pay under 1.5 s at the 75th percentile on a mid-range phone on 4G,
    measured locally with the harness's throttled profile; the 5 s pay-run budget belongs to payroll.

- **D12. Privacy and records engine (NM-SEC-03, NM-SEC-04, NM-SEC-05, NM-SEC-07).**
  - A registry of **data categories**. Each record type, when a later change adds it, declares: a key, what it
    holds, its purpose (PDPA), the right that restricts it (D8), its retention rule (a minimum and a business or
    legal reason, from the pack and company policy), how to export it for an access request, and how to erase it.
    A test fails when a column whose name looks personal is in no category and not marked not-personal with a
    reason, so a record type cannot land without its inventory entry (the rule Fyniq's `TestTheDataInventory`
    applies). The inventory is `compliance/nemura-data-inventory.yaml`.
  - Retention: a schedule per record type; a due deletion is a job that produces a **report for review** with a
    default review date 30 days later, after which it is ✖ and never silently kept for ever; data under a legal
    hold is skipped and listed; each deletion leaves proof of what went and when (an audit event with fingerprints,
    no content). The Singapore minimums of NM-SEC-04 are pack values (`p1-nemura-records-packs`); here the engine
    reads whatever the registry and the pack hold, and refuses a deletion earlier than a statutory minimum.
  - Access requests (NM-SEC-07, CMP-PD-04): a tracked request with the 30-day due date or a written notice
    within it; HR reviews each category's records, releases, redacts other people's data or withholds with a recorded
    exception reason (the PDPC selected-topics exception, `[verify the current edition with the DPO]` as the PRD
    says), and the outcome is given in writing. Withdrawing consent never deletes records the law requires.
    Candidates' requests share the engine; candidate data itself is P2.
  - Overdue and open requests expose a state the Checks of `p1-nemura-records-packs` (N.1) read; this change
    does not build Checks.

- **D13. Honest scope (the rule of this change).** A requirement whose full proof needs an object a later change
  builds gets its mechanism here, a test on what exists and an entry in the "Left" column of `tasks.md` naming
  the change that finishes it. It is tagged `// req:` only for what the test proves. The affected rows:
  NM-ARC-01 (the payroll journal and bank files: payroll), NM-ARC-04 (linking: the link change), NM-ARC-05
  (reversal of posted payroll and the oracle: payroll), NM-ROLE-03 (the people record: records), NM-ROLE-04 (the
  pay-run and bank-account pairs and the cross-app half: payroll and the link), NM-NFR-03 and NM-NFR-05 (the
  hand-off and the pay-day jobs: link and payroll), NM-UX-05 (payslip and letter content) and NM-SEC-04 (the
  Singapore periods: records).

- **D14. CI, images and local mode.**
  - `GO_IMAGES` gains `nemura`; `deploy/docker/budgets.json` and PRD-001 §12.9 gain its 30 MB row in the same
    pull request, with a PRD revision, the container-images spec, `python3 scripts/prdspec.py stamp` and
    `make roadmap`. `scripts/images.py` needs no change for the checks, the scan, the signing or the publish;
    its list of images that must carry a local adapter in the local flavour gains `nemura` in the slice that
    links the first local adapter (the sign-in page).
  - `.dockerignore` and the Dockerfile gain `db/nemura/`, then `apps/nemura-web/` and
    `docs/design-system/nemura/tokens.css` with the web stage.
  - Compose: services in the profile `people` (`nemura-migrate`, `nemura-api`, later `nemura-worker`,
    `nemura-executor`, `nemura-oauth-migrate`, `nemura-oauth`, `nemura-edge`), hardened as ours, no shared volume;
    `scripts/dev.sh` runs the Nemura api on port 8087 with `PEOPLE=yes`.
  - `make check` and `make test` already run every Go package and every web workspace, so the new tests join them
    with no new target. A new command would be added to the Makefile and AGENTS.md's table (`PEOPLE=yes make up`
    is a variable of an existing one).
  - A `scripts/local-smoke.sh` variant for Nemura (sign in with no Fyniq running, through Nemura's edge) arrives
    with sign-in, and joins CI beside QA-LOCAL-01.

- **D15. Cloud, written offline (NM-NFR-01).** The last slice adds Nemura's services, database, identity
  tenants and queues to `infra/` (the platform spec's service table, the environment module, the tests) so
  `make infra-check` proves the rules offline: the Singapore region, service accounts without keys, no
  authoritative IAM, one identity and one database role per service. A plan or apply waits for the cloud gate
  (`[live-verify]`).

## Decisions needed (owner)

None of these blocks slices 1 and 2 (the skeleton and its database). Each is needed before the slice or the
cloud gate named; until answered, the slices proceed on the recommended default, which is cheap to reverse
except where stated.

### O1. Where Nemura's database lives in the cloud (needed before the cloud gate; D15)

PRD-002 ADR-N1 says "its own PostgreSQL database". It does not say whether that is its own instance. Locally it
is a second database in the same container.

| Option | Pros | Cons |
|---|---|---|
| **A. A second Cloud SQL instance** | Independent restore, failover, maintenance and scaling; pay and NRIC data on a separate instance and its own IAM bindings; PRD-001 DR-06's "restore one app alone" drill is a plain point-in-time restore; moving to a separate product later changes nothing | A second instance's floor price (Fyniq's cost review puts Cloud SQL at US$36 to 69 a month at the smallest sizes, so roughly double); a second instance to patch, monitor and back up |
| **B. A second database and its own roles on Fyniq's instance** | No added fixed cost; one instance to run; roles and databases are already isolated, and `CONNECT` is limited per database | One restore point for both: restoring the ledger to a time also rewinds people data, so the drill must restore a copy and copy one database back; a noisy neighbour (a pay run during a close); one maintenance window; a compromise of the instance reaches both |
| **C. Own instance only for production, shared in dev and staging** | Spends the money where the data is real | Staging stops being a copy of production's shape; two topologies to test |

Recommendation: **B until Nemura is sold or has its first paid person's pay data, then A before that data
arrives.** Nemura is internal for now (question 8), the connection string is the only application difference, and
the move is a dump and restore into a new instance. Revisit at the cloud gate with the drill's result
(`[live-verify]`). If you would rather not hold pay and NRIC data beside the ledger at all, choose A now.

### O2. Which cloud projects and how cost is split (needed before the cloud gate; D15)

| Option | Pros | Cons |
|---|---|---|
| **A. Fyniq's existing projects per environment, Nemura's services, identities and database under their own names and a cost label `app=nemura`** | No new project or billing set-up; one set of org policies, budgets and alerts; Nemura's services cost only what they use (scale to zero) | A role mistake could reach across (mitigated by one service account per service and the infra rules); costs need the label to read apart |
| **B. Separate projects for Nemura per environment** | Hard blast-radius and billing separation; ready to hand over if Nemura is sold; its own audit-log sink | More infra to run and keep identical (projects, networks, budgets, a second Artifact Registry or cross-project pulls); cross-project service calls for the link |

Recommendation: **A, with the label and an infra test that no Nemura identity holds a role on a Fyniq resource
and the reverse, except the link's audience-bound calls.** Go to B if Nemura is sold or an auditor asks for it.

### O3. Nemura's hostname and the shared passkey domain (needed before the cloud gate; D7)

Question 10 fixed that both apps sit under one registrable domain, which is the passkey relying-party ID. The
domain and the name are not fixed. Locally the relying-party ID is `localhost` and nothing is needed.

| Option | Pros | Cons |
|---|---|---|
| **A. `people.<your domain>`** | Neutral, survives a product rename; reads well beside `app.` | Not the brand people will hear |
| **B. `nemura.<your domain>`** | The product's name | If Nemura is renamed or sold on its own domain, the passkeys registered under the shared relying-party ID would not move: those users register again |
| **C. A different domain for Nemura** | Clean to sell later | Breaks question 10: no shared passkey and no cross-app approval verification |

Recommendation: **A.** Please name the registrable domain (the production domain of Fyniq is not in the
repository).

### O4. Whether the `nemura` image rides Fyniq's release train from now (needed before the first release after slice 1; D14)

Adding it to `budgets.json` makes `scripts/images.py publish` build, scan, sign and attest it with each release, and
the release evidence lists it. Nothing deploys it.

| Option | Pros | Cons |
|---|---|---|
| **A. Yes: it joins the images and the release now** | Its supply chain (budget, scan, signature, SBOM, reproducibility, both platforms) is proven from the first slice and cannot regress unseen; no special case in the scripts | One more image in each release (a few minutes of release time and a few MB in the registry); a Nemura scan finding can block a Fyniq release |
| **B. Built and checked in CI, held out of `release.yml` until Nemura's own launch** | Fyniq's releases are untouched by a product that is not live | A special case in the release scripts and tests (`archtest` and `images.py` assume every budgeted image publishes); the first release of Nemura is the first time its signing is exercised |

Recommendation: **A.** It is a dormant, scanned image; the cost is small and a special case in the release
scripts would be new risk in the pipeline guarding go-live. If a Nemura finding ever blocks a Fyniq release, a
reviewed, expiring scan waiver is the existing remedy.

### O5. Confirm the code boundary: one module with enforced limits (needed before slice 3; D3)

Proceeding on the recommendation. It is the one choice that is expensive to undo once the audit, outbox and
identity code are written against it.

| Option | Pros | Cons |
|---|---|---|
| **A. One Go module, `internal/nemura/…`, an allow-list of shared infrastructure enforced by `archtest`** | Matches ADR-N1 and the plan (`cmd/nemura`); no module split; the shared packages are reused as they are; the allow-list is the future shared module's list | A discipline held by tests, not by the compiler: a later change can widen the list (a reviewed change to `archtest`) |
| **B. A second module (`nemura/go.mod`, `go.work`) with the shared packages moved to an exported module** | The compiler enforces the boundary; ready for a separate repository | Moves roughly ten packages out of `internal/` now, touching every Fyniq import; two module graphs for Dependabot, the scans and the licences; delays Nemura |
| **C. Copy the shared code into Nemura** | No coupling at all | Two audit, clock and money implementations to keep correct; against the "shared rules" intent (NM-ARC-05) |

Recommendation: **A**, and revisit B when Nemura has its own team (the alternative ADR-N1 already keeps open).

## Risks / Trade-offs

- [A shared Postgres role or database setting leaks across the two databases] → `CONNECT` is revoked per database
  and tested both ways; role names never overlap; the Nemura grants golden file fails on any extra privilege.
- [Nemura's code drifts into Fyniq's, breaking ADR-N4] → the archtest rules of D3 fail the build at the import.
- [The shared `fyniq` goose table and Nemura's migrate run in one cluster] → separate tables and schemas; the
  runner takes a set and never both.
- [Fyniq's `NRIC`-naming tests (`TestNoNRICOrCardDataIsHeld`, CMP-PD-07) trip over Nemura's NRIC or FIN fields,
  which PRD-002 requires, encrypted] → the rule's scope is Fyniq's schema golden, API schemas and events, and
  stays so; Nemura has its own rule (NM-PPL-02: encrypted, masked, audited). Checked in the first slice that
  adds such a field (`p1-nemura-records-packs`), and flagged in this change's threat model.
- [Two web apps drift apart in accessibility] → the same axe tests, contrast check in four themes
  (`make check-tokens` extended to Nemura's tokens) and the registry test run on both.
- [The embedded web build is stale in the image] → the Dockerfile builds it in the same `docker build`; a test
  fails when the placeholder is served by a release image.
- [One image, five roles: a role starts with another role's environment] → each role refuses to start without its
  own database role and refuses extra ones; the compose and the OpenTofu give each only its own URL.
- [A shared relying-party ID makes one phished passkey registration useful in both apps] → step-up is bound to
  the app's origin and an approval's digest; the shared registry and its verification are designed in
  `p1-nemura-fyniq-link` with the security review.

## Migration plan

Slices land in the order of `tasks.md`, one pull request each, every one green on its own (`make test`,
`make images`, `make check`):

1. Skeleton: the service, image, budget, compose profile, health and version, CI wiring.
2. Database and migrations runner, roles, tenancy base, local bootstrap.
3. Audit chain and the outbox, the worker role.
4. Identity: Hydra, sign-in, sessions, passkeys, members.
5. Roles, the matrix, approvals, need to know.
6. The API contract and its conventions.
7. The web app, experience and sign-in screen.
8. Privacy and records: the registry, retention, access requests.
9. My pay and print frames, performance, the pay-day alert mechanism.
10. Cloud code written offline, threat model, controls, gates and the closing archtest sweep.

Rollback is deleting the compose profile and the image from `GO_IMAGES` and the budget: nothing else depends on
Nemura until `p1-nemura-records-packs`.

## Open questions

None beyond "Decisions needed". Two facts are carried with their tags, not stated as settled:
the PDPC exception for opinions kept solely for evaluative purposes (`[verify the current edition with the DPO]`,
NM-SEC-07) and the Identity Platform adapter against the real service (`[live-verify]`).
