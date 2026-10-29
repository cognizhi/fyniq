---
key: reports.kpis
type: task
title: Read the start-up KPIs and enter the month's customer counts
summary: A month's KPIs beside the month before, how each is worked out, why one shows a dash, and entering the customer counts
sources:
  - docs/user-guide/kpis.md sha256:45f813938e895b95882dffc4bc33e2d96627cab45c7e1b4fdd875764d23ba131
data_class: general
taint: trusted
status: draft
human_edited: false
compiled_by: claude-opus-5-5 via Claude Code (fyniq-knowledge-base skill)
schema: kb-schema@1
compiled_at: 2026-10-29
aliases:
  - KPIs
  - start-up metrics
  - MRR
  - ARR
  - churn
  - CAC
  - LTV
  - burn multiple
  - runway
  - customer counts
  - headcount
  - why is churn blank
---

# Read the start-up KPIs and enter the month's customer counts

## What the page shows

- **Reports → Start-up KPIs** shows a month's KPIs beside the month before: booked and contracted MRR, ARR, revenue growth, the margins, spend by function, net burn, runway, ARPA, churn, CAC, LTV, the burn multiple, the current and quick ratios, working capital, debt to equity, DSO, DPO, the headcount and the revenue per employee.
- Basic mode shows the main ones; Expert mode shows every KPI with how it is worked out, its source and its definition's version.
- Every figure is read from the ledger at one revision, which the page shows: the same month read again at that revision gives the same figures.
- A KPI the books cannot give shows **—** with the reason, never 0; churn says "no subscription data: enter the month's customer counts" until the counts are entered.

(src: docs/user-guide/kpis.md#what-it-is)

## Booked and contracted MRR

- **Booked MRR**: the subscription revenue the ledger recognised in the month, from revenue accounts whose report group names a subscription stream.
- **Contracted MRR**: the monthly value of the subscriptions active at the month's end, what is signed. It needs subscription data, which arrives in a later phase; until then it says so.
- Which revenue counts as subscription revenue waits for the accountant's confirmation.

(src: docs/user-guide/kpis.md#booked-and-contracted-mrr)

## Choose the month and see how a KPI is worked out

- Open **Reports**, then **Start-up KPIs**, and choose the month under **Month**; the page opens on this month, or the books' last month when today is after it.
- **ⓘ** beside a headline KPI, or **How it is worked out** in Expert mode, names the formula, the dates it reads (the month, the month end, or the last three months of the financial year) and its source: the ledger, the counts a person entered, or both.

(src: docs/user-guide/kpis.md#choose-the-month) (src: docs/user-guide/kpis.md#see-how-a-kpi-is-worked-out)

## Enter the month's customer counts and headcount

- ARPA, churn, CAC, LTV and the revenue per employee need the paying, new and lost customers and the headcount; until subscriptions and payroll supply them, an Owner, Controller or Accountant enters them each month.
- Under **Customer counts and headcount**, select **Enter** (or **Change**); enter **Paying customers** at the month's end, **New customers** and **Customers lost** in the month, and the **Headcount**; the three customer counts go together, or none of them.
- **Save** works out the KPIs that use them again and marks them **Entered**. Each change is a new version, with who entered it and when, in the audit trail.

(src: docs/user-guide/kpis.md#enter-the-months-customer-counts-and-headcount)

## Common problems

- "Enter it with the other two customer counts, or clear all three.": enter the paying, new and lost customers together, or clear them and save the headcount alone.
- "Enter a whole number of zero or more.": counts are whole numbers.
- The counts changed meanwhile: someone else saved the month's counts after the page opened; reload, then enter yours again.
- "The books have no period for" the month: choose a month of an open financial year, or open the year in **Accounting → Periods and close** ([[accounting.periods]]).

(src: docs/user-guide/kpis.md#common-problems)

## Related

- The Home page's pulse card and tiles: [[home.overview]]
- The financial statements behind the figures: [[reports.statements]]

(src: docs/user-guide/kpis.md#related)
