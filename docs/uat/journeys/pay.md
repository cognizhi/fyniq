# UAT: pay

- **Journey:** pay (QA-E2E-01)
- **People:** Bea Bookkeeper (Bookkeeper), Cal Accountant (Accountant)
- **Proves:** FR-AP-02 (payments recorded by hand, and the payment state from what is allocated)
- **Organisation:** `make uat-organisation NAME="UAT Pay Pte Ltd" PEOPLE="Bea Bookkeeper:bookkeeper;Cal Accountant:accountant"`

A posted bill is paid from the operating account: the Accountant records the payment that left the bank, the bill is
paid in full, and the payment takes its own number. Payment batches and their file (`p1-payments-out`) have scripts of
their own when they are built.

## Steps

| # | Who | Screen | Do | What to see | Evidence to keep |
|---|---|---|---|---|---|
| 1 | Bea | `bill-editor` | As in the buy script, steps 1 to 3: the supplier "Lumen Print House Pte Ltd" (a vendor), their invoice LPH-0931, one line 6140 Printing and stationery, "Business cards and letterheads", NR, 642.80; save as a draft and attach their invoice. | A draft bill of 642.80. | Screenshot of the draft |
| 2 | Cal | `bill-record` | From Purchases › Bills › Drafts, open it and "Post". | "Posted as BILL-0001." Open 642.80. | Screenshot of the posted bill |
| 3 | Cal | `bill-record` | Select "Record payment…". In "Record a payment of this bill": paid from 1020 OCBC Business Growth, amount paid 642.80, bank reference "OCBC FAST 7781". "Record payment". | The bill shows Paid, open 0.00, and the payment PAY-0001 of 642.80. | Screenshot of the paid bill |
| 4 | Cal | `bill-record` | Record a payment of 700.00 on a second posted bill of 642.80 (enter it as in steps 1 and 2). | The payment is refused or the excess is named: the bill cannot be paid more than it owes without a credit on account. | Screenshot of what Fyniq says |
| 5 | Cal | `bank-account` | Open Banking › the 1020 account's register. | PAY-0001's 642.80 out, with its reference. | Screenshot of the register |
| 6 | Cal | `contact-record` | Open Lumen Print House Pte Ltd. | BILL-0001 paid; the company owes them nothing for it. | Screenshot of the contact |

## Sign-off

| Who | Name | Date | Result |
|---|---|---|---|
| Founder | | | |
| Founder | | | |
| Accountant | | | |
