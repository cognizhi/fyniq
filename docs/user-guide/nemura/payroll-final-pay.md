---
key: nemura.payroll-final-pay
title: Pay a leaver's final pay and hold it for tax clearance
mode: expert
roles: [payroll_admin, owner]
requirements: [NM-PAY-06, NM-PAY-08, NM-INT-08, NM-PAY-03]
---

# Pay a leaver's final pay and hold it for tax clearance

## What it is

When you record that a person is leaving, **Pay** shows their final pay under their pay record: when the final salary is due and the latest day to pay it for the case, the annual leave left to the last day with what it is worth, the final pay items already added and the tax-clearance hold. The days come from MOM's page on paying salary, held in the people pack, and Checks shows what is due (N.1 #15).

- A resignation with the notice served is due on the last day. Without it, it is due within 7 days of the last day.
- A termination by the employer, a dismissal for misconduct and a retrenchment are due on the last day, or within 3 working days of it if that is not possible. MOM's page does not name a retrenchment: Nemura treats it as a termination by the employer until that is confirmed.
- Other endings, such as the end of a contract, are due on the last day.
- Nemura counts the notice as served when a date of notice is recorded. It does not compare the notice period with the contract: check that yourself.

## Before you start

- Record how and when the person leaves: see [Invite new people to Fyniq and record who is leaving](onboarding.md). The last day sets the month of the final run, which pays the last salary part month.
- The person has a pay record: see [Set up pay groups and record each person's pay](payroll-records.md).

## Steps

1. Open **Pay**, choose the person and read their final pay. The proposed worth of the leave is the days left times the day rate of an unpaid-leave deduction (basic pay over the month's working days, rounded once to the cent). It is only a proposal.
2. Add the items the contract pays out as **one-off items** of the final month, with the nature **Leave pay on leaving**, **Pay in lieu of notice** or **Retrenchment benefit**. A retrenchment benefit is never CPF wages, because the CPF Board lists it with compensation not given for work done. Whether leave pay or pay in lieu of notice is CPF wages is for you to confirm against the CPF Board's table of payments: the run flags both every time until then (accountant to confirm).
3. For a foreign employee who is ceasing work in Singapore, select **Put a tax-clearance hold on** when you file IR21. Calculate the next version of the run: every dollar due to the person is held and none is disbursed.
4. When IRAS gives clearance, choose a document of the person that shows it and select **Release the hold**. Calculate the next version, approve it and pay it.

## What happens next

- Fyniq is told each version of the hold, so its payment path leaves the person out while it is held.
- The final pay is late on Checks when the latest day has passed and it is not paid. Pay held under a hold shows as a warning with the hold named, never as late.

## Common problems

- **There is no final pay for this person.** Nobody has recorded them leaving: record it first.
- **The hold cannot be released.** A release needs a document of the person as evidence, and only the document of that person counts.
- **The leave left is negative.** The person took more than they earned to the last day. Whether anything is deducted is for the contract and the people team: Nemura does not propose a deduction.

## Related

- [Read and approve a pay run](payroll-approval.md)
- [Read your payslips](my-pay.md)
