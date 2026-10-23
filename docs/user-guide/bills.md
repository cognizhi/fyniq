---
key: purchases.bills
title: Bills
mode: basic
roles: [bookkeeper, ap_specialist, accountant, controller, approver, owner, auditor, tax_agent]
requirements: [FR-AP-02, FR-AP-03, FR-AP-06, FR-AP-19, FR-AP-20, FR-AR-18, RND-GST-EXCL, RND-GST-SHARE, RND-WHT, ENG-41, ENG-42, FM-1, FM-2, SOD-3]
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

## Attach the supplier's invoice

Under **Evidence** on a saved bill or credit note, choose the file, say what it is (such as the
invoice or a receipt) and select **Attach**. PDF, image, CSV, text, XLSX, DOCX and email files of up
to 25 MB are accepted; Fyniq scans each one and strips image metadata such as location.

- While the bill is a draft, a file can come off again.
- Once the bill is posted, its files are kept with it and never deleted, for as long as the records
  are. A file added later is kept the same way.

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
TX. Until then the GST is part of the cost. **Input tax** below says what else decides it.

## Input tax

Whether a bill's GST is claimed is decided line by line from what the bill records, never from the
GST amount alone. Under **Input tax** on the bill, say:

- **What the claim rests on**: a **Tax invoice** addressed to the company (an invoice to "Company
  c/o another" counts); a **Simplified tax invoice**, which needs no customer name; an **Import
  permit** naming the company; or **No valid tax invoice**;
- whether **the supplier is GST-registered**: its GST number is on the document;
- whom it is **addressed to**: the company, or an employee or director who bought it for the
  company;
- on each line, the **Business share (%)**: how much of the purchase is used for the business.
  Leave it blank for all of it.

Each line then shows how its GST is treated:

- **Claimable**, with the reason, such as a tax invoice addressed to the company;
- **Claimed for 60%** (or another share): only the business share of the GST is claimed, rounded
  half up to the cent, and the rest stays in the cost;
- **Not claimable**, with the reason: the company is not GST-registered on the bill's date, the code
  is one whose GST is not claimed (such as BL for a motor car or club fees), the supplier is not
  GST-registered, there is no valid tax invoice, or the business share is 0;
- **For the tax agent** (⚠): the rules leave it to the tax agent, and the GST is not claimed until
  they decide.

A simplified tax invoice is claimable while the bill's total, GST included, is at most S$1,000.00:
the limit in the GST pack from 23 Oct 2026. Above it, ask the supplier for a tax invoice. A
simplified invoice dated before 23 Oct 2026 goes to the tax agent, as the pack does not record the
limit before then.

**An invoice in an employee's or director's name.** When they bought something for the company and
were paid back, choose **An employee or director** under **Addressed to**. Bill what the company paid them
back, not the whole invoice, and enter the GST inside that amount: at 9% GST, 9/109 of it. Attach the
invoice and the record of the repayment; until a file is attached the bill shows ⚠. When the company
credited their director's loan instead of paying them back, choose **Credited to their director's
loan**: the GST is not claimed until the tax agent decides.

**The tax agent's decision.** When a line is for the tax agent, record what they decided under
**The tax agent's decision** and attach their advice. On a posted bill, select **Edit**, record the
decision and post the edit: the claim then posts with it.

## Non-resident suppliers

A supplier resident for tax outside Singapore may have tax withheld from what the company pays it,
which the company then pays to IRAS. Its contact says where it is resident: its **Tax residence**,
or its **Country** when that is the same.

1. On such a supplier's bill, choose the **Nature of payment**: what the payment is for, such as
   software for the company's own use or services performed in Singapore. The bill does not post
   without it.
2. Under **Withholding tax**, select **Record the tax agent's decision** and record what the tax agent
   decided for this supplier and nature: **Not applicable** (nothing is withheld, as for an ordinary
   software subscription), **Withhold tax** at the rate they advised, or **Withhold at a reduced
   treaty rate**. Name the **Tax agent** and attach their advice, and for a treaty rate the
   certificate of residence, to the bill.
3. The decision applies to the supplier's bills for that nature from then on, so it is recorded once.
   A new decision replaces it for later bills; bills posted before keep theirs.

Until there is a decision, the bill shows ⚠ and posts without withholding, and its payment waits.
Once tax is withheld, the bill costs the whole amount, owes the supplier what is left after the tax,
and owes the tax to IRAS (2330 Withholding tax payable). The tax is the rate of the payment before
GST, in SGD rounded down to the dollar. If a decision to withhold comes after the bill posted, select
**Edit** on the bill and post the edit: it then withholds, and its payment can be recorded.

## Spread a cost over months

A cost paid ahead, such as a year's software subscription or insurance, belongs to the months it
covers. In Expert mode each line of a bill has **Spread over** (1 to 120 months) and **Spread
from** (the month the service starts; blank for the invoice's month). A line that has a spread
shows both fields in Basic mode too.

- Posting the bill puts the line's cost in **1210 Prepayments**, in SGD, instead of the account it
  is for. The GST is claimed in full at once: only the cost is spread.
- Each month, one part moves from Prepayments to the account the line is for. The parts are even,
  and the cents rounding leaves fall in different months, so they add up exactly to the cost.
- A part posts on the first day of its month, or on the invoice date for the months before it: a
  service that started before the invoice catches up when the bill posts.
- Fyniq posts each part by itself once its date comes, in the company's own time zone, and never a
  month ahead. A part whose month has no open period waits until it opens.
- The bill lists the months under **Spread over months**: posted, due but not posted yet, or
  projected (months to come, which are not in the books).
- Voiding the bill reverses the parts already posted, on the void's date. Editing it reverses them
  on their own dates and posts the new version's.

Only a cost on an expense account is spread, and a credit note's lines never are.

## Recurring bills

A bill that comes every month, quarter or year, such as the office rent, can be set up once under
**Purchases → Bills → Recurring**: select **New recurring bill**, name it, choose how often it
repeats (every month, 3 months, 6 months or year) and on which day, when it starts and, if it
ends, when; then the supplier, the currency and the lines, as on a bill. The 29th to the 31st fall
on the last day of a shorter month.

- Fyniq makes a draft bill on each date by itself, within the hour. Nothing recurring posts by
  itself: open the draft, add **Their invoice number** from the supplier's invoice, check it and
  post it as any bill.
- Whoever wrote a recurring bill's lines does not approve the posting of the drafts made from them.
- **Check** shows the date of the next draft and what would stop the drafts posting, without saving.
  A recurring bill whose lines a bill would refuse is kept, and shows ✖ until it is fixed.
- A change to the supplier, currency, lines or notes applies to the drafts made after it; drafts
  already made keep theirs. **Pause** stops the drafts; **Resume** starts them again from today,
  skipping the dates passed while it was paused.
- At the month's close, Fyniq warns of a recurring bill due in the month whose draft is not made
  yet, and refuses the close when one is more than a day late, as the scheduler may have failed.

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

## Credit notes

When a supplier credits the company (for a seat cancelled, a refund of an overcharge, a returned
item), enter their credit note: select **New credit note** under **Purchases → Bills** and fill it
in as a bill, line by line, with its GST as the credit note states it. Posting it takes it off what
the company owes the supplier, numbered from its own sequence (VC-0001 onwards). Switch the list
to **Credit notes** to see them.

A posted credit note is used in one of two ways:

- **Apply to a bill** settles one of the supplier's open bills in its currency: the bill and the
  credit both go down by the amount. In SGD nothing posts; for a credit note in another currency
  the difference between the two rates is the exchange gain or loss.
- **Record refund** records money the supplier paid back: the date it came in, the account it came
  into and how much. It posts at once, numbered from the refund sequence (RF-0001 onwards).

What a payment paid beyond a bill is a credit with the supplier too, applied or refunded the same
way. A credit note shows **Part applied** or **Applied**, and what is left. Each application or
refund is voided under **Payments**, which makes the credit available again; a credit note is not
voided or edited while it is applied or refunded.

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
| More than the bill has open or the credit has left (CREDIT-05) | Apply or refund at most what is open on both |
| The credit is applied to bills (CREDIT-08) | Void its applications and refunds first |
| The spread cannot be used (LEDGER-28) | Spread an expense line over 1 to 120 months; a credit note's lines are not spread |
| An input-tax fact is not one Fyniq reads (BILL-14) | Choose what the claim rests on and whom it is addressed to from the lists; say how an agent was paid back only for an agent's invoice |
| A line's business share is not a percentage (BILL-15) | Enter the share used for the business from 0 to 100, such as 60, or leave it blank |
| The bill does not say what the payment is for (BILL-16) | Choose the **Nature of payment**: the supplier is resident outside Singapore |
| The bill waits for its withholding tax decision (BILLPAY-11) | Record the tax agent's decision; if it withholds, edit the bill first |
| The withholding decision cannot be recorded (BILL-17) | Record the supplier's tax residence; give a rate only when tax is withheld; name the tax agent |

## Related

- [Purchase orders](purchase-orders.md)
- [Exchange rates](currencies.md)
- [Organisation settings](organisation-settings.md)
- [Contacts and items](contacts-and-items.md)
- [Approvals](approvals.md)
