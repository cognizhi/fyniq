---
key: compliance.audit-trail
title: Search and verify the audit trail
mode: expert
roles: [owner, admin, controller, accountant, auditor, tax_agent, dpo]
requirements: [FR-CTL-03, AUD-02, AUD-03, AUD-05, AUD-06, AUD-07, AUD-09, AUD-10]
---

# Search and verify the audit trail

## What it is

**Audit trail** (from **Accounting**, Expert mode) shows every saved change in the company's books and settings: who
made it, on whose behalf, why, from where, and each field's value before and after. Events are only ever added, never
changed or removed, and each is linked to the one before it by its hash, so changing any event breaks the chain. Every
hour the chain's latest hash is signed and written to a store nobody can change.

The Owner, the Administrator, the Controller, the Accountant, the Auditor, the Tax agent and the Compliance officer see
it. Nobody can change it.

## Find a change

1. Type an **Action**, such as `bill.updated`, or the start of one ending in a dot, such as `bill.` for every change of
   a bill.
2. Paste a **Record ID** to see one record's history, and set **From** and **to** to look at some days.
3. Tick **Only what AI proposals changed** to see only the changes an approved AI proposal carried out.
4. Under **More filters**, look for one **Actor ID**, a **Record type**, a **Field** that changed or an **Approval ID**.
5. Select **Find**.

The newest events come first; **Show more** loads older ones. An event an AI proposal carried out has the AI mark.

## Open an event

Select an event to see it beside the list (on a phone, in its place; **Events** goes back):

- **Recorded**, the **Record** with its version, the **Actor** and, for an AI or a service, **On behalf of** whom.
- **Originator**: the AI proposal it carried out, and the **Approvals** it carried out.
- **Reason**, as given, and **Request**: the request and session it came from. For a security event, such as a
  sign-in, revealing a full bank number or an export, its IP address and browser too, until the organisation's
  **Security events' IP address and device kept for** has passed; then they are erased and the event says so.
- **Before and after** for each field. A sensitive value, such as a bank account number, is never kept: the trail
  shows only whether it **Changed**, **Changed back** to what it was before, or stayed **Unchanged**, with its last
  four characters. A person's name, email addresses, phone and address, in an event about a member or a contact who is
  a person, are sealed under that person's own key: they show **Sealed** with their values, or **Shredded** once the
  records' retention for that financial year has ended and the key is gone.
- Its **hash** and the **previous** event's hash.

## Verify integrity

1. Under **Verify integrity**, set **Events from** and **to**: up to 366 days.
2. Select **Verify integrity**.

Fyniq recomputes the chain of those days and checks it against every signed hourly anchor and close snapshot in them,
and each security event's details against what the event recorded. Verifying is recorded in the audit trail. It ends
in one of three ways:

- **Chain verified**: every link, anchor and detail agrees.
- **The audit trail was altered**: an event or an anchor does not agree, and Fyniq says which. Treat it as tampering:
  open an incident and keep everything as it is.
- **Verified, with an explained gap**: some events are gone for a reason Fyniq recorded and signed: a purge after
  retention, or a restore behind the anchors. The reason is named.

The whole trail is also verified every night.

## Purges and restores

**Purges and restores** lists each purge after retention, oldest first, and each restore behind the anchors:

- **The audit trail's events up to** an event, purged once the organisation's retention had passed.
- **The ledger before** a day, **carried forward**: the journal entries before that day were replaced by the balances
  they left, dated the day before; a few, such as share issues, are kept whole. Each report from that day on is
  unchanged; a report of an earlier day is refused. A journal whose postings went says so on its page; the journal and
  its evidence are kept.
- **Restored behind the anchors**: see below.

Each lists what it purged or lost, when, under which setting, and where its signed copy is. Nothing is listed while
**Purge the ledger and the audit trail after retention** is off.

## A restore behind the anchors

When the database is restored from a backup, the copy can end before the last signed anchor. Fyniq then records the
restore: the last event that still agrees, the events the anchors signed that are no longer proven, and a signed copy.
The Owner and the Compliance officer are told at once, and a banner shows it here.

1. Follow the restore runbook it names, and use the incident to decide what must be entered again from the source
   documents.
2. The Owner then selects **Acknowledge**, once. Until then the check of the audit trail (**Checks**) fails.

Verification goes on explaining the gap after the acknowledgement.

## Export

1. Search with the filters you want.
2. Select **Export**, set **Export from** and **to**, and choose **Excel (XLSX)** or **CSV**.
3. Select **Ask for the export**.

The export holds the events your filters match on those days, oldest first; a sensitive value stays a proof, and a
person's sealed value stays sealed, as the trail recorded it. It is
prepared in the background, appears under **Your recent exports**, and downloads for 24 hours by the person who asked
for it. Asking for it is recorded in the audit trail.
