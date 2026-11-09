# Design

## Context

See proposal.md for why. What exists:

- **Packs (`p1-statutory-packs`).**
  - The pack format and loader: `internal/domain/pack`, with `Schemas`, `RunPinned`, `At`, `RateOn`, `Code` and
    `Limit`.
  - The global tables `statutory_pack_version` and `statutory_pack_row`, which `migrate up` fills.
  - `sg-gst@2026.2`: rates, codes with their F5 boxes, and the `simplified_invoice` limit.
  - C.2 #16 reports a pack out of date.
  - No `sg-deadlines` pack exists.
- **GST status.**
  - `gst_registration` (FR-ORG-03) is effective-dated with its return frequency and quarters.
  - `gst.StatusOn` and `gst.ReturnPeriod` give the registration and the period on a day.
  - Every sales document keeps a supply code on each line, registered or not (SR, ZR, ES, OS). Only a registered
    organisation charges GST on it.
- **Sales in the ledger.** An invoice's or credit note's entry has one line per document line, in order, beside the
  controls. `management.documents` already relies on this (FR-RPT-03).
- **The organisation's years.** `period.CalendarOf` reads the books start and the first year's end. `Calendar.Year(n)`
  gives each financial year. `director.sixMonthsAfter` already applies the month-end rule for a conversion's lapse.
- **The registers' deadlines, with their checks:**
  - **Controllers** (`equity.StatutoryTestsIn`, C.2 #27): enter within 7 days; the central register within 2 business
    days; the annual notices.
  - **Conversions** (`director.Flags`, C.2 #22): lodge within 14 days of the resolution; the approval's lapse.
  - **Equity transactions** (`equity_step` ⏰).
- **Documents.**
  - Records that post nothing (`acra_extract`, `investor_right`, `controller_entry`) are documents: drafted with their
    evidence, recorded, append-only, and audited (`document.created`, `document.revised`, `document.posted`,
    `document.reversed`).
  - Their evidence commits to the retention-locked store when they are recorded (FR-DOC-13).
- **Notifications (FR-NTF-01).**
  - `notify.Emit` reaches an audience by capability and need, deduplicated by record key.
  - `reminders.BillsDue` is the daily pattern (`notify.bills_due`, 07:30 SGT).
  - The Inbox page composes sections: approvals, bills to pay and the weekly review.
- **Settings.** `settings.Catalogue` holds effective-dated organisation settings of fixed kinds: Switch, Days,
  Percent, Choice and others. Settings › Organisation renders them from the catalogue.
- **Access.** Appendix D has no calendar row. `Compliance Center` gives V to the Owner, Administrator, Controller,
  Accountant and Auditor, and C/A to the DPO. `GST returns` gives C to the Accountant and Tax agent, and A to the Owner
  and Controller.
- **Screens.** The compliance canvas has StatutoryCalendar, GstReturns, GstReturn and GstAdjustments, designed before the
  build.

Constraints:
- golden rules 1, 2, 7, 8 and 12 (exact decimals, the database as the backstop, every change audited, tenancy, official
  sources);
- ADR-T7 (background work through the outbox; the reminders are a platform schedule);
- ADR-T8: no new port. The pack store, documents, evidence, outbox and notification centre exist.

## Goals / Non-Goals

**Goals:**
- Every date the calendar shows comes from a pack row with its official source, never from a constant in code. The
  registers' own periods are the exception: they move to `sg-acra` in `p1-company-secretary`.
- The monitor's figures and the calendar's dates are each verified by a test worked by hand from the official examples.
- Nothing claims "all clear" while an input is missing: no financial year, a pack year without holidays, or an anchor
  before a rule was read. Each shows **Not assessed** and names the missing input (FR-CS-03).

**Non-Goals:**
- The F5 return and its boxes, the filing lock, the audit listings and corrections. These come at registration
  (proposal, out of scope).
- Filing anything with IRAS, CPF Board or ACRA. Fyniq records what a person filed (golden rule 5: no filing tool).
- A calendar stored as rows. Items are generated each time from the rules and facts, so a changed financial year or
  registration moves them at once (FR-TAX-07 "dates follow the FY and GST periods").

## Decisions

### D1. Two pack versions

- **`sg-gst@2026.3`** supersedes `2026.2` and adds one row to `limits`:
  - `registration_threshold`, 1000000.00 SGD;
  - from 2026-11-07, the day it was read;
  - source `iras-register`, IRAS's "Do I need to register for GST".

  As with `simplified_invoice`, its history is not recorded. A test before that day is Not assessed. The pinned cases
  cover the day before, the day itself and a later year.
- **`sg-deadlines@2026.1`** is a new pack. `pack.Schemas` gains it with three tables and three pinned tables:
  - **`rules`:** `obligation, area, kind, anchor, months, day, days, roll, condition, effective_from, effective_to,
    description, source`.
    - `kind` is `deadline`, or `starts` for the day a registration takes effect.
    - `area` is `tax`, `payroll` or `corporate`.
    - `anchor` is one of `gst_period_end`, `month_end`, `calendar_year_end`, `fy_end`, `fy_end_year_end` and
      `forecast_on`.
    - `roll` is `none` or `next_working_day`.
    - `condition` names a setting value that selects the row: `overseas_branch_register` or `agm_route=…`.
  - **`holidays`:** `date, name, observed, source`. Singapore's public holidays for 2026 and 2027 from MOM's page, with
    each Sunday holiday's observed Monday as its own row.
  - **`extensions`:** `obligation, due_from, due_to, due_on, source`. One row: IRAS's notice that an F5 due between
    2026-12-22 and 2027-01-03 is due on 2027-01-09.
  - **Pinned:**
    - `due_dates`: `case, obligation, anchor, condition, want, why, source`;
    - `working_days`: `case, date, want, why, source`;
    - `extensions`: `case, obligation, due, want, why, source`.

  The rows, with their sources:

  | obligation | kind | anchor | months / day / days | roll | source |
  |---|---|---|---|---|---|
  | `gst_f5` | deadline | gst_period_end | 1 | none | IRAS: "due 1 month after the end of your accounting period" (Apr–Jun → 31 Jul; 5 Apr–5 Jul → 5 Aug) |
  | `cpf` | deadline | month_end | 1, day 14 | next_working_day | CPF Board: by the 14th, "or the next working day if the 14th falls on a Saturday, Sunday or Public Holiday" |
  | `ir8a_ais` | deadline | calendar_year_end | 3, day 1 | none | IRAS AIS: "by 1 Mar each year" |
  | `eci` | deadline | fy_end | 3 | none | IRAS ECI: "within 3 months from the end of your financial year" (FYE 31 Mar 2025 → 30 Jun 2025) |
  | `form_cs` | deadline | fy_end_year_end | 11, day 30 | none | IRAS: "30 Nov each year"; preceding-year basis |
  | `annual_return` | deadline | fy_end | 7 (8 with `overseas_branch_register`) | none | ACRA: "Within seven months after FYE"; "eight months" with an overseas branch register |
  | `agm` | deadline | fy_end | 6, with `agm_route=hold` | none | ACRA: "Within six months after FYE" |
  | `statements_to_members` | deadline | fy_end | 5, with `agm_route=send_statements` | none | ACRA: no AGM if the statements are sent "within five months after the FYE" |
  | `gst_register_retrospective` | deadline | calendar_year_end | 1, day 30 | none | IRAS: apply "between 1 Jan to 30 Jan of the following year" |
  | `gst_starts_retrospective` | starts | calendar_year_end | 3, day 1 | none | IRAS: "registered for GST on 1 Mar of the following year" |
  | `gst_register_prospective` | deadline | forecast_on | days 30 | none | IRAS: "within 30 days after the date of your forecast" |
  | `gst_starts_prospective` | starts | forecast_on | 2 | none | IRAS: "registered 2 months from the date of your forecast", for a liability from 1 Jul 2025 |

  Each row's `effective_from` is the day its page was read (2026-11-07). A rule applies when the date it gives falls
  within its effective dates: an obligation falling due from the day the rules were read is assessed, whatever its
  anchor, and one due earlier is Not assessed, since the pack does not record the rule then. (Built: selecting by the
  anchor day left October 2026's CPF, due 16 Nov, unassessed.) IRAS dates the two months of `gst_starts_prospective`
  from 1 Jul 2025; every forecast whose start falls from 7 Nov 2026 is later than that, so the row needs no earlier
  date.

  The `agm`, `statements_to_members` and `annual_return` rows are ACRA's corporate timing, which FR-CS-02 puts in
  `sg-acra`. They sit here until `p1-company-secretary` writes that pack, and then move to it in one release.

  *Alternative:* code the deadlines as constants, as the registers' are today. Rejected: §10.6 and golden rule 12 put
  statutory values in packs, so a changed deadline is a pack release and not a platform release (ENG-33).

### D2. The due-date rule

`pack.DueOn(v, obligation, anchor, facts)` takes the first rule row whose condition the facts meet and whose effective
dates hold the date it gives, worked out as follows:
1. Move the anchor `months` months forward. For `fy_end_year_end`, the anchor passed is the financial year's end, and
   the rule counts from 31 December of its year. When the anchor is its month's last day, the result is the target month's
   last day (IRAS: 30 Jun → 31 Jul). Otherwise it keeps the anchor's day, or the month's last day when that day does not
   exist.
2. If the row sets `day`, take that day of the resulting month.
3. Add `days`.
4. Apply an `extensions` row whose window holds the date.
5. With `next_working_day`, move past Saturdays, Sundays and the `holidays` rows. A year with no holiday rows is Not
   assessed, since the pack does not know it. C.2 #16 already reports a pack past its review date.

The result is the date, the rule's row and the steps applied, so the screen can say "31 Jul 2027: one month after the
period end (IRAS)". The pinned cases are written from the sources' own examples, independently of the rows:
- the four GST quarters of 2027, a special period, and the December 2026 extension;
- CPF for October 2026 (Saturday 14 Nov → Monday 16 Nov) and January 2027 (Sunday 14 Feb → 15 Feb);
- the working day after Deepavali 2026 (Sunday 8 Nov, observed Monday 9 Nov → Tuesday 10 Nov);
- ECI and the annual return for a 31 March and a 31 December year end;
- Form C-S for both;
- the four GST registration dates.

*Alternative:* `AddDate(0, n, 0)`. Rejected: Go normalises 31 Apr to 1 May and keeps the 30th of a 30 June anchor, both
against IRAS's examples.

### D3. Taxable supplies

`gst.TaxableSupplies(ctx, u, base, from, to)` adds up, at the ledger revision read, the entries posted from `from` to
`to` of:
- invoices and credit notes, the same way `management.documents` reads them;
- bank transactions.

It counts the lines on accounts of type revenue whose document line's code is a supply reported in Box 1 or Box 2 of
the version in force (standard-rated or zero-rated: IRAS's taxable supplies), at their base amounts. A credit's sign is
turned, and a reversal counts on its own day.

Left out:
- exempt (Box 3) and out-of-scope codes;
- lines on any other account type, so a sale of a capital asset (FR-FA-03 credits the disposal clearing account) never
  counts;
- deposits, which count when the invoice that uses them posts.

It returns the total and the documents behind it, for the drill-down. An entry whose lines do not match its document's
counts in a line of its own, "Not matched to a line", so the total still agrees with the ledger.

*Assumption:* the posting date stands for the time of supply, since nothing is registered yet
[accountant to confirm].

*Alternative:* the revenue accounts' balances. Rejected: they hold exempt interest and out-of-scope income, which IRAS
leaves out.

### D4. The monitor

`gst.Monitor(ctx, u, today)` returns, for an organisation not registered on `today`:
- **Last calendar year.** Its taxable supplies, the threshold in force on 31 December, and whether it was exceeded:
  "more than $1 million". When it was, the dates to apply by and to be registered from, by D2.
- **This year so far.** Its taxable supplies, the threshold, `gst.threshold_warning_percent` (80) and the share
  reached. Each percentage is computed exactly and rounded only for display.
- **The forecast in force.** The latest recorded `gst_forecast` whose 12 months hold `today`. Once its value exceeds
  the threshold: the date to apply by and the date registration starts, by D2.
- **A status:**
  - `liable`: a test is met;
  - `near`: the share reached is at or above the percentage;
  - `below`;
  - `registered`: the organisation is registered on `today`, and the monitor says so and stops;
  - `not_assessed`: no threshold is in force, or the books hold no year.

A **`gst_forecast`** is a document drafted with:
- `forecast_on`: the day the forecast was made;
- `value`: SGD with cents, the taxable supplies expected in the 12 months from that day;
- `basis`: up to 1,000 characters, such as "Signed contracts with A and B";
- at least one evidence file.

It is recorded by whoever holds C on GST returns, drafted and recorded as the equity records are, and a later one supersedes it.
The guard trigger (FY298) keeps its keys, the date, and a positive amount at the base currency's scale. An empty
evidence list is refused when it is recorded (GST-31), because IRAS asks for the "supporting documents". A forecast of a
business plan is not refused: the guide says what IRAS counts as certain.

### D5. The calendar

`deadlines.Calendar(ctx, u, from, to, today)` generates the items due from `from` to `to` (at most 18 months; the API
refuses more). It also returns every earlier item still open.

Each item has:
- `key`: the obligation and its anchor, such as `gst_f5:2027-03-31`;
- `obligation`, `area`, `title`, `anchor` and `due_on` with its explanation from D2;
- `status`: `open`, `due_soon` (within 14 days), `overdue`, `filed`, `not_required` or `not_assessed`;
- the filing record, if any;
- `source` (`rule`, or `register` for D7's items) and a link.

The facts, read in the same unit of work:
- **Financial years**, from `period.CalendarOf`: `eci`, `form_cs`, `annual_return`, and `agm` or
  `statements_to_members`. Without them, one item, Not assessed: "Set the books start and the first year's end".
- **GST registrations**, by `gst.ReturnPeriod`: one `gst_f5` per return period that ends in the window. The monitor's
  dates while liable (D4).
- **Pays staff** (`calendar.pays_staff`, effective-dated): one `cpf` per month while it is on, and `ir8a_ais` for each
  calendar year with a month on.
- **AGM route** (`corporate.agm_route`): `hold` (the default), `send_statements` or `dispensed`. Dispensed has no item.
  The guide says a member may still ask for an AGM (ACRA).
- **Overseas branch register** (`corporate.overseas_branch_register`, off): the eight-month annual return.

### D6. Recording a filing

(Revised as built, PRD-001 rev 0.211: FR-DOC-06's table requires the submission or payment acknowledgement for GST F5,
Form C-S/C, ECI and AIS filings and CPF payments, so a filed record is drafted, given its acknowledgement and recorded,
as the ACRA extract is; the setting `evidence.statutory_filing` is required by default, and under a recommended rule
C.2 #30 lists a filed record without its file. A withdrawal is a record of its own beside the reversed one, with its
reason, and the live-record rule is a lock on the item's key in the posting trigger, not an index.)

A **`statutory_filing`** document records an item's outcome:
- `key`;
- `outcome`: `filed` or `not_required`;
- for `filed`: `filed_on` and `reference`, such as the IRAS acknowledgement number;
- for `not_required`: `reason`, up to 500 characters, such as "Revenue under S$5 million and ECI nil: waived (IRAS)";
- an optional `note`.

The rules:
- It is drafted and recorded in one call, `recordStatutoryFiling`.
- Its evidence is attached to the record afterwards.
- C.2 #30's evidence rule (`evidence.statutory_filing`, "recommended") flags a filed item without its acknowledgement.
- A record cannot be edited. `withdrawStatutoryFiling` reverses it with a reason, and the item is open again.
- The guard trigger (FY299) keeps the keys, the key's form, the outcome's fields and one live record per key. The
  latter is a partial unique index on the posted rows' key, so a second record of the same item is refused (GST-32).

The F5 items are recorded this way until FR-TAX-03's filing record exists. That change then completes them from its own
record, and this path refuses an F5 key (noted in its tasks).

*Alternative:* a draft step with evidence before recording, as the ACRA extract has. Rejected: the acknowledgement often
arrives after the filing. A recorded filing without its file is visible on C.2 #30, and there is nothing to approve.

### D7. The registers' deadlines

The calendar lists, read-only, the items the registers already compute:
- controller entries to enter or to lodge, and annual notices due (`equity.StatutoryTestsIn`);
- conversions' approvals to lodge and lapsing (`director.Flags`);
- equity steps due.

Each links to its record. It is done there and checked there (C.2 #22, C.2 #27). The calendar never records them, and
C.2 #15 leaves them out, so nothing is reported twice.

### D8. Access and the API

Appendix D's new row **Statutory calendar** (PRD rev 0.208) is added to the PRD, the `access/roles-and-segregation`
spec and `authz.Grants` in the same commit:
- **C:** Owner, Controller, Accountant, Tax agent;
- **V:** Admin, Auditor, DPO;
- all others —.

The authorisation matrix test grows by its operations:

| Operation | Capability / need |
|---|---|
| `getStatutoryCalendar` `GET /v1/compliance/calendar?from&to` | Statutory calendar V |
| `createStatutoryFiling` `POST /v1/compliance/filings`, `replaceStatutoryFiling`, `recordStatutoryFiling` `POST …/{filing_id}/record` | Statutory calendar C (If-Match) |
| `withdrawStatutoryFiling` `POST /v1/compliance/filings/{filing_id}/withdraw` | Statutory calendar C (If-Match) |
| `getStatutoryFiling`, `listStatutoryFilings` (by item key) | Statutory calendar V |
| the filing's evidence (add, list, get, remove) | Statutory calendar C / V |
| `getGSTMonitor` `GET /v1/compliance/gst-monitor` | GST returns V |
| `createGSTForecast`, `replaceGSTForecast`, `recordGSTForecast`, `getGSTForecast`, `listGSTForecasts` | GST returns C (record) / V |
| the forecast's evidence | GST returns C / V |

- The calendar's array is `deadlines`, not `items`, and has no cursor: the window bounds it, so Spectral's list rule does
  not apply, as with `getAcraReconciliation`.
- Writes take `Idempotency-Key` and `Prefer: return=preview`.
- Errors use the new codes GST-30 to GST-33 and CAL-01 to CAL-04 (RFC 9457).
- The AI's column is — for the calendar's writes. For the monitor it reads "Ask only", as GST returns does. No AI or MCP
  tool is added (§9.6).

### D9. Checks

- **C.1 #37 `gst.registration-threshold`** (Tax, ⚠, owner accountant):
  - the monitor's `liable` or `near`, with the figures and the dates;
  - OK `below`, or "Registered for GST from …";
  - Not assessed with the monitor's reason.

  It triggers on documents and on the setting.
- **C.2 #13 `gst.return-due`** (Tax, ⏰):
  - each return period that has ended and has no `filed` record, due within 7 days or past;
  - OK "Not registered for GST: no return is due" before registration.
- **C.2 #15 `compliance.deadlines`** (Compliance, ⏰): each of the calendar's rule items other than F5 that is open and
  due within 14 days or past, overdue first. Not assessed items are listed first and named.

All three run nightly and on request. They read through the same functions the screens do, so a check never disagrees
with the calendar.

### D10. Reminders and the Inbox

- **The daily job `notify.deadlines`.** It runs at 07:35 SGT, after `notify.bills_due` (PlatformSchedules, ADR-T7).
  For each item open with 30, 14, 7 or 1 days left, it emits `deadline.reminder`:
  - **To:** Statutory calendar C, or GST returns C for the monitor's dates;
  - **Keyed by:** the item and the days, so a rerun adds nothing;
  - **Title:** such as "ECI due in 14 days: 31 Mar 2027";
  - **Link:** `/compliance/calendar#<key>`.

  An overdue item gets `deadline.overdue` once, on its first day past.
- **The Inbox's Deadlines section.** It shows the items overdue or due within 30 days to members holding C on the row,
  from `getStatutoryCalendar` (today − 1 year to today + 30 days, open ones). They are "tasks" in FR-TAX-07's sense: the
  Inbox's existing sections are views, not stored tasks, and this one is too.

### D11. Settings

| Key | Kind | Recommended | Why |
|---|---|---|---|
| `gst.threshold_warning_percent` | Percent, 50–99 | 80 | The owner's decision; months of notice before the S$1m test is met [accountant to confirm] |
| `calendar.pays_staff` | Switch | false | The owner's decision; turned on from the first month with staff, effective-dated |
| `corporate.agm_route` | Choice: hold, send_statements, dispensed | hold | The Companies Act's default for a private company. The others need what ACRA names: statements sent within five months, or a resolution of all members |
| `corporate.overseas_branch_register` | Switch | false | ACRA's eight months apply only with an overseas branch register |
| `evidence.statutory_filing` | Choice | required | FR-DOC-06's table: the submission or payment acknowledgement (as built; first planned as recommended) |

### D12. Screens

- **StatutoryCalendar** (`/compliance/calendar`, Basic and Expert):
  - **Two views:** a month list with the ⏰ chip, area filter and status; and the list grouped by month.
  - **On each item:** its due date and why, then "Record as filed" and "Not required" (C only), and the evidence.
  - Not assessed items show their missing input first.
  - Basic shows everything except the rule's steps and the key.
- **GstReturns** (`/compliance/gst`, Expert, per the inventory):
  - the monitor card: three figures, the bar to the threshold, the status and the dates;
  - the forecasts with "Record a forecast";
  - while not registered, the note that overseas vendors' GST is part of the cost (FR-TAX-01);
  - the periods table appears with registration.
- **The Inbox:** a Deadlines card.
- **Help:** keys for each column, chip and action, and two guide pages compiled into `kb/`.
- **The canvas:** the compliance canvas is updated as built.

## Risks / Trade-offs

- **[A wrong rule row silently moves every deadline.]** Pinned cases from the official examples, independent of the
  rows (D2). Activation by two people (SOD-10). C.2 #16 at the review date.
- **[Rules read on 2026-11-07 leave earlier anchors Not assessed.]** That is honest: the pack does not record the law's
  history. Earlier filings can still be recorded as filed against the item, which shows its rule as "not in the pack".
- **[Holidays for 2028 are missing in late 2027.]** CPF items in 2028 show Not assessed, and C.2 #15 lists them first.
  The January routine adds the year (`fyniq-sg-statutory`).
- **[Taxable supplies by posting date is not the time of supply.]** For a company far below S$1m, the difference is a
  timing of days. The choice is tagged [accountant to confirm] and revisited at registration, when F5 needs the real
  time of supply.
- **[Two places for register deadlines (code and pack).]** Listed in the proposal. `p1-company-secretary`'s `sg-acra`
  moves them.

## Migration Plan

- Migrations:
  - add `gst_forecast` and `statutory_filing` to `document_type_known`, NOT VALID then validated;
  - FY298 and FY299 guards;
  - the partial unique index on a live filing's key, built concurrently in its own migration.
- The packs load with `migrate up`. Organisations see the "parameter update available" task (ENG-33). Until
  `sg-deadlines@2026.1` is active in an organisation, its calendar is Not assessed with that reason, never empty.
- Rollback: the migrations only widen. The settings fall back to their recommended values. Turning the job off stops
  reminders.

## Open Questions

None that change the build. The accountant confirms the 80% and the posting-date reading of taxable supplies at the P1
rule review (FR-UX-41 banner on the GST page meanwhile).
