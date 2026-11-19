---
key: nemura.payroll-approval
type: task
title: Read and approve a pay run in Nemura
summary: Who approves a pay run and why not its preparer, what the approver reads (digest, flags, overrides, what changed since last month) and what approving does
sources:
  - docs/user-guide/nemura/payroll-approval.md sha256:faefdfceaf5267911be09a113f6b9df3b1eca34fe53e50e2c2c228be42a2b9aa
data_class: personal
taint: trusted
status: published
human_edited: false
compiled_by: claude-sonnet-5-5 via Claude Code (fyniq-knowledge-base skill)
schema: kb-schema@2
compiled_at: 2026-11-20
aliases:
  - approve payroll
  - pay run approval
  - segregation of duties payroll
  - second approver
---

# Read and approve a pay run in Nemura

## What it is

- The Payroll administrator or the Owner who did not prepare the newest version reads it and confirms with a passkey over its digest. The person who prepared a version cannot approve it: the database refuses it too. In a team of one, the Owner is the second person. (src: docs/user-guide/nemura/payroll-approval.md#what-it-is)

## Steps

1. Open the run from **Pay runs** and read the totals and the digest your passkey confirms.
2. Read what to look at: a ✖ must be resolved and the next version calculated; a ⚠ is for the approver to judge.
3. Read each override and its reason.
4. Read the run against last month's: gross pay, net pay and employer cost, and every difference with its cause (a joiner, a leaver, a pay change, unpaid leave, overtime, a one-off item, an allowance or a deduction); anything with no explanation is listed apart.
5. Open a person to see their items, their class and the working of each figure, then confirm with a passkey and approve.

(src: docs/user-guide/nemura/payroll-approval.md#steps)

## What happens next

- The approval is written once with what the passkey confirmed; a correction of an approved month is a new version with a reason, and once approved it supersedes the one before.
- The one-off items, unpaid leave and overtime the version used are marked used by the run. Nothing is posted or paid by approving. (src: docs/user-guide/nemura/payroll-approval.md#what-happens-next)

## When it looks wrong

- Nobody approves their own run: ask another approver. A digest that is not this version's means the next version was calculated while you read: open the run again. A version with failing checks is resolved and recalculated first. See [[nemura.payroll-records]]. (src: docs/user-guide/nemura/payroll-approval.md#common-problems)
