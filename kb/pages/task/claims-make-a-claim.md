---
key: claims.make-a-claim
type: task
title: Claim back money spent for the company
summary: How an employee claims a receipt or a car trip in My claims, what happens after submitting, and the common policy problems
sources:
  - docs/user-guide/claims-my-claims.md sha256:4b19fca0fa1636de863d1450960bdc58102e512beb37e36d193eb6c0a4ea70ef
data_class: general
taint: trusted
status: draft
human_edited: false
compiled_by: claude-opus-5-5 via Claude Code (fyniq-knowledge-base skill)
schema: kb-schema@1
compiled_at: 2026-10-14
aliases:
  - expense claim
  - reimbursement
  - claim a receipt
  - mileage claim
---

# Claim back money spent for the company

## Where claims live

- **My claims** is where a person asks the company to pay back what they spent for it: a receipt,
  or a trip in their own car. (src: docs/user-guide/claims-my-claims.md#what-it-is)
- Someone who only claims lands on My claims and sees only their own items, never the company's
  figures. (src: docs/user-guide/claims-my-claims.md#what-it-is)
- From Nemura, the **Claims** tile opens My claims with the same sign-in; **Back to People**
  returns. (src: docs/user-guide/claims-my-claims.md#before-you-start)
- The expense policy is under **Before you claim**: limits per category, receipts needed and the
  deadline. (src: docs/user-guide/claims-my-claims.md#before-you-start)

## Steps

1. Open **My claims** and select **New claim from a receipt**: paste, drag in, or use
   **Camera** on a phone. A receipt email can also be forwarded to the person's capture address.
2. Check the date, amount and currency, category and project; split lines when one receipt
   covers different things.
3. For a car trip, use **Mileage trip**: date, purpose, from and to, distance. The amount is the
   distance times the company's rate for the vehicle type.
4. Fix what the policy check flags, then submit, and follow the status under **Your claims**:
   draft, submitted, approved, scheduled, paid or returned.

(src: docs/user-guide/claims-my-claims.md#steps)

## After submitting

- The claim goes to the line manager first, then finance; in Standard mode nobody approves their
  own claim. The manager's side: [[claims.approve-team-claims]].
- An approved claim is paid in the next payment batch, or with the salary as a reimbursement; a
  claim in another currency is paid back in Singapore dollars.

(src: docs/user-guide/claims-my-claims.md#what-happens-next)

## Common problems

- **Already claimed or captured**: blocked until finance records why it is not a duplicate.
- **Above the category limit**: can be sent; the policy's extra approver checks it.
- **Past the deadline**: needs finance's approval with a reason.
- **Returned**: read the approver's note, fix, submit again.

(src: docs/user-guide/claims-my-claims.md#common-problems)

## Payslips

- Payslips are not in Fyniq: with Nemura they are under **My pay**, otherwise in the payroll
  provider's system. (src: docs/user-guide/claims-my-claims.md#related)
