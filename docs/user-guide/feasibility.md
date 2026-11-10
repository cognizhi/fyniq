---
key: planning.feasibility
title: Feasibility studies
mode: expert
roles: [owner, controller, accountant, approver, auditor, tax_agent, board_viewer]
requirements: [FR-PLN-06, FR-PLN-08]
status: draft
---

# Feasibility studies

## What it is

**Planning › Feasibility** (Expert mode) answers whether an idea, such as a new product line or a piece of equipment,
is worth it. You enter its figures as the workbook's feasibility sheet has them, and Fyniq works out its cash flows
year by year, its NPV, IRR and payback, its break-even and its margin of safety, and gives a verdict. A study is kept,
and you can change it at any time. It never posts: nothing on the page changes the books.

## Start a study

1. Select **New study**, and name it.
2. Enter its figures:
   - **Period and rate**: the years to evaluate (1 to 10), the discount rate, and the longest payback you accept;
   - **Investment**: what is spent at the start, the years it is depreciated over, what it fetches at the end (no more
     than the investment), and the working capital it ties up, as a share of each year's revenue;
   - **Sales**: the units sold in the first year and their yearly growth, and the price and its yearly change;
   - **Costs**: the variable cost per unit and its yearly change, and the fixed costs and their yearly change.

   Rates, shares and changes are fractions: 0.12 for 12%. A change can be below zero, above −1.
3. Select **Save**. Every figure that is out of its limits is named at once, with how to fix it.

To change a study, choose it under **Study**, edit its figures and select **Save** again.

## Read the results

- **The verdict**:
  - **Feasible** when the NPV is above zero and the payback is within the longest you accept;
  - **Marginal** when the NPV is above zero but the payback is longer, or never comes;
  - **Not feasible** when the NPV is zero or below.
- **NPV**: each year's cash flow discounted at the rate, added up. **IRR**: the rate that makes the NPV zero, the one
  nearest 10% when there are several; none when no rate does.
- **Payback**: the years until the cash flows add up to the investment, and **discounted** the same with the
  discounted flows.
- **Profitability index**: 1 + NPV ÷ investment.
- **Break-even**: the first year's fixed costs ÷ (price − variable cost), in units and revenue. **Margin of safety**:
  how far the first year's units are above break-even.
- **Cash flows**: each year's revenue and costs, depreciation, EBIT, tax, the change in working capital, and at the end
  the salvage (after tax on its gain over its book value) and the working capital released.
- **Sensitivity**: the NPV with sales volume and fixed costs each at 80% to 120% of yours. **NPV at other rates**: at
  8% to 40%.

## How it is worked out

- The tax rate is the corporate income tax rate in force on the study's date (17%), fixed for its years, so a year's
  loss saves tax. The study keeps its first date when you change it.
- Each amount is rounded to the cent once, and the totals add the rounded amounts, so what is shown adds up. The
  workbook added unrounded figures, so its totals can differ by a few cents.
- These are the workbook's simplifications, kept as it had them [accountant to confirm]: one tax rate for every year,
  with no partial or start-up exemption; break-even and the margin of safety from the first year only.

## Related

- [Cash ahead](cash-flow.md)
