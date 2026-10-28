---
key: purchases.payment-batches
type: task
title: Pay suppliers and claims in a payment batch
summary: Prepare a batch of approved bills and claims, have another person approve it, release it, pay it from the payment list, and see it paid
sources:
  - docs/user-guide/payment-batches.md sha256:8df0cbc1fd0bc56fe9ab89d0e4c95710dcd1bc0dff260586f3772c90f807a1a1
data_class: general
taint: trusted
status: draft
human_edited: false
compiled_by: claude-opus-5-5 via Claude Code (fyniq-knowledge-base skill)
schema: kb-schema@1
compiled_at: 2026-10-28
aliases:
  - payment batch
  - pay suppliers
  - bulk payment
  - payment run
  - pay claims
  - release payments
  - payment list
  - FAST
  - GIRO
  - remittance advice
  - needs approving again
---

# Pay suppliers and claims in a payment batch

## What it is

- A payment batch pays approved bills and expense claims together, from one SGD bank account on one date, by FAST or GIRO.
- Each supplier gets one payment for all its bills in the batch; each person one payment for all their claims.
- One person prepares it, another approves it, and the Payment releaser releases it.
- It pays SGD to Singapore bank accounts only; overseas and foreign-currency payments are still recorded by hand.

(src: docs/user-guide/payment-batches.md#what-it-is)

## Before you start

- The bank account it is paid from is set up in **Banking**, in SGD ([[banking.add-account]]).
- Each supplier has an approved SGD account at a Singapore bank under **Contacts › Bank details** ([[contacts.manage]]); each person who claims has their own approved account under **Settings › Profile and security** ([[preferences.security]]).
- An Accountant or an AP specialist prepares a batch; the Controller approves it; the Payment releaser releases it.

(src: docs/user-guide/payment-batches.md#before-you-start)

## Prepare a batch

- **Purchases › Bills › Batches**, **New batch**: the bank account, the payment date, FAST or GIRO, then **Prepare the batch**.
- It starts with the bills planned to be paid on or before the date and the claims still owed; remove anything not to pay now on the batch's page.
- An item shows why it cannot be paid: already paid, not in SGD, a withholding-tax decision pending, in another approved batch, or a payee with no approved bank account.
- A supplier or person whose bank details are changing is held until the change is approved, rejected or withdrawn.

(src: docs/user-guide/payment-batches.md#prepare-a-batch)

## Approve a batch

- **Ask for approval** and confirm it's you; another person who approves payments, such as the Controller, approves it from **Inbox › To approve**, seeing each payee, the account paid into and the total ([[approvals.decide]]).
- Whoever prepared it never approves it; approval numbers it, such as PB-0003.
- While approved and not released, its bills and claims cannot be paid by hand.

(src: docs/user-guide/payment-batches.md#approve-a-batch)

## When a payee's account changes

- A supplier's or person's bank account changed after approval, or what is open on a bill changed: the batch shows **Needs approving again** and cannot be released.
- **Ask to approve again**: the approver sees the accounts and total it pays now.

(src: docs/user-guide/payment-batches.md#when-a-payees-account-changes)

## Release a batch

- The Payment releaser selects **Release** and confirms it's you: each payment is recorded as recording it by hand does, dated the payment date, and the payment list is written.
- Standard mode: the releaser is a third person who neither prepared, asked for nor approved the batch, nor a payee's bank account change.
- Founder mode: the founder who prepared or approved it may release it; never one person alone, since another approved it, and the release is listed for the weekly review.

(src: docs/user-guide/payment-batches.md#release-a-batch)

## Pay it in the bank

- **Download payment list** and confirm it's you: it holds each payee's full account number, and each download is recorded.
- Key each payment into the bank's bulk payment screen; check the bank's count and total against the list's last row.
- Delete the file once the bank has it; the batch's page shows the list's SHA-256 to check a copy.

(src: docs/user-guide/payment-batches.md#pay-it-in-the-bank)

## Paid, and remittance advice

- Matching a batch's payment to its bank statement line shows it **Paid**; the batch is paid once every payment is.
- A supplier is then emailed remittance advice to its remittance address, once; one with no remittance email is warned of before release and sent nothing.
- A payment the bank rejects is voided on its own, and its bills or claims are open again.

(src: docs/user-guide/payment-batches.md#paid-and-remittance-advice)

## Cancel a batch

- An approved batch not yet released: **Cancel batch** with a reason; its bills and claims are free for another batch or to pay by hand.
- A released batch is never cancelled.

(src: docs/user-guide/payment-batches.md#cancel-a-batch) (src: docs/user-guide/payment-batches.md#related)
