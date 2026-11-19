---
key: nemura.payroll-final-pay
type: task
title: Pay a leaver's final pay and hold it for tax clearance in Nemura
summary: When a leaver's final salary is due, how leave pay and a retrenchment benefit are added, and how a tax-clearance hold holds the pay until IRAS clears it
sources:
  - docs/user-guide/nemura/payroll-final-pay.md sha256:fe4aaff0e56106fa2d797523d96c3306cb0c5e967cae776b43a38c13b40b4bca
data_class: personal
taint: trusted
status: published
human_edited: false
compiled_by: claude-sonnet-5-5 via Claude Code (fyniq-knowledge-base skill)
schema: kb-schema@2
compiled_at: 2026-11-20
aliases:
  - final pay
  - final salary
  - leave encashment
  - tax clearance hold
  - IR21 hold
  - retrenchment benefit
---

# Pay a leaver's final pay and hold it for tax clearance in Nemura

## What it is

- Under a leaver's pay record, Pay shows when the final salary is due and the latest day to pay it for the case, the annual leave left to the last day with its worth as a proposal, the final pay items added and the tax-clearance hold. The days come from MOM's page on paying salary, held in the people pack, and Checks shows what is due. (src: docs/user-guide/nemura/payroll-final-pay.md#what-it-is)
- A resignation with the notice served is due on the last day; without it within 7 days of the last day. A termination by the employer, a dismissal for misconduct and a retrenchment are due on the last day, or within 3 working days if that is not possible; a retrenchment is treated as a termination until MOM's page is confirmed. Notice counts as served when a date of notice is recorded; the contract's notice period is not compared. (src: docs/user-guide/nemura/payroll-final-pay.md#what-it-is)

## Steps

1. Read the final pay under the person's pay record.
2. Add leave pay on leaving, pay in lieu of notice or a retrenchment benefit as one-off items of the final month. A retrenchment benefit is never CPF wages; whether the other two are CPF wages is for you to confirm against the CPF Board's table, and the run flags them until then.
3. For a foreign employee ceasing work in Singapore, put a tax-clearance hold on when you file IR21: every dollar due is held and none is disbursed from the next version of the run.
4. When IRAS clears, release the hold with a document of the person as evidence, then calculate, approve and pay.

(src: docs/user-guide/nemura/payroll-final-pay.md#steps)

## What happens next

- Fyniq is told each version of the hold. Final pay not paid by the latest day is late on Checks; pay held under a hold shows as a warning naming the hold. (src: docs/user-guide/nemura/payroll-final-pay.md#what-happens-next)

## When it goes wrong

- No final pay shown: nobody has recorded the person leaving ([[nemura.onboarding]]). A hold that cannot be released needs a document of that person. A negative leave balance is for the contract and the people team: Nemura proposes no deduction. (src: docs/user-guide/nemura/payroll-final-pay.md#common-problems)
- Related: [[nemura.payroll-approval]]. (src: docs/user-guide/nemura/payroll-final-pay.md#related)
