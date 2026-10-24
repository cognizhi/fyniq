# Threat model: invoices

| | |
|---|---|
| Feature and requirement IDs | Customers' invoices and credit notes saved, issued, voided and edited through the API, and approved as bills are posted: FR-AR-03, FR-AR-04, FR-GL-12, FR-APR-01, FM-1, FM-2, FM-3, SOD-3, FR-DOC-05, FR-DOC-13 (PRD-001 rev 0.117, provisional; `p1-sales` tasks 1.3 and 1.4; credit notes rev 0.123, task 3.1 in part) |
| Author, reviewer, date | Claude Code for the founders, 24 Oct 2026; second review at sign-off (waived before production, AGENTS.md) |
| Data and its class | Customers, accounts, quantities, prices, references, notes and attached files (confidential); a customer's name may name a person; no bank details or secrets |
| Who acts | Whoever holds C on "Invoices, credit notes" (the Bookkeeper, the AR specialist, the Accountant) saves drafts and attaches files; whoever holds P (the AR specialist, the Accountant) issues, voids and edits, at or below the self-approval threshold and outside Founder mode for a void or an edit; above it, or in Founder mode, a member who holds A on that row approves through the approval engine; the API's database role `fyniq_api` |

## 1. What we are building

An invoice is a versioned document: a customer, a date, lines of quantity × unit price less a discount, GST codes and
notes. A draft has no number. Issuing it takes the next number from the invoice sequence and posts its entry under
`invoice@1` (Dr 1100, Cr revenue or 2400, Cr 2200 once GST-registered) in the same transaction. A void reverses the
entry; an edit saves the next version, reverses the old entry and posts the new one. Files attach to a draft and
are committed to the retention-locked store once it is issued.

A credit note to a customer (rev 0.123) is an invoice of kind `credit_note` through the same operations: numbered from
its own sequence (`CN-`) when it is issued, posted under `credit_note@1` (an invoice's legs reversed: Cr 1100, Dr
revenue, Dr 2200), linked to the issued invoice it credits or standalone. A linked credit note adjusts GST at the rates
and the registration of that invoice's date and credits at most its total.

Trust boundaries: **person ↔ API** (Appendix D's row, the threshold, the approval engine with step-up); **API ↔
database** (`fyniq_api`, RLS per organisation, the document lifecycle checks, the posting invariants).

## 2. What can go wrong (STRIDE per boundary)

| # | Boundary or step | Threat (STRIDE) | Impact | Likelihood |
|---|---|---|---|---|
| T1 | person ↔ API | Elevation of privilege: a large invoice is issued by the person who prepared it, with no second look | Revenue and receivables overstated, or a customer billed what nobody approved | Medium |
| T2 | person ↔ API | Tampering: an issued invoice is voided or edited to hide a sale, or to divert a receipt | Revenue understated; a receipt with nothing to settle | Medium |
| T3 | person ↔ API | Tampering: a draft is changed after its issue was asked for, so the approver approves one total and another posts | An approval for figures it never saw | Low |
| T4 | API ↔ database | Tampering: posted history is changed in place, or an entry posts without its document's change | The ledger and the invoices disagree (INV-07 later) | Low |
| T5 | person ↔ API | Repudiation: nobody can tell who issued, voided or edited an invoice, or why | No accountability for revenue | Low |
| T6 | person ↔ API | Spoofing: the AI or an integration issues, voids or edits an invoice | A posting no person stands behind | Low |
| T7 | person ↔ API | Information disclosure: one organisation reads another's invoices or files | Customers and prices leak | Low |
| T8 | person ↔ API | Tampering and elevation of privilege (teeming and lading): a credit note is issued to take a customer's debt off the books, hiding cash received and kept, or giving away revenue | Receivables and revenue understated; money taken | Medium |
| T9 | person ↔ API | Tampering: a credit note names an invoice of another customer, another currency or a smaller total, or one not issued, so GST or a customer's balance is adjusted wrongly | Output tax and a customer's balance misstated | Low |

## 3. What we do about it

| Threat | Control | Test that proves it (`// req:`) |
|---|---|---|
| T1 | Above the self-approval threshold, in the base currency, issuing is refused (INVOICE-10) and asked for; the approver holds A on the row, is never the requester (the engine) nor whoever saved the version (INVOICE-12), and passes step-up; at or below it a Founder-mode issue is logged as self-approved for the weekly review (FM-3) | `TestInvoicesThroughTheAPI` (FM-2, SOD-3) |
| T2 | In Founder mode a void (INVOICE-11) and an edit (INVOICE-14) need a second person; in any mode an edit above the threshold does (INVOICE-10); a void or an edit is refused while receipts or credits settle the invoice (INVOICE-13); a void is a reversal with a reason, and the invoice keeps its number | `TestInvoicesThroughTheAPI` (FM-1), `TestInvoicesPostThroughThePostingService` |
| T3 | The approval envelope holds the invoice's version, status and total receivable, so any change voids the request (FR-APR-05); an edit's content is kept with its SHA-256 in the envelope and checked before it executes | `TestInvoicesThroughTheAPI` |
| T4 | The posting service writes the entry and the document's change in one transaction (ENG-02); the database refuses edits of posted lines (INV-02), unbalanced entries (INV-01), postings into closed periods (INV-03), and a posted document's new version without its entries | `TestInvoicesPostThroughThePostingService`, the invariant attack suites |
| T5 | Every save, issue, void and edit is audited in its transaction with the actor and, for a void or an edit, the reason; the version records who prepared it and the document who posted it | `TestInvoicesThroughTheAPI` |
| T6 | Every invoice operation needs a person or a client holding the row's right; the AI's cell is a proposal only, and no proposal task issues invoices yet (§9.6) | `TestAuthorisationMatrix` |
| T7 | RLS on every table by organisation; a file is found only under its own invoice | `TestEvidenceOnAnInvoice`, the tenancy attack suites |
| T8 | A credit note is judged on its size by the invoice's threshold (INVOICE-10) and approved as an invoice is, never by whoever prepared it (INVOICE-12, SOD-3); at or below it a Founder-mode issue is logged for the weekly review (FM-3) under its own kind; it is audited, numbered from a gap-free sequence, and voided only with a second person in Founder mode (FM-1) | `TestCreditNotesThroughTheAPI` |
| T9 | Issuing refuses a link to a document that is not an issued invoice of the credit note's customer, in its currency, dated on or before it, or to a total below the credit note's (INVOICE-15); an invoice that names an invoice is refused | `TestCreditNotesPostThroughThePostingService`, `TestCreditNotesThroughTheAPI` |

## 4. What remains

- Sending, the online link and its "viewed" tracking arrive with task 5.x and need their own threat model (the link is
  public by design).
- The AR sub-ledger's reconciliation with 1100 (INV-07) arrives with task 1.6; until then a manual journal to 1100 is
  not compared with the invoices.
- The independent oracle posts invoices in its own session (task 9.1).
- Applying credit notes to invoices and refunding them arrive with the rest of task 3.1; until then an issued credit
  note stays a credit with the customer in the AR sub-ledger, and a linked one does not settle its invoice. The
  credit check is one credit note against its invoice's total: several linked credit notes together are bounded when
  they are applied, which never takes more than is open.
- Approval policies for credit notes apart from invoices (FR-APR-01 lists "credit note") follow with the policies per
  amount; until then a credit note takes the invoice's.
