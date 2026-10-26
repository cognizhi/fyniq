# Proposal

## Why

People spend the company's money out of their own pockets: a taxi to a client, a meal, software on a personal card,
mileage in their own car, a trip paid from a cash advance. The workbook kept no claims; the founders reimbursed each
other by journal. On 26 Oct 2026 the owner put expense claims in the launch subset (PRD-001 rev 0.146), and its
dependency, `p1-purchases`, has landed (`docs/prd/plans/p1-p3-build-plan-2026-10-16.md`, wave 2).

## What Changes

- **The owner's decisions of 26 Oct 2026, taken interactively (PRD-001 rev 0.146 and 0.147):**
  - **expense claims and payment files join the launch subset**, and **projects follow go-live** to make room for
    them (open question 25); claims therefore carry no project until `p1-projects`;
  - **routing until line managers and ordered policy steps exist:** a claim is approved by one person other than the
    claimant who holds A on "Expense claims" (the other founder in Founder mode, FM-1 and FM-2), and also by finance
    (the Controller) when it is above its category limit or past the claim deadline (FR-AP-17);
  - **reimbursement is recorded by hand** (Dr 2150 · Cr Bank), as a bill payment is, and bank import matches it,
    until payment batches (`p1-payments-out`) add claims (FR-AP-07);
  - **a director's "keep as a loan"** (FR-EQ-02) is built with the director accounts in `p1-founders-money`.
- **My claims (FR-AP-14, FR-UX-39):** every member's `/me/claims`: their claims by status (draft, submitted,
  returned, approved, reimbursed) with totals and what the company owes them; start a claim from a pasted, dropped or
  forwarded receipt (to their own capture address, FR-DOC-12); fix and resubmit a returned claim; export the history.
  A person with personal rights only lands there.
- **The claim (FR-AP-07, FR-AP-17):** receipt lines with category, date, merchant, currency, amount and GST; mileage
  lines; a claim may settle a cash advance. Submitting runs the policy checks and asks for approval; approving posts
  it; returning it, with a comment, sends it back to the claimant with both versions kept.
- **Posting (PRD §10.2, Appendix B):** `expense_claim@1`: Dr each line's expense with its net and Dr the GST control
  with the claimable input tax (FR-AP-20: a tax invoice or a simplified one), Cr 2150 for the claimant, or Cr 1250
  for the part that settles their advance; a foreign-currency line is converted at the rate cascade (ENG-40), and the
  SGD the claimant states they paid, when given, is what the company owes, the difference to 7030.
  `claim_reimbursement@1`: Dr 2150 · Cr Bank. `cash_advance@1`: Dr 1250 · Cr Bank; its refund Dr Bank · Cr 1250.
- **Mileage (FR-AP-16, RND-MILEAGE):** trip date, purpose, from and to (an address or a saved place), distance,
  one way or return, vehicle type; the amount is the distance rounded to 0.1 km times the organisation's rate for the
  vehicle on the trip date, rounded to the cent (`RND-MILEAGE`), with no GST; a monthly mileage log per person. The
  maps service stays off (it is a connector for later).
- **The expense policy (FR-AP-15):** claim categories, each an expense account with a limit per claim and its receipt
  and attendee rules; the claim deadline (60 days by default) and the mileage rates as organisation settings with
  recommended values (FR-ORG-08); a breach shows ⚠ with its rule; above a limit or past the deadline finance
  approves too; a receipt already claimed or captured by anyone is blocked until a person records why it is not a
  duplicate.
- **Cash advances (FR-AP-17, UC-EXP-03):** finance records an advance; the claimant marks claims as settling it; the
  rest is refunded (matched by bank import) or reimbursed; an advance unsettled past the deadline shows ⚠.
- **Company-card spend (FR-AP-18):** card statements come in through banking (a money account of kind card, by import);
  a card line is assigned to its cardholder, who completes it from My claims with the receipt and category; personal
  spend becomes an amount the cardholder owes the company.
- **Reimbursements and the 2150 sub-ledger:** what the company owes each person, from the ledger; reimbursement
  recorded by hand, matched from the bank.
- **Screens:** `my-claims`, `claim-editor`, `mileage-claim`, `claims-review`, `expense-policy` and `personal-claims`,
  with their help keys and the guide pages `claims.my-claims` and `claims.approve-team`, compiled into `kb/`.

## Capabilities

### New Capabilities

None. The requirements are already approved in the main specs (`purchases/expense-claims`, `ledger/posting-rules`,
`ledger/money-and-rounding`); this change builds them (`skip_specs: true`).

### Modified Capabilities

None.

## Impact

- **Requirements built:** FR-AP-07, FR-AP-14, FR-AP-15, FR-AP-16, FR-AP-17, FR-AP-18 (P1: import), RND-MILEAGE, the
  posting rules for expense claims, mileage claims and cash advances, and UC-EXP-01…04.
- **Not built here:** FR-AP-08 corporate card feeds (P2); per diem (C); camera capture in the PWA (P2); drafting a
  claim from the receipt's contents (document capture, `p1-document-capture`, after go-live); project tags
  (`p1-projects`, after go-live); payment batches (`p1-payments-out`); a director's "keep as a loan"
  (`p1-founders-money`); the C.2 #39 check line (`p1-checks`); the maps service.
- **Code:** migrations (claim documents, categories, saved places, the 2150 and 1250 sub-ledgers), `internal/domain`
  (claim, mileage, advance), the posting rules, the API, the web screens, settings, and the independent oracle in a
  session of its own.
- **Estimate:** about eight slices, each its own pull request: the claim and its posting; approval and return;
  reimbursement and the sub-ledger; mileage; the policy and duplicates; cash advances; company-card spend; the screens
  and guide. It displaces `p1-projects`, which follows go-live (open question 25).
- **Security:** money movement and personal data (receipts can show card numbers and names): a threat model at
  `docs/security/threat-models/expense-claims.md`; no self-approval; masking in logs.
