## Why

The owner put the company secretary on the launch path in full (PRD-001 rev 0.274, open question 25). `p1-scheduled-agents`
built the framework and the company secretary assistant's first checks: the statutory calendar, six checks, and the
workspace at Compliance → Company secretary assistant. What the assistant still cannot check is the company itself:
it reports the corporate profile as Not assessed, because Fyniq keeps no officers, no registered-office history, no
nominee registers and no ACRA rules beyond the calendar's deadlines. This change gives Fyniq the corporate records and
rules that §9.12 asks for. With them the cross-checks run with or without the agent, and the assistant can draft the
documents a named human secretary signs.

## What Changes

- **The ACRA pack (FR-CS-02).** `sg-acra@2026.1` is an effective-dated, sourced pack, dual-approved like the statutory
  packs (§10.6). It holds the corporate rules the calendar does not:
  - officer and registered-office changes within 14 days;
  - the nominee registers, 7 days and then 2 business days;
  - the secretary's office, vacant at most 6 months;
  - minutes within one month;
  - written-resolution majorities and 14 days' notice of a general meeting;
  - the solvency and XBRL categories and the audit-exemption criteria.
  A point where ACRA's guidance differs from the Act, such as 14 days for share changes, is kept as guidance with its
  source. The rules come from the source notes of 12 Oct 2026, re-read against SSO and ACRA as the pack is made;
  anything not confirmed keeps `[verify]` and `[accountant to confirm]`.
- **The corporate profile (FR-CS-01, FR-CS-06).** Effective-dated records of these:
  - the company's name, registered office and SSIC code, beside the UEN, the FY end and the exempt-private-company
    status Fyniq already keeps;
  - the officers (directors, the secretary, auditors and the CEO) with their appointment and cessation dates, the
    named human secretary among them;
  - the nominee directors and the nominee shareholders.
  A change opens its lodgement deadline in the statutory calendar from the ACRA pack. The deadline is done once its
  lodgement evidence is attached.
- **The ACRA business profile (FR-CS-01).** The uploaded ACRA extract gains the officers and the registered office,
  typed in with the profile's PDF attached, as its members already are. The profile is reconciled to it, and each
  difference shows on C.2 #34.
- **The cross-checks (FR-CS-03).** Each is a check of Appendix C, so it runs without the agent:
  - C.2 #32, no secretary 6 months after incorporation, and a sole director as secretary (FR-CS-06), ✖;
  - C.2 #33, the financial statements not approved within 30 days of their deadline, ⏰;
  - C.2 #34, the profile differing from the latest extract, ⚠;
  - C.2 #35, a change without its lodgement evidence by its deadline, ⏰;
  - the nominee filings (G4-102).
  A check whose input is missing or out of date is Not assessed and names the input, never OK. The assistant's skill
  reads them in place of its corporate placeholder.
- **Document drafts (FR-CS-05).** `sec.draft_document` (tier 1, the Owner or the Company secretary approves) and its
  command `DraftCorporateDocument` fill a corporate template from the records:
  - directors' resolutions (approve the financial statements, allot shares, appoint or remove an officer, change the
    registered office);
  - a members' written resolution;
  - an AGM notice and minutes, or the papers for dispensing with the AGM.
  The draft is never signed or filed. Signed and uploaded, it becomes evidence in the minute book. A minute book entry
  not made within a month is a finding the assistant reminds about.
- **The Company secretary role (FR-CS-08).** The role's own cells (Appendix D, rev 0.6 and 0.280): C on the corporate
  profile, the registers and evidence; V on the equity register and the assistant's runs; A on `sec.*` proposals. It
  sees no transaction, payroll or bank detail unless granted. An external secretary's access is time-boxed (FR-ADM-01).
- **The screen.** `corporate-profile` (Compliance → Corporate profile and registers, Expert): the profile, the officers,
  the controllers and nominees, the extract and its differences, and each change's lodgement.

## Capabilities

### New Capabilities

None: the requirements are in the main spec already (`governance/company-secretary`, with C.2 #32 to #35 in
`controls/platform-checks` and `sec.draft_document` in `ai/task-catalogue`). This change builds them (`skip_specs:
true`).

### Modified Capabilities

None. FR-CS-04 (the workspace) was built with `p1-scheduled-agents`; FR-CS-07 (the annual-return pack) stays in P2.

## Impact

- **Requirements built** (PRD-001 §9.12, Appendix C.2, Appendix D, Appendix E):
  - FR-CS-01, FR-CS-02, FR-CS-03, FR-CS-05, FR-CS-06, FR-CS-08 (`governance/company-secretary`; FR-CS-04 already);
  - C.2 #32 to #35 (`controls/platform-checks`), `sec.draft_document` (`ai/task-catalogue`);
  - the acceptance scenarios G4-102 and G4-110.
- **Screens**: `corporate-profile` (its fragment updated locally; no Claude Design publishing).
- **Code**: the ACRA pack and its loader; the corporate profile's stores (migrations under FORCE RLS, with guards); the
  ACRA extract's officers and office; the checks; the assistant's skill, version 2; the task type's command and its
  templates; the role's grants; the screen; the agent eval cases.
- **Review**: the pack and the checks are statutory: the accountant's review is pending (`[accountant to confirm]`),
  and the pages that show them carry the "Needs additional verification" banner through the review register, in its
  own pull request.
- **Depends on** `p1-scheduled-agents` (in review) and `p1-equity-register` (built).
