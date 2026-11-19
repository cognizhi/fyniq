---
key: nemura.payroll-records
type: task
title: Set up pay groups and record each person's pay in Nemura
summary: What the Payroll administrator and the Owner record for payroll, how a pay record, the year to date and a one-off item work, and why nobody else sees pay
sources:
  - docs/user-guide/nemura/payroll-records.md sha256:1b5b5864b69221ea217faa5103d905c2035b73f90bed73927f3bdf24da466ace
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
4. For a first- or second-year permanent resident with a joint application, add the CPF Board's approval to the person's file, then record the election with its kind: F/G (full employer, graduated employee rates) or F/F (full rates for both, the full table). A different kind is a new election from a later day.
5. Add a month's one-off items: a bonus, a commission or an allowance, with the month it is for and the day it is payable.

(src: docs/user-guide/nemura/payroll-records.md#steps)

## Calculate a pay run

- Choose the wage month and calculate the run for a pay group: every person with pay in the group is worked out exactly from their pay record and the inputs (a part month by the days each pay record covers, the one-off items, unpaid leave by the month's working days, approved overtime), each amount rounded once to the cent, and each person's month shows its items with their sources, its working and what to look at.
- A missing fact stops the run and names the person; a ✖ blocks approval and a ⚠ is shown to the approver.
- An override replaces one figure of one person's month (employee or employer CPF, SDL or the fund) with a reason of at least ten characters; it applies from the next calculation and is flagged. A version is read-only with a digest; the next version supersedes it, and after an approved version it is a correction with a reason.
- Overtime on a rest day or public holiday is added as a one-off item, not guessed. (src: docs/user-guide/nemura/payroll-records.md#calculate-a-pay-run)

## What happens next

- A pay record is history: a change starts a new one from a later day. A one-off item's class follows its facts, not its name; for the month and payable by the 14th of the next month is ordinary wages, otherwise additional. A one-off is used by one run once. (src: docs/user-guide/nemura/payroll-records.md#what-happens-next)

- The run is approved by another person, who reads it against last month's and confirms with a passkey: see [[nemura.payroll-approval]]. (src: docs/user-guide/nemura/payroll-records.md#related)

## When it looks wrong

- A pay record cannot start before the person started or on the day the current one began; the year to date is imported once and subject wages cannot exceed paid; the election's evidence is a document on the person's file. See [[nemura.person]]. (src: docs/user-guide/nemura/payroll-records.md#common-problems)
