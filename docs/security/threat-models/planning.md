# Threat model: planning (forecasts, budgets, the cash projection, feasibility studies)

| | |
|---|---|
| Feature and requirement IDs | Planning: FR-PLN-01 to FR-PLN-03, FR-PLN-05 to FR-PLN-09, FR-RPT-02, FR-RPT-03, FR-GL-14, FR-KPI-01, FR-NTF-01 (PRD-001 rev 0.214 to rev 0.229; `p1-planning` task 8.5) |
| Author, reviewer, date | Claude Code for the founders, 8 Nov 2026; second review at sign-off (waived before production, AGENTS.md) |
| Data and its class | Forecasts, budgets and feasibility studies (internal plans); the books' figures they read (confidential). The cash projection reads every open invoice and bill, its contact's name and its amount, and how each contact has paid over the last 12 months (confidential, business data about customers and suppliers). No personal data beyond member IDs and contacts' names, and no bank numbers or secrets |
| Who acts | Appendix D's "Budgets, forecasts" row: V (Owner, Controller, Accountant, Approver, Auditor, Tax agent, Board) reads; C (the Accountant, and the Owner and Controller through A) drafts, changes and records forecasts, drafts budgets and keeps studies; A (Owner, Controller) approves budgets. The daily job runs on `fyniq_worker`; the API on `fyniq_api`. The AI has no planning tool |

## 1. What we are building

- **Forecasts** model the company forward from drivers. A recorded forecast is the company's plan and Home's; its
  results are worked out on every read.
- **Budgets** come in versions. Another person approves one through the approval engine, which records and locks it.
  The year's budget feeds Budget vs Actual, the P&L's budget column, the close's budget variances and Home.
- **The cash projection** shows the money accounts' balance day by day from the open items, recurring documents and
  approved batches. Its what-ifs move or leave out an item.
- **The buffer alert** is a daily job that notifies C and A holders once per crossing.
- **Feasibility studies** hold one idea's inputs, worked out at sg-cit's rate.

None of it posts (FR-PLN-08). Trust boundaries:

- **person ↔ API**: the Appendix D row, SEC-02;
- **API and worker ↔ database**: RLS per organisation, and the document guards FY300 to FY302;
- **job ↔ notification**: the audience and the title.

## 2. What can go wrong (STRIDE per boundary)

| # | Boundary or step | Threat (STRIDE) | Impact | Likelihood |
|---|---|---|---|---|
| T1 | person ↔ API | Tampering: a forecast run, a what-if or a study changes the books, a document, a planned payment date or a batch | A plan or a what-if alters real records; the sandbox lies | Low |
| T2 | person ↔ API | Elevation of privilege: the person who drafted a budget approves it, or a recorded budget is changed after approval | A budget nobody else agreed becomes the year's | Medium |
| T3 | API ↔ database | Tampering: a forecast, budget or study is written outside its rules (keys, limits, decimals, a revised budget revising nothing) | Figures the engine cannot read, or a budget the reports misread | Low |
| T4 | person ↔ API | Information disclosure: the cash projection shows every open item and each contact's payment behaviour to a role that should not see customers' and suppliers' dealings | Customers' and suppliers' business data seen beyond those who plan | Medium |
| T5 | job ↔ notification | Information disclosure: the buffer alert reaches members who may not read the plans, or carries amounts into email | The company's cash position leaks by email or to the wrong member | Low |
| T6 | job ↔ notification | Denial of service (alert fatigue) or repudiation: the alert repeats every day, or a crossing is never told | The alert is ignored, or a fall below the buffer goes unseen | Medium |
| T7 | person ↔ API | Denial of service: a plan or study whose exact figures grow without bound, an IRR that never settles, or a large XLSX import | The API's workers are tied up | Low |
| T8 | person ↔ API | Tampering: an imported spreadsheet carries formulas, a zip bomb or malware | The import runs code, exhausts memory, or keeps a malicious file | Low |
| T9 | person ↔ API | Elevation of privilege: the AI or an MCP client drafts, records or approves a plan | A plan the people did not make becomes the company's | Low |
| T10 | person ↔ API | Repudiation: nobody can tell who recorded a forecast or asked for a budget's approval | No evidence for the auditor | Low |

## 3. What we do about it

| Threat | Control | Test that proves it (`// req:`) |
|---|---|---|
| T1 | Runs, what-ifs and results save nothing. The what-if takes moves and leave-outs and returns a projection; recording a forecast changes only its own status; a study is a document of its own | `TestTheCashProjectionThroughTheAPI` (the ledger revision and the bill's planned date unchanged), `TestForecastsThroughTheAPI`, `TestFeasibilityThroughTheAPI` (FR-PLN-08) |
| T2 | The approval object `budget` is decided by another person who holds A, never the one who asked (FM-1, the approval engine's separation); approving records the version, and a recorded document version never changes | `TestBudgetsThroughTheAPI`, `TestApprovalPoliciesNameAppendixDRows` |
| T3 | The guards FY300 (forecasts), FY301 (budgets) and FY302 (studies) refuse a version outside its keys, limits and decimal strings, whoever writes it | `TestAttackForecastsKeepTheirRules`, `TestAttackBudgetsKeepTheirRules`, `TestAttackFeasibilityStudiesKeepTheirRules` |
| T4 | Every planning operation needs V on the row without a qualifier, so an Approver's department-qualified C does not grant them and a Bookkeeper is refused. The projection shows contacts' names and amounts only, never bank details | `TestAuthorisationMatrix`, `TestTheCashProjectionThroughTheAPI` (a Bookkeeper refused) |
| T5 | The alert's audience is C or A on the row; its title gives the date and the buffer's months, never an amount, and email carries the title only under the details setting (FR-NTF-02) | `TestTheCashBufferAlert` |
| T6 | Each notice is keyed to its crossing: a rerun adds nothing, a recovery is told once, and a later fall is a new crossing | `TestTheCashBufferAlert` |
| T7 | The engine's exact arithmetic fails rather than rounds past 4,000 digits (PLAN-09); horizons are 12 to 60 months, studies 1 to 10 years; the IRR's grid has 1,100 points and its bisection stops at 400 halvings; budgets hold at most 5,000 lines. Every operation is rate-limited as a read or a write (FR-INT-16) | `TestTheFeasibilityCalculator`, `TestTheIRR`, the forecast differential |
| T8 | The XLSX reader is the bank import's bounded reader (archive and entry limits, no formulas evaluated, exact decimals); the upload is scanned for malware and not kept | `TestReadXLSX`, `TestBudgetsThroughTheAPI` |
| T9 | No AI or MCP tool reads or writes plans; the never-list is unchanged; agent-class tokens are proposal-only (AI-34) | `TestAuthorisationMatrix` (the AI holds no C on the row), the MCP red-team suite |
| T10 | Every saved version, recording and approval request is audited with its actor in the same transaction (AUD-01) | `TestForecastsThroughTheAPI`, `TestBudgetsThroughTheAPI` |

## 4. What remains

- **The Board role reads the cash projection (T4).** It shows customers' and suppliers' names, open amounts and
  payment behaviour, as Appendix D's V on "Budgets, forecasts" allows. Revisit with the board pack: it could see the
  totals only.
- **The cash projection is not complete.** Without payroll, CPF, GST and tax, its lowest point can look better than it
  is. The page and the alert say which inputs are not included (design D7).
- **The workbook's simplifications** are kept [accountant to confirm]: the forecast's 12-month years, its CPF rate, its
  tax lag and its 60/40 collections; the study's single tax rate and first-year break-even.
- **No AI proposals yet.** The proposal of a budget version (`plan.propose_budget`) waits for the AI catalogue. Its own
  threat model comes with it.
