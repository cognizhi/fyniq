# Threat model: projects, time entry and timesheet approval

| | |
|---|---|
| Feature and requirement IDs | Projects, their people and tasks, time entries, the timer, weekly submissions and their approval: FR-PRJ-01, FR-PRJ-02, FR-PRJ-05, FR-PRJ-07, FR-ORG-06, FR-UX-40 (`p1-projects` slices 1 and 2). Rates, reports and billing follow in slices 3 to 5 and add their own cases here |
| Author, reviewer, date | Claude Code for the founders, 19 Nov 2026; second review at sign-off (waived before production, AGENTS.md) |
| Data and its class | A project's name, customer, budget and people (`general`); a person's time by project and day (`general`, personal to the person: only they and their project's managers see it). No pay data in these slices: a person's cost rate is payroll data (FR-PAY-13) and arrives with slice 3 |
| Who acts | The Accountant keeps projects (C on "Chart of accounts, dimensions"); an Employee logs their own time (C own on "Timesheets and project time"); the Approver (A, project manager) and the Controller (A) decide weeks; the Owner, Accountant, AR specialist, Auditor and Tax agent view |

## 1. What we are building

A project is a value of the Project dimension with a record (customer, billing, budget) and people: members log time on it and
managers approve that time. A person logs whole minutes on a task of a project they are on, saves their week, and submits each
project's part to that project's managers, who approve or return it. Approved time never changes; a correction is a reversing entry.
Time posts nothing to the ledger. The approval is the project's own, not a signed approval-engine request (design decision B).

Trust boundaries: **person ↔ API** (Appendix D rows, the handlers' own-only narrowing, `PRJ-09`); **manager ↔ API** (the approval
rule of design D4); **API ↔ database** (`fyniq_api`, RLS per organisation, the guards FY600 to FY604).

## 2. What can go wrong (STRIDE)

| # | Boundary | Threat | Impact | Likelihood |
|---|---|---|---|---|
| T1 | manager ↔ API | Elevation: a person approves their own time, or a member named a manager without the Approver role approves | Time nobody checked is approved and later billed | Medium |
| T2 | manager ↔ API | Elevation: a manager of one project approves another project's time | A cross-project grant | Medium |
| T3 | API ↔ database | Tampering: an approved entry is changed or deleted, or a reversing entry is forged (wrong minutes, twice, of a draft) | Approved, perhaps invoiced, time rewritten | Low |
| T4 | person ↔ API | Information disclosure: a person reads another's week, a project they are not on, or its submissions | Personal and commercial data disclosed | Medium |
| T5 | person ↔ API | Tampering: time logged on a project the person is not on, a closed project, or a task out of use; billable time on an internal project | Time billed that was never agreed | Medium |
| T6 | any ↔ API | A module switched off (FR-ORG-06) still answers, or a tenant reads another's projects | Hidden features used; cross-tenant disclosure | Low |
| T7 | person ↔ API | Tampering: two sessions save one week at once, or a stale save overwrites newer time | Lost updates | Medium |
| T8 | AR specialist ↔ API ↔ database | Tampering: work billed twice (two draft invoices naming the same time or cost), billed work marked by a forged payload, or a marked line edited away, a milestone billed unaccepted or twice, a milestone's amount changed after acceptance | Revenue overstated; a customer billed twice | Medium |
| T9 | person ↔ API | Information disclosure: the unbilled view or the report shows a person's cost rate; a manager reads another project's unbilled work | Payroll data disclosed | Medium |
| T10 | person ↔ API ↔ AI | Information disclosure: the project report's export or the semantic layer's project measures carry a cost or rate the viewer's own report would not (a person's cost in a file, a rate in a fact the assistant reads), or a project manager reads another project's figures through them | A person's pay inferred; another project's results disclosed | Medium |

## 3. Mitigations and the test that proves each

| # | Mitigation | Test |
|---|---|---|
| T1 | `timesheet.MayDecide`: never the person, A on the row, a manager or the Controller; the database repeats it (FY602) | `TestNobodyApprovesTheirOwnTime`, `TestAWeekOfTimeIsLoggedSubmittedAndApprovedByTheProjectsManager` (a named manager without the role: 403), `TestWhoMayDecideAWeek`, `TestAttackTimeIsDecidedByTheProjectsManager…` |
| T2 | The approval rule reads the project's managers; another project's manager is answered not found | `TestAWeekOfTime…` (Otto), the matrix rows `approveTimesheetSubmission` and `returnTimesheetSubmission` |
| T3 | `time_entry_guard`: approved entries never change or go in any role; a reversal is the exact opposite of an approved entry, once (FY603) | `TestAttackTimeIsDecidedByTheProjectsManager…` (every attack as the owner and the application role) |
| T4 | Personal rows narrowed to the caller; a submission is visible to its person, its project's managers and those who see every week; others get 404 | `TestAWeekOfTime…`, `TestProjectsAreKeptAndNarrowedToTheirPeople`, the matrix |
| T5 | `CheckWeek` and `time_entry_guard` (membership, task in use, closed, internal) | `TestAWeekIsCheckedWithEveryProblemAtOnce`, `TestAWeekOfTime…` |
| T6 | Every operation answers `PRJ-09` while the switch is off; `FORCE ROW LEVEL SECURITY` on every table | `TestProjectsRefuseWhileSwitchedOff`, `TestAProjectIsADimensionValueWithAttributes`, `TestEveryTenantTableForcesRowLevelSecurity` |
| T7 | `If-Match` with the week's ETag; every entry change at its version | `TestAWeekOfTime…` (stale and missing ETags) |
| T8 | The marks are written only by the `project_billing_follows_invoice` trigger when an invoice is posted, and refuse (FY606, `STORAGE-BILL-01`) time that is not approved billable time of the project, a cost that is not a posted bill or claim, and work already billed; no role may write the table; a void releases the work; `project_milestone_guard` keeps a milestone's name and amount, accepts it once and refuses one on the wrong billing method (FY607) | `TestTwoInvoicesNeverBillTheSameWork`, `TestAProjectBillsItsUnbilledWorkOnceThroughADraftInvoice`, `TestEachRoleHoldsExactlyItsGrants` |
| T9 | The unbilled view carries hours and billing rates only; the report values cost at the viewer's tier; a manager reads only their projects | `TestProjectReportsComeFromThePostedLedgerAndApprovedTime`, the matrix rows `getProjectUnbilled` and `draftProjectInvoice` |
| T10 | The export is built from the same report body as the screen, so a column the viewer may not see is not in the file; the workbook's person cost only for the payroll tier; the semantic layer has no cost measure, gives approved minutes (hours only) and the P&L by project, and a manager's reads are narrowed to their own projects and never the untagged lines; every export is audited with its hash | `TestProjectReportsComeFromThePostedLedgerAndApprovedTime` (the Controller's and the AR specialist's CSV, the Owner's workbook, an Employee refused, the audit event, revenue and approved minutes by project, no cost or rate field), the matrix row `exportProjectReport`, `TestEveryChangeToAToolIsANewVersion` (the tool is version 2) |

## 4. Residual risk

- The in-module approval carries no step-up or signed envelope (decision B): the audit trail and the database guards are the evidence. The
  owner confirms or chooses the engine route.
- A Controller approves any project's time and a founder holding several roles may approve another founder's: the matrix says so.
- Slice 3 adds the person-level cost rate, the real payroll-privacy surface (G4-050): its cases and tests join this document with it.
