---
key: compliance.evidence
type: task
title: Find evidence across the records, and see what is missing
summary: Search the files attached to records by kind of record, evidence type and date, and list the posted records still missing their evidence
sources:
  - docs/user-guide/evidence-search.md sha256:c29a2390519b24ccd21fcd6258a379a07d21ed13a9b98ac6e447d66381d80970
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

## Missing evidence

- **Missing evidence** lists the posted records of the open months with no file while their evidence is required or recommended: a bill without the supplier's invoice or receipt, or a manual journal above the evidence threshold without its supporting calculation or document.
- Attach the file in the record's **Evidence** panel and it leaves the list; the same list is the check "Posted records have their evidence" on **Checks** ([[checks.overview]]).
- What each kind of record needs is set in the organisation's evidence rules ([[settings.organisation]]).

(src: docs/user-guide/evidence-search.md#missing-evidence)
