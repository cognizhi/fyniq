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
| `fyniq_worker` | `worker` and the jobs | Read; post scheduled entries; make the scheduler's draft journals only through `create_scheduled_draft`, and move a recurring journal's next occurrence (rev 0.60); save the scheduler's month-end revaluations only through `create_scheduled_revaluation` (rev 0.75); verify and rebuild the balances cache; delete expired idempotency keys and ended rate-limit windows; list the organisations (`tenants`) and read the audit trail to anchor and verify it |
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
`installation_org` and `count_super_admins` (the API and the worker), `super_admin_users` (the
worker, to tell the Super admins of a failed sending domain, 000107) and
`issue_deployment_recovery` (the migrator: the two-person recovery,
`docs/runbooks/super-admin-recovery.md`). At most two Super admins in P0 and never none
(`super_admin_guard`); a Super admin is removed, never changed or deleted. The web app's first-party
sessions (000013): `user_session` (each session token as its SHA-256, with the sign-in identity, the
provider and the step-up time), reached only through `start_session`, `session_for`, `end_session` and
`end_user_sessions` (the API); `session_for` reads the identity's user, organisation and live roles on
every request, one organisation at a time under its row-level security.
Passkeys (000045, FR-ADM-02, SEC-01): `webauthn_credential` (a person's WebAuthn credentials: the COSE public key, the
signature counter, revoked once) and `webauthn_challenge` (32 random bytes bound to a session and a purpose, taken once
before they expire and answered once), reached only through `webauthn_challenge_issue`, `webauthn_challenge_take`,
`webauthn_credentials`, `webauthn_credential_register`, `session_step_up`, `webauthn_credential_revoke` and
`session_step_up_method` (the API); registering and revoking are audited in the installation's trail
(`require_passkey_audit`), and a counter never goes back (`webauthn_credential_guard`).
The statutory packs (000033, ENG-30): `statutory_pack_version` (a version's metadata, sources and
SHA-256 digest) and `statutory_pack_row` (its tables and pinned cases, each row as an object of strings,
so decimals stay exact), read by every service through `statutory_pack_versions` and
`statutory_pack_rows`, and written only by the migrator, which loads the versions a release ships
(`packs/`, after the migrations); a loaded version never changes or goes (`statutory_pack_immutable`,
FY110) and no value marked `[verify]` is stored. Each organisation's activation of a version is a tenant
row, `statutory_pack_activation` (000034): insert-only, audited, approved by someone other than its
proposer (SOD-10); no role may insert one until the approval engine lands.
The daily exchange-rate feed (000070, FR-GL-05): `fx_feed_rate` (one rate per base currency, currency and date, as
published and under `RND-RATE`), read by every service through `fx_feed_rates` and written only by the worker's feed
job through `record_feed_rate`; a feed rate never changes or goes, and a different rate for a stored date is refused
(`FY168`).
The registry of email-in address names (000102, FR-INT-22): `capture_name` (a name, its owning organisation for good,
and its live address there and that address's member, if any), written only by `capture_address`'s trigger and read only through `capture_route`, which
the receiver of inbound mail calls before any tenant is set; no role holds a grant on it, and a name never passes to
another organisation (`FY183`).
The installation's banners (000108, FR-NTF-03): `installation_announcement` (a Super admin's banner to everyone),
published and withdrawn only through `publish_installation_announcement` and `withdraw_installation_announcement`, which
refuse anyone but an active Super admin (`FY085`), and read only through `installation_announcements` (the API); a
banner changes only in being withdrawn, once (`FY190`). People's dismissals of them are tenant rows in
`announcement_dismissal`, whose trigger reads this table to refuse a banner that cannot be dismissed (`FY192`).
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
| INV-05 minor-unit scale, RND-FX base amounts | Row CHECKs; (rev 0.72) a base-only line (a foreign currency, amount 0) passes the row check and `journal_line_base_only`, deferred to commit, accepts it only from `fx_revaluation`, on an account marked `revalue`, offset by base-currency lines on the unrealised-FX account (`FY008`); `account_revalue_monetary` marks only assets and liabilities that are not prepayments or deferred revenue | `TestAttackAmountsOffScaleNaNOrOutOfRangeFail`, `TestAttackBaseOnlyLinesComeOnlyFromTheRevaluation` |
| INV-06 tenant isolation | RLS as above | `TestEveryTenantTableForcesRowLevelSecurity`, `TestAttackCrossTenantReadsAndWritesFail`, `TestAttackPooledConnectionWithoutTenantSeesNothing` |
| INV-08 balances cache = lines | `journal_line_balances` in the posting statement; `balance_differences()`, `rebuild_balances()` | `TestRandomPostingsKeepTheCacheAndTrialBalanceExact` |
| INV-09 trial balance nets to zero | Follows from INV-01 | the same test |
| ENG-46 gap-free ledger revision | `journal_entry_assign` takes the next revision under the revision row's lock; `ledger_revision_guard` | `TestRaceRevisionsAreGapFreeAndAsAtIsStable` |
| ENG-47 lock order | The source document `FOR UPDATE`, then the revision row, then the period, then the audit chain head (events are written just before commit); `lock_ledger()` for close and reopen; `period_guard` records the revision at close | the race tests |
| INV-12 audit chain | `audit_event_append` chains every event, `SHA-256(prev_hash ‖ body)`, under the chain head's lock; events and the head are immutable (`FY060`); format in `contracts/audit/` | `TestTamperingWithTheTrailIsDetected`, `TestTheChainStaysLinearUnderConcurrentWrites` |
| AUD-08 no change without its event | `require_audit` constraint triggers at commit (`FY061`); no role can update, delete or truncate the trail, or turn triggers off | `TestAttackChangesWithoutAnAuditEventFail` |
| Outbox (§12.11, ADR-T7) | `outbox` rows are written in the change's transaction; `outbox_guard` keeps the state machine pending → dispatched → running → done, failed, unknown_outcome or dead (dead → pending for a manual replay), and rows are never deleted | `TestOutboxRowsCommitWithTheirChangeOrNotAtAll`, `TestADeliveredTwiceTaskHasOneEffect` |
| INV-13 share capital only from the register or the opening balances (rev 0.24, from P0; rev 0.49) | `account.role = 'share-capital'` (000014), set at creation and fixed by `account_guard` (`FY040`); `journal_line_check` refuses a line to such an account from any source but `share_issue` and `opening_balance` (`FY007`, an allow list since 000054) | `TestAttackManualJournalToShareCapitalFails`, `TestAttackOpeningBalancesAreOneAndOnlyTheyReachShareCapital` |
| Documents (§11.4, FR-GL-12) | `document_version` rows immutable; `document_guard`: draft → posted → reversed only, type fixed, version only forward; the current version must exist (deferred FK) | `TestAttackDocumentVersionsAndStatusesCannotBeRewound` |
| Preparer and approver (FR-GL-01, rev 0.48) | `document_version.created_by` required on every new version (`document_version_created_by`, `FY053`); `document.posted_by` and `posted_at` set with the move from draft to posted and never changed (`document_guard`, `FY053`) | `TestAttackDocumentActorsAreRecordedOnce` |
| Reversals of posted documents (FR-GL-12, rev 0.55) | `document_reversed_posted`, deferred to commit: a move from posted to reversed needs the reversal of the current version's entry (`FY056`); the worker may move a status for the scheduler's automatic reversals (FR-GL-02) | `TestAttackAnEditReversesAndPostsTogether`, `TestAutomaticReversals` |
| Recurring journals (FR-GL-02, FR-AUT-02, rev 0.60) | `create_scheduled_draft`, the worker's one way to make a document: the version-1 draft journal of an active recurring journal's next occurrence, from its current version, saved by `scheduler:journals.recurring`, once (`FY057`); a recurring journal's content versions never change or go (`recurring_journal_version_immutable`, `FY058`) |
| Recurring bills (FR-AP-09, FR-AUT-02, rev 0.84) | `create_scheduled_bill_draft`, the worker's one way to make a bill: the version-1 draft bill of an active recurring bill's next occurrence, from its current version, saved by `scheduler:bills.recurring`, once (`FY057`); a recurring bill's content versions never change or go (`recurring_bill_version_immutable`, `FY058`) and hold no date, due date or supplier's number | `TestRecurringBillsMakeDraftsTheSchedulerCannotPost` |
| The scheduler's month-end revaluation (FR-GL-04, FR-AUT-02, rev 0.75) | `create_scheduled_revaluation`, the worker's one way to make a revaluation: a posted `fx_revaluation` of an open period whose `period_id`, last day and day after are the payload's, saved and posted by `scheduler:fx.revalue` (`FY169`); its entry is appended under the ledger's own checks (`FY008` for base-only lines) | `TestTheSchedulerRevaluesAnEndedMonth` |
| Releases of a spread cost (FR-GL-03, ENG-05, rev 0.83) and of spread revenue (rev 0.118) | `journal_entry_bill_release`: a `bill_release` entry, not its reversal, names a posted bill at its current version; `journal_entry_invoice_release`: an `invoice_release` entry, not its reversal, names an issued invoice at its current version (`FY172`); `document_edit_posted` and `document_reversed_posted` do not count a release as the document's own entry (`FY055`, `FY056`) | `TestTheSchedulerReleasesASpreadBill`, `TestTheSchedulerReleasesASpreadInvoice` |
| Purchase orders (FR-AP-01, rev 0.87) | The document type `purchase_order` posts nothing: `journal_entry_not_purchase_order` refuses an entry naming one (`FY173`); approving it posts the document with who approved it (`FY053`), and closing it reverses the document with no entry, which `document_reversed_posted` allows for an order alone (`FY056`); an approved order's version never moves without an entry (`FY055`) | `TestPurchaseOrdersAreBilledByQuantity` |
| Withholding decisions (FR-AP-19, rev 0.90) | `withholding_decision_immutable`: the tax agent's decision for a supplier and nature of payment never changes or goes (`FY174`); a later one replaces it from when it is recorded, and a rate goes with withholding only | `TestWithholdingIsDecidedAndPostedWithTheBill` |
| Payment plans (FR-AP-13, rev 0.93) | `bill_payment_plan_immutable`: a bill's planned payment date never changes or goes (`FY175`); a later plan replaces it, and one with no date unschedules the bill | `TestBillPaymentsArePlannedAndReminded` |
| Document templates (FR-TPL-04, rev 0.95) | `document_template_guard`: a template keeps its kind and type and is never deleted; its version moves forward by one; its latest published number moves forward by one, only to a version that exists; a version is its template's next number, read under the template's lock, republishes only an earlier one, and never changes or goes (`FY176`) | `TestTemplateVersionsNeverChange` |
| Template choices (FR-TPL-05, rev 0.99) | `document_template_choice_guard`: a type's default or a contact's override never changes or goes (`FY177`), and names only a published template of its own kind and document type, read FOR SHARE (`FY178`); the latest is in force | `TestTemplateChoicesAreAppendOnly` |
| The accounting framework and policies (FR-ORG-10, rev 0.62) | `accounting_version_guard`: a framework or policy version's content never changes and a version never goes; its confirmation or approval is recorded once and never withdrawn (`FY160`), and never by whoever wrote that version (`FY161`); the API's role may write only the signature columns |
| Edits of posted documents (FR-GL-12, rev 0.54) | `document_guard`: a posted document's version moves forward only with who posted the edit and when set anew (`FY053`), and a reversed document never changes (`FY055`); `document_edit_posted`, deferred to commit: the new version has its entry and the entry of the version it replaced is reversed (`FY055`) | `TestAttackAnEditReversesAndPostsTogether` |
| The close (FR-GL-07, FM-4, AUD-04; rev 0.50) | `period_close_check` and `period_close` never change (`FY152`); `period_close_guard` refuses an attestation by the one who ran the checks, on unclean checks or another period's, or before them (`FY153`); `period_close_snapshot_hash` recomputes the snapshot's SHA-256; the stale-check refusal (G4-053) runs in the API under the ledger lock | `TestAttackTheCloseIsAttestedByAnotherOnItsOwnChecks`, `TestRaceBackDatedPostingAgainstTheAttestedClose` |
| Reopening (FR-GL-08, FM-1; rev 0.51) | `period_reopen` never changes (`FY154`); `period_reopen_guard` ties it to the period's latest close and an approved request to reopen that period, signed by the one who reopens it (`FY155`); `period_reopen_after_hash` recomputes the after snapshot's SHA-256 | `TestAttackAReopeningIsTheApprovedRequest` |
| Variance explanations (FR-GL-14; rev 0.65) | `period_variance_explanation` is append-only: `period_variance_explanation_guard` refuses an update, a delete or a truncate, even for the table's owner (`FY163`), and an explanation in a period that is not open (read FOR SHARE, so it never interleaves with the close) or of an account that is not an income or expense account (`FY162`); the totals it explains are checked against the balance service by the domain, which also judges whether it is current | `TestAttackVarianceExplanationsAreAppendOnlyAndOnlyInAnOpenPeriod` |
| Exchange rates (FR-GL-05, `RND-RATE`; rev 0.70) | `fx_rate` keeps the organisation's rates for a day and its monthly table: `fx_rate_guard` refuses a rate of the base currency or a monthly rate not on its month's first day (`FY166`), and any change but one withdrawal with who, when and why, even for the table's owner (`FY167`); `fx_rate_day_current` keeps one current rate per currency and day; `fx_rate_rnd_rate` and `fx_feed_rate_rnd_rate` check each rate against its quote and form; the feed as above (`FY168`) | `TestAttackExchangeRatesAreWithdrawnNeverChanged`, `TestTheDailyFeedIsTheWorkersAndNeverChanges` |
| The close checklist (FR-GL-07; rev 0.68) | `close_checklist_template` versions and `close_checklist_entry` marks never change or go, even for the tables' owner (`FY165`); `close_checklist_guard` refuses a mark in a period that is not open, reading it FOR SHARE so a mark and the close never interleave (`FY164`); a skipped mark names its reason, and a template is one to 40 tasks | `TestAttackTheCloseChecklistIsAppendOnlyAndOnlyInAnOpenMonth` |

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
| FY008 | STORAGE-INV-05 | A base-only line comes only from the FX revaluation, on an account marked for it, against the unrealised-FX account |
| FY014 | STORAGE-ORG-01 | The base currency cannot change |
| FY021 | STORAGE-INV-06 | The entry is not in this organisation |
| FY030 | STORAGE-PERIOD-01 | No period covers the posting date |
| FY031 | STORAGE-PERIOD-02 | A period's dates cannot change |
| FY040 | STORAGE-ACCOUNT-01 | An account's code and type cannot change |
| FY041 | STORAGE-ACCOUNT-02 | The account tree is invalid (parent not a group, a cycle, a group with postings) |
| FY046 | STORAGE-REV-01 | The ledger revision only increases by one |
| FY050, FY052 | STORAGE-DOC-01 | A document's type cannot change, nor its version go back |
| FY051 | STORAGE-DOC-02 | A document moves only draft → posted → reversed |
| FY053 | STORAGE-DOC-03 | A document version names who saved it; a posting names who posted it and when, set when it posts or its edit posts, and never otherwise |
| FY054 | STORAGE-DOC-01 | The opening balances document is one per organisation, of its own type, and never changes or goes |
| FY055 | STORAGE-DOC-04 | An edit of a posted document reverses the entry it replaces and posts its new version in the same transaction; a reversed document never changes |
| FY056 | STORAGE-DOC-04 | A posted document becomes reversed only with the reversal of its entry in the same transaction |
| FY057 | STORAGE-RECURRING-01 | The scheduler makes the draft of an active recurring journal's next occurrence only, once |
| FY058 | STORAGE-RECURRING-01 | A recurring journal's content version never changes or goes |
| FY152 | STORAGE-CLOSE-01 | A record of the close (a run of the checks, a close) never changes or goes |
| FY153 | STORAGE-CLOSE-01 | A close is attested by someone other than the one who ran its checks, on clean checks of the same period, after them |
| FY154 | STORAGE-CLOSE-01 | A reopening never changes or goes |
| FY155 | STORAGE-CLOSE-01 | A period reopens on its latest close, by an approved request to reopen it, signed by the one who reopens it |
| FY160 | STORAGE-ACCOUNTING-01 | A framework or policy version never changes or goes; it is confirmed or approved once, never withdrawn |
| FY161 | STORAGE-ACCOUNTING-01 | The writer of a framework or policy version never confirms or approves it |
| FY162 | STORAGE-CLOSE-02 | A variance is explained only while its period is open, and only for an income or expense account |
| FY163 | STORAGE-CLOSE-02 | An explanation of a variance never changes or goes: a new one replaces it |
| FY164 | STORAGE-CLOSE-03 | A checklist task is marked only while its period is open |
| FY165 | STORAGE-CLOSE-03 | A checklist template version or mark never changes or goes: save a new version, or mark the task again |
| FY166 | STORAGE-FX-01 | An exchange rate is never of the base currency, and a monthly rate is dated on its month's first day |
| FY167 | STORAGE-FX-01 | An organisation's exchange rate never changes or goes; it is withdrawn once |
| FY168 | STORAGE-FX-01 | A rate of the daily feed never changes or goes |
| FY169 | STORAGE-FX-02 | The scheduler saves a revaluation only of an open period, as that period's |
| FY172 | STORAGE-RELEASE-01 | A release of a spread names a posted bill or an issued invoice at its current version |
| FY173 | STORAGE-PO-01 | A purchase order posts nothing: no entry names one as its source |
| FY174 | STORAGE-WHT-01 | A withholding decision never changes or goes: a later decision for the supplier and nature replaces it |
| FY175 | STORAGE-PLAN-01 | A bill's payment plan never changes or goes: a later plan replaces it |
| FY176 | STORAGE-TEMPLATE-01 | A template keeps its kind and type and is never deleted; a published version is its template's next and never changes or goes |
| FY177 | STORAGE-TEMPLATE-01 | A template choice (a default or a contact's override) never changes or goes: choose again |
| FY178 | STORAGE-TEMPLATE-01 | A template choice names a published template of its own kind and document type |
| FY179 | STORAGE-VERSION-01 | An email sender choice never changes or goes: choose again, and the latest of a purpose is in force |
| FY180 | STORAGE-VERSION-01 | An email event a provider reported never changes or goes: a later event is recorded beside it |
| FY181 | STORAGE-VERSION-01 | An email suppression is only ever released, once: a later bounce suppresses the address again |
| FY182 | STORAGE-VERSION-01 | An email-in address is only ever retired, once: make a new one |
| FY183 | STORAGE-CAPTURE-01 | An email-in address name is taken: a name belongs to one organisation for good |
| FY184 | STORAGE-VERSION-01 | A decision about a sender at an email-in address never changes or goes: decide again |
| FY185 | STORAGE-VERSION-01 | A received message's status moves forward only, and what it was received as never changes |
| FY186 | STORAGE-VERSION-01 | A received message's file is only ever removed, once |
| FY187 | STORAGE-VERSION-01 | A notification event never changes or goes: only its fan-out is recorded, once |
| FY188 | STORAGE-VERSION-01 | A notification changes only in being read, unread or archived (for good), and in the email that carried it |
| FY189 | STORAGE-VERSION-01 | A notification digest or preference never changes or goes: add a new one, and the latest is in force |
| FY190 | STORAGE-VERSION-01 | A banner changes only in being withdrawn, once: withdraw it and publish another |
| FY191 | STORAGE-VERSION-01 | A banner's dismissal never changes or goes |
| FY192 | STORAGE-BANNER-01 | A critical banner, or one published as not dismissible, cannot be dismissed: it stays until it ends |
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
