---
key: nemura.payroll-records
type: task
title: Set up pay groups and record each person's pay in Nemura
summary: What the Payroll administrator and the Owner record for payroll, how a pay record, the year to date and a one-off item work, and why nobody else sees pay
sources:
  - docs/user-guide/nemura/payroll-records.md sha256:5929244f7df47a45d1f8dea7ab75a2303a63a84c58ee1af5278b85c46e3c19fe
data_class: personal
taint: trusted
status: published
human_edited: false
compiled_by: claude-sonnet-5-5 via Claude Code (fyniq-knowledge-base skill)
schema: kb-schema@2
compiled_at: 2026-11-20
aliases:
  - pay record
  - pay group
  - year to date
  - joint election
  - one-off bonus
  - payroll setup
---

# Set up pay groups and record each person's pay in Nemura

## What it is

- **Payroll** keeps what a pay run is worked out from: pay groups and their calendar, each person's pay record, the year to date of CPF wages, a permanent resident's joint election and the one-off items of a month.
- Only the Payroll administrator and the Owner see pay; the HR roles see that a run exists, not an amount, and the audit trail says that pay was set, never what it was. (src: docs/user-guide/nemura/payroll-records.md#what-it-is)

## Steps

1. Make a pay group: its name, the pay day (0 is the last day of the month), the days before the pay day that inputs close and the days before it that the run is approved.
2. Start a person's pay record: the day, the pay group, the basic monthly salary, the self-help group fund, and each allowance and fixed deduction; say whether an allowance is CPF wages (a fixed deduction never is).
3. For a company that starts mid-year, import the year to date: the CPF wages already counted since 1 January and the last month counted; once for a year.
4. For a first- or second-year permanent resident with a joint application, add the CPF Board's approval to the person's file, then record the election.
5. Add a month's one-off items: a bonus, a commission or an allowance, with the month it is for and the day it is payable.

(src: docs/user-guide/nemura/payroll-records.md#steps)

## What happens next

- A pay record is history: a change starts a new one from a later day. A one-off item's class follows its facts, not its name; for the month and payable by the 14th of the next month is ordinary wages, otherwise additional. A one-off is used by one run once. (src: docs/user-guide/nemura/payroll-records.md#what-happens-next)

## When it looks wrong

- A pay record cannot start before the person started or on the day the current one began; the year to date is imported once and subject wages cannot exceed paid; the election's evidence is a document on the person's file. See [[nemura.person]]. (src: docs/user-guide/nemura/payroll-records.md#common-problems)
