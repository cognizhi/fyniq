# UAT: sell

- **Journey:** sell (QA-E2E-01)
- **People:** Bea Bookkeeper (Bookkeeper), Cal Accountant (Accountant), Ann Owner (Owner and Controller)
- **Proves:** FR-AR-03 (invoice lines, its approval and ledger states), FR-ORG-04 (the gap-free number on posting),
  FR-ORG-03 (GST follows the registration's dates), FR-APR-01 and FR-APR-02 (the approval steps, and what approving
  would post)
- **Organisation:** `make uat-organisation NAME="UAT Sell Pte Ltd" PEOPLE="Bea Bookkeeper:bookkeeper;Cal Accountant:accountant;Ann Owner:owner,controller"`

A Bookkeeper adds a customer and prepares an invoice of two lines. The Accountant, who issues invoices, finds its total
above the self-approval threshold and asks for approval; the Controller approves it in the Inbox, and it is issued with
the next number. What the customer owes is its total.

## Steps

| # | Who | Screen | Do | What to see | Evidence to keep |
|---|---|---|---|---|---|
| 1 | Bea | `contacts-list` | Open Contacts, select "New contact". Legal name: "Harbourline Logistics Pte Ltd"; tick "A customer"; "Add the contact". | The customer is in Contacts. | Screenshot of the contact |
| 2 | Bea | `invoice-editor` | Open Sales › Invoices › new. Customer: Harbourline Logistics Pte Ltd. Customer's reference: PO-7781. Line 1: "Implementation services, November", quantity 2, unit price before GST 1,500.00, revenue account 4020 Services revenue, GST code SR. "Add a line"; line 2: "Subscription, November", unit price 249.50, 4010 Subscription revenue, SR. "Save as a draft". | "This invoice adds SGD 3,249.50 to what Harbourline Logistics Pte Ltd owes." Net 3,249.50, GST 0.00 (the company is not registered for GST), total 3,249.50. No number yet. There is no Issue button for Bea: the preparer does not issue. | Screenshot of the draft |
| 3 | Cal | `invoices-list` | Open Sales › Invoices, select "Drafts". | The draft, Harbourline Logistics Pte Ltd PO-7781, 3,249.50 SGD. | Screenshot of the list |
| 4 | Cal | `invoice-record` | Open it and select "Issue". | It is "above the self-approval threshold of 2000.00". Select "Ask for approval": "Asked for approval: it is issued when another person approves it." The invoice stays a draft. | Screenshot of the request |
| 5 | Ann | `inbox` | Open the Inbox and the request for Harbourline Logistics Pte Ltd. | The request shows what approving would post: receivable 3,249.50 against 4020 and 4010. | Screenshot of the request |
| 6 | Ann | `approval-confirmation` | Select "Approve…" and confirm. | "Approved: the change is made." | Screenshot of the confirmation |
| 7 | Cal | `invoice-record` | Reload the invoice. | Its heading names INV-0001 (the next number of the sequence); status posted; balance 3,249.50. | Screenshot, and the invoice's PDF downloaded |
| 8 | Cal | `contact-record` | Open Harbourline Logistics Pte Ltd in Contacts. | INV-0001 is listed, and what they owe is 3,249.50. | Screenshot of the contact |
| 9 | Cal | `management-report` | Open the receivables ageing. | INV-0001's 3,249.50 in its age band, under Harbourline Logistics Pte Ltd. | Screenshot of the ageing |

## Sign-off

| Who | Name | Date | Result |
|---|---|---|---|
| Founder | | | |
| Founder | | | |
| Accountant | | | |
