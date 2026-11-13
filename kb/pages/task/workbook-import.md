---
key: settings.import
type: task
title: Import from the finance workbook step by step, and import records from CSV or Excel
summary: Settings › Import, the six migration steps M1 to M6, starting, uploading and checking the workbook, mapping and signing, and the generic import
sources:
  - docs/user-guide/workbook-import.md sha256:688f1849b97b4e059de7efb74e92f9ba6593cfbc4975a6b65d96714a1425a1f9
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

## Other imports

- Import one kind of record from CSV or Excel: contacts, items, accounts of the chart, opening balances, journals, invoices, bills, monthly FX rates, bank rules or budgets.
- **Download the template** gives the columns of the workbook's sheet for them, as CSV or Excel; the workbook's own sheet imports as it is.
- **Check the file** reads every row and lists each problem with its row and column, saving nothing; **Import it** then imports the file whole or not at all, at most 1,000 rows.
- Each record has the same checks and rights as entering it by hand; journals, invoices, bills, opening balances and budgets come in as drafts, approved later as usual ([[approvals.decide]]).

(src: docs/user-guide/workbook-import.md#other-imports)
