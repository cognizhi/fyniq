---
key: compliance.data-protection
title: Keep the data protection registers
mode: expert
roles: [owner, auditor, dpo]
requirements: [CMP-05, CMP-PD-02, CMP-PD-01, CMP-PD-04, CMP-PD-05, CMP-PD-06, CMP-PD-08, CMP-PD-09, CTL-DP-05, CTL-DP-06, CTL-DP-07, AUD-02]
---

# Keep the data protection registers

## What it is

**Data protection** (from **Compliance**, Expert mode) holds the company's PDPA programme. The Compliance officer keeps
it; the Owner and the Auditor read it; nobody else sees it.

## The inventory

The **Inventory** tab says what personal data the company holds:
- **What Fyniq holds**: each kind of personal data the finance system keeps, such as members' names and sign-in
  addresses, contacts' details, sign-in records and the bank accounts people are paid to. Each says why it is held and
  how long it is kept; Expert mode adds where it is held, who reads it, how it is protected and where it goes abroad.
  Fyniq keeps this list complete as the system changes.
- **What Fyniq never holds**: NRIC and FIN numbers, and payment card numbers.
- **The retention schedule**: how long each kind of data is kept, from the settings in force, and what happens then.

## Add what the company holds elsewhere

The company may keep personal data outside Fyniq, such as job applicants' CVs or a mailing list. Under **What the
company holds outside Fyniq**, the Compliance officer selects **Add an entry** and says what it is, why the company
holds it, where, who reads it, how long it is kept, where it goes abroad, and whether it needs a data protection impact
assessment. **Change** saves its next version; **Retire it** marks it stopped once the company no longer does it. Every
version is kept, and each is in the audit trail.

## Review the inventory each quarter

Once a calendar quarter ends, the Compliance officer checks that the inventory still says what the company holds, and
selects **Record the review**, within 14 days of the quarter's end. Until then a banner says the review is due, and after
it that it is overdue. The review also shows on **Reviews and attestations**.

## Impact assessments

The PDPA expects an assessment of the risks to people before a new processing of their personal data. The **Impact
assessments** tab lists them:
- Fyniq's drafts for the **payroll link** and the **AI assistant**, written from the PDPC's guides; each is required
  once its feature is switched on, and says so;
- the company's own, for any other processing, such as an inventory entry marked as needing one.

Select **Open** to read one. The Compliance officer edits the text and selects **Save the next version**, or **Write
one** for a new processing, and approves the latest version with **Approve**, confirming it is them. Approving Fyniq's
draft adopts it as it is. A later change needs approving again. Clauses marked [counsel to confirm] bear on the law:
have counsel confirm them before you approve.

A processing that goes on without an approved assessment shows as overdue on **Reviews and attestations**.

## Access and correction requests

People may ask to see the personal data the company holds about them, or to correct it. A member asks in **Settings** ›
**Profile**; the Compliance officer records any other request under the **Requests** tab with **Record a request**. Each
is due 30 days after it was received.

For each request, the Compliance officer selects **Next step** to acknowledge it, extend it, answer it with its outcome,
then close it. An extension is told to the requester in writing within the first 30 days, and names the later day it
will be answered. **Data held** shows the personal data Fyniq holds about a member or a contact who asked; reading it
is recorded in the audit trail. Corrections are made where the data lives, such as the contact's page.

A request past its day shows as overdue, here and on **Reviews and attestations**.

## Breaches

Record a personal-data breach, or a suspected one, under the **Breaches** tab with **Record a breach** as soon as the
company becomes aware of it: what happened, how many people it affects as best known, and the kinds of data involved.
The Compliance officer, the Owner and the Auditor are told at once. Then the PDPA's timers run:

- **Assess it** within 30 days of becoming aware of it.
- **Decide whether it is notifiable.** A breach is notifiable when it is likely to cause significant harm to the people
  affected, or when it affects 500 or more people. A name or an identification number together with pay, card or bank
  account numbers, or with sign-in credentials, is prescribed data: a breach of it is deemed to cause significant harm.
  Fyniq marks such a breach **Prescribed data** and does not let it be decided not notifiable.
- **Notify the PDPC** within 3 calendar days of deciding that it is notifiable, and record the PDPC's reference. Tell
  the people affected too when it is likely to cause them significant harm.

Select **Next step** to record each step: assessed, decided, the PDPC notified, the people told, contained and closed.
A notifiable breach closes only once the PDPC has been notified. Steps are only ever added, each in the audit trail.
An assessment or a PDPC report past its day shows as overdue, here and on **Reviews and attestations**.

Rehearse the breach runbook twice a year, and record each rehearsal under **Tabletop exercises** with what it found and
what changed. The next is due 6 months after the last.

## Overseas transfers

Personal data that leaves Singapore must stay protected to the PDPA's standard there. The **Transfers** tab records each
transfer and what protects it: a contract with the recipient, binding corporate rules, another law or binding
instrument, a certification such as APEC CBPR, or the person's consent.

**Fyniq's flows abroad** lists the ways Fyniq's own data leaves Singapore: sign-in through the deployment's sign-in
providers, and email through its email providers. Each shows **Assessed** once a transfer covers it, or **Needs an
assessment**. The Compliance officer selects **Record the assessment** and says who receives the data, where they process
it, why, what protects it, when it was assessed and by whom, where the evidence is kept, and when to review it next.
**Record a transfer** does the same for a transfer the company makes outside Fyniq.

**Change** saves a transfer's next version, such as its review with a new day, and **Retire it** marks it stopped. A
transfer keeps the flow it covers. A review is due from 30 days before its day. A flow without an assessment, or a
transfer past its review, shows here and on **Reviews and attestations**.

## Shredding at the end of retention

A person's name, email addresses, phone and address in the audit trail are sealed under a key of their own for each
financial year. Once a year's records' retention has ended (**Records kept for** in Settings › Organisation, at least 7
years after the year ends), Fyniq lists that year under the **Shredding** tab, and the Compliance officer is told.
Nothing is shredded until they select **Approve**, confirming it is them. The next daily run then writes a signed entry
to the shredding register and only after that deletes the year's keys: those values no longer open, and the audit
explorer shows them as **Shredded**. The events and their proof stay, so the trail still verifies. It cannot be
undone, and a restore from an older backup shreds them again.

When a person's request is honoured or their purpose ends, the Compliance officer can also **Anonymise** the contact
from their answered request under **Requests**, giving the reason. Their emails and phone go and their address reads
Anonymised; their name and every issued document stay, as the records need them. **Anonymised contacts** lists each
one. A request alone does not shred the trail's values while their retention continues.

## The Data Protection Officer

The **Data Protection Officer** tab shows the DPO's business contact, from the organisation's profile, with the text to
publish on the company's website, and links to the data protection policy and the privacy notices.

## The penalty

The PDPC may fine an organisation that breaches the PDPA's data protection provisions up to 10% of its annual turnover
in Singapore when that is above S$10 million, otherwise up to S$1 million. Keep a data protection risk in the risk
register.
