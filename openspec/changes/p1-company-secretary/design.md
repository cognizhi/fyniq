## Context

`p1-scheduled-agents` runs the company secretary assistant on the statutory calendar and six checks, and reports the
corporate profile as Not assessed (`corporate:profile`). Fyniq already keeps part of the company:
- its UEN, legal name, registered address and financial years (the organisation profile, FR-ORG-01);
- the exempt-private-company status (FR-EQ-11) and the register of members, holders and controllers (FR-EQ-25);
- ACRA business-profile extracts of the members, reconciled to the register (FR-EQ-26, C.2 #26);
- the calendar's ACRA deadlines in `sg-deadlines@2026.1`: the annual return, the AGM and the statements sent instead;
- the controllers' register deadlines (7 days, 2 business days, the yearly notice) in the equity register.

It keeps no officers, no history of the registered office or name, no SSIC code, no nominee registers, no ACRA rules
beyond the calendar's, no minute book and no corporate document templates. PRD-001 §9.12 asks for all of them, as
effective-dated records reconciled to ACRA, with the checks running without the agent and the agent drafting
documents a person signs.

## Goals / Non-Goals

**Goals:**
- the ACRA pack, sourced and dual-approved, with guidance kept apart from the Act (FR-CS-02);
- the corporate profile's records, their lodgement deadlines and evidence, and the extract's officers and office
  (FR-CS-01, FR-CS-06);
- the cross-checks C.2 #32 to #35 and the nominee filings, each Not assessed when an input is missing (FR-CS-03);
- `sec.draft_document` and the minute book (FR-CS-05);
- the Company secretary role's own cells (FR-CS-08);
- the `corporate-profile` screen, the assistant's skill reading the checks, and the agent eval cases G4-102 and G4-110.

**Non-Goals:**
- the annual-return pack and XBRL readiness (FR-CS-07, P2);
- ACRA's API Marketplace (FR-CS-01's P2 scenario): P1 reconciles to an uploaded extract;
- filing anything with ACRA: no filing API exists, and the agent never files (AI-66);
- changing the existing controllers' deadlines, which stay in the equity register; the pack names the same rules and
  a test pins them equal.

## Decisions

**D1. `sg-acra@2026.1` (FR-CS-02).**
- `packs/sg-acra/2026.1/` follows `sg-deadlines`: `pack.toml` with its sources (SSO sections and ACRA pages, each
  retrieved, quoted and dated), `rules.csv`, and `pinned/` tests that hand-check each rule.
- A rule has a key, a kind (`deadline`, `threshold` or `guidance`), the event it counts from, a period (days, business
  days or months), the section, the source and a verdict (Act, ACRA guidance, or unclear).
- The rules:
  - an officer's appointment, cessation or change of particulars, and a registered-office change: 14 days (s173A, s143);
  - the secretary's office, vacant at most 6 months (s171(4A); 6 months from incorporation is ACRA guidance);
  - a nominee director informing the company (30 days), then the register (7 days) and ACRA's central register
    (2 business days) (s386AL);
  - a nominee shareholder: the register within 7 days, then the central register within 2 business days;
  - the controllers' register: 7 days, then 2 business days, and the yearly notice (s386AG, s386AH), equal to the
    equity register's;
  - minutes within one month (s188); 14 days' notice of a general meeting (s177); written resolutions by a majority
    or 75% (s184A–G);
  - the solvency declaration and the XBRL categories (revenue and assets each up to S$500,000 for Simplified XBRL), and
    the audit exemption (2 of 3: revenue and assets up to S$10 million, up to 50 employees; s205C, Thirteenth
    Schedule);
  - guidance: share allotments and transfers reported within 14 days, which the Act does not state (s63, s126).
- Loading and activation are the packs' own (§10.6): a version is activated under dual control, and Fyniq reads the
  active one on a day. Every figure in it carries `[accountant to confirm]` until the review.

**D2. The corporate profile (FR-CS-01, FR-CS-06).**
- `corporate_detail`: effective-dated versions of the company's name, registered office and SSIC code, each from a
  day. The UEN and financial years stay in the organisation profile, the exempt status in the equity register; the
  profile reads them.
- `corporate_officer`: a person (a member, or a name and identity kept only as the last four characters, SEC-15) in a
  role: `director`, `secretary`, `auditor` or `ceo`. Each has an appointment date, a cessation date once it ends, and
  for a director whether they are a nominee and whom for. The named human secretary is the officer in the `secretary`
  role; when they are a member, they hold the Company secretary role.
- `nominee_shareholding`: a holder of the equity register's who holds shares as a nominee, for whom, from when; the
  day the company's register entered it and the day ACRA's central register was told.
- Each record is effective-dated and changes forward only. A correction is a new version; nothing is deleted. Every
  step is audited, under FORCE RLS, with a guard, and has an attack test.
- A change opens a lodgement deadline in the statutory calendar from the ACRA pack: 14 days for an officer or the
  registered office, 7 days and 2 business days for a nominee. The deadline is done on its record once its lodgement
  evidence (the BizFile+ acknowledgement) is attached, as the registers' deadlines are (FR-DOC-06).

**D3. The ACRA extract's officers and office (FR-CS-01).** An ACRA extract gains the officers (name, role, appointed)
and the registered office, typed in with its PDF attached, as its holdings are. The reconciliation compares the
profile in force on the extract's day with the extract. Each difference, an officer missing on either side, a date or
the office differing, is a line of C.2 #34. An extract older than the last recorded change makes the check Not
assessed: it no longer shows the company.

**D4. The cross-checks (FR-CS-03).** Each is a check of the checks engine (Appendix C.2), so it runs without the agent.
Each is Not assessed, naming the input, when that input is missing or out of date:
- `corporate.secretary` (C.2 #32, ✖): no secretary 6 months after incorporation, or a sole director as secretary.
  Not assessed with no incorporation date.
- `corporate.statements-approved` (C.2 #33, ⏰): the financial statements of the last financial year not approved by
  the directors when their deadline is within 30 days. The deadline is the AGM, or sending them to members. The
  approval is a dated record per financial year (`statements_approval`), with its evidence. Not assessed with no FY
  end.
- `corporate.acra-profile` (C.2 #34, ⚠): the profile differs from the latest extract, or no extract is recorded, or it
  is out of date. The last two are Not assessed.
- `corporate.lodgements` (C.2 #35, ⏰): a change past its lodgement deadline without its evidence.
- `corporate.nominees` (G4-102, ⏰): a nominee's register entry or central filing past its deadline. It shows beside
  the controllers' notices the equity register checks already make.
The rest of FR-CS-03's list is checked already by the checks it names: the GST threshold, the equity register's,
director accounts in debit, ECI and Form C-S, CPF and IR8A, and filings without acknowledgement. The skill's checklist
names them.

**D5. The assistant reads them (AI-64).** The company secretary skill becomes version 2. Its checklist reads
`corporate.*` in place of the `corporate:profile` placeholder, and the run's findings take each check's status. The
skill's hash is re-pinned in `agents.lock.json`. A run queued under version 1 is skipped, saying its skill changed.

**D6. `sec.draft_document` and the minute book (FR-CS-05, Appendix E).**
- The task type (tier 1; the Owner or the Company secretary approves) names a document kind and its fields. The kinds:
  - directors' resolutions: approve the financial statements, allot shares, appoint or remove an officer, change the
    registered office;
  - a members' written resolution (ordinary or special);
  - an AGM notice (14 days, s177) and its minutes;
  - the papers for dispensing with the AGM.
- `DraftCorporateDocument` fills the kind's template from the records: the company's name, UEN and office, the
  officers, the financial year, the share classes. The dry run shows the text. Executed, it keeps a draft corporate
  document, never signed or filed, as the approving person.
- A person downloads it as PDF (the render service), signs it outside Fyniq and uploads the signed copy. The signed
  copy becomes evidence (FR-DOC-05) and an entry of the minute book.
- A meeting or written resolution recorded without its minute book entry within one month (s188) is a finding the
  assistant reminds about, with `sec.draft_reminder`.
- The templates are Fyniq's, versioned in the repository. They carry `[counsel to confirm]` until a lawyer reads them.
  An organisation does not edit them in P1.

**D7. The Company secretary role (FR-CS-08; Appendix D, rev 0.6 and 0.280).** `authz` gains the role's own cells on
the rows the note names: C on the corporate profile, the registers and evidence; V on the equity register and the
assistant's runs; and A on `sec.*` proposals (already built). Elsewhere it keeps the Board viewer's column. It sees no
transaction, payroll or bank detail. An external secretary is invited with an expiry, as FR-ADM-01 does for any
external member. The matrix's expectations follow the note.

**D8. The screen (`corporate-profile`, Expert).** Compliance → Corporate profile and registers shows:
- the profile in force and its history;
- the officers, with the named secretary marked;
- the controllers (linked to the equity register) and the nominees;
- the latest extract and its differences;
- each change's lodgement deadline, with its evidence or ⏰.
The checks' statuses show in both modes. Help keys and a guide page compiled into `kb/`; the fragment updated locally.

**D9. Evals (QA-AI-07).** The `secretary-launch` suite gains:
- G4-102: a nominee filing missing 2 business days after the register, and a controller with no notice for 12 months;
- G4-110: a profile with no FY end and no controller data, with the model unavailable. The findings name the missing
  inputs, the summary is never an all clear, and CMP-01 shows Not assessed.

## Risks / Trade-offs

- **Statutory rules without the accountant's review.** Mitigation: every rule is sourced, guidance is kept apart, and
  each carries `[accountant to confirm]`. The review register shows the "Needs additional verification" banner on the
  pages that show them (FR-UX-41).
- **Templates are legal documents.** Mitigation: drafts only, never signed or filed. Each is marked as Fyniq's draft
  for the named secretary to review, and `[counsel to confirm]`.
- **Officers' identity numbers are personal data.** Mitigation: only the last four characters are kept, never a full
  NRIC (SEC-15); officers are in the data inventory, and the AI exclusions mask them.
- **Two sources of the controllers' rules.** Mitigation: the equity register keeps computing them, and a pinned test
  holds its periods equal to the pack's.

## Open Questions

- Directors' written resolutions follow the constitution, which Fyniq does not read: the template follows the model
  constitution's unanimity `[verify]` `[counsel to confirm]`.
- Whether the central register of nominee shareholders takes 2 business days from the company's register or from the
  nominee's notice `[verify]` (the research notes used ACRA's guidance).
