# Tasks

## 1. The claim and its posting

- [x] 1.1 Migrations: the document types `expense_claim`, `claim_reimbursement` and `cash_advance`; claim categories
  (organisation, name, expense account, limits, rules, mileage flag) with the starter set, made with the standard chart;
  RLS, grants and audit triggers; squawk clean; goldens updated. (Saved places move to 4.1, with the mileage they serve.)
  - Tests: the tenancy and grants goldens; a category on a control or money account refused. `// req: FR-AP-15`
- [x] 1.2 `internal/domain/claim`: the draft (receipt lines and the claimant; mileage lines arrive with 4.1 and the
  advance a claim settles with 6.1), read and validated with every problem at once (plain messages with codes, ENG-23);
  `expense_claim@1` in the ledger (design D2): expenses and claimable GST in SGD, Cr 2150 or 1250, foreign-currency
  lines at the rate cascade with the stated SGD and its difference to 7030; `journal.Claims` drafts, checks and posts a
  claim, and the replay reproduces it.
  - Tests: UC-EXP-01's S$27.30 taxi (no GST claimable), a simplified tax invoice with claimable GST, a foreign-currency
    line with and without the SGD paid, a claim settling part of an advance; the entry balances.
    `// req: FR-AP-07`, `// req: ledger/posting-rules Posting an expense claim`,
    `// req: purchases/expense-claims Out-of-pocket receipt claim`

## 2. Submit, approve, return

- [x] 2.1 The API: create, read, list (mine, waiting for me, everyone's for V), change a draft, submit, and the claim's
  approval request with its steps (design D3); approving posts it; rejecting returns it with the comment; resubmitting
  makes a new version. Every operation in the contract first (`make openapi`), with the matrix rows (QA-SEC-03). The
  database posts a claim only under an approved request its claimant asked for and did not sign (FY233).
  - Tests: nobody approves their own claim; in Founder mode the other founder approves; above a limit the Controller's
    step follows (past the deadline arrives with `claims.deadline_days` in 5.1); a returned claim keeps both versions.
    `// req: FR-AP-17`, `// req: FR-AP-14`, `// req: purchases/expense-claims Claim returned for a policy breach`

## 3. Reimbursement and what the company owes

- [x] 3.1 `claim_reimbursement@1` recorded by hand with its allocations under the "Payment batches" row (C; rev 0.148),
  voided while its period is open (in Founder mode on the Controller's approval, FM-1); bank import
  matches it as money recorded by hand (design D7); the 2150 sub-ledger by person, reconciled with 2150 (design D4).
  - Tests: a reimbursement settles two claims; matched from a bank line; My claims' "the company owes you" equals the
    sub-ledger. `// req: FR-AP-07`, `// req: purchases/expense-claims Reimbursement recorded by hand until payment batches`

## 4. Mileage

- [x] 4.1 `RND-MILEAGE` in `internal/domain/rounding`; the mileage rates as effective-dated settings (unset until the
  owner or the accountant sets them; mileage refused with a plain message until then); mileage lines in the claim's
  draft; saved places per person (migration, RLS, grants, audit); the monthly mileage log; mileage lines post with no
  GST (`expense_claim@1` already refuses GST on a mileage line).
  - Tests: UC-EXP-02's 18.4 km return trip at the car rate on the trip date; rounding at the boundaries; a rate changed
    mid-month applies by trip date. `// req: RND-MILEAGE`, `// req: FR-AP-16`, `// req: ledger/posting-rules Mileage claim`,
    `// req: purchases/expense-claims Mileage claim for a client visit`

## 5. The expense policy and duplicates

- [ ] 5.1 Claim categories through the API (A to change, design D6), and the starter set for an organisation that
  loaded its chart before it (`claim.LoadStarter`); `claims.deadline_days` (60), whose breach adds finance's step as a
  line above its limit does (2.1); the checks at
  submission: receipt required ✖, attendees ⚠, above the limit ⚠ with finance's step, past the deadline ⚠ with a
  reason and finance's step; duplicate receipts across people by file hash ✖ until a reason is recorded, look-alikes ⚠.
  - Tests: each breach named on the claim; UC-EXP-04's S$64.80 meal without attendees returned; a receipt claimed by
    another person blocked until its reason. `// req: FR-AP-15`

## 6. Cash advances

- [ ] 6.1 `cash_advance@1` and its refund, recorded under the "Payment batches" row as reimbursements are (rev 0.148); the advance a claim settles in the claim's draft, and the setting
  `claims.advances_account` (1250); a claim settling an advance (`expense_claim@1` credits it already); what is left
  refunded and matched from the bank; ⚠ on an advance unsettled past the deadline (the C.2 #39 line arrives with `p1-checks`).
  - Tests: UC-EXP-03: S$800 advanced, S$742.15 claimed in IDR at the rate cascade, S$57.85 refunded and matched; claims
    above the advance reimburse the excess. `// req: ledger/posting-rules Cash advance to an employee`,
    `// req: purchases/expense-claims Cash advance settled by foreign-currency claims`

## 7. Company-card spend

- [ ] 7.1 A card money account's lines assigned to its cardholder; completed from My claims with the receipt and a
  category (the banking command, the evidence attached); personal spend posts Dr 1250 · Cr the card account.
  - Tests: a card line completed by its cardholder, not by another member; personal spend owed. `// req: FR-AP-18`

## 8. The screens and the guide

- [ ] 8.1 `my-claims` and `personal-claims` (`/me/claims`, `/me`: by status, totals, what the company owes, export;
  the landing for personal-only people), `claim-editor` (receipt from paste, drop or the capture address), and
  `mileage-claim`; help keys and registry entries; component tests with axe.
  `// req: FR-AP-14`, `// req: FR-UX-39`
- [ ] 8.2 `claims-review` (the approver's queue with the policy outcome, approve and return with a comment) and
  `expense-policy` (categories, limits and the settings); component tests with axe. `// req: FR-AP-15`, `// req: FR-AP-17`
- [ ] 8.3 The guide pages `claims.my-claims` and `claims.approve-team` updated to what is built, compiled into
  `kb/pages/`, `make kb-index` and `make kb-lint` clean; the screens checked in the dev stack against the real API.
  `// req: QA-UI-05`

## 9. The oracle

- [ ] 9.1 `expense_claim@1`, `claim_reimbursement@1`, `cash_advance@1` and `RND-MILEAGE` in the independent oracle,
  with scenario shapes and golden cases, in a session of its own (never from a build session).
  - Verified by `make oracle-test` and `make differential` agreeing to the cent. `// req: QA-ENG-01`, `// req: QA-ENG-02`

## 10. Finish

- [ ] 10.1 The threat model `docs/security/threat-models/expense-claims.md`, each abuse case with its test; `make check`,
  `make test`, `make scan` and `make images` pass; a conventional commit with upgrade notes (the mileage rates to set).

## Workflow follow-up

- Before 4.1 is submitted for real: the owner or the accountant confirms the mileage rates per vehicle type.
- Projects on claim lines arrive with `p1-projects`, after go-live; keep-as-loan with `p1-founders-money`; batches with
  `p1-payments-out`.
