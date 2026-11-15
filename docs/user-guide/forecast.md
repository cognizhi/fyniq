---
key: planning.forecast
title: The driver forecast and its scenarios
mode: expert
roles: [owner, controller, accountant, approver, auditor, tax_agent, board_viewer]
requirements: [FR-PLN-02, FR-PLN-03, FR-PLN-07, FR-PLN-08, FR-KPI-01]
status: draft
---

# The driver forecast and its scenarios

## What it is

**Planning › Forecast** (Expert mode) models the company forward from its drivers: customers and their revenue,
services, direct costs, the team, other expenses, purchases and funding. It starts from the books' figures at the last
actual month: the cash, what customers owe and what the company owes, and the register's planned depreciation. It is
the workbook's own forecast model, worked out in exact figures. A forecast never posts: nothing on the page changes the
books.

## Draft a forecast

1. Select **New forecast**, name it and choose its months (12 to 60).
2. Leave **Last actual month** empty to start after the last closed month, or name a month.
3. Enter the drivers:
   - **Revenue**: customers at the start, new customers in month 1 and their monthly growth, churn, revenue per
     customer and its yearly increase, services and their growth, other revenue, and when a month's revenue is
     collected (three shares that add to 1);
   - **Direct costs and salaries**: hosting, AI and API, contractors and payment fees as shares of revenue, and each
     year's salary increase;
   - **Other expenses**: each a monthly base that grows, plus a share of revenue. Naming a report group of the chart
     shows the books' average beside it;
   - **Team**: each person or role, its function, first and last month, monthly salary, and whether CPF applies;
   - **Purchases**: each item's cost, month and months of depreciation;
   - **Funding**: equity, a SAFE, a loan (with its rate and months of repayment) or a grant, and its month;
   - **Tax**: the years under the start-up exemption, the losses brought forward, and the months until tax is paid.
4. Select **Save**. Every problem is named at once, under its field. A draft can be changed and saved again.

A new forecast gets three scenarios, **Base**, **Best** and **Worst**, with the workbook's levers: multiples of new
customers, churn, price, services, other expenses, direct costs and funding, and delays to hiring and funding. Change a
scenario's levers in the **Scenarios** table.

## Record it

**Record** makes a draft the company's plan: Home's **Cash runs out** tile and the alerts read the latest recorded
forecast. A recorded forecast changes no more; to change the plan, start a new forecast and record that. Recording
needs no approval and posts nothing.

## Read the results

The results come first on the page. Once a forecast is recorded its plan folds below them: **Show the plan it was
worked out from** opens it, read-only. A draft's plan stays open below its results, for editing.

Choose a scenario above its results. None of the funding planned is received yet, so the page shows each figure
**without the funding planned** first, and with it beside:

- **Runway** and **Cash runs out**: the months until cash first falls below zero, and that month;
- **Minimum cash**, its month, and **Break-even**: the first month with revenue whose EBITDA is not negative;
- **Funding gap**: what would keep the buffer of months of burn above the minimum cash;
- **Revenue, next 12 months**, the ARR and customers at the end;
- **Cash by month**, with and without the funding, with **Table**, **PNG** and **CSV**;
- each forecast year's revenue, gross profit, EBITDA, net profit, net burn, cash, customers and headcount.

These are the workbook's simplifications, kept as it had them [accountant to confirm]:

- forecast years are 12-month blocks from the forecast's start, not the company's financial years or years of
  assessment;
- everyone with CPF is at the employer rate for 55 and below, up to the Ordinary Wage ceiling;
- each year's tax is paid a fixed number of months after the year;
- what customers owed and what the company owed at the start are collected and paid 60% in the first month and 40% in
  the second.

## Compare the scenarios

**Compare the scenarios** sets every scenario's cash-out month, runway, minimum cash, funding gap and break-even side by
side, with and without the funding planned.

## Related

- [Cash ahead](cash-flow.md)
- [Budgets and Budget vs Actual](budgets.md)
- [The Home page](home.md#read-the-plans-on-home)
