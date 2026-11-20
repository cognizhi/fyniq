---
key: reports.builder
type: task
title: Build, save and schedule your own report
summary: The report builder's rows, columns and calculated rows, saving and sharing a report, and scheduling it by notification with permissions checked at send time
sources:
  - docs/user-guide/report-builder.md sha256:f22dfb64d13baee2c0dd86b8d0dad480581237f58fc316d30cb49668b5a06529
data_class: general
taint: trusted
status: draft
human_edited: false
compiled_by: claude-sonnet-5-5 via Claude Code (fyniq-knowledge-base skill)
schema: kb-schema@2
compiled_at: 2026-11-23
aliases:
  - report builder
  - custom report
  - calculated row
  - schedule a report
  - share a report
  - how do I make my own report
---

# Build, save and schedule your own report

## Find it

- In Expert mode, **Reports** has a **Report builder**. It lists the reports the person can open: their own, and those shared with everyone who may read the whole ledger's reports.
- A role that reads only part of the books (sales, purchases, payroll, a department or the board pack) does not see the builder, because a report can name any account.

(src: docs/user-guide/report-builder.md#find-it)

## Build a report

- A report is rows and columns. A row is an account (by its code), a report group, a calculation or a heading.
- A column is the run, the period before it, the same months a year earlier, or the year to date. A profit and loss account shows its movement over the column's months; a balance sheet account shows its balance at the column's last day. A column the books do not have shows not available.
- Show the figures works the report out while editing and saves nothing. A row of zeros is left out unless asked for.

(src: docs/user-guide/report-builder.md#build-a-report)

## Calculated rows

- A calculation uses the IDs of the rows above it, numbers, `+ - * /` and brackets, such as `(rev - cogs) / rev`.
- It is worked out exactly and rounded once, half away from zero: an amount to the currency's decimals, a percent (the result times 100) to one decimal, a ratio to two.
- Where it divides by zero the figure is not available, never 0.

(src: docs/user-guide/report-builder.md#calculated-rows)

## Save it and share it

- Saving keeps the report as version 1; each change is the next version. A report is private to its maker or shared with everyone who may read the whole ledger's reports.
- Whoever runs it sees their own rights at that moment, so sharing never shows anyone figures they could not already read.
- Only the maker, or an Owner or Administrator for a shared report, changes or archives it. Archiving ends it without deleting it.

(src: docs/user-guide/report-builder.md#save-it-and-share-it)

## Send it on a schedule

- An Owner, Administrator, Controller or Accountant can schedule a report weekly on a weekday or monthly on a day from 1 to 28, for the last closed month or the month so far.
- Each person is told by a notification that links to the report. It carries no figure and no file, so the figures are seen only after signing in.
- At send time each person's right is checked again; someone who lost it, or left, is skipped and the schedule's runs say who and why. Sending to Slack waits for the Slack connection.

(src: docs/user-guide/report-builder.md#send-it-on-a-schedule)

## Every figure has its source

- Every figure is read at one ledger revision, which the report shows, so the same months read again at that revision give the same figures.

(src: docs/user-guide/report-builder.md#every-figure-has-its-source)
