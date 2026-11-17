## 1. The ACRA pack

- [ ] 1.1 `packs/sg-acra/2026.1`: `pack.toml` with its sources, `rules.csv` (deadlines, thresholds and guidance, each
  with its section, source and verdict) and `pinned/` tests; the loader and its activation under dual control, as the
  statutory packs' (design D1). A test holds the controllers' periods equal to the equity register's. Tests
  `// req: FR-CS-02`, `// req: governance/company-secretary FR-CS-02 Guidance kept apart from the Act`.

## 2. The corporate profile

- [ ] 2.1 `corporate_detail` (name, registered office, SSIC, effective-dated) and `corporate_officer` (directors, the
  secretary, auditors, the CEO, nominee directors): migrations, guards, grants, the storage port and the postgres
  adapter, with attack tests (design D2). Tests `// req: FR-CS-01`, `// req: FR-CS-06`.
- [ ] 2.2 `nominee_shareholding` and `statements_approval` (the directors' approval of a financial year's statements):
  the same (design D2, D4). Tests `// req: FR-CS-01`, `// req: FR-CS-03`.
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
