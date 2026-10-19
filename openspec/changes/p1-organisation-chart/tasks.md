# Tasks

## 1. Data model claimed

- [x] 1.1 Map each of the 14 `data/data-model` requirements to the P0 test that already proves it, and tag the test (`// req: data/data-model <title>`, one tag per requirement). List what is missing in this file
  (19 Oct 2026, tagged `// req: data/data-model <title>`.)

  | Requirement | Proved by (P0) | Missing |
  |---|---|---|
  | Logical data model | `TestSchemaSnapshot` | The entities arrive with their modules; this change adds settings, dimensions, contacts and items |
  | Keys | `TestTenantKeysIndexesAndForeignKeys`, `TestAttackCrossTenantReadsAndWritesFail` | Gap-free human numbers per sequence (task 2.2) |
  | Tenancy and row-level security | `TestEveryTenantTableForcesRowLevelSecurity`, `TestTenantKeysIndexesAndForeignKeys`, `TestEachRoleHoldsExactlyItsGrants`, `TestRolesCannotBypassTheDatabase`, `TestAttackCrossTenantReadsAndWritesFail`, `TestAttackPooledConnectionWithoutTenantSeesNothing`, conformance "one organisation never sees another's rows" | The scope predicate on payroll and employee-identifier tables (with payroll) |
  | Global tables | `TestEveryTenantTableForcesRowLevelSecurity` (the explicit list), `TestEachRoleHoldsExactlyItsGrants` (now: no service role writes any global table) | Reads through column views: today the installation tables are reached through `SECURITY DEFINER` functions, which expose only what each caller needs; the published statutory packs and connector manifests arrive with their changes |
  | Versioning of records | `TestETagAndIfMatch` (documents) | Version counters and stale refusals for contacts and settings (tasks 2.1, 4.1) |
  | Insert-only ledger records | `TestAttackPostedHistoryCannotChange`, `TestPostedEntriesCarryTheirProvenance`, `TestReversalUndoesAPostedJournal`, `TestEachRoleHoldsExactlyItsGrants` | — |
  | Money columns | `TestEveryNumericColumnHasPrecisionAndRefusesNaN`, `TestAttackAmountsOffScaleNaNOrOutOfRangeFail` | — |
  | Effective-dated reference data | — | `WITHOUT OVERLAPS` keys, with the organisation's settings (task 2.1); master-data versions recording their ledger revision |
  | Idempotency keys and posting identity | `TestIdempotencyKeyReplaysAndRefusesReuse`, conformance "a posting identity is never used twice" | Imports and proposal executions, with their changes |
  | Single posting path | `TestOnlyThePostingServiceAppendsToTheLedger` (new archtest) | — |
  | Balances are a rebuildable cache | `TestRandomPostingsKeepTheCacheAndTrialBalanceExact`, conformance "balances follow the lines and rebuild to the same figures" | Keyed by dimension set (task 3.3) |
  | Accounting date and recorded time | `TestPostedEntriesCarryTheirProvenance` | — |
  | Reports as at a ledger revision | `TestRaceRevisionsAreGapFreeAndAsAtIsStable` | The reports themselves (`p1-reports`) |
  | Valid time and system time for master data | — | Effective-dated settings and account attributes (tasks 2.1, 3.2) |
- [ ] 1.2 Add what is missing:
  - effective-dated reference data with `WITHOUT OVERLAPS` keys (`btree_gist`);
  - the explicit list of global tables, which application roles may only read;
  - version counters on mutable records.

  Attack tests in `internal/adapters/postgres` attempt an overlap, a write to a global table and a stale version

## 2. Organisation settings

- [x] 2.1 Profile, financial year, base currency, time zone and GST status with effective dates and the accounting basis. Tests `// req: FR-ORG-01`, `// req: FR-ORG-02` and `// req: FR-ORG-03`, including a short or long first year, and refusing cash basis until its rules are pinned
  (19 Oct 2026, the profile, financial year and time zone: migrations 000018 and 000019 add them to `fyniq.organisation`,
  checked by the database (UEN, lengths, a first year that ends after the books start, on the day before a year starts);
  `GET` and `PATCH /v1/organisation/profile` under the new Appendix D row (PRD-001 rev 0.29: the Owner approves, the Admin
  and the Controller change, the Employee has none); every change at the version read, audited with bank and PayNow
  numbers masked; first-run setup defaults the base currency to SGD. No PRD rule bounds the first year's length, so none
  is imposed. Documents taking their payment details and footer from the profile arrive with the templates (`p1-templates-email`).)
  (19 Oct 2026, the GST status: migration 000020 adds `gst_registration`, the first effective-dated family: valid from
  `registered_from` until `deregistered_from`, an exclusion constraint on that range refusing overlaps (the
  "equivalent exclusion constraint" the data-model requirement allows, as `period_no_overlap` does), `recorded_at` and the
  version as system time, every version audited, and invoice basis only until ENG-45 pins the Cash Accounting Scheme.
  `internal/domain/gst` gives the status on a date and the return period that contains it, by frequency and the quarters'
  start month; the API lists, records, reads and changes registrations and answers `GET /v1/organisation/gst-status`. IRAS
  may set a different first return period on its notice of registration: marked `[verify]` in `gst.ReturnPeriod`)
- [x] 2.2 Defaults and gap-free number sequences per document type, locked in the posting transaction. Test `// req: FR-ORG-04` races two postings on independent connections and finds no gap and no repeat
  (19 Oct 2026, the number sequences: migration 000021 adds `document_sequence` (per document type: prefix, next number,
  digits, gap-free; its next number never goes back) and the insert-only `document_number`. A gap-free sequence numbers a
  journal in the posting's transaction under the sequence's lock, ENG-47 step 1a (PRD-001 rev 0.30); another numbers it
  when it is saved. `TestDocumentNumbers` posts twenty journals at once, a third into a closed period, and finds JNL-0001
  to JNL-0013 with no gap or repeat; a preview takes no number. `GET`, `GET` and `PATCH /v1/organisation/sequences`.
  **Left, with task 2.3:** the payment terms, the settlement tolerances (FR-BNK-08, open question 32: S$1.00, or the greater
  of S$1.00 and 3% in foreign currency, capped at S$50 a document and S$200 a month, accountant to confirm) and the
  capitalisation threshold, as recommended, effective-dated settings (FR-ORG-08); the PRD sets no default for the payment
  terms or the threshold)
- [ ] 2.3 Feature switches, region and retention, and the recommended defaults with reset and effective dates. Tests `// req: FR-ORG-06`, `// req: FR-ORG-07` and `// req: FR-ORG-08`; changes under FM-1 wait for `p1-approval-engine`
- [ ] 2.4 Logo and brand: SVG and PNG, light and dark, the icon and the accent colour, through the blob-store port with SVG sanitising. Test `// req: FR-ORG-09` covers G4-099's hostile files, and an issued document keeps its logo version

## 3. Chart of accounts and dimensions

- [x] 3.1 Pre-load the workbook's 83-account chart from data, with every attribute. Tests `// req: FR-COA-01` and `// req: FR-COA-02`; the accountant reviews the data file
  (19 Oct 2026: `internal/domain/chart/standard-chart.csv`, 86 accounts from the workbook's `src/cfh/coa.py` at
  a705fd1 with every attribute, plus B.5's 2120, 3300 and 7040, the workbook's 7040 moved to 7050 (PRD-001 rev 0.31,
  provisional until the CoA review; the file's header says what changed from the workbook). Migrations 000022 and
  000023 add FR-COA-02's attributes to `fyniq.account`. `POST /v1/accounts:load-standard-chart` loads it once, into an
  organisation with no account; it is not a side effect of setup, because `make seed-load` and the smoke test load the
  sample company's own chart into a new organisation. The first-run wizard calls it when the screens arrive. **For the
  accountant:** the data file, the payroll-cost set (rev 0.31 [verify]) and the control accounts.)
- [ ] 3.2 System accounts by role, deactivation, the approved journal-preserving merge, and the hierarchy with posting only to leaves. Tests `// req: FR-COA-03`, `// req: FR-COA-04` and `// req: FR-COA-05`, each attacked as the application role
  (19 Oct 2026: migrations 000024 and 000025 give every B.5 system account its role, one account per role, two for
  share capital, and keep a system account active (PRD-001 rev 0.32); the standard chart sets the roles. P0 already
  keeps code, type and role fixed, accounts undeletable, the hierarchy acyclic with postings only to active leaves.
  `POST /v1/accounts` and `PATCH /v1/accounts/{account_id}` add and change accounts at their version; changes to the
  control and payroll-cost flags are refused until the approval engine. Manual journals to system accounts post, as
  question 55 decided, except share capital (INV-13). **Left:** the approved journal-preserving merge (FR-COA-04), which
  needs the approval engine; the ⚠ on a manual journal to a control account (question 55), with the checks.)
- [ ] 3.3 Dimensions and dimension sets on journal lines. The balances cache is keyed by dimension set, and the INV-08 rebuild and the oracle's half are extended. Tests `// req: FR-DIM-01` and `// req: FR-DIM-02`; the differential gate is green

## 4. Contacts and items

- [ ] 4.1 Contacts with roles and details, live balances, financial-year totals, ageing and a timeline. Tests `// req: FR-CT-01` and `// req: FR-CT-02` cover the balances P0's ledger holds
  (20 Oct 2026, the records: migration 000026 adds `fyniq.contact` (customer, vendor or both, with every FR-CT-01
  detail, mutable at its version, audited, never deleted) under the new Appendix D row "Contacts and items" (PRD-001
  rev 0.33). `GET`/`POST /v1/contacts`, `GET`/`PATCH /v1/contacts/{contact_id}`. **Left:** the live balances, the
  financial-year totals, the ageing and the timeline (FR-CT-02): P0's ledger lines carry no contact, so they come with
  the sales and purchase documents that post to one.)
- [ ] 4.2 Vendor bank details as versioned high-risk data. A pending version freezes payments and notifies every founder through the outbox; values are masked in logs. Test `// req: FR-CT-03`; approval waits for `p1-approval-engine`, and until then the change is refused, never applied
  (20 Oct 2026: waits for `p1-approval-engine` and the notifications. A pending version must freeze payments and reach
  every founder, and neither exists yet; storing bank details with no way to approve them would only hold personal data
  for nothing.)
- [ ] 4.3 Duplicate detection on name, UEN, email and bank account (`// req: FR-CT-04`), and items with prices per currency (`// req: FR-ITM-01`)
  (20 Oct 2026: duplicates on the name (case and spacing aside), the UEN or tax ID and the emails are flagged on every
  contact read and write, never refused; the bank account joins them with task 4.2. Items, 20 Oct 2026: migration 000027
  adds `fyniq.item` (code, never changed; name, printed description, product or service, unit, revenue account, GST code,
  active flag; mutable at its version, audited, never deleted) and `fyniq.item_price`, one unit price per currency as
  NUMERIC(20,6) (§11.3), never NaN or below zero, replaced with the item and audited with it. `GET`/`POST /v1/items`,
  `GET`/`PATCH /v1/items/{item_id}` under the "Contacts and items" row; prices cross the API as decimal strings with
  at least the currency's decimals (`money.UnitPrice`). **Left:** the item's quantity and sales for the financial year,
  which come with the sales documents (`p1-sales`).)

## 5. API and screens

- [ ] 5.1 Add every operation to the contract with `make openapi`, then regenerate the client; the authorisation-matrix rows for each new endpoint pass (QA-SEC-03)
- [ ] 5.2 Build the screens as designed:
  - chart-of-accounts, contact-record, contacts-list, items, logo-brand and organisation-settings;
  - help keys, tooltips and a page in `docs/user-guide/` for each, compiled into `kb/` (`make kb-index`, `make kb-lint`).
- [ ] 5.3 Finish with `make check` and `make test`
