---
key: reports.kpis
type: task
title: Read the start-up KPIs and enter the month's customer counts
summary: A month's KPIs beside the month before, how each is worked out, why one shows a dash, and entering the customer counts
sources:
  - docs/user-guide/kpis.md sha256:b90c4c7f334b6d0b0ca2c70281560d00d7df96c6ed82e5a76b0175308b5b1bbf
data_class: general
taint: trusted
status: draft
human_edited: false
compiled_by: claude-opus-5-5 via Claude Code (fyniq-knowledge-base skill)
schema: kb-schema@2
compiled_at: 2026-11-04
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

- **Booked MRR**: the subscription revenue the ledger recognised in the month, from revenue accounts whose report group names a subscription stream (not other income, and not a group heading).
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

## Counts from recurring invoices come first

- When the organisation has active recurring invoices, Fyniq counts the customers itself and uses that count ahead of an entry; the headcount stays as entered. A recurring invoice counts for a month while it is active now, has started by the month's last day and has not ended before it. A customer with two invoices counts once and a paused invoice counts for no month; a customer whose first invoice starts in a month is new in it, and one whose invoice ended before the month's end is lost in the month after.
- The KPIs that use the counts are marked Counted from recurring invoices instead of Entered. Subscription schedules and billing integrations will answer ahead of the recurring invoices as they arrive.

(src: docs/user-guide/kpis.md#counts-from-recurring-invoices-come-first)

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
