---
key: compliance.evidence
type: task
title: Find evidence across the records, and see what is missing
summary: Search the files attached to records, open or save one, and list the posted records still missing their evidence
sources:
  - docs/user-guide/evidence-search.md sha256:007b85129827555d3c411690b1bc111de6860cd43eb2c5d7ee6a0b413b3bd479
data_class: general
taint: trusted
status: published
human_edited: false
compiled_by: claude-opus-5-5 via Claude Code (fyniq-knowledge-base skill)
schema: kb-schema@1
compiled_at: 2026-11-06
aliases:
  - evidence search
  - find a receipt
  - find an invoice file
  - missing evidence
  - missing receipts
  - audit evidence
  - open a file
  - download a receipt
  - see an attachment
---

# Find evidence across the records, and see what is missing

## What it is

- **Evidence** (from **Accounting**, Expert mode) lists the files attached across the company's records: bills, invoices, journals, bank transactions, expense claims, changes of bank details, founders' money and Owner successions.
- Only the files on the records your role lets you see are listed.

(src: docs/user-guide/evidence-search.md#what-it-is)

## Find evidence

- Choose the **Records** to look in (or **All you may see**), type part of the **Evidence type** such as "invoice", set **Records from** and **to** for a period, then **Find**.
- The newest records come first; each file shows its record, the record's date, its name, what it is and when it was attached, and a record's number opens it.
- A file whose text shows an NRIC or FIN in full is marked **Shows an NRIC**.

(src: docs/user-guide/evidence-search.md#find-evidence)

## Open a file

- In a record's **Evidence** panel, **Show** (the eye) shows a picture, such as a photo of a receipt or a screenshot, below the list, and **Hide** closes it.
- **Save** (the arrow) saves any other file, such as a PDF or a spreadsheet; the saved copy keeps only the letters, digits, spaces, dots, dashes and underscores of its name.
- Only a file on a record your role lets you see opens; an Employee opens the receipts of their own claims. The file is checked against the fingerprint recorded when it was attached, each opening is in the audit trail, and a file taken off a draft does not open.

(src: docs/user-guide/evidence-search.md#open-a-file)

## Missing evidence

- **Missing evidence** lists the posted records of the open months with no file while their evidence is required or recommended: a bill without the supplier's invoice or receipt, or a manual journal above the evidence threshold without its supporting calculation or document.
- Attach the file in the record's **Evidence** panel and it leaves the list; the same list is the check "Posted records have their evidence" on **Checks** ([[checks.overview]]).
- What each kind of record needs is set in the organisation's evidence rules ([[settings.organisation]]).

(src: docs/user-guide/evidence-search.md#missing-evidence)
