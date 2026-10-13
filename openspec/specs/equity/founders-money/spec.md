# equity/founders-money Specification

## Purpose
How Fyniq records what the company owes its founders, from the first receipt to the share allotment: director accounts, costs paid personally, founder pay choices, the settlement routes (repay, keep, convert to shares, waive, convert to a SAFE or note), the ACRA steps of a conversion, presentation, overdrawn director accounts and the year-end confirmation, with the evidence the accountant, the tax agent and investors will ask for. Derived from PRD-001 §8.22.

## Requirements

### Requirement: Amounts owed to founders and their settlement routes
An amount the company owes a founder, whatever it started as (money lent, a cost paid personally, unpaid pay, a fee), SHALL be a liability to a related party, settled by one of five routes. The routes' tax effects are general information, not tax or legal advice: the accountant and the tax agent confirm a route before it is used.

Source: PRD-001 §8.22

#### Scenario: Any origin is a related-party liability
- **WHEN** the company owes a founder money lent, a cost paid personally, unpaid pay or a fee
- **THEN** the amount is recorded as a liability to a related party

#### Scenario: Repay
- **WHEN** a founder's balance is repaid in cash
- **THEN** the posting is Dr 2500 / Cr Bank
- **AND** it is not income to the founder (repaying a loan or reimbursing the company's own cost), the company deducts the underlying costs as usual, and the receipts are in the company's name for the GST claim

#### Scenario: Keep as a loan
- **WHEN** a founder's balance is kept as a loan
- **THEN** nothing is posted and 2500 stays
- **AND** the balance is interest-free and repayable on demand unless agreed otherwise, with the terms in a short loan note or board minute
- **AND** there is no tax or CPF effect for the founder, and no tax effect for the company, while it stays a loan

#### Scenario: Convert to shares
- **WHEN** a founder's balance is converted to shares
- **THEN** the posting is Dr 2500 (or 2120) / Cr 3010 or 3020 share capital, the account of the class allotted (B.7, FR-EQ-06), with no cash
- **AND** the company gets no deduction for issuing shares and has no gain or loss when the founder converts as a shareholder
- **AND** the founder has no income when the debt was a loan or a reimbursement; when it was pay, the pay was already taxed when due, and shares worth more than the debt may be a further gain [verify]

#### Scenario: Conversion paperwork and the date shares exist
- **WHEN** a conversion to shares is prepared
- **THEN** it needs the shareholders' approval (a written resolution is allowed) lodged with ACRA within 14 days, then the return of allotment with the written conversion agreement
- **AND** the shares exist only when ACRA updates the register of members, a date that cannot be backdated

#### Scenario: Waive
- **WHEN** a founder waives their balance as a shareholder and receives nothing
- **THEN** the posting is Dr 2500 / Cr 3300 capital contribution, with a deed of waiver or written resolution
- **AND** it has no tax effect for the founder; for the company a waived non-trade loan is generally capital (IRAS ruled so for one applicant in 2026), while waiving an amount the company deducted earlier (pay, fees) may be taxable [verify]

#### Scenario: Convert to a SAFE or convertible note
- **WHEN** a founder's balance is converted to a SAFE or convertible note
- **THEN** the posting is Dr 2500 / Cr 2700
- **AND** the paperwork is the instrument's terms, and the instrument appears in the cap table (FR-EQ-18, FR-EQ-20)
- **AND** the tax effects are as for a loan, for the founder and for the company

#### Scenario: How investors tend to see each route
- **WHEN** the routes are explained to the founders
- **THEN** the explanation says that repaying is fine while small and rarely done from a new round's money; a loan is disclosed in due diligence and often converted or waived as a condition of the round; converting to shares is preferred, a clean balance sheet before investors' money comes in; waiving is simple and every shareholder gains pro rata; a SAFE or note is common at seed and adds to the dilution the round must model
- **AND** it says this practice comes from US and UK sources and Singapore-specific practice is not confirmed

#### Scenario: Grant money is not used to repay founders
- **WHEN** a repayment to a founder is planned
- **THEN** the Startup SG Founder grant (S$20k–S$50k, matched 1:1 by the founders' paid-up capital) is not used to pay off loans from the founders
- **AND** whether a capitalised founder loan counts as the founders' capital injection is left for Enterprise Singapore to confirm [verify]

### Requirement: Founder pay choices and their effects
Founder pay SHALL be decided per director before the period, because the tax and CPF effects follow the choice, not the cash: no salary, salary paid, salary deferred, director's fee, or shares instead of pay.

Source: PRD-001 §8.22

#### Scenario: No salary
- **GIVEN** a director's choice of no salary, agreed in writing before the period
- **WHEN** the period is processed
- **THEN** there is nothing to post, no CPF and nothing to convert later
- **AND** it is the usual and cleanest choice while the company cannot pay

#### Scenario: Salary paid
- **GIVEN** a director's choice of salary paid, when cash allows
- **WHEN** the period is processed
- **THEN** the director is paid in a normal pay run (§8.7)

#### Scenario: Salary deferred
- **GIVEN** a director's choice of salary deferred
- **WHEN** the pay run posts
- **THEN** it posts as usual (salary expense, CPF, SDL) and the net pay moves to 2120 Deferred remuneration payable instead of the bank
- **AND** CPF falls due at the month end on wages due, paid or not (paid by the 14th of the next month to avoid interest of 1.5% a month), and the founder is taxed on the salary in the year it is due, paid or not [verify CPF for unpaid director salary]
- **AND** it fits only when the founders accept paying CPF and tax on money not yet received, to repay or convert later

#### Scenario: Director's fee
- **GIVEN** a director's fee approved by the shareholders
- **WHEN** the fee is recorded
- **THEN** it posts Dr 6050 / Cr 2120 until paid
- **AND** it is taxed in the year the director becomes entitled (for fees approved in arrears, the date of the meeting that votes them), with no CPF on fees voted at a general meeting
- **AND** it fits directors who are not employees, or a deliberate split between fee and salary

#### Scenario: Shares instead of pay
- **GIVEN** a director takes shares instead of pay ("sweat equity")
- **WHEN** it is recorded
- **THEN** it is an equity-settled share-based payment measured at the shares' grant-date fair value [verify valuation basis]
- **AND** it is employment income to the founder, taxed without cash received, with no CPF on shares in kind and no tax deduction for the company for newly issued shares
- **AND** it is rarely sensible before a priced round and needs a valuation

### Requirement: FR-EQ-01 Director accounts
The system SHALL keep one sub-ledger per director or shareholder on 2500 Loan from director / shareholder (and on 2120 Deferred remuneration payable), linked to the person's contact and, where employed, the employee record.

Source: PRD-001 §8.22 · Priority M · Phase P1

#### Scenario: Sub-ledger linked to the person
- **WHEN** a director or shareholder has a balance on 2500 or 2120
- **THEN** it is held in that person's sub-ledger, linked to their contact and, where they are employed, their employee record

#### Scenario: Every movement records its origin
- **WHEN** a movement is posted to a director account
- **THEN** it records its origin (cash lent, cost paid personally, pay, fee), because the tax and accounting result depends on it

#### Scenario: Statement per person
- **WHEN** a person's statement is produced
- **THEN** it shows the opening balance, movements by origin, repayments, conversions, waivers and the closing balance
- **AND** it can be exported as a PDF for balance confirmations

### Requirement: FR-EQ-02 "Paid personally"
When the claimant on an expense claim (FR-AP-07) is a director, the system SHALL let each claim settle either to 2150 (reimburse) or to the director's account on 2500 (keep as a loan).

Source: PRD-001 §8.22 · Priority M · Phase P1

#### Scenario: Reimburse or keep as a loan
- **GIVEN** a director's expense claim marked "Paid personally"
- **WHEN** the claim is settled
- **THEN** it settles either to 2150 (reimburse) or to the director's account on 2500 (keep as a loan)

#### Scenario: Receipt and business purpose
- **WHEN** a director submits a claim paid personally
- **THEN** a receipt and a business purpose are required, as for any claim

#### Scenario: Input GST only when eligible
- **WHEN** a director's claim line carries GST
- **THEN** input GST is claimed only when the line meets the input-tax eligibility rule (FR-AP-20): a tax invoice addressed to the company, a simplified tax invoice (up to S$1,000), or an invoice in a founder's name when the founder acted as the company's agent
- **AND** otherwise the GST stays in the cost

#### Scenario: Another founder approves
- **WHEN** a director's claim is approved
- **THEN** another founder approves it (FM-2/FM-3 apply below the self-approval threshold)

### Requirement: FR-EQ-03 Costs before incorporation or before GST registration
The system SHALL record costs from before incorporation or before GST registration with their original date, flag them for the accountant's decision, and post them at the books start or the adoption date.

Source: PRD-001 §8.22 · Priority S · Phase P1

#### Scenario: Recorded with the original date and flagged
- **WHEN** a cost from before incorporation or GST registration is recorded
- **THEN** it keeps its original date and is flagged for the accountant's decision
- **AND** it posts at the books start or the adoption date

#### Scenario: Income tax treatment
- **WHEN** the accountant decides the income-tax treatment
- **THEN** revenue costs from up to 12 months before the deemed start of business are deductible (Income Tax Act s14R), incorporation costs are capital, and the treatment of founders' revenue costs from before incorporation is unconfirmed [verify]

#### Scenario: Pre-incorporation input tax
- **WHEN** input tax from before incorporation is considered
- **THEN** it is claimable in the first GST return with IRAS's approval when the payer became a member, officer or employee, was reimbursed (or holds a written undertaking), was not GST-registered and bought only for the business
- **AND** the pre-registration rules hold (services within 6 months; goods still held)

### Requirement: FR-EQ-04 Founder pay choice
The system SHALL record a founder pay choice per director per period (no salary, salary paid, salary deferred, director's fee, shares instead of pay), effective-dated and stored with the signed decision, and the pay run SHALL apply it.

Source: PRD-001 §8.22 · Priority M · Phase P1 (with payroll)

#### Scenario: Choice recorded with the signed decision
- **WHEN** a director's pay choice is recorded for a period
- **THEN** it is effective-dated and stored with the signed decision

#### Scenario: Shares instead of pay
- **GIVEN** a director whose choice is shares instead of pay (the fifth choice of §8.22)
- **WHEN** the choice is applied
- **THEN** it is recorded as an equity transaction at the shares' grant-date fair value with its valuation evidence (FR-EQ-17) [verify the valuation basis with the accountant]

#### Scenario: Deferred pay in the pay run
- **GIVEN** a director whose choice for the period is salary deferred
- **WHEN** the pay run posts
- **THEN** that person's deferred net pay goes to 2120
- **AND** the run shows in plain words the CPF, SDL and income tax that still fall due

#### Scenario: Deferral without a recorded choice is blocked
- **GIVEN** no recorded pay choice for a director
- **WHEN** the pay run would defer that director's pay
- **THEN** the deferral cannot post until fixed

#### Scenario: Default for new directors
- **WHEN** a new director is added
- **THEN** their pay choice defaults to "No salary" (§20 recommended default 16; FR-ORG-08)

### Requirement: FR-EQ-05 Settlement routes
The system SHALL offer settlement routes for a director's balance on 2500 or 2120, each an approval object (FR-APR-01) that shows its ledger impact before approval (FR-APR-02), with FM-9 applying.

Source: PRD-001 §8.22 · Priority M · Phase P1

#### Scenario: Routes offered
- **WHEN** a director's balance is to be settled
- **THEN** the routes are repay (payment batch; FM-1), keep (terms: interest, repayment date, subordination), convert to shares (FR-EQ-06), waive (FR-EQ-07), or convert to a SAFE or convertible note (2700; FR-EQ-18)

#### Scenario: Ledger impact before approval
- **WHEN** a settlement route is submitted for approval
- **THEN** it shows its ledger impact before it is approved (FR-APR-02)
- **AND** FM-9 applies to its approval

#### Scenario: Default route and price basis
- **WHEN** a settlement is started without other choices
- **THEN** the route defaults to "keep as a loan" and the price basis to "latest priced round", both organisation settings (§20 recommended default 15; FR-ORG-08)

### Requirement: FR-EQ-06 Conversion to share capital
The system SHALL convert a director's balance to share capital in two steps, approve and then effective, posting only on the effective date and never backdating.

Source: PRD-001 §8.22 · Priority M · Phase P1

#### Scenario: Approve the conversion
- **WHEN** a conversion is set up for approval
- **THEN** the user chooses the person, the balance and amount, the share class, and the price per share with its basis (latest priced round, independent valuation, or a price every shareholder approves)
- **AND** the shares are computed with `RND-SHARE` and the remainder stays on the director's account

#### Scenario: Approval documents and capacity
- **WHEN** a conversion is approved
- **THEN** the shareholders' approval (Companies Act s161; a private company may use a written resolution, s184A), the board resolution, the written conversion agreement and the founder's signed balance confirmation are attached
- **AND** the founder's capacity (lender, employee or shareholder) is recorded

#### Scenario: Price basis protects the other shareholders
- **WHEN** a conversion is approved
- **THEN** its price basis is recorded and the shareholders' approval is attached, because a price away from the latest round or a valuation moves value between shareholders and shares issued without the shareholders' approval are void

#### Scenario: The conversion becomes effective
- **WHEN** the lodgement reference and the effective date are recorded, the effective date being the filing date once ACRA's electronic register of members shows the allotment (FR-EQ-09)
- **THEN** the entry posts on that date, never backdated: Dr 2500/2120 converted amount, Cr 3010 or 3020

#### Scenario: Pending until registered
- **GIVEN** an approved conversion that ACRA has not yet registered
- **WHEN** the balance sheet or the director's account is viewed
- **THEN** the balance stays a liability and the conversion shows as pending

#### Scenario: Registration after the year end
- **GIVEN** a conversion approved before the year end that ACRA registers after it
- **WHEN** that year's balance sheet is produced
- **THEN** the balance is still a liability, since a conversion shows in a year's balance sheet only when registered before the year end

#### Scenario: Measurement basis
- **WHEN** the accountant picks the measurement basis
- **THEN** it is the carrying amount (the default, when the founder converts as a shareholder) or the shares' fair value with the difference in profit or loss (IFRIC 19 ¶9 and its Singapore equivalents; SFRS for Small Entities ¶22.15A–C)
- **AND** a basis other than the default needs a note

### Requirement: FR-EQ-07 Waiver
The system SHALL post a waiver of a director's balance as Dr 2500/2120 and, as the accountant chooses with a reason, Cr 3300 Capital contribution (equity) or Cr other income.

Source: PRD-001 §8.22 · Priority M · Phase P1

#### Scenario: Waiver as a shareholder
- **WHEN** a founder waives their balance as a shareholder
- **THEN** the waiver posts Dr 2500/2120, Cr 3300 Capital contribution (equity)

#### Scenario: Waiver in another capacity
- **WHEN** a founder waives their balance other than as a shareholder
- **THEN** the waiver posts Dr 2500/2120, Cr other income

#### Scenario: The accountant chooses with a reason
- **WHEN** a waiver is posted
- **THEN** the credit account is the accountant's choice, recorded with a reason

#### Scenario: Previously deducted amounts tagged for tax
- **WHEN** a waived amount was deducted by the company earlier (pay, fees)
- **THEN** it is tagged for the tax computation [verify taxability]

### Requirement: FR-EQ-08 Register of share issues
The system SHALL keep a register of share issues with every allotment, for cash or not; it is the issues part of the equity register (§8.23), from which the cap table is computed.

Source: PRD-001 §8.22 · Priority M · Phase P1

#### Scenario: What an allotment records
- **WHEN** an allotment is recorded
- **THEN** it holds the holder, class, number, price, consideration type (cash, debt capitalised, services, SAFE or note conversion), resolution references, the ACRA lodgement reference and the effective date (the filing date, once ACRA has updated the electronic register of members)

#### Scenario: Share capital only from the register
- **WHEN** a posting to a share-capital account comes from anywhere other than this register or opening balances
- **THEN** it is refused (INV-13)

#### Scenario: Cap table computed from the register
- **WHEN** the cap table is produced
- **THEN** its issues come from this register, as part of the equity register (§8.23)

### Requirement: FR-EQ-09 ACRA steps
The system SHALL track the ACRA steps of a share issue with their evidence; Fyniq does not lodge (§4.2, §9.6).

Source: PRD-001 §8.22 · Priority M · Phase P1

#### Scenario: Fyniq tracks, people lodge
- **WHEN** an ACRA step is completed
- **THEN** it is recorded in Fyniq with its evidence
- **AND** Fyniq itself lodges nothing with ACRA

#### Scenario: Shareholders' approval lodged within 14 days
- **WHEN** the shareholders pass the resolution approving a share issue
- **THEN** a statutory-calendar task (FR-TAX-07) shows as due or overdue until the approval is lodged, within 14 days of the resolution (Companies Act s161(5) and s186)

#### Scenario: Approval lapses at the next AGM
- **GIVEN** an approved conversion not registered by the next AGM (or the date by which it must be held)
- **WHEN** that date passes
- **THEN** it raises a please-review warning that a new approval is needed

#### Scenario: Return of allotment
- **WHEN** the return of allotment is lodged
- **THEN** it is lodged with the written conversion agreement or a statement of particulars for shares not paid in cash (s63, s63B)

#### Scenario: Approved conversion not yet registered
- **GIVEN** an approved conversion not registered after the configured number of days (default 14, matching ACRA's guidance; the Act sets no deadline for private companies)
- **WHEN** the checks run
- **THEN** it raises a please-review warning

### Requirement: FR-EQ-10 Presentation
The system SHALL present conversions, waivers and other non-cash movements as non-cash in the financial statements and feed a related-party schedule into the notes (FR-RPT-11).

Source: PRD-001 §8.22 · Priority M · Phase P1

#### Scenario: Non-cash movements in the cash flow statement
- **WHEN** the cash flow statement is produced for a period with conversions, waivers or other non-cash movements
- **THEN** they are left out of it and listed as non-cash financing transactions
- **AND** a conversion is never shown as shares issued for cash and a loan repaid

#### Scenario: Statement of changes in equity
- **WHEN** the statement of changes in equity is produced
- **THEN** it shows shares issued for cash and shares issued for non-cash consideration separately

#### Scenario: Related-party schedule
- **WHEN** the notes are prepared
- **THEN** a related-party schedule of balances with directors, their pay and fees, conversions and waivers feeds them (FR-RPT-11)

### Requirement: FR-EQ-11 Director's account in debit
The system SHALL raise a please-review warning on Checks when a director's net balance (2500 and 2120 together) is in debit at a month end, and the balance sheet SHALL present it in 1250 Staff & director advances.

Source: PRD-001 §8.22 · Priority M · Phase P1

#### Scenario: Debit balance at a month end
- **WHEN** a director's net balance on 2500 and 2120 together is in debit at a month end
- **THEN** a please-review warning shows on Checks
- **AND** the balance sheet presents the balance in 1250 Staff & director advances

#### Scenario: Exempt-private-company status
- **WHEN** the organisation's exempt-private-company status is recorded
- **THEN** it is an effective-dated attribute of the corporate profile (FR-CS-01), meaning no corporate shareholder and at most 20 members

#### Scenario: Message for a company that is not exempt
- **GIVEN** the company is not an exempt private company
- **WHEN** a director's account is in debit
- **THEN** the message cites the Companies Act s162 ban on loans to directors and its exception for advances for company expenses (approved in general meeting, or repaid within 6 months after the next AGM)

#### Scenario: Losing exempt status re-runs the check
- **WHEN** the company loses exempt status, for example when a corporate investor joins
- **THEN** the director's-account check re-runs

### Requirement: FR-EQ-12 Year-end confirmation
Each director SHALL confirm their closing balance in the app with a passkey.

Source: PRD-001 §8.22 · Priority S · Phase P1

#### Scenario: Director confirms with a passkey
- **WHEN** a director confirms their closing balance at the year end
- **THEN** the confirmation is made in the app with a passkey

#### Scenario: Unconfirmed balance on the close checklist
- **GIVEN** a director has not confirmed their closing balance
- **WHEN** the year-end close checklist is viewed
- **THEN** it shows a please-review warning for that balance

### Requirement: FR-EQ-13 Shares for services
The system SHALL record shares for services as an equity-settled share-based payment: a grant record with the grant-date fair value (SFRS(I) 2 / IFRS 2; SFRS for Small Entities section 26), vesting, and the expense recognised over the service period.

Source: PRD-001 §8.22 · Priority C · Phase P2

#### Scenario: Expense over the service period
- **WHEN** a grant of shares for services is recorded with its grant-date fair value and vesting
- **THEN** the expense is recognised over the service period

#### Scenario: Employment income and no tax deduction
- **WHEN** the year's payroll returns are prepared
- **THEN** the grant provides employment-income data for IR8A [verify valuation]
- **AND** the company takes no tax deduction for newly issued shares

#### Scenario: Settling accrued pay is a conversion
- **WHEN** shares settle pay already accrued
- **THEN** it is a conversion (FR-EQ-06), not a share-based payment

### Requirement: FR-EQ-14 Modes
The founders' money features SHALL be tagged by experience mode (§7.8), and their warnings and due items SHALL show in both modes (FR-UX-24).

Source: PRD-001 §8.22 · Priority S · Phase P1

#### Scenario: Basic mode
- **WHEN** a user works in Basic mode
- **THEN** they see the "Paid personally" option and a "Money I put in" card for each founder (balance, latest items, pending settlements)

#### Scenario: Expert mode
- **WHEN** a user works in Expert mode
- **THEN** they also see the settlement routes and the register of share issues

#### Scenario: Warnings and due items in both modes
- **WHEN** an item of FR-EQ-09 or FR-EQ-11 is a please-review warning or due or overdue
- **THEN** it shows in Basic mode and in Expert mode
