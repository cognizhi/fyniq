# Tasks

Five slices, each its own pull request. Migrations 000600 to 000619 only (design, "Migration Plan").

## 1. Projects (slice 1)

- [x] 1.1 Migration 000600 (and 000601 if a check is widened): `project`, `project_member`, `project_task` on the
  project dimension's values (design D1): RLS with `FORCE`, grants by column, audit triggers, the guard (a project's
  value is of the project dimension; versions move by one; nothing deleted; a closed project takes no new tasks);
  squawk clean; goldens updated.
  - Tests: tenancy and grants goldens; a project on a department value refused; a second tenant sees nothing.
    `// req: FR-PRJ-01`
- [x] 1.2 `internal/domain/project`: the project's draft read and checked with every problem at once (ENG-22, ENG-23):
  statuses, billing methods, dates, budget, currency, managers and members who are active members of the organisation,
  at least one manager unless internal.
  - Tests: each problem named with its code and fix. `// req: FR-PRJ-01`
- [x] 1.3 The API, contract first (`make openapi`): `listProjects`, `createProject`, `getProject`, `replaceProject`
  (ETag, If-Match), project tasks, `PRJ-OFF` when the switch is off (design D7); matrix rows
  (`TestAuthorisationMatrix`): project records are kept under "Chart of accounts, dimensions" (Accountant C), read
  under "Timesheets and project time" or "Project reports and cost rates" V.
  - Tests: the matrix rows; every operation refused while the switch is off; creating a project creates its dimension
    value and, when none, the Project dimension; audit events. `// req: FR-PRJ-01`, `// req: FR-ORG-06`
- [x] 1.4 Web: Projects (`/projects`, list with budget use, billing method, status, feature-off state) and the project
  record (`/projects/:id`: details, managers and members, tasks; the tabs for time, costs and billing arrive with
  their slices), the feature registry entries (`basic`/`expert`), help keys, the guide page `projects.projects`, and the
  compiled knowledge-base page.
  - Tests: Vitest with axe; the registry and help tests; the navigation shows Projects only when on and the person has a
    right. `// req: FR-PRJ-01`, `// req: FR-UX-40`
- [x] 1.5 Close the slice: `make check`, `make test`-level suites touched, a Conventional Commit with upgrade notes
  (new endpoints, new setting behaviour).

Built (slice 1, PR 843): migration 000600; `internal/domain/project`; the API with `PRJ-09`; Projects and the project record; the guide
page `projects.projects` and its knowledge-base page. Tasks 1.1 to 1.5 are ticked. The tests: `TestProjects*`, `TestProjectTasks`, the matrix
rows, `TestAProjectIsADimensionValueWithAttributes`, `TestAttackAProject…`, `src/screens/projects/projects.test.tsx`.

## 2. Time, submission and approval (slice 2)

- [x] 2.1 PRD change (rev 0.N, provisional): `RND-TIME` in §10.4 (and its spec); FR-PRJ-07's route (decision B); the new
  Appendix D row for person-level cost rates (decision A); `python3 scripts/prdspec.py stamp`, `make roadmap`.
- [x] 2.2 Migration 000602/000603: `time_entry`, `time_timer`, `timesheet_submission`; the triggers: a locked entry
  never changes or goes, one reversal per original, the Monday check, `decided_by` ≠ `user_id` and the manager-or-Controller
  rule (design D2 to D4).
  - Tests: attack tests on the triggers as the application role. `// req: FR-PRJ-05`, `// req: FR-PRJ-07`
- [x] 2.3 `internal/domain/timesheet`: the week grid, the timer, `RND-TIME` in `internal/domain/rounding`, the missing
  days (holidays from the calendar pack), submit, approve, return, reverse.
  - Tests: UC-PRJ-01's 2 h 15 min timer; a correction after approval; property tests for `RND-TIME` (a reversal costs
    exactly the negative; sums are exact). `// req: FR-PRJ-02`, `// req: FR-PRJ-05`
- [x] 2.4 The API: my week, save the grid, the timer (start, stop, current), submit the week, list submissions (mine,
  waiting for me, a project's), approve, return, reverse an entry; operation policies and the matrix rows: Employee
  `C (own)`, Approver only on their projects, Controller on any, nobody on their own time, the Owner never.
  - Tests: a member without the Approver role named as manager decides nothing; a manager of project X cannot decide
    project Y; self-approval refused in both modes; the matrix rows. `// req: FR-PRJ-07`, `// req: FR-UX-40`
- [x] 2.5 Web: My time (`/me/time`, personal workspace, FR-UX-39), the timesheet (`/projects/timesheet`: grid, timer,
  submit, the team's waiting weeks for a manager), the Inbox item "Time to approve", reminders shown on the grid; help
  keys, the guide page `projects.time`, the compiled page.
  - Tests: Vitest with axe; a person with personal rights only sees My time and nothing else of Projects.
    `// req: FR-PRJ-05`, `// req: FR-UX-39`
- [x] 2.6 Missing-day reminders as notifications on the scheduler's tick (Friday), skipped while the module is off.
  - Tests: a person with a missing day is reminded once. `// req: FR-PRJ-05`

Built (slice 2): migration 000602; `internal/domain/timesheet` and `RND-TIME`; the API (`/v1/timesheet`, `/v1/timer`, `/v1/time-entries`,
`/v1/timesheet-submissions`); My time and Timesheet; PRD-001 rev 0.293 (FR-PRJ-05, FR-PRJ-07, `RND-TIME`; the Appendix D row for cost rates waits
for slice 3). The Inbox card for weeks to approve is built (slice 2).

Built (reminder): the job `notify.timesheets` (daily at 16:00 Singapore time; on each organisation's Friday, one notice `time.missing` to each member of
an active project naming the weekdays of the week with no time, public holidays excluded, none while the module is off), its schedule, handler, watched-job
entry, event schema and notification type; PRD-001 rev 0.302 (FR-PRJ-05).

## 3. Rates (slice 3)

- [x] 3.1 Migration 000604/000605: `person_cost_rate`, `billing_rate`; the setting `projects.standard_cost_rate` and
  `projects.standard_hours_per_week` (40) in `internal/domain/settings`, with guide anchors and recommended values.
  - Tests: goldens; the column grants. `// req: FR-PRJ-06`
- [x] 3.2 `internal/domain/projectrate`: the rate in force on a date, the tier function (person, role average over at
  least three, standard, none), billing-rate precedence; the person-level rate is sensitive in the audit trail.
  - Tests: the three-person boundary (two people: standard; three: average), a rate change mid-month by work date,
    precedence. `// req: FR-PRJ-06`
- [x] 3.3 The API: cost rates (Owner and Payroll administrator only), billing rates, the standard rate; the project
  record's rates tab; the new matrix row; G4-050 on every surface that exists.
  - Tests: G4-050: a project manager without payroll rights opens per-person profitability, drills to time entries,
    calls the cost-rate API and asks the AI read tool: no person-level rate or cost anywhere; a differencing test.
    `// req: FR-PRJ-06`, `// req: FR-PAY-13`
- [x] 3.4 Web: the rates tab and My cost rates screen (Owner and Payroll administrator), help and guide.

Built (slice 3, PR 859): migration `000604` (`person_cost_rate`, `billing_rate`; the standard rate is a cost-rate row with no person, not a setting); `projectrate` (the
tier function, billing precedence); the Appendix D row "Project cost rates, person level" (PRD-001 rev 0.295); the audit trail masks `cost_rate`; Cost rates and the
project's Rates card; G4-050 tests.

## 4. Reports and checks (slice 4)

- [x] 4.1 `internal/domain/projectreport`: profitability per project, customer and person; direct costs excluding the
  payroll-cost flag and the reconciliation line; utilisation and realisation; budget use; each through `balance.BySet`
  and approved time at the viewer's tier (design D6).
  - Tests: the tie-out against the trial balance (every project plus untagged equals the account totals); G4-049
    (S$5,000 salary tagged to Acme, same people's time: staff cost counted once, the tag on the reconciliation line);
    utilisation across a public holiday; the architecture test that no report reads lines around the service.
    `// req: FR-PRJ-12`, `// req: QA-RPT-01`
- [x] 4.2 The API: `projectReports` with drill-down to ledger entries and time entries; metrics for the semantic layer
  at the viewer's tier; export to CSV with the ASCII minus and plain decimals.
  - Tests: exports at both tiers; the drill sums to the figure. `// req: FR-PRJ-12`
- [x] 4.3 Checks C.1 #26, #27 and C.2 #40, #41 with fixtures that trigger each (QA-RPT-02), off while the module is off.
- [x] 4.4 Web: Project reports (`/reports/projects`, expert), the project record's profitability, budget-burn and
  unbilled figures with drill, small-group warning, the basis chip; help and guide; the compiled page.
- [x] 4.5 The threat model `docs/security/threat-models/projects.md`, each abuse case with its test.

Built (slice 4, PR 864): `projectreport` (revenue and direct costs through `balance.BySet`, payroll-cost accounts listed apart, time at cost at the viewer's tier,
utilisation, realisation, budget use); `GET /v1/reports/projects` and the time-entries drill (hours only); Reports > Projects; checks C.1 #26, C.1 #27 and
C.2 #41; migration `000605` (a week's `submitted_at` from the application's clock); PRD-001 rev 0.296. Utilisation uses 8 hours a weekday less public holidays until
FR-PPL-07 supplies working patterns. Deferred then, built 20 Nov 2026 (PRD-001 rev 0.331): the CSV and Excel export of 4.2 (`GET /v1/reports/projects/export`, the caller's own columns, audited), the semantic layer's
Project dimension and `project_minutes` measure (the `metrics.query` tool is version 2; hours only, never a rate or a cost; a manager sees only their projects), and the project
record's Profitability card of 4.4 (the report's row at the viewer's tier, the rate named, the small-group warning, the approved entries on request, Export CSV). The time-cost
margin is not a semantic measure by decision (it depends on the viewer's tier; the assistant is never given a person's cost).

## 5. Billing and project costs (slice 5)

- [x] 5.1 Billable and markup on bill and claim lines that carry a project; a project on a purchase order, carried to its
  bill (FR-PRJ-08); migrations 000606 and following.
  - Tests: a line with markup and no project refused; the markup amount by `RND-LINE`. `// req: FR-PRJ-08`
- [x] 5.2 The unbilled view and the draft invoice for time and materials; invoiced entries lock and a voided invoice
  releases them; the C.2 #40 fixture (design D9).
  - Tests: UC-PRJ-02: 62.5 approved hours at billing rates plus a S$420 expense at 10% markup; posting locks them.
    `// req: FR-PRJ-10`
- [x] 5.3 Fixed fee by milestone and the retainer drawn down; internal projects never billed.
- [x] 5.4 Web: the Unbilled tab, milestones, retainer; help and guide.

Built (slice 5a, PR 866): `RND-MARKUP`; billable and markup on bill lines and claim receipts and trips (`PRJ-10`, `PRJ-11`); a project on a purchase order carried to its bill;
PRD-001 rev 0.297. (slice 5b, PR 869): migration `000606` (`project_billed_item`, the invoice trigger, `FY606`); the unbilled view and `POST /v1/project-invoice-drafts`;
`projectbilling`; check C.2 #40; PRD-001 rev 0.298. A flat route is used because a command under a record's path needs If-Match (FR-INT-09). (slice 5c, PR 872):
migration `000608` (`project_milestone`, `FY607`, the trigger reads `milestone_id`); milestones, retainer periods and the drawdown balance; PRD-001 rev 0.301.
Invoiced entries are locked by the trigger's marks (an entry is billed once), not by a column on the entry, and the marks are made when the invoice is posted.

## Left (named, not forgotten)

- **Fixed-fee revenue over time** (FR-PRJ-11, P2): left out, as the PRD says. The CSV export, metrics and profitability card of 4.2 and 4.4 are built (rev 0.331).

- **FR-PRJ-09** per-task budgets, forecasts at completion, alerts at 80% and 100%, change orders: in this change's plan
  row, outside the commission; the project's own budget in hours and money and its use are built (slice 4).
- **The payroll-derived default cost rate** (FR-PRJ-06), **working patterns and leave in available hours**
  (FR-PPL-07): need payroll and Nemura.
- **The PWA timer** (P2); **FR-PRJ-11** revenue over time (P2); **the AI draft** of a time-and-materials invoice, as an AI
  read tool for the unbilled view (needs the AI slice's read-tool registry and its red-team cases).
- **The Owner's grant of person-level rates** to another person (FR-PRJ-06; FR-PAY-13's grant mechanism).
- **Decisions A to H** (design): the owner confirms or changes them; each is built on its recommendation.
