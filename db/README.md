# db: the system of record's schema

The database is the backstop (golden rule 2). Balanced entries, immutable posted history, locked
periods and tenant isolation hold here even when application code is wrong (PRD-001 §10.7, §11.3,
§11.4, ADR-D1). PostgreSQL 18 locally (`make deps-up`, port 55432) and on Cloud SQL; any
PostgreSQL 17+ that passes the conformance and attack suites (NFR-PORT-02).

| Path | What |
|---|---|
| `migrations/NNNNNN_name.sql` | goose migrations, embedded in the `migrate` binary (`embed.go`). Forward only: a merged migration is never edited; a change is a new migration (§12.10) |
| `../internal/adapters/postgres/` | The only Go code that holds SQL (NFR-PORT-01): the storage adapter, its sqlc queries (`queries/`, generated `sqlcdb/`), the migration runner and the test suites |
| `../internal/adapters/postgres/testdata/schema.golden` | The schema after every migration: tables, constraints, indexes, triggers, policies, functions |
| `../internal/adapters/postgres/testdata/grants.golden` | Every privilege each role holds (SEC-03) |

## Commands

| Command | Does |
|---|---|
| `go run -tags local ./cmd/migrate up` | Apply pending migrations to the local database (`make dev` runs it). The local build first creates `fyniq_migrator` and afterwards lets each service role log in |
| `go run -tags local ./cmd/migrate status` | List the migrations and whether each is applied |
| `make db-lint` | squawk on every migration (`.squawk.toml`) and a check that `sqlcdb/` matches the queries; part of `make test` |
| `make sqlc` | Regenerate `sqlcdb/` after changing a query or a migration |
| `go test -tags local ./internal/adapters/postgres/` | The conformance, catalogue, attack and race suites against PostgreSQL 18 in Testcontainers (needs Docker); add `-run <Test> -update` to rewrite a golden file, then review the diff |

## Roles (SEC-03)

| Role | Logs in as | May |
|---|---|---|
| `fyniq_migrator` | the `migrate` job only | Own every object, create the roles below. Created by the environment before the first run: locally by `migrate up` in the local build, in the cloud by p0-infra-opentofu |
| `fyniq_api` | the `api` service | Read the tenant tables; create organisations, accounts, periods, documents and journal entries; close and reopen periods; post and reverse documents; count requests in rate-limit windows (FR-INT-16) |
| `fyniq_worker` | `worker` and the jobs | Read; post scheduled entries; verify and rebuild the balances cache; delete expired idempotency keys and ended rate-limit windows; list the organisations (`tenants`) and read the audit trail to anchor and verify it |
| `fyniq_executor` | the proposal executor | Read; create and post the documents and entries of approved proposals |
| `fyniq_ai` | the AI orchestrator | Nothing: no table grant, no write, ever (AI-P2, CMP-AI-01). Inserting proposals arrives with the proposal tables in P1 |
| `fyniq_readonly` | investigations and reports | Read |

The exact list is `grants.golden`; `TestEachRoleHoldsExactlyItsGrants` fails on any difference, and
on any grant of TRUNCATE, REFERENCES, TRIGGER or MAINTAIN, any write by the AI or read-only role, any
UPDATE or DELETE on the journal, and any privilege held by a role outside this table. The roles are
NOLOGIN group roles: login identities and passwords never appear in a migration. Locally every role
uses the password `fyniq-local-only`, valid nowhere else (SEC-12).

## Rules every migration follows

- **Tenancy** (§11.4, INV-06): `organisation_id` on every tenant row, first in every index and in the
  primary key; `UNIQUE (organisation_id, id)`; composite foreign keys between tenant tables, because
  foreign-key checks bypass row-level security; `ENABLE` and `FORCE ROW LEVEL SECURITY` with the one
  policy `organisation_id = fyniq.current_org()`. The unit of work sets
  `set_config('app.org_id', …, true)` first in every transaction; unset, it matches no row.
- **Global tables** have no grant; services read them through views. Today: `currency` (view
  `currencies`, every service) and `tenant_directory` (view `tenants`, the worker only: the list of
  organisations the jobs run for; the API's tick reads it too), `job_slot` (view `job_slots`) and
`heartbeat` (view `heartbeats`), which the tick and the jobs change only through the functions
`claim_job_slot`, `skip_job_slots`, `started_job_slot`, `finish_job_slot`, `latest_job_slot` and
`beat` (§12.11). The installation's people and first-run setup (000010): `app_user`,
`user_identity` (sign-in identities as (issuer, subject) pairs, FR-ADM-08), `installation` (the
setup code's SHA-256 until setup is done) and `super_admin`, reached only through `claim_setup`,
`setup_done` and `user_for_identity` (the API) and `set_setup_code` (the migrator, at deployment).
The Super admins' one-time codes (000012): `super_admin_grant` (invitations, recovery codes,
recovery confirmations and deployment recoveries, each as its SHA-256, used once), reached only
through `invite_super_admin`, `accept_super_admin_invitation`, `set_recovery_codes`,
`confirm_super_admin_recovery`, `recover_super_admin` and `super_admin_for_identity` (the API),
`installation_org` and `count_super_admins` (the API and the worker) and
`issue_deployment_recovery` (the migrator: the two-person recovery,
`docs/runbooks/super-admin-recovery.md`). At most two Super admins in P0 and never none
(`super_admin_guard`); a Super admin is removed, never changed or deleted. The web app's first-party
sessions (000013): `user_session` (each session token as its SHA-256, with the sign-in identity, the
provider and the step-up time), reached only through `start_session`, `session_for`, `end_session` and
`end_user_sessions` (the API); `session_for` reads the identity's user, organisation and live roles on
every request, one organisation at a time under its row-level security.
The statutory packs (000033, ENG-30): `statutory_pack_version` (a version's metadata, sources and
SHA-256 digest) and `statutory_pack_row` (its tables and pinned cases, each row as an object of strings,
so decimals stay exact), read by every service through `statutory_pack_versions` and
`statutory_pack_rows`, and written only by the migrator, which loads the versions a release ships
(`packs/`, after the migrations); a loaded version never changes or goes (`statutory_pack_immutable`,
FY110) and no value marked `[verify]` is stored. Each organisation's activation of a version is a tenant
row, `statutory_pack_activation` (000034): insert-only, audited, approved by someone other than its
proposer (SOD-10); no role may insert one until the approval engine lands.
Add one only with its access model here and in the test's list.
- **The installation organisation** (`organisation.kind = 'installation'`, one per installation,
  created by first-run setup) holds only the installation's audit trail: setup and Super-admin events.
  Nobody is a member of it; `require_installation_audit` checks that a Super-admin change has its
  event there, and `require_grant_audit` that every code created or used has one. Every unit of work may write to more than one organisation (setup does), so
  `require_audit` looks for the event in the row's own organisation.
- **Audit** (§11.5, AUD-01, AUD-08): a migration that adds a table holding business records adds a
  `require_audit` constraint trigger to it, and the adapter records an event for every write to it.
  Audited today: organisations, accounts, periods, documents (with their versions), journal
  entries (with their lines), memberships, role assignments, Super admins and their one-time codes. Not audited, as derived data or caches: balances, the ledger revision,
  the audit chain head, idempotency keys and rate-limit windows.
- **Money** (§11.3): amounts `NUMERIC(20,4)` with the currency's minor units on the row and
  `CHECK (amount <> 'NaN' AND amount = trunc(amount, minor_units))`; FX rates `NUMERIC(20,10)`,
  positive and not NaN. Unconstrained `NUMERIC` fails the catalogue test.
- **Functions**: not executable by PUBLIC; a `SECURITY DEFINER` function pins
  `search_path = pg_catalog, fyniq, pg_temp`.
- **squawk** must pass; each migration sets `lock_timeout` and `statement_timeout`.
- A new table, numeric column or grant is checked by the catalogue tests automatically; a numeric
  column also needs an entry in `nanAttacks` (`attack_test.go`).

## The invariants here (PRD-001 §10.7)

| Invariant | Enforced by | Proved by |
|---|---|---|
| INV-01 balanced entries | `journal_entry_balanced` and `journal_line_balanced`, deferred constraint triggers at commit; at least two lines; per currency for single-currency entries | `TestAttackUnbalancedEntriesFailAtCommit` |
| INV-02 posted history immutable | No UPDATE, DELETE or TRUNCATE grant; `journal_immutable` triggers for every role, the owner included; lines join an entry only in the transaction that posted it | `TestAttackPostedHistoryCannotChange` |
| INV-03 no posting into a closed period | `journal_entry_assign` reads the period `FOR SHARE` after the ledger lock | `TestAttackPostingIntoAClosedPeriodFails`, `TestRacePostingAgainstCloseOfTheSamePeriod` |
| INV-04 active leaf accounts of the organisation | Composite foreign keys; `journal_line_check` reads the account `FOR SHARE` | `TestAttackPostingToGroupInactiveOrForeignAccountsFails` |
| INV-05 minor-unit scale, RND-FX base amounts | Row CHECKs; base-only lines are refused until FX revaluation (FR-GL-04, P1) adds its own trigger | `TestAttackAmountsOffScaleNaNOrOutOfRangeFail` |
| INV-06 tenant isolation | RLS as above | `TestEveryTenantTableForcesRowLevelSecurity`, `TestAttackCrossTenantReadsAndWritesFail`, `TestAttackPooledConnectionWithoutTenantSeesNothing` |
| INV-08 balances cache = lines | `journal_line_balances` in the posting statement; `balance_differences()`, `rebuild_balances()` | `TestRandomPostingsKeepTheCacheAndTrialBalanceExact` |
| INV-09 trial balance nets to zero | Follows from INV-01 | the same test |
| ENG-46 gap-free ledger revision | `journal_entry_assign` takes the next revision under the revision row's lock; `ledger_revision_guard` | `TestRaceRevisionsAreGapFreeAndAsAtIsStable` |
| ENG-47 lock order | The source document `FOR UPDATE`, then the revision row, then the period, then the audit chain head (events are written just before commit); `lock_ledger()` for close and reopen; `period_guard` records the revision at close | the race tests |
| INV-12 audit chain | `audit_event_append` chains every event, `SHA-256(prev_hash ‖ body)`, under the chain head's lock; events and the head are immutable (`FY060`); format in `contracts/audit/` | `TestTamperingWithTheTrailIsDetected`, `TestTheChainStaysLinearUnderConcurrentWrites` |
| AUD-08 no change without its event | `require_audit` constraint triggers at commit (`FY061`); no role can update, delete or truncate the trail, or turn triggers off | `TestAttackChangesWithoutAnAuditEventFail` |
| Outbox (§12.11, ADR-T7) | `outbox` rows are written in the change's transaction; `outbox_guard` keeps the state machine pending → dispatched → running → done, failed, unknown_outcome or dead (dead → pending for a manual replay), and rows are never deleted | `TestOutboxRowsCommitWithTheirChangeOrNotAtAll`, `TestADeliveredTwiceTaskHasOneEffect` |
| INV-13 no manual journal to share capital (rev 0.24, from P0) | `account.role = 'share-capital'` (000014), set at creation and fixed by `account_guard` (`FY040`); `journal_line_check` refuses a `manual_journal` line to such an account (`FY007`); other sources (the register of share issues, P1) may post | `TestAttackManualJournalToShareCapitalFails` |
| Documents (§11.4, FR-GL-12) | `document_version` rows immutable; `document_guard`: draft → posted → reversed only, type fixed, version only forward; the current version must exist (deferred FK) | `TestAttackDocumentVersionsAndStatusesCannotBeRewound` |

INV-07, INV-10 and INV-11 arrive with their modules in P1, as do INV-13's per-person sub-ledgers and its
nightly reconciliation; INV-12 with p0-audit-chain.

## Error codes

The database raises its own SQLSTATEs; the adapter maps them to the storage port's stable codes
(`internal/ports/storage`, ENG-23).

| SQLSTATE | Port code | Meaning |
|---|---|---|
| FY001 | STORAGE-INV-01 | The entry does not balance, or has fewer than two lines |
| FY002 | STORAGE-INV-02 | Posted history cannot change |
| FY003 | STORAGE-INV-03 | The posting date is in a closed period |
| FY004 | STORAGE-INV-04 | The account is missing, a group or inactive |
| FY005 | STORAGE-INV-05 | The currency is unknown |
| FY006 | STORAGE-INV-06 | No organisation is set for the transaction |
| FY014 | STORAGE-ORG-01 | The base currency cannot change |
| FY021 | STORAGE-INV-06 | The entry is not in this organisation |
| FY030 | STORAGE-PERIOD-01 | No period covers the posting date |
| FY031 | STORAGE-PERIOD-02 | A period's dates cannot change |
| FY040 | STORAGE-ACCOUNT-01 | An account's code and type cannot change |
| FY041 | STORAGE-ACCOUNT-02 | The account tree is invalid (parent not a group, a cycle, a group with postings) |
| FY046 | STORAGE-REV-01 | The ledger revision only increases by one |
| FY050, FY052 | STORAGE-DOC-01 | A document's type cannot change, nor its version go back |
| FY051 | STORAGE-DOC-02 | A document moves only draft → posted → reversed |
| FY060 | STORAGE-AUDIT-01 | The audit trail cannot be changed or deleted |
| FY061 | STORAGE-AUDIT-01 | A change has no audit event in its transaction |
| FY070 | STORAGE-OUTBOX-01 | An outbox row cannot change what it is, count backwards or be deleted |
| FY071 | STORAGE-OUTBOX-01 | An outbox row cannot move to that state (§12.11's contract) |
| FY080 | STORAGE-SETUP-01 | The setup code is not valid, or setup is done (no new code after it) |
| FY081, FY082 | STORAGE-MEMBER-01 | An organisation's kind cannot change, the installation organisation exists already, nobody is a member of it, or a role assignment changes instead of being revoked |
| FY083, FY084 | STORAGE-SUPERADMIN-01 | The last Super admin cannot be removed; a Super admin is removed, never changed or deleted; at most two Super admins in P0 |
| FY085 | STORAGE-GRANT-01 | An invitation, recovery code, confirmation or deployment recovery is not valid, used or expired (one answer for every reason), or the user is not an active Super admin |
| FY110 | STORAGE-PACK-01 | A loaded statutory pack version never changes and is never removed: a correction is a new version (ENG-30) |
| FY120 | STORAGE-EVIDENCE-01 | An evidence file keeps what it is (file, hashes, parent, uploader, source) and, once final, its locked copy |
| FY121 | STORAGE-EVIDENCE-01 | Evidence is removed only while its record is a draft: append-only after (FR-DOC-07) |
| FY122 | STORAGE-EVIDENCE-01 | Evidence moves attached → removed (draft) or attached → final (record not a draft), never back |
| FY123 | STORAGE-EVIDENCE-01 | An evidence file's retain-until only moves later (FR-DOC-02) |
| FY124 | STORAGE-EVIDENCE-01 | Evidence is never deleted |
| 40001, 40P01 | STORAGE-RETRY-01 | Serialisation failure or deadlock: retry with the same Idempotency-Key (ENG-47) |
| 55P03 | STORAGE-LOCK-01 | A lock wait exceeded the unit of work's lock timeout (5 s by default, ENG-47) |
