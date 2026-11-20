---
key: payroll.salary-payments
title: Pay salaries from a pay run
mode: basic
roles: [payroll_administrator, payment_releaser, owner, controller, accountant, auditor]
requirements: [FR-PAY-11, FR-PPL-10, FR-PPL-12, FR-PPL-13, FR-PPL-14, FR-PPL-17]
status: draft            # written with p1-payroll-link; reviewed with the salary payment screens
---

# Pay salaries from a pay run

## What it is

When a pay run approved in Nemura has been posted here, Nemura sends one salary line for each person: how much to pay and where. They arrive as a **salary batch**, such as SB-0003, which is
prepared, approved by another person and released like a payment batch. Fyniq never makes a person's pay a contact or a bill, and a salary is never shown to someone who has no need to see it.

## Before you start

- Payroll lives in Nemura and the link in **Settings › People and HR** is active.
- The pay run has been posted here: Fyniq takes salary lines only for the version it posted last.
- The bank account it is paid from is set up in **Banking**, in SGD.
- How the month's payslips, key employment terms, CPF, SDL and SHG, and where they apply the Auto-Inclusion Scheme and IR21 are met is recorded as evidence. While one is missing, a salary batch cannot be asked for approval.

## Prepare a salary batch

- **Payroll › Salary payments**, open the batch and choose **Prepare the batch**: the bank account, the payment date, and GIRO or FAST.
- Nemura sends each person's bank by its code and name, and Fyniq looks the BIC up from the code. For a bank Fyniq does not know, the batch cannot be asked for approval until you fill in that bank's BIC when preparing it, from the bank's own advice; the line then becomes a new version, so an earlier approval is stale.
- A batch that is prepared again needs its approval again.
- **Cancel the batch** is for a batch with nothing released; it pays nothing.

## Approve a salary batch

- **Ask for approval** and confirm it's you. Another person who holds the approval right on pay runs approves it from **Inbox › To approve**; they sign for the total and the lines, never a name or an account.
- Neither whoever prepared the pay run in Nemura nor whoever approved it there approves or releases its salary batch, and whoever asked for the approval never approves it.

## Release the free lines

- The Payment releaser selects **Release** and confirms it's you. The lines that are free to go are written to the payment file, with an instruction ID each; download it and pay it in the bank.
- A person held for tax clearance, a person whose bank details changed after the batch was approved, and a person with an unknown outcome wait while the others are paid. Each is released later, in a further release.
- Fyniq asks Nemura whether each person is held, and waits up to five seconds; with no answer, that person's line waits.
- When Nemura sends a changed line, the batch shows **Needs approving again**; ask again, and the approver signs for the lines as they are now.
- A release is never "paid". The file holds names and account numbers: key it into the bank or upload it, then delete it.

## What became of a payment

- A line is **paid** only when the bank feed shows its debit, matched to the lines it paid, or when a person records the outcome with a file of evidence. A posted journal or a payment file is never taken as payment.
- **Match a bank debit** on the batch pays released lines from the bank feed: choose the debit of the batch's bank account that waits in the Categorise queue, tick the lines it paid, and the amounts must make the debit exactly, to the cent. It posts to Salaries payable as categorising it does, and each paid line goes back to Nemura.
- A person who did not prepare or approve the pay run in Nemura records it: sent, accepted, paid, rejected, returned or unknown, with the day and the evidence.
- An unknown outcome means the money may have gone: that person cannot be paid again in the run until a person resolves it as paid, rejected or returned.
- Each outcome goes back to Nemura.

## Who sees the lines

- The Owner and the Payroll administrator see each person, the account masked to its last four digits and the amount.
- The Payment releaser, the Controller, the Accountant and the Auditor see the batch's total, the number of people and its state, and no line.
- Names and accounts are in no notification, audit trail entry or log.

## Payroll in the books

- **Payroll** in the sidebar (when the organisation has switched payroll on) is Fyniq's part of payroll on one page: where each feature lives (People records, Payroll, Leave, Letters), the state of the link to Nemura and a warning when it is not delivering everything, whether every statutory output of the month has a recorded arrangement, and the latest salary batches.
- **Away in the next 30 days** lists approved leave Nemura told Fyniq of, by person and dates; Nemura sends no leave type or reason, and it moves no money. Each part is read on its own, so a role that may not see one part still sees the others. Linking, unlinking and recording arrangements are in **Settings › People and HR**.

## A person from Nemura

- Open a person from **Payroll** to see what Nemura told Fyniq: their status, department and line manager, and whether they are held for tax clearance. Pay, bank numbers and NRIC stay in Nemura.
- **Map to this user** says which Fyniq user the person is. You choose the user; Fyniq never matches by email, and the mapping grants no role. Only an Owner, an Administrator or the People and HR right may map a person.

