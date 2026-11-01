---
key: reports.kpis
title: Read the start-up KPIs
mode: basic
roles: [owner, administrator, controller, accountant, bookkeeper, auditor, tax_agent, dpo, board_viewer]
requirements: [FR-KPI-01, FR-KPI-02, FR-KPI-03]
status: draft            # written with the KPI page (P1); which revenue counts as subscription revenue waits for the accountant's confirmation
---

# Read the start-up KPIs

## What it is

**Reports → Start-up KPIs** shows a month's KPIs beside the month before: booked and contracted
MRR, ARR, revenue growth, the margins, spend by function, net burn, runway, ARPA, churn, CAC, LTV,
the burn multiple, the current and quick ratios, working capital, debt to equity, DSO, DPO, the
headcount and the revenue per employee. Basic mode shows the main ones; Expert mode shows every KPI
with how it is worked out, where it comes from and its definition's version.

Every figure is read from the ledger at one revision, which the page shows, so the same month read
again at that revision gives the same figures. A KPI the books cannot give shows **—** with the
reason, never 0: for example, churn says "no subscription data: enter the month's customer counts"
until the counts are entered.

## Booked and contracted MRR

**Booked MRR** is the subscription revenue the ledger recognised in the month: revenue accounts
whose report group names a subscription stream. **Contracted MRR** is the monthly value of the
subscriptions active at the month's end: what is signed. It needs subscription data, which arrives
in a later phase; until then it says so.

## Steps

### Choose the month

1. Open **Reports**, then **Start-up KPIs**.
2. Choose the month under **Month**. The page opens on this month, or the books' last month when
   today is after it.

### See how a KPI is worked out

Select **ⓘ** beside a headline KPI, or read **How it is worked out** in Expert mode. Each KPI
names its formula, the dates it reads (the month, the month end, or the last three months of the
financial year) and its source: the ledger, the counts a person entered, or both.

### Enter the month's customer counts and headcount

ARPA, churn, CAC, LTV and the revenue per employee need the number of paying, new and lost
customers and the headcount. Until subscriptions and payroll supply them, an Owner, Controller or
Accountant enters them each month:

1. Under **Customer counts and headcount**, select **Enter** (or **Change**).
2. Enter **Paying customers** at the month's end, **New customers** and **Customers lost** in the
   month, and the **Headcount**. Enter the three customer counts together, or none of them.
3. Select **Save**. The KPIs that use them are worked out again and marked **Entered**.

Each change is saved as a new version, with who entered it and when, in the audit trail.

## Common problems

- **"Enter it with the other two customer counts, or clear all three."** The paying, new and lost
  customers go together. Enter all three, or clear them and save the headcount alone.
- **"Enter a whole number of zero or more."** Counts are whole numbers.
- **The counts were not saved because they changed meanwhile.** Someone else saved the month's
  counts after you opened the page. Reload the page, then enter yours again.
- **"The books have no period for" the month.** Choose a month of an open financial year, or open
  the year in **Accounting → Periods and close**.

## Related

- [The Home page and finding your way around](home.md)
- [Read the financial statements](financial-statements.md)
