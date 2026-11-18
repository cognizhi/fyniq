# Design

## Context

What already exists, found with `git grep` on `origin/main` (19 Nov 2026, PRD-001 rev 0.289):

- **The Project dimension.** `fyniq.dimension` (kind `project`, once per organisation) and `fyniq.dimension_value`
  (`000218`, `000221`); a journal line names a `dimension_set` (at most one value per dimension); bills, invoices,
  claim and mileage lines, bank-line parts and journals carry `dimension_value_ids` (rev 0.171); the balance cache is keyed
  by account × period × set (`balance.BySet`, `000219`/`000220`). Posted lines never change (INV-02). So **revenue and
  direct costs by project already exist in the ledger**; this change reads them and adds the project's own data.
- **The feature switch.** `features.projects` (off by default) is a setting and part of the session (`Features.Projects`),
  and the navigation hides what is off (`navFor`). Nothing on the server refuses a call when a switch is off yet.
- **The authorisation matrix.** Appendix D has the rows "Timesheets and project time" (Owner V, Controller A, Accountant,
  AR, Auditor and Tax agent V, **Approver A (project manager)**, Employee C (own), AI Proposal (draft invoice)) and
  "Project reports and cost rates" (V cells only; Controller, Accountant, Auditor and Tax agent at role-average rates,
  AR revenue and hours, Approver own projects at role-average rates, Payroll and Owner V). Both are in
  `internal/authz/policy.go` (`Timesheets`, `ProjectReports`) with no operation attached. "Chart of accounts, dimensions"
  (Owner and Controller A, Accountant C) is where dimensions and their values are kept today.
- **The approval engine** signs an envelope with a second factor in the last five minutes, for requester and approver
  (FR-APR-03, FR-APR-04), and narrows a step to roles or named members (`approval.Step`, `Eligible`).
- **The payroll-cost flag** is on accounts already (`payroll_cost`, FR-COA-02), so direct costs can exclude it.
- **No people or salary data in Fyniq.** There is no employee table; people are members (user IDs) and payroll lives
  in Nemura or an external system (FR-PPL-01, FR-PPL-06). The statutory calendar pack holds Singapore's public holidays
  (`internal/domain/deadlines`, `pack.ErrNoHolidays`); working patterns and leave arrive with FR-PPL-07.
- **Claims** (`p1-expense-claims`) show the pattern for "own" rights: Employee `C (own)`, the handler narrows to the
  caller's own records, a person who holds V or A sees all.

## Goals / Non-Goals

**Goals:** projects, time, rates, approval, reports and the unbilled view end to end in local mode; every figure from
posted ledger lines and approved time, in exact decimals, tying out to the ledger; payroll privacy held on every
surface; the five screens with their help.

**Non-Goals:** FR-PRJ-09's per-task budgets, forecasts, alerts and change orders; FR-PRJ-11 (P2); the PWA timer (P2);
the payroll-derived default cost rate, working patterns and leave (Nemura, FR-PPL-07); per-diem and the AI draft of a
time-and-materials invoice (design D9); the cloud (nothing here needs it).

## Decisions

### D1. A project is the dimension value plus one attribute row

`fyniq.project (organisation_id, dimension_value_id, customer_id, currency, status, billing_method, starts_on, ends_on,
budget_minutes, budget_amount, version, …)`, keyed by the existing `dimension_value`, whose dimension must be of kind
`project` (a trigger). A project's ID **is** its dimension value's ID, so every line already tagged keeps working and
the plain dimension API still lists it. Creating a project writes the value (code, name) and the row in one unit of work
(creating the Project dimension first when the organisation has none); a value made by the plain dimension API shows as
an unconfigured project until `PUT /v1/projects/{id}` writes its row. Statuses: `planned`, `active`, `on_hold`,
`closed`; a closed project takes no new time or tags (the value goes out of use with it, DIM-06). Billing methods:
`time_and_materials`, `fixed_fee`, `retainer`, `internal`. A project's currency is its billing currency (FR-PRJ-06); its
time cost and budget money are in the organisation's base currency.

`project_member (organisation_id, project_id, user_id, is_manager)` and `project_task (id, project_id, name, billable,
is_active)`. A **manager** is a member with `is_manager`. A project needs at least one manager unless it is `internal`
and the Controller holds its approvals (D4). Neither is ever deleted: a member or task goes out of use, as dimension
values do. Every table: `organisation_id` first, `FORCE ROW LEVEL SECURITY`, an audit trigger, grants by column.

*Why not a separate projects table with its own ID?* Two IDs for one thing, and a backfill of every line's set. The
extension row costs one join.

### D2. Time entries: integer minutes, immutable once approved, corrected by reversal

`time_entry (id, user_id, project_id, task_id, work_date, minutes, billable, note, status, submission_id, reverses_id,
invoice_id, …)`. `minutes` is an integer (−1,440…1,440, never 0); hours are shown as `h:mm` and decimals only at the
edge. **No floats anywhere:** a rate is `NUMERIC(20,4)` per hour and a cost is `round(minutes × rate ÷ 60)` to the
currency's minor units, half away from zero, per entry (**`RND-TIME`**, new in §10.4, PRD change in slice 2); a
reversal costs exactly the negative of what it reverses, so a corrected pair sums to zero.

Statuses: `draft` → `submitted` → `approved` (locked) or `returned` (editable again, with the manager's comment). A
database trigger refuses any change to an `approved` or invoiced entry, and any delete of one; a **correction** is a
new entry with `reverses_id` set and `minutes` the negative of the original, plus a new entry with the right values,
both submitted and approved again (UC-PRJ-01). One reversal per original (unique index). Entries are the person's own:
Employee `C (own)`; the handlers narrow by the caller. A person logs time only on a project they are a member or a
manager of, `active`, on a day not before the project's start (a warning, not a refusal, after its end).

The **timer** is one row per person (`time_timer`): started at the app's clock (`ports.Clock`, DEV-05), stopped into a
draft entry for the nearest whole minute, half up, at least one, on the day it started (a timer past 24 hours is
refused with how to fix it). The weekly grid saves a week's draft rows in one call.

### D3. Submitting a week: one submission per person, project and week

`timesheet_submission (id, user_id, project_id, week_start, status, submitted_at, decided_by, decided_at, comment)`,
the Monday as `week_start` (ISO, checked), one open submission per person, project and week (partial unique index).
Submitting a week creates one submission per project with draft or returned entries and links them; the managers differ
per project, which is why a week is not one request. A return reopens the entries with the comment; a resubmission is a
new submission row, so the history stays. Missing-day reminders (FR-PRJ-05) are computed, not stored: a weekday with no
entry for a person who is a member of an active project, less public holidays from the statutory calendar pack (an
organisation-set hours-per-week standard of 40 by default, FR-PRJ-12; leave and per-person patterns join with
FR-PPL-07).

### D4. Who approves, and the scope that never widens a grant (FR-PRJ-07, FR-UX-40)

The API allows a decision when **all** hold: the caller is a person; they hold A on "Timesheets and project time" (the
Approver or the Controller role, Appendix D); they are not the submitter; and they are a manager of the submission's
project **or hold the Controller role** (the matrix gives the Controller an unqualified A). Being named a manager never
confers A: a member without the Approver or Controller role named as manager sees the project and decides nothing, and
the project form says so. The database repeats the two rules that must never fail whatever the caller (a trigger on
`timesheet_submission`: `decided_by` ≠ `user_id`, and `decided_by` is a manager of the project or holds an active
Controller membership role), as `FY233` does for claims. The Owner holds V only: an Owner who is a project manager and
holds the Approver role decides as an Approver, not as an Owner.

A decision shows in the Inbox as an item for the manager, not as an area (FR-UX-40a): the Inbox lists the submissions
waiting for the caller beside approvals, and `/projects/timesheet` is the team view for those who hold the right.

### D5. Rates and the payroll privacy line (FR-PRJ-06, FR-PAY-13, G4-050)

- `person_cost_rate (user_id, role_name, effective_from, hourly_rate, base currency)` — **payroll data**: readable
  and writable only by the Owner and the Payroll administrator (a new Appendix D row, decision A). Audit events show
  such a rate by fingerprint, never in clear (AUD-02, `Sensitive`).
- `projects.standard_cost_rate` — the Owner-set organisation standard rate per hour (a setting, effective-dated like
  the other settings, `AmountSGD`; unset until the Owner sets it).
- `billing_rate (project_id | customer_id, role_name | user_id | null, effective_from, hourly_rate, currency)`: scope
  precedence person-on-project, role-on-project, the project's default, the customer's default; the rate in force on the
  entry's `work_date`; the currency is the project's.
- **Time at cost, computed on the server for the viewer's tier, never sent as a rate to be hidden.** A viewer with
  payroll rights (Owner, Payroll administrator) gets time at each person's own rate in force on the entry's date. Everyone
  else gets the **role-average rate** — the mean of the rates in force that day over the people in that role — when the
  role has at least **three** people with a rate, else the standard rate, else no cost (hours only, with the reason). Per-person
  profitability shows hours and standard value, never person-level cost, for such a viewer. The response says which basis
  it used (`rate_basis`: `person`, `role_average`, `standard`, `none`) so the screens can say so. The same function feeds
  the API, exports, drill-downs, the metrics endpoint and the AI read tool; one test per surface proves G4-050 and a
  differencing test proves one person's rate cannot be inferred from two viewers' totals when the role has three or more
  people.
- **Until payroll exists in Fyniq or Nemura**, the cost rate is entered by the Owner or the Payroll administrator. The
  workbook's default — monthly payroll cost (salary + employer CPF + SDL) ÷ standard hours — arrives with payroll and
  is a **Left** item; the entry form says "enter the rate; payroll will fill it in later".

### D6. Reports read the ledger through the balance service and nothing else (FR-PRJ-12)

- **Revenue** = project-tagged posted lines on revenue accounts; **direct costs** = project-tagged posted lines on
  expense accounts **without** the payroll-cost flag; project-tagged payroll lines are listed on a **reconciliation
  line**, never in direct costs (G4-049). Other account types are out of the margin. Both come from `balance.BySet` over
  the months chosen (balances are monthly), sums of the sets that contain the project's value; nothing reads journal lines
  around the service (ENG-04, the architecture test).
- **Time at cost** = approved entries (net of reversals) in the months chosen at the viewer's tier (D5), money by
  `RND-TIME`; time is a management view and **posts nothing**.
- **Margin** = revenue − time at cost − direct costs, in the base currency (a project billed in another currency is
  shown in the base currency from its posted lines, which already carry the base amounts). **Per customer** = the sum of
  its projects; **per person** = hours, billable hours and standard value, and time at cost only at payroll tier.
- **Utilisation** = billable minutes ÷ available minutes, available = 40 hours a week by default (organisation setting)
  as 8 hours a working day, less Singapore public holidays on weekdays from the calendar pack (the version in force; a
  year the pack has no holidays for shows "not assessed", as the calendar does) [verify the standard hours with the
  accountant]. **Realisation** = billed ÷ standard value, billed being the revenue posted to the project in the same
  months and standard value the approved billable minutes at billing rates.
- **Tie-out:** a test posts a small sample company's journals with project values, then proves that every project's
  revenue and direct costs plus the untagged remainder equal the trial balance's totals for those accounts to the cent,
  and that the project list's totals equal the per-customer totals (QA-RPT-01). Drill-down returns the ledger entries
  (by line) and time entries behind a figure; a drill returns a person-level amount only at payroll tier.

### D7. Feature switch on the server (FR-ORG-06, FR-UX-40d)

Every operation of this change refuses with `PRJ-OFF` (403: "Projects and time are switched off. An Owner turns them on in
Settings → Features") when `features.projects` is off for the organisation, whatever the caller's rights; the session
already carries the switch to the web app. Turning it off hides the module and keeps its data; tags on posted lines
stay (the ledger is immutable). The reminders and the checks of the module do not run while it is off.

### D8. Costs on projects (FR-PRJ-08)

Bills, claim and mileage lines, bank-line parts and journals carry the project already. Added: a **billable** flag and a
**markup percent** on a bill's and a claim's line that carries a project value (a line without a project is refused
with a plain message), and a project on a purchase order, carried to the bill made from it. The unbilled view lists
posted, billable, not yet invoiced lines by project; their billable amount = cost + markup, rounded `RND-LINE`, in the
document's currency. Card spend arrives as bank-line parts, which carry the project already.

### D9. Billing methods and the unbilled view (FR-PRJ-10)

- **Time and materials:** `GET /v1/projects/{id}/unbilled` lists approved, not invoiced entries (valued at billing rates
  at their work date) and billable expense lines (D8); `POST /v1/projects/{id}/draft-invoice` makes a **draft invoice**
  through the existing invoice command (lines carry the project value and the revenue account the AR specialist picks);
  posting that invoice (the existing approval) marks the entries and expense lines **invoiced** and locks them, by the
  entry's `invoice_id`. An invoice voided releases them. The AI's `ar.draft_invoice` task type already exists in the
  catalogue; its project reads (`projects.unbilled`) are an AI read tool and follow with the AI slice, so the flow
  works without the AI first.
- **Fixed fee by milestone:** milestones on the project (name, amount, due, accepted_on, invoice_id); accepting one
  enables its invoice (the same draft-invoice command). Revenue over time is FR-PRJ-11 (P2).
- **Retainer drawn down:** a retainer's monthly amount and a balance: time valued at billing rates draws down what an
  invoice for the retainer billed; the unbilled view shows the balance used and left.
- **Internal or non-billable:** no billing rates, no unbilled view; entries are never billable.

### D10. The checks

C.1 #26 "over budget or loss-making" and #27 "timesheet problems", and C.2 #40 "approved time not invoiced 30 days after
the period end on a time-and-materials project" and #41 "missing or unapproved timesheets for a closed week" are
checks in the checks catalogue, each with a fixture that triggers it (QA-RPT-02), skipped while the module is off.

## Decisions needed

Each is built on its recommended default, tagged *provisional until sign-off* in the PRD, as the repository does; the
owner confirms or changes it.

**A. Who sets person-level cost rates (a new Appendix D row).** Appendix D has no C cell on "Project reports and cost
rates", so nobody may write a rate.
- *Option 1 (recommended):* a new row **"Cost rates, person level (rev 0.N)"**: Owner V/C, Payroll administrator V/C,
  everyone else — (the AI —). Pros: exactly the people FR-PRJ-06 and FR-PAY-13 name for seeing them; no existing grant
  widens. Cons: a new row to maintain, and the Owner's "grant to another person" (FR-PRJ-06) waits for FR-PAY-13's grant
  mechanism, which comes with payroll.
- *Option 2:* give the Owner a C on the existing "Project reports and cost rates" row. Pros: no new row. Cons: mixes a
  report right with a payroll-data right; the Payroll administrator cannot enter rates.
- *Option 3:* no entry until Nemura's payroll supplies rates. Pros: no new payroll-data path in Fyniq. Cons: time at
  cost cannot work until after go-live and Nemura's payroll; the module has no cost.

**B. How the project manager approves a week (FR-PRJ-07 says "FR-APR-01 object").**
- *Option 1 (recommended):* an **in-module approval**: submit and decide through the project's own endpoints, by the
  managers (D4), recorded with who and when, audited, with the two database backstops of D4; no signed envelope and
  no step-up. Pros: weekly time approval is routine and low-risk (it posts nothing); the engine demands a second factor
  in the last five minutes of the requester and each approver and a request per person, week and project, which makes a
  busy team's Friday unusable. Cons: departs from the literal "FR-APR-01 object"; the approval policy page (FR-APR-09)
  cannot change who approves it.
- *Option 2:* a new approval-engine object type `timesheet` (one request per person, week and project, `Step.Users` =
  the project's managers, the envelope holding the week's minutes). Pros: literally FR-APR-01; versioned policies;
  signed. Cons: step-up for every submit and decision; two migrations to widen the engine's checks; about a third of the
  time slice again.
- Either way the Approver's cell stays "A (project manager)": the scope is by project, never wider.

**C. Cost rate until payroll exists.** Option 1 (recommended): the Owner or the Payroll administrator enters rates (A);
the standard rate is the Owner's fallback; the payroll-derived default is **Left**. Option 2: refuse time cost until
payroll supplies it (pro: one source; con: no margin before payroll).

**D. Can the Controller approve any project's time?** Appendix D gives the Controller an unqualified A. Recommended: yes,
as the matrix says. The Owner (V only) cannot.

**E. Self-approval.** The PRD forbids it in Standard mode; in Founder mode a founder holds several roles. Recommended: **never**,
in either mode (stricter than the PRD; the other founder approves). A solo organisation approves nothing of its own;
the time stays submitted and is reported as unapproved (C.2 #41).

**F. Who may log time on a project.** Recommended: its members and managers only (the project record names them).
Alternative: any member on any active project (less to set up; time on projects a person has no part in).

**G. Units and rounding.** Recommended: whole minutes; `RND-TIME` (D2) per entry, half away from zero; a timer to the nearest
minute. Alternative: hours to two decimals (loses the "2 h 15 min" of UC-PRJ-01 exactness; costs rounding at entry).

**H. Standard hours and holidays.** 40 hours a week and the statutory calendar's public holidays [verify with the
accountant]; no leave or per-person pattern until FR-PPL-07.

## Risks / Trade-offs

- **Two ways to approve** (this change's and the engine's) until the owner decides B; the Inbox shows both.
- **Role averages need three people**: a two-person firm sees the Owner's standard rate or hours only; that is the PRD's
  rule, and the screen says why.
- **BySet is monthly**: a report by day range is rounded to months; time uses the same months.
- **Report totals differ by viewer** (role average versus person rates); each report states its basis.
- **The AI**: `ar.draft_invoice` reads the project's unbilled amounts at the user's tier only; no write path is added.

## Migration Plan

Numbers 000600 to 000619, by slice (each CHECK widening is `NOT VALID`, then validated in the next number):
000600 projects, members, tasks; 000601 validate; 000602 time entries, timers, submissions; 000603 validate;
000604 cost and billing rates; 000605 validate; 000606 to 000609 reserved for the reports and the unbilled links
(`invoice_id`, milestones); 000610 to 000619 spare. Goldens with `-update`; `make db-lint`.

## Open Questions

- The accountant confirms the standard hours (40) and that time at cost is the margin's staff cost (FR-PRJ-12).
- Whether the Owner's grant of person-level rates to another person (FR-PRJ-06) is wanted before payroll exists.
