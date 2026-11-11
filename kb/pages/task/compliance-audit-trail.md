---
key: compliance.audit-trail
type: task
title: Search and verify the audit trail
summary: Find who changed what and why, open an event's before and after, verify the chain has not been altered, see purges and restores, and export the events
sources:
  - docs/user-guide/audit-trail.md sha256:5f93e3f1be599c13c22ba2151edb7066d3be5e822314cafa7fb3f005703e66ed
data_class: general
taint: trusted
status: published
human_edited: false
compiled_by: claude-opus-5-5 via Claude Code (fyniq-knowledge-base skill)
schema: kb-schema@1
compiled_at: 2026-11-09
aliases:
  - audit log
  - change history
  - who changed this
  - who approved this change
  - before and after values
  - verify integrity
  - tamper check
  - hash chain
  - export the audit trail
  - restore behind the anchors
  - acknowledge a restore
  - purged postings
---

# Search and verify the audit trail

## What it is

- **Audit trail** (from **Accounting**, Expert mode) shows every saved change in the books and settings: who made it, on whose behalf, why, from where, and each field's value before and after.
- Events are only ever added; each is linked to the one before it by its hash, so changing any event breaks the chain. Every hour the chain's latest hash is signed and written to a store nobody can change.
- The Owner, the Administrator, the Controller, the Accountant, the Auditor, the Tax agent and the Compliance officer see it; nobody can change it ([[members.roles]]).

(src: docs/user-guide/audit-trail.md#what-it-is)

## Find a change

- Filter by **Action** (such as `bill.updated`, or `bill.` for every change of a bill), **Record ID**, **From** and **to**, and **Only what AI proposals changed**; **More filters** adds **Actor ID**, **Record type**, **Field** and **Approval ID**. Then **Find**.
- The newest events come first; **Show more** loads older ones. An event an AI proposal carried out has the AI mark.

(src: docs/user-guide/audit-trail.md#find-a-change)

## Open an event

- An event shows when it was recorded, the record and its version, the actor and on whose behalf, the AI proposal and approvals it carried out, the reason, and the request it came from.
- A security event (a sign-in, revealing a full bank number, an export) also shows its IP address and browser until the organisation's **Security events' IP address and device kept for** has passed; then they are erased and the event says so ([[settings.organisation]]).
- **Before and after** for each field. A sensitive value, such as a bank account number, is never kept: only whether it **Changed**, **Changed back** or stayed **Unchanged**, with its last four characters.
- Its hash and the previous event's hash are shown.

(src: docs/user-guide/audit-trail.md#open-an-event)

## Verify integrity

- Set **Events from** and **to** (up to 366 days) and select **Verify integrity**. Fyniq recomputes the chain of those days, checks it against every signed hourly anchor and close snapshot, and each security event's details; verifying is itself recorded.
- **Chain verified**: everything agrees. **The audit trail was altered**: Fyniq says which event or anchor does not agree; treat it as tampering, open an incident and keep everything as it is. **Verified, with an explained gap**: events gone for a recorded, signed reason (a purge after retention or a restore behind the anchors), which is named.
- The whole trail is also verified every night.

(src: docs/user-guide/audit-trail.md#verify-integrity)

## Purges and restores

- Lists each purge after retention, oldest first, and each restore behind the anchors, with what it purged or lost, when, under which setting, and where its signed copy is. Nothing is listed while **Purge the ledger and the audit trail after retention** is off.
- **The ledger before** a day, **carried forward**: the entries before that day were replaced by the balances they left, dated the day before (a few, such as share issues, are kept whole). Reports from that day on are unchanged; a report of an earlier day is refused.
- A journal whose postings went says so on its page; the journal and its evidence are kept ([[accounting.journals]]).

(src: docs/user-guide/audit-trail.md#purges-and-restores)

## A restore behind the anchors

- When the database is restored from a backup that ends before the last signed anchor, Fyniq records the last event that still agrees, the signed events no longer proven, and a signed copy; the Owner and the Compliance officer are told at once and a banner shows it.
- Follow the restore runbook it names and decide from the source documents what must be entered again; then the Owner selects **Acknowledge**, once. Until then the audit trail's check on **Checks** fails ([[checks.overview]]); verification goes on explaining the gap.

(src: docs/user-guide/audit-trail.md#a-restore-behind-the-anchors)

## Export

- Search with your filters, select **Export**, set **Export from** and **to**, choose **Excel (XLSX)** or **CSV**, then **Ask for the export**.
- It holds the matching events of those days, oldest first, with sensitive values as proofs; it is prepared in the background, appears under **Your recent exports**, and downloads for 24 hours by the person who asked. Asking is recorded in the audit trail.

(src: docs/user-guide/audit-trail.md#export)
