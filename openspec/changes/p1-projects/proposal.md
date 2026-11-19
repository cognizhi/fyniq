# Proposal

## Why

A start-up that sells people's time needs to know what each project earns and costs, and the books alone cannot say:
salaries sit in payroll, not on the work. The workbook kept timesheets in a sheet nobody reconciled to the ledger
(`migration` archives them, rev 0.254). Fyniq has had the **Project dimension** since `p1-organisation-chart`: bills,
claims, invoices, bank lines and journals can already carry a project value on a line, and the balance service sums
by it. What is missing is the project itself (customer, dates, status, billing method, managers), the time people spend
on it, the rates that value that time, and the reports that put revenue, direct cost and staff time side by side.

On 26 Oct 2026 the owner moved projects after go-live (PRD-001 rev 0.147, `docs/prd/plans/p1-p3-build-plan-2026-10-16.md`
wave 2), feature-switched and off by default, to make room for expense claims and payment files. The founders have now
asked for the change to be planned and built; it stays behind `features.projects` (FR-ORG-06, off by default) and
nothing in it is on the go-live path.

## What Changes

- **Projects (FR-PRJ-01):** a project is the existing Project dimension's value plus an attribute row: customer,
  currency, dates, status, billing method (time and materials, fixed fee, retainer, internal), a budget in hours and in
  money, tasks, and its **members and managers**. A line already tagged keeps working. `/projects`, `/projects/:id`.
- **Time (FR-PRJ-02, FR-PRJ-05):** time entries in whole minutes by person, project, task and day, a billable flag and
  a note; the weekly grid and a start/stop timer; reminders for a missing day; **My time** in the personal workspace
  (`/me/time`, FR-UX-39); `/projects/timesheet` for the team. Approved and invoiced entries lock; a correction is a
  reversing entry plus a new one.
- **Approval (FR-PRJ-07):** a person submits a week; the **project managers** of each project approve or return that
  project's part. A manager is named on the project and holds the Approver role (or is the Controller); naming
  someone never gives them a right. Nobody approves their own time.
- **Rates (FR-PRJ-06):** effective-dated **cost rates** per person (payroll data, FR-PAY-13: the Owner and the Payroll
  administrator only), an Owner-set standard rate, and **role-average rates** over at least three people for everyone
  else; effective-dated **billing rates** per project, role, person or customer, in the project's currency.
- **Costs on projects (FR-PRJ-08):** bills, claims, mileage, bank lines and journals already carry the project; this
  change adds billable expenses with an optional markup and purchase orders per project.
- **Billing methods (FR-PRJ-10):** the **Unbilled** view (approved time at billing rates plus billable expenses with
  markup) and a draft invoice from it; fixed fee by milestone; retainer drawn down; internal projects never billed.
- **Reports (FR-PRJ-12):** profitability per project, customer and person (revenue − time at cost − direct costs),
  utilisation, realisation, unbilled work and budget burn, from **posted ledger lines only** and approved time, each
  figure with a drill to the lines and entries behind it; a tie-out of every project plus untagged to the ledger.
  Time never posts.
- **Checks:** C.1 #26 over budget or loss-making and #27 timesheet problems; C.2 #40 approved time not invoiced 30 days
  after period end and #41 missing or unapproved timesheets for a closed week.
- **Screens:** `projects`, `project-record`, `timesheet`, `personal-time`, `project-reports`, with help keys, the
  guide pages `projects.*`, and the compiled knowledge base.

## Capabilities

### New Capabilities

None. The requirements are already approved in the main specs (`projects/time-and-costs`, `controls/checks`,
`controls/platform-checks`); this change builds them (`skip_specs: true`). Where building finds the PRD wrong or silent
(design, "Decisions needed"), the PRD changes first, in the slice that builds the thing, tagged `(rev 0.N)`.

### Modified Capabilities

None.

## Impact

- **Requirements built:** FR-PRJ-01, FR-PRJ-02, FR-PRJ-05, FR-PRJ-06, FR-PRJ-07, FR-PRJ-08, FR-PRJ-10, FR-PRJ-12 and
  UC-PRJ-01, -02 and -04; C.1 #26, #27; C.2 #40, #41 (with the reports slice).
- **Not built here:** FR-PRJ-09 budgets per task, burn forecasts, alert thresholds and change orders (planned in this
  change's row, but outside the commission: hours and money budgets on the project and their use are shown, per-task
  budgets and alerts are left); FR-PRJ-11 revenue over time (P2, `p2-customer-portal-subscriptions`); the PWA timer
  (P2); the payroll-derived default cost rate and per-person working patterns and leave (they need payroll and
  FR-PPL-07, which live in Nemura); the AI draft of a time-and-materials invoice (`ar.draft_invoice` is in the
  catalogue; its project reads follow the human flow, design D9).
- **Code:** migrations 000600 to 000619 (projects, members, tasks, time entries, timers, submissions, rates),
  `internal/domain/{project,timesheet,projectreport}`, the postgres adapter, the API, the authorisation matrix, the
  web screens, settings, checks, the guide and the knowledge base.
- **Security:** payroll privacy (G4-050): a person-level cost rate or cost never leaves the server to a viewer
  without payroll rights, by UI, API, export, drill-down, metrics or the AI; the manager scope never widens a grant;
  a threat model at `docs/security/threat-models/projects.md`.
- **Estimate:** five slices, each its own pull request: projects; time and approval; rates; reports and checks;
  billing and project costs. About two working weeks.
