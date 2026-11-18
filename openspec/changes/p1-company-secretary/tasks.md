## 1. The ACRA pack

- [x] 1.1 `packs/sg-acra/2026.1`: `pack.toml` with its sources, `rules.csv` (deadlines, thresholds and guidance, each
  with its section, source and verdict) and `pinned/` tests; the loader and its activation under dual control, as the
  statutory packs' (design D1). A test holds the controllers' periods equal to the equity register's. Tests
  `// req: FR-CS-02`, `// req: governance/company-secretary FR-CS-02 Guidance kept apart from the Act`.
  As built:
  - Three tables, not one: `periods.csv` (a rule, its event, the rule it follows, a count in days, business days or
    months, the section and the verdict), `thresholds.csv` (the Simplified XBRL and audit-exemption figures) and
    `rules.csv` (rules with no period, such as who may be the secretary, and the solvent exempt private company's
    declaration instead of statements).
  - The verdict is `act` or `guidance`. The loader refuses `act` unless a cited source is of kind law or regulation,
    and `guidance` unless one is official guidance. ACRA's pages were read again on 16 Nov 2026; the Act's sections
    are as the 12 Oct notes read them on SSO, which refuses the fetch tool. So only s143, s171 and s173A are `act`; the
    controllers' and nominees' periods are `guidance` (the Regulations behind them were never read).
  - `pack.AcraDue` counts days, business days (from the day after, skipping weekends and sg-deadlines' holidays through
    `WorkdaysOf`) and months (the same day, or the month's last: 31 Aug and six months is 28 or 29 Feb, not the
    calendar's last-day rule). `PeriodOn`, `AcraThreshold` and `CorporateRuleOn` read the rest. Loading into the
    global tables and activation under dual control are the packs' own, unchanged.
  - Left for `sg-acra@2026.2`, as their only source is the Act on SSO: minutes within one month (s188), 14 days'
    notice of a general meeting (s177), written-resolution majorities (s184A–G) and the lapse of a share-issue
    approval (s161). Tasks 4.1 and 4.2 need the first two: they wait for a person to read SSO.
  - The open question on the nominees' central register is settled by ACRA's page: two business days "starting after
    you update your private registers".

## 2. The corporate profile

- [x] 2.1 `corporate_detail` (name, registered office, SSIC, effective-dated) and `corporate_officer` (directors, the
  secretary, auditors, the CEO, nominee directors): migrations, guards, grants, the storage port and the postgres
  adapter, with attack tests (design D2). Tests `// req: FR-CS-01`, `// req: FR-CS-06`.
  As built:
  - Both are document types in the document store, as the equity register's records are, so its port, adapter, grants,
    versions and evidence serve them: drafted, then recorded on another person's approval with a file attached, and
    never changed after. Migration 000392 adds the types, their approval operations and the guard
    `corporate_record_guard` (FY352 a detail, FY353 an officer); 000393 validates the widened checks.
  - A detail holds the name, the office, a five-digit SSIC code from a day, and the day it was lodged; the one in force
    on a day is the latest recorded to take effect by then (a correction of the same day wins).
  - An officer is one appointment: a role, a name, the member they sign in as if any, an individual's identifier as its
    last 4 characters (an audit firm's UEN), the days appointed and ceased and the day each was lodged, and a nominee
    director with whom they act for. A later version (a cessation, a lodgement, a correction) names the first and needs
    one recorded; the last recorded is in force. A director, the secretary and the CEO must be individuals.
  - The owner's decision of 16 Nov 2026 (rev 0.282): Appendix D gains the row "Corporate profile and registers" (Owner
    and Finance controller A, Accountant C, Auditor, Tax agent and Board viewer V, DPO M, AI V). The API for these two
    records is built here (part of 2.3): `getCorporateProfile` on a day, and create, get, replace, ask and evidence for
    each, with the matrix's rows. An officer never approves their own appointment (FM-1). The officers are a class of
    the data inventory; the tenant export already carries every document.
- [x] 2.2 `nominee_shareholding` and `statements_approval` (the directors' approval of a financial year's statements):
  the same (design D2, D4). Tests `// req: FR-CS-01`, `// req: FR-CS-03`.
  As built:
  - `nominee_entry` in place of `nominee_shareholding`: one record serves both registers (ROND and RONS), a director
    among the officers or a holder of the equity register, whom they act for, since when, and the days they informed
    the company, the register entered it and ACRA's central register was told, and when they ceased. So nominee
    directors have their register days too, which the officers' nominee flag alone could not hold.
  - `statements_approval`: the year's end, the day the directors approved the statements (after the year ends) and the
    day they were sent to the members; the last recorded for a year is in force.
  - Migration 000394 adds both, their operations and `corporate_register_guard` (FY354, FY355); 000395 validates. The
    API (part of 2.3): create, get, replace, ask and evidence for each, and the profile lists the entries in force on
    its day, every version, and every approval. A nominee never approves their own entry (FM-1).
- [x] 2.3 The API: the profile, its officers, nominees and approvals, each recorded forward with Idempotency-Key and
  If-Match, with evidence; the permission matrix's rows; the data inventory and the tenant export (design D2). Tests
  `// req: FR-CS-01`, `// req: FR-CS-06`.
  As built: with 2.1 and 2.2, each record's operations as it was built; `getCorporateProfile` shows the profile on a
  day. The tenant export already carries every document, so it needed nothing.
- [x] 2.4 Each change's lodgement deadline in the statutory calendar from the ACRA pack, done once its evidence is
  attached (design D2). Tests `// req: governance/company-secretary FR-CS-01 A change opens its lodgement checklist`.
  As built:
  - The calendar gains the profile's own items, done on their records as the registers' are: an officer's appointment
    after incorporation and a cessation (14 days, s173A), a change of the details after the first (14 days, s143),
    and each nominee entry's steps (told within 30 days, entered within 7, filed centrally 2 business days after).
  - Done once a version with the day lodged is recorded, with the acknowledgement as its evidence.
  - Incorporation is the first detail's day; with no detail recorded, an appointment is Not assessed and names the
    details. With no sg-acra version in force, one item is Not assessed and names the pack.
  - An event before the pack's first day (16 Nov 2026) has no rule, so no item, as the calendar's own rules start.
- [x] 2.5 The ACRA extract's officers and registered office, and their reconciliation (design D3). Tests
  `// req: governance/company-secretary FR-CS-01 Reconciled to the ACRA business profile`.
  As built:
  - The extract gains `officers` (name, role, day appointed) and `registered_office`, both optional so an extract typed
    before stays valid; migration 000396 replaces its guard (FY297) to check them.
  - `getCorporateReconciliation` compares the officers in office and the office on the extract's day: an officer missing
    on either side, a day appointed that differs, an office that differs. Names match whatever their case, spacing and
    punctuation. It is Not assessed, naming the input, with no extract, an extract with no officers typed in, or one
    older than the profile's last recorded change. C.2 #34 reads it (3.1).

## 3. The cross-checks

- [x] 3.1 `corporate.secretary` (C.2 #32), `corporate.statements-approved` (C.2 #33), `corporate.acra-profile`
  (C.2 #34), `corporate.lodgements` (C.2 #35) and `corporate.nominees`, each Not assessed with its missing input
  named (design D4). Tests `// req: FR-CS-03`, `// req: C.2 #32`, `// req: C.2 #33`, `// req: C.2 #34`, `// req: C.2 #35`,
  `// req: G4-102`.
  As built:
  - Four checks, not five: the nominee filings (G4-102) are lodgements of `corporate.lodgements` (C.2 #35), each past
    its deadline ⏰, as the officers' and the office's are; the calendar shows them due before then. A separate check
    would have needed an Appendix C row of its own.
  - `corporate.secretary`: ✖ with no secretary six months after incorporation (the first detail's day) or after the
    last one ceased, ⏰ within those six months, ✖ for a sole director as the secretary. The six months come from
    sg-acra through `pack.AcraDueSince`, the rule in force on the later of the event and the pack's first day, as a
    company incorporated before 16 Nov 2026 has no rule on its own day.
  - `corporate.statements-approved`: ⏰ for a year whose AGM, or statements to members, falls due within 30 days or
    has passed, with no approval recorded.
  - `corporate.acra-profile`: ⚠ for each difference of the reconciliation (2.5); Not assessed as it is.
- [x] 3.2 The company secretary skill, version 2: its checklist reads the corporate checks in place of the placeholder;
  re-pinned (design D5). Tests `// req: AI-64`, `// req: G4-110`.
  As built: the runner's checks (`secretaryChecks`) gain `corporate.secretary`, `corporate.statements-approved`,
  `corporate.acra-profile` and `corporate.lodgements`, and the `corporate:profile` placeholder is gone; each finding
  takes its check's status, Not assessed naming the input. The skill's checklist rows 1, 3, 6 and 7 name them, and
  `agents/agents.lock.json` pins version 2. A run queued under version 1 is skipped as its skill changed, by the
  runner's existing rule. The launch suite's "no financial year" case now expects the statements check Not assessed.

## 4. Documents

- [x] 4.1 The corporate templates (directors' resolutions, a members' written resolution, an AGM notice and minutes,
  the AGM dispensation papers), versioned in the repository, rendered to PDF (design D6). Tests
  `// req: governance/company-secretary FR-CS-05 Documents drafted`.
  As built:
  - Nine templates in `internal/domain/corporate/templates/`, each pinned by version and hash in
    `templates.lock.json`, so a changed template is a new version:
    - directors' resolutions: approve the statements, allot shares, appoint an officer, end the secretary's or CEO's
      appointment (the members remove a director), change the registered office;
    - a members' written resolution, ordinary or special;
    - the AGM's notice and minutes;
    - the notice that the statements were sent without an AGM (s175A).
  - Each template names its fields and its signers: the directors in office, the members holding shares, the
    secretary, or the chair. A paper with no one to sign is refused, naming what to record.
  - `corporate_document` is a document type (migration 000399, guard FY356 with an attack test; 000400 validates). It
    holds the kind, the template's version, the fields and the paper as filled from that day's records. Drafted again,
    it is filled from that day's.
  - The render service gains a paper layout (`render.Paper`, `paper.typ`), PDF/A-2a and PDF/UA-1 with snapshot
    hashes. Every paper carries a banner: Fyniq's draft, never signed or filed by Fyniq, and a template a lawyer has yet
    to confirm (`[counsel to confirm]`, rev 0.283).
  - The API, on the "Corporate profile and registers" row:
    - `getCorporateTemplates`;
    - `createCorporateDocument` (DraftCorporateDocument), with OwnerMay, as the Owner approves `sec.draft_document`;
    - `getCorporateDocument` and `replaceCorporateDocument`;
    - `getCorporateDocumentPDF`, whose content is masked for the DPO.
    The profile lists the documents.
  - The record operation (the signed copy into the minute book) is task 4.2's.
- [x] 4.2 `sec.draft_document` and `DraftCorporateDocument`: the draft kept, never signed or filed; the signed copy
  uploaded as evidence into the minute book; the one-month minute book finding (design D6). Tests
  `// req: ai/task-catalogue sec.draft_document Draft a corporate document`, `// req: FR-CS-05`.
  As built:
  - The minute book:
    - a drafted document takes its signed copy as evidence, after which its text stays;
    - `requestCorporateDocumentApproval` saves the day it was signed and asks another person;
    - `corporate_document.record` (migration 000401, validated by 000402) records it with the copy, never to change.
    The profile's recorded documents are the minute book.
  - `sec.draft_document` (tier 1, one document, the Owner or the Company secretary approves) maps to
    `DraftCorporateDocument`, the command `createCorporateDocument` runs. Its schema asks for the templates' kinds and
    fields, a test holding them equal.
    - The dry run shows the paper's text and drafts nothing.
    - Executed as the approving person, it drafts the document naming the proposal.
    - The text approved is its base: when a director, the secretary or a member changes meanwhile, it is stale and
      drafts nothing (AI-P5).
  - The executor gains SELECT on `holding_movement` (migration 000403), to name a members' resolution's signers;
    `grants.golden` shows the one line.
  - The eval suite `sec-draft-document` has two AGM minutes cases (the sample company records no officers).
  - The screen: the corporate profile gains "Papers and minute book", which:
    - drafts from each template's fields;
    - reads a paper's text, downloads its PDF and attaches the signed copy;
    - records it in the minute book with the day it was signed;
    - labels a draft the AI proposed "AI".
    The Inbox's proposal review shows a `sec.draft_document` dry run as its paper reads. The fragment is updated
    locally, not published.
  - Left for `sg-acra@2026.2`, by the owner's decision of 16 Nov 2026 (rev 0.283): the one-month minute book finding
    (s188), with the notice period (s177) and the written resolutions' majorities (s184A–G).

## 5. The role

- [x] 5.1 The Company secretary role's own cells (Appendix D's note): C on the corporate profile, registers and
  evidence; V on the equity register and the runs; nothing on transactions, payroll or bank details; the matrix's
  expectations (design D7). Tests `// req: FR-CS-08`.
  As built: `authz.SecretaryCells` gives the role C on "Corporate profile and registers" (rev 0.282) and on Evidence, V
  on the cap table (every holding) and on the AI company secretary; elsewhere the Board viewer's column. Evidence of a
  transaction needs a view of its own row, which the role does not hold, so C on Evidence reaches only the profile's,
  the registers' and the runs' files. The matrix test keeps its own copy of the note (`secretaryNote`), apart from the
  code, and checks every operation of those rows and the approvals for every role.

## 6. The screen

- [x] 6.1 Compliance → Corporate profile and registers (`corporate-profile`, Expert): the profile and its history, the
  officers and the named secretary, controllers and nominees, the extract and its differences, each lodgement. Help
  keys, its guide page compiled into `kb/`, and the fragment updated locally (design D8). Tests `// req: FR-CS-01`,
  `// req: FR-CS-06`, Vitest with axe.
  As built: `/compliance/corporate-profile` shows the four checks (C.2 #32 to #35), the details in force and their
  history, the officers in office with the named secretary, the nominee registers (controllers linked to the equity
  register), the comparison with ACRA, the lodgements due and the statements approvals. The Accountant and the Company
  secretary draft each record in a dialog, attach its evidence and ask for approval in place. The ACRA extract's form
  (Equity › Reconciliation) gains officers and the registered office. Guide `corporate-profile.md` compiled into
  `compliance.corporate-profile`, the equity guide and its page updated; the fragment rewritten to the built headings,
  not published (Claude Design is held).

## 7. Evals and the finish

- [x] 7.1 The `secretary-launch` suite's G4-102 and G4-110 cases (design D9). Tests `// req: QA-AI-07`.
  As built:
  - The suite is version 2. `nominee-and-controller-registers` (G4-102) plants on 1 Jul 2027:
    - a nominee shareholder entered in the register on 24 Jun and not filed centrally;
    - Lion Rock Fund I, a controller by its right to appoint the board's majority, with no yearly notice since 5 Jun
      2026.
    Both are overdue with a task each, and C.2 #35 is ⏰.
  - `model-unavailable` (G4-110): no financial year and no corporate profile, with a provider that never answers. The run
    keeps its findings, the GST return's task and each check Not assessed. Its report says the AI step did not run, and
    its summary that the AI could not explain (rev 0.283).
  - The harness gains, in the format:
    - a case's own plants;
    - a plant made as the Accountant or the Company secretary and recorded (evidence, ask, the Owner signs);
    - `$<id>` for an earlier plant's ID;
    - a wanted key ending in `*` that matches by prefix;
    - `"model": "unavailable"`.
    `TestTheAgentSuitesAreWellFormed` and `TestTheAgentJudge` cover them.
  - Building it found the harness's check findings were "has not run yet": nothing ran the checks before the monitor. Each
    case's day now runs them first, as the nightly checks do, and a finding that still says so fails the harness. The
    launch cases' check expectations are now real.
- [x] 7.2 The review register's entries for the pack and the checks (FR-UX-41), in a pull request of their own; the
  build plan's paragraph; `make check`, `make test` and `make evals` pass.
  As built:
  - The review register gains two areas in #734, which waits for a person:
    - `corporate` (the accountant): the pack's deadlines and the checks, on the profile, the calendar and the run
      report;
    - `papers` (a lawyer): the templates, on the company secretary workspace.
  - The build plan's paragraph says what was built and what waits.
  - The scheduled agents' threat model gains T11 (papers from a proposal) and T12 (papers and personal data).
  - Each slice passed `make check` and `make test` before it was pushed, and the last passed `make evals`.
