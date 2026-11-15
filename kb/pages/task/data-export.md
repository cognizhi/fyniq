---
key: settings.data
type: task
title: See where your data lives, and ask for, approve and save a full export
summary: Settings › Data and export, the region and retention read only, the full export, asked by an Administrator, approved by another Owner, saved within 24 hours
sources:
  - docs/user-guide/data-export.md sha256:1a277f0c377e22b0eea33ea5c9eb1120663e5b3371e312afb7cc57999b66178a
data_class: general
taint: trusted
status: published
human_edited: false
compiled_by: claude-opus-5-5 via Claude Code (fyniq-knowledge-base skill)
schema: kb-schema@1
compiled_at: 2026-11-13
aliases:
  - full export
  - tenant export
  - export all data
  - download everything
  - leave fyniq
  - data portability
  - data region
  - where is my data
---

# See where your data lives, and ask for, approve and save a full export

## What it is

- **Settings → Data and export** shows where the organisation's data lives and how long it is kept, and gives a full copy of everything it keeps in Fyniq.
- It is in Expert mode, for the Owner, the Administrator and the data protection officer.
- To bring data in, use **Settings → Import** ([[settings.import]]).

(src: docs/user-guide/data-export.md#what-it-is)

## Where your data lives

- **Data region**: every record is kept and processed in Google Cloud's Singapore region; the platform sets it, so it is shown as fixed.
- **Records kept for**, the purge after retention and the other retention settings are shown as they stand; change them in **Settings → Organisation**, under **Data region and retention** ([[settings.organisation]]).
- A retention can be lengthened, never shortened.

(src: docs/user-guide/data-export.md#where-your-data-lives)

## What a full export holds

- One ZIP file: each table of records as CSV (plain decimal amounts, YYYY-MM-DD dates), every version of every document, the evidence and attachments, the payment files released to the bank, and the audit trail one event per line with its hashes.
- A manifest gives each file's SHA-256, and a README says how to check them.
- It holds the vendors' bank account numbers in clear: keep the file encrypted and share it only with whoever must have it.
- Secrets, keys and sign-in tokens are never in it; the manifest lists what was left out and why.

(src: docs/user-guide/data-export.md#what-a-full-export-holds)

## Ask for a full export

- An Administrator selects **Ask for a full export** and confirms it's them; nothing is prepared yet.
- An Owner other than the person who asked approves it in **Approvals** ([[approvals.decide]]).
- Once approved it is prepared in the background, and the person who asked is told when it is ready.
- Each export is listed: awaiting approval, being prepared, ready to save, failed with why, or expired.

(src: docs/user-guide/data-export.md#ask-for-a-full-export)

## Save it

- Only the person who asked can save the export, for 24 hours after it is ready; then the file is deleted and the export can be asked for again.
- **Save** makes a link that works for 5 minutes; each link is recorded in the audit trail with who made it.
- Check the file against its SHA-256, shown with the export in Expert mode.

(src: docs/user-guide/data-export.md#save-it)
