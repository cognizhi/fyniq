# Threat model: write-offs of invoices

| | |
|---|---|
| Feature and requirement IDs | Write-offs of what is left of an invoice, reason-coded and approval-gated: FR-AR-17, FR-BNK-08, SOD-9, FM-1, FR-APR-01, FR-GL-12, INV-05, INV-07 (PRD-001 rev 0.122, provisional; `p1-sales` task 2.3) |
| Author, reviewer, date | Claude Code for the founders, 24 Oct 2026; second review at sign-off (waived before production, AGENTS.md) |
| Data and its class | The invoice, the amount, the reason, the account and the explanation (confidential); who prepared, asked, approved and voided it, with the reasons. No bank details or secrets |
| Who acts | Whoever holds C on "Invoices, credit notes" drafts; whoever holds P asks for its post or voids it; a member who holds A on that row approves, stepped up; the API's database role `fyniq_api` |

## 1. What we are building

A write-off clears what is left of an issued invoice for one reason (an agreed discount, bank charges, a bad debt, a
cross-currency difference, a migrated residual) to the reason's account. It is drafted, then posts only when another
person approves it: Cr 1100 at the invoice's stored rate, Dr the reason's account, and an allocation that settles the
invoice. Voiding it reverses the entry and the allocation, which reopens the invoice.

Trust boundaries: **person ↔ API** (the Appendix D row, the approval engine with step-up); **API ↔ database**
(`fyniq_api`, RLS per organisation; the allocation guard, the base-only line check, the ledger guards).

## 2. What can go wrong (STRIDE per boundary)

| # | Boundary or step | Threat (STRIDE) | Impact | Likelihood |
|---|---|---|---|---|
| T1 | person ↔ API | Elevation of privilege and tampering (teeming and lading): one person receives a customer's cash, keeps it, and writes the invoice off to hide it | Money taken with the books showing a bad debt | Medium |
| T2 | person ↔ API | Tampering: a write-off clears more than is open, or an invoice that is not issued | 1100 below what is owed; receivables understated | Low |
| T3 | person ↔ API | Tampering: the write-off changes after it is approved, so a larger amount or another account posts | An approval for figures it never saw | Low |
| T4 | person ↔ API | Repudiation: nobody can tell why an invoice was written off or who approved it | No accountability for revenue given up | Low |
| T5 | API ↔ database | Tampering: the residual line clears more than a cent's worth without an amount (INV-05) | A base value changed with no transaction behind it | Low |
| T6 | person ↔ API | Spoofing: the AI or an integration writes off an invoice | A write-off no person stands behind | Low |
| T7 | person ↔ API | Tampering: a write-off is voided alone to reopen an invoice and pursue the customer again | A customer asked to pay twice | Low |

## 3. What we do about it

| Threat | Control | Test that proves it (`// req:`) |
|---|---|---|
| T1 | Every write-off posts only through an approval, in any team mode; the approver holds A on the row, is never the requester (the engine), the write-off's preparer, or anyone who recorded a receipt of the invoice that still settles it (WRITEOFF-11, SOD-9), and passes step-up | `TestWriteOffsThroughTheAPI` (SOD-9) |
| T2 | The write-off locks the invoice before reading what is open; more than is open is refused (WRITEOFF-04); an invoice that is not issued is refused (WRITEOFF-03); the allocation guard accepts an allocation from a write-off to an invoice only (FY171) | `TestWriteOffsSettleWhatIsLeft` |
| T3 | The approval envelope holds the write-off's version, status and carrying amount, so any change voids the request (FR-APR-05); posting reads the version the approval was for | `TestWriteOffsThroughTheAPI` |
| T4 | The explanation is required (WRITEOFF-12) and kept with the reason; the request, the decision and the posting are audited with their actors; a bad debt is tagged for the relief review | `TestWriteOffsThroughTheAPI` |
| T5 | `journal_line_base_only` accepts a base-only line from a write-off only on the accounts-receivable account and at most 1.00 in the base currency (FY008) | `TestWriteOffsSettleWhatIsLeft` (the cent's residual) |
| T6 | The AI's cell on the row is a proposal only, and no proposal task writes off (§9.6); every operation needs a person or a client with the row's right | `TestAuthorisationMatrix` |
| T7 | In Founder mode a void is refused (WRITEOFF-10) and asked for; another person approves it, never the requester (FM-1) | `TestWriteOffsThroughTheAPI` |

## 4. What remains

- Evidence files on a write-off (the liquidator's notice, the bank's advice) wait for the evidence rules per activity
  (FR-DOC-06); until then they are attached to the invoice and the explanation names them.
- The reasons' accounts are provisional: a discount to 4095 does not adjust its GST (a credit note does), and a bad
  debt names its account until the chart-of-accounts review assigns one `[verify]`.
- Write-offs above an amount that need a second approver (question 32's last part) follow with the approval policies
  per amount; until then every write-off needs one approver other than the people above.
