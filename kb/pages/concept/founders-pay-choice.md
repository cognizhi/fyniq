---
key: founders.pay-choice
type: concept
title: Founder pay choices
summary: No salary (the recommended default), salary paid, deferred, director's fee or shares; deferral still triggers CPF and tax
sources:
  - docs/user-guide/founders-money.md sha256:3fee41697faa10d7801395b2ba9c3f20be8c81e257aeee8290edb0832ae08a27
  - docs/user-guide/director-accounts.md sha256:6dc3ddc6df584a9a9e6bd24fa5dac69cc0066bdd683d15a3633aed9c543863a2
data_class: general
taint: trusted
status: draft
human_edited: false
compiled_by: claude-opus-5-5 via Claude Code (fyniq-knowledge-base skill)
schema: kb-schema@1
compiled_at: 2026-11-03
---

# Founder pay choices

## Recommended default

- **No salary**, agreed in writing before go-live and reviewed each financial year, until the
  company can pay in cash; then a normal salary through payroll.
  (src: docs/user-guide/founders-money.md#the-recommended-defaults)
- Why: no CPF or tax on money the founder never received. (src: docs/user-guide/founders-money.md#the-recommended-defaults)
- Exception: a founder holding an **Employment Pass** may be required by its conditions to be
  paid the declared salary; ask the accountant before choosing "no salary".
  (src: docs/user-guide/founders-money.md#the-recommended-defaults)
- It is changed in Accounting › Director accounts → Change pay choice.
  (src: docs/user-guide/founders-money.md#the-recommended-defaults)

## Record a pay choice

- Each person's card shows the choice in force this month; with none recorded, **No salary**.
  (src: docs/user-guide/director-accounts.md#record-a-pay-choice)
- **Change pay choice**: the month it applies from and the choice, then the signed decision (board
  minute or directors' resolution) attached, then **Record the pay choice**; an Owner or the
  Controller records it. (src: docs/user-guide/director-accounts.md#record-a-pay-choice)
- A recorded choice never changes: a new one from a later month applies from then. Payroll
  applies the choice once it is built. (src: docs/user-guide/director-accounts.md#record-a-pay-choice)

## The choices

| Choice | What it means |
|---|---|
| No salary | Nothing to record, no CPF, nothing to convert later; the cleanest while cash is short |
| Salary paid | A normal payroll |
| Salary deferred | Payroll runs, but net pay is owed instead of paid. **CPF is still due at the end of the month and the salary is taxed**, though nothing was received. Avoid unless the accountant recommends it |
| Director's fee | Approved by the shareholders; no CPF on fees voted at a general meeting |
| Shares instead of pay | Income tax on the shares' value without receiving cash; rarely sensible before a priced round |

(src: docs/user-guide/founders-money.md#your-pay-choices)

Unpaid pay becomes part of the founder's balance: [[founders.balance]].

## Messages and fixes

- **✖ Pay deferred without a recorded pay choice**: record the pay choice for that director and
  month, or pay the salary. (src: docs/user-guide/founders-money.md#common-problems)
- **The pay choice has no signed decision attached**: attach the minute or resolution, then record
  it; **the pay choice is recorded, so it changes no more**: record a new choice from the month it
  changes. (src: docs/user-guide/director-accounts.md#common-problems)
