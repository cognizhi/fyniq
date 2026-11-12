---
key: compliance.data-protection
type: task
title: Keep the data protection registers
summary: The PDPA programme - the data inventory and schedule, its quarterly review, impact assessments, access requests, breaches and their timers, the DPO
sources:
  - docs/user-guide/compliance-data-protection.md sha256:5b146ce580b2e60265274aa3f82bedc4bd554fa4a683839b740bcbb024c8ad80
data_class: general
taint: trusted
status: published
human_edited: false
compiled_by: claude-opus-5-5 via Claude Code (fyniq-knowledge-base skill)
schema: kb-schema@1
compiled_at: 2026-11-09
aliases:
  - DPIA
  - impact assessment
  - data inventory
  - personal data inventory
  - retention schedule
  - PDPA penalty
  - what personal data do we hold
  - data breach
  - notify the PDPC
  - notifiable breach
  - tabletop exercise
---

# Keep the data protection registers

## What it is

- **Data protection** (from **Compliance**, Expert mode) holds the company's PDPA programme; the Compliance officer keeps it, the Owner and the Auditor read it, nobody else sees it ([[members.roles]]).

(src: docs/user-guide/compliance-data-protection.md#what-it-is)

## The inventory

- **What Fyniq holds**: each kind of personal data the finance system keeps, with why and how long; Expert mode adds where, who reads it, how it is protected and where it goes abroad. Fyniq keeps the list complete as the system changes.
- **What Fyniq never holds**: NRIC and FIN numbers, and payment card numbers.
- **The retention schedule**: how long each kind of data is kept, from the settings in force, and what happens then ([[settings.organisation]]).

(src: docs/user-guide/compliance-data-protection.md#the-inventory)

## Add what the company holds elsewhere

- Under **What the company holds outside Fyniq**, the Compliance officer selects **Add an entry**: what, why, where, who reads it, how long, where it goes abroad, and whether it needs a data protection impact assessment.
- **Change** saves the next version; **Retire it** marks it stopped; every version is kept and audited ([[compliance.audit-trail]]).

(src: docs/user-guide/compliance-data-protection.md#add-what-the-company-holds-elsewhere)

## Review the inventory each quarter

- Once a calendar quarter ends, the Compliance officer checks the inventory and selects **Record the review**, within 14 days of the quarter's end; a banner says when it is due or overdue.
- The review also shows on **Reviews and attestations** ([[compliance.reviews]]).

(src: docs/user-guide/compliance-data-protection.md#review-the-inventory-each-quarter)

## Impact assessments

- The **Impact assessments** tab lists Fyniq's drafts for the **payroll link** and the **AI assistant**, each required once its feature is switched on, and the company's own for any other processing, such as an inventory entry that needs one.
- The Compliance officer edits and selects **Save the next version**, or **Write one**, and approves the latest version with **Approve**, confirming it is them; approving Fyniq's draft adopts it, and a later change needs approving again. Clauses marked [counsel to confirm] need counsel first.
- A processing without an approved assessment shows as overdue on **Reviews and attestations** ([[compliance.reviews]]).

(src: docs/user-guide/compliance-data-protection.md#impact-assessments)

## Access and correction requests

- A member asks in **Settings** › **Profile** ([[preferences.security]]); the Compliance officer records any other request under **Requests** with **Record a request**. Each is due 30 days after it was received.
- **Next step** acknowledges, extends (told in writing within the first 30 days, naming a later day), answers with its outcome, then closes it.
- **Data held** shows what Fyniq holds about a member or contact who asked, and the read is recorded in the audit trail; corrections are made where the data lives.
- A request past its day shows as overdue here and on **Reviews and attestations**.

(src: docs/user-guide/compliance-data-protection.md#access-and-correction-requests)

## Breaches

- Record a breach, or a suspected one, under **Breaches** with **Record a breach** as soon as the company becomes aware of it: what happened, how many people as best known, and the kinds of data; the Compliance officer, the Owner and the Auditor are told at once.
- **Assess it** within 30 days of becoming aware of it.
- **Decide whether it is notifiable**: likely to cause significant harm, or 500 or more people. A name or identification number with pay, card or bank account numbers, or with sign-in credentials, is prescribed data and deemed to cause significant harm; Fyniq marks it **Prescribed data** and does not let it be decided not notifiable.
- **Notify the PDPC** within 3 calendar days of deciding it is notifiable and record the reference; tell the people affected when it is likely to cause them significant harm.
- **Next step** records assessed, decided, the PDPC notified, the people told, contained and closed; a notifiable breach closes only once the PDPC has been notified; steps are only ever added and audited ([[compliance.audit-trail]]). A step past its day shows as overdue here and on **Reviews and attestations** ([[compliance.reviews]]).
- Rehearse the breach runbook twice a year under **Tabletop exercises**, with what it found and what changed; the next is due 6 months after the last.

(src: docs/user-guide/compliance-data-protection.md#breaches)

## The Data Protection Officer

- The tab shows the DPO's business contact from the organisation's profile, the text for the website, and links to the data protection policy ([[compliance.policies]]) and the privacy notices ([[compliance.privacy-notices]]).

(src: docs/user-guide/compliance-data-protection.md#the-data-protection-officer)

## The penalty

- The PDPC may fine up to 10% of annual turnover in Singapore when that is above S$10 million, otherwise up to S$1 million; keep a data protection risk in the risk register ([[compliance.risks]]).

(src: docs/user-guide/compliance-data-protection.md#the-penalty)
