# The launch's gaps

The first report of the trace gate (QA-TRACE-01; `p1-launch-readiness` task 1.3), on 16 Nov 2026, with task 2's
tags and tests. `make trace` writes the live report to `.data/trace/report.json`; this page says how each gap closes.
It is kept by hand, and its counts are those of the day it was written.

## Counts

On main, the launch subset (`docs/prd/plans/launch-subset.json`) holds 37 changes and 854 requirements:

| | Requirements and scenarios |
|---|---|
| Covered by a tagged test | 655 |
| Excepted (`docs/prd/plans/launch-exceptions.json`) | 39 |
| Reported, not blocking: S and C rows, and the specs' prose requirements with no ID | 153 |
| Untested, blocking | 77 |

Once the stacked chain (#645 to #743) merges, 33 stay untested. The chain's tests cover 44.

## How each closes

### When the chain merges (44)

- `p1-ai-providers`: AI-12, CMP-AI-02, C.2 #55.
- `p1-ai-assistant`: AI-01 to AI-05, AI-18 to AI-25, AI-68, AI-69, QA-AI-01 to QA-AI-03, QA-AI-07, CTL-AI-06.
- `p1-scheduled-agents`: AI-61 to AI-65, AI-67, CTL-AI-09, C.2 #36, G4-076.
- `p1-company-secretary`: FR-CS-01, FR-CS-02, FR-CS-04, FR-CS-06, FR-CS-08, C.2 #32 to C.2 #35, C.2 #54, G4-102,
  G4-110.

### Tagged where a test already proves it (task 2, 24)

Each was read against its test before it was tagged (design D3); four tests gained the assertion a clause lacked, and
G4-054 gained its race test. The pull request lists each tag with the clauses its test asserts.

- `p1-statutory-packs`: G4-018, G4-019 (the tax point, the deposit's postings and the invoice deducting it).
- `p1-approval-engine`: G4-056.
- `p1-general-ledger-close`: G4-017, G4-053.
- `p1-multi-currency-fx`: G4-023 (its Box 7 comes with the F5, rev 0.25), G4-025.
- `p1-sales`: G4-045, G4-054.
- `p1-purchases`: G4-024.
- `p1-connector-framework`: G4-096.
- `p1-bank-import-reconcile`: G4-042, G4-059 (an AI-proposed rule waits for `p1-ai-proposals`' bank task types).
- `p1-equity-register`: C.2 #26, C.2 #27, C.2 #28, G4-043.
- `p1-reports`: G4-065, G4-098.
- `p1-planning`: G4-108.
- `p1-checks`: G4-037.
- `p1-ai-providers`: G4-088.
- `p1-security-hardening`: G4-097.
- `p1-launch-readiness`: G4-013 (the database's NaN refusal).

### Tests written for task 2 (9)

Built behaviour that no test asserted in full:
- G4-020: `TestGSTFollowsTheTaxPointAcrossTheRegistration`, invoices dated before the registration posting no GST once
  it is recorded;
- C.2 #49: the write-off whose approver then records the invoice's receipt, named for review (SOD-9);
- G4-044: before its template is enabled, the SAFE's conversion is the accountant's to complete;
- G4-105: deferred revenue S$10.00 short of its schedule fails the check, and the close's checks are not clean
  (C.2 #52 is P2);
- SEC-26: `TestEveryDependencyHasItsUpdates`. Building it found that Dependabot did not update the infrastructure's
  OpenTofu providers or the dev container's base image; both now have their updates, grouped with a 14-day cooldown;
- QA-UI-03, FR-VIZ-04, FR-VIZ-07 and QA-IMG-01: `scripts/test_quality_checks.py` runs the contrast, palette and image
  checks on planted failures, and checks the sequential and diverging scales from the tokens.

### Gates built (8)

- QA-REL-01 (task 3.4): `scripts/release_policy.py` in CI fails a `fix` of calculation code without its test; the
  bug-fix skill has the root-cause record's template.
- QA-MCP-01, the shell's part (task 6.3): the MCP shell exposes no tool and refuses every call with `MCP-OFF-01`,
  each refusal logged. The suite in full comes with `p1-mcp-first-party`, before any tool is exposed.
- QA-SEC-05 (task 6.1): `internal/hostile`, one suite of hostile files, logos and exports, each refused, neutralised
  or bounded.
- QA-RPT-03 (task 3.3): every export of the launch read back figure by figure from its XLSX, its PDF's text and its
  CSV, which reconciles. It found the XLSX writer rounding figures past 15 significant digits, now written as text.
- QA-UI-02 (task 4.1): Storybook, every shared component's stories run with axe in four themes (`make storybook`).
- QA-PROD-01 (task 6.2): the canary's nightly transactions through the public API, by the owner's decision of 16 Nov
  2026; its cloud run waits for the integration client (`p1-public-api` task 3.1) and is `[live-verify]`
  (`docs/runbooks/canary.md`).
- QA-ENG-07 (task 3.1): the workbook's golden sets in `test/golden/workbook/`, read from its repository by
  `scripts/workbook_golden.py`: the posting, document-number, regression and miscellaneous sets post through the engine
  and the oracle, which agree and reproduce the workbook's pinned figures; the non-posting cases are each mapped to the
  Fyniq test that holds the same rule; the sample company is the migration's reconciliation (the owner's decision of
  16 Nov 2026).
- QA-UI-01 (task 4.2): `src/screens/forms.test.ts` reads each of the 56 forms, the validation messages it writes and
  the tests that reach it; every message is triggered, and every form's tests check a refused field marked invalid
  and described by its message (`src/test/invalid.ts`). Building it found fields whose error a screen reader did not
  read with them: first-run setup's, a credit note's date applied, and an item price's currency.

### Built in this change (6)

- Tasks 5 to 7 (6): QA-RPT-04, QA-UI-06 with G4-104, QA-UI-07, QA-UI-08 and QA-E2E-01.
- Task 3's migration scenarios are in: G4-016, the revaluations in the migration register
  (`TestRevaluationAfterTheImportIsARuleChangeInTheRegister`), and G4-038, its bank half
  (`TestMatchingABankLineSettlesWhatItPays`), its migration half (`TestAMigratedForeignBillPaidInFullKeepsTheWorkbooks7030`)
  and G.3's June figure (`TestTheWorkbookGoldenSets`).

### In their own change (27)

- `p1-app-shell` (14): NFR-A11Y-03, NFR-A11Y-04, NFR-A11Y-05, NFR-PERF-UI-01, FR-UX-06, FR-UX-08, FR-UX-11, FR-UX-14,
  FR-UX-25, FR-UX-30, FR-UX-32, FR-UX-34 (the first-run tour, which QA-UI-06 waits for), FR-UX-37, G4-077.
- `p1-ai-assistant` (2): QA-AI-06, the knowledge evals, since the launch's assistant reads the knowledge base; G4-087,
  the pre-go-live check of the model IDs, proved in the dev cloud with `p1-go-live`.
- `p1-approval-engine` (2):
  - FM-6, the AI's advisory review in Founder mode. It launches, by the owner's decision of 16 Nov 2026, taken
    interactively: a scheduled agent like the company secretary reviews the week's journals, vendor bank changes and
    payments, and writes advisory findings into the weekly review, never approving. It is built on the scheduled agents
    of the chain, once they merge.
    - *Not chosen:* after go-live with `p1-ai-proposals`, or deterministic rule checks at launch with the AI later.
  - G4-057's pack half: no envelope yet holds the versions of the packs it used, so a pack activated while an approval
    waits restarts nothing. The policy half is `TestApprovalPolicyChangesAreApprovedAndKept`.
- `p1-users-roles` (2): SEC-07, its OAuth clients' classes, `private_key_jwt` and exact redirect URIs (task 3.1);
  G4-061, a refused sign-in matching an existing user's email, audited (FR-ADM-08).
- `p1-public-api` (1): FR-INT-02, the integration clients and personal access tokens (task 3.1).
- `p1-documents-evidence` (1): G4-068's contract, kept until the year its transactions complete (the bill's part is
  `TestRetainUntil`). G4-069, first listed here, is covered: its staging sweep is built (`evidence.Sweep`, the hourly
  `evidence.sweep` job) and was tagged with task 6.1.
- `p1-operations-dr` (2): G4-073's dead row, shown on Checks with a replay (its unknown email outcome and its posting
  once are tested); G4-085, the assistant saying it is paused while postings, approvals and the deadline alert go on.
  The budget that stops the calls is `TestBudgetsAdmitOnlyWhatTheyCover`; the assistant is the chain's, so G4-085
  follows its merge.
- `p1-reports` (2):
  - FR-VIZ-05, status colours always beside an icon and a label: some chips in a status tone show a label alone (Home's
    set-up steps, the invitations on Join);
  - G4-052: a re-run as at a board pack's revision shows today's names, flagged restated presentation; the pack's own
    names and grouping, and its stored PDF's hash, are not kept.
- `p1-scheduled-agents` (1): G4-103, an ACRA extract older than the last share change and a mandatory pack update not
  accepted, each not assessed with the pack version labelled.

## The exceptions

- **After go-live (14):**
  - the GST F5's part of `p1-gst`, at registration: FR-TAX-01 to FR-TAX-04, FR-TAX-12, C.2 #46, C.2 #47, G4-011,
    G4-021 (the input-tax claim date) and G4-035, G4-036 (the 2200 roll-forward against the boxes);
  - SOD-4, with payroll;
  - QA-API-04, with Nemura;
  - G4-046, contracted MRR, with P2's subscription schedules (rev 0.8).
  The others rest on rev 0.25.
- **Proved in the dev cloud (23):**
  - the controls whose checks run in the cloud (CTL-IAM-03, CTL-IAM-04, CTL-DP-01, CTL-RES-01, CTL-AUD-04, CTL-RES-03,
    CTL-CHG-04, CTL-CHG-05);
  - DR-01, DR-03, NFR-DR-02, NFR-PERF-06, NFR-AVAIL-01 and SEC-30;
  - the costs (NFR-COST-01 to NFR-COST-04, G4-078, G4-086);
  - the drills (G4-072, G4-074, G4-075).
  Each names its runbook or control file.
- **Covered elsewhere (1):** C.1 #34, the workbook's fast-mode staleness, which C.2 #12 replaced.
