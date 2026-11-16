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
- [ ] 2.3 The API: the profile, its officers, nominees and approvals, each recorded forward with Idempotency-Key and
  If-Match, with evidence; the permission matrix's rows; the data inventory and the tenant export (design D2). Tests
  `// req: FR-CS-01`, `// req: FR-CS-06`.
- [ ] 2.4 Each change's lodgement deadline in the statutory calendar from the ACRA pack, done once its evidence is
  attached (design D2). Tests `// req: governance/company-secretary FR-CS-01 A change opens its lodgement checklist`.
- [ ] 2.5 The ACRA extract's officers and registered office, and their reconciliation (design D3). Tests
  `// req: governance/company-secretary FR-CS-01 Reconciled to the ACRA business profile`.

## 3. The cross-checks

- [ ] 3.1 `corporate.secretary` (C.2 #32), `corporate.statements-approved` (C.2 #33), `corporate.acra-profile`
  (C.2 #34), `corporate.lodgements` (C.2 #35) and `corporate.nominees`, each Not assessed with its missing input
  named (design D4). Tests `// req: FR-CS-03`, `// req: C.2 #32`, `// req: C.2 #33`, `// req: C.2 #34`, `// req: C.2 #35`,
  `// req: G4-102`.
- [ ] 3.2 The company secretary skill, version 2: its checklist reads the corporate checks in place of the placeholder;
  re-pinned (design D5). Tests `// req: AI-64`, `// req: G4-110`.

## 4. Documents

- [ ] 4.1 The corporate templates (directors' resolutions, a members' written resolution, an AGM notice and minutes,
  the AGM dispensation papers), versioned in the repository, rendered to PDF (design D6). Tests
  `// req: governance/company-secretary FR-CS-05 Documents drafted`.
- [ ] 4.2 `sec.draft_document` and `DraftCorporateDocument`: the draft kept, never signed or filed; the signed copy
  uploaded as evidence into the minute book; the one-month minute book finding (design D6). Tests
  `// req: ai/task-catalogue sec.draft_document Draft a corporate document`, `// req: FR-CS-05`.

## 5. The role

- [ ] 5.1 The Company secretary role's own cells (Appendix D's note): C on the corporate profile, registers and
  evidence; V on the equity register and the runs; nothing on transactions, payroll or bank details; the matrix's
  expectations (design D7). Tests `// req: FR-CS-08`.

## 6. The screen

- [ ] 6.1 Compliance → Corporate profile and registers (`corporate-profile`, Expert): the profile and its history, the
  officers and the named secretary, controllers and nominees, the extract and its differences, each lodgement. Help
  keys, its guide page compiled into `kb/`, and the fragment updated locally (design D8). Tests `// req: FR-CS-01`,
  `// req: FR-CS-06`, Vitest with axe.

## 7. Evals and the finish

- [ ] 7.1 The `secretary-launch` suite's G4-102 and G4-110 cases (design D9). Tests `// req: QA-AI-07`.
- [ ] 7.2 The review register's entries for the pack and the checks (FR-UX-41), in a pull request of their own; the
  build plan's paragraph; `make check`, `make test` and `make evals` pass.
