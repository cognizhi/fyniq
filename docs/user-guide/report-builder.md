---
key: reports.builder
title: Build, save and schedule your own report
mode: expert
roles: [owner, administrator, controller, accountant, bookkeeper, auditor, tax_agent, dpo]
requirements: [FR-RPT-04, FR-RPT-07, FR-RPT-08]
status: draft            # written with the report builder (P2); the rounding of calculated rows waits for the accountant's confirmation
---

# Build, save and schedule your own report

## Find it

In Expert mode, open **Reports** and choose **Report builder**. The page lists the reports you can open:
your own and the ones shared with everyone who may read the whole ledger's reports.

A role that reads only part of the books, such as sales, purchases, payroll, a department or the board
pack, does not see the builder, because a report can name any account.

## Build a report

A report is **rows** and **columns**.

- A row is an **account** (by its code), a **report group** (every account the chart puts in that group),
  a **calculation** or a **heading**.
- A column is **the run** you choose, **the period before** it, **the same months a year earlier**, or **the
  year to date**. A profit and loss account shows its movement over the column's months; a balance sheet
  account shows its balance at the column's last day. A column the books do not have, such as a prior year
  they do not go back to, shows **not available**.
- **Show the figures** works the report out as you edit, and saves nothing.

A row of zeros is left out unless you ask for it.

## Calculated rows

A calculation uses the IDs of the rows above it, numbers, `+ - * /` and brackets, such as
`(rev - cogs) / rev`. It is worked out exactly and rounded once, half away from zero: an amount to the
currency's decimals, a **percent** (the result times 100) to one decimal, a **ratio** to two. Where it
divides by zero, the figure is **not available**, never 0.

## Save it and share it

**Save the report** keeps it as version 1; each change is the next version. A report is **private** to you
or **shared** with everyone who may read the whole ledger's reports. Whoever runs it sees their own rights
at that moment: sharing a report never shows anyone figures they could not already read.

Only you, or an Owner or Administrator for a shared report, change it. **Archive** ends a report; it is
never deleted, so a schedule or a board pack that named it keeps naming it.

## Send it on a schedule

An Owner, Administrator, Controller or Accountant can schedule a report **weekly** on a weekday or
**monthly** on a day from 1 to 28, for the last closed month or the month so far, to people who may read
the whole ledger's reports.

Each person is told by a notification that links to the report. It carries **no figure and no file**, so
the figures are seen only after signing in. When it is sent, each person's right is checked again: someone
who has since lost it, or left, is skipped, and the schedule's runs say who and why. Sending to a Slack
channel waits for the Slack connection.

## Every figure has its source

Every figure is read at one ledger revision, which the report shows, so the same months read again at
that revision give the same figures.
