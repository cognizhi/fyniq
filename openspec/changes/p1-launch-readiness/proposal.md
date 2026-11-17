# Proposal

## Why

Fyniq goes live when every launch requirement is proved by a test and every quality gate of §16.2 holds. Nothing yet
says which requirements those are or checks them. The launch subset is a column of the build plan, not a file a gate
reads. `scripts/trace_requirements.py`, the P0 extractor QA-TRACE-01 names, was never written. Several gates of §16.2
have no test: the golden data sets, export round-trips, visual snapshots, Storybook, the tours, the role walkthroughs,
the end-to-end journeys, file safety, production verification and the release policy.

A diagnostic on 16 Nov 2026 found about 97 launch requirements with no `// req:` test across main and the stacked
`p1-company-secretary` chain. They fall into four groups:
- built but untagged, such as the equity checks C.2 #26 to #28, C.2 #49 and the palette validator;
- `[live-verify]` items that only the dev cloud can prove;
- requirements of launch changes that the owner moved after go-live, such as the GST F5 at registration and SOD-4 with
  payroll;
- real gaps.

Another 67 of Appendix G's 110 acceptance scenarios are named by no test. This change is wave 7's last change before
`p1-go-live` (PRD-001 §16.2, §16.3, §17.3, Appendix G).

## What Changes

- **The owner's decisions of 16 Nov 2026, taken interactively (PRD-001 rev 0.284):**
  - **QA-UI-02 stays as written:** Storybook stories for every shared component in light, dark and high contrast,
    with the axe add-on at 0 violations.
    - *Not chosen:* a Vitest theme matrix in its place, or Vitest at launch and Storybook in P2.
  - **QA-UI-07's visual snapshots are split:**
    - every pull request snapshots the key pages (Home, the Inbox, a financial statement, an invoice and Settings) at
      the five frames in light and dark, and a failure blocks the merge;
    - every P1 route is snapshotted nightly and before each release, and a failure blocks the next release.
    - *Not chosen:* every route on every pull request, or the key pages only.
    - The PRD row, its main spec (`quality/test-gates`) and the drift lock change in this proposal's commit.
- **The launch subset in the repository (§16.2, the build plan's open question 25):** `docs/prd/plans/launch-subset.json`
  lists the launch changes from the plan and, within them, every requirement and acceptance scenario due at launch.
  It records each exception with its decision and revision:
  - a requirement moved after go-live by the owner, such as FR-TAX-02 at GST registration, SOD-4 with payroll, the
    MCP server's suite, or Nemura's cross-product contracts;
  - a `[live-verify]` requirement that waits for the dev cloud, with the runbook or evidence that will prove it.
  `make roadmap-check` holds the file equal to the plan.
- **The trace gate (QA-TRACE-01):** `scripts/trace_requirements.py` reads PRD-001 and PRD-002's requirement tables (ID,
  priority, phase) and the tests' `req:` tags (`// req:` in Go and TypeScript, `# req:` in Python, `-- req:` in SQL).
  - It parses a Phase cell naming several phases, and a conditional priority (`M before GST registration`) as M.
  - It fails on an unparseable row, and on a row whose text records a phase move its Phase cell does not show
    (G4-001).
  - It leaves INV-* rows to QA-ENG-05's attack tests, and CTL-* rows to the control registry, which it checks covers
    them.
  - It reports S and C requirements without blocking.
  - `make trace` reports, as part of `make check`, which fails only on the extractor's own errors. `make trace-gate
    PHASE=launch` fails while a launch requirement without an exception is untested; a release runs it, and so does
    a nightly CI job.
- **Tags where the tests already prove a requirement:** each built-but-untagged requirement and acceptance scenario
  gets its `req:` tag on the test that asserts its acceptance criteria. A sample of the tagged tests is checked as
  QA-TRACE-01 asks.
- **The gates without tests, built:**
  - **QA-ENG-07 and the 13 unnumbered sets of Appendix G.1 and G.2:**
    - the workbook's sample company and its edge-case sets, ported from `zh-sheet-accounting` as fixtures with their
      verified results;
    - the pinned cases: document totals, FX and rounding on 7030, depreciation, unrealised revaluation, a balance
      converted to shares and a SAFE's conversion and dilution;
    - the hourly cost follows projects, after go-live.
  - **QA-RPT-03:** every export round-tripped. An XLSX is re-read figure by figure, including a value at 15
    significant digits and one above it written as text; a PDF's text is extracted and compared; a CSV's totals are
    reconciled.
  - **QA-RPT-04 and QA-UI-07:**
    - Playwright snapshots against the local stack on the sample company, in light and dark, at the five frames;
    - a journey per device class with a fold that keeps a draft;
    - the motion tests, with computed durations 0.
  - **QA-UI-01 and QA-UI-02:** a check that every shared component has its component test and its stories in three
    themes, and every form's validation messages a test; Storybook, pinned and at least two weeks old, with its
    test runner and the a11y add-on in CI.
  - **QA-UI-03:** the contrast and palette validators run on every pull request and fail it.
  - **QA-UI-06:**
    - the first-run tour completed by keyboard alone per role, with motion reduced and with animation, axe at 0;
    - nothing written but the tour state;
    - once `p1-app-shell` task 4.2 builds the tour.
  - **QA-UI-08:** a walkthrough per role of §7.1 against the local stack. It covers the landing page, the navigation,
    the Home blocks and the Inbox items, the not-available page, and the API's refusal of the same reads.
  - **QA-E2E-01:** browser journeys against the local stack:
    - set-up, record, sell, buy, pay, reconcile and close;
    - the GST monitor and calendar;
    - AI ask, and AI propose → approve → execute.
    Payroll's journey and the GST F5's follow their changes after go-live.
  - **QA-SEC-05:** one fixture suite of hostile files and exports, covering FR-DOC-08's files, FR-ORG-09's logos and
    FR-RPT-05's formulas, each neutralised, refused or bounded.
  - **QA-IMG-01:** the image checks of `make images` are named as the test that proves the gate.
  - **QA-PROD-01:**
    - a canary organisation whose synthetic transactions the scheduler posts and verifies each night, beside the
      ledger and audit-chain verifications, alerting on any failure;
    - its cloud run is `[live-verify]`.
  - **QA-REL-01:** a CI check that a `fix` of the engine, ledger, oracle or packs adds or changes a test, and the bug-fix
    skill's root-cause record.
  - **QA-MCP-01 at launch:** the MCP shell exposes no tool and refuses every call; the suite in full arrives with
    `p1-mcp-first-party` after go-live.
- **The acceptance scenarios of Appendix G:** each scenario of the launch subset is named by the test that asserts it,
  or recorded with its exception.
- **The UAT scripts:** `docs/uat/` holds one script per launch journey and role. Each lists its steps, what to see and
  the evidence to keep. The founders and the accountant run them on a staging copy of the sample company before
  `p1-go-live`.
- **What the gate finds elsewhere stays with its change:** for example, `p1-app-shell`'s accessibility, performance
  and localisation requirements, QA-AI-06's knowledge evals, and FM-6's advisory review. The launch cannot pass the
  gate until they are built or recorded as exceptions.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

None in this change's deltas: it builds approved requirements (`skip_specs: true`). The owner's rev 0.284 revises
QA-UI-07 in `quality/test-gates` directly, in the proposal's commit, as Recipe B does for a single PRD row.

## Impact

- **Requirements:** `quality/calculation-correctness` (QA-ENG-01 to QA-ENG-08, QA-RPT-01 to QA-RPT-04, QA-MIG-01,
  QA-PROD-01, QA-REL-01 and 13 unnumbered sets); `quality/test-gates` (QA-API-01 to QA-API-04, QA-MCP-01, QA-LOCAL-01,
  QA-IMG-01, QA-UI-01 to QA-UI-09, QA-TRACE-01, QA-SEC-03 to QA-SEC-05, QA-E2E-01, QA-PERF-01); Appendix G's
  acceptance scenarios (G4-001 to G4-110) of the launch subset. PRD-001 §16.2, §16.3, §17.3, Appendix G.
- **Screens:** none added or altered (`docs/design/screens/inventory.json` is unchanged). Storybook is a developer
  tool, not a product screen.
- **Code and tooling:**
  - `scripts/trace_requirements.py` and `docs/prd/plans/launch-subset.json`;
  - Makefile targets `trace`, `trace-gate`, `storybook`, `test-visual` and `test-journeys`, and their CI jobs;
  - Storybook and its runner in `apps/web` (new dev dependencies, pinned);
  - Playwright suites against the local stack;
  - fixtures under `test/golden/`, the canary job in `internal/jobs`, `docs/uat/` and the bug-fix skill.
- **Dependencies:** `p1-workbook-migration` (archived). `p1-app-shell` task 4.2 for the tours. The dev cloud for every
  `[live-verify]` item. `zh-sheet-accounting`'s synthetic sample company and tests, read-only, for the golden sets:
  synthetic data only (DEV-08).
- **Merging:** the stacked chain #643 to #733 is still merging, and holds migrations up to 000403; a migration here
  takes 000404 or later.
