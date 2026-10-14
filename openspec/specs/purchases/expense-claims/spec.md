# purchases/expense-claims Specification

## Purpose
How employees and directors claim back business spending in Fyniq: the My claims page, the expense policy, receipt and mileage claims, cash advances, reimbursement and company-card spend, with the use cases that show them end to end. Derived from PRD-001 §8.6 and PRD-001 §J.1.

## Requirements

### Requirement: FR-AP-07 Expense claims
The system SHALL let an employee submit expense claims from receipts (mobile), with a category, a project and the GST captured, approved by an approver, posted to 2150 Expense claims payable and reimbursed in a payment batch.

Source: PRD-001 §8.6 · Priority M · Phase P1 (web) / P2 (PWA)

#### Scenario: Employee submits a claim
- **WHEN** an employee submits receipts, from a phone or the web
- **THEN** they select a category and a project and the GST is captured

#### Scenario: Approved claim posts to 2150
- **WHEN** an approver approves a claim
- **THEN** it posts to 2150 Expense claims payable

#### Scenario: Reimbursed in a payment batch
- **GIVEN** an approved claim
- **WHEN** it is reimbursed
- **THEN** it is paid in a payment batch

#### Scenario: Director keeps a claim as a loan
- **WHEN** a director chooses to keep a claim as a loan to the company
- **THEN** it is kept as a loan instead of being reimbursed (FR-EQ-02)

### Requirement: FR-AP-08 Corporate card transactions
The system SHALL import corporate card transactions, by feed or statement, with receipt matching and missing-receipt reminders.

Source: PRD-001 §8.6 · Priority S · Phase P2

#### Scenario: Card transactions imported
- **WHEN** a card feed delivers transactions or a card statement is imported
- **THEN** the card transactions are recorded

#### Scenario: Receipt matching
- **WHEN** a receipt is captured for a card transaction
- **THEN** it is matched to that transaction

#### Scenario: Missing-receipt reminders
- **GIVEN** a card transaction without a receipt
- **WHEN** the reminder schedule runs
- **THEN** the cardholder is reminded to provide the receipt

### Requirement: FR-AP-14 My expense claims
The system SHALL give every user a My claims page (`/me/claims`, "My claims" in the personal section of the navigation) showing the user's claims by status, with totals and what the company owes them, and letting them start, submit, fix and export claims.

Source: PRD-001 §8.6 · Priority M · Phase P1 (web) / P2 (PWA camera)

#### Scenario: Claims by status with totals
- **WHEN** a user opens My claims
- **THEN** their claims are shown by status (draft, submitted, approved, scheduled, paid, returned) with totals
- **AND** the page shows what the company owes them

#### Scenario: A claim starts from a receipt
- **WHEN** a user starts a new claim
- **THEN** they can paste a screenshot of the receipt, drag and drop it, take it with the phone camera in the PWA, or forward it to their own capture address (FR-DOC-12)

#### Scenario: Claim details
- **WHEN** a user fills in a claim
- **THEN** they can set the category, project, GST and currency, split it into lines, and claim mileage at the organisation's rate (setting)

#### Scenario: Foreign-currency claim is reimbursed in SGD
- **WHEN** a claim is in a foreign currency
- **THEN** it is reimbursed in SGD at the rate cascade (ENG-40)

#### Scenario: Policy checks before submission
- **WHEN** a user submits a claim
- **THEN** the policy checks (FR-AP-15) run before it is submitted

#### Scenario: Approver's comments, fix and resubmit
- **GIVEN** an approver returned a claim with comments
- **WHEN** the user opens it in My claims
- **THEN** they see the comments, can fix the claim and resubmit it

#### Scenario: Export history
- **WHEN** a user exports from My claims
- **THEN** they get their claim history

#### Scenario: Directors can keep a payment as a loan
- **WHEN** a director opens My claims
- **THEN** they also see "Paid personally → keep as a loan" (FR-EQ-02)

#### Scenario: Basic-mode page; approvers use the Inbox
- **WHEN** a user is in Basic mode
- **THEN** My claims is available to them
- **AND** approvers handle claims from the Inbox

#### Scenario: Landing for personal-only users
- **GIVEN** a person whose Fyniq rights are personal only
- **WHEN** they sign in
- **THEN** My claims is their landing page, in the personal workspace (FR-UX-39)

### Requirement: FR-AP-15 Expense policy
The system SHALL keep an expense policy with per-category limits, receipt and evidence rules (FR-DOC-06), the mileage rate, a claim deadline, approval routing (FR-APR-01) and duplicate-receipt detection across users, shipped with recommended defaults and configurable (FR-ORG-08).

Source: PRD-001 §8.6 · Priority S · Phase P1

#### Scenario: Claim deadline
- **WHEN** the claim deadline is set
- **THEN** it is a period after the expense, e.g. within 60 days of the expense

#### Scenario: Duplicate receipts across users
- **WHEN** a receipt already claimed or captured by any user is claimed again
- **THEN** the claim is blocked (✖) until a person records why it is not a duplicate (UC-EXP-01)

#### Scenario: Recommended defaults, configurable
- **WHEN** a new organisation starts
- **THEN** the expense policy holds its recommended defaults
- **AND** each setting can be changed (FR-ORG-08)

#### Scenario: Breach shown with the rule named
- **WHEN** a claim breaches the expense policy
- **THEN** the claim shows ⚠ with the rule named

#### Scenario: Above the category limit
- **WHEN** a claim is above its category limit
- **THEN** it also needs the extra approver the policy names

#### Scenario: Past the claim deadline
- **WHEN** a claim is past the deadline
- **THEN** it needs finance's approval with a reason (UC-EXP-02)

### Requirement: FR-AP-16 Mileage claims
The system SHALL record mileage claims with trip date, purpose, project, from and to (an address or a saved place), distance, one-way or return, and vehicle type, and SHALL compute the amount from the organisation's rate per km for the vehicle type.

Source: PRD-001 §8.6 · Priority M · Phase P1

#### Scenario: Distance typed or computed
- **WHEN** a mileage claim is entered
- **THEN** the distance is typed in, or computed by a maps service when the organisation enables one

#### Scenario: Maps service off by default
- **GIVEN** a maps service computes distances
- **WHEN** a distance is computed
- **THEN** the addresses go to that provider
- **AND** the service is therefore off by default and listed in the data inventory

#### Scenario: Amount from the rate per km
- **WHEN** a mileage amount is computed
- **THEN** it is the distance × the organisation's rate per km for the vehicle type, effective-dated (a policy setting, not a statutory rate), rounded with `RND-MILEAGE`

#### Scenario: Reasonable rate and tax treatment
- **WHEN** the rate per km is set
- **THEN** it must be reasonable, as IRAS prescribes no per-km rate
- **AND** reimbursing business mileage in a private car is not taxable, while private mileage is

#### Scenario: No input GST on mileage
- **WHEN** a mileage claim is posted
- **THEN** it claims no input GST, because there is no tax invoice and GST on motor-car expenses is disallowed (GST Regulations, reg. 27)

#### Scenario: Monthly mileage log and optional evidence
- **WHEN** a user has mileage claims in a month
- **THEN** a monthly mileage log is kept for that user
- **AND** a map screenshot can be attached as optional evidence

### Requirement: FR-AP-17 Employee claims processing
The system SHALL process employee claims of four types (receipt, mileage, per diem (C), settlement of a cash advance), route them for approval, reimburse them, and handle cash advances and foreign-currency claims.

Source: PRD-001 §8.6 · Priority M · Phase P1

#### Scenario: Routing by amount and category
- **WHEN** a claim is submitted
- **THEN** it is routed by amount and category to the line manager, then finance (FR-APR-01)
- **AND** in Standard mode nobody approves their own claim

#### Scenario: Reimbursement in a batch or the pay run
- **WHEN** an approved claim is reimbursed
- **THEN** it is paid in a payment batch (FR-BNK-12) or as a non-wage reimbursement line in the pay run

#### Scenario: Tax and CPF treatment of reimbursements
- **WHEN** a reimbursement of actual business expenses incurred for official purposes is paid
- **THEN** it is not taxable and not CPF wages
- **AND** amounts above the actual spend, or above a reasonable mileage rate, are taxable and CPF wages

#### Scenario: Cash advance
- **WHEN** a cash advance is paid to an employee
- **THEN** it posts Dr 1250, Cr Bank
- **AND** it is settled by claims, with the balance refunded, or deducted from salary only under FR-PAY-16

#### Scenario: Foreign-currency claim
- **WHEN** a foreign-currency claim is reimbursed
- **THEN** it is reimbursed in SGD at the rate cascade (ENG-40) and the difference goes to 7030

#### Scenario: Claim deadline applies
- **WHEN** a claim is submitted
- **THEN** the claim deadline of the expense policy (FR-AP-15) applies

#### Scenario: A fixed allowance is not a claim
- **WHEN** an employee receives a fixed allowance
- **THEN** it is taxable pay, goes through payroll and is reported in the IR8A
- **AND** non-taxable reimbursements are not reported

### Requirement: FR-AP-18 Company-card spend
The system SHALL bring company-card lines in by import or feed (FR-AP-08) and let the cardholder attach the receipt, category and project from My claims.

Source: PRD-001 §8.6 · Priority S · Phase P1 (import) / P2 (feed)

#### Scenario: Cardholder completes a card line
- **GIVEN** a company-card line has arrived by import or feed
- **WHEN** the cardholder opens it in My claims
- **THEN** they can attach the receipt and set the category and project

#### Scenario: Personal spend on a company card
- **WHEN** a card line is personal spend
- **THEN** it becomes an amount the employee owes
- **AND** it is repaid, or deducted from salary only under FR-PAY-16

### Requirement: Out-of-pocket receipt claim
An employee's receipt claim SHALL go from a pasted receipt through document capture, policy checks and approval by the line manager then finance into the next payment batch, posting Dr expense · Cr 2150 Expense claims payable and, on payment, Dr 2150 · Cr Bank (UC-EXP-01).

Source: PRD-001 §J.1

#### Scenario: Taxi fare drafted from a pasted receipt
- **GIVEN** an employee took a taxi to a client and has the e-receipt
- **WHEN** she pastes a screenshot of it in My claims
- **THEN** document capture drafts the claim with the merchant, date, S$27.30 and category Local travel
- **AND** the GST is not claimable because the e-receipt shows no GST registration number, so no GST was charged
- **AND** the low-confidence field is highlighted

#### Scenario: Receipt that is a simplified tax invoice
- **WHEN** the receipt shows the operator's GST number and a total up to S$1,000
- **THEN** it is a simplified tax invoice and the GST is claimable (FR-AP-20)

#### Scenario: Submit and approve
- **WHEN** she picks the project "Acme rollout", checks the fields and submits
- **THEN** the policy checks (FR-AP-15) run
- **AND** her line manager approves from the Inbox, then finance approves

#### Scenario: Posting and payment
- **WHEN** the claim is approved and paid
- **THEN** it posts Dr Travel (local) 27.30 · Cr 2150 Expense claims payable, joins the next payment batch, and on payment posts Dr 2150 · Cr Bank
- **AND** the remittance email tells her when it is paid

#### Scenario: Duplicate receipt across users is blocked
- **WHEN** a receipt already claimed by another user is claimed
- **THEN** the claim is blocked

#### Scenario: Claim above the category limit
- **WHEN** a claim is above its category limit
- **THEN** it shows ⚠ with the rule
- **AND** it still needs an extra approver

### Requirement: Mileage claim for a client visit
A mileage claim SHALL take its places, direction, vehicle and distance from the employee, compute the amount at the car rate in force on the trip date with no GST, follow the same approvals as a receipt claim, and post Dr Travel (mileage) · Cr 2150 (UC-EXP-02).

Source: PRD-001 §J.1

#### Scenario: Mileage claim entered
- **GIVEN** an employee drove his own car from the office to a client and back
- **WHEN** in My claims → Mileage he picks the saved places Office and Client HQ, "return" and vehicle "car", and types 18.4 km (or the maps service computes it, if enabled)
- **THEN** the amount is 18.4 × the car rate in force on the trip date (a policy setting), rounded with `RND-MILEAGE`, with no GST

#### Scenario: Submit and post
- **WHEN** he adds the purpose and project and submits
- **THEN** approvals run as for a receipt claim
- **AND** the claim posts Dr Travel (mileage) · Cr 2150, is not taxable and not CPF wages at a reasonable rate, and claims no input GST (FR-AP-16, FR-AP-17)

#### Scenario: Trip older than the claim deadline
- **WHEN** a mileage claim is for a trip older than the claim deadline
- **THEN** it needs finance's approval with a reason

### Requirement: Cash advance settled by foreign-currency claims
A cash advance SHALL post Dr 1250 Staff & director advances · Cr Bank and be settled by the claims the employee marks as settling it, with any balance refunded and any excess reimbursed (UC-EXP-03).

Source: PRD-001 §J.1

#### Scenario: Advance, foreign receipts and settlement
- **GIVEN** finance advances S$800 for a trip to Jakarta, recorded Dr 1250 Staff & director advances · Cr Bank 800
- **WHEN** the employee snaps receipts in IDR during the trip, each line converting at the rate cascade (ENG-40), and on return submits the claims marked as settling the advance
- **THEN** the claims total S$742.15 and post Dr expenses 742.15 · Cr 1250
- **AND** S$57.85 stays on 1250

#### Scenario: Balance refunded by bank transfer
- **WHEN** the employee refunds the S$57.85 by bank transfer
- **THEN** the bank sync matches it and it posts Dr Bank 57.85 · Cr 1250

#### Scenario: Claims exceed the advance
- **WHEN** the claims exceed the advance
- **THEN** the excess is reimbursed in the next payment batch

#### Scenario: Advance unsettled after the claim deadline
- **WHEN** an advance is still unsettled after the claim deadline
- **THEN** it shows ⚠ (check C.2 #39)

### Requirement: Claim returned for a policy breach
An approver SHALL be able to return a claim that breaches the expense policy with a comment, and the employee SHALL be able to fix and resubmit it, with the audit trail keeping both versions (UC-EXP-04).

Source: PRD-001 §J.1

#### Scenario: Meal claim without attendees is returned
- **GIVEN** a policy rule that meals above S$50 need attendees
- **WHEN** a S$64.80 meal claim lacks attendees and the approver returns it with a comment
- **THEN** the employee sees ⚠ and the comment in My claims

#### Scenario: Fixed and resubmitted
- **WHEN** she adds the attendees and resubmits
- **THEN** the audit trail keeps both versions (FR-GL-12)
