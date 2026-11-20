---
key: nemura.payroll-approval
title: Read and approve a pay run
mode: expert
roles: [payroll_admin, owner]
requirements: [FR-PAY-07, NM-ROLE-04, NM-PAY-09, NM-PAY-11, NM-INT-03, NM-INT-06]
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

## When Nemura is linked to Fyniq

A run is then approved with proof, so that Fyniq can check that the people who approved it really did before it posts anything. The page shows **Approve with proof for Fyniq** instead of the plain button.

1. Read the figures Fyniq will post: the gross pay of each of Fyniq's departments, the month's CPF, SDL and self-help group funds, the deductions by kind, the claims paid with the salary, and the version of Fyniq's account mapping the run was calculated against. If something is missing, the page says what in plain words: a person with no department, a department Fyniq does not have (departments come from Fyniq: add it there), no mapping yet, or someone who has not signed in once.
2. Select **Confirm, sign the figures and approve**. Your browser asks for your passkey twice: once to confirm it is you, and once to sign the digest of the figures.
3. Sign with **the passkey you enrolled in Fyniq**. Fyniq checks your signature against it, so every approver signs in to Fyniq once and adds a passkey in its security settings. That gives you no Fyniq role.
4. Under **What Fyniq did with it** you see whether it was sent, and Fyniq's answer: the posting's reference, or the code and words of its refusal, with which the run waits. If the refusal is about something Fyniq's side can put right (you have no passkey there, a period is closed), put it right and select **Send it to Fyniq again**: the same approval goes again as a new message. If Fyniq refused the figures or the proof, calculate the run again and approve the new version.

Before you sign, the page tells you what Fyniq last said of the account you signed in with. If Fyniq knows you and says you have no passkey enrolled there, the page says so first: sign in to Fyniq, add a passkey in its security settings, then come back. If Fyniq has not said anything, you may have no sign-in in Fyniq yet: ask its Owner to invite you, then add a passkey there. This is a hint to save you a refused approval; Fyniq's own check after you sign decides. The approval names the account you signed in with, so use the same account in both apps.

While the apps are linked the plain approval is closed: a run is approved with proof or not at all.

## What happens next

- The approval is written once with what your passkey confirmed. A correction of an approved month is a new version that needs a reason; once approved, it supersedes the one before.
- The one-off items, unpaid leave and overtime the version used are marked used by this run, so no later run takes them again.
- Under **Payments**, each person owed money shows the state of their payment: scheduled, released, sent to the bank, accepted by the bank, paid, rejected, returned or outcome unknown. With Fyniq connected the states arrive from the bank feed. Standalone, you record an outcome yourself: choose the person, the outcome, the amount and the date, and a document of the person that shows it, such as a bank statement line. "Paid" is never accepted without that evidence, and a file or a posted journal is never proof. An unknown outcome blocks a new payment of that person until you record whether it was paid, rejected or returned; a rejected, returned or unknown payment shows on Checks. A payment confirmed for an amount other than the one owed is flagged.
- Under **Files for the books and the bank**, select **Make the journal file** or **Make the payment file** for the approved version. Standalone, Nemura makes both as CSV: the journal is the run's balanced entry, one line for each account; the payment file has one row for each person owed money with a bank account on record, and asks for your passkey because it holds account numbers. Neither file is kept, only its manifest (the run, the version, the totals and the SHA-256 of the exact file). People left out, such as those with no bank account on record, a payment already on its way or an unknown outcome, are listed with why. A file is never proof of payment, and no bank's own format is made yet. The same approved version also makes the **CPF EZPay file** in the CPF Board's own layout (effective 16 January 2025), under **Files for CPF and IRAS** (**Make the CPF file**): it needs your CPF Submission Number's payment type and serial number and an advice code, which only you know, the company's UEN from the letter settings and each person's NRIC, so it asks for your passkey too. A person with no NRIC on record, or an identification number that is not an S or T number, is left out and listed with why. The file is not proof that CPF was submitted or paid: upload it to CPF EZPay and record the Board's receipt as evidence.
- Select **Publish the payslips** on an approved version to make each person's itemised payslip, with the PDF and its hash, in their **My pay**. A version is published once; a corrected version has its own. The employer's name is the letterhead's, so set it first.
- Nothing is posted or paid by approving: posting to the books and paying are separate steps.

## Common problems

- **Nobody approves the run they prepared.** Ask another Payroll administrator or the Owner.
- **The digest you confirmed is not this version's.** Someone calculated the next version while you read: open the run again.
- **The version has failing checks.** Resolve each, calculate the next version, then approve it.
- **Fyniq says the approver has no passkey enrolled there (PPL-19).** Sign in to Fyniq once, add a passkey in its security settings, then select **Send it to Fyniq again**. The page warns you of this before you sign when Fyniq has told Nemura.
- **Your passkey did not sign the figures.** Use the passkey you enrolled in Fyniq, from Nemura's own address, and confirm with your fingerprint, face or PIN.

## Related

- [Link Nemura to Fyniq and keep the link healthy](fyniq-link.md)
- [Set up pay groups and record each person's pay](payroll-records.md)
- [Read your payslips](my-pay.md)
- [Pay a leaver's final pay and hold it for tax clearance](payroll-final-pay.md) The same section makes the **IR8A file** for IRAS's Auto-Inclusion Scheme (**Make the IR8A file**): the IR8A payload of the income year the run is paid in, from every approved run paid in that year, each person's pay items in the IR8A's own boxes (salary, bonus, allowances, commission, notice pay and loss of office; the accountant confirms the mapping) and the year's employee CPF, in whole dollars. It needs the person authorised to file, their contact number and email, and whether this is the original, an amendment or a revision. It also needs each person's NRIC or FIN and the sex and address the IR8A asks for, which the HR officer or the HR administrator sets on the person (kept sealed like the NRIC; only read to make this file). A person without them is left out and listed with why, and a detail that breaks one of IRAS's rules is refused with the rule named, so you correct it before anything is sent. A work permit holder or a passport holder is not yet written, the benefits-in-kind and share scheme appendices are sent empty, and a year of more than 2,000 people is refused until it can be sent in chunks.
