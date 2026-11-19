---
key: projects.reports
title: See what projects earn and cost
mode: expert
roles: [owner, controller, accountant, approver, auditor, ar_specialist]
requirements: [FR-PRJ-12, FR-PRJ-09, FR-PRJ-05, FR-PRJ-07]
status: draft            # provisional until the owner signs off (PRD-001 rev 0.296)
---

# See what projects earn and cost

## What it is

**Reports, Projects** shows each project's profit for the months you choose, from two sources: the **posted ledger** (revenue and direct
costs) and the **approved time** (what staff time costs). Time posts nothing, because salaries are already in the ledger through payroll;
the report only values it.

For each project it shows:

- **Revenue**: what the books hold on revenue accounts tagged to the project.
- **Direct costs**: costs tagged to the project, **without** salary accounts. Salary tagged to a project is shown beside it, listed apart
  to reconcile, and is never counted again, so staff cost appears once, as time at cost.
- **Time at cost**: approved hours at the rate your role may use. The report says which: each person's own rate, the average rate of a role,
  the standard rate or hours only.
- **Margin**: revenue less time at cost less direct costs.
- **Realisation**: what was billed (the revenue) as a percentage of the project's billable hours at billing rates.
- **Budget used**: approved hours as a percentage of the hours budget.

A last row, **Tagged with no project**, holds the revenue and costs with no project, so every project plus that row equals the ledger's
revenue and expense totals. Only people who see every project see it.

## Utilisation

For each person, **billable hours over available hours** for the months you chose. Available hours are the weekdays less public holidays at 8
hours a day, which is 40 hours a week. A person's working pattern and leave join the calculation when people records arrive.

## Who sees what

The Owner sees each person's own rate and cost. The Controller, the Accountant, the Auditor and a project's manager see the cost at a role
average (a role of at least three people) or the standard rate, and never one person's cost. The AR specialist sees revenue and hours
only. A project's manager sees only the projects they manage.

## Look behind a figure

Open a project, then its time, to see the approved entries behind its hours. The entries show hours only, never a rate.

## Checks

Three checks watch projects: **No project is over its hours budget or loses money**, **No week of time waits too long** (a week submitted or
returned more than 7 days ago), and **Every closed week has approved time** (the last four weeks).
