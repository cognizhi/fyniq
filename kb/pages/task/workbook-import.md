---
key: settings.import
type: task
title: Import from the finance workbook step by step, and import records from CSV or Excel
summary: Settings › Import, the migration steps M1 to M6, uploading, mapping, committing and reconciling the workbook, and the generic import
sources:
  - docs/user-guide/workbook-import.md sha256:62e33b3728270fd7be5acf7b915c60aca58470a200beea65c934db7d7fea08c3
data_class: general
taint: trusted
status: published
human_edited: false
compiled_by: claude-opus-5-5 via Claude Code (fyniq-knowledge-base skill)
schema: kb-schema@1
compiled_at: 2026-11-13
aliases:
  - import the workbook
  - migrate from the spreadsheet
  - workbook migration
  - import csv
  - import excel
  - import template
  - dry run import
  - map the workbook
---

# Import from the finance workbook step by step, and import records from CSV or Excel

## What it is

- **Settings → Import** moves the organisation from the finance workbook into Fyniq step by step, with a report at each step before anything is saved to the books, and imports one kind of record at a time from CSV or Excel.
- It is in Expert mode, for the Owner, the Controller, the Accountant and the Auditor, who only reads.
- The six steps: **Upload and check the workbook** (M1); **Map and sign** (M2), the Accountant's sign-off; **Commit** (M3), approved by an Owner who did not ask; **Reconcile and sign** (M4), every account and month compared and each difference explained; **Run a parallel month** (M5); **Archive and go live** (M6).

(src: docs/user-guide/workbook-import.md#what-it-is)

## Start the import

- A Controller or the Accountant selects **Start the import**.
- Only an organisation that has posted nothing yet can import the workbook; a failed or rejected import starts again in a new organisation, never in live books. An organisation has one import.

(src: docs/user-guide/workbook-import.md#start-the-import)

## Upload and check the workbook

- Upload the frozen workbook: the published .xlsx saved after its own checks pass; nothing goes into the books here.
- Each sheet is checked in the background; the page shows each sheet's rows with its errors and warnings, then every problem with its sheet, row and column, what is wrong and how to fix it.
- Fix the workbook and upload it again; the latest upload counts.

(src: docs/user-guide/workbook-import.md#upload-and-check-the-workbook)

## Map and sign

- Once the workbook checks clean, Fyniq proposes how its set-up maps onto Fyniq's: each account onto one the chart has, or created from the Accounts sheet.
- Change where an account goes and select **Save**; each save is a new version, and errors against the organisation's set-up stop the sign-off.
- The Accountant selects **Sign the mapping** and confirms it's them; a later version needs its own sign-off, and once the import is committed the mapping is fixed.

(src: docs/user-guide/workbook-import.md#map-and-sign)

## Commit

- Once the mapping is signed, choose what to import: the year's history from the books start, or a clean start from the workbook's Carry Forward sheet.
- **Ask to commit** and confirm it's you; an Owner who did not ask approves it in **Approvals** ([[approvals.decide]]).
- Once approved, **Run the import** enters the records batch by batch, each through the same checks as entering it by hand, with the progress shown by kind.
- A record that cannot be entered stops the import, naming its row, what is wrong and how to fix it; the records before it stay entered, and **Continue the import** picks up where it stopped.

(src: docs/user-guide/workbook-import.md#commit)

## Reconcile and sign

- Once committed, every account and month is compared with the workbook to the cent, with the customers' and suppliers' open balances, the pay runs' totals, and each workbook check against Fyniq's.
- Each figure or check that differs goes into the difference register, where the Accountant decides its class: **a**, a cent from how spreads and depreciation are allocated; **b**, a rule Fyniq applies deliberately differently; **c**, a workbook defect found during the move, with a note saying what it is.
- If the figures move after a line is decided, it is marked to decide again.
- Once every line is decided, the Accountant selects **Sign the register** and confirms it's them; the GST F5 is compared once Fyniq makes the F5 return.

(src: docs/user-guide/workbook-import.md#reconcile-and-sign)

## Run a parallel month

- Once the register is signed, keep one month in both the workbook and Fyniq; at its end, **Upload the month's workbook** as it stands then.
- It is checked as the first was, then the month is compared with Fyniq's books the same way, each difference decided in its own register.
- The Accountant then selects **Sign the parallel month** and confirms it's them.

(src: docs/user-guide/workbook-import.md#run-a-parallel-month)

## Archive and go live

- Once the parallel month is signed, add the PDFs of the workbook's reports, then select **Archive and go live** and confirm it's you.
- Each of the import's files (the workbooks, their Checks exports and the PDFs) is copied, write-once, to the retention-locked store with its hash, and kept as long as the records are.
- The import then closes, read only: nothing of it changes again.

(src: docs/user-guide/workbook-import.md#archive-and-go-live)

## Other imports

- Import one kind of record from CSV or Excel: contacts, items, accounts of the chart, opening balances, journals, invoices, bills, monthly FX rates, bank rules or budgets.
- **Download the template** gives the columns of the workbook's sheet for them, as CSV or Excel; the workbook's own sheet imports as it is.
- **Check the file** reads every row and lists each problem with its row and column, saving nothing; **Import it** then imports the file whole or not at all, at most 1,000 rows.
- Each record has the same checks and rights as entering it by hand; journals, invoices, bills, opening balances and budgets come in as drafts, approved later as usual ([[approvals.decide]]).

(src: docs/user-guide/workbook-import.md#other-imports)
