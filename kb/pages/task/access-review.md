---
key: settings.access-review
type: task
title: Review who can do what each quarter, attest it and download it
summary: The access review page, what it shows (people, role conflicts, integration clients, earlier quarters), attesting the quarter as an Owner, and the CSV download
sources:
  - docs/user-guide/access-review.md sha256:d4e2de8c3647dd1c55b220106158137b9acc19acec61a34aed1223ecc7c9037d
data_class: general
taint: trusted
status: published
human_edited: false
compiled_by: claude-opus-5-5 via Claude Code (fyniq-knowledge-base skill)
schema: kb-schema@1
compiled_at: 2026-11-02
aliases:
  - access review
  - user access review
  - quarterly attestation
  - who has access
  - attest access
  - access review overdue
---

# Review who can do what each quarter, attest it and download it

## What it is

- **Settings → Access review** shows who can do what, as it stands today; once each calendar quarter ends, an Owner reviews and attests it.
- It is in Expert mode, for the Owner, the Administrator, the Controller, the Auditor and the data protection officer.

(src: docs/user-guide/access-review.md#what-it-is)

## What it shows

- **People and their access**: each member's roles, an external person's end date and roles that have ended, their last sign-in and how they passed their second factor.
- The **Review** column points out what to look at: a role conflict, roles that ended while the person is still a member, or no sign-in for 30 days.
- **Role conflicts**: a person who manages users and roles and also records or approves transactions; Founder mode allows it with the weekly review, Standard mode blocks it unless an approved override lets them keep it ([[settings.team-mode]]).
- **Integration clients**: the connectors installed, what they may reach, and whether their last test passed.
- **Earlier quarters**: each attested quarter, who attested it, when, and their note.
- Change roles or remove someone in **Settings → Members and roles** ([[members.remove]]).

(src: docs/user-guide/access-review.md#what-it-shows)

## Attest the quarter

- When a quarter has ended, the page names it at the top; an Owner reviews everyone's access, optionally writes what they checked, selects **Attest** and confirms it's them.
- Each quarter is attested once; attesting the latest quarter also answers for any missed quarter before it.
- What was attested is kept exactly as seen, with its fingerprint (SHA-256), in the audit trail, and never changes.
- If someone's access changed while the Owner was reading, the page asks them to look again; a sign-in alone does not count.
- Until it is attested, a check warns from 14 days after the quarter ends; it cannot be marked reviewed on the Checks page ([[checks.overview]]).

(src: docs/user-guide/access-review.md#attest-the-quarter)

## Download it

- **Download CSV** gives a spreadsheet with one row per person, invitation not yet used, and integration; each download is recorded in the audit trail.

(src: docs/user-guide/access-review.md#download-it)
