---
key: nemura.payroll-approval
title: Read and approve a pay run
mode: expert
roles: [payroll_admin, owner]
requirements: [FR-PAY-07, NM-ROLE-04, NM-PAY-09, NM-PAY-11]
---

# Read and approve a pay run

## What it is

Approving a pay run is the second pair of eyes before money moves. The Payroll administrator or the Owner who did **not** prepare the newest version reads it, then confirms with a passkey over its
digest. The person who prepared a version cannot approve it: the database refuses it, and the page says so instead of showing a button that would fail. In a team of one, the Owner is the second person.

## Before you start

- The run is calculated and its newest version has no failing check (✖).
- You did not prepare that version. You hold the Payroll administrator's or the Owner's role.

## Steps

1. Under **Pay runs** on **Payroll**, select **Read and approve the run**.
2. Read the totals and the **Digest**: it identifies exactly these figures, and your passkey confirms it.
3. Read **What to look at**: a ✖ must be resolved and the next version calculated; a ⚠ is yours to judge, such as a pay item whose name disagrees with its class, or an override.
4. Read **Overrides in this version**: each replaces a calculated figure and says why.
5. Read **Since last month**: gross pay, net pay and employer cost against last month's approved run, and every difference with its cause and the item it came from: a joiner, a leaver, a pay change,
   unpaid leave, overtime, a one-off item, an allowance or a deduction. Anything with no explanation is listed apart: check it before you approve.
6. Open any person under **Each person's month** to see their items with their class and the working of each figure.
7. Select **Confirm with my passkey and approve**.

## What happens next

- The approval is written once with what your passkey confirmed. A correction of an approved month is a new version that needs a reason; once approved, it supersedes the one before.
- The one-off items, unpaid leave and overtime the version used are marked used by this run, so no later run takes them again.
- Under **Payments**, each person owed money shows the state of their payment: scheduled, released, sent to the bank, accepted by the bank, paid, rejected, returned or outcome unknown. With Fyniq connected the states arrive from the bank feed. Standalone, you record an outcome yourself: choose the person, the outcome, the amount and the date, and a document of the person that shows it, such as a bank statement line. "Paid" is never accepted without that evidence, and a file or a posted journal is never proof. An unknown outcome blocks a new payment of that person until you record whether it was paid, rejected or returned; a rejected, returned or unknown payment shows on Checks. A payment confirmed for an amount other than the one owed is flagged.
- Select **Publish the payslips** on an approved version to make each person's itemised payslip, with the PDF and its hash, in their **My pay**. A version is published once; a corrected version has its own. The employer's name is the letterhead's, so set it first.
- Nothing is posted or paid by approving: posting to the books and paying are separate steps.

## Common problems

- **Nobody approves the run they prepared.** Ask another Payroll administrator or the Owner.
- **The digest you confirmed is not this version's.** Someone calculated the next version while you read: open the run again.
- **The version has failing checks.** Resolve each, calculate the next version, then approve it.

## Related

- [Set up pay groups and record each person's pay](payroll-records.md)
- [Read your payslips](my-pay.md)
