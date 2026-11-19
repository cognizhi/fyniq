---
key: projects.rates
title: Set cost rates and billing rates, and see what time costs
mode: expert
roles: [owner, payroll_administrator, accountant, controller, approver, auditor]
requirements: [FR-PRJ-06, FR-PAY-13]
status: draft            # provisional until the owner signs off (PRD-001 rev 0.295)
---

# Set cost rates and billing rates, and see what time costs

## What it is

Time on a project is valued two ways. A **cost rate** is what an hour of a person's time costs the company. A **billing rate** is what an
hour is sold for. Both start from a day and never change: a new rate from a later day replaces the earlier one from that day. Time
at cost is a management view: it posts nothing to the books, because salaries are already in the ledger through payroll.

## Enter cost rates

Only the **Owner** and the **Payroll administrator** enter and see a person's cost rate, because it is payroll data. Open **Projects, Cost
rates**, select **Enter a cost rate**, choose the person, the role their rate is grouped under (such as Consultant), the day it starts and
the cost of an hour. Until payroll supplies the figure you type it; a payroll-derived rate replaces it later.

Enter a **standard rate** too, with no person: it is what people who may not see a person's rate are shown when a role has fewer than three
people.

## Enter billing rates

On a project that is billed, select **Enter a billing rate** and give the price of an hour in the project's currency, for the project, for
a role or for a person, and the day it starts. The rate in force on the day of the work is used.

## What time costs, and who sees it

A project shows its approved time this month and what it costs, worked out by Fyniq for you and never by your screen:

- The Owner and the Payroll administrator see each person's own rate and cost.
- The Controller, the Accountant, the Auditor, the Tax agent and a project's manager see the cost at the average rate of a role with at least
  three people, or else the standard rate, and hours for each person but no person's cost.
- The AR specialist sees hours only.

The page says which rate it used. With too few people in a role and no standard rate, it shows hours only.
