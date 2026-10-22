# Threat model: payments of bills recorded by hand

| | |
|---|---|
| Feature and requirement IDs | Payments of suppliers' bills recorded by hand, and supplier credits applied to bills: FR-AP-02, FR-AP-03, FR-AR-18, ENG-41, INV-05, FM-1, FR-GL-12 (PRD-001 rev 0.77 and 0.81, provisional) |
| Author, reviewer, date | Claude Code for the founders, 23 Oct 2026; second review at sign-off (waived before production, AGENTS.md) |
| Data and its class | The money account a payment left, its date, amounts and the bank's reference (internal); the bills it settles; who recorded and who voided it, with the reason. No bank account numbers, payee bank details or secrets |
| Who acts | The Accountant (P on "Bills, vendor credits") records and voids; in Founder mode another member who holds A on that row approves a void; the API's database role `fyniq_api` |

## 1. What we are building

A payment the company has already made is recorded by hand: the money account, the date, and what it settled of each
of the supplier's posted bills. Recording it posts `bill_payment@1` at once (Dr 2010, Cr the money account, realised FX
to 7030) and writes one allocation per bill, in one transaction with the payment's number. Voiding it reverses the
entry and each allocation, which reopens the bills. No money moves through Fyniq: releasing money is "Payment batches",
which arrives with bank import.

Trust boundaries: **person ↔ API** (the Appendix D row, SEC-02, step-up for an approval); **API ↔ database**
(`fyniq_api`, RLS per organisation; the allocation guard, the base-only line check, the ledger guards).

## 2. What can go wrong (STRIDE per boundary)

| # | Boundary or step | Threat (STRIDE) | Impact | Likelihood |
|---|---|---|---|---|
| T1 | person ↔ API | Tampering: a payment that never happened clears a bill, hiding that it is still owed, or hiding money taken from the account | The payables and the bank balance in the books are wrong until the bank is reconciled | Medium |
| T2 | person ↔ API | Tampering: two payments recorded at once each settle all of a bill, or one settles more than is open | 2010 goes below what is owed; a bill shows paid twice | Low |
| T3 | person ↔ API | Repudiation and tampering: one founder voids a payment alone to reopen a bill and pay it again | A supplier paid twice with the books showing it once | Low |
| T4 | API ↔ database | Tampering: an allocation is changed or deleted, or a void undoes a different amount from what was settled | The bill's open amount and AP stop adding up (INV-07) | Low |
| T5 | API ↔ database | Tampering: the base-only line that clears a bill's residual is used to move a balance without an amount (INV-05) | A base value changed with no transaction behind it | Low |
| T6 | person ↔ API | Spoofing: the AI or an integration records or voids a payment | A posting no person stands behind | Low |
| T7 | person ↔ API | Tampering: a retried request records the payment twice | A bill settled twice and the bank credited twice | Medium |
| T8 | person ↔ API | Tampering (rev 0.81): a credit is applied to a bill beyond what either has open, or applied twice, or the credit or payment behind it is voided while it stands | A bill shown settled by a credit that no longer exists; 2010 and the sub-ledger apart | Low |
| T9 | person ↔ API | Tampering (rev 0.82): a refund recorded that never came in, to clear a credit and hide that the supplier still owes it | A credit shown settled; the bank in the books above the statement until reconciled | Medium |

## 3. What we do about it

| Threat | Control | Test that proves it (`// req:`) |
|---|---|---|
| T1 | Recording needs P on "Bills, vendor credits" and is audited with the actor; the bank reconciliation (FR-BNK) compares the money account with the statement, and bank import will take a recorded payment as a match candidate rather than post again; voids keep the reason | `TestAuthorisationMatrix`, `TestBillPaymentsRecordAndVoidThroughThePostingService` |
| T2 | Recording locks every bill it names, in one order, before reading what is open; a part above what is open is refused (BILLPAY-05); a bill named twice is refused | `TestBillPaymentsRecordAndVoidThroughThePostingService`, `TestBillPaymentsThroughTheAPI` |
| T3 | In Founder mode a void is refused (BILLPAY-10) and asked for; another member who holds A approves it, stepped up, never the requester (FM-1); the envelope holds the payment's status and amount | `TestBillPaymentsThroughTheAPI` |
| T4 | `allocation_guard` refuses any update or delete (FY170) and a reversing allocation that does not negate the one it names exactly, on or after its date, from the same payment (FY171); `allocation_reversed_once` allows one reversal; `fyniq_api` has no UPDATE or DELETE | `TestAttackAllocationsAndBaseOnlyResiduals` |
| T5 | `journal_line_base_only` accepts a base-only line from a bill payment only on the accounts-payable account and at most 1.00 in the base currency (FY008); every other line keeps INV-05's RND-FX check | `TestAttackAllocationsAndBaseOnlyResiduals`, `TestTheLastPartOfABillClearsWhatIsLeft` |
| T6 | Recording and voiding need a person or a client with P on the row; the AI holds no write on bills (§9.6) | `TestAuthorisationMatrix` |
| T7 | `recordBillPayment` takes an Idempotency-Key: a retry gets the saved answer and records nothing; a second record of the same money is still possible without the key, and the bank reconciliation shows it | The API conventions' idempotency tests |
| T8 | An application locks the bill and the credit, refuses more than either has open (CREDIT-05), and records both allocations in one transaction; a vendor credit or a payment whose credit is applied is voided only once its applications are (CREDIT-08); the database reverses an application that posted nothing only with the reversal of its allocations (FY056); the close's INV-07 check sees any posting and document that disagree | `TestCreditsAreAppliedToBills`, `TestCreditApplicationsThroughTheAPI` |
| T9 | Recording a refund needs P and is audited with the actor; it settles at most what is left of the credit (CREDIT-05); the bank reconciliation compares the money account with the statement; a void in Founder mode needs a second person (FM-1) | `TestSuppliersRefundCredits`, `TestSupplierRefundsThroughTheAPI` |

## 4. What remains

- A payment recorded by hand is not checked against the bank until bank import and reconciliation exist (P1, later);
  until then the monthly reconciliation of the money account is the check.
- Clearing the residual of a bill paid in parts with a base-only line is a second exception to INV-05, provisional
  until the accountant's sign-off (rev 0.77).
- A bill from a non-resident supplier is not yet held before payment (FR-AP-19's withholding-tax hold arrives with
  slice 9).
