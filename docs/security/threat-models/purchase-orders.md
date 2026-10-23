# Threat model: purchase orders

| | |
|---|---|
| Feature and requirement IDs | Purchase orders, billed by quantity and approved as bills are posted: FR-AP-01, FR-APR-01, FM-2, FM-3, SOD-3, RND-LINE (PRD-001 rev 0.87, provisional; the owner's decisions of 23 Oct 2026) |
| Author, reviewer, date | Claude Code for the founders, 23 Oct 2026; second review at sign-off (waived before production, AGENTS.md) |
| Data and its class | Suppliers, accounts, quantities, prices and notes (confidential); no personal data, bank details or secrets |
| Who acts | Whoever holds C on "Bills, vendor credits" (the Bookkeeper, the AP specialist, the Accountant) raises, approves at or below the self-approval threshold, closes and bills orders; above it a member who holds A on that row approves through the approval engine; the API's database role `fyniq_api` |

## 1. What we are building

A purchase order lists what is ordered from a supplier: lines with a quantity, a unit price and a GST code. It posts
nothing. Approving it posts the document with no entry and gives it a number; closing it, with a reason, reverses the
document with no entry. A draft bill is made from an approved order by quantity; each bill line names the order line
and quantity it bills, and the bill posts as any bill does, under the bill's own approvals.

Trust boundaries: **person ↔ API** (Appendix D's row, the threshold, the approval engine with step-up); **API ↔
database** (`fyniq_api`, RLS per organisation, the document lifecycle checks).

## 2. What can go wrong (STRIDE per boundary)

| # | Boundary or step | Threat (STRIDE) | Impact | Likelihood |
|---|---|---|---|---|
| T1 | person ↔ API | Elevation of privilege: a large order is approved by the person who raised it | A commitment to a supplier with no second look | Medium |
| T2 | person ↔ API | Tampering: an order is changed after it was approved, so bills are made of what was not approved | Bills of quantities or prices nobody approved | Low |
| T3 | person ↔ API | Tampering: bills take more of an order than was ordered, or bill a closed or unapproved order | Payables above the approved commitment | Medium |
| T4 | API ↔ database | Tampering: an order posts to the ledger, or is reversed by an entry | Figures in the books from a commitment that is not a liability | Low |
| T5 | person ↔ API | Repudiation: nobody can tell who approved or closed an order, or why | No accountability for commitments | Low |
| T6 | person ↔ API | Spoofing: the AI or an integration approves an order | An approval no person stands behind | Low |

## 3. What we do about it

| Threat | Control | Test that proves it (`// req:`) |
|---|---|---|
| T1 | Above the self-approval threshold the approval is refused (PO-14) and asked for; the approver holds A on the row, is never the requester (FM-1's engine) nor the order's preparer (PO-15), and passes step-up; at or below it, a Founder-mode approval is logged as self-approved for the weekly review (FM-3) | `TestPurchaseOrdersThroughTheAPI` (FM-2, SOD-3) |
| T2 | An approved order is never revised (PO-10); the database refuses a posted document's new version without an entry (FY055); the approval envelope holds the order's version, status and total, so a change to a draft voids a pending request | `TestPurchaseOrdersAreBilledByQuantity`, `TestPurchaseOrdersThroughTheAPI` |
| T3 | Making a bill takes at most what no bill takes yet (PO-12); a bill posts only when each of its order lines names an approved, open order of its supplier and currency and takes no more than posted bills leave (PO-13), checked in the posting's transaction; a credit note never names an order | `TestPurchaseOrdersAreBilledByQuantity` |
| T4 | `journal_entry_not_purchase_order` refuses any entry naming an order (FY173); closing reverses the document with no entry, which `document_reversed_posted` allows for an order alone (FY056) | `TestPurchaseOrdersAreBilledByQuantity` |
| T5 | Approving and closing are audited with the actor and, for a close, the reason; the approver is recorded as who posted the document (FY053) | `TestPurchaseOrdersThroughTheAPI` |
| T6 | Every order operation needs a person or a client holding C or V on the row; the AI holds no write on purchases (§9.6) | `TestAuthorisationMatrix` |

## 4. What remains

- A bill's amount may differ from its order line's price: the three-way match that compares them is P2 (FR-AP-01).
- `RND-LINE` is a new rounding policy, provisional until the accountant's sign-off; the independent oracle implements it
  with the purchase rules (task 10.1).
