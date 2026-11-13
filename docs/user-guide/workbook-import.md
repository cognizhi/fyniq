---
key: settings.import
title: Import from the finance workbook, and import records from CSV or Excel
mode: expert
roles: [owner, controller, accountant, auditor]
requirements: [FR-MIG-01, FR-MIG-02, FR-MIG-03]
---

# Import from the finance workbook, and import records from CSV or Excel

## What it is

**Settings → Import** moves the organisation from the finance workbook into Fyniq, step by step, with a report at
each step before anything is saved to the books. It also imports one kind of record at a time from a CSV or Excel
file. It is in Expert mode, for the Owner, the Controller, the Accountant and the Auditor, who only reads.

The move has six steps, shown at the top of the page:

1. **Upload and check the workbook** (M1).
2. **Map and sign** (M2): the Accountant signs how the workbook's set-up maps onto Fyniq's.
3. **Commit** (M3): an Owner who did not ask approves it, and the workbook's records are entered.
4. **Reconcile and sign** (M4): every account and month compared with the workbook, and each difference explained.
5. **Run a parallel month** (M5): a month kept in both, compared again.
6. **Archive and go live** (M6): the workbook archived, and the import closed.

## Start the import

A Controller or the Accountant selects **Start the import**. Only an organisation that has posted nothing yet can
import the workbook: a failed or rejected import starts again in a new organisation, never in live books. An
organisation has one import.

## Upload and check the workbook

Upload the frozen workbook: the published .xlsx, saved after its own checks pass. Nothing goes into the books here.
Each sheet is read and checked in the background, and the page shows each sheet's rows with its errors and
warnings, then every problem with its sheet, row and column, what is wrong and how to fix it. Fix the workbook and
upload it again; the latest upload is the one that counts.

## Map and sign

Once the workbook checks clean, Fyniq proposes how its set-up maps onto Fyniq's: each of the workbook's accounts
onto one the chart has, or created from the Accounts sheet. Change where an account goes and select **Save**; each
save is a new version. Errors against the organisation's set-up are listed and stop the sign-off.

The Accountant then selects **Sign the mapping** and confirms it's them. A later version needs its own sign-off, and
once the import is committed the mapping is fixed.

## Commit

Once the mapping is signed, choose what to import: the year's history from the books start, or a clean start from
the workbook's Carry Forward sheet. Select **Ask to commit** and confirm it's you. An Owner who did not ask approves
it in **Approvals** ([Approve or reject a request](approvals.md)).

Once approved, select **Run the import**. The records are entered batch by batch, each through the same checks as
entering it by hand, with the progress shown by kind. If a record cannot be entered, the import stops and names its
row, what is wrong and how to fix it; the records before it stay entered, and **Continue the import** picks up where
it stopped.

## Reconcile and sign

Once the import is committed, every account and month is compared with the workbook to the cent, with the customers'
and suppliers' open balances and the pay runs' totals, and each of the workbook's checks with Fyniq's. Each figure or
check that differs goes into the difference register, where the Accountant decides its class:

- **a**: a cent from how spreads and depreciation are allocated;
- **b**: a rule Fyniq applies deliberately differently;
- **c**: a defect in the workbook found during the move, with a note saying what it is.

If the figures move after a line is decided, it is marked to decide again. Once every line is decided, the
Accountant selects **Sign the register** and confirms it's them. The GST F5 is compared once Fyniq makes the F5
return.

## Run a parallel month

Once the register is signed, keep one month in both the workbook and Fyniq. At its end, upload the workbook as it
stands then, with **Upload the month's workbook**. It is checked as the first was, then the month is compared with
Fyniq's books the same way, and each difference decided in its own register. The Accountant then selects **Sign the
parallel month** and confirms it's them.

## Archive and go live

Once the parallel month is signed, add the PDFs of the workbook's reports, then select **Archive and go live** and
confirm it's you. Each of the import's files (the workbooks, their Checks exports and the PDFs) is copied, write-once,
to the retention-locked store with its hash, and kept as long as the records are. The import then closes, read only:
nothing of it changes again.

## Other imports

Import one kind of record from CSV or Excel: contacts, items, accounts of the chart, opening balances, journals,
invoices, bills, monthly FX rates, bank rules or budgets.

1. Choose the records and select **Download the template**: the columns of the workbook's sheet for them, as CSV or
   Excel. The workbook's own sheet imports as it is, too.
2. Choose the filled-in file and select **Check the file**. Every row is read and each problem is listed with its row
   and column. Nothing is saved.
3. When the check finds no problem, select **Import it**. The file is imported whole, or not at all; a file holds at
   most 1,000 rows.

Each record goes through the same checks and rights as entering it by hand: you need the right to create those
records. Journals, invoices, bills, opening balances and budgets come in as drafts, approved later as usual.
