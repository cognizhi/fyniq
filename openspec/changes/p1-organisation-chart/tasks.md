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
  (20 Oct 2026: migration 000028 adds `fyniq.organisation_setting`, insert-only: a setting's value from a date at the
  key's next version, audited with the value before and after; the value on a day is the latest change in effect then.
  The catalogue (`internal/domain/settings`) holds 16 settings with their recommended values, reasons and guide
  sections: the five feature switches, payment terms for invoices and bills, the capitalisation threshold, the five
  settlement tolerances, the data region (fixed by the platform; the database refuses a row) and the retention of
  records (7 years, lengthen-only, a database trigger too, FY091) and of AI conversations (365 days). `GET`, `GET` and
  `PATCH /v1/organisation/settings`, read as of a day; changes from today or later in the organisation's time zone;
  reset records the recommended value. FM-1 settings (the tolerances, the threshold, retention, the AI assistant) are
  refused, never applied, until `p1-approval-engine`: the database does not backstop that yet, and the engine's
  migration brings the executor's grant and the rule. The feature switches reach `GET /v1/session`. Guide page
  `docs/user-guide/organisation-settings.md` (draft until the accountant reviews it), compiled to `settings.organisation`
  and `settings.change`. PRD-001 rev 0.34 records the defaults, provisional until sign-off. **Left:** the self-approval
  limit (recommended default 5) and the approval policies, with `p1-approval-engine`; the evidence rules (FR-DOC-06),
  with the evidence work; the capitalisation threshold is read by nothing until the fixed assets (FR-FA-01); the
  screens, with 5.x.)
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
  (4 Nov 2026, slice 1, the database: migrations 000218 to 000220 add `dimension` (Department, Project and custom
  places 1 to 8), `dimension_value` and `dimension_set` (one row per combination, its values in ascending order, at
  most one of each dimension, `FY257`, immutable `FY002`), `journal_line.dimension_set_id` (none for a line with no
  dimension, design "Dimensions on lines") and the cache keyed by account × period × dimension set × currency; the
  posting trigger, `rebuild_balances()` and `balance_differences()` group by the set, and the balance service's
  period balances sum an account's sets, so every reader is unchanged. Tests
  `TestAttackDimensionSetsAreCanonicalAndNeverChange`, `TestAttackTheBalancesCacheKeepsEachDimensionSet`. **Left:**
  the dimensions' API and settings screen, the values on lines through the engine and every source with "required per
  account", reports by dimension (FR-DIM-02), and the oracle's half.)
  (5 Nov 2026, slice 2, the API: migration 000221 lets the API role add a dimension and its values, rename them and put
  them out of use, and the guard `dimension_guard` keeps, for every role, a dimension's kind and place and a value's
  dimension and code, one version at a time, never deleted (`FY258`). `internal/domain/dimension` checks them: Department
  and Project once each, custom ones in the first free of places 1 to 8, names unique whatever the case, codes unique in
  their dimension (DIM-01 to DIM-04). Six operations under Appendix D's "Chart of accounts, dimensions": `listDimensions`,
  `createDimension`, `replaceDimension` (If-Match), `listDimensionValues`, `createDimensionValue`,
  `replaceDimensionValue` (If-Match); a value's audit event is its dimension's (the database's `require_audit`). Tests
  `TestDimensionsAreDepartmentProjectAndUpToEightCustom`, `TestAValueHasACodeUniqueInItsDimension`, `TestDimensionsAreKeptWithTheirAuditTrail`,
  `TestAttackDimensionsKeepWhatTheyAre`, `TestDimensionsAndTheirValues` and the matrix's six rows. **Left:** the
  Dimensions tab of the chart's screen, values on lines and "required per account", reports by dimension, the oracle's
  half.)
  (5 Nov 2026, slice 2b, the screen: the chart's **Dimensions** section (`Dimensions.tsx`, under the accounts as the
  ChartOfAccounts design draws it) lists each dimension with its values in use and "n of 10 used"; an Accountant or a
  Tax agent adds a dimension of a kind still free, renames it or puts it out of use, and in **Values** adds, renames
  and puts out of use its values; everyone who sees the chart sees them. Help keys `chart.dimension-add` and
  `chart.dimension-values`; the guide's "Dimensions" and "Values of a dimension", compiled into `accounting.chart`.
  The design's "Required on" column comes with "required per account" in slice 3. Tests: `Dimensions.test.tsx`.)
  (5 Nov 2026, slice 3a, values on journal lines, on the owner's decision of rev 0.171: a line a person enters on an
  account that requires a dimension names a value of it or does not post, and the system's own postings are flagged
  instead (Appendix C.2 #60). Migration 000222 gives an account its `required_dimension_ids` (the organisation's own
  dimensions, once each: `FY259`); `createAccount` and `updateAccount` set them, each in use when added. A manual
  journal's line takes `dimension_value_ids`; the engine keeps them once each in ascending order, refuses a value the
  organisation does not have, one out of use or two of one dimension (DIM-06), and a missing required value (DIM-05; a
  dimension out of use requires nothing; opening balances are not held to it; rev 0.172: a journal whose lines on the
  account net to zero, moving amounts between values, may carry the line without one); a reversal keeps each line's values, so
  every set nets to zero, and replay compares them. The unit of work writes each combination's `dimension_set` once and
  reads a line's values back with it. Tests `TestALinesDimensionValuesAreCheckedAndKept`,
  `TestJournalLinesCarryTheirDimensionValues`, `TestJournalLinesTakeDimensionValues`. **Left:** the screens (the
  journal editor's values, the account's requirement, "Required on"), C.2 #60, values on the other documents' lines,
  reports by dimension, the oracle's half.)
  (6 Nov 2026, slice 3c, Appendix C.2 #60: the balance service reads a period's totals by account and dimension set
  (`balance.BySet`, from the new `Balances.ForPeriodBySet`); the close's finding `dimensions` (⚠) names each account
  whose movement for the month on lines with no value of a dimension it requires, in use, is not zero, with the
  dimension and the amount, and passes once a journal netting to zero on the account moves it to a value; nothing
  when no account requires a dimension in use. The catalogue's `close.dimensions` reads it. Test
  `TestTheCloseFlagsAmountsWithoutARequiredDimension`; the guide's close checks and `accounting.periods`.)
  (6 Nov 2026, slice 3b, the screens: each journal line in the journal and recurring journal editor has a choice for
  each dimension in use (and for one out of use it already has a value of), its values in use, a value out of use it
  has kept and marked; under one its account requires, "<code> needs one", the API deciding (DIM-05, the rev 0.172
  exception); the dry runs show each line's values. The account form's **Required dimensions** ticks the dimensions in
  use and keeps one out of use it already requires until it is taken off, sending the whole list. The Dimensions
  section's **Required on** lists the accounts by code. Help keys `journal.line-dimensions`,
  `chart.required-dimensions`; the guides' "Tag a line with dimensions" and "Require a dimension", compiled into
  `accounting.journals` and `accounting.chart`. Tests: `JournalPage.test.tsx`, `ChartOfAccounts.test.tsx`,
  `Dimensions.test.tsx`. **Left:** values on the other documents' lines (slice 3d), reports by dimension and the
  oracle's half (slice 4).)
  (6 Nov 2026, slice 3d, sales: an invoice's, a credit note's, a quote's and a recurring invoice's line takes
  `dimension_value_ids`; the invoice rule checks them as a person's lines (DIM-06, and DIM-05 with "Choose the line's
  <dimension>", no journal's exception) and puts them on the line's revenue, or a spread line's 2400 and both lines of
  each month it releases; the schedule the scheduler releases from keeps them. The invoice editor, shared with credit
  notes, quotes and recurring invoices, has a choice per dimension, Expert unless set or required. Help key
  `invoice.line-dimensions`; the guide's "Tag a line with dimensions", compiled into `sales.invoices`. Tests
  `TestAnInvoicesLinesCarryTheirDimensionValues`, `TestInvoiceLinesCarryTheirDimensionValues`,
  `TestInvoiceLinesTakeDimensionValues`, `invoices.test.tsx`. **Left:** purchases, bank lines and claims.)
  (6 Nov 2026, slice 3d, purchases: a bill's, a vendor credit's and a recurring bill's line takes
  `dimension_value_ids`, checked as the invoice's are; its cost, or a spread line's 1210 and both lines of each month
  it releases, carry them, and the schedule the scheduler releases from keeps them. The bill editor's line card has a
  choice per dimension, Expert unless set or required. Help key `bill.line-dimensions`; the guide's "Tag a line with
  dimensions", compiled into `purchases.bills`. Tests `TestABillsLinesCarryTheirDimensionValues`,
  `TestBillLinesCarryTheirDimensionValues`, `TestBillLinesTakeDimensionValues`, `bills.test.tsx`. A purchase order's
  lines take none: a bill made from one is tagged on the bill. **Left:** bank lines and claims.)

## 4. Contacts and items

- [ ] 4.1 Contacts with roles and details, live balances, financial-year totals, ageing and a timeline. Tests `// req: FR-CT-01` and `// req: FR-CT-02` cover the balances P0's ledger holds
  (20 Oct 2026, the records: migration 000026 adds `fyniq.contact` (customer, vendor or both, with every FR-CT-01
  detail, mutable at its version, audited, never deleted) under the new Appendix D row "Contacts and items" (PRD-001
  rev 0.33). `GET`/`POST /v1/contacts`, `GET`/`PATCH /v1/contacts/{contact_id}`. **Left:** the live balances, the
  financial-year totals, the ageing and the timeline (FR-CT-02): P0's ledger lines carry no contact, so they come with
  the sales and purchase documents that post to one.)
- [x] 4.2 Vendor bank details as versioned high-risk data. A pending version freezes payments and notifies every founder through the outbox; values are masked in logs. Test `// req: FR-CT-03`; approval waits for `p1-approval-engine`, and until then the change is refused, never applied
  (20 Oct 2026: waits for `p1-approval-engine` and the notifications. A pending version must freeze payments and reach
  every founder, and neither exists yet; storing bank details with no way to approve them would only hold personal data
  for nothing.)
  (27 Oct 2026: both exist now. The owner's decisions (PRD-001 rev 0.152): one approved account per currency, Singapore
  or overseas; until batches, planning a payment date is refused and recording one warns; account numbers sealed with
  their last four digits and a keyed fingerprint; the full number revealed after step-up, audited. First slice: the
  `Cipher` port (SEC-11), `internal/fieldseal`, its local adapter and `gcp.KMSFields` on the KMS keys `fields` and
  `fingerprints` (`infra/gcp/modules/environment/fields.tf`, [live-verify] in the dev cloud). Tests:
  `TestTheEnvelopeSealsBoundToTheRecord`, `TestFieldKeysConformance`, `TestKMSFieldsConformance`. Second slice (rev
  0.153): the change as a `payee_bank_change` document with its sealed `payee_bank_account` (migrations 000180, 000181;
  FY237, FY238), ten operations under "Vendor bank details" and "Evidence", the required letter, the freeze on planning
  (and ⚠ on recording) a payment, the founders' security notice, approval through the engine by someone who neither
  prepared nor asked for it (`payee_bank_change.approve`), the reveal. Tests: `TestAChangeOfBankDetailsNeedsASecondPerson`,
  `TestABankDetailsChangeIsChecked`, `TestAttackBankDetailsChangeOnlyByApproval`, `TestAttackPayeeBankAccountsKeepTheirChange`,
  `TestAChangesAccountIsChecked`, `TestANumberIsSealedForItsChange`, the matrix's rows. Threat model
  `docs/security/threat-models/vendor-bank-details.md`. Third slice: the screens. Contacts gives each vendor its
  **Bank details** (the accounts in force, masked, with **Show number**; the changes and the freeze), the change's editor
  with its evidence and **Ask for approval** (`screens/contacts/BankDetails.tsx`), and the approval page shows both
  numbers beside the letter (`CompareNumbers`). Guide `contacts-and-items.md` (Bank details, Change a supplier's bank
  details, See the full number), compiled into `contacts.manage`. Tests `bankDetails.test.tsx`, `approvals.test.tsx`.
  Not built: the contacts list's chip for a vendor whose change waits (the record page arrives with FR-CT-02); payment
  batches refusing a vendor whose change waits come with `p1-payments-out`.)
- [ ] 4.3 Duplicate detection on name, UEN, email and bank account (`// req: FR-CT-04`), and items with prices per currency (`// req: FR-ITM-01`)
  (20 Oct 2026: duplicates on the name (case and spacing aside), the UEN or tax ID and the emails are flagged on every
  contact read and write, never refused; the bank account joins them with task 4.2 (27 Oct 2026: built, rev 0.153: the
  same account in force at another vendor is a warning on the change, PAYEE-17). Items, 20 Oct 2026: migration 000027
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

  (20 Oct 2026, in part: organisation-settings as Settings › Organisation (`apps/web/src/screens/settings/Organisation.tsx`,
  Expert mode): the profile with Edit the profile (only the fields changed, over the organisation's ETag), and every
  setting by group with Recommended or Changed from recommended, Needs a second approver and Set by the platform; Change
  sets a value from a day, today or later, with Preview (`Prefer: return=preview`), Save or Ask for approval (202), and
  Reset to recommended, showing the earlier values. Tests `Organisation.test.tsx`. Then chart-of-accounts as
  Accounting › Chart of accounts (`screens/accounting/ChartOfAccounts.tsx`, Expert): the accounts by type, each group's
  accounts under it, with report group, function, GST code and flags, system accounts locked; an empty chart offers
  the standard chart; an Accountant or Tax agent adds an account and changes one (only the fields changed, over its
  ETag), with the guide page `chart-of-accounts.md` compiled into `accounting.chart`. Tests
  `ChartOfAccounts.test.tsx`. Then contacts-list and items (`screens/contacts/Contacts.tsx`, `Items.tsx`): customers and
  vendors filtered by role and found by name, UEN or email, a possible duplicate flagged (a preview lists them before
  an add, FR-CT-04), adding and changing a contact over its ETag; items with a unit price per currency as exact
  decimals; guide page `contacts-and-items.md` compiled into `contacts.manage`. Tests `contacts.test.tsx`. **Left:**
  contact-record's tabs (documents, ageing, timeline, bank details, and its templates, which Contacts' Templates sets
  meanwhile: `p1-templates-email` 4.1, FR-TPL-05) and logo-brand; balances and the year's figures on
  contacts and items, which come with the sales and purchase documents; on chart-of-accounts, merge, the Dimensions
  tab and balances, which have no operations yet; the organisation-settings sections whose operations do not exist
  yet (brand, numbering, GST status on this page).)
- [ ] 5.3 Finish with `make check` and `make test`
