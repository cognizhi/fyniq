# Tasks

Each numbered group below is one pull request (a "slice"), in this order, and each ends with the gates green:
`make check`, `make test` and, for a slice that touches an image, `make images`. A slice never leaves a
test red or a requirement tagged that its tests do not fully prove. `[live-verify]` marks what only the cloud
can show; it is written and tested offline here and left for the cloud gate. Slices 1 and 2 are the skeleton
and its database; the owner's "Decisions needed" (design.md O1 to O5) block none of the first two.

Decisions (owner, 19 Nov 2026), all as recommended in design.md: O1 a second database on Fyniq's Cloud SQL instance until the first paid person's pay data, O2 Fyniq's projects with the label `app=nemura`, O3 `people.<domain>` as configuration, O4 the image rides the release train, O5 one Go module with the archtest allow-list.

Load before each slice: `fyniq-local-dev` (images, compose), `fyniq-feature-change` (every slice),
`fyniq-security-compliance` (3, 5, 8, 10), `fyniq-api-integrations` (4), `fyniq-design-system` and
`fyniq-knowledge-base` (7, 9), `fyniq-release` (a PRD revision in slice 1).

## 1. The service, its image and local mode (slice 1)

- [x] 1.1 `cmd/nemura`: one binary that runs the role in `NEMURA_ROLE` (`api`; `migrate` joins in slice 2; `worker`,
  `executor` and `assistant` refuse until the slice that builds them), through `internal/app`, so `/healthz`,
  `/readyz`, `/version`, `healthcheck` and `--version` behave as every image's. An unset, unknown or not-yet-built
  role refuses to start with `NEMURA-ROLE-01` and a plain fix; `/version` names the service `nemura-<role>`; the
  release flavour refuses outside staging and production as the others do. Tests `// req: NFR-IMG-06` (the three
  endpoints and the probe), and the role refusals (no tag: they prove part of NM-ARC-06, which is tagged in 5.3).
  (Built, 18 Nov 2026, #823: `cmd/nemura` and `internal/nemura/role`; `cmd/nemura/main_test.go` and `serve_test.go`. `FYNIQ_ENV`, `FYNIQ_BUSINESS_DATE` and `PORT` keep their Fyniq names; Nemura's settings are `NEMURA_*`.)
- [x] 1.2 The image and its gates (D2, D14): `nemura` joins `GO_IMAGES` and `deploy/docker/budgets.json` at 30 MB;
  PRD-001 §12.9 gains its row in a new revision (`(rev 0.N)`, a row in the revision history), with the
  `platform/container-images` spec, `python3 scripts/prdspec.py stamp`, and `make roadmap`; `.dockerignore` and the
  Dockerfile stay as they are for this slice (the service copies nothing new). Tests: `TestImageBudgetsAreThePRDs`
  `// req: NFR-IMG-04`, `make images` in both flavours and both platforms (budget, non-root, no shell, no local
  adapter in release), and the scan, signing and reproducibility runs pick it up from the budget file.
  (Built, 18 Nov 2026, #823: PRD-001 rev 0.290; image 4.4 MB of 30 MB at the time.)
- [x] 1.3 The boundaries of D3 as `archtest` rules, written first: Nemura's allow-list of shared packages, nothing
  outside it importing `internal/nemura`, and the money rule on its packages, each with a planted violation that the
  rule catches. Test `TestNemuraStaysInItsLane`, with no
  requirement tag: it proves one clause of NM-ARC-05, which stays untagged until payroll (see Coverage).
  (Built, 18 Nov 2026, #823: `internal/archtest/nemura.go`; the money rule joins with Nemura's first money package, in payroll.)
- [x] 1.4 Local mode (D14): compose services under the profile `people` (`nemura-api`, hardened as ours, no shared
  volume, own port `NEMURA_API_PORT`, default 8087), `PEOPLE=yes make up` and `make dev`, `scripts/dev.sh`, the row for
  `nemura` in `cmd/README.md`, and AGENTS.md's table (the `PEOPLE` variable on `make up`). Tests: a Go test that
  every Nemura service in the compose file is in the `people` profile and hardened (`TestComposeHardensOurServices`
  already covers the second), `scripts/local-smoke.sh` unchanged and still green.
  (Built, 18 Nov 2026, #823: `PEOPLE=yes make up` and `make dev`; `make down` and `make reset` stop both profiles.)
- [x] 1.5 Verify: `make check`, `make test`, `make images`. Tick 1.1 to 1.4 with a "Built:" note.
  (Done for #823: `make check`, `make images` and the compose service healthy.)

## 2. Its own database and migrations (slice 2)

- [x] 2.1 The runner is a shared `internal/adapters/postgres/gooserun` package that takes a migration set (embedded files, directory, goose table), so Nemura's binary links none of Fyniq's store; `db/nemura/embed.go` and
  `db/nemura/migrations/`; `NEMURA_ROLE=migrate` with `up` and `status` under `nemura_migrator`; `.dockerignore` lines
  for `db/nemura/`; `make db-lint` lints both streams; the local bootstrap creates `nemura_migrator`, the `nemura`
  database and `nemura_hydra` with its role. Fyniq's `migrate` is unchanged and its tests still pass. Tests: the
  runner applies Nemura's stream to an empty database and is idempotent; Fyniq's `TestSchemaSnapshot` and grants
  golden files do not change.
  (Built, 19 Nov 2026: `internal/adapters/postgres/gooserun`, `db/nemura/`, `cmd/nemura` migrate role, `.squawk.toml` and `make db-lint`. `nemura_hydra` and its role are created with the issuer, in the identity slice, not now.)
- [x] 2.2 Migration 000001 (roles, schema, tenancy): group roles `nemura_api`, `nemura_worker`, `nemura_executor`,
  `nemura_assistant` (no grant yet) and `nemura_readonly`; schema `nemura`; `nemura.current_org()` and
  `nemura.require_org()` reading `app.org_id`; `organisation` and the global `tenant_directory` with its view;
  `ENABLE` and `FORCE ROW LEVEL SECURITY`. Squawk clean; a CHECK widening is `NOT VALID` then validated in the
  next migration. Tests `// req: SEC-03` for the roles' exact grants (`grants.golden`, an extra privilege fails),
  `// req: INV-06` for tenant isolation, each with `// asvs:` IDs from `docs/security/asvs-5.0.tsv`; the schema
  golden file; `CONNECT` on Nemura's database refused to a login that holds no Nemura role (the reverse is infra's, `[live-verify]`).
  (Built, 19 Nov 2026: migration 000001, `internal/adapters/postgres/nemura/nemura_test.go` and its golden files. The tests carry no Fyniq requirement tags (SEC-03, INV-06 are Fyniq's rows); NM-SEC-01 is tagged in 10.2. `ALL` is revoked from `PUBLIC` on the database.)
- [x] 2.3 `internal/adapters/postgres/nemura`: the unit of work (`store.go`) that sets `app.org_id` first in every
  transaction and the `Ping` behind `/readyz`'s `database` check (sqlc joins when the first query is worth generating); the archtest
  exception for `nemura/store.go` only, so no other file begins or ends a transaction. Compose gains
  `nemura-migrate` and `nemura-api` waits for it; `nemura-api` connects as `nemura_api`. Tests: the unit of work
  refuses a query with no organisation set and cannot read another organisation's row; a planted transaction call
  outside `store.go` is caught.
  (Built, 19 Nov 2026: `internal/adapters/postgres/nemura/store.go`, `bootstrap_local.go`, `nemuratest`, the compose services `nemura-migrate` and `nemura-api`, and the archtest exception.)
- [x] 2.4 Verify: `make check`, `make test`, `make images`, `make up PEOPLE=yes` brings `nemura-api` ready.
  (Done: `make check`, `make db-lint`, gosec, semgrep and the compose stack ready.)

## 3. Audit chain, outbox and worker (two slices)

- [x] 3.1 (slice 3a) Audit in Nemura's database (D5): `audit_chain` and `audit_event` with the append trigger,
  the same canonical form and chain rule, a Nemura schema id in `contracts/audit/`, written by the unit of work in
  the same transaction as the change, masked by the shared code, sealed per person and year with the local key
  adapter. Tests `TestNemuraAuditSameTransaction`
  (no tag yet: NM-SEC-01 is tagged in 10.2, when all its clauses are proven), tamper detection (an edited or removed event fails verification), no secret, token
  or full NRIC in an event (`// asvs: V16.2.5` or the matching ID).
  (Built, 19 Nov 2026: migration 000002, `internal/adapters/postgres/nemura/audit.go` and `audit_test.go`: `TestAChangeAndItsAuditEventCommitTogether`, `TestTheChainDetectsTampering`, `TestNoSecretReachesTheAuditTrail`, `TestOneOrganisationsTrailIsInvisibleToAnother`. The event form is the shared `internal/domain/audit` under the schema id `nemura-audit-event@1`. **Left:** sealing of personal values per person and year (it arrives with the first personal field, in `p1-nemura-records-packs`), and a Nemura schema file in `contracts/audit/`.)
- [ ] 3.2 (slice 3a) Verification and anchoring: the Nemura storage port's chain reads, the nightly verification
  and the anchors in a Nemura-prefixed bucket (local blob store; the real bucket is `[live-verify]`). Tests: a
  tampered chain fails the nightly check, an anchor mismatch is reported, a restored copy verifies.
  (Built in part, 19 Nov 2026: `Store.VerifyAudit` recomputes a whole chain from its first event and checks it against the chain head; `TestTheChainDetectsTampering`. **Left:** the anchors in a Nemura-prefixed bucket, the nightly verification job and a restored copy's check, which need the signer and blob ports wired into the worker.)
- [x] 3.3 (slice 3b) The outbox and the `worker` role (D6): `outbox` table and its functions, `POST /tasks` with an
  audience-bound task token for `nemura-worker`, retry with backoff, dead-lettering with an alert event, the
  scheduler tick endpoint, and `/jobs` for long work; `nemura_worker` grants. Tests: a task queued while its
  consumer is stopped runs once it is back and exactly once (idempotent on its key); a poisoned task is
  dead-lettered; the worker refuses to start without its own database URL or with another role's (`// req: NM-ARC-06`
  is tagged in 5.3, when the service identities are all proven).
  (Built, 19 Nov 2026: migration 000003, `internal/nemura/outbox`, `internal/adapters/postgres/nemura/outbox.go` and `outbox_test.go`, the worker role of `cmd/nemura` (`/tasks`, `/tick`) and the compose service `nemura-worker`. The release flavour's Cloud Tasks wiring is task 10.1 [live-verify] and refuses to start the worker until then. A service refuses another role's database URL (`NEMURA-ROLE-02`), tested in `TestAServiceRefusesAnotherRolesDatabaseURL`.)
- [x] 3.4 Verify each slice: `make check`, `make test`, `make images`.

## 4. The API foundation (slice 4; golden rule 4)

- [x] 4.1 The HTTP layer on the same framework as Fyniq's (huma on `net/http`), served under `/api/v1/` of
  Nemura's own origin; `make openapi` also writes `contracts/openapi/nemura/nemura.v1.yaml`, its JSON schemas under
  `contracts/schemas/nemura/` and the typed client for `apps/nemura-web`; `make test` fails while any differ from
  the code; `make api-check` runs Spectral and `oasdiff` against the last release on both contracts. First
  operations: the session, the feature registry stub and the error catalogue. Tests `// req: NM-ARC-03`
  (tagged here for the contract's generation and drift only; full in 4.3).
  (Built, 19 Nov 2026: `internal/nemura/api`, `contracts/openapi/nemura/nemura.v1.yaml` and `spectral.yaml`, `make openapi` and `make api-check`, release-please keeps the version. The generated TypeScript client arrives with `apps/nemura-web` (7.1); oasdiff against a release starts once Nemura has one. First operations: `GET /v1/info`, `GET /v1/errors`, `GET /v1/errors/{code}`; the session operation arrives with sign-in.)
- [x] 4.2 Conventions: RFC 9457 errors with the `NM_*` catalogue in `contracts/openapi/nemura/errors.yaml` and a
  test that triggers each code with a plain message that names the field and the fix (ENG-23); `Idempotency-Key` on
  every write, stored in Nemura's database; `ETag` and `If-Match` on versioned records; cursor paging; additive-only
  inside `/v1`. Tests: each convention on a sample resource; a planted breaking change fails `oasdiff`.
  (Built in part, 19 Nov 2026: the RFC 9457 `Problem` with the `NM_*` catalogue (`NM_NOT_FOUND`, `NM_INVALID`), each triggered by `TestEveryCodeInTheCatalogueIsTriggered`; the Idempotency-Key rule is a contract test over every write, vacuous until the first one. **Left:** Idempotency-Key storage, ETag and If-Match and cursor paging arrive with the first write operations and the first versioned list.)
- [x] 4.3 `TestNemuraAuthorisationMatrix` skeleton: every operation in the contract has a row (role, expected status),
  and an operation without one fails the build; unknown routes deny. Tag `// req: NM-ARC-03` for the whole slice here
  and tick it (the later slices add operations under the same rules).
  (Built, 19 Nov 2026: `api.Matrix` and `TestEveryOperationHasARowInTheAuthorisationMatrix`, with a planted operation; the rows grow with each operation. `// req: NM-ARC-03` is tagged on the contract, route, Idempotency-Key, catalogue and matrix tests.)
- [x] 4.4 Verify: `make openapi` clean, the web app's typecheck, `make api-check`, `make check`, `make test`.
  (Done: `make openapi` clean, `make api-check`, `make check`.)

## 5. Identity, the token issuer and sign-in with no Fyniq (slice 5)

- [ ] 5.1 Members and identities: `member`, `user_identity` (issuer, subject, tenant; never matched on email alone,
  NM-INT-12's rule), invitations with a one-time code and expiry, and first-run setup that makes the first
  organisation's Owner with a one-time setup code (as FR-ADM-06; a `make` command with a `## description` and a
  row in AGENTS.md's table). Tests: one person in two organisations; a matching email with a different subject is
  not the same person; a used or expired code is refused.
- [ ] 5.2 Nemura's own Ory Hydra (D7): `nemura-oauth-migrate` and `nemura-oauth` in the `people` profile on
  `nemura_hydra`, the same pinned image and settings (opaque tokens, PKCE only, 60-minute access tokens), the
  `OAuth` port wired, `/api/auth/login` and `/consent` on Nemura's origin, the audience `nemura-api`, and Nemura's own
  Caddy site on `NEMURA_HTTP_PORT` (default 8180) as the edge. Tests, named for Nemura (AI-36 is Fyniq's
  tag): PKCE only, audience binding, no token passthrough.
- [ ] 5.3 Sign-in and sessions: the `SignIn` port's local adapter (a sign-in page) and the Identity Platform adapter
  per organisation tenant (tested against its fake; against the service `[live-verify]`); an HTTP-only `__Host-`
  session cookie on Nemura's origin; sign-out and deactivation revoke the session and its tokens through the
  worker; a role change takes effect within 60 seconds. The `nemura` image's local flavour then carries a local
  adapter, so `scripts/images.py` lists it in the images that must. Tests `// req: NM-ARC-06` for: the services'
  separate identities and roles, the standalone profile (the list of what Nemura needs) and **sign-in with every
  Fyniq service stopped**, proven by a new `scripts/local-smoke-people.sh` that CI runs beside QA-LOCAL-01.
- [ ] 5.4 Passkeys and step-up: WebAuthn with the relying-party ID from `NEMURA_RP_ID` (locally `localhost`), single-use
  server-side challenges, assertions kept for re-verification, step-up for approvals and role changes; and Nemura's
  own API clients, audience-bound, with the scopes of NM-ARC-06's own token issuer (question 11). Tests: a step-up is
  bound to the origin and the digest it approves; a replayed assertion is refused. The registry shared with Fyniq
  is `p1-nemura-fyniq-link`'s.
- [ ] 5.5 Verify: the smoke script, `make images`, `make check`, `make test`.

## 6. Roles, the permission matrix, approvals and need to know (slice 6)

- [ ] 6.1 Appendix B as data in `internal/nemura/authz` (roles, areas, per-field and per-action rights, scope:
  own, reports, assigned, organisation), `TestTheGrantsAreAppendixB` comparing it with the PRD's rows, an unknown
  area denies. Roles are Nemura's own and assigned per organisation; no code reads Fyniq's role table. Tests
  `// req: NM-ROLE-01` and the "Permission matrix" requirement `// req: people/experience Permission matrix`
  (one test per scenario: employee, AI, payroll without medical data, interviewer, administrator).
- [ ] 6.2 Role assignment, effective-dated and audited, with its history; the role table's `RLS` and grants.
  Tests: a grant takes effect from its date and an end date removes it; every change emits its event.
- [ ] 6.3 The approvals primitive (D9): `approval_request`, the rule that the approver is neither the requester nor
  the subject, expiry, the passkey assertion, and the version and digest approved. Role grants use it: Payroll
  administrator, signatory and Owner need the Owner and a second approver; in a team of one the Owner is the second
  person. Tests `// req: NM-ROLE-05` (no self-escalation: an HR administrator cannot give themselves pay or
  signing rights) and `// req: NM-ROLE-04` for the generic rule only, with its tag held back until the pay-run
  and bank-account pairs exist (**Left:** `p1-nemura-records-packs` for the bank account, `p1-nemura-payroll` for the
  run, `p1-nemura-fyniq-link` for the run's salary batch in Fyniq).
- [ ] 6.4 Need to know (NM-SEC-02): the restricted-field framework (masked by default, released only with the right,
  `nemura.restricted.read` on every release), row-level scope predicates reading `app.person_id` and `app.roles`,
  `reporting_line` (effective-dated) as the source for the manager scope, and the platform's own restricted objects
  as the proof (role history, access requests, the audit read). Tests `// req: NM-SEC-02` for the mechanism over
  the platform's objects (the Left: each record type's fields are added by its change, which adds its own test
  to the same suite), `// req: NM-ROLE-03` for the scope (a manager sees only their reports; never pay), and an
  attack test that a missed API check cannot read what the role may not, with ASVS IDs.
- [ ] 6.5 Landing by role: the session operation returns the person's landing page and effective rights (the table of
  PRD-002 §3). Tests: each role's landing; no ledger, bank, revenue, cash or company-financial field in any
  Nemura operation's schema (a contract scan), `// req: NM-ROLE-02`, completed with the web app in 7.4.
- [ ] 6.6 Verify: `TestNemuraAuthorisationMatrix` rows for every new operation (`FYNIQ_MATRIX_ONLY=<regex>` while
  working), `make openapi`, `make check`, `make test`.

## 7. Nemura's web app and experience (two slices)

- [ ] 7.1 (slice 7a) `apps/nemura-web`: the scaffold on the same stack as `apps/web`, Nemura's tokens
  (`docs/design-system/nemura/tokens.css`, `make check-tokens` already checks them in four themes), themes, the
  generated client, the typecheck and Vitest with axe in `make test`, the `nemura-web` workspace in
  `pnpm-workspace.yaml`. Tests `// req: NM-UX-01` (tokens only, contrast in four themes, AI content violet with the
  badge, no third-party font request).
- [ ] 7.2 (slice 7a) The feature registry for Nemura (route, action, mode, help key, guide page; a missing one fails)
  and the modes: one simple mode for employees, more for the people team; WCAG 2.2 AA, 44 px targets, the five device
  frames from 320 px, switchable motion. Tests `// req: NM-UX-02`, `// req: FR-UX-38` for motion in the same
  way as Fyniq's (`e2e/motion.spec.ts`).
- [ ] 7.3 (slice 7a) `nm-sign-in` built to its fragment: Google, email and password, the company's OpenID Connect
  provider, passkey; states standalone and wrong password (the "linked to Fyniq" state arrives with the link).
  Tests: the page's journeys in Playwright with no Fyniq running; axe; the page never shows the banner.
- [ ] 7.4 (slice 7b) Workspaces and landing by role in the shell (completes NM-ROLE-02 with 6.5); the header's slot
  for the Finance | People switch and the rule that it shows only to people with a finance workspace, proven on a
  stubbed session (**Left:** the switch itself, its last-page memory and the single sign-in across both apps:
  `p1-nemura-fyniq-link`, NM-UX-03 and NM-INT-02). Tests `// req: NM-ROLE-02`.
- [ ] 7.5 (slice 7b) Help on every screen (NM-UX-04): help keys, tooltips in `src/i18n/en-SG.json`, guide pages in
  `docs/user-guide/nemura/` (sign-in, roles and access, your data, my pay), the first-run tours for employees and the
  people team; compiled into `kb/` in the same commit (`python3 scripts/kb.py hash <src>`, `kb.py log compile`, `make
  kb-index`, `make kb-lint`). Tests `// req: NM-UX-04` and the registry test.
- [ ] 7.6 (slice 7b) "Needs additional verification" (NM-UX-06): the banner reads
  `docs/prd/reviews/review-status.json`; the people team's payroll, pay-run approval, leave-policy, country-pack,
  checks, onboarding and settings pages carry it; self-service and sign-in never do. A change to the register
  goes in its own pull request (it never auto-approves). Tests `// req: NM-UX-06`: the banner on each listed page
  and on none of the excluded ones.
- [ ] 7.7 (slice 7b) Localisation (NM-NFR-04): every string through the catalogue, `en-SG` dates, numbers and S$ from
  the worksite's locale, a test that fails on a string literal in a component, money as a decimal string and the
  clipboard's ASCII minus. Tests `// req: NM-NFR-04`.
- [ ] 7.8 (slice 7b) Delivery (D11): the static build embedded in the binary (a committed placeholder so `go build`
  works), the web stage in the Dockerfile for the `nemura` target, `.dockerignore` for `apps/nemura-web/` and
  `docs/design-system/nemura/tokens.css`, the headers of `security-headers.inc`, a test that a release image
  never serves the placeholder, and the image's size remeasured against the 30 MB budget (raise it by a reviewed
  PRD change if the real build needs it). Tests `// req: NM-ARC-02` (image, database, migrations, local mode and the
  embedded web app, all now proven), `// req: NM-UX-02` for the first-load budget.
- [ ] 7.9 Verify: `pnpm exec tsc -b --noEmit` and `pnpm exec vitest run` in `apps/nemura-web`, `make images`,
  `make check`, `make test`, `make test-e2e` for the sign-in journey.

## 8. Privacy and records (slice 8)

- [ ] 8.1 The data-category registry (D12) and `compliance/nemura-data-inventory.yaml`: key, what it holds, purpose,
  restricting right, retention rule, export and erase hooks; the completeness test over Nemura's schema golden file;
  the register of transfers outside Singapore with their assessments (CMP-PD-06). Tests `// req: NM-SEC-03` (a
  purpose per type, an inventory entry per type, a transfer refused without a current assessment).
- [ ] 8.2 Retention (NM-SEC-04, NM-SEC-05): a schedule per record type with its reason; the minimum periods read
  from the pack when it exists and refused to go below; the deletion job that produces a report for review with a
  default 30-day review date, ✖ after it, legal holds skipped and listed, and a proof event for each deletion;
  never silent. Tests `// req: NM-SEC-05` for the job, the review date, the hold and the proof; `// req: NM-SEC-04`
  for the refusal below a minimum (**Left:** the Singapore pack's own values, reviewed with
  `p1-nemura-records-packs`; a test of this engine with a stub pack minimum is what is tagged here).
- [ ] 8.3 Access to one's own file (NM-SEC-07, CMP-PD-04) and `nm-access-request` built to its fragment: a tracked
  request with a due date at 30 days or a written notice within them, HR's review of each category (release, redact
  other people's data, withhold with a recorded exception), the written outcome, withdrawal never deleting
  records the law requires, and the overdue state for Checks to read later. The PDPC exception stays
  `[verify the current edition with the DPO]`. Tests `// req: NM-SEC-07`; the page's journey; axe.
- [ ] 8.4 Verify: `make check`, `make test`, `make kb-lint`, the controls (`make controls`) for the new entries.

## 9. My pay, performance and the pay-day alert (slice 9)

- [ ] 9.1 `nm-my-pay` built to its fragment on the document frame: the person's own pay only (NM-SEC-02), next pay
  day, latest payslip and forms with their empty and "form not ready" states, and the print and PDF view readable
  in black and white. Tests: another person's pay is refused (API and row-level), the print stylesheet (black and white,
  letterhead slot), `// req: NM-UX-05` is **not** tagged until payslip and letter content exist (**Left:**
  `p1-nemura-payroll` and `p1-nemura-self-service-letters`).
- [ ] 9.2 Performance (NM-NFR-02): My pay under 1.5 s at the 75th percentile on the throttled mobile profile,
  measured locally against the embedded build; the harness is the one `p1-nemura-self-service-letters` reuses for My
  home, and the pay-run budget is payroll's. Tests: the measured budget in `make test-e2e`; tag nothing until My
  home exists (**Left**).
- [ ] 9.3 The pay-day alert mechanism (NM-NFR-05, NM-NFR-03): a job-failure alert that reaches the Payroll
  administrator and the Owner within an hour (the worker's dead-letter and the scheduler's heartbeat feed it), and the
  outbox queueing a hand-off while its consumer is down. Proved on a fake job; the real pay-day jobs and the hand-off
  to Fyniq are payroll's and the link's. Tests: the alert fires within the window on a fake clock; a queued item
  survives a stopped consumer.
- [ ] 9.4 Region and scale to zero (NM-NFR-01): the release flavour refuses a non-Singapore region setting; the
  infra spec (10.1) has no minimum instances; local mode and `make test` pass offline. Tests `// req: NM-NFR-01`
  for the refusal and the offline run; the Cloud Run behaviour is `[live-verify]`.
- [ ] 9.5 Verify: `make check`, `make test`, `make images`.

## 10. Cloud code written offline, the threat model, controls and the close (slice 10)

- [ ] 10.1 `infra/`: Nemura's services (api, worker, executor, assistant, issuer), their identities and database
  roles in the platform spec, the queues and tick, the database and identity tenants per O1 (the database on Fyniq's instance, with its own roles) and O3 (the hostname as configuration), the cost label of
  O2 and its test that no identity of either app holds a role on the other's resources, with the module tests and `make infra-check` proving the rules offline (region, no keys, no authoritative IAM,
  no cross-app role). Plan and apply: `[live-verify]` at the cloud gate. Tests `// req: NM-NFR-01`.
- [ ] 10.2 The threat model `docs/security/threat-models/nemura-platform.md` (tenancy, the shared relying-party ID,
  restricted data, the approvals, the token issuer, the embedded web app), the controls in `compliance/controls/`,
  ASVS L2 with L3 on sign-in, authorisation and logging for pay and approval flows, the runbooks (first run, role
  recovery, restoring Nemura alone, `[live-verify]` for the drill), and the second-reviewer label on the pull
  requests that change tenancy, approvals or the audit trail. Tests `// req: NM-SEC-01` (tagged now: tenancy,
  audit, logging, secrets and ASVS are all proven in 2, 3, 5 and here) and `// req: SEC-20`.
- [ ] 10.3 The close: every requirement in the coverage table below carries a test tag or a "Left" note naming its
  change; `make roadmap` and `python3 scripts/prdspec.py` report nothing stale; a final `openspec validate
  p1-nemura-platform --strict`; `make check`, `make test`, `make images`, `make image-scan`, `make image-sign`.

## Coverage

Every requirement of the plan's row for `p1-nemura-platform` (30 requirements, two unnumbered requirements and
three screens), with where it is built and what, if anything, a later change finishes. A requirement is tagged
only when its row below says "complete".

| Requirement | Built in | Status | Left to |
|---|---|---|---|
| Architecture decisions (unnumbered) | 1.3, 2, 5, 8 | complete with 8 | |
| NM-ARC-01 Standalone first | 5.3 (sign-in and the profile with no Fyniq) | partial | the journal and bank files: `p1-nemura-payroll` |
| NM-ARC-02 One image, own database | 1.2, 2, 7.8 | complete with 7.8 | |
| NM-ARC-03 API-first | 4 | complete with 4.3 | each later operation follows its rules |
| NM-ARC-04 Linking to Fyniq | 1.3 (nothing in Nemura reads Fyniq's code or data), 6.3 (dual control) | foundation only; no link state is pre-built | `p1-nemura-fyniq-link` |
| NM-ARC-05 Shared rules | 1.3, 3.1 | partial | reversal of posted payroll and the oracle: `p1-nemura-payroll` |
| NM-ARC-06 Service identities and the standalone profile | 3.3, 5.3 | complete with 5.3 | the executor's and the assistant's tools: `p1-nemura-assistant-external` |
| NM-NFR-01 Region and residency | 9.4, 10.1 | complete locally | Cloud Run and region `[live-verify]` |
| NM-NFR-02 Performance | 9.2 | partial (My pay) | My home: `p1-nemura-self-service-letters`; the 5 s pay run: `p1-nemura-payroll` |
| NM-NFR-03 Availability | 3.3, 9.3 | partial | the hand-off to Fyniq: `p1-nemura-fyniq-link`; 99.5% `[live-verify]` |
| NM-NFR-04 Localisation | 7.7 | complete | |
| NM-NFR-05 Pay-day path | 9.3 | mechanism only | the pay-day jobs: `p1-nemura-payroll` |
| NM-SEC-01 Inherited controls | 2, 3, 5, 10.2 | complete with 10.2 | |
| NM-SEC-02 Need to know | 6.4 | mechanism complete | each record type's fields join the suite in its change |
| NM-SEC-03 PDPA | 8.1 | complete | |
| NM-SEC-04 Records the law requires | 8.2 | engine complete | the Singapore periods: `p1-nemura-records-packs` |
| NM-SEC-05 Retention per record type | 8.2 | complete | |
| NM-SEC-07 Access to one's own file | 8.3 | complete | |
| NM-ROLE-01 Roles are Nemura's own | 6.1, 6.2 | complete | |
| NM-ROLE-02 Landing by role | 6.5, 7.4 | complete with 7.4 | |
| NM-ROLE-03 Line managers | 6.4 | scope complete | the people record that edits it: `p1-nemura-records-packs` |
| NM-ROLE-04 Segregation | 6.3 | generic rule | pay-run and bank-account pairs: `p1-nemura-records-packs`, `p1-nemura-payroll`; across both apps: `p1-nemura-fyniq-link` |
| NM-ROLE-05 No self-escalation | 6.3 | complete | |
| Permission matrix (unnumbered) | 6.1 | complete | |
| NM-UX-01 Nemura's own design system | 7.1 | complete | |
| NM-UX-02 Easy for everyone | 7.2, 7.8 | complete | |
| NM-UX-03 Workspace switch | 7.4 (the slot and the rule) | partial | the switch: `p1-nemura-fyniq-link` |
| NM-UX-04 Help on every screen | 7.5 | complete for this change's screens | each later screen adds its help |
| NM-UX-05 Print-ready documents | 9.1 (the frame) | partial | payslip and letter content: `p1-nemura-payroll`, `p1-nemura-self-service-letters` |
| NM-UX-06 Needs additional verification | 7.6 | complete | |
| Screen `nm-sign-in` | 7.3 | complete (standalone; linked state with the link) | |
| Screen `nm-access-request` | 8.3 | complete | |
| Screen `nm-my-pay` | 9.1 | page and rules | payslips and forms: `p1-nemura-payroll` |

NM-SEC-06 (grievances) is P2 and sits in `p2-nemura-people`, not here.
