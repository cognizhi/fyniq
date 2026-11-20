---
key: planning.forecast
type: task
title: The driver forecast and its scenarios
summary: Model the company forward from its drivers in Base, Best and Worst, record the plan, and read runway with and without funding
sources:
  - docs/user-guide/forecast.md sha256:c51780e5782172b35db4ca85c8b31f20df086e46a03dbc6fa560af405c8648d7
data_class: general
taint: trusted
status: draft
human_edited: false
compiled_by: claude-opus-5-5 via Claude Code (fyniq-knowledge-base skill)
schema: kb-schema@2
compiled_at: 2026-11-11
aliases:
  - financial forecast
  - driver-based forecast
  - scenarios
  - runway forecast
  - funding gap
  - break-even month
  - record the forecast
  - hiring plan
  - effect of hiring on cash
---

# The driver forecast and its scenarios

## What it is and how to draft one

- **Planning › Forecast** (Expert mode) models the company forward from its drivers (customers and their revenue, services, direct costs, the team, other expenses, purchases and funding), starting from the books' figures at the last actual month: the cash, what customers owe and what the company owes, and the register's planned depreciation. It is the workbook's own model, worked out in exact figures, and never posts.
- **New forecast**, a name and its months (12 to 60); **Last actual month** left empty starts after the last closed month. The drivers: **Revenue** (customers, new customers and their growth, churn, revenue per customer and its yearly increase, services and their growth, other revenue, and when a month's revenue is collected, three shares adding to 1); **Direct costs and salaries** (hosting, AI and API, contractors and fees as shares of revenue, the yearly salary increase); **Other expenses** (a monthly base that grows plus a share of revenue; a report group shows the books' average beside it); **Team** (function, first and last month, salary, CPF); **Purchases** (cost, month, months of depreciation); **Funding** (equity, a SAFE, a loan with its rate and repayment months, or a grant); **Tax** (start-up exemption years, losses brought forward, months until tax is paid).
- **Save** names every problem at once under its field. Scenarios **Base**, **Best** and **Worst** come with the workbook's levers (multiples of new customers, churn, price, services, other expenses, direct costs and funding, and delays to hiring and funding), changed in the **Scenarios** table.

(src: docs/user-guide/forecast.md#what-it-is) (src: docs/user-guide/forecast.md#draft-a-forecast)

## Record it

- **Record** makes a draft the company's plan: Home's **Cash runs out** tile and the alerts read the latest recorded forecast ([[home.overview]]). A recorded forecast changes no more; to change the plan, start and record a new one. Recording needs no approval and posts nothing.

(src: docs/user-guide/forecast.md#record-it)

## Read and compare the results

- The results come first on the page. Once a forecast is recorded its plan folds below them: **Show the plan it was worked out from** opens it, read-only; a draft's plan stays open below its results, for editing.
- Per scenario, each figure first **without the funding planned** (none of it is received yet) and with it beside: **Runway** and **Cash runs out**; **Minimum cash** and its month; **Break-even** (the first month with revenue whose EBITDA is not negative); **Funding gap** (what would keep the buffer of months of burn above the minimum cash); **Revenue, next 12 months**, ARR and customers at the end; **Cash by month** with and without the funding; each forecast year's revenue, gross profit, EBITDA, net profit, net burn, cash, customers and headcount.
- **Compare the scenarios** sets every scenario's cash-out month, runway, minimum cash, funding gap and break-even side by side, with and without the funding.
- The workbook's simplifications are kept [accountant to confirm]: forecast years are 12-month blocks from the start, not financial years or years of assessment; everyone with CPF is at the employer rate for 55 and below up to the Ordinary Wage ceiling; each year's tax is paid a fixed number of months after it; what was owed at the start is collected and paid 60% in the first month and 40% in the second. Related: [[planning.cash]], [[planning.budgets]].

(src: docs/user-guide/forecast.md#read-the-results) (src: docs/user-guide/forecast.md#compare-the-scenarios) (src: docs/user-guide/forecast.md#related)

## The funding plan

- Give an **Equity** row of the funding plan an **Equity pre-money**, the valuation the round is planned at, and the page shows **What the planned equity rounds do to ownership** below the results.
- It starts from the equity register's cap table as it stands today, fully diluted, and takes each Equity row in the order of its month through the round model of [[equity.register]]: the pre-money valuation divided by the shares fully diluted before the round is the price per share, the money buys whole shares of it, and each holder's share before and after, and the points they lose, follow. A second round dilutes the holders as the first left them.
- A round with no pre-money valuation, or one the model refuses, is listed with why and passes the cap table through unchanged. SAFEs, loans and grants are listed apart, as they issue no shares in the plan.
- It uses the rows as the forecast holds them, before any scenario's levers, and plans no option pool top-up. Only people who may read the whole cap table see it, and it posts and saves nothing.

(src: docs/user-guide/forecast.md#the-funding-plan)
