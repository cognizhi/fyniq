---
key: compliance.evidence
type: task
title: Find evidence across the records, and see what is missing
summary: Search the files attached to records, open or save one, and list the posted records still missing their evidence
sources:
  - docs/user-guide/evidence-search.md sha256:a457de7d566afd5907cf298fb6099458ed439b5acc4572968c5f86327a77e4ed
data_class: general
taint: trusted
status: published
human_edited: false
compiled_by: claude-opus-5-5 via Claude Code (fyniq-knowledge-base skill)
schema: kb-schema@1
compiled_at: 2026-11-17
aliases:
  - how long are files kept
  - retention of evidence
  - evidence search
  - find a receipt
  - find an invoice file
  - missing evidence
  - missing receipts
  - audit evidence
  - open a file
  - download a receipt
  - see an attachment
  - files I attached
  - filing acknowledgement
---

# Find evidence across the records, and see what is missing

## What it is

- **Evidence** (from **Accounting**, Expert mode) lists the files attached across the company's records: bills, invoices, journals, bank transactions, expense claims, changes of bank details, founders' money, Owner successions, the equity register, statutory filings and payments, and GST forecasts.
- Only the files on the records your role lets you see are listed.

(src: docs/user-guide/evidence-search.md#what-it-is)

## Find evidence

- Choose the **Records** to look in (or **All you may see**), type part of the **Evidence type** such as "invoice", set **Records from** and **to** for a period, tick **Only files I attached** for your own files, then **Find**.
- The newest records come first; each file shows its record, the record's date, its name, what it is and when it was attached, and a record's number opens it, or the page that lists it.
- A file whose text shows an NRIC or FIN in full is marked **Shows an NRIC**.
- The first 50 files show; **Show more** lists the next ones.

(src: docs/user-guide/evidence-search.md#find-evidence)

## Open a file

- In a record's **Evidence** panel, and in each row of **Find evidence**, **Show** (the eye) shows a picture, such as a photo of a receipt or a screenshot, below the list, and **Hide** closes it; a PDF is saved rather than shown.
- **Save** (the arrow) saves any other file, such as a PDF or a spreadsheet; the saved copy keeps only the letters, digits, spaces, dots, dashes and underscores of its name.
- Only a file on a record your role lets you see opens; an Employee opens the receipts of their own claims. The file is checked against the fingerprint recorded when it was attached, each opening is in the audit trail, and a file taken off a draft does not open.

(src: docs/user-guide/evidence-search.md#open-a-file)

## How long files are kept

- Once a record is posted its files sit in a locked store nobody can delete from, not even an Owner, until the end of the financial year in which the record's transactions complete plus the records' retention (7 years unless the organisation keeps them longer).
- A contract on an invoice or bill spread over months completes in the spread's last month: one for 2027 to 2029 is kept until the end of the financial year holding that month, plus the retention.
- When a record's transactions come to complete later, such as a spread lengthened by an approved edit, its files are kept longer from that night; a file is never kept for less time than it was.

(src: docs/user-guide/evidence-search.md#how-long-files-are-kept)

## Missing evidence

- **Missing evidence** lists the posted records of the open months with no file while their evidence is required or recommended: a bill without the supplier's invoice or receipt, a manual journal above the evidence threshold without its supporting calculation or document, or a filing recorded as filed without its acknowledgement.
- A filing's line opens the statutory calendar, where the record is withdrawn and recorded again with the file ([[compliance.record-filing]]).
- Attach the file in the record's **Evidence** panel and it leaves the list; the same list is the check "Posted records have their evidence" on **Checks** ([[checks.overview]]).
- What each kind of record needs is set in the organisation's evidence rules ([[settings.organisation]]).

(src: docs/user-guide/evidence-search.md#missing-evidence)
