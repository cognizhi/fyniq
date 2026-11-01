---
key: banking.import
type: task
title: Import bank statement files, with a preview first
summary: Import CSV, XLSX, OFX, camt.053 or MT940 statements for one account or several, preview what each adds, and save a column mapping
sources:
  - docs/user-guide/banking.md sha256:930af81aef70c44afbf4c03e78e113d8e6cfcf0e531ecd9c7c2f14a358ed5332
data_class: general
taint: trusted
status: draft
human_edited: false
compiled_by: claude-opus-5-5 via Claude Code (fyniq-knowledge-base skill)
schema: kb-schema@1
compiled_at: 2026-10-29
aliases:
  - import a statement
  - upload a bank statement
  - bank CSV
  - MT940
  - camt.053
  - OFX
  - column mapping
  - statement preview
---

# Import bank statement files, with a preview first

## Steps

- Select **Import statement**, then choose the bank's files: several at once, for one account or several.
- For each file, check its **format**; a CSV or XLSX file needs its **account** and a saved **column mapping**, while OFX, camt.053 and MT940 files name their own account.
- **Preview** shows, for each statement, the lines to add, the lines held already and dropped, the possible duplicates kept for a person, and whether it continues the balance chain; nothing is saved.
- **Import** imports each file on its own, with its own result; Fyniq then reconciles what it can by itself, within the organisation's limits, and the rest waits in the queue.
- Each file is checked and scanned for malware before it is read, and kept with its statements.

(src: docs/user-guide/banking.md#import-statements)

## Column mappings

- **New column mapping** saves which column of a bank's CSV or XLSX export holds the date, the amount (one signed column, or money in and money out), and optionally the reference, counterparty, description and running balance.
- With a running balance, an overlap with a statement already held is proved and its lines are dropped; without one, a look-alike line is kept as a possible duplicate ([[banking.accounts]]).

(src: docs/user-guide/banking.md#import-statements)
(src: docs/user-guide/banking.md#an-accounts-statements-and-lines)
