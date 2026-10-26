# Threat model: expense claims, reimbursements, cash advances and company-card spend

| | |
|---|---|
| Feature and requirement IDs | Members' expense claims and their approval, the expense policy, mileage, reimbursements recorded by hand, cash advances and company-card spend: FR-AP-07, FR-AP-14…18, FR-AP-20, FR-APR-01…03, FM-1, FR-GL-12, FR-DOC-05…08 (PRD-001 rev 0.147 and 0.148; p1-expense-claims design D3…D10) |
| Author, reviewer, date | Claude Code for the founders, 26 Oct 2026; second review at sign-off (waived before production, AGENTS.md) |
| Data and its class | A claim's lines (merchant, amount, category, who attended), its receipts (evidence, `general` unless marked otherwise; card numbers on a receipt are never read into fields), a member's saved places for trips (an address, possibly their home: personal), the money a member was advanced or refunded, and the lines of a company card with its holder. No bank account numbers, payee bank details or secrets |
| Who acts | A member who claims (the Employee's "C (own)" on "Expense claims"); the Approver and the Controller (A on it); the Accountant and the AP specialist recording money paid and advanced (C on "Payment batches"), the Controller approving a void in Founder mode (A there, FM-1); a card's holder completing its lines; the API's database role `fyniq_api` |

## 1. What we are building

A member claims back what they spent for the company: receipts in any currency, and trips in their own vehicle at the
organisation's rate per km (RND-MILEAGE). The expense policy's categories decide the account each line costs, the limit
per line and when a receipt or who attended is needed; the policy runs when the claim is submitted. Another person
approves it through the approval engine, and the Controller too when a line is above its limit or past the claim
deadline; the last approval posts `expense_claim@1` (Dr the expense, input tax when claimable · Cr 2150, or 1250 for what
settles the member's cash advance). Money paid back is recorded by hand under "Payment batches" (`claim_reimbursement@1`,
Dr 2150 · Cr the bank) until payment batches exist; money advanced is `cash_advance@1` (Dr 1250 · Cr the bank) and its
refund the other way. A company card names its holder, who completes each of its lines with the policy's category or as
personal spend, then attaches the receipt.

Trust boundaries: **member ↔ API** (the Appendix D rows, the handlers' "own only" narrowing, step-up for submitting and
approving); **approver ↔ approval engine** (the signed envelope, FR-APR-03); **API ↔ database** (`fyniq_api`, RLS per
organisation, `expense_claim_approved`, `allocation_guard`, `bank_match_posting`, the ledger guards); **browser ↔ the
receipt store** (the evidence upload's checks).

## 2. What can go wrong (STRIDE per boundary)

| # | Boundary or step | Threat (STRIDE) | Impact | Likelihood |
|---|---|---|---|---|
| T1 | member ↔ API | Elevation and repudiation: a member approves their own claim, or a founder holding both rights posts their own claim alone | The company pays a claim nobody checked | Medium |
| T2 | API ↔ database | Tampering: a claim posts without an approved request, or under a request its claimant signed | As T1, past the application's checks | Low |
| T3 | member ↔ API | Tampering: the same receipt claimed twice, on two claims or on a claim and a bill | The company pays twice for one cost | Medium |
| T4 | member ↔ API | Tampering: a foreign-currency line inflated through what the member says left their account, or a rate typed in their favour | The company pays more than was spent | Medium |
| T5 | browser ↔ receipt store | Tampering and information disclosure: a receipt file that is malware, carries location metadata, or is swapped after approval | A compromised device; a member's movements disclosed; evidence that no longer matches what was approved | Low |
| T6 | member ↔ API | Information disclosure: a member reads another member's claims, saved places (a home address), advances or card lines | Personal data and spending disclosed | Medium |
| T7 | approver ↔ approval engine | Tampering: the claim changes between the approver's look and the posting (a line, its total, or the split between the advance and 2150) | The approver signs one claim and another posts | Low |
| T8 | member ↔ API | Tampering: a trip's distance or vehicle inflated, or a rate per km changed by one person | Mileage paid above the trip | Medium |
| T9 | member ↔ API | Tampering: a reimbursement pays more than is owed, pays a claim twice, or pays one member's claim to another; a founder voids one alone to pay again | Money paid out with the books showing it owed or paid once | Low |
| T10 | member ↔ API | Tampering: a claim settles another member's advance, or more than is open on one; personal card spend is settled by a claim; an advance is voided after claims settled it | 1250 and the members' balances apart; personal spend never repaid | Low |
| T11 | member ↔ API | Spoofing and elevation: a member completes a card line of a card they do not hold, or codes card spend to an account outside the expense policy | The card's spend booked by someone who did not make it, or to any account | Low |
| T12 | member ↔ API | Spoofing: the AI or an integration drafts, submits, approves or pays a claim | A posting or payment no person stands behind | Low |
| T13 | member ↔ API | Tampering: a retried request records a reimbursement, an advance or a claim twice | Money recorded twice | Medium |

## 3. What we do about it

| Threat | Control | Test that proves it (`// req:` and `// asvs:`) |
|---|---|---|
| T1 | Submitting asks for another person's approval through the approval engine; the claimant never signs their own claim's request (CLAIM-11), whoever asked; finance approves too on a breach (FR-AP-17); the request's requester and the claimant are refused at signing | `TestExpenseClaimsAreApprovedByAnother` (`// req: FR-AP-17`, `// asvs: V8.2.2`, `V8.3.1`) |
| T2 | `expense_claim_approved` (deferred to commit): a claim posts only under an approved `expense_claim.post` request its claimant asked for and nobody signed as the claimant (FY233) | `TestExpenseClaimsPostThroughThePostingService` (`// req: QA-ENG-05`) |
| T3 | A receipt's file is found by its SHA-256 on the organisation's other records (evidence by hash, migration 000167): the line is blocked until it says why it is not a duplicate (CLAIM-26), and the approver sees the reason; a look-alike receipt of the same date, merchant and amount on another claim shows ⚠ | `TestTheExpensePolicyAtSubmission` (`// req: FR-AP-15`) |
| T4 | What left the member's account and a typed rate are shown to the approver with the line and in the ledger impact; the difference from the rate cascade posts to 7030, visible in the entry; a line above its limit in the base currency needs finance too | `TestPostExpenseClaimInAnotherCurrency`, `TestExpenseClaimsPostThroughThePostingService` (`// req: ENG-40`, `// req: FR-AP-17`) |
| T5 | The evidence upload decides the type from the content, scans it, strips image metadata and keeps the SHA-256 as received and as stored; once the claim posts the file is committed to the retention-locked store and is append-only; only the claimant attaches or removes, and only on a draft | `TestTheExpensePolicyAtSubmission` (the claimant-only rows), `TestEvidenceRefusals`, `TestEvidenceOnAPostedJournalIsCommitted` (the shared upload; `// req: FR-DOC-08`, `// asvs: V5.2.1`, `V5.2.2`) |
| T6 | The handlers narrow every claim, receipt, advance, mileage log and card line to the caller for a person whose right is their own (an Employee): another's answers 404; saved places are their person's only, by the API and RLS (FY234); no address leaves Fyniq (the maps service stays off) | `TestExpenseClaimsAreApprovedByAnother`, `TestClaimPlacesStayTheirPersons`, `TestCashAdvancesSettledByClaims`, `TestCardLinesCompletedByTheirHolder`, `TestAuthorisationMatrix` (`// asvs: V8.2.2`) |
| T7 | The approval envelope holds the claim's version, status and what the company owes, with the split between the advance and 2150; any change voids the request before it executes (APR-04, FR-APR-05) | `TestExpenseClaimsAreApprovedByAnother`, `TestCashAdvancesSettledByClaims` (`// req: FR-APR-03`) |
| T8 | The rate per km is a setting under dual control, effective-dated, read on the trip's date; RND-MILEAGE rounds the distance to 0.1 km; the mileage log shows a member's trips by month to the approver; a trip with no rate is refused (CLAIM-19) | `TestMileageIsClaimedAtTheRate`, `TestMileagePinnedCases`, `TestMileageProperties` (`// req: FR-AP-16`, `// req: RND-MILEAGE`) |
| T9 | A reimbursement locks the claims it pays, pays only the claimant's posted claims, at most what is open on each (CLAIM-13, CLAIM-14); `allocation_guard` lets a reimbursement settle only expense claims (FY171) and never changes an allocation; in Founder mode a void needs a second person (CLAIM-17, FM-1); the 2150 sub-ledger by person is shown against 2150's balance, any difference shown | `TestReimbursementsPayClaimsBack`, `TestExpenseClaimsPostThroughThePostingService` (`// req: FR-AP-07`, `// asvs: V8.2.2`) |
| T10 | A claim settles only a posted advance to its own claimant, at most what is open, with the advance locked while it posts (CLAIM-32); personal spend is refused as a claim's advance; a refund refunds at most what is open (CLAIM-30) and takes If-Match with the advance's ETag; an advance that claims or refunds settled is not voided (CLAIM-31); in Founder mode a void needs a second person (CLAIM-33); `allocation_guard` lets only a claim or a refund settle an advance (FY171) | `TestCashAdvancesSettledByClaims`, `TestCardLinesCompletedByTheirHolder`, `TestExpenseClaimsPostThroughThePostingService` (`// req: FR-AP-17`, `// asvs: V8.2.2`) |
| T11 | A card names one holder, an active member, and only a card has one (BANK-14; the database's check and foreign key); a card line is completed only by its holder (another's answers 404, CLAIM-34 underneath) and only to an active category of the expense policy that is not the mileage one (CLAIM-05) | `TestCardLinesCompletedByTheirHolder`, `TestBankingStoreKeepsWhatTheBankSaid` (`// req: FR-AP-18`, `// asvs: V8.2.2`) |
| T12 | Submitting, approving, recording and voiding need a person signed in: a claim is a person's own (CLAIM-09 for any other principal), approving needs step-up, and the AI holds no write on claims or payments (§9.6); it drafts a claim only through a proposal (Appendix D) | `TestAuthorisationMatrix`, `TestExpenseClaimsAreApprovedByAnother` |
| T13 | Every recording takes an Idempotency-Key: a retry gets the saved answer and records nothing; bank import matches a bank line to money recorded by hand rather than posting it again, and refuses to record money a recorded document already holds | The API conventions' idempotency tests, `TestReimbursementsPayClaimsBack`, `TestCashAdvancesSettledByClaims` |

## 4. What remains

- Claims are routed to someone who approves expense claims, not to the claimant's line manager, until the people records
  give it (the owner's decision of 26 Oct 2026, rev 0.147).
- A reimbursement or an advance recorded by hand is checked against the bank only when its bank line is imported and
  matched; until payment batches exist, the monthly reconciliation of the money account is the check.
- The mileage rates per vehicle are unset until the owner or the accountant sets them; until then a trip cannot be
  submitted.
- Receipt capture through a member's own capture address (inbound email) is not built; receipts are uploaded.
- The independent oracle does not post `expense_claim@1`, `claim_reimbursement@1`, `cash_advance@1` or RND-MILEAGE yet
  (task 9.1, in a session of its own); until it does, the hand-worked tests and the replay are the second check.
