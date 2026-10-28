---
key: purchases.payment-batches
title: Pay suppliers and claims in a batch
mode: basic
roles: [accountant, ap_specialist, controller, payment_releaser, owner, auditor, tax_agent]
requirements: [FR-BNK-12, FR-AP-13, FR-AP-22, G4-055, SOD-1, SOD-2, FM-1, FM-3]
status: draft            # written with p1-payments-out; reviewed with the batch screens
---

# Pay suppliers and claims in a batch

## What it is

A payment batch pays approved bills and expense claims together, from one SGD bank account on one date, by FAST or
GIRO. Each supplier gets one payment for all its bills in the batch, and each person one payment for all their claims.
Each step is done by a different person: one prepares it, another approves it, and the Payment releaser releases it.

A batch pays SGD to Singapore bank accounts only. Overseas and foreign-currency payments are still recorded by hand.

## Before you start

- The bank account it is paid from is set up in **Banking**, in SGD.
- Each supplier has an approved bank account in SGD at a Singapore bank (**Contacts › Bank details**). Each person who
  claims has their own approved account (**Settings › Profile and security**).
- To prepare a batch you are an Accountant or an AP specialist; to approve it, the Controller; to release it, the
  Payment releaser.

## Prepare a batch

1. Open **Purchases › Bills › Batches** and select **New batch**.
2. Choose the bank account it is paid from, the payment date, and FAST or GIRO.
3. Select **Prepare the batch**. It starts with the bills planned to be paid on or before that date and the claims still
   owed.
4. On the batch's page, remove anything you do not want to pay now.

A bill or claim shows why it cannot be paid in the batch: it is already paid, it is not in SGD, its withholding-tax
decision is pending, it is in another approved batch, or its payee has no approved bank account. A supplier or person
whose bank details are changing is held until the change is approved, rejected or withdrawn.

## Approve a batch

Select **Ask for approval** and confirm it's you. Another person who approves payments, such as the Controller, approves
it from **Inbox › To approve**: they see each payee, the account it is paid into, and the total. Whoever prepared it
never approves it. Approval numbers the batch, such as PB-0003.

While a batch is approved and not released, its bills and claims cannot be paid by hand.

## When a payee's account changes

If a supplier's or person's bank account changes after the batch was approved, or what is open on a bill changes, the
batch shows **Needs approving again** and cannot be released. Select **Ask to approve again**: the approver sees the
accounts and total it pays now.

## Release a batch

The Payment releaser selects **Release** and confirms it's you. Releasing records each payment as recording it by hand
does, dated the payment date, and writes the payment list.

In Standard mode the releaser is a third person, who neither prepared, asked for nor approved the batch, nor the change
of a payee's bank account. In Founder mode the founder who prepared or approved it may release it; it is still never one
person alone, because another person approved it, and the release is listed for the weekly review.

## Pay it in the bank

1. Select **Download payment list** and confirm it's you. The list holds each payee's full account number, and each
   download is recorded.
2. Key each payment into your bank's bulk payment screen, and check the bank's count and total against the list's last
   row.
3. Delete the file once the bank has it.

The batch's page shows the list's SHA-256, so you can check a copy is the one released.

## Paid, and remittance advice

When you match a batch's payment to its line on the bank statement, it shows **Paid**, and the batch is paid once every
payment is. A supplier is then emailed remittance advice to its remittance address, once. A supplier with no remittance
email is warned of before release, and is sent nothing. If sending the advice fails after several tries, the payment
shows **Remittance not sent**: tell the supplier of the payment another way. A payment the bank rejects is voided on its
own, and its bills or claims are open again.

## Cancel a batch

An approved batch that is not released is cancelled with **Cancel batch** and a reason. Its bills and claims are free
for another batch, or to pay by hand. A released batch is never cancelled.

## Related

- [Approve a change](approvals.md)
- [Your profile and security](profile-and-security.md)
- [Contacts and items](contacts-and-items.md)
