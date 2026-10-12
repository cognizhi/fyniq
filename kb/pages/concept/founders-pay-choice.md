---
key: founders.pay-choice
type: concept
title: Founder pay choices
summary: No salary (the recommended default), salary paid, deferred, director's fee or shares; deferral still triggers CPF and tax
sources:
  - docs/user-guide/founders-money.md sha256:e5cdae8c67b4c128861edea59e52ebd50836b668f636b471915223b4a31c249a
data_class: general
taint: trusted
status: draft
human_edited: false
compiled_by: claude-opus-5-5 via Claude Code (fh-knowledge-base skill)
schema: kb-schema@1
compiled_at: 2026-10-12
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
- It is a setting: Settings → Founders' money → Pay choice per director.
  (src: docs/user-guide/founders-money.md#the-recommended-defaults)

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
