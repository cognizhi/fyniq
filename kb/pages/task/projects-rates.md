---
key: projects.rates
type: task
title: Set cost rates and billing rates, and see what time costs
summary: Who enters a person's cost rate, how billing rates work, and what a viewer sees of time at cost
sources:
  - docs/user-guide/projects-rates.md sha256:c03c409eba7cf08a3ede59964478d2f2395b88d622d17722b087b4b59202c6b9
data_class: general
taint: trusted
status: draft
human_edited: false
compiled_by: claude-sonnet-5-5 via Claude Code (fyniq-knowledge-base skill)
schema: kb-schema@1
compiled_at: 2026-11-19
aliases:
  - cost rate
  - billing rate
  - hourly rate
  - time at cost
  - standard rate
---

# Set cost rates and billing rates, and see what time costs

## What the rates are

- A cost rate is what an hour of a person's time costs the company; a billing rate is what an hour is sold for. Both start from a day and
  never change: a later day's rate replaces the earlier one from then. Time at cost posts nothing to the books.
  (src: docs/user-guide/projects-rates.md#what-it-is)

## Enter them

- Only the Owner and the Payroll administrator enter and see a person's cost rate, because it is payroll data: **Projects, Cost rates**
  › **Enter a cost rate**. A standard rate with no person is what others see when a role has fewer than three people.
  (src: docs/user-guide/projects-rates.md#enter-cost-rates)
- On a billed project, **Enter a billing rate** for the project, a role or a person, in the project's currency.
  (src: docs/user-guide/projects-rates.md#enter-billing-rates)

## Who sees what time costs

- The Owner and the Payroll administrator see each person's own rate and cost; the Controller, the Accountant, the Auditor, the Tax agent
  and a project's manager see the average rate of a role with at least three people, or the standard rate, and no person's cost; the AR
  specialist sees hours only. (src: docs/user-guide/projects-rates.md#what-time-costs-and-who-sees-it)

Related: [[projects.time]], [[projects.projects]], [[projects.reports]].
