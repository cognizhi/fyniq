---
key: purchases.bills
title: Bills
mode: basic
roles: [bookkeeper, ap_specialist, accountant, controller, approver, owner, auditor, tax_agent]
requirements: [FR-AP-02, FR-AP-06, FR-AR-18, RND-GST-EXCL, ENG-41, ENG-42, FM-1, FM-2, SOD-3]
status: draft            # written with the bill screens (P1, p1-purchases); reviewed with the accountant
---

# Bills

## What it is

**Purchases → Bills** keeps what suppliers have billed the company: each supplier's invoice, with
its lines and GST. Posting a bill adds its total to what the company owes the supplier.

A bill has separate states:

- **Draft** until it is posted; **Awaiting approval** while another person is asked to approve it;
  **Rejected** when they said no;
- **Posted** once it is in the books, or **Voided** once it is reversed;
- **Part paid** or **Paid** once payments are recorded against it;
- **Overdue** once its due date has passed while it is not paid in full.

A Bookkeeper, an AP specialist or an Accountant enters bills. An Accountant posts, voids and edits
them. The Owner, the Auditor and the Tax agent can see them.

## What the company owes

At the top of **Purchases → Bills**, **What we owe suppliers** says how much the bills due in the
next 30 days need, in each currency, and how much of it is overdue. A bill falls due on its due
date, or by the payment terms when it has none. Credits with suppliers are not taken off: apply them
to a bill first.

In Expert mode it also shows the ageing: what is owed by how long it is past due (not yet due,
1–30, 31–60, 61–90 and over 90 days), with the credits apart.

## Enter a bill

1. Select **New bill**.
2. Choose the **Supplier**: a contact marked as a vendor. Add one under Contacts if it is new.
3. Enter **Their invoice number** and the **Invoice date** as the invoice shows them. Leave the
   **Due date** blank to take the supplier's payment terms, or the company's (30 days unless
   changed in the organisation's settings).
4. For each line, choose **What it is for** (an expense or asset account), the **GST code** on the
   invoice, the **Amount before GST** and the **GST as stated**.
5. Select **Save as a draft**. Nothing is in the books until the bill is posted.

For a bill in another currency, enter the amounts in that currency. Leave the rate blank to take
the rate for the invoice date (see [Exchange rates](currencies.md)), and enter the **GST stated in
SGD** as the supplier's tax invoice states it: that is the input tax claimed.

## Check and post a bill

A saved draft shows what stops it posting (✖) and what it would post with (⚠):

- the stated GST differs from Fyniq's own working of it, even by a cent: check it against the
  invoice; the GST as stated is what posts;
- another bill from the supplier has the same number and total within 60 days: it may be the same
  bill entered twice;
- a bill in another currency states no GST in SGD: its GST stays in the cost, not claimed.

In Expert mode **Ledger impact** shows what posting writes; in Basic mode one sentence says how
much the bill adds to what the company owes the supplier. Select **Post** to post it on its date.
Its number comes from the bill sequence (BILL-0001 onwards).

Input tax is claimed only once the company is GST-registered on the bill's date, for codes such as
TX. Until then the GST is part of the cost.

## Bills above the self-approval threshold

A bill whose total is above the self-approval threshold (S$2,000 unless changed) is not posted by
the person posting it. Select **Ask for approval**: another person who approves bills, and who did
not prepare this one, approves it in the Inbox, and it posts then. Below the threshold, in Founder
mode, a founder posts a bill alone and it is listed for the weekly review.

## Void or edit a posted bill

A posted bill is never changed in place.

- **Void** posts a reversing entry on a date you choose, with a reason. The bill stays in the books,
  marked voided.
- **Edit** reverses the entry the bill has and posts the new version in one step, with a reason.
  The bill keeps its number.

In Founder mode another founder approves a void or an edit first: the buttons say **Ask to void**
and **Ask to post the edit**.

## Record a payment

When the company has paid a supplier, record the payment on the bill. Bank import will match
payments later; until then they are recorded by hand.

1. Open the posted bill and select **Record payment**.
2. Enter the **Date paid** and choose the account it was **Paid from**: a bank, card or wallet
   account in the bill's currency or in SGD.
3. Enter the **Amount paid of the bill**. It starts at what is still open; enter less to record a
   part payment. A payment never settles more than is open.
4. Enter the **Bank reference** if there is one, and select **Record payment**.

The payment posts at once, numbered from the payment sequence (PAY-0001 onwards): what the company
owes the supplier goes down, and so does the account it was paid from. The bill shows **Part paid**
or **Paid**, what is still open, and its payments.

For a bill in another currency:

- paid from an account in that currency, the payment takes the rate for the date paid, or the rate
  you enter from the bank's advice;
- paid from an SGD account, enter **What the account paid** in SGD as the statement shows it.

The difference between what the bill carried at its own rate and what the payment cost is the
exchange gain or loss, posted to FX & rounding differences. A bill paid in parts can leave a cent
of rounding; the last part clears it.

## Void a payment

A payment recorded by mistake is voided, never deleted. Select **Void** beside it under
**Payments**, with a date and a reason: a reversing entry posts and the bill is open again by what
the payment paid. In Founder mode another founder approves the void first: the button says **Ask to
void**.

A bill that payments settle is not voided or edited until its payments are voided.

## Common problems

| Message | What to do |
|---|---|
| The bill's supplier is not an active vendor (BILL-05) | Choose a contact marked as a vendor, or reactivate it |
| Not a GST code for purchases (BILL-06) | Choose the code on the supplier's invoice, such as TX, BL or NR |
| The due date is before the bill's date (BILL-07) | Enter a due date on or after the invoice date, or leave it blank |
| The bill needs another person's approval (BILL-09) | Select **Ask for approval** |
| A system account is missing (LEDGER-22) | Load the standard chart, which has 2010 Accounts payable and 2200 GST control |
| More than the bill has open (BILLPAY-05) | Enter at most what is open; void a payment entered twice |
| What the account paid is missing (LEDGER-27) | For a bill in another currency paid from an SGD account, enter what the account paid in SGD |
| Payments settle the bill (BILL-13) | Void its payments first, then void or edit the bill |

## Related

- [Exchange rates](currencies.md)
- [Organisation settings](organisation-settings.md)
- [Contacts and items](contacts-and-items.md)
- [Approvals](approvals.md)
