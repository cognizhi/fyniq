# Design

## Context

What exists:
- **Proofs, not values (AUD-02).** Bank numbers, identifiers and pay reach the audit trail only as a keyed fingerprint
  and a hint. Other personal values, such as names, email addresses, phone numbers and addresses, are kept in
  events' before and after values, under the hash chain, for life unless the trail's purge is switched on.
- **The erasure register (AUD-06, G4-070).** `audit.erase` erases security events' IP addresses and browsers after
  400 days. It writes a signed entry to a register in the write-once bucket first, and the restore step re-applies
  it. This change's shredding and anonymisation register follows the same pattern.
- **Sealed fields (SEC-11).** `internal/fieldseal` seals a value with a fresh data key wrapped by the deployment's key
  (`ports.Cipher`; KMS in the cloud, a local key file otherwise).
- **NRIC guards.** Fyniq holds no NRIC field. A holder record refuses a full NRIC (FY285), the evidence scan flags a file
  that shows one (FR-DOC-09), and an archtest keeps NRIC out of sign-in (CTL-DP-04).
- **The policy library (CMP-07).** It has drafts, published versions and every member's acknowledgement, insert-only
  (FY310), with the "Acknowledge policy" card on Home and in the Inbox.
- **The reviews list (CMP-03).** Seven recurring reviews, each read from its source; the controls' attestation checks
  read them.
- **Payroll** lives in Nemura (PRD-002), which holds NRIC and pay. Fyniq receives pay runs for posting and payment
  (`p1-payroll-link`).

Constraints:
- The PDPA gives no general right to erasure on request.
- Statutory records are kept for their retention under the legal-obligation exception (§11.9).
- Counsel confirms the retention approach before payroll data is loaded (open question 39).

## Goals / Non-Goals

Goals:
- The PDPA programme of §14.3 for launch: the DPO, notices, the inventory and schedule, requests, breaches, transfers,
  NRIC, DPIAs and penalty awareness.
- §11.8's crypto-shredding for the audit trail's personal values.
- §11.9's anonymisation of personal data the records do not need.

Non-goals:
- The Data Protection Trustmark (CMP-PD-10, P3).
- Data-intermediary duties to customers (P3).
- Nemura's own PDPA duties (PRD-002).
- AI conversations' purge, which needs the conversations (`p1-ai-assistant`; see Open Questions).
- Region loss (open question 37).

## Decisions

### D1. The DPO and their contact (CMP-PD-01)

- **The organisation profile** gains `dpo_user_id` (a member), `dpo_name` and `dpo_email`, under its Appendix D row
  (the Owner approves, the Administrator prepares). Each change is audited, as every profile change is.
- **A warning, not a refusal**, when the named member does not hold the Compliance officer role. A company of two may
  name the Owner.
- **Where the contact shows:**
  - Compliance › Data protection;
  - each privacy notice (D2);
  - a footer line in every email Fyniq sends to a contact or a member;
  - the default document templates' footer block, in new template versions only. Issued documents are never changed.
- **The website.** A copy-ready block (the DPO's name, email and the contacts' notice summary) for the company's
  website, which publishes it under PDPA s11(5). Fyniq hosts no public page.

### D2. Privacy notices and consent (CMP-PD-03)

- **Two more keys in the policy library:** `notice-members` and `notice-contacts`. They share its drafts, versions,
  acknowledgements, guards (FY310) and Appendix D row. A migration widens the two key checks: it adds the new
  constraint NOT VALID, validates it, then drops the old one.
- **Fyniq's drafts** (`compliance/notices/*.md`, `fyniq-notice-draft@1`) are written from the PDPC's guides. Each
  draft states the purposes, the DPO's contact, retention, transfers, and access and correction. Clauses that bear on
  the law are marked [counsel to confirm]. The contacts' notice opens with a one-paragraph summary, which D1's footer
  carries.
- **Members acknowledge when they join.** The invitation's page shows the members' notice in force. Joining records
  the acknowledgement of that version in the same unit of work. A later version waits on the "Acknowledge policy"
  card, as any policy does.
- **Consent.** `consent_purpose` holds the purposes the organisation defines, each saying whether it needs consent.
  None is seeded. `consent_record` holds each consent given or withdrawn, for a member or a contact, insert-only and
  audited (`consent.given`, `consent.withdrawn`). Both sit under the "Data protection (PDPA)" row.
- **Alternatives rejected:** a notice table of its own, which would copy the library's guards and its card.

### D3. The data inventory and retention schedule (CMP-PD-02, CMP-05, CTL-DP-05)

- **Fyniq's catalogue.** `compliance/data-inventory.yaml` lists each personal-data class Fyniq holds:
  - what it is, why it is held (the purpose), where (store and region), who reads it (Appendix D rows), how long (a
    retention rule or a setting), its cross-border flows, and how it is protected (sealed, proved, shreddable,
    erased);
  - "NRIC/FIN: not held" and "card data: not held" as entries of their own.
- **Kept complete by a test.** It reads the schema snapshot and fails on any column whose name looks personal (name,
  email, phone, address, IP, user agent, bank, birth, NRIC, FIN) that no class names. A new personal column cannot
  arrive silently.
- **The organisation's own entries** (`data_inventory_entry`, versioned, DPO C) cover what it processes outside
  Fyniq. An entry can say it needs a DPIA (D4).
- **The retention schedule** is generated from the catalogue and the settings:
  - `retention.records_years`;
  - `retention.security_events_days`;
  - the logs' and backups' retention from `infra/`, `[live-verify]`;
  - AI conversations, "arrives with `p1-ai-assistant`".
- **A quarterly review.** The DPO marks the inventory reviewed for the quarter. This is the attestation
  `data-inventory`, which CTL-DP-05's Q check reads.

### D4. DPIAs (CMP-PD-08)

- **Tables.** `dpia` and `dpia_version`, insert-only. A version has the sections of the PDPC's Guide to Data
  Protection Impact Assessments: the processing, its data flows, the risks, the mitigations, the residual risk and the
  approval. The DPO approves a version (A), stepped up.
- **Fyniq's drafts** (`compliance/dpias/*.md`) cover the payroll link (Nemura's pay runs in Fyniq) and the AI
  assistant. They are written from official sources, with [counsel to confirm].
- **The attestation `dpia`** reads them: a catalogue class or an organisation entry that needs a DPIA but has no
  approved one reads as due.

### D5. Access and correction requests (CMP-PD-04)

- **Tables.** `pd_request` (kind `access` or `correction`; whose data; received on; due 30 days on) with insert-only
  steps:
  - acknowledged;
  - extended, with the written notice's date, its text and the new date, sent within the first 30 days;
  - answered, with the outcome and evidence;
  - closed.

  Each step is audited.
- **A member asks about themselves** under "Own personal-data requests". The DPO records requests from anyone else.
  Corrections are made on the record's own screen, never by a generic edit.
- **For an access request,** the DPO can attach a report of a member's or contact's personal data, read through the
  catalogue's classes.
- **Timers.** A request due within 7 days notifies the DPO, and an overdue one is critical. The attestation
  `pd-requests` reads them.

### D6. The breach register and the tabletop (CMP-PD-05, CTL-DP-06)

- **Tables.** `pd_breach`: when the company became aware, what happened, the data classes involved, and how many
  individuals. Its steps are insert-only and audited:
  - assessed: significant harm and the 500-individual test;
  - decided notifiable or not, with the time of decision;
  - PDPC notified, with the reference;
  - individuals notified;
  - contained;
  - closed, with the lessons learned.
- **The timers:**
  - the assessment is due 30 days after awareness;
  - the PDPC is to be notified within 3 calendar days of a decision that the breach is notifiable.

  Each sends a critical notice to the DPO and the Owners when it starts, a day before it is due, and when it is overdue.
- **Prescribed data.** When the classes involved include an individual's name or NRIC together with pay or bank
  details, Fyniq marks the breach "prescribed data: notifiable" and the decision defaults to notifiable. The
  prescribed list is checked against the PDPA (Notification of Data Breaches) Regulations 2021 when built [verify].
- **The tabletop.** A tabletop exercise is recorded twice a year, with its report as evidence. The attestations
  `breach` (E) and `breach-tabletop` (S) are CTL-DP-06's checks.

### D7. Overseas transfers (CMP-PD-06)

- **Table.** `pd_transfer` (versioned): the recipient, its country, the data classes, the purpose, the basis
  (contract, binding corporate rules, certification such as APEC CBPR, or consent), when it was assessed and by whom,
  the evidence, and the next review.
- **Kept complete by a check.** Each catalogue flow marked cross-border, and each AI connection outside Singapore once
  `p1-ai-providers` declares them (AI-75), needs a current transfer entry. A missing one reads as due on the reviews
  list.

### D8. Person keys and `fyniq-audit-event@3` (§11.8)

- **`person_key`:** the organisation, the person (`member:<user id>` or `contact:<contact id>`), the cohort (the
  financial year), the key ID, and the data key wrapped by `ports.Cipher`.
  - The API and the worker insert and read keys.
  - Only the worker deletes, through a guarded function that requires an approved shredding run (D9).
- **Which values are sealed.** In events whose object is a member or an individual contact, the fields named as
  personal (names, email addresses, phone numbers, addresses) are sealed as `{"pk": "<key id>", "ct": "<base64>"}`
  under that person's key for the event's financial year, with the organisation, event and field as associated data.
  The chain hashes the sealed text, so shredding never breaks it.
- **Earlier events** (`@1`, `@2`) stay as written; they are immutable. No production data exists yet, so production
  starts at `@3`.
- **Individual contacts.** A contact is an individual when its new field `individual` is set. It defaults to true for
  a director, and the Accountant sets it for any other natural person. Business contact information about a company's
  staff is outside most of the PDPA [counsel to confirm]; the field records which contacts are people.
- **The explorer** opens a sealed value for readers with V on "Audit trail". Once its key is shredded, the value reads
  "shredded on <date> (FY<year>)". The export writes the sealed text as recorded.

### D9. The shredding run, the register and the restore (§11.8)

- **Weekly.** `pdpa.shred` lists the cohorts whose retention has ended (the financial year's end plus
  `retention.records_years`; a legal hold stops it once records can be held). It opens one shredding run for the DPO
  to approve as an approval object, `personal_data_shredding`.
- **On the DPO's approval** (stepped up), the worker:
  1. writes a signed `fyniq-shredding@1` entry to the register in the write-once bucket: the organisation, the
     person's fingerprint (never their name), the cohort, the key ID, the approver and the approval;
  2. deletes the keys;
  3. writes `pdpa.keys_shredded` to the trail.
- **On a restore,** the restore step (`audit.Recover`, `jobs audit-recover`) re-applies every shredding and
  anonymisation entry, checking each signature, before the copy opens. The erasure register's re-application already
  works this way.
- **Alternatives rejected:**
  - a KMS key per cohort (the owner's decision);
  - shredding without approval.

### D10. Anonymisation (§11.9)

- **Who and when.** The DPO anonymises a contact when a PDPA request is honoured or its purpose ends.
- **What changes.** Fields the records do not need (phone, emails and address, for an individual) become
  "anonymised". The legal name and every issued document stay.
- **The trail.** Each anonymisation is audited, and entered in the same register (`fyniq-anonymisation@1`), so a
  restore re-applies it.
- **Not on request alone.** A request does not shred the trail's values while their retention continues.

### D11. NRIC and minimisation (CMP-PD-07, SEC-14)

- **An archtest** refuses any column, API field or event field named for an NRIC, a FIN, a card number or a CVV. It
  sits beside FY285, the evidence scan and CTL-DP-04.
- **The inventory** records "not held" for both.
- **Masking** is a setting in Nemura, which holds NRIC, until the PDPC's guidance is out (rev 0.19).

### D12. Encryption, residency, logs and backups (SEC-10, SEC-13, §11.9)

- **HSTS** is set by the API and checked by a test.
- **TLS** is Cloud Run's and Caddy's.
- **CMEK and residency** are CTL-DP-01's and CTL-DP-07's cloud checks.
- **Retention in `infra/`, each with a module test, `[live-verify]`:**
  - Cloud Logging's application-log bucket is set to 30 days;
  - Cloud SQL's backups (point-in-time recovery 7 days, daily backups kept 35 days);
  - a monthly export kept one year in the Singapore bucket.

### D13. Penalty awareness (CMP-PD-09)

- **R7** ("Data breach of payroll or personal data") gains the penalty in its description: up to 10% of Singapore
  turnover above S$10m, otherwise S$1m (PDPA s48J(3) and reg 10A).
- **Every organisation.** The Data protection page states the penalty, and offers to add a PDPA risk to an
  organisation's register when none names it.

### D14. API, screens and rights

- **API.** Operations sit under "Data protection (PDPA)" (V for the Owner and Auditor, C/A for the DPO) and under "Own
  personal-data requests" (C for every member). Notices use the policy library's operations. The DPO fields are part
  of the organisation profile. Every write is audited, uses Idempotency-Key, and uses If-Match where the record is
  versioned. Matrix rows are added.
- **Screens:**
  - Compliance › Data protection (Expert, `/compliance/data-protection`), with tabs for the inventory and schedule,
    DPIAs, requests, breaches, transfers, shredding, and the DPO;
  - Compliance › Privacy notices and consent (Expert, `/compliance/privacy/notices`);
  - Settings › My personal data (Basic), where a member asks about their own data;
  - the join page's notice.
- **Canvas.** Local fragments now; the canvas after the design hold.

## Risks / Trade-offs

- **The legal basis is the company's, not Fyniq's.** Notices, the consent default, business contact information and
  the retention approach all carry [counsel to confirm]. Payroll data waits for counsel (open question 39).
- **Events before `@3` cannot be shredded.** Today they hold only developer data. Production starts at `@3`.
- **A restore must re-apply the register before the copy opens.** The same step already handles the erasure register,
  and the drill checks it (DR-06).
- **Personal fields are found by name.** A personal value under an unexpected key escapes sealing. The inventory's
  schema test and an event test (every personal key in the action catalogue's changes is sealed) narrow the gap.
- **An unapproved shredding run waits.** The reviews list shows it as due, so it cannot be forgotten.

## Migration Plan

- **Additive migrations:** the new tables, `person_key`, the library's widened key checks and the contact's
  `individual` field. New events are written as `@3`; earlier events verify as they are.
- **The restore runbook** (`docs/runbooks/audit-restore.md`) adds the shredding and anonymisation register.
- **Rollback.** Turn off the job and keep the keys. Nothing is shredded without the DPO's approval.

## Open Questions

- **AI conversations' retention.** §11.9 sets 1 year. There are no conversations until `p1-ai-assistant`. Build its
  setting and purge with the conversations there, moved by a PRD revision when this slice is reached, or build the
  setting here with nothing to purge.
- **Business contact information.** Counsel to confirm that contacts' staff details used in business are outside the
  retention and notice obligations, so that only individual contacts need keys.
- **The prescribed-data list** is checked against the Notification of Data Breaches Regulations 2021 when the breach
  register is built.
