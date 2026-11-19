---
key: projects.reports
type: task
title: See what projects earn and cost
summary: What the project report shows, where each figure comes from, who sees which cost, and the checks that watch projects
sources:
  - docs/user-guide/projects-reports.md sha256:b6a8bf6bf3cdb40ef15b6b48074d06c9a7ff3d62d8422809801bf31efccdb2d3
data_class: general
taint: trusted
status: draft
human_edited: false
compiled_by: claude-sonnet-5-5 via Claude Code (fyniq-knowledge-base skill)
schema: kb-schema@2
compiled_at: 2026-11-19
aliases:
  - project profitability
  - utilisation
  - realisation
  - project margin
---

# See what projects earn and cost

## What the report shows

- **Reports, Projects** shows revenue and direct costs from the posted ledger and the cost of staff time from approved time, per project
  and for the months chosen. Time posts nothing. (src: docs/user-guide/projects-reports.md#what-it-is)
- Direct costs leave out salary accounts. Salary tagged to a project is listed apart to reconcile and never counted again, so staff cost
  appears once, as time at cost. Margin is revenue less time at cost less direct costs; realisation is billed over billable hours at billing
  rates; budget used is approved hours over the hours budget. (src: docs/user-guide/projects-reports.md#what-it-is)
- The row "Tagged with no project" makes every project plus that row equal the ledger's revenue and expense totals; only people who see
  every project see it. (src: docs/user-guide/projects-reports.md#what-it-is)

## Utilisation

- Billable hours over available hours: the weekdays less public holidays at 8 hours a day (40 a week); working patterns and leave join later.
  (src: docs/user-guide/projects-reports.md#utilisation)

## Who sees what

- The Owner sees each person's own rate and cost; the Controller, the Accountant, the Auditor and a project's manager see a role average
  (at least three people) or the standard rate, never one person's cost; the AR specialist sees revenue and hours only; a manager sees only
  their own projects. (src: docs/user-guide/projects-reports.md#who-sees-what)

## Checks

- Three checks watch projects: over the hours budget or losing money, a week submitted or returned more than 7 days ago, and the last four
  closed weeks with no approved time. (src: docs/user-guide/projects-reports.md#checks)

Related: [[projects.rates]], [[projects.time]], [[projects.projects]].
