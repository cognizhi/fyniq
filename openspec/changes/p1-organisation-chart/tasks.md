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

- [ ] 2.1 Profile, financial year, base currency, time zone and GST status with effective dates and the accounting basis. Tests `// req: FR-ORG-01`, `// req: FR-ORG-02` and `// req: FR-ORG-03`, including a short or long first year, and refusing cash basis until its rules are pinned
  (19 Oct 2026, the profile, financial year and time zone: migrations 000018 and 000019 add them to `fyniq.organisation`,
  checked by the database (UEN, lengths, a first year that ends after the books start, on the day before a year starts);
  `GET` and `PATCH /v1/organisation/profile` under the new Appendix D row (PRD-001 rev 0.29: the Owner approves, the Admin
  and the Controller change, the Employee has none); every change at the version read, audited with bank and PayNow
  numbers masked; first-run setup defaults the base currency to SGD. No PRD rule bounds the first year's length, so none
  is imposed. **Left:** the GST status with its effective dates, the return frequency and the accounting basis (FR-ORG-03),
  as the first effective-dated family with `WITHOUT OVERLAPS` keys; documents taking their payment details and footer from
  the profile arrive with the templates (`p1-templates-email`))
- [ ] 2.2 Defaults and gap-free number sequences per document type, locked in the posting transaction. Test `// req: FR-ORG-04` races two postings on independent connections and finds no gap and no repeat
- [ ] 2.3 Feature switches, region and retention, and the recommended defaults with reset and effective dates. Tests `// req: FR-ORG-06`, `// req: FR-ORG-07` and `// req: FR-ORG-08`; changes under FM-1 wait for `p1-approval-engine`
- [ ] 2.4 Logo and brand: SVG and PNG, light and dark, the icon and the accent colour, through the blob-store port with SVG sanitising. Test `// req: FR-ORG-09` covers G4-099's hostile files, and an issued document keeps its logo version

## 3. Chart of accounts and dimensions

- [ ] 3.1 Pre-load the workbook's 83-account chart from data, with every attribute. Tests `// req: FR-COA-01` and `// req: FR-COA-02`; the accountant reviews the data file
- [ ] 3.2 System accounts by role, deactivation, the approved journal-preserving merge, and the hierarchy with posting only to leaves. Tests `// req: FR-COA-03`, `// req: FR-COA-04` and `// req: FR-COA-05`, each attacked as the application role
- [ ] 3.3 Dimensions and dimension sets on journal lines. The balances cache is keyed by dimension set, and the INV-08 rebuild and the oracle's half are extended. Tests `// req: FR-DIM-01` and `// req: FR-DIM-02`; the differential gate is green

## 4. Contacts and items

- [ ] 4.1 Contacts with roles and details, live balances, financial-year totals, ageing and a timeline. Tests `// req: FR-CT-01` and `// req: FR-CT-02` cover the balances P0's ledger holds
- [ ] 4.2 Vendor bank details as versioned high-risk data. A pending version freezes payments and notifies every founder through the outbox; values are masked in logs. Test `// req: FR-CT-03`; approval waits for `p1-approval-engine`, and until then the change is refused, never applied
- [ ] 4.3 Duplicate detection on name, UEN, email and bank account (`// req: FR-CT-04`), and items with prices per currency (`// req: FR-ITM-01`)

## 5. API and screens

- [ ] 5.1 Add every operation to the contract with `make openapi`, then regenerate the client; the authorisation-matrix rows for each new endpoint pass (QA-SEC-03)
- [ ] 5.2 Build the screens as designed:
  - chart-of-accounts, contact-record, contacts-list, items, logo-brand and organisation-settings;
  - help keys, tooltips and a page in `docs/user-guide/` for each, compiled into `kb/` (`make kb-index`, `make kb-lint`).
- [ ] 5.3 Finish with `make check` and `make test`
