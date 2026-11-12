# Proposal

## Why

Fyniq holds personal data: members' names and email addresses, contacts' business details, the individuals among the
contacts (directors, shareholders, investors), claimants' bank accounts (sealed), and security events' IP addresses
(erased after 400 days). PRD-001 §14.3 makes the company accountable for it under the PDPA. Nothing in Fyniq yet:
- names a Data Protection Officer;
- gives a notice;
- lists what personal data is held and for how long;
- tracks an access request or a breach and its timers;
- records an overseas transfer;
- removes personal data from the audit trail, which by default is kept for life, once its retention ends.

`p1-compliance-center` built the Compliance Center and handed it the PDPA registers (CMP-05) and the controls CTL-DP-05
and CTL-DP-06. The launch plan puts this change before any payroll data is loaded: counsel confirms the retention
approach first (open question 39).

## What Changes

- **The owner's decisions of 9 Nov 2026, taken interactively (PRD-001 rev 0.244):**
  - **Shredding survives a restore by a register (§11.8).** Each person has a data key per financial year, kept in the
    database and wrapped by the deployment's key. Shredding deletes the key. Before that, it writes a signed entry to
    a shredding and anonymisation register in the write-once bucket, outside the database backup. A restore re-applies
    every entry, as the audit trail's erasure register does (AUD-06). The alternatives were a Cloud KMS key per cohort,
    and both.
  - **What is shreddable: the audit trail's personal values.** These are the names, email addresses, phone numbers and
    addresses in audit events about a member or an individual contact. Identifiers and pay are already proofs
    (AUD-02); documents stay as issued until their own purge. Payroll joins the same keys when Nemura's pay data
    arrives. The alternative was shredding documents too.
  - **The DPO approves each shredding run.** A weekly job lists the cohorts whose retention has ended, and the DPO
    approves the list, stepped up. The alternatives were automatic shredding after a dual-controlled switch, and the
    DPO with an Owner.
  - **The DPO's contact (CMP-PD-01).** The Owner names the DPO and their business contact in the organisation profile.
    Fyniq shows the contact in the app, in each notice, and in the emails and documents it sends. It also gives a
    copy-ready block for the company's website. Fyniq hosts no public page. The alternatives were a public page per
    organisation, and both.
  - **Notices (CMP-PD-03).** Fyniq drafts a notice for each audience (members, contacts) from the PDPC's guides. The
    Owner or the DPO adopts it and publishes versions, as for the policy library. Members acknowledge each version
    when they join and when it changes. Contacts get a summary in what Fyniq sends. Consent is recorded only for a
    purpose that needs it, and none does by default [counsel to confirm]. The alternatives were notices without
    acknowledgements, and a consent register for every contact.
  - **Two Appendix D rows.**
    - **Data protection (PDPA):** the DPO C/A, the Owner and the Auditor V, nobody else.
    - **Own personal-data requests:** every member asks about their own data.

    The alternatives were the Compliance Center's row, and the DPO and Owner alone.
- **The PDPA registers in the Compliance Center (CMP-05):**
  - **The data inventory and retention schedule (CMP-PD-02).** Fyniq's own catalogue of every personal-data class it
    holds: what, why, where, who, how long, and cross-border flows. A test keeps the catalogue complete against the
    database schema. The organisation adds what it processes outside Fyniq. The DPO reviews it each quarter
    (CTL-DP-05).
  - **DPIAs (CMP-PD-08)**, with Fyniq's drafts for payroll and the AI assistant, approved by the DPO.
  - **Access and correction requests (CMP-PD-04)**, with their 30-day timer and the written extension.
  - **The breach register (CMP-PD-05)**, with its timers and the prescribed-data rule, and the tabletop twice a year
    (CTL-DP-06).
  - **Overseas transfers (CMP-PD-06)**, each assessed and recorded.
- **Crypto-shredding (§11.8):**
  - person keys and the audit event format `fyniq-audit-event@3`, which seals personal values;
  - the weekly shredding job, the DPO's approval, the register, and its re-application on restore;
  - anonymisation of a contact's fields that the records do not need (§11.9).
- **NRIC and minimisation (CMP-PD-07, SEC-14).** Fyniq holds no NRIC, FIN or card data. An architecture test keeps it
  that way, beside the existing guards. Masking belongs to Nemura, which holds NRIC.
- **Penalty awareness (CMP-PD-09).** The operator's risk R7 records the PDPA penalty. The Data protection page states
  it for every organisation.
- **Encryption, residency, logs and backups (SEC-10, SEC-13, §11.9).** HSTS and the retention of logs and backups are
  checked where `infra/` and the services set them. Cloud parts are tagged `[live-verify]`.
- **Screens:**
  - Compliance › Data protection (Expert);
  - Compliance › Privacy notices and consent (Expert);
  - a member's own requests, in Settings.

  Help keys and guide pages are compiled into the knowledge base. The canvas waits for the design account.

## Capabilities

### New Capabilities

None. The requirements are approved in the main specs:
- `compliance/pdpa`: CMP-PD-01 to CMP-PD-09;
- `compliance/compliance-center`: CMP-05;
- `security/data-protection`: SEC-10, SEC-11, SEC-13 and SEC-14;
- `data/backup-and-recovery`: the seven unnumbered retention and shredding rows.

This change sets `skip_specs: true`.

### Modified Capabilities

None in this change. Rev 0.244's decisions are already in the main specs and stamped:
- `compliance/pdpa`: CMP-PD-01, CMP-PD-03 and CMP-PD-09;
- `data/backup-and-recovery`: the shredding rows;
- `access/roles-and-segregation`: the two new rows.

If building shows a requirement is wrong, the PRD changes first.

## Impact

- **Database:**
  - `person_key`;
  - the inventory's entries, DPIAs, requests, breaches, transfers and consent;
  - the policy library's two notice keys.

  All are insert-only or versioned, under RLS, with attack tests.
- **Audit:**
  - the event format `fyniq-audit-event@3`;
  - the explorer reads sealed values, or says when they were shredded;
  - the restore step re-applies the shredding and anonymisation register.
- **API:** the registers' operations under the "Data protection (PDPA)" row, a member's own requests, and the DPO
  fields of the organisation profile, all with matrix rows.
- **Jobs:**
  - `pdpa.shred` (weekly);
  - the timers' notices for requests and breaches;
  - the CTL-DP-05 and CTL-DP-06 attestations in the reviews list.
- **Web:**
  - two Compliance screens;
  - the join page's notice;
  - the members' notice card;
  - a member's own requests;
  - the footer line in emails and in new versions of the document templates.
- **Docs:**
  - Fyniq's notice and DPIA drafts, from official sources;
  - guide pages compiled into `kb/`;
  - a threat model.
- **No AI or MCP surface.** The AI holds nothing on either row.
