# Design

## Context

See proposal.md for why. What exists today:
- `scripts/roadmap.py` keeps each change's wave, dependencies and launch column, and maps every requirement and
  screen after P0 to one change. `make roadmap-check` keeps the build plan's tables equal to it.
- Tests carry `// req: <ID>` tags in Go and TypeScript, and Python tests carry `# req:`. Nothing counts them: there is
  no `scripts/trace_requirements.py`.
- `make check` already runs the contrast and palette validators (QA-UI-03).
- `make images` checks the budgets and hardening (QA-IMG-01); the authorisation matrix, the differential and oracle
  suites, mutation testing and the attack tests carry their own tags.
- The Playwright suites in `apps/web/e2e/` answer every `/api` call with fixtures against `pnpm preview`.
  `scripts/local-smoke.sh` brings the real stack up from the local images behind Caddy (`localhost:8080`), with the
  local sign-in page and no cloud credentials.
- There is no Storybook, no visual snapshot, no canary organisation, no UAT script and no port of the workbook's sample
  company or pinned cases.
- The stacked chain #643 to #733 merges about one pull request an hour and holds migrations up to 000403.

## Goals / Non-Goals

**Goals:**
- One file says what launches, with every exception tied to a recorded decision.
- One script says what is untested, and a release cannot pass with a launch requirement untested.
- Each §16.2 gate of the launch subset has the test that proves it, or its recorded exception.

**Non-Goals:**
- Building other changes' missing features: `p1-app-shell`'s accessibility, performance and localisation
  requirements, the knowledge evals (QA-AI-06) and FM-6's advisory review stay with their changes. The gate lists them.
- The MCP security suite in full (`p1-mcp-first-party`), cross-product contracts (Nemura), payroll's journey and the
  GST F5's (after go-live by the owner's decisions).
- Proving `[live-verify]` items: this change lists them, and `p1-go-live` proves them in the dev cloud.

## Decisions

**D1. The launch subset is generated, the exceptions are written by hand.**
- `scripts/roadmap.py` writes `docs/prd/plans/launch-subset.json`, which `make roadmap-check` keeps current: each
  launch change (its column starts "Launch", or names a launch part as `p1-gst`'s does), its requirements and
  acceptance scenarios.
- `docs/prd/plans/launch-exceptions.json` is written by hand. Each entry names an ID, its kind and its revision:
  - `after-go-live`, with the owner's decision;
  - `live-verify`, with the runbook or evidence path that will prove it;
  - `covered-elsewhere`, for INV-* and CTL-*.
- An exception without a PRD revision or decision reference fails the gate.
- Rejected: the subset as a hand-kept list, which drifts from the plan; reading the plan's Markdown at gate time,
  which is the generated output and not the source.

**D2. `scripts/trace_requirements.py` follows the PRD, not the specs.**
- It parses the requirement tables of PRD-001 and PRD-002 into rows (ID, priority, phase cell). It reuses
  `scripts/prdspec.py`'s extractor, so one parser reads the PRD.
- A phase cell naming several phases lists the row at each; a conditional priority counts as M.
- A row whose text records "rev 0.N: P1" against another Phase cell, or a cell it cannot parse, fails the script and is
  named (G4-001).
- Tags are scanned with `git grep` over test files only:
  - `*_test.go`, `*.test.ts(x)`, `apps/web/e2e/*.spec.ts`, Storybook stories' play tests;
  - `oracle/` and `test/` Python, `db/` SQL tests.
- INV-* rows count when QA-ENG-05's attack tests name them. CTL-* rows count when `compliance/controls/` lists a
  check for them.
- Output: a JSON report in `.data/trace/report.json` and a summary.
- `make trace` prints the summary as part of `make check`, which fails only on the script's own errors.
  `make trace-gate PHASE=launch` fails while a launch row has neither a tag nor an exception; `PHASE=P1` serves the
  phase gate later.
- Rejected: tracing from the specs' scenarios, since the PRD is the source of truth and QA-TRACE-01 names it.

**D3. A tag counts only where the test asserts the criteria.**
- Tagging the built-but-untagged requirements is done by reading each test, never by pasting tags.
- The gate's report samples 10% of the tags added in this change, at least 20, for the reviewer. A pull request
  adding a tag names the scenario it proves.

**D4. Golden sets are ported from the workbook's synthetic data, read-only.**
- `zh-sheet-accounting`'s sample company and its edge-case and reference tests are read from a pinned commit of the
  private repository, as `scripts/workbook_migration_fixture.py` already reads the sample workbook for QA-MIG-01. That
  repository is synthetic: no real company data comes to a laptop (DEV-08).
- The inputs and the verified expected results are written as fixtures under `test/golden/workbook/`, in the same
  scenario format the differential suite already reads. Each set's README names its source file and commit.
- The engine and the oracle must both reproduce every figure. A disagreement is a bug, fixed through the bug-fix
  skill, never by editing the expected value.
- The workbook's Python is not run: its expected values are its own pinned numbers, so nothing from that repository
  executes here.
- (16 Nov 2026, the owner's decision, taken interactively) The sample company is ported by the migration's
  reconciliation: its 1,649 figures on the engine against the workbook's own engine, with the differential suite on each
  rule it posts by. It is not one oracle scenario, since it holds pay runs, which the scenario format has not and the
  launch does not need. Sets that post nothing (the non-posting cases, the miscellaneous set's other tests) are
  ported as a mapping of each case to the Fyniq test that holds the same rule, checked to name real tests.

**D5. Visual snapshots run against the local stack, with hashes kept and images uploaded.**
- Playwright runs in its pinned Linux container against `make up` with the sample company loaded. Fonts are vendored,
  animations off, the clock frozen and the sample data fixed.
- Baselines are SHA-256 hashes in `apps/web/visual/baselines.json`, not images in git. A changed hash fails the run
  and uploads both images and their diff as CI artefacts.
- A new baseline is approved by committing its hash in a pull request labelled `visual-baseline`.
- Key pages run on every pull request. Every route of the registry runs nightly and in the release workflow, at the
  five frames and in light and dark (rev 0.284).
- Rejected: images in git, about 2,000 PNGs of hundreds of megabytes; a hosted visual-diff service, which is outside
  local mode.

**D6. Journeys and walkthroughs use the real stack.**
- QA-E2E-01, QA-UI-08 and QA-UI-07's device-class journeys sign in through the local sign-in page.
- Each journey starts from a fresh organisation with the sample company loaded through `make seed-load`.
- Each asserts both what the screen shows and what the API then returns.
- ~~The AI journey uses the replay model the eval suites use.~~ Superseded on 17 Nov 2026 by the owner's decision,
  taken interactively: no model was recorded yet (recording needs a live key), and a recording is keyed on the exact
  request, tool results included, so a journey's fresh organisation never matches one. The AI journeys (AI ask; AI
  propose → approve → execute) use a **scripted local model**, in the local build only like the replay adapter: it makes
  the reads the journey names and cites what they return, or makes the named proposal. The journeys so cover the panel,
  the API, the orchestrator, the tools, numeric grounding, the proposal, its approval in the Inbox and the executor,
  offline on every pull request; answer quality stays the eval suites' (QA-AI-01…03). Rejected: normalising IDs and
  timestamps in replay keys and recording the journeys live (a key now, a re-recording at every prompt or tool change,
  and a key that could hide a request that really changed); running them live in the dev cloud only, with a launch-gate
  exception.
- Rejected: fixture-answered journeys for these gates, which prove the screens but not the journeys.

**D7. Storybook as written (rev 0.284).**
- Storybook, its a11y add-on and its test runner are added to `apps/web`, every version pinned exactly and at least two
  weeks old (ADR-T1's rule).
- Each shared component in `src/components/` has stories, rendered through a decorator in light, dark and high
  contrast.
- The test runner fails on any axe violation or missing story. `registry.test.ts`'s pattern adds a test that every
  component file has a story file.
- The CI job builds Storybook and runs the runner.

**D8. Production verification adds a canary organisation.**
- A seeded organisation, `canary`, gets a fixed set of synthetic transactions each night from the scheduler: an
  invoice, a receipt, a bill, its payment and a journal. The nightly verifications then check the trial balance, the
  documents' totals and the audit chain.
- A failure raises the same alert as a failed ledger verification.
- Locally it is a job and a test. In the dev cloud it is `[live-verify]`.
- **The canary writes as a client of the public API** (the owner's decision of 16 Nov 2026, taken interactively). The
  jobs run as the worker's database role, which by design creates no documents. An `integration`-class OAuth client
  of the canary organisation proves the whole stack and adds no grant; it waits for FR-INT-02's integration clients
  (`p1-public-api` task 3.1), and until then the job does nothing.
  - *Not chosen:* a database role of its own (a new role that writes ledger data in production, bypassing the API);
    an endpoint inside the API called by the scheduler (outside the public contract); after go-live (QA-PROD-01 only
    partly met at launch).

**D9. The release policy for calculation defects is a CI check.**
- `scripts/release_policy.py` fails a pull request that has a `fix` commit touching `internal/domain/`,
  `internal/engine`, `oracle/` or `packs/` without adding or changing a test in the same pull request.
- The bug-fix skill gains the root-cause record's template (QA-REL-01).

**D10. Merging beside the chain.**
- This change's pull requests start from origin/main and stay off the chain.
- Each touches generated files (`contracts/`, `docs/api/`, `schema.d.ts`) as little as it can. The chain's restacks
  regenerate them when they conflict, and a migration here takes 000404 or later.

## Risks / Trade-offs

- [Visual snapshots flake on font hinting or timing] → Run them in the pinned container only, with animations off and a
  frozen clock. A known-flaky route is listed with its reason and retried once, never skipped silently.
- [Tagging inflates coverage with weak tags] → D3's reading rule and the sample in the gate's report.
- [The golden sets find engine bugs late] → Each disagreement is a bug fixed through the bug-fix skill, and the
  differential suite gains the case. The schedule absorbs it before go-live, not after.
- [Storybook adds dependency churn] → Pinned versions and Dependabot's monthly group. Storybook builds only in its own
  CI job, outside `make test`.
- [The gate lists many gaps in other changes] → That is its purpose. The report groups them by change, so each change's
  owner sees its own list.
- [Merging beside the stacked chain costs conflict fixes] → D10, and the chain's driver already stops for a person on
  each conflict.

## Migration Plan

No data migration. Rollout:
1. The subset, the exceptions and the trace script land first, reporting only.
2. The gates land one by one, each blocking from its own pull request.
3. `make trace-gate PHASE=launch` joins the release workflow last. Removing it from the workflow is the rollback, and
   the report keeps running.

## Open Questions

None that change the approach. Each exception's wording is settled in its pull request, with the decision it cites.
