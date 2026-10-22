---
key: purchases.bills
title: Bills
mode: basic
roles: [bookkeeper, ap_specialist, accountant, controller, approver, owner, auditor, tax_agent]
requirements: [FR-AP-02, RND-GST-EXCL, ENG-42, FM-1, FM-2, SOD-3]
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
- **Overdue** once its due date has passed while it is still unpaid.

A Bookkeeper, an AP specialist or an Accountant enters bills. An Accountant posts, voids and edits
them. The Owner, the Auditor and the Tax agent can see them.

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

## Common problems

| Message | What to do |
|---|---|
| The bill's supplier is not an active vendor (BILL-05) | Choose a contact marked as a vendor, or reactivate it |
| Not a GST code for purchases (BILL-06) | Choose the code on the supplier's invoice, such as TX, BL or NR |
| The due date is before the bill's date (BILL-07) | Enter a due date on or after the invoice date, or leave it blank |
| The bill needs another person's approval (BILL-09) | Select **Ask for approval** |
| A system account is missing (LEDGER-22) | Load the standard chart, which has 2010 Accounts payable and 2200 GST control |

## Related

- [Exchange rates](currencies.md)
- [Organisation settings](organisation-settings.md)
- [Contacts and items](contacts-and-items.md)
- [Approvals](approvals.md)
