# UAT: buy

- **Journey:** buy (QA-E2E-01)
- **People:** Bea Bookkeeper (Bookkeeper), Cal Accountant (Accountant)
- **Proves:** FR-AP-02 (bill lines, attachments, the bill and its posting), FR-DOC-06 (a bill's evidence), FR-ORG-04
  (the gap-free number on posting)
- **Organisation:** `make uat-organisation NAME="UAT Buy Pte Ltd" PEOPLE="Bea Bookkeeper:bookkeeper;Cal Accountant:accountant"`

A Bookkeeper adds a supplier, enters their bill of two lines as their invoice states them and attaches the invoice;
the Accountant, who posts bills, posts it. It takes the next number, and the company owes the supplier its total.

## Steps

| # | Who | Screen | Do | What to see | Evidence to keep |
|---|---|---|---|---|---|
| 1 | Bea | `contacts-list` | Open Contacts, "New contact". Legal name: "Kopi Cloud Services Pte Ltd"; tick "A vendor"; "Add the contact". | The supplier is in Contacts. | Screenshot of the contact |
| 2 | Bea | `bill-editor` | Open Purchases › Bills › new. Supplier: Kopi Cloud Services Pte Ltd. Their invoice number: KCS-2026-114. Untick "The supplier is GST-registered". Line 1: "What it is for" 6135 SaaS tools, "Team plan, November", GST code NR, amount before GST 1,200.00. "Add a line"; line 2: 5010 Cloud hosting, "Compute, November", NR, 386.40. "Save as a draft". | A draft bill of 1,586.40, with no number yet. No Post button for Bea: the preparer does not post. | Screenshot of the draft |
| 3 | Bea | `evidence-panel` | In the bill's Evidence panel, attach the supplier's invoice (a synthetic PDF, `KCS-2026-114.pdf`) as "invoice". | The file is listed under "Files attached". | Screenshot of the evidence |
| 4 | Cal | `bills-board` | Open Purchases › Bills, select "Drafts". | The draft, Kopi Cloud Services Pte Ltd KCS-2026-114. | Screenshot of the board |
| 5 | Cal | `bill-record` | Open it and select "Post". | "Posted as BILL-0001." Status posted; total and open 1,586.40. | Screenshot of the posted bill |
| 6 | Cal | `contact-record` | Open Kopi Cloud Services Pte Ltd in Contacts. | BILL-0001 is listed; the company owes 1,586.40. | Screenshot of the contact |
| 7 | Cal | `general-ledger` | Open the general ledger for 6135 and 5010, this month. | 1,200.00 on 6135 and 386.40 on 5010, each naming BILL-0001; accounts payable credited 1,586.40. | Screenshot of the ledger |

## Sign-off

| Who | Name | Date | Result |
|---|---|---|---|
| Founder | | | |
| Founder | | | |
| Accountant | | | |
