# Threat model: customer deposits

| | |
|---|---|
| Feature and requirement IDs | Deposits and retainers customers pay ahead of their invoices, recorded by hand: FR-AR-09, ENG-45, FM-1, FR-GL-12 (PRD-001 rev 0.126, provisional; `p1-sales` task 3.2 in part; the owner's decisions of 24 Oct 2026) |
| Author, reviewer, date | Claude Code for the founders, 24 Oct 2026; second review at sign-off (waived before production, AGENTS.md) |
| Data and its class | The customer, the money account, the date, the amount, the GST code and the bank's reference (internal); who recorded and who voided it, with the reason. No bank account numbers or secrets |
| Who acts | The AR specialist and the Accountant (P on "Invoices, credit notes") record and void; in Founder mode another member who holds A on that row approves a void; the API's database role `fyniq_api` |

## 1. What we are building

Money a customer has already paid ahead of an invoice is recorded by hand: a part payment, which bears GST when
received once the organisation is GST-registered, or a security deposit, which bears none until it is used. Recording
it posts `customer_deposit@1` at once: Dr the money account, Cr 2410 (role `customer-deposits`) in the base currency at
the rate it came in, Cr 2200 with a part payment's GST. The final invoice deducts it (a later slice); a void reverses it
once nothing uses it.

Trust boundaries: **person ↔ API** (Appendix D's row, the approval engine with step-up for a void); **API ↔ database**
(`fyniq_api`, RLS per organisation, the document and role checks, the posting invariants).

## 2. What can go wrong (STRIDE per boundary)

| # | Boundary or step | Threat (STRIDE) | Impact | Likelihood |
|---|---|---|---|---|
| T1 | person ↔ API | Tampering: a deposit that never came in is recorded, to show money the company does not have, or one that came in is left out | 2410 and the bank in the books apart from the statement until reconciled | Medium |
| T2 | person ↔ API | Tampering: a part payment is recorded as a security deposit, so no GST is accounted for when it is due (ENG-45) | Output tax understated in the period received | Medium |
| T3 | person ↔ API | Repudiation and tampering: one founder voids a deposit alone, to hide money received | Money that came in hidden | Low |
| T4 | API ↔ database | Tampering: 2410 is revalued at month end, changing what an advance is held at | A non-monetary liability moved by exchange rates | Low |
| T5 | person ↔ API | Tampering: a deposit is voided while an invoice deducts it or a refund settles it | An invoice settled, or money paid out, against nothing | Low |
| T6 | person ↔ API | Spoofing: the AI or an integration records or voids a deposit | A posting no person stands behind | Low |
| T7 | person ↔ API | Tampering: a retried request records the deposit twice | Money counted twice | Medium |

## 3. What we do about it

| Threat | Control | Test that proves it (`// req:`) |
|---|---|---|
| T1 | Recording needs P on "Invoices, credit notes" and is audited with the actor and numbered from a gap-free sequence (DP-); the bank reconciliation compares the money account with the statement | `TestAuthorisationMatrix`, `TestCustomerDepositsPostThroughThePostingService` |
| T2 | The kind is required and shown on the deposit; a part payment needs a supply GST code from the pack in force (DEPOSIT-05) and bears GST once registered; a security deposit names none; the kind is in the audit event, and the accountant reviews deposits at the GST return | `TestCustomerDepositsPostThroughThePostingService` |
| T3 | In Founder mode a void is refused (DEPOSIT-10) and asked for; another member who holds A approves it, stepped up, never the requester (FM-1); the envelope holds the deposit's status and amount | `TestCustomerDepositsThroughTheAPI` |
| T4 | `account_revalue_monetary` refuses revaluation on the account with the role `customer-deposits`, as the chart rules do (COA-10) | The migration's check, `chart` rules |
| T5 | A deposit that an invoice deducts or a refund settles is voided only once those are (DEPOSIT-08) | `TestCustomerDepositsPostThroughThePostingService` (with the slices that use deposits) |
| T6 | Recording and voiding need a person or a client with P on the row; the AI's cell is a proposal only, and no proposal task records deposits (§9.6) | `TestAuthorisationMatrix` |
| T7 | `recordCustomerDeposit` takes an Idempotency-Key: a retry gets the saved answer and records nothing; a second record of the same money without the key shows in the bank reconciliation | The API conventions' idempotency tests |

## 4. What remains

- Deducting deposits on the final invoice and refunding them arrive in the rest of task 3.2, with their own controls
  (at most what is left of a deposit, of the invoice's customer and currency).
- A reconciliation of 2410 with the deposits that are left, as INV-07 does for 1100, follows with the deductions.
- The classification of 2410 and the treatment of a foreign-currency deposit's difference at use (7030 or revenue,
  IFRIC 22) wait for the accountant `[verify]`.
