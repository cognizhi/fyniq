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
- [x] 1.2 `scripts/trace_requirements.py` (design D2), with `make trace` in `make check`, `make trace-gate PHASE=…` and
  its nightly CI job; the report goes to `.data/trace/report.json`. Tests `scripts/test_trace_requirements.py`, on
  planted PRD rows and test files, prove QA-TRACE-01's scenarios:
  - tags in Go, TypeScript, Python and SQL;
  - an untested M row fails the gate, and S and C rows are reported;
  - an unparseable row and a recorded phase move that differs both fail, naming the row (G4-001);
  - several phases, and a conditional priority counted as M;
  - INV-* and CTL-* covered elsewhere;
  - both PRDs.
  `# req: QA-TRACE-01`, `# req: G4-001`
  As built:
  - The script reads 755 requirement rows of both PRDs through `prdspec.Source`. The PRD-001 payroll rows Nemura runs
    take their phase from NM-PAY-07's delivery map. A table with no priority column (the task types) counts as M.
  - Tags count in test files only: Go, TypeScript and e2e tests, stories, the oracle's and the scripts' Python tests,
    and SQL tests. A tag may name a capability and a title.
  - The launch gate blocks on a PRD row of priority M, a check of Appendix C or an acceptance scenario. It reports S
    and C rows, and the requirements the specs draw from the PRD's prose with no ID, as QA-TRACE-01 gates the PRD's
    rows.
  - `make trace` is part of `make check`. The daily `controls` workflow uploads the report. The release gate joins in
    task 7.2.
- [x] 1.3 The first report, for this change: the untested launch rows listed by change in `docs/quality/launch-gaps.md`.
  Each is tagged where a test already proves it (task 2), built here (tasks 3 to 7), recorded as an exception, or left
  to its own change. Verified by `make trace` printing the same counts as the file.
  As built (16 Nov 2026):
  - On main, 599 of the subset are covered, 125 untested, 166 reported and 34 excepted. Once the chain (#645 to #739)
    merges, 81 stay untested.
  - The exceptions add the rest of the F5 at registration (G4-011, G4-035) and QA-API-04 with Nemura, both rev 0.25.
    They also add 24 cloud-proved items, each naming its runbook or control file.
  - FM-6 launches, by the owner's decision of 16 Nov 2026: a scheduled agent writes advisory findings into the weekly
    review. It is built in `p1-approval-engine` on the chain's scheduled agents.

## 2. Tags where the tests already prove the requirement (design D3)

- [x] 2.1 The built-but-untagged requirements of the launch changes (the diagnostic of 16 Nov 2026, refreshed by 1.3).
  The tag goes on the test that asserts each one's acceptance criteria. Among them:
  - equity's C.2 #26 to #28;
  - C.2 #49's settlement residuals;
  - FR-VIZ-04, FR-VIZ-05 and FR-VIZ-07's validators;
  - QA-UI-03 and QA-IMG-01's checks;
  - FR-INT-17's generated client.
  Verified by `make trace` showing them covered, and by the reviewer's sample (D3).
  As built (16 Nov 2026), with 2.2's tags:
  - C.2 #26 to #28 tagged with 2.2. C.2 #49, G4-020, G4-044 and G4-105 gained the tests or assertions they lacked.
  - QA-UI-03, FR-VIZ-04, FR-VIZ-07 and QA-IMG-01: `scripts/test_quality_checks.py` (planted failures, and the scales
    read from the tokens); QA-IMG-01 also on the image archtests.
  - SEC-26: `TestEveryDependencyHasItsUpdates`. It found the OpenTofu providers and the dev container's base image
    without Dependabot updates; both now have them.
  - Not tagged: FR-VIZ-05 goes to `p1-reports`, since some status chips carry no icon; G4-085 to `p1-operations-dr`,
    since the assistant that says it is paused is the chain's. FR-INT-17 is an S row, so the gate only reports it, and
    its P1 part stays with `p1-public-api` task 3.3.
  - `make trace`: 632 covered and 88 untested.
- [x] 2.2 The acceptance scenarios of Appendix G in the launch subset: each G4 scenario a test already asserts is
  tagged on it, and the rest are listed for tasks 3 to 7 or their own change. Verified by `make trace`'s scenario
  count.
  As built (16 Nov 2026): the 48 candidates of 1.3, each read against its test.
  - 24 are tagged, 21 of them G4 scenarios. Four tests gained the assertion one clause lacked: G4-024's posting,
    G4-045's ageing credits, G4-065's download signed in as no one, and G4-098's `+1-2`. G4-054 gained
    `TestRaceTwoReceiptsForOneInvoice`.
  - Four are exceptions: G4-021, G4-036 and G4-046 after go-live, and C.1 #34 covered by C.2 #12.
  - Eleven went to tasks 2.1 and 3.1, and nine to their own change, as `docs/quality/launch-gaps.md` lists.
  - `make trace`: 623 covered, 97 untested and 38 excepted, from 599, 125 and 34.

## 3. Calculation correctness (QA-ENG-07, QA-RPT-03, the 13 sets of Appendix G.1 and G.2)

- [x] 3.1 `scripts/workbook_golden.py` ports, from a pinned commit of `zh-sheet-accounting` (design D4), the sample
  company and the posting, non-posting, document-number, regression and miscellaneous sets into `test/golden/workbook/`,
  with their verified results and a README naming each source file. The Go engine and the oracle reproduce every
  figure. Tests:
  - `// req: QA-ENG-07`;
  - `// req: quality/calculation-correctness Golden data set, sample company` and the other five sets;
  - `TestTheFixturesAreTheRecordedOnes` (hashes and commit).
  As built (16 Nov 2026):
  - The script reads the workbook's files as data (literals, dates and rows; nothing of that repository runs) at
    `a705fd14`, the commit the migration's sample is pinned at, and writes each set's scenario, a mapping of every row
    to the document it became or why it does not post, and `sources.json` with the hashes. `expected.json` is kept by
    hand from the workbook's tests, each figure naming its test (`test/golden/workbook/README.md`).
  - The posting edge cases: 71 figures of `tests/test_edge_posting.py` (document totals, Doc Lines amounts, what stays
    owed, 7030 by month, the spreads, each asset's depreciation, July's GST on the accounts) hold on the engine and on
    the oracle, which agree to the cent and both refuse the 8 ✖ rows the format can hold
    (`TestTheWorkbookGoldenSets`). Payroll waits for pay runs, which are not in the launch; the F5 boxes for F5
    (`p1-gst`).
  - The document numbers (18 figures), the regressions (9) and the miscellaneous cases (7, the unrealised FX of 210
    posted at each month's end and reversed on day 1 among them) hold the same way. The bank matcher's
    `TestAWorkbookNumberNamesOnlyItself` holds the workbook's numbers with `*`, `?`, `~` and `<`, and its spaced
    references. The posting set's payments with no reference are now recorded on account for their contact (FR-AR-18).
  - The non-posting set's 125 cases (`statuses.json`, read by the script) are each mapped to the Fyniq rule and the
    tests that hold it, or to after the launch (employees, projects, timesheets), or said to have no counterpart (the
    print sheet): `TestTheNonPostingCasesHaveTheirTests` checks every case is mapped once and every test named exists.
    The miscellaneous set's other tests are held the same way. Mapping them added
    `TestTheWorkbooksStudyWithoutAnInvestment` and `TestADiscountBudgetReducesRevenue` (the migration's sign rule for
    a discount budget, now its own function).
  - The sample company: the owner's decision of 16 Nov 2026, taken interactively, accepts the migration's
    reconciliation (QA-MIG-01: 1,649 figures against the workbook's engine) with the differential suite on each rule it
    posts by; it is not one oracle scenario, since it holds pay runs, which the format has not (design D4).
  - Tagged `// req: QA-ENG-07` and the six sets.
- [x] 3.2 The pinned cases of Appendix G.2 that launch: document totals, FX and rounding on 7030, depreciation,
  unrealised revaluation, a balance converted to shares (RND-SHARE) and a SAFE's conversion and dilution. Each is
  worked by hand from its source, asserted in Go and in the oracle, and tagged with its requirement. The hourly cost is
  recorded `after-go-live` with projects (rev 0.146).
  As built (16 Nov 2026):
  - The document totals, 7030's June and August, and the assets' depreciation hold in the workbook's posting set on the
    engine and the oracle (`TestTheWorkbookGoldenSets`).
  - The unrealised revaluation: the memo's 240 - 30 = 210 posts as each month-end's revaluation and is reversed on
    day 1 (the miscellaneous set, engine and oracle); on the imported sample, the revaluations the scheduler would post
    are the register's class (b) lines by account and month (`TestRevaluationAfterTheImportIsARuleChangeInTheRegister`,
    G4-016).
  - RND-SHARE's 10,288 and 329,217 shares were pinned by hand already (`TestSharePinnedCases`, the oracle's
    `test_rnd_share_allots_whole_shares_and_leaves_the_rest_owed`), and the SAFE's pre-money and post-money conversion
    and dilution (`TestP1SAFEConversion` on the engine and the oracle, `TestARoundIsModelledByHand`): tagged.
  - The hourly cost is in `launch-exceptions.json`, after go-live with `p1-projects` (rev 0.146).
- [x] 3.3 Export round-trip (QA-RPT-03) for every export of the launch:
  - financial statements, the cap table, a director's statement, the report exports and the tenant export's CSVs;
  - XLSX re-read figure by figure, including a value at 15 significant digits and one above it written as text;
  - PDF text extracted and compared;
  - CSV totals reconciled.
  Test `// req: QA-RPT-03`.
  As built (16 Nov 2026):
  - `internal/pdftext` reads back the text of the PDFs Typst writes: Flate streams and Type0 fonts with ToUnicode
    maps, a line per text block. Its tests print a report of two pages and an invoice through the pinned Typst.
  - The sheet writer did not keep ENG-10's boundary: a figure past 15 significant digits went out as a number cell,
    which a spreadsheet rounds. It now writes such a figure as exact text, and the sheet's footer says why; the CSV was
    already exact. `TestAReportRoundTripsFigureByFigure` re-reads 9,999,999,999,999.99 as a number and
    99,999,999,999,999.99 as text (G4-014).
  - `TestExportsRoundTripFigureByFigure` exports the P&L, the balance sheet, the cash flow and the trial balance as
    CSV, XLSX and PDF. Every amount the API gives is a number cell, a CSV decimal and printed text, and each CSV
    reconciles. The catalogue's trial balance and general ledger, prepared by the worker, hold the same balances and
    movements.
  - Added to existing tests: the cap table's holdings and the director's statement (its PDF signed as owed to the
    person, as it says) read back, and the tenant export's ledger balances entry by entry.
- [x] 3.4 QA-REL-01: `scripts/release_policy.py` in CI (design D9), and the bug-fix skill's root-cause record. Tests
  `scripts/test_release_policy.py` (a `fix` touching the engine without a test fails; with one it passes).
  `# req: QA-REL-01`
  As built (16 Nov 2026):
  - Calculation code is `internal/domain/`, `oracle/src/` and `packs/`. A test is a Go, TypeScript or SQL test, the
    oracle's tests, a scenario under `test/` or a pack's pinned cases.
  - A `fix` commit or a `fix` pull request title counts, since the squash merge takes the title. Merge commits are
    left out, so a restack's changes from main never count.
  - CI's commit job runs it on every pull request and push; `make release-policy` runs it locally.
  - The template is `.claude/skills/fyniq-bug-fix/templates/root-cause.md`, which the skill's §7 names.

## 4. Storybook and component coverage (QA-UI-01, QA-UI-02; rev 0.284)

- [x] 4.1 Storybook, its a11y add-on and its test runner in `apps/web`, pinned and at least two weeks old (design D7).
  A theme decorator renders light, dark and high contrast. `make storybook` and the CI job build and run it. Verified
  by the job failing on a planted axe violation.
  As built (16 Nov 2026):
  - Storybook 10.6.5 (27 Oct) with `@storybook/addon-a11y`. The test runner is `@storybook/addon-vitest`, with
    `@vitest/browser` 5.0.3, which matches the Vitest pinned already: each story runs as a test in headless Chrome
    (the runner's own, as the journeys use) and fails on any axe violation. `@storybook/test-runner` has no 10.x
    release. Telemetry is off.
  - The decorator applies `.dark` and `data-contrast` on `<html>` as the app does, from `VITE_STORY_THEME`.
    `make storybook` runs every story in light, dark, high contrast and high-contrast dark, then expects the planted
    story (`src/test/planted/`: an image without text, a button without a name) to fail.
  - Its own CI job, outside `make test`.
- [x] 4.2 Stories for every shared component in `src/components/`, and `stories.test.ts` failing on a component without
  a story. `component-coverage.test.ts` fails on a shared component without a component test, or a form's validation
  message no test triggers. Tests `// req: QA-UI-01`, `// req: QA-UI-02`.
  As built (16 Nov 2026):
  - Stories for the six shared components (19 stories). `coverage.test.ts` holds the component checks
    (`coverage.ts`), each failing on a planted component without a story or a test. The button and the dialog
    gained their component tests.
  - The forms: `src/screens/forms.test.ts` (`src/test/forms.ts`) reads every screen that marks a field invalid (56),
    the validation messages it writes, and the tests that reach it through up to three imports. Each message must
    appear in one of them, and one must check a refused field: `invalid()` (`src/test/invalid.ts`) checks it is
    marked `aria-invalid` and described by its message, waiting for a refusal that comes from the API. It fails on
    planted forms.
  - Writing the tests found fields whose error a screen reader did not read with them: first-run setup's three (now
    described by the summary's message), a credit note's date applied, and an item price's currency (its error sat
    on the price). The director settlement's check moved into `checkSettlement`, since the page always chooses an
    account and its message cannot be reached from the page. First-run setup gained its component test.

## 5. The browser gates against the local stack (QA-E2E-01, QA-UI-06 to QA-UI-08, QA-RPT-04)

- [x] 5.1 The journey harness (design D6), each journey also checking what the API returns:
  - `apps/web/journeys/` runs Playwright against `make up` with the sample company loaded per journey;
  - the local sign-in page signs each journey in;
  - the replay model answers the AI.
  `make test-journeys` and its CI job. Verified by a smoke journey passing in CI.
  Built: `cmd/seed --new-organisation` makes a journey's organisation with the sample company and an invitation per
  person; each person signs in through the local sign-in page and accepts theirs on "Joining by invitation", the path a
  person invited takes, since signing in alone makes no user. First-run setup runs once when the installation has none
  (`journeys/setup.ts`). The replay model is the local build's own (`internal/adapters/local/models.go`); the AI
  journeys use it when the assistant of `p1-ai-providers` reaches main (5.2). CI runs the journeys in the images job on
  the stack the smoke test and the DAST scan use, and keeps a failed journey's trace.
- [ ] 5.2 QA-E2E-01's journeys: set-up, record, sell, buy, pay, reconcile, close, the GST monitor and calendar, AI ask,
  and AI propose → approve → execute. Payroll's journey and the GST F5's are recorded `after-go-live`. Test
  `// req: QA-E2E-01`, each journey naming the scenarios it asserts.
  Built in part: set-up, record, sell, buy, pay, reconcile, close and the GST monitor and calendar pass on the local
  stack (`apps/web/journeys/README.md`). Left: AI ask and AI propose → approve → execute, when the assistant and the
  proposals of the AI chain reach main. (17 Nov 2026: the AI chain reached main in #771; the owner decided the two AI
  journeys run on a scripted local model, design D6.) Building them found the sample company's chart without the 2330 and 2410
  system accounts, so no sample month could close; the loader now adds the standard chart's accounts for the roles it
  lacks (FR-COA-03).
- [ ] 5.3 QA-UI-08: a walkthrough per role of §7.1, covering the landing page, the navigation, the Home blocks, the
  Inbox items, the not-available page, and the API's refusal of the same reads. Test `// req: QA-UI-08`.
  (17 Nov 2026, in part: `apps/web/journeys/walkthroughs.spec.ts`, one walkthrough per role of §7.1 (16; the line
  manager is a relationship and the HR officer is Nemura's), each alone in an organisation with the sample company:
  the landing page, the sidebar's areas in Expert mode, the company pulse on Home for the full-Home roles and never
  for the Approver, AR, AP and payroll roles, the Inbox, and a page outside the role's areas, which shows "This page
  isn't part of your access" while the API refuses its list (403). The sidebar, the personal workspace, the landings
  and the not-available page were built for it (p1-app-shell 2.1). **Left:** the Home blocks §7.1 names per role
  beyond the pulse (the Approver's "My department", the Bookkeeper's cash today and bank lines, the releaser's
  batches and balances, the Administrator's users and roles waiting, the DPO's data requests), the Auditor's
  workspace, and each role's Inbox items, which need records waiting for that role.
  17 Nov 2026, later: Home's **Your work** gives the Bookkeeper, the AR and AP specialists and the payment releaser
  their rows of §7.1 (p1-app-shell 2.1), and the walkthroughs check each role's rows, and none for the full-Home
  roles. **Left:** the Approver's "My department", the Administrator's, Company secretary's and DPO's blocks on the
  pages they land on, the Auditor's workspace, and each role's Inbox items.)
- [ ] 5.4 QA-UI-07 and QA-RPT-04's visual snapshots (design D5; rev 0.284):
  - hash baselines in `apps/web/visual/baselines.json`;
  - the key pages on every pull request, every route nightly and in the release workflow, at the five frames, in light
    and dark;
  - diffs uploaded as artefacts.
  Then a journey per device class with a fold that keeps a draft, and the motion tests (computed durations 0 with
  motion Off, Reduce motion and `prefers-reduced-motion`). Tests `// req: QA-UI-07`, `// req: QA-RPT-04`.
  (17 Nov 2026, in part: `make test-visual` (`scripts/visual.sh`, `apps/web/visual/`) starts a stack of its own with the
  business date fixed at 31 Dec 2026, makes an organisation with the sample company, a customer and a draft invoice,
  and runs Playwright in its pinned container (Makefile `PLAYWRIGHT_IMAGE`, both platforms): the key pages at the five
  frames in light and dark, the browser's clock and locale fixed, motion reduced, each picture's SHA-256 against
  `baselines.json`. A page the per-minute read limit refused is opened again after its Retry-After, and the viewport
  grows to the page's height so the frame is drawn where it stands. Two runs, each in an organisation of its own, drew
  the same 50 hashes. CI runs it in the images job on every pull request, uploads `visual-diffs`, and refuses a change
  to the baselines without the label `visual-baseline`; `scripts/visual.py approve` takes a run's hashes in. Until the
  first baselines are approved, from CI's linux/amd64 run, a run only records. The device-class journeys with a fold
  that keeps a draft are p1-app-shell 5.1's `e2e/devices.spec.ts`, and the motion tests `e2e/motion.spec.ts` (#772).
  `VISUAL_ALL=1` adds every signed-in route of the registry whose path names no record (102 pages, 1,020 pictures),
  drawn nightly (`.github/workflows/visual.yml`) and by release.yml's visual job, which the images job waits for, so a
  changed picture builds no image (archtest `TestTheReleaseWaitsForEveryRoutesPictures`). **Left:** the first
  baselines, from CI's first runs; a picture of a route that still changes run to run (a time of day) is masked once a
  nightly shows it; the run uploads the changed picture, not a drawn diff, as the baseline image is not kept.)
- [ ] 5.5 QA-UI-06: the first-run tour per role by keyboard alone, reduced and animated, axe at 0. Nothing is written
  but `user_help_state`, and G4-104 resumes it. Waits for `p1-app-shell` task 4.2. Test `// req: QA-UI-06`,
  `// req: G4-104`.

## 6. Security and production gates (QA-SEC-05, QA-PROD-01, QA-MCP-01)

- [x] 6.1 QA-SEC-05: one fixture suite of hostile inputs, each neutralised, refused or bounded:
  - files: macros, ZIP and XML bombs, polyglots, oversize and EXIF (FR-DOC-08);
  - logos: an SVG with script or a foreign reference (FR-ORG-09);
  - exports: formula injection (FR-RPT-05).
  Test `// req: QA-SEC-05`.
  As built (16 Nov 2026): `internal/hostile`, test-only. The fixtures are made in code, so no hostile file is kept in
  the repository, and each goes through the check the product applies to it.
  - Files, through `evidence.Check`: refused are a macro-enabled workbook, a ZIP bomb named as an XLSX, renamed Windows
    and Linux executables, HTML named as a PDF, an oversized file and a flagged one. A PDF that is also HTML is a PDF.
    A photo loses its location. A DOCX declaring an external entity or a billion laughs is kept as received and never
    parsed as XML: a local server proves the entity's address is never fetched.
  - Hostile file names keep no path or control character, and their download header reads back as one parameter.
  - Logos, through `brand.Check`: a script, event handlers, a foreign object, a stylesheet import and references by URL
    are taken out, and an SVG declaring an entity is refused.
  - Exports, through the sheet writer: a formula-like name and note are inert text in CSV and XLSX, and a negative
    amount is a number with an ASCII minus.
  - Reading the evidence tests for this also found that G4-069's staging sweep is built, so 2.2's route to
    `p1-documents-evidence` was wrong. G4-069 is tagged here instead.
- [x] 6.2 QA-PROD-01: the canary organisation and its nightly synthetic transactions (design D8), beside the ledger and
  audit-chain verifications. Its failure raises the verification alert. Test `TestTheCanaryPostsAndVerifiesNightly`
  `// req: QA-PROD-01`; the cloud run recorded `live-verify` with its runbook.
  As built (16 Nov 2026), on the owner's decision of the same day (D8: a client of the public API):
  - `internal/canary` makes its contacts on the first night, loading the standard chart if the organisation has none.
    It posts an invoice and its receipt, a bill and its payment, and a journal, then reads each back as posted, for
    its total and paid, and the month's trial balance in balance.
  - The platform job `canary.run` runs at 01:55 Singapore time, before the audit-chain and ledger verifications. A
    failed night fails the slot, which the heartbeat check alerts on. Its HTTP client comes from `internal/egress`.
  - The test runs two nights through the API, and a planted fault (an invoice read back for another total) and a
    missing account each fail the night.
  - Until the integration client (`p1-public-api` task 3.1), the job logs that no canary is configured.
    `docs/runbooks/canary.md` sets it up and proves it in the dev cloud `[live-verify]`.
- [x] 6.3 QA-MCP-01 at launch: the MCP shell exposes no tool and refuses every call, with each refusal audited. Test
  `// req: QA-MCP-01` (the shell's part). The full suite is recorded `after-go-live` with `p1-mcp-first-party`.
  As built (16 Nov 2026): `internal/mcpshell`, which `cmd/mcp` serves.
  - Every request under `/mcp`, whatever its method, gets 403 with an RFC 9457 problem coded `MCP-OFF-01`, which names
    no tool: initialize, tools/list, tools/call, a stream and a session's close.
  - Each refusal is logged as the security event `mcp.call_refused`, with its method, path, origin and whether it
    carried a bearer token, never the token or the body (SEC-15).
  - The shell holds no database, as §12.3 has it reach data only through the API, so the refusals are in the service's
    log, not the audit trail.
  - Test `TestTheShellExposesNoToolAndRefusesEveryCall`. QA-MCP-01 is covered by it until `p1-mcp-first-party` opens the
    server. That change's suite must pass before any tool is exposed, so no exception is recorded.

## 7. UAT and the launch gate

- [x] 7.1 `docs/uat/`: one script per launch journey and role. Each has its steps, what to see and the evidence to keep,
  run on a staging copy of the sample company, with a sign-off line for the founders and the accountant. Verified by
  `scripts/test_uat.py`: every journey of 5.2 and every role of 5.3 has its script, and each step names a screen of the
  inventory.
  As built (17 Nov 2026):
  - Ten journey scripts (set-up, record, sell, buy, pay, reconcile, close, the GST monitor and calendar, AI ask, AI
    propose → approve → execute) and eighteen role scripts, in the words of the screens and of the browser journeys.
    Payroll's journey and the GST F5's are after go-live (rev 0.25), as the README says.
  - `make uat-organisation` makes a script's organisation on the local stack, with the sample company and an invitation
    per person, as the journeys' harness does; each person signs in on the local sign-in page and joins. The dev cloud's
    run is `[live-verify]`.
  - `scripts/test_uat.py` reads QA-E2E-01's journeys and §7.1's roles from the PRD, so a change to either fails it until
    its script follows; it fails on planted gaps (an unknown screen, no evidence, no sign-off, a missing role).
  - The AI scripts' local set-up follows task 5.2's AI journeys once the chain's assistant merges `[verify]`. Writing the
    Company secretary's script found §7.1 and Appendix D disagreeing on the statutory calendar
    (`docs/quality/launch-gaps.md`).
- [x] 7.2 `make trace-gate PHASE=launch` joins the release workflow. `docs/quality/launch-gaps.md` lists what still
  blocks, by change; each blocker is built in its own change or recorded as an exception with the owner. Verified by the
  release workflow's dry run failing on a planted untested row.
  As built (17 Nov 2026):
  - `release.yml`'s first job, `launch-gate`, runs it on every release; the images job waits for it, so a release with
    an untested launch row builds, signs and attests no image. Its report is the artefact `release-trace-report`.
  - The dry run: `scripts/test_trace_requirements.py` runs the gate's step from `release.yml`, through the Makefile, on a
    planted repository: it fails on an untested row and passes once a test tags it.
    `TestTheReleaseWaitsForTheLaunchGate` (archtest) refuses a release whose images do not wait for the gate, or whose
    gate runs another phase, cannot fail, or may fail without failing the release.
  - `docs/quality/launch-gaps.md`: the 77 blockers by change, each with how it closes (44 with the chain).
- [ ] 7.3 The build plan's paragraph says what was built and what waits. `make check`, `make test`, `make test-e2e` and
  `make test-journeys` pass.

## Workflow follow-up

- Archive once 7.3 passes and `p1-go-live` takes the launch gate.
