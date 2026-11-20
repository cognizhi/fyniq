---
key: nemura.my-pay
type: task
title: Read your payslips in Nemura
summary: What My pay shows, the twelve items every payslip holds, how to print or save one, and what a corrected payslip is
sources:
  - docs/user-guide/nemura/my-pay.md sha256:d448d4a879add3a549b654f9e75819599a228d05005e7b53eda84d5bcbebe74b
data_class: personal
taint: trusted
status: published
human_edited: false
compiled_by: claude-sonnet-5-5 via Claude Code (fyniq-knowledge-base skill)
schema: kb-schema@2
compiled_at: 2026-11-20
aliases:
  - payslip
  - my payslip
  - itemised payslip
  - download payslip
  - corrected payslip
---

# Read your payslips in Nemura

## What it is

- **My pay** shows the next pay day from the pay group's calendar and lists a person's payslips by month, each with the net salary and the day paid. Only the person reads their own.
- A payslip holds the twelve items MOM requires: the employer's and the employee's full names, the date of payment, the basic salary, the start and end of the salary period, allowances, other additional payments such as a bonus, deductions (CPF, a self-help group fund, no-pay leave and others), overtime hours, overtime pay and overtime payment period when overtime applies, and the net salary. (src: docs/user-guide/nemura/my-pay.md#what-it-is)

## Steps

1. Open **My pay** and select a month.
2. **Print** it, or **Open the PDF** to save it; the PDF is the exact document issued, and Nemura refuses to show one whose bytes no longer match its hash.
3. A payslip marked **Corrected** is replaced by a newer one: read the newer one.

(src: docs/user-guide/nemura/my-pay.md#steps)

## What happens next

- In the month a person joins or leaves, basic salary is the monthly pay times the working days employed over the working days of the month; rest days are left out and public holidays are counted. (src: docs/user-guide/nemura/my-pay.md#what-happens-next)
- A correction of the pay is a new payslip for that month; the first stays, marked corrected. After leaving, the narrow account keeps payslips for the record-keeping period; see [[nemura.leaver-account]]. (src: docs/user-guide/nemura/my-pay.md#what-happens-next)

## When it looks wrong

- You can ask what a payslip line is: [[nemura.ask-assistant]].
- No payslip for a month: the run is not approved or published yet; ask payroll ([[nemura.payroll-approval]]). A figure that looks wrong: ask the people team ([[nemura.ask-hr]]). (src: docs/user-guide/nemura/my-pay.md#common-problems)
