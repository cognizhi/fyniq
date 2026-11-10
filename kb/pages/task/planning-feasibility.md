---
key: planning.feasibility
type: task
title: Feasibility studies
summary: Whether an idea is worth it: its inputs as the workbook's sheet, its cash flows, NPV, IRR, paybacks, break-even and verdict
sources:
  - docs/user-guide/feasibility.md sha256:73f89a0f077627e0357a19f0cbd1a74e53deabadfb4445675a3f0c890ec775f7
data_class: general
taint: trusted
status: draft
human_edited: false
compiled_by: claude-opus-5-5 via Claude Code (fyniq-knowledge-base skill)
schema: kb-schema@1
compiled_at: 2026-11-08
aliases:
  - feasibility calculator
  - is it worth it
  - NPV
  - IRR
  - payback period
  - break-even
  - investment appraisal
---

# Feasibility studies

## What it is and how to start

- **Planning › Feasibility** (Expert mode) answers whether an idea, such as a new product line or a piece of equipment, is worth it: its cash flows year by year, its NPV, IRR and payback, its break-even and margin of safety, and a verdict. A study is kept and can be changed at any time; it never posts.
- **New study**, a name, then the figures: **Period and rate** (1 to 10 years, the discount rate, the longest payback accepted); **Investment** (spent at the start, the depreciation years, what it fetches at the end, no more than the investment, and the working capital as a share of each year's revenue); **Sales** (year-1 units and their yearly growth, the price and its yearly change); **Costs** (the variable cost per unit and its change, the fixed costs and their change). Rates, shares and changes are fractions, 0.12 for 12%; a change can be below zero, above −1.
- **Save** names every figure out of its limits at once, with how to fix it. To change a study, choose it under **Study**, edit and **Save** again.

(src: docs/user-guide/feasibility.md#what-it-is) (src: docs/user-guide/feasibility.md#start-a-study)

## Read the results

- **Feasible**: the NPV is above zero and the payback within the longest accepted; **Marginal**: the NPV is above zero but the payback is longer or never comes; **Not feasible**: the NPV is zero or below.
- **NPV**: each year's cash flow discounted at the rate, added up. **IRR**: the rate that makes the NPV zero, the one nearest 10% when there are several, none when no rate does. **Payback**: years until the cash flows add up to the investment, and **discounted** the same with the discounted flows. **Profitability index**: 1 + NPV ÷ investment.
- **Break-even**: year 1's fixed costs ÷ (price − variable cost), in units and revenue; **Margin of safety**: how far year 1's units are above break-even.
- **Cash flows** by year (revenue, costs, depreciation, EBIT, tax, the change in working capital, and at the end the salvage after tax on its gain over book value and the working capital released); **Sensitivity**: the NPV with sales volume and fixed costs each at 80% to 120%; **NPV at other rates**: 8% to 40%.

(src: docs/user-guide/feasibility.md#read-the-results)

## How it is worked out

- The tax rate is the corporate income tax rate in force on the study's date (17%), fixed for its years, so a year's loss saves tax; a changed study keeps its first date.
- Each amount is rounded to the cent once and totals add the rounded amounts; the workbook added unrounded figures, so its totals can differ by a few cents.
- The workbook's simplifications are kept [accountant to confirm]: one tax rate for every year, with no partial or start-up exemption; break-even and the margin of safety from the first year only. Related: [[planning.cash]].

(src: docs/user-guide/feasibility.md#how-it-is-worked-out)
