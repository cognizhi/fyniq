# Design

## Context

Bills give the pattern: a document type with a JSON payload and versions (`internal/domain/bill`), a posting rule in
`internal/domain/ledger` run by the posting engine in the document's unit of work, a journal service that creates,
posts, voids and edits through the approval engine (`internal/domain/journal`), payments recorded by hand that settle
documents through allocations, and a sub-ledger read from the documents and reconciled with its control account
(`internal/domain/payables`, `subledger`). A claim differs in four ways: the party is a member, not a contact; every
claim needs another person's approval, not only above a threshold; it may be in several currencies while what the
company owes is in SGD; and it may settle a cash advance on 1250 instead of adding to 2150.

## Goals / Non-Goals

**Goals:** claims end to end in local mode, from a receipt to the reimbursement matched from the bank; the posting
rules of PRD §10.2 for claims, mileage and advances, with the oracle's half in its own session; the policy checks;
the six screens.

**Non-Goals:** payment batches, keep-as-loan, projects, document capture's drafting, card feeds, per diem, the maps
service, and the C.2 #39 check line (each in the change the proposal names).

## Decisions

### D1. Three document types

- `expense_claim`: the claimant (a member's user ID), lines, the advance it settles (optional), and the policy
  outcome (breaches, duplicate overrides with their reasons). Lines are of two kinds:
  - **receipt**: date, merchant, category (a claim category, D6), description, currency, amount, the GST code and
    stated GST, attendees when the category asks for them, and optionally the SGD the claimant paid for a
    foreign-currency line;
  - **mileage**: trip date, purpose, from and to (text, or a saved place), one way or return, vehicle type, distance
    in km (D5).
- `claim_reimbursement`: the claimant, the money account it left, the date, and what it settles of each claim
  (allocations, as a bill payment's), or of an advance's refund in the other direction (D7).
- `cash_advance`: the person, the money account, the date and the amount, in SGD.

Each is versioned like a bill (FR-GL-12): a posted claim is never edited in place; a returned claim is a draft again,
and its earlier version stays in the history (UC-EXP-04).

### D2. Posting rules

- `expense_claim@1`: for each receipt line, Dr its category's expense account with the net and Dr the GST control
  with the claimable input tax, both in SGD; for each mileage line, Dr the mileage category's account with the amount
  and no GST (GST Regulations reg. 27); Cr 2150 Expense claims payable for what the company owes the claimant, or Cr
  1250 Staff & director advances for the part that settles their advance (up to what is open on it).
  - **Input tax** follows `p1-purchases`' eligibility (FR-AP-20): claimable only on a tax invoice or a simplified tax
    invoice (the supplier's GST registration number shown), the organisation GST-registered on the date; otherwise the
    GST is part of the expense.
  - **Foreign currency:** a line in another currency converts at the rate cascade on its date (ENG-40, `RND-FX`). When
    the claimant states the SGD they actually paid, that is what the company owes, and the difference against the
    converted amount goes to 7030 (FR-AP-17).
  - Rounding: `RND-LINE` per line, `RND-GST-EXCL` or the stated GST as bills use it, `RND-MILEAGE` (D5).
- `claim_reimbursement@1`: Dr 2150 · Cr the money account, with allocations to the claims it settles.
- `cash_advance@1`: Dr 1250 · Cr the money account; the refund of what is left, Dr the money account · Cr 1250.

The rules are versioned (`@1`) and named in each entry's posting identity; the independent oracle implements them
from PRD §10.2 in a session of its own (task 9.1), as for sales and purchases.

### D3. Approval: submit asks, approve posts, return sends back (the owner's decision, rev 0.147)

- Submitting a claim validates it (✖ blocks), runs the policy (D6, ⚠ annotates) and creates an approval request for
  `expense_claim` with its steps chosen for this claim:
  - step 1: one person other than the claimant who holds A on "Expense claims" (Approver, Controller); in Founder
    mode the other founder (FM-1);
  - step 2, only when a line is above its category limit or the claim is past the deadline: the Controller
    ("finance"), with the breach's reason recorded at submission.
- The requests already carry ordered steps (`approval.CurrentStep`, `Eligible`); nobody signs two steps of one request,
  and the claimant signs none.
- Approving the last step posts the claim in the same transaction; rejecting it is **return**: the claim goes back to
  draft with the approver's comment, and the claimant fixes and resubmits it (a new request, a new version).
- FM-2's self-approval threshold does not apply: a claim is always approved by someone else (FR-AP-17).

### D4. The 2150 and 1250 sub-ledgers by person

What the company owes each person is read from the documents (posted claims less what reimbursements allocated to
them) and reconciled with 2150, as payables does with 2010. Advances are read the same way against 1250: posted
advances less what claims settled and refunds returned. My claims shows the person's own figures; finance sees
everyone's. A difference between a sub-ledger and its account is shown, never hidden (INV-07's pattern).

### D5. Mileage

`RND-MILEAGE` goes into `internal/domain/rounding` with the other named policies: round(distance, 0.1 km, half-up) ×
rate, then to the cent, half-up, per trip. The distance typed is the whole trip's, as driven, return or not: UC-EXP-02
claims a typed 18.4 km return trip at 18.4 × the rate. (Superseded: "a return trip doubles the distance before
rounding". Doubling a one-way distance belongs to the maps service, which stays off.) The rates per vehicle
type (car, motorcycle) are effective-dated organisation settings (FR-ORG-08), not statutory values; IRAS prescribes no
per-km rate, so the recommended values are confirmed by the owner or the accountant before a mileage claim can be
submitted (until then the setting is unset and mileage is refused with a plain message). Saved places are per person
(an address, never sent anywhere: the maps service stays off). The monthly mileage log is a read of the person's
mileage lines by month.

### D6. The expense policy

- **Claim categories** (a table): name, the expense account (a posting account, not a control or money account), a
  limit per claim line (optional), "receipt required above" (optional), "attendees required above" (optional), and
  whether it is the mileage category. A starter set comes with the standard chart (local and overseas travel, meals and
  entertainment, software and subscriptions, telephone and internet, office supplies, mileage; `claim.Starter`), each
  limit unset until the organisation sets it.
- **Settings** (FR-ORG-08, recommended values): `claims.deadline_days` 60 (FR-AP-15's example); the mileage rates
  (D5).
- **Checks at submission:** a missing receipt where required ✖; attendees missing where required ⚠ and returned by
  the approver if they choose (UC-EXP-04); above the limit ⚠ with the rule named, and finance's step; past the
  deadline ⚠ with the rule named, finance's step, and the claimant's reason required.
- **Duplicate receipts across people (UC-EXP-01):** each receipt file's SHA-256 (evidence already keeps it) is looked
  up across every claim and every captured document of the organisation; a match blocks the claim (✖) until the
  claimant records why it is not a duplicate, which the approver sees. Look-alikes without the same file (same date,
  merchant and amount) show ⚠ only.

### D7. Reimbursement by hand, then batches (the owner's decision, rev 0.147)

A reimbursement is recorded by hand like a bill payment: the person, the money account, the date, the claims it
settles (in full or in part). Bank import matches the transfer to it as money recorded by hand (the banking change's
`recordedTypes` gains `claim_reimbursement` for money out and the advance refund for money in). `p1-payments-out`
later puts approved claims in batches.

### D8. Cash advances

An advance is recorded by finance under Appendix D's "Payment batches" row, as a reimbursement is (D10; the owner's
decision of 26 Oct 2026, rev 0.148, superseding "A on Expense claims"), with its person, amount and money account. A claim marks the advance it settles; the claim's credit goes
to 1250 up to what is open on the advance, and the rest to 2150. What is left on the advance is refunded (a bank line
matched to the refund) or stays open; past the claim deadline it shows ⚠ on the advance (the check line in C.2 #39
arrives with `p1-checks`).

### D9. Company-card spend (FR-AP-18, import)

A card is a money account of kind `card` (banking). Its lines are assigned to a cardholder (a member) on the money
account; the cardholder sees their card lines in My claims and completes each with a receipt and a category, which
categorises the bank line (the banking command) with the evidence attached. A line marked personal spend posts Dr 1250
(the cardholder owes it) · Cr the card account, repaid by bank transfer or, later, through payroll (FR-PAY-16).

### D10. Permissions and personal data

Appendix D's "Expense claims" row: Employee C (own), Approver and Controller A, Owner, Accountant, Bookkeeper, AP,
Auditor and Tax agent V. Nobody holds P there, so the money paid to a member, a reimbursement recorded by hand and a
cash advance with its refund, is recorded under the "Payment batches" row, which the batches take over later: the
Accountant and the AP specialist record it (C), and in Founder mode a void needs the Controller (A, FM-1); whoever
approves a claim is then not who pays it (the owner's decision of 26 Oct 2026, rev 0.148). A member sees and changes only their own claims (row filter and the API's check); approvers
see the claims waiting for them and those they decided. Receipts are evidence of data class `general` unless marked
otherwise; card numbers on receipts are not parsed or stored as fields. The threat model covers self-approval,
duplicate claims, inflated foreign-currency amounts, tampered receipts and cross-person reads.

## Risks / Trade-offs

- **No line managers yet:** an Approver sees every claim; acceptable while the team is the founders (rev 0.147), and
  the steps change when employee records arrive.
- **The mileage rates:** without a confirmed rate, mileage claims wait; the owner or the accountant sets it.
- **Card lines in two places:** a card line is a bank line completed from My claims; the banking queue shows it as
  assigned, so it is not categorised twice.

## Open Questions

- The recommended mileage rates per vehicle type, for the setting's default (the owner or the accountant).
- The starter claim categories' limits, if any, for a founder-only company (none by default).
