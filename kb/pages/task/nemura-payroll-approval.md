---
key: nemura.payroll-approval
type: task
title: Read and approve a pay run in Nemura
summary: Who approves a pay run and why not its preparer, what the approver reads and signs (with proof for Fyniq when linked) and what approving does
sources:
  - docs/user-guide/nemura/payroll-approval.md sha256:a1ec9c0a766b50b5211242d4fc55eacaf0a6e7162b0d2437b13422b6c2eb2713
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

## When Nemura is linked to Fyniq

- A run is then approved with proof and the plain approval is closed. The approver reads the figures Fyniq will post (the gross pay of each of Fyniq's departments, the month's CPF, SDL and self-help group funds, the deductions by kind, the claims paid with the salary and Fyniq's account mapping version), confirms with a passkey and then signs the figures' digest with the passkey they enrolled in Fyniq. Fyniq checks that signature before it posts anything, so every approver signs in to Fyniq once and adds a passkey in its security settings; that gives no Fyniq role. The page says in plain words what is missing: a person with no department, a department Fyniq does not have, no mapping yet, or someone who has not signed in once.
- Under **What Fyniq did with it** the run shows whether it was sent and Fyniq's answer: the posting's reference, or the code and words of its refusal, with which the run waits. A refusal Fyniq's side can put right (no passkey there, a closed period) is followed by **Send it to Fyniq again**, the same approval as a new message; a refusal of the figures or the proof needs a new calculation and a new approval. (src: docs/user-guide/nemura/payroll-approval.md#when-nemura-is-linked-to-fyniq)
- Before signing, the page says what Fyniq last said of the account signed in with: if Fyniq knows the person and says no passkey is enrolled there, sign in to Fyniq, add one in its security settings, then return; if Fyniq has said nothing, the person may have no sign-in there yet and asks its Owner for an invitation. It is a hint that saves a refused approval: Fyniq's own check after signing decides. The approval names the account signed in with, so use the same account in both apps.

## What happens next

- The approval is written once with what the passkey confirmed; a correction of an approved month is a new version with a reason, and once approved it supersedes the one before.
- The one-off items, unpaid leave and overtime the version used are marked used by the run. Under **Payments** each person owed money shows the state of their payment (scheduled, released, sent to the bank, accepted by the bank, paid, rejected, returned or outcome unknown); standalone you record an outcome with the amount, the date and a document of the person as evidence, and "paid" is never accepted without evidence. An unknown outcome blocks a new payment of that person until it is resolved, and a rejected, returned or unknown payment shows on Checks. Under **Files for the books and the bank**, make the journal file or, with your passkey, the bank payment file of the approved version as CSV: only the manifest (run, version, totals and the SHA-256 of the file) is kept, people left out are listed with why, and a file is never proof of payment. The same approved version makes the CPF EZPay file in the CPF Board's own layout (effective 16 January 2025), through the API for now with kind cpf: it needs the payment type, serial number and advice code of the employer's CPF Submission Number, the company's UEN and each person's NRIC, so it asks for a passkey; a person with no NRIC, or one that is not an S or T number, is left out and listed with why, and the file is not proof that CPF was submitted or paid. Select **Publish the payslips** on an approved version to put each person's itemised payslip in their My pay (see [[nemura.my-pay]]; a leaver's final pay: [[nemura.payroll-final-pay]]); a version is published once and the employer's name is the letterhead's. Nothing is posted or paid by approving. (src: docs/user-guide/nemura/payroll-approval.md#what-happens-next)

## Files for CPF and IRAS

Under **Files for CPF and IRAS** an approved version makes the **CPF EZPay file** (it needs your CPF Submission Number's payment type and serial number and an advice code, which only you know) and the **IR8A file** for IRAS's Auto-Inclusion Scheme: the IR8A payload of the income year the run is paid in, from every approved run paid in that year, with each person's pay items in the IR8A's boxes (the accountant confirms the mapping) and the year's employee CPF. The IR8A file also needs the person authorised to file, their contact number and email, and each person's NRIC or FIN and the sex and address set on the person by the HR officer or the HR administrator. Each asks for your passkey. A person without the details is left out and listed with why, and a detail that breaks one of IRAS's rules is refused with the rule named. A year of more than 2,000 people is made a part at a time (choose the part, from 1; the file says how many parts the year has and is named for its part). Neither file is proof that anything was submitted.

(src: docs/user-guide/nemura/payroll-approval.md#steps)

## When it looks wrong

- Fyniq says the approver has no passkey enrolled there (PPL-19): sign in to Fyniq once, add a passkey, then send the approval again. A passkey that did not sign the figures is the wrong one: use the passkey enrolled in Fyniq, from Nemura's own address (the link itself: [[nemura.fyniq-link]]). Nobody approves their own run: ask another approver. A digest that is not this version's means the next version was calculated while you read: open the run again. A version with failing checks is resolved and recalculated first. See [[nemura.payroll-records]]. (src: docs/user-guide/nemura/payroll-approval.md#common-problems)
