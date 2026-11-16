# Tasks

Each slice is its own pull request from origin/main (design D10). Each passes `make check` and `make test` on its own,
and names the requirement IDs it proves.

## 1. The launch subset and the trace gate (QA-TRACE-01)

- [x] 1.1 `scripts/roadmap.py` writes `docs/prd/plans/launch-subset.json`, which `make roadmap-check` keeps current
  (design D1). It holds the launch changes, with their requirements and acceptance scenarios.
  `docs/prd/plans/launch-exceptions.json` is written by hand, its kinds `after-go-live`, `live-verify` and
  `covered-elsewhere`, each with its revision or evidence path. Tests `scripts/test_roadmap.py`:
  - a change whose Launch column changes moves in or out of the subset;
  - an exception without its reference fails.
  `# req: QA-TRACE-01`
  As built:
  - The subset holds 37 changes and 854 requirements, with the Appendix G scenarios found under each change's
    requirements.
  - The exceptions are seeded with the plan's own: `p1-gst`'s F5 part at registration, and SOD-4 with payroll after
    go-live (rev 0.25). Task 1.3 adds the rest.
  - The scripts' tests run with `make script-tests`, part of `make check`.
- [ ] 1.2 `scripts/trace_requirements.py` (design D2), with `make trace` in `make check`, `make trace-gate PHASE=…` and
  its nightly CI job; the report goes to `.data/trace/report.json`. Tests `scripts/test_trace_requirements.py`, on
  planted PRD rows and test files, prove QA-TRACE-01's scenarios:
  - tags in Go, TypeScript, Python and SQL;
  - an untested M row fails the gate, and S and C rows are reported;
  - an unparseable row and a recorded phase move that differs both fail, naming the row (G4-001);
  - several phases, and a conditional priority counted as M;
  - INV-* and CTL-* covered elsewhere;
  - both PRDs.
  `# req: QA-TRACE-01`, `# req: G4-001`
- [ ] 1.3 The first report, for this change: the untested launch rows listed by change in `docs/quality/launch-gaps.md`.
  Each is tagged where a test already proves it (task 2), built here (tasks 3 to 7), recorded as an exception, or left
  to its own change. Verified by `make trace` printing the same counts as the file.

## 2. Tags where the tests already prove the requirement (design D3)

- [ ] 2.1 The built-but-untagged requirements of the launch changes (the diagnostic of 16 Nov 2026, refreshed by 1.3).
  The tag goes on the test that asserts each one's acceptance criteria. Among them:
  - equity's C.2 #26 to #28;
  - C.2 #49's settlement residuals;
  - FR-VIZ-04, FR-VIZ-05 and FR-VIZ-07's validators;
  - QA-UI-03 and QA-IMG-01's checks;
  - FR-INT-17's generated client.
  Verified by `make trace` showing them covered, and by the reviewer's sample (D3).
- [ ] 2.2 The acceptance scenarios of Appendix G in the launch subset: each G4 scenario a test already asserts is
  tagged on it, and the rest are listed for tasks 3 to 7 or their own change. Verified by `make trace`'s scenario
  count.

## 3. Calculation correctness (QA-ENG-07, QA-RPT-03, the 13 sets of Appendix G.1 and G.2)

- [ ] 3.1 `scripts/workbook_golden.py` ports, from a pinned commit of `zh-sheet-accounting` (design D4), the sample
  company and the posting, non-posting, document-number, regression and miscellaneous sets into `test/golden/workbook/`,
  with their verified results and a README naming each source file. The Go engine and the oracle reproduce every
  figure. Tests:
  - `// req: QA-ENG-07`;
  - `// req: quality/calculation-correctness Golden data set, sample company` and the other five sets;
  - `TestTheFixturesAreTheRecordedOnes` (hashes and commit).
- [ ] 3.2 The pinned cases of Appendix G.2 that launch: document totals, FX and rounding on 7030, depreciation,
  unrealised revaluation, a balance converted to shares (RND-SHARE) and a SAFE's conversion and dilution. Each is
  worked by hand from its source, asserted in Go and in the oracle, and tagged with its requirement. The hourly cost is
  recorded `after-go-live` with projects (rev 0.146).
- [ ] 3.3 Export round-trip (QA-RPT-03) for every export of the launch:
  - financial statements, the cap table, a director's statement, the report exports and the tenant export's CSVs;
  - XLSX re-read figure by figure, including a value at 15 significant digits and one above it written as text;
  - PDF text extracted and compared;
  - CSV totals reconciled.
  Test `// req: QA-RPT-03`.
- [ ] 3.4 QA-REL-01: `scripts/release_policy.py` in CI (design D9), and the bug-fix skill's root-cause record. Tests
  `scripts/test_release_policy.py` (a `fix` touching the engine without a test fails; with one it passes).
  `# req: QA-REL-01`

## 4. Storybook and component coverage (QA-UI-01, QA-UI-02; rev 0.284)

- [ ] 4.1 Storybook, its a11y add-on and its test runner in `apps/web`, pinned and at least two weeks old (design D7).
  A theme decorator renders light, dark and high contrast. `make storybook` and the CI job build and run it. Verified
  by the job failing on a planted axe violation.
- [ ] 4.2 Stories for every shared component in `src/components/`, and `stories.test.ts` failing on a component without
  a story. `component-coverage.test.ts` fails on a shared component without a component test, or a form's validation
  message no test triggers. Tests `// req: QA-UI-01`, `// req: QA-UI-02`.

## 5. The browser gates against the local stack (QA-E2E-01, QA-UI-06 to QA-UI-08, QA-RPT-04)

- [ ] 5.1 The journey harness (design D6), each journey also checking what the API returns:
  - `apps/web/journeys/` runs Playwright against `make up` with the sample company loaded per journey;
  - the local sign-in page signs each journey in;
  - the replay model answers the AI.
  `make test-journeys` and its CI job. Verified by a smoke journey passing in CI.
- [ ] 5.2 QA-E2E-01's journeys: set-up, record, sell, buy, pay, reconcile, close, the GST monitor and calendar, AI ask,
  and AI propose → approve → execute. Payroll's journey and the GST F5's are recorded `after-go-live`. Test
  `// req: QA-E2E-01`, each journey naming the scenarios it asserts.
- [ ] 5.3 QA-UI-08: a walkthrough per role of §7.1, covering the landing page, the navigation, the Home blocks, the
  Inbox items, the not-available page, and the API's refusal of the same reads. Test `// req: QA-UI-08`.
- [ ] 5.4 QA-UI-07 and QA-RPT-04's visual snapshots (design D5; rev 0.284):
  - hash baselines in `apps/web/visual/baselines.json`;
  - the key pages on every pull request, every route nightly and in the release workflow, at the five frames, in light
    and dark;
  - diffs uploaded as artefacts.
  Then a journey per device class with a fold that keeps a draft, and the motion tests (computed durations 0 with
  motion Off, Reduce motion and `prefers-reduced-motion`). Tests `// req: QA-UI-07`, `// req: QA-RPT-04`.
- [ ] 5.5 QA-UI-06: the first-run tour per role by keyboard alone, reduced and animated, axe at 0. Nothing is written
  but `user_help_state`, and G4-104 resumes it. Waits for `p1-app-shell` task 4.2. Test `// req: QA-UI-06`,
  `// req: G4-104`.

## 6. Security and production gates (QA-SEC-05, QA-PROD-01, QA-MCP-01)

- [ ] 6.1 QA-SEC-05: one fixture suite of hostile inputs, each neutralised, refused or bounded:
  - files: macros, ZIP and XML bombs, polyglots, oversize and EXIF (FR-DOC-08);
  - logos: an SVG with script or a foreign reference (FR-ORG-09);
  - exports: formula injection (FR-RPT-05).
  Test `// req: QA-SEC-05`.
- [ ] 6.2 QA-PROD-01: the canary organisation and its nightly synthetic transactions (design D8), beside the ledger and
  audit-chain verifications. Its failure raises the verification alert. Test `TestTheCanaryPostsAndVerifiesNightly`
  `// req: QA-PROD-01`; the cloud run recorded `live-verify` with its runbook.
- [ ] 6.3 QA-MCP-01 at launch: the MCP shell exposes no tool and refuses every call, with each refusal audited. Test
  `// req: QA-MCP-01` (the shell's part). The full suite is recorded `after-go-live` with `p1-mcp-first-party`.

## 7. UAT and the launch gate

- [ ] 7.1 `docs/uat/`: one script per launch journey and role. Each has its steps, what to see and the evidence to keep,
  run on a staging copy of the sample company, with a sign-off line for the founders and the accountant. Verified by
  `scripts/test_uat.py`: every journey of 5.2 and every role of 5.3 has its script, and each step names a screen of the
  inventory.
- [ ] 7.2 `make trace-gate PHASE=launch` joins the release workflow. `docs/quality/launch-gaps.md` lists what still
  blocks, by change; each blocker is built in its own change or recorded as an exception with the owner. Verified by the
  release workflow's dry run failing on a planted untested row.
- [ ] 7.3 The build plan's paragraph says what was built and what waits. `make check`, `make test`, `make test-e2e` and
  `make test-journeys` pass.

## Workflow follow-up

- Archive once 7.3 passes and `p1-go-live` takes the launch gate.
