# P0 · Foundation: the build plan

P0 builds the skeleton that every later phase stands on: local mode, the database backstop, the ledger
engine and its independent oracle, the audit chain, background work, the API's conventions, identity,
images and releases, infrastructure and controls as code (PRD-001 §18, the P0 column). It is done when
the seven exit criteria of §17.3 hold, with evidence in the release record.

The plan is eleven OpenSpec changes in `openspec/changes/p0-*`. Each one builds requirements that are
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
| 10 | `p0-identity-oauth-first-run` | Hydra pinned, audience-bound tokens, the open question 40 spike, Google and Zoho sign-in with issuer checks, first-run setup and two Super admins | 2, 7 | Staging part: 9; the Zoho developer app, the Google OAuth client, the Ory Enterprise Licence quote | Required (security reviewer) |
| 11 | `p0-compliance-as-code` | The Appendix F registry, the runner, the P0 CI checks, SEC-15/16/20 | 8 | Branch protection on `main` for CTL-CHG-01 | — |

Changes 1 to 7 need nothing from outside, so they can be built now. Changes 8 to 11 each have a founder
step listed above. Starting those steps now keeps them off the critical path.

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

## Rules for building P0

- **The oracle is written independently.** A different person, or a separate agent session, writes
  `oracle/` from PRD-001 §10 and Appendix B (and the workbook's `reference.py`) without reading the engine
  in `internal/domain/ledger`. The pull request says who wrote it (QA-ENG-01: "not by translating the
  engine code").
- **Second human reviewer.** Code that touches money movement, payee bank details, approvals, the audit
  trail, tenancy or the AI/MCP write path needs a second human reviewer, whoever wrote it (AGENTS.md).
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
