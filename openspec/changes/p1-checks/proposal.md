# Proposal

## Why

The workbook's promise was that nothing is ever silently wrong: its Checks sheet ran 37 checks, each ✔, ⚠ or ✖,
and a problem said what it was and where. Fyniq has the period close's checks (`internal/domain/closing`: the
balances, the trial balance, the sub-ledgers against their control accounts, the bank, recurring entries, variances,
the checklist) and the documents' own ✖/⚠ statuses, but no one place that runs every check, keeps its result, and
says how healthy the books are. Home's pulse card leaves a place for books health (`p1-dashboard-kpis` D5). This
change builds the checks engine (FR-CTL-01), books health (FR-CTL-02), the nightly verification on Checks
(FR-CTL-05), the user access review (FR-CTL-06), and every check of Appendix C whose module exists.

## What Changes

- **The owner's decisions of 29 Oct 2026, taken interactively (PRD-001 rev 0.162):**
  - each check is built with the module it guards. This change builds the engine and the checks of the modules
    built. The checks of payroll, projects, the founders' money, equity, the company secretary, AI, payment
    providers, GST filing and budgets join with those modules' changes. The P1–P3 plan moves those 37 IDs. The
    alternatives were listing them all as "not assessed" now, or keeping them in this change until each module came;
  - a check's owner is a role the catalogue names, or a person an Owner or Controller names instead, audited. The
    alternatives were a role only, or a person required for every check;
  - the user access review is a page of its own, **Settings › Access review**, which an Owner attests each quarter.
    C.2 #7 warns 14 days after a quarter ends without its attestation. The alternative was the API now and the screen
    with the Compliance Center.
- **Also defined in rev 0.162:**
  - books health is **problems** when any check finds ✖, **review** when any finds ⚠ or ⏰, and **OK** otherwise;
    checks not assessed are counted beside it;
  - a check that cannot run for want of an input is **not assessed** and names it;
  - a run that fails counts as ✖.
- **The checks engine** (`internal/domain/checks`):
  - a catalogue of definitions, each with its Appendix C reference, area, severities, plain words, the input it
    needs, its default owner role, the area a viewer needs to see its items, and the changes it reacts to;
  - each run records its result with the offending items (capped, with the total) and the ledger revision it read;
  - checks run on a relevant change (through the outbox, coalesced per organisation), nightly as a platform job, and
    on request;
  - every status change is audited.
- **The checks of the modules built:** 30 rows of C.1 (29 live: C.1 #34 is retired, replaced by C.2 #12) and 29 of
  C.2. Those the close already runs are moved into the catalogue and keep their behaviour there, so the close and
  Checks show one result.
- **Screens:**
  - **Checks** (`/checks`): the summary, the checks by area with their severity, words, owner, last run and items;
  - books health in Home's pulse card;
  - the new screen **Access review** (`/settings/access-review`), designed in this change
    (`docs/design/screens/src/AccessReview.html`; the auditor workspace moves to the "Workspaces by role" canvas to
    make room).

## Capabilities

### New Capabilities

None.

### Modified Capabilities

None. The rev 0.162 scenarios are already in the main specs `controls/checks` (FR-CTL-01, FR-CTL-02, FR-CTL-06) and
`controls/platform-checks` (C.2 #7), converted with the PRD revision. This change builds FR-CTL-01, FR-CTL-02,
FR-CTL-05, FR-CTL-06, INV-07 and the C.1 and C.2 rows the plan places in it (`skip_specs: true`).

## Impact

- **Migrations:**
  - `check_definition_owner`: the person named for a check;
  - `check_run` and `check_result`: append-only, RLS forced;
  - `access_review_attestation`: one per quarter, append-only.
- **The API:** `listChecks`, `getCheck`, `runChecks`, `setCheckOwner`, `getAccessReview`, `attestAccessReview`, and
  the books health in `getChecks`' summary.
- **Platform job** `checks.run`; outbox work `checks.refresh`.
- **Permissions:** a new Appendix D row "Checks and books health", added with its code in the first build slice,
  because the authorisation test reads the matrix. The access review uses "Users, roles, team mode" (Owner A to
  attest).
- **Screens and help:** Checks, Home's pulse card, Access review, their help keys and guide pages compiled into
  `kb/`, and the canvases `core`, `settings-admin` and `roles` republished.
- **Out of scope:**
  - the checks of later modules (above);
  - FR-CTL-03, the audit explorer (`p1-audit-explorer`);
  - the Compliance Center's view of the nightly result (`p1-compliance-center`), which reads this change's runs.
