# Design

## Context

See proposal.md for why. What exists today and shapes the approach:

- **The workbook** (`zh-sheet-accounting`, the synthetic sample at commit a705fd1) has 42 sheets. Each record and
  set-up sheet has the same layout: row 4 marks the columns a person fills in (✍) and the calculated ones (⚙), row 5
  holds the headers, and data starts at row 6. A dropdown cell holds "code - name", a date an Excel serial number, and
  an amount a person typed is exact; calculated cells hold the binary float the workbook computed. The workbook also
  carries its own engine:
  - **Trial Balance**: each account's opening at the books start, its movement each month of the financial year and
    its closing at each month end;
  - **Checks**: each check by number with its status;
  - **Carry Forward**: the balances and open invoices and bills at an "as of" date, for a clean start.
- **Fyniq's import paths**: `internal/sheet` reads an `.xlsx`'s first sheet of values safely (bounded archive, exact
  decimals) for the bank statement and budget imports. Every record has a domain command shared by the API, imports and
  dry runs (ENG-22). Most commands preview a write (`Prefer: return=preview`, FR-INT-14).
- **Built modules** the workbook's sheets map to:
  - the organisation, its settings and the chart;
  - dimensions (department and project kinds);
  - contacts and items;
  - FX rates and bank rules;
  - money accounts, bank transactions, customer receipts and bill payments recorded by hand;
  - invoices with spreads, bills, quotes and orders;
  - journals, recurring invoices, bills and journals;
  - the fixed-asset register;
  - budgets, forecasts and the equity register.
- **What is missing:**
  - payroll, timesheets and employees (after go-live);
  - opening open items (FR-GL-06's unbuilt part): opening balances are one base-currency document, so an invoice
    unpaid at the books start cannot be paid and aged in Fyniq.
- **Appendix D** has a "Tenant export" row (the Owner approves with a second approver, the Administrator prepares, the
  DPO views) but none for the workbook import.

## Goals / Non-Goals

**Goals:**
- One pass from the frozen workbook to reconciled books, every record entered through the command a person's entry
  takes, so the imported books obey every rule the live ones do.
- A validation report that says, row by row, what will be imported, what is kept in the archive and what must be fixed
  first, before anything is written.
- Parity by account and month to the cent with the workbook's own Trial Balance, every difference explained in the
  register and signed by the accountant.
- An export that a person outside Fyniq can read, check against its manifest and verify.
- The migration path rehearsed end to end in a test on the synthetic sample workbook.

**Non-Goals:**
- Importing from Xero, QuickBooks Online or Zoho Books (P3).
- Payroll records, timesheets and per-person employee records (after go-live; kept in the archive).
- Merging a workbook into books that have posted entries (decided against, rev 0.254).
- Bank reconciliations' history: the first reconciliation in Fyniq starts at the cut-over; the workbook's Bank Rec
  sheet stays in the archive, and its balances appear in the parity report.

## Decisions

**D1. Read the workbook sheet by sheet, values only, within bounds of its own.**
- `internal/sheet` gains a reader of named sheets. It reads the shared and inline strings and each cell's cached value,
  as exact decimal text.
- Its bounds are sized for a workbook rather than a statement: 256 MB uncompressed in all and 64 MB per sheet read.
  It reads only the sheets the import needs.
- Columns are found by their header text on row 5, not by position, so a column moved in a later workbook release
  still reads.
- A workbook saved without its calculated values, never opened in a spreadsheet application after a change, is
  refused with how to fix it: open and save it, as M1's `make verify` does.
- Alternative: a spreadsheet library. Rejected, as for the statement import: one reader of untrusted input, bounded
  before decompression.

**D2. A typed model, then a validation report as the dry run.**
- Each sheet's rows become typed records:
  - dates from serial numbers;
  - "code - name" labels as codes;
  - amounts a person typed as exact decimals;
  - the workbook's calculated amounts rounded half away from zero to cents (`RND-IMPORT`, a new named policy).
- Validation runs the mapping (D4) and each record's command in preview, within a transaction that rolls back.
- The report lists each sheet's rows to import, its warnings (⚠, such as "kept in the archive") and its errors (✖,
  each with its row, column, what is wrong and how to fix it).
- A report with an error cannot be committed. A row the workbook itself blocked (its status ✖) is reported and not
  imported, as the workbook did not post it.

**D3. The target is an organisation with no posted entries (rev 0.254); the import is one approved operation.**
- The migration record refuses an organisation holding a posted entry.
- The commit is one request: prepared by a person who holds C on "Workbook import (migration)" (the Controller, the
  Accountant or an Owner) and approved by an Owner who did not prepare it (FM-1). The approval's envelope holds the
  workbook's SHA-256, its validation report's and the signed mapping's, so a new upload or a changed mapping before
  the approval voids it.
- (rev 0.258, the owner's decision of 13 Nov 2026) Once approved, the commit runs in batches of at most 50 records.
  Each batch is one request on the API's own database role, through the domain commands a person's request uses; each
  record is written in its own transaction with its migration record: its business key, its sheet and row, and the
  commit, which names the approval. The web app sends the batches and shows the progress; closing it pauses the
  import. Not chosen: tasks from the outbox to a new API endpoint (an internet-facing endpoint for Cloud Tasks only);
  a new `fyniq_migration` role and job (the widest new grant). No role, grant or endpoint is added for background
  work.
- Once a commit is approved, the migration's workbook and mapping are fixed: a new upload or mapping version is
  refused, by the API and by the database (FY325).
- Each record has a business key from its row, so a commit that stopped part way resumes where it stopped. A rejected
  import is abandoned with its organisation, never undone, since posted history is reversed, never deleted.
- The weekly review lists the import as one entry (FM-3).
- Alternative: an approval per document. Rejected: hundreds of approvals, and their thresholds were the workbook's
  rules, not this import's.

**D4. Mapping, reviewed and signed off (M2).**
- Accounts map by code. The workbook's chart is the origin of Fyniq's standard chart, so most map one to one. An
  account Fyniq lacks is created from the Accounts sheet: type, report group, cash-flow line, function, money account
  and default GST code.
- GST codes map by code to the `sg-gst` pack's codes.
- Departments come from the Employees sheet and projects from the record sheets' Project column, as dimension values
  (rev 0.254).
- The Budget sheet's P&L lines map to accounts: each line to its report group's first account, which a person can
  change.
- Bank rules come from the Lists sheet, in their order (first match wins in both).
- The mapping report is signed off by the Accountant, with step-up, before the commit.

**D5. What each sheet becomes, in this order.**
- Set-up:
  - the organisation's profile and settings: name, UEN, financial year, books start, base currency, GST
    registration;
  - dimensions, chart, contacts and items;
  - FX rates, by month from the Lists sheet;
  - bank rules, and money accounts (the accounts marked "Money account?").
- (As built, slice 4) Each set-up record goes through the command a person's request uses, with its rules: the profile
  and its first financial year (each month the organisation lacks); the GST registration from the books start, filing
  quarterly in quarters that start with the financial year, as the workbook keeps neither [accountant to confirm]; the
  standard chart when the organisation has none, then each account the mapping creates; the department and project
  dimensions and their values; contacts with their role, terms, category and country (the workbook's country names
  read as ISO codes); items with their base-currency price; the Lists sheet's monthly FX rates; and the bank rules in
  their order, suggesting until an approver makes one auto-apply (FR-BNK-14). Two cases are warnings of the validation
  report and left out: a UEN not in ACRA's form (the sample's placeholder), and a bank rule that gives one of the
  workbook's money accounts, money moving between accounts, which Fyniq records as a transfer the bank import matches.
- Then, by the founders' choice at M3, one of two paths.
- **History** of the current financial year:
  - opening balances at the books start, with opening open items (D6);
  - in date order: invoices with their Doc Lines and spreads; bills; quotes and orders;
  - Transactions:
    - one that names an invoice becomes a customer receipt recorded by hand;
    - one that names a bill becomes a bill payment recorded by hand;
    - any other becomes a categorised bank transaction;
  - journals;
  - payroll, as one summary journal a month (D7);
  - assets, with depreciation run for each past month.
- (As built, slice 5; rev 0.259) The history goes through the commands a person's request uses:
  - the opening balances, at the books start, from the Accounts sheet's balances on their side; the receivables and
    payables on 3900, as the opening items carry them; a director's account's balance in its person's name, with the
    origin lent or pay (a credit) or paid out (a debit);
  - each invoice with its Doc Lines (the workbook's resolved description, quantity and price; its discount as a
    percentage), or one line of its amount; the GST code, rate and due date the workbook used; its spread and project;
    a negative row as a credit note; a draft as a draft. It keeps the workbook's number, the sequence continuing past
    the highest of its own form (FR-ORG-04);
  - each bill with the GST the workbook worked out (its total less its amount), the workbook's number as the
    supplier's reference, and a non-resident supplier's nature of payment from the mapping (FR-AP-19);
  - each journal entry, its number kept, a director's account's line in its person's name.
  - The GST registration starts at the earliest invoice or bill when that is before the books start, as the workbook
    charged GST on it [accountant to confirm]. Imported records carry no evidence of their own: the workbook kept as
    M1's evidence, and archived at M6, is theirs (FR-DOC-06).
  - The mapping (M2) gains the people behind the director's and subscriber's accounts and each non-resident supplier's
    nature of payment, which the Accountant chooses: the sample's two US suppliers are the mapping's two errors until
    then.
- (As built, slice 6; rev 0.260) The Transactions sheet: a statement of each money account the rows move, the
  workbook as its file, each row a line referenced `WB-<row>` (so no two rows are taken for duplicates, and a row finds
  its line), and a transfer's other side, which the workbook keeps on the same row, a line referenced `WB-<row>-to` or
  `-from`. Each row is then, in date order with the documents: the receipt of the invoice it names (MatchInvoices),
  the payment of the bill it names (MatchBills), a transfer (RecordTransfer), or its line categorised. A document in
  another currency than the line's, as the USD invoices paid into the wallet kept in SGD, is settled in full at the
  workbook's own total, its realised difference to 7030 (ENG-41). The set-up adds a money account in Banking for each
  of the workbook's (its kind from its name), and the mapping holds the tax agent's withholding decision for each
  non-resident supplier, which its bills post with (FR-AP-19). Rows to share capital or to a SAFE wait as lines for
  the Funding sheet's share issue and SAFE (task 4.6). At the sample's as-of, every account the import has built equals
  the workbook's trial balance.
- (As built, slice 7) Quotes and orders in the workbook's states, their numbers kept: a quote or sales order sent,
  accepted (the workbook's "Invoiced" too, whose invoice is its own Invoices row) or declined; a purchase order approved,
  and closed once received or billed; a line of the amount alone takes the contact's usual category, as the workbook
  keeps none, and a blank GST code the item's or the account's default. Assets: one bought before the books start is
  an opening asset with the depreciation the workbook carried for it then, straight-line by month from its start to
  the month before the books start, rounded once [accountant to confirm]; one bought since is registered from the bill
  or bank transaction line its cost posted on; their depreciation runs to the "as of" as the history's last record. A
  disposed asset is registered with a warning, its disposal recorded after the import. At the sample's "as of", the
  depreciation equals the workbook's to the cent.
- **Clean start**: the Carry Forward sheet's balances as opening balances at the cut-over, and its open invoices and
  bills as opening open items.
- Both paths:
  - active Recurring rows become recurring invoices, bills and journals from the cut-over on; recurring
    transactions are reported as kept in the archive (bank feeds bring them);
  - Budget becomes a budget version, approved;
  - the Forecast drivers become a forecast document (fyniq-forecast@1, as `scripts/workbook_planning_fixture.py`
    already reads them);
  - Funding's cap table becomes the equity register's holders, classes and holdings at the cut-over
    [accountant to confirm].
- Timesheets and the per-person Employees rows are reported as kept in the archive (rev 0.254).

**D6. Opening open items post through a clearing account [accountant to confirm].**
- An invoice or bill marked "Opening balance item?" becomes a document of its kind flagged `opening_item`. It posts
  at the books start under `opening_invoice@1` or `opening_bill@1` (as built, rev 0.257: one rule version each, so each
  keeps its replay golden), against 3900 Opening balance clearing:
  - an invoice: Dr Accounts receivable, Cr **Opening balance clearing**;
  - a bill: Dr Opening balance clearing, Cr Accounts payable;
  - each in its currency at its rate, with no revenue, cost or GST.
- The opening balances then carry the receivable and payable totals on the clearing account instead. The clearing
  nets to zero once both have posted, and a check says when it does not.
- Every receivable and payable is then backed by a document, so INV-07's sub-ledgers agree from the books start, and
  the items can be paid, aged and reminded.
- Alternative: the workbook's way, where the item posts nothing and the account's opening balance carries it.
  Rejected: Fyniq's sub-ledgers are read from postings by document, so the items could not be settled without a
  manual adjustment.
- (rev 0.261, slice 9) A credit open at the books start is an opening item too: a credit note to a customer posts
  opening_credit_note@1 (Cr 1100, Dr 3900), a supplier's credit opening_vendor_credit@1 (Dr 2010, Cr 3900), each the
  opening invoice's or bill's legs the other way, so the Carry Forward sheet's open credit note is a document that can
  be applied or refunded. The oracle posts them as its credit notes do, and the differential's opening-items source
  generates both [accountant to confirm].

**D7. Payroll as one summary journal a month (rev 0.254).**
- From the Payroll sheet's calculated columns, one manual journal per pay month, lines aggregated by account and
  department:
  - gross pay to each employee's salary account;
  - employer CPF and SDL to their expense accounts;
  - net pay to salaries payable;
  - employee and employer CPF, SDL and SHG to the CPF payable.
- The amounts are those paid and reported, as the workbook computed them. Where the workbook flagged a difference from
  the statutory tables, that becomes a register item (FR-MIG-02), never a silent change.
- Memo "Payroll <month> (imported from the workbook)"; approved under Journals (Appendix D's People and HR row).
- (As built, slice 8) On the month's last day, from each row's calculated gross, CPF, SDL, SHG and net pay and the
  salary account the workbook worked out; the department is the employee's on the Employees sheet, the expense and
  payable accounts the Settings sheet's system accounts. Its credit to the CPF payable is what was due to the CPF
  Board; with the Transactions that paid them, the payroll accounts end each month as the workbook's.

**D8. Reconciliation against the workbook's own engine (FR-MIG-02, QA-MIG-01).**
- The workbook's Trial Balance sheet is the independent implementation the imported books must agree with: golden
  rule 3, with the workbook as the second implementation.
- For each account: its opening at the books start, and its movement and closing each month, from Fyniq's balance
  service against the sheet's columns, to the cent after `RND-IMPORT`.
- Each difference is a register line: account, month, both figures, its class (a cent allocation, b deliberate rule
  change, c workbook defect) and a note. The class is proposed (a for spread and depreciation accounts within the
  months' count in cents, b for revaluation and statutory accounts) and the Accountant decides.
- The workbook's Checks rows, by their number, are compared with Fyniq's C.1 checks after a run. A different status
  needs a register line or a fix.
- The Accountant signs the parity report and the register together, with step-up, over their hash (M4).
- G4-016 (open USD receivables) is a fixture case: the revaluation at month ends is register class b.

**D9. Generic import (FR-MIG-03): the workbook's sheet is the template.**
- One template per entity: accounts, contacts, items, opening balances, journals, invoices with lines, bills, FX
  rates, bank rules and budgets (the budget import exists).
- Each template is that entity's sheet's input columns, as CSV or XLSX, so one parser serves both imports.
- A dry run gives the same report (D2). A commit enters each record through its command, with the approvals and limits
  a person's entry has, in any organisation, unlike the workbook import.
- Each entity needs its own Appendix D row's C.

**D10. The full tenant export (FR-MIG-04).**
- An approval object `tenant_export`: the Administrator or the Owner asks, and the Owner and a second approver sign
  (Appendix D's "Tenant export").
- The worker writes a ZIP:
  - each table of the organisation as RFC 4180 CSV, with decimal strings, ISO dates and formula-safe cells;
  - each document's versions as JSON;
  - the evidence and attachments as their files;
  - the audit trail as JSON lines with its hashes, verifiable outside Fyniq with `contracts/audit`;
  - a `manifest.json`: each file's path, rows, SHA-256, the schema and platform versions, the ledger revision and the
    audit chain's head;
  - a README.
- A catalogue in code names every table with `organisation_id` as exported or excluded with a reason: secrets, key
  material, sessions, tokens, idempotency keys and the outbox are excluded. A test fails on a table in neither list.
- Sealed values (bank account numbers, NRIC/FIN) are exported in clear, since the export exists for exit and
  portability: the organisation's own data, dual controlled [counsel to confirm].
- The file is downloaded by the requester for 24 hours and recorded as a sensitive export (AUD-05).

**D11. The migration record and its steps (§17.1 M1 to M6).**
- `migration_run` (the organisation, its status and the founders' M3 choice) with one `migration_step` per step:
  status, evidence files, who signed and when, and the hash signed.
- The steps:
  - **M1**: the frozen workbook and its Checks export uploaded as evidence (FR-DOC-05), each hashed; the validation
    report;
  - **M2**: the mapping signed;
  - **M3**: the commit;
  - **M4**: the parity report and the register signed;
  - **M5**: the workbook at the end of the parallel month uploaded and that month reconciled the same way, then
    signed;
  - **M6**: the frozen workbook archived as evidence under retention (FR-DOC-07's lock), the record closed and read
    only.
- A step opens only when the one before is signed.

**D12. The sample workbook as a pinned fixture.**
- `scripts/workbook_migration_fixture.py` writes `test/migration/workbook-sample.xlsx` from a checkout of
  `zh-sheet-accounting` at a pinned commit: the sheets the import reads, values only, with the commit recorded. It is
  synthetic data (DEV-08) and small.
- The rehearsal test runs M1 to M6 on it in the local flavour. Its expected register (classes a and b only) and the
  expected Checks comparison are committed beside it.

**D13. Ports, audit and the threat model.**
- No new port: the blob store keeps the uploaded workbook, the archive and the export; the outbox runs the validation,
  the commit, the reconciliation and the export; the clock dates them.
- An uploaded workbook passes the scanner service before it is read.
- Audit events:
  - `migration.started`, `migration.workbook_received`, `migration.validated`, `migration.mapping_signed`,
    `migration.committed`, `migration.reconciled`, `migration.register_item_changed`, `migration.register_signed`,
    `migration.parallel_signed`, `migration.archived`;
  - `import.run`;
  - `tenant_export.requested`, `tenant_export.completed`, `tenant_export.downloaded`.
- `docs/security/threat-models/migration-and-export.md` covers:
  - an untrusted workbook: bombs, formulas, oversized text, injected codes;
  - a commit by the wrong person;
  - an export taken by one person, a stale link or a leaked file.

## Risks / Trade-offs

- [A later workbook release moves or renames columns] → columns are found by header text; an unknown header is a
  validation error naming the sheet and column, never a silent skip.
- [The workbook's float arithmetic differs from exact decimals by a cent] → `RND-IMPORT` rounds its calculated figures
  once, and the remaining cents are register class a, re-derived by `ALLOC-EVEN` as FR-MIG-02 says.
- [A commit stops part way] → business keys per row make it resumable; an abandoned organisation is discarded, not
  repaired.
- [The clearing account of D6 is the accountant's to confirm] → it is provisional, like the other accounting choices,
  and the parity report shows it at zero, so a change of method later is a posting rule's version, not a re-import.
- [The export holds sealed values in clear] → it is dual controlled, single-requester, short-lived and audited; counsel
  confirms before the first real export.
- [The sample workbook may not show every real case] → the real workbook's own validation report and parity run in M1
  and M4 on its copy before go-live, and the parallel month (M5) catches what the sample did not.

## Migration Plan

- Deployed as an ordinary release: additive migrations, no backfill.
- The founders' migration itself is §17.1's path, run in Settings › Import on the real workbook in a fresh
  organisation, after the rehearsal passes.
- Rollback: before M6 the old workbook remains the books of record; abandoning the Fyniq organisation is the rollback.

## Open Questions

- Which month is the parallel month (M5), and whether the founders import history or start clean at M3: the founders'
  choices at migration time; both paths are built.
