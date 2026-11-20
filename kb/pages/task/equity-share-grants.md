---
key: equity.share-grants
type: task
title: Record shares for services and expense them over the service period
summary: Recording a grant of shares for services, how each tranche's expense is spread over whole service months, and proposing a month as a draft journal
sources:
  - docs/user-guide/share-grants.md sha256:56b3dd9fb62e1bfff6eccfbc2915474e17ad9b4506a3b055b90872aae4ad9ff8
data_class: general
taint: trusted
status: draft
human_edited: false
compiled_by: claude-sonnet-5-5 via Claude Code (fyniq-knowledge-base skill)
schema: kb-schema@2
compiled_at: 2026-11-21
aliases:
  - shares for services
  - share-based payment
  - share grant expense
---

# Record shares for services and expense them over the service period

## Find it

- In Expert mode, **Equity** has **Shares for services**. An Accountant records a grant; a Controller or Owner reads them. The Board viewer, an employee and the DPO see none of it.

(src: docs/user-guide/share-grants.md#find-it)

## Record a grant

- A grant is an equity-settled share-based payment (SFRS(I) 2 / IFRS 2). Enter the recipient, the fair value of a share at the grant date with its valuation (confirm it before relying on it), the service start (the first of a month), the tranches (whole shares and the day each vests) and two accounts: the expense account and the share-based payment reserve, an equity account that is not share capital.
- The fair value is fixed at the grant date and never remeasured. A tranche's service months are the whole calendar months from the service start's month to its vest day's month, both counted.

(src: docs/user-guide/share-grants.md#record-a-grant)

## The expense by month

- A tranche's total is its shares times the fair value, rounded once to the cent, spread over its months by cumulative rounding, so the months add to the total exactly; a month's expense is the tranches' parts for it.
- **Propose** a month to make a draft journal: debit the expense account, credit the reserve. An approver posts it like any journal, and a month is proposed once. Once a month is proposed the grant's figures stay as they were; only the recipient, the valuation note and archiving can change.
- No tax deduction is claimed for newly issued shares. A forfeiture before vesting is not worked here, and the reserve moves to share capital when the shares are issued, which is an equity transaction, not a journal.

(src: docs/user-guide/share-grants.md#the-expense-by-month)
