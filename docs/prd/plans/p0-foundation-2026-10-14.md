# P0 · Foundation: the build plan

P0 builds the skeleton that every later phase stands on: local mode, the database backstop, the ledger
engine and its independent oracle, the audit chain, background work, the API's conventions, identity,
images and releases, infrastructure and controls as code (PRD-001 §18, the P0 column). It is done when
the seven exit criteria of §17.3 hold, with evidence in the release record.

The plan is eleven OpenSpec changes in `openspec/changes/p0-*`, and a twelfth, added on 15 Oct 2026, for the
rules a manual journal and its reversal post by, which PRD-001 rev 0.24 decided. Each one builds requirements that are
already approved in the main specs, so each sets `skip_specs: true`. Each has a proposal, a design and
tasks that name the requirement IDs and the test proving each one. Build them with `/opsx:apply <change>`,
then `/opsx:verify` and `/opsx:archive --skip-specs`.

Every decision P0 builds on is accepted (ADR-D1, ADR-T1, ADR-T3, ADR-T4, ADR-T5, ADR-T7 and ADR-T8; PRD-001
rev 0.21). Building does not wait for the second reviews (rev 0.22). They run in pull requests #4
(security), #5 (statutory) and #6 (roles, approvals and payments), and until each merges, its pages show
the "Needs additional verification" banner (FR-UX-41).

## The changes, in order

| # | Change | Builds | Starts after | Blocked by the founder until | Second human reviewer |
|---|---|---|---|---|---|
| 1 | `p0-repo-skeleton-local-mode` | Layout, `mise`, dev container, the `make` contract, clock, build flavours, health endpoints, the sample-company generator (DEV-01…05, DEV-07, DEV-08, NFR-DEVX-01…03, NFR-PORT-03, NFR-IMG-06, SEC-16) | — | — | — |
| 2 | `p0-database-tenancy-migrations` | goose migrations, roles, RLS on every table, ledger tables, INV-01…06/08/09, ENG-46/47, the storage port, attack tests (QA-ENG-05) | 1 | — | Required (tenancy, ledger) |
| 3 | `p0-money-posting-engine` | Money on apd, named rounding, the pure engine, provenance, validation, the manual journal (ENG-01…04, 10…14, 22, 44) | 2 | — | Required (money, ledger) |
| 4 | `p0-python-oracle-differential` | The independent reference ledger and the differential harness (QA-ENG-01, QA-ENG-02) | 1; its harness runs once 3 posts | — | — |
| 5 | `p0-audit-chain` | AUD-01 schema, same-transaction writes, hash chain, hourly anchors, nightly verification (AUD-03, AUD-08, INV-12, CTL-AUD-02) | 2 | Staging part: 9 | Required (audit trail) |
| 6 | `p0-outbox-background-work` | Outbox, Cloud Tasks and River, the tick, jobs, heartbeats, executor isolation (§12.11, ADR-T7) | 2 | Cloud part: 9 | Recommended (executor path) |
| 7 | `p0-api-conventions-authz` | Contract-first `/v1`, problem details, idempotency, ETags, the authorisation matrix skeleton (NFR-API-01, SEC-02, QA-SEC-03, QA-API-02) | 2, 3 | — | Required (authorisation, tenancy) |
| 8 | `p0-ci-images-release` | CI gates, hardened and signed images within budget, QA-LOCAL-01, release-please with an App token, staging by digest | 1 | The GitHub App (open question 13) for REL-06; staging deploys need 9 | — |
| 9 | `p0-infra-opentofu` | GCP projects, Singapore-only policy, Cloud Run, Cloud SQL 18, secrets, KMS, WIF, the WORM bucket, budgets, drift checks | 1 | A Google Cloud organisation, billing account and projects | Recommended (IAM) |
| 10 | `p0-identity-oauth-first-run` | Hydra pinned, audience-bound tokens, the open question 40 spike, Google and Zoho sign-in with issuer checks, first-run setup and two Super admins | 2, 7 | Staging part: 9; the Zoho developer app, the Google OAuth client; ~~the Ory Enterprise Licence quote~~ (not sought, 16 Oct 2026) | Required (security reviewer) |
| 11 | `p0-compliance-as-code` | The Appendix F registry, the runner, the P0 CI checks, SEC-15/16/20 | 8 | Branch protection on `main` for CTL-CHG-01 | — |
| 12 | `p0-manual-journal-rules` | The rules a P0 manual journal and its reversal post by, decided in rev 0.24: ENG-24, the base-line rate (ENG-03), decimals as written and computed amounts out of range (ENG-44), reversals (ENG-14 and FR-GL-12's note), share capital (INV-13); the scenario format's account role and the oracle's half (engine first, then the oracle) | 3, 4 | — | Required (money, ledger) |

Changes 1 to 7 and 12 need nothing from outside, so they can be built now. Changes 8 to 11 each have a founder
step listed above. Starting those steps now keeps them off the critical path.

Progress (15 Oct 2026): changes 1 to 4 and 7 are built and archived in `openspec/changes/archive/` (pull
requests #8, #10, #13, #14, #16, #18, #22 and #23); change 5, the audit chain, is built (#15) apart from its
staging and checks-catalogue tasks; change 6, background work, is built (#17, #21) apart from its Cloud Tasks
and scale-to-zero tasks, which wait for change 9. Branch protection on `main`, the founder step for
change 11, is on: a pull request with one approval and three required checks (commit messages, design tokens,
and `make test`, which runs the database attack and race suites and the differential gate). Before
production, development pull requests are approved by a workflow and merge automatically (AGENTS.md).

## Exit criteria (§17.3) and the changes that prove them

| Exit item | Proved by |
|---|---|
| 1 · `make setup && make dev` and `make test` pass on a clean machine and in CI with no cloud credentials | 1, then every change keeps it true; 8 (QA-LOCAL-01) |
| 2 · Database invariant attack tests and the authorisation matrix skeleton green | 2 (QA-ENG-05), 7 (QA-SEC-03) |
| 3 · Audit chain written, verified nightly and anchored to a locked bucket in staging | 5, with 6 (jobs) and 9 (bucket) |
| 4 · Every image within budget, hardened, signed and deployed to staging by digest | 8, with 9 |
| 5 · The first release cut by release-please from Conventional Commits | 8 |
| 6 · OAuth server: test client token, wrong-audience rejection, MCP-audience consent, separate API token without passthrough, expired and revoked rejection, refresh-family revocation, role change within 60 s | 10 |
| 7 · Live Zoho sign-in in staging with an exact `iss` check | 10, with 9 |

## Founder decisions, 16 Oct 2026: no Google Cloud for now

There will be no Google Cloud organisation for some time, so P0 is tested completely in local mode first.

- **P0 closes on local evidence.** The parts of exit items 3, 4 and 7 that need staging move to a new gate
  before the first cloud deployment. That changes §17.3, so it waits for a PRD revision; until it lands,
  the table above keeps the PRD's wording.
- **No Ory Enterprise Licence.** Fyniq stays on the Apache-2.0 Hydra. Option (b) of open question 40, the
  JWT-bearer grant, is the path for P1's MCP work, still subject to the MCP red-team suite (QA-MCP-01) and
  the security review (#4) (`docs/adr/ADR-T3-mcp-delegation-spike.md`).
- **Zoho stays with the cloud.** In the cloud, Zoho signs in through Identity Platform, so local mode keeps
  its development sign-in page; there is no direct Zoho path to test on a laptop.

What the local evidence shows, item by item:

| Exit item | Shown locally | Moves to the gate before the first cloud deployment |
|---|---|---|
| 1 | `make test` offline, and CI with no cloud credentials (QA-LOCAL-01 runs `make up` from the local images) | — |
| 2 | The attack suites (QA-ENG-05) and the authorisation matrix (QA-SEC-03) in `make test` | — |
| 3 | The chain is written in the same transaction and checked by the nightly `audit.VerifyAll`. Anchors are signed (local Ed25519, and the Cloud KMS adapter against a fake) and written once (the local store, and the Cloud Storage adapter against fake-gcs-server) | The retention-locked bucket in staging, and an attempted delete |
| 4 | Every image within its budget and hardened (`make images`). Each release image is signed, its SBOM attested and both verified by digest (`make image-sign`) | Keyless signing into Artifact Registry, and the deployment to staging by digest |
| 5 | — | Not the cloud: release pull request #3 (0.1.0) waits for the GitHub App (REL-06) |
| 6 | `TestOAuthTokensAreAudienceBound`, `TestMCPDelegationOptionB` and `TestTokenLifecycle` against the pinned Hydra | — |
| 7 | The exact `iss` check (FR-ADM-08) against the local issuer and a fake of Identity Platform | The live Zoho sign-in |

The tasks that wait for the cloud stay open in their changes until the PRD revision names the new gate.
Then they move into one change for the first cloud deployment, and the P0 changes are archived.

## Rules for building P0

- **The oracle is written independently.** A different person, or a separate agent session, writes
  `oracle/` from PRD-001 §10 and Appendix B (and the workbook's `reference.py`) without reading the engine
  in `internal/domain/ledger`. The pull request says who wrote it (QA-ENG-01: "not by translating the
  engine code").
- **Second human reviewer.** Code that touches money movement, payee bank details, approvals, the audit
  trail, tenancy or the AI/MCP write path needs a second human reviewer, whoever wrote it (AGENTS.md).
  (14 Oct 2026: waived before production by the founders; development pull requests are auto-approved
  and auto-merged when the required checks pass. The column above records where a person should still
  look, and the rule returns before the first production deploy.)
  Rev 0.22 lifted the spec-review gate on building, not this one.
- **Requirement IDs in tests** (`// req: <ID>`), and `make check` and `make test` green before every
  merge (QA-TRACE-01, golden rule 14).
- **When building shows a requirement is wrong, stop** and change the PRD first (fyniq-openspec,
  Recipe D).

## Notes found while planning

- FR-ADM-06 (first-run setup and the Super admin) is Phase P0 in its requirement row, while §18's summary
  lists "first-run Super admin" under P1. The plan follows the requirement row; the roadmap summary
  should say P0 in the next PRD revision.
- FR-INT-06…11 (versioning, problem details, idempotency, concurrency, lists, long operations) are P1
  requirements. P0 builds and tests their middleware (change 7) without claiming the requirements.
- FR-UX-41's banner is P1, built with the web app's screens. The review register and its check
  (`make review-check`) already exist (rev 0.22).
- Two release pull requests are open (#1 and #3, both "release 0.1.0"). release-please should own one;
  close the stale one before change 8 switches its token.
- `openspec/config.yaml` had an unquoted `: ` in a tasks rule, so the CLI ignored the tasks rules; this
  plan's pull request quotes it.

### For the next PRD revision (16 Oct 2026)

The founder decisions above change PRD-001, which another session edits. The revision should:

- **§17.3:** close P0 on local evidence, and move the staging clauses of items 3, 4 and 7 to a new gate
  before the first cloud deployment (the table above lists the clauses).
- **Open question 40 (§20, and its row in the decisions table):** record the decision. No Enterprise
  Licence; option (b) on the Apache-2.0 build for P1's MCP work, subject to QA-MCP-01 and the security
  review; option (c) is the fallback. §12.6's cost note on the licence then no longer applies.
- **CTL-CHG-04:** its automated check is Binary Authorization's, so it belongs to the same cloud gate; no
  change to the row is needed if the gate says so.
