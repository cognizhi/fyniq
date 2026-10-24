# Threat model: customer receipts recorded by hand

| | |
|---|---|
| Feature and requirement IDs | Money customers paid, recorded by hand against their invoices: FR-AR-18, ENG-41, INV-05, INV-07, FM-1, FR-GL-12 (PRD-001 rev 0.120, provisional; `p1-sales` task 2.1; the owner's decision of 24 Oct 2026) |
| Author, reviewer, date | Claude Code for the founders, 24 Oct 2026; second review at sign-off (waived before production, AGENTS.md) |
| Data and its class | The money account a receipt came into, its date, amounts and the bank's reference (internal); the invoices it settles; who recorded and who voided it, with the reason. No bank account numbers or secrets |
| Who acts | The AR specialist and the Accountant (P on "Invoices, credit notes") record and void; in Founder mode another member who holds A on that row approves a void; the API's database role `fyniq_api` |

## 1. What we are building

Money a customer has already paid is recorded by hand: the money account, the date, and what it settled of each of the
customer's issued invoices. Recording it posts `customer_receipt@1` at once (Cr 1100, Dr the money account, realised FX
to 7030) and writes one allocation per invoice, in one transaction with the receipt's number; what came in beyond the
invoices is a credit with the customer. Voiding it reverses the entry and each allocation, which reopens the invoices.
No money moves through Fyniq; bank import will match statement lines to recorded receipts.

Trust boundaries: **person ↔ API** (the Appendix D row, SEC-02, step-up for an approval); **API ↔ database**
(`fyniq_api`, RLS per organisation; the allocation guard, the base-only line check, the ledger guards).

## 2. What can go wrong (STRIDE per boundary)

| # | Boundary or step | Threat (STRIDE) | Impact | Likelihood |
|---|---|---|---|---|
| T1 | person ↔ API | Tampering: a receipt that never came in clears an invoice, hiding that the customer still owes it, or hiding money taken | Receivables understated and the bank in the books above the statement until reconciled | Medium |
| T2 | person ↔ API | Tampering: two receipts recorded at once each settle all of an invoice, or one settles more than is open | 1100 below what is owed; an invoice shown paid twice | Low |
| T3 | person ↔ API | Repudiation and tampering: one founder voids a receipt alone to reopen an invoice and pursue the customer again, or to hide a receipt | A customer asked to pay twice, or money that came in hidden | Low |
| T4 | API ↔ database | Tampering: an allocation is changed or deleted, or settles an invoice from a document that is not a receipt | The invoice's open amount and 1100 stop adding up (INV-07) | Low |
| T5 | API ↔ database | Tampering: the base-only line that clears an invoice's residual moves a balance without an amount (INV-05) | A base value changed with no transaction behind it | Low |
| T6 | person ↔ API | Spoofing: the AI or an integration records or voids a receipt | A posting no person stands behind | Low |
| T7 | person ↔ API | Tampering: a retried request records the receipt twice | An invoice settled twice and the bank debited twice | Medium |
| T8 | person ↔ API | Tampering: an invoice is voided or edited while a receipt settles it, leaving the receipt settling nothing | Money received with no invoice behind it; 1100 and the sub-ledger apart | Low |

## 3. What we do about it

| Threat | Control | Test that proves it (`// req:`) |
|---|---|---|
| T1 | Recording needs P on "Invoices, credit notes" and is audited with the actor; the bank reconciliation compares the money account with the statement; voids keep the reason | `TestAuthorisationMatrix`, `TestCustomerReceiptsSettleInvoices` |
| T2 | Recording locks every invoice it names, in one order, before reading what is open; a part above what is open is refused (RECEIPT-05); an invoice named twice, another customer's, or one in another currency is refused (RECEIPT-04) | `TestCustomerReceiptsSettleInvoices`, `TestCustomerReceiptsThroughTheAPI` |
| T3 | In Founder mode a void is refused (RECEIPT-10) and asked for; another member who holds A approves it, stepped up, never the requester (FM-1); the envelope holds the receipt's status and amount | `TestCustomerReceiptsThroughTheAPI` |
| T4 | `allocation_guard` refuses any update or delete (FY170), an allocation to an invoice from anything but a customer receipt, and a reversing allocation that does not negate the one it names exactly (FY171); `fyniq_api` has no UPDATE or DELETE | `TestCustomerReceiptsSettleInvoices`, `TestAttackAllocationsAndBaseOnlyResiduals` |
| T5 | `journal_line_base_only` accepts a base-only line from a customer receipt only on the accounts-receivable account and at most 1.00 in the base currency (FY008) | `TestPostCustomerReceiptRealisesFX` (the rule), the migration's check |
| T6 | Recording and voiding need a person or a client with P on the row; the AI's cell is a proposal only, and no proposal task records receipts (§9.6) | `TestAuthorisationMatrix` |
| T7 | `recordCustomerReceipt` takes an Idempotency-Key: a retry gets the saved answer and records nothing; a second record of the same money is still possible without the key, and the bank reconciliation shows it | The API conventions' idempotency tests, `TestCustomerReceiptsThroughTheAPI` |
| T8 | An invoice receipts settle is voided or edited only once they are voided (INVOICE-13); the close's INV-07 check sees any posting and document that disagree | `TestCustomerReceiptsSettleInvoices`, `TestCustomerReceiptsThroughTheAPI` |

## 4. What remains

- A receipt recorded by hand is not checked against the bank until bank import and reconciliation exist; until then the
  monthly reconciliation of the money account is the check.
- Clearing the residual of an invoice received in parts with a base-only line is a further exception to INV-05,
  provisional until the accountant's sign-off, as it is for bills (rev 0.77).
- Applying a customer's credit to a later invoice, and refunding it, arrive with credit notes (task 3.1); until then a
  receipt whose credit is applied cannot exist, so its void needs no such check.
