# ledger/posting-rules Specification

## Purpose
How each source document, schedule and founders' event becomes journal entries: the debit and credit legs and their accounts, the amounts and rounding policies of each leg, settlement tolerances, the payroll calculation, depreciation, the system accounts and the GST codes. Derived from PRD-001 §10.2, the conventions of PRD-001 §B and Appendix B.1–B.7.

## Requirements

### Requirement: Posting conventions
Posting rules SHALL carry over the workbook's rules unchanged unless marked new or changed, and SHALL post debits as positive and credits as negative amounts in base currency.

Source: PRD-001 §10.2

#### Scenario: Debit positive, credit negative
- **WHEN** an entry is posted
- **THEN** its debits are positive and its credits negative, in base currency, as in the workbook engine

#### Scenario: Workbook rules carry over
- **WHEN** a posting rule is not marked new or changed
- **THEN** it is the workbook's rule (workbook PRD §9, verified by its reference ledger)

#### Scenario: Period of a posting
- **WHEN** an entry is posted
- **THEN** its period is the month of its posting date (Appendix B)

#### Scenario: GST rate used by the legs
- **WHEN** a leg uses the GST "rate"
- **THEN** it is the GST rate in force at the document's tax point (ENG-45) when the organisation is GST-registered at that tax point and the code is standard-rated (SR, TX, IM)
- **AND** otherwise the rate is 0 (Appendix B)

### Requirement: Posting a bank transaction, money in
A bank transaction with money in SHALL debit the bank with the gross and credit the category with the net and the GST control with the output GST.

Source: PRD-001 §10.2

#### Scenario: Money-in legs
- **WHEN** a money-in bank transaction is posted
- **THEN** it posts Dr Bank (gross); Cr Category (net); Cr GST control (output GST, by GST-inclusive extraction)

#### Scenario: GST only when registered and the code carries GST
- **GIVEN** the organisation is not GST-registered at the tax point (for a bank transaction, its date), or the code carries no GST
- **WHEN** the money-in bank transaction is posted
- **THEN** no output GST is posted

### Requirement: Posting a bank transaction, money out
A bank transaction with money out SHALL debit the category with the net and the GST control with the input GST, and credit the bank with the gross.

Source: PRD-001 §10.2

#### Scenario: Money-out legs
- **WHEN** a money-out bank transaction is posted
- **THEN** it posts Dr Category (net); Dr GST control (input GST); Cr Bank (gross)

### Requirement: Spreading a bank transaction over N months
A bank transaction spread over N months SHALL post to a deferral account instead of the category and release it monthly to the category.

Source: PRD-001 §10.2

#### Scenario: Deferral account instead of the category
- **WHEN** a bank transaction spread over N months is posted
- **THEN** Prepayments 1210 (money out) or Deferred revenue 2400 (money in) takes the place of the category

#### Scenario: Monthly release
- **WHEN** each month of the spread is reached
- **THEN** that month's part is released from the deferral account to the category

### Requirement: Transfer between money accounts
A transfer between money accounts SHALL debit the receiving account and credit the paying account, recorded once.

Source: PRD-001 §10.2

#### Scenario: Transfer legs
- **WHEN** a transfer between two money accounts is posted
- **THEN** it posts Dr receiving account; Cr paying account

#### Scenario: Recorded once, matched on both feeds
- **GIVEN** the transfer appears in the feeds of both accounts
- **WHEN** the feeds are processed
- **THEN** the transfer is recorded once
- **AND** it is matched on both feeds

### Requirement: Posting an invoice
A posted invoice SHALL debit AR 1100 with the total and credit revenue, or deferred revenue 2400 when spread, with the net and the GST control with the GST.

Source: PRD-001 §10.2

#### Scenario: Invoice legs
- **WHEN** an invoice is posted
- **THEN** it posts Dr AR 1100 (total); Cr Revenue (net), or Deferred revenue 2400 when spread; Cr GST control

#### Scenario: Credit notes reverse
- **WHEN** a credit note is posted
- **THEN** it posts the reverse of the invoice legs

#### Scenario: An invoice that deducts deposits (rev 0.127)
- **WHEN** an invoice deducts customer deposits (FR-AR-09)
- **THEN** it posts Dr 1100 with its total less what they take, Dr 2410 with each deposit's part less its GST part, Cr revenue, and Cr 2200 with its GST less what the deposits bore
- **AND** the deposits' parts against what they take at the invoice's rate go to 7030 as realised FX [verify]

#### Scenario: Built as credit_note@1 (rev 0.123)
- **WHEN** a credit note to a customer is issued
- **THEN** every line is the line invoice@1 posts for the same content, amount and base amount negated, so the negation is exact under RND-FX
- **AND** a spread line is refused (LEDGER-28): crediting a spread invoice debits the revenue account on the credit note's date

#### Scenario: Drafts never post
- **WHEN** an invoice is saved as a draft
- **THEN** nothing is posted

### Requirement: Posting a bill
A posted bill SHALL debit an expense or asset, or Prepayments 1210 when spread, with the net and the GST control with the GST, and credit AP 2010.

Source: PRD-001 §10.2

#### Scenario: Bill legs
- **WHEN** a bill is posted
- **THEN** it posts Dr Expense or asset (net), or Prepayments 1210 when spread; Dr GST control; Cr AP 2010

#### Scenario: Vendor credits reverse
- **WHEN** a vendor credit is posted
- **THEN** it posts the reverse of the bill legs

#### Scenario: Built as vendor_credit@1 (rev 0.80)
- **WHEN** a credit note is posted
- **THEN** every line is the line bill@1 posts for the same content, amount and base amount negated, so the negation is exact under RND-FX

#### Scenario: Built: the spread line and its releases (rev 0.83)
- **WHEN** a bill with a line spread over months is posted under bill@1
- **THEN** 1210 takes RND-FX of the line's cost at the bill's rate, in the base currency, never revalued
- **AND** bill_release@1 posts each month's part, Dr the line's account and Cr 1210 in the base currency; LEDGER-28 refuses a spread of 0 or over 120 months, of a line not an expense or with no cost, or on a vendor credit

### Requirement: Customer and supplier payments
A customer payment SHALL debit the bank and credit AR 1100, matched to documents, and a supplier payment SHALL debit AP 2010 and credit the bank.

Source: PRD-001 §10.2

#### Scenario: Customer payment
- **WHEN** a customer payment is posted
- **THEN** it posts Dr Bank; Cr AR 1100
- **AND** it is matched to documents (FR-BNK-07)

#### Scenario: Supplier payment
- **WHEN** a supplier payment is posted
- **THEN** it posts Dr AP 2010; Cr Bank

#### Scenario: Built as bill_payment@1 (rev 0.77)
- **WHEN** a payment of bills is recorded
- **THEN** it posts Dr 2010 with each bill's part at that bill's stored rate and any unapplied amount at the payment's rate, Cr the money account at the payment's rate or with what a base-currency account paid, and to 7030 each part's realised FX (ENG-41) and the cent RND-FX leaves between the lines

#### Scenario: The residual of a bill paid in parts (rev 0.77)
- **GIVEN** a bill whose parts' RND-FX does not sum to its carrying amount
- **WHEN** the part that settles what is left of it is recorded
- **THEN** a base-only line on 2010 of at most S$1.00 clears the residual, against 7030, in the month of the last payment

#### Scenario: Built as customer_receipt@1 (rev 0.120)
- **WHEN** a receipt from a customer is recorded
- **THEN** it posts Cr 1100 with each invoice's part at that invoice's stored rate and any unapplied amount at the receipt's rate, Dr the money account at the receipt's rate or with what a base-currency account received, and to 7030 each part's realised FX (ENG-41) and the cent RND-FX leaves between the lines
- **AND** the part that settles what is left of an invoice clears its residual with a base-only line on 1100 of at most S$1.00 (USD 1,000.00 at 1.34 received at 1.36: a gain of S$20.00)

#### Scenario: A settlement difference within tolerance as built (rev 0.121)
- **WHEN** a part of a payment or receipt is recorded in full with less than is open
- **THEN** its control line settles all that is open at the document's stored rate, and the difference's carrying amount goes to 7030 on a line of its own, apart from the realised FX
- **AND** USD 1,000.00 at 1.34 received USD 990.00 in full at 1.36 posts Dr 7030 S$13.40 for the difference and Cr 7030 S$19.80 of realised gain

### Requirement: Customer refunds and deposits
Customer refunds and deposits received and applied (FR-AR-18, FR-AR-09) SHALL post through allocation records and SHALL never be netted against AP without an approved contra.

Source: PRD-001 §10.2

#### Scenario: Refund of a customer credit
- **WHEN** a customer credit is refunded
- **THEN** it posts Dr 1100; Cr Bank

#### Scenario: Built as customer_refund@1 (rev 0.125)
- **WHEN** a refund of a credit note or of a receipt's unapplied amount is recorded
- **THEN** it posts Dr 1100 with the amount at the credit's rate, Cr the money account with what left it, and the realised FX to 7030 (ENG-41)
- **AND** an allocation from the refund settles the credit

#### Scenario: Deposit received, then applied
- **WHEN** a customer deposit is received
- **THEN** it posts Dr Bank; Cr the deposits account
- **AND** when it is applied it posts Dr the deposits account; Cr 1100

#### Scenario: By allocation records, never netted against AP
- **WHEN** a refund or deposit is posted
- **THEN** it is posted by allocation records
- **AND** it is never netted against AP without an approved contra

### Requirement: Paying salaries or CPF
Paying salaries or CPF SHALL debit 2100 or 2110 and credit the bank.

Source: PRD-001 §10.2

#### Scenario: Salary or CPF payment legs
- **WHEN** salaries or CPF are paid
- **THEN** it posts Dr 2100 (salaries) or 2110 (CPF); Cr Bank

### Requirement: Posting a manual journal
A manual journal SHALL post its lines as entered, and only when it is balanced.

Source: PRD-001 §10.2

#### Scenario: Balanced journal posts as entered
- **GIVEN** a journal whose debits equal its credits
- **WHEN** it is posted
- **THEN** its lines are posted as entered

#### Scenario: Unbalanced journal does not post
- **GIVEN** a journal whose debits and credits differ
- **WHEN** it is posted
- **THEN** it does not post

### Requirement: Opening balances
Opening balances SHALL post as entered at the books start date and SHALL net to zero.

Source: PRD-001 §10.2

#### Scenario: Opening balances post at books start
- **WHEN** opening balances are posted
- **THEN** they are posted as entered, at the books start date

#### Scenario: Opening balances must net to zero
- **GIVEN** opening balances that do not net to zero
- **WHEN** they are posted
- **THEN** they do not post

### Requirement: Posting an expense claim
An expense claim SHALL debit the expense with the net and the GST control with the GST, and credit 2150 Expense claims payable, reimbursed via a payment batch.

Source: PRD-001 §10.2

#### Scenario: Expense claim legs
- **WHEN** an expense claim is posted
- **THEN** it posts Dr Expense (net); Dr GST control; Cr 2150 Expense claims payable

#### Scenario: Reimbursed via a payment batch
- **WHEN** the claim is reimbursed
- **THEN** it is paid through a payment batch

### Requirement: FX revaluation at month end
At month end the system SHALL revalue open foreign-currency items and foreign-currency bank balances against unrealised FX gain or loss (7040, in the standard chart) and reverse the revaluation automatically on the first day of the next period.

Source: PRD-001 §10.2

#### Scenario: Revaluation legs
- **WHEN** the month-end FX revaluation is posted
- **THEN** it posts Dr open FC items and FC bank balances, or 7040 unrealised FX loss; Cr 7040 unrealised FX gain, or the items

#### Scenario: Automatic reversal
- **WHEN** the next period begins
- **THEN** the revaluation auto-reverses on its first day

#### Scenario: Base-only lines on monetary items
- **WHEN** the revaluation adjusts an item
- **THEN** it posts base-only lines on the monetary items of FR-GL-04 (the INV-05 exception)

#### Scenario: Realised FX uses the stored document rate
- **WHEN** a foreign-currency document is settled
- **THEN** realised FX is computed from the stored document rate

### Requirement: Asset disposal
An asset disposal SHALL be posted by the system, where the workbook left it to a manual journal, clearing the asset's cost and accumulated depreciation against the proceeds and a gain or loss.

Source: PRD-001 §10.2

#### Scenario: Disposal legs
- **WHEN** an asset disposal is posted
- **THEN** it posts Dr 1590 (accumulated depreciation), Bank or AR (proceeds), loss on disposal; Cr asset cost account, gain on disposal, GST control if applicable

### Requirement: Income tax provision
The income tax provision SHALL be proposed from the tax working paper and posted as an approved journal, Dr 8010 Income tax expense and Cr 2300 Income tax payable.

Source: PRD-001 §10.2

#### Scenario: Provision legs
- **GIVEN** an income tax provision proposed from the tax working paper
- **WHEN** the journal is approved
- **THEN** it posts Dr 8010 Income tax expense; Cr 2300 Income tax payable

### Requirement: Bad-debt write-off
A bad-debt write-off SHALL be approval-gated (SOD-9) and SHALL debit bad-debt expense and, when eligible, the GST control for the relief, and credit AR 1100.

Source: PRD-001 §10.2

#### Scenario: Write-off legs
- **GIVEN** a bad-debt write-off that has been approved (SOD-9)
- **WHEN** it is posted
- **THEN** it posts Dr bad-debt expense; Dr GST control (relief, when eligible); Cr AR 1100

#### Scenario: Built as write_off@1 (rev 0.122)
- **WHEN** an approved write-off of an invoice posts
- **THEN** it posts Dr the reason's account with the carrying amount cleared and Cr 1100 with the amount at the invoice's stored rate, with the residual RND-FX left on a base-only 1100 line when it takes all that is open
- **AND** the GST relief leg waits for FR-AR-11 (P2)

### Requirement: Founder loans, directors' fees and repayments
Money a founder lends, an approved director's fee and a repayment to a director SHALL post to that director's 2500 or 2120 balance.

Source: PRD-001 §10.2

#### Scenario: Money a founder lends
- **WHEN** a founder lends money to the company
- **THEN** it posts Dr Bank; Cr 2500 (that director)

#### Scenario: Director's fee approved
- **WHEN** a director's fee is approved
- **THEN** it posts Dr 6050; Cr 2120 (that director)
- **AND** it is paid later from 2120

#### Scenario: Repaying a director
- **WHEN** a director is repaid
- **THEN** it posts Dr 2500 or 2120; Cr Bank
- **AND** it is paid through a payment batch (FM-1, FM-9)

### Requirement: Mileage claim
A mileage claim (FR-AP-16) SHALL debit the travel (mileage) expense by project and credit 2150 Expense claims payable, with the amount by `RND-MILEAGE` and no GST.

Source: PRD-001 §10.2

#### Scenario: Mileage claim legs
- **WHEN** a mileage claim is posted
- **THEN** it posts Dr Travel (mileage) expense, by project; Cr 2150 Expense claims payable
- **AND** the amount follows `RND-MILEAGE`
- **AND** no GST is posted

### Requirement: Cash advance to an employee
A cash advance to an employee (FR-AP-17) SHALL post to 1250 Staff & director advances and SHALL be settled by the employee's claims.

Source: PRD-001 §10.2

#### Scenario: Advance paid
- **WHEN** a cash advance is paid to an employee
- **THEN** it posts Dr 1250 Staff & director advances; Cr Bank

#### Scenario: Advance settled by claims
- **WHEN** a claim settles the advance
- **THEN** it posts Dr the claim's expense lines; Cr 1250

#### Scenario: Unsettled balance
- **GIVEN** the claims do not settle all of the advance
- **WHEN** the unsettled balance is cleared
- **THEN** it is refunded or deducted

#### Scenario: Old advance needs review
- **GIVEN** an unsettled advance older than the claim deadline
- **WHEN** it is shown
- **THEN** it carries a warning (⚠)

### Requirement: Payment-provider sale, fee and payout
Payment-provider sales, fees and payouts (FR-PSP-02…06) SHALL post through the provider clearing account, which SHALL net to zero per payout (FR-PSP-06).

Source: PRD-001 §10.2

#### Scenario: Sale, fee and payout legs
- **WHEN** a provider sale, its fee and the payout are posted
- **THEN** they post Dr provider clearing (gross); Dr fees expense and input GST when invoiced; Dr Bank (net payout)
- **AND** Cr Revenue or AR (gross); Cr provider clearing (fees, payout)

#### Scenario: Clearing nets to zero per payout
- **WHEN** a payout is posted
- **THEN** the provider clearing account nets to zero for that payout

#### Scenario: Fees post once
- **GIVEN** a provider fee already posted with its transaction
- **WHEN** the payout is posted
- **THEN** the fee is not posted again

### Requirement: Edits of posted documents
An edit of a posted document SHALL post a reversal of the previous version's entries and the entries of the new version, keeping history where the workbook recalculated in place (FR-GL-12).

Source: PRD-001 §10.2

#### Scenario: Edit posts reversal and new entries
- **GIVEN** a posted document
- **WHEN** it is edited
- **THEN** the previous version's entries are reversed
- **AND** the entries of the new version are posted
- **AND** the previous entries stay in the history

### Requirement: Amounts of a bank, card or cash transaction
A bank, card or cash transaction SHALL post the signed gross to its money account, the GST extracted by `RND-GST-INCL` to 2200 and the remainder to its category.

Source: PRD-001 §B.1

#### Scenario: Legs without a spread
- **WHEN** a transaction is posted without a spread
- **THEN** the money account takes + signed gross (money in positive, money out negative)
- **AND** 2200 GST control takes − GST, where GST = `RND-GST-INCL`(signed gross)
- **AND** the category account takes − (signed gross − GST)

#### Scenario: Spread over n months, P&L category only
- **GIVEN** a transaction with a P&L category spread over n months
- **WHEN** it is posted
- **THEN** the category leg goes to 2400 (money in) or 1210 (money out) in the posting month
- **AND** monthly releases from the deferral account to the category run from the posting month for n months (`ALLOC-EVEN`), because a bank line has no service period (documents start at their service start month, B.2)

#### Scenario: Settlement of a matched document
- **GIVEN** a transaction with category 1100 or 2010 and a matched document
- **WHEN** it is posted
- **THEN** it clears AR or AP
- **AND** a difference within tolerance goes to 7030

### Requirement: Settlement tolerances
Settlement tolerances SHALL be organisation settings, defaulting to the workbook's, and SHALL apply only to the residual left after realised FX (FR-BNK-08), never above the caps per document and per month.

Source: PRD-001 §B.1

#### Scenario: Default tolerances
- **WHEN** the tolerances have not been changed
- **THEN** the base-currency tolerance is S$1.00
- **AND** the foreign-currency tolerance is the greater of S$1.00 and 3% of the document total

#### Scenario: Only the residual after realised FX
- **WHEN** a settlement difference is assessed
- **THEN** the tolerance applies only to the residual left after realised FX
- **AND** the foreign total is converted at the document's stored rate

#### Scenario: Caps per document and per month
- **WHEN** a residual is above the absolute cap per document or the cap per month (settings; open question 32)
- **THEN** it is not within tolerance

#### Scenario: Realised FX is never limited
- **WHEN** realised FX is posted
- **THEN** it is never limited by a tolerance

#### Scenario: Overpayment above S$1.00
- **WHEN** a document is overpaid by more than S$1.00
- **THEN** the overpayment is never within tolerance

### Requirement: Amounts of a posted invoice
A posted invoice (not a draft, not an opening item) SHALL post its net with `RND-FX`, its GST with `RND-GST-EXCL` and its total to 1100, in base currency.

Source: PRD-001 §B.2

#### Scenario: Invoice net
- **WHEN** an invoice is posted
- **THEN** − `RND-FX`(net in document currency × rate) goes to revenue
- **AND** it goes to 2400 instead when service months > 1 for P&L categories

#### Scenario: Invoice GST and control
- **WHEN** an invoice is posted
- **THEN** − `RND-GST-EXCL`(net base) goes to 2200
- **AND** + total goes to 1100

### Requirement: Amounts of a posted bill
A posted bill (not a draft, not an opening item) SHALL post its net with `RND-FX` to an expense or asset (or 1210 when spread), the SGD GST stated on the supplier's tax invoice to 2200 (ENG-42) and its total to 2010.

Source: PRD-001 §B.2

#### Scenario: Bill net and control
- **WHEN** a bill is posted
- **THEN** + `RND-FX`(net in document currency × rate) goes to the expense or asset account, or to 1210 when spread
- **AND** − total goes to 2010

#### Scenario: Input GST is the stated GST
- **WHEN** a bill is posted
- **THEN** + the SGD GST stated on the supplier's tax invoice goes to 2200 (ENG-42)

#### Scenario: Withholding tax (rev 0.90)
- **GIVEN** a non-resident supplier's bill whose tax agent decided tax is withheld (FR-AP-19)
- **WHEN** the bill is posted
- **THEN** − the tax goes to 2330 under `RND-WHT`, and − the total less what is withheld of the supplier's own amount goes to 2010
- **AND** a foreign bill's difference goes to 7030

#### Scenario: Only claimable input tax, for its business share (rev 0.89)
- **GIVEN** a bill line whose input tax is claimable for a business share under 100% (FR-AP-20)
- **WHEN** the bill is posted
- **THEN** 2200 takes `RND-GST-SHARE` of the line's GST (of the GST stated in SGD for a foreign bill)
- **AND** the GST not claimed stays in the line's cost (S$1,000.00 + GST 90.00 at 60%: Dr expense 1,036.00, Dr 2200 54.00, Cr 2010 1,090.00)

#### Scenario: Stated GST differs from the recomputation
- **GIVEN** the stated GST differs from `RND-GST-EXCL`(net base) by more than rounding
- **WHEN** the bill is posted
- **THEN** it carries a warning (⚠)

#### Scenario: Foreign-currency difference
- **GIVEN** a foreign-currency bill
- **WHEN** it is posted
- **THEN** a foreign-currency difference goes to 7030

#### Scenario: G4-024 Supplier rounded GST per line
- **GIVEN** an SGD bill where the supplier rounded GST per line, one cent above the subtotal method
- **WHEN** the bill is posted
- **THEN** input tax = the stated GST
- **AND** ⚠ "differs by S$0.01 from the recomputation" is acknowledged by the approver

### Requirement: Settlement difference on an invoice or bill
After realised FX per payment (ENG-41), the residual of an invoice or bill within tolerance SHALL post to 7030 in the month of the last payment, and a larger residual SHALL need a reason-coded, approved write-off (FR-BNK-08, FR-AR-17).

Source: PRD-001 §B.2

#### Scenario: Realised FX first
- **WHEN** payments settle a document
- **THEN** realised FX is posted per payment first (ENG-41)

#### Scenario: Invoice residual within tolerance
- **GIVEN** an invoice's residual (total − all payments, in base) is within tolerance
- **WHEN** the last payment is posted
- **THEN** the residual is posted in the month of the last payment: AR −, 7030 +
- **AND** the legs are Dr 7030 or AR; Cr AR or 7030 (§10.2)

#### Scenario: Bill residual within tolerance
- **GIVEN** a bill's residual is within tolerance
- **WHEN** the last payment is posted
- **THEN** the residual is posted: AP +, 7030 −, within tolerance only

#### Scenario: Marked fully paid with a larger residual
- **GIVEN** an invoice or bill with a residual larger than the tolerance
- **WHEN** it is marked as fully paid
- **THEN** no settlement-difference posting is made
- **AND** a reason-coded write-off is opened (FR-BNK-08, FR-AR-17)

#### Scenario: Overpayment stays a customer credit
- **WHEN** a customer overpays by more than S$1.00
- **THEN** the overpayment stays a customer credit (FR-AR-18)

### Requirement: Spreading an invoice or bill
When an invoice's or bill's net is spread, the deferral account SHALL take the full net in the posting month and release it monthly from the service start month (`ALLOC-EVEN`).

Source: PRD-001 §B.2

#### Scenario: Invoice spread
- **WHEN** a spread invoice is posted
- **THEN** 2400 takes the full net in the posting month
- **AND** monthly releases run from the service start month (`ALLOC-EVEN`)

#### Scenario: Bill spread
- **WHEN** a spread bill is posted
- **THEN** 1210 takes the full net in the posting month
- **AND** monthly releases run from the service start month (`ALLOC-EVEN`)

#### Scenario: G4-022 Annual subscription paid upfront
- **GIVEN** an annual subscription invoiced and paid upfront, with the revenue spread over 12 months
- **WHEN** it is posted
- **THEN** all the GST is in the invoice period
- **AND** only the revenue is spread

### Requirement: Credit documents and opening items
Credit notes and vendor credits SHALL post the reverse of an invoice or bill, and opening items SHALL carry their open balance into AR or AP without touching revenue or expense.

Source: PRD-001 §B.2

#### Scenario: Credit notes and vendor credits
- **WHEN** a credit note or vendor credit is posted, as a document with a negative total or as a linked credit document
- **THEN** it posts the reverse

#### Scenario: Opening items
- **WHEN** an opening invoice or bill is posted
- **THEN** its open balance goes to AR or AP
- **AND** no revenue or expense is posted

### Requirement: Payroll per employee-month
Payroll SHALL compute CPF, SDL and SHG once per employee-month (calendar month, per employer) on the total wages payable in that month, across all its runs and corrections.

Source: PRD-001 §B.3

#### Scenario: Total wages across runs
- **GIVEN** an employee paid in a main run, an off-cycle run and a correction of the same month
- **WHEN** CPF, SDL and SHG are computed
- **THEN** they are computed on the total wages payable in the calendar month (CPF Board: contributions "are computed based on the Total Wages payable in a calendar month")

#### Scenario: Bands, ceiling and rounding applied once
- **WHEN** the employee-month is computed
- **THEN** the wage band (including S$50–750), the OW ceiling, the age band and the `RND-CPF` rounding apply once, to the month's total

#### Scenario: Later run posts only the difference
- **GIVEN** a posted run for the month
- **WHEN** a later run or correction in the same month is posted
- **THEN** the employee-month is recomputed
- **AND** only the difference from what was already posted is posted

#### Scenario: Replaces the duplicate-employee status
- **WHEN** an employee appears in more than one run of a month
- **THEN** this rule applies instead of the workbook's "✖ Employee entered twice this month" (ENG-20)

### Requirement: Gross pay and CPF wages
Payroll SHALL compute gross pay, ordinary wages for CPF and additional wages for CPF from the pay items of the employee-month.

Source: PRD-001 §B.3

#### Scenario: Gross
- **WHEN** gross pay is computed
- **THEN** gross = basic + allowances + overtime/other ordinary pay + bonus/commission − unpaid leave

#### Scenario: Ordinary wages for CPF
- **WHEN** ordinary wages for CPF are computed
- **THEN** OW = min(max(0, basic + allowances + OT − unpaid leave), OW ceiling)
- **AND** an item counts only when FR-PAY-17's facts make it ordinary wages

#### Scenario: Additional wages for CPF
- **WHEN** additional wages for CPF are computed
- **THEN** additional wages are the items FR-PAY-17 classes as such (an annual bonus is the usual case), limited by the annual AW ceiling (new: the workbook did not enforce it)
- **AND** a commission is not additional wages by its name

### Requirement: CPF, SDL and SHG in a pay run
Payroll SHALL compute CPF with `RND-CPF` and graduated rates, SDL with `RND-SDL` for all employees, and SHG from the SHG table unless the employee opted out.

Source: PRD-001 §B.3

#### Scenario: CPF for Citizens and PRs
- **WHEN** CPF is computed for a Citizen or PR
- **THEN** it follows `RND-CPF` with rates by residency, age band and PR year (new: PR graduated rates)
- **AND** graduated rates apply below the full-rate wage floor (new)

#### Scenario: SDL for all employees
- **WHEN** SDL is computed
- **THEN** it follows `RND-SDL` for all employees: per employee on the month's total wages, then the employer-month payable rounded down to the dollar
- **AND** a later run in the same month posts only the change

#### Scenario: SHG from the table
- **GIVEN** an employee who has not opted out of SHG
- **WHEN** SHG is computed
- **THEN** it comes from the SHG table by fund and wage (new: the workbook used a flat amount)

### Requirement: Posting a pay run
A pay run SHALL post the gross by department, the employer CPF and the SDL as expenses, the amount to disburse and any held monies to 2100 and the CPF, SDL and SHG due to 2110, with deductions and reimbursements on their own accounts.

Source: PRD-001 §B.3

#### Scenario: Net pay
- **WHEN** net pay is computed
- **THEN** net pay = gross − EE CPF − SHG − deductions authorised under FR-PAY-16
- **AND** the amount to disburse = net pay + non-wage reimbursements (FR-AP-17) − monies held (FR-PAY-10) (FR-PAY-18)

#### Scenario: Pay-run legs
- **WHEN** a pay run is posted
- **THEN** it posts Dr salary account (by department) gross; Dr 6020 ER CPF; Dr 6030 SDL
- **AND** Cr 2100 net pay; Cr 2110 EE CPF + ER CPF + SDL + SHG

#### Scenario: Deductions, reimbursements and held monies
- **GIVEN** a run with an advance recovered, another authorised deduction, a booked claim paid with the salary and a held person
- **WHEN** it is posted
- **THEN** it posts Cr 1250 for the advance, Cr the deduction's own account (for example a court order's payee) and Dr 2150 for the claim
- **AND** Cr 2100 is the amount to disburse plus the held monies, which stay in 2100, flagged, until released
- **AND** gross + ER CPF + SDL + reimbursements = advances recovered + other deductions + 2100 + 2110

### Requirement: Depreciation
Depreciation SHALL post monthly, Dr 6610 and Cr 1590, as an `ALLOC-EVEN` split of cost less residual over the useful life in months.

Source: PRD-001 §B.4

#### Scenario: Monthly amount
- **WHEN** monthly depreciation is computed
- **THEN** monthly = `ALLOC-EVEN`(cost − residual over the useful life in months)

#### Scenario: Start and end months
- **WHEN** the depreciation period of an asset is set
- **THEN** the start month is the depreciate-from date or the purchase month
- **AND** the end is start + life − 1, or the month before disposal

#### Scenario: From the later of start and books start
- **GIVEN** an asset whose depreciation started before the books start
- **WHEN** depreciation is posted
- **THEN** it is posted from the later of the start and the books start
- **AND** earlier depreciation is part of the opening balances

#### Scenario: Depreciation legs
- **WHEN** a month's depreciation is posted
- **THEN** it posts Dr 6610; Cr 1590

### Requirement: System accounts
Posting rules SHALL use the system accounts by key and code; codes not yet fixed SHALL be marked as proposed or to be assigned in the chart-of-accounts review.

Source: PRD-001 §B.5

#### Scenario: System accounts from the workbook
- **WHEN** posting rules use a system account
- **THEN** the codes are 1100 Accounts receivable, 1210 Prepayments, 1590 Accumulated depreciation, 2010 Accounts payable, 2100 Salaries payable, 2110 CPF & SDL payable, 2150 Expense claims payable, 2200 GST control, 2300 Income tax payable, 2400 Deferred revenue, 2999 Suspense, 3100 Retained earnings, 6020 Employer CPF expense, 6030 SDL expense, 6610 Depreciation expense, 7030 FX & rounding differences, 8010 Income tax expense

#### Scenario: Withholding tax payable (rev 0.90)
- **WHEN** a bill withholds tax from a non-resident supplier (FR-AP-19)
- **THEN** the tax goes to 2330 Withholding tax payable, the account with the role withholding-tax-payable [accountant to confirm the code]

#### Scenario: Customer deposits (rev 0.126)
- **WHEN** a customer deposit is recorded (FR-AR-09)
- **THEN** it goes to 2410 Customer deposits & retainers, the account with the role customer-deposits, never revalued [accountant to confirm the classification]

#### Scenario: New accounts still to be assigned
- **WHEN** the unrealised FX, payment-provider clearing, gain or loss on disposal and bad debts accounts are needed
- **THEN** the standard chart assigns them provisionally until the CoA review: 7040 unrealised FX gain or loss, 7050 gain or loss on disposal (the workbook's 7040, moved), 1050 payment gateway as the payment-provider clearing account, and the workbook's 5040 for payment fees (rev 0.31)
- **AND** bad debts stay to be assigned
- **AND** share application monies pending allotment, a liability per subscriber (B.7), get a code in the review [accountant to confirm]

#### Scenario: Bank accounts and accrued expenses
- **WHEN** bank and money accounts and accrued expenses are set up
- **THEN** they use the proposed 10xx range and the proposed 2050 (the prototype and the `gl.post_journal` examples use these)
- **AND** they are confirmed in the CoA review

#### Scenario: Founders' accounts
- **WHEN** founders' balances are posted
- **THEN** 2500 is Loan from director / shareholder (workbook code; a control account with a sub-ledger per person)
- **AND** proposed 2120 is Deferred remuneration payable (sub-ledger per person), used as fixed in §10.2 and B.7 and confirmed in the CoA review
- **AND** 1250 is Staff & director advances (workbook code; presents a director's debit balance, FR-EQ-11)
- **AND** 6050 is Directors' fees (workbook code)

#### Scenario: Equity accounts
- **WHEN** share capital or a capital contribution is posted
- **THEN** 3010 / 3020 are Share capital, ordinary / preference (workbook codes; postings only from the register of share issues, INV-13)
- **AND** proposed 3300 is Capital contribution (equity)
- **AND** 2700 is Convertible notes / SAFE (workbook code, §8.22; each instrument classified per FR-EQ-18)

### Requirement: GST codes and F5 reporting
The system SHALL provide the workbook's GST codes, each marked standard-rated or not, with its direction and its F5 reporting, extended per current IRAS guidance [verify].

Source: PRD-001 §B.6

#### Scenario: Supply codes
- **WHEN** a supply is coded
- **THEN** SR is standard-rated and reported in Box 1 (value) and Box 6 (GST)
- **AND** ZR is reported in Box 2, ES in Box 3, and OS is not reported

#### Scenario: Standard-rated purchase codes
- **WHEN** a purchase is coded TX or IM
- **THEN** it is standard-rated and reported in Box 5 (value) and Box 7 (GST)

#### Scenario: Other purchase codes
- **WHEN** a purchase is coded ZP, BL, NR, EP or OP
- **THEN** ZP is reported in Box 5
- **AND** BL is not reported, with the GST not claimable and included in the cost (rev 0.37; IRAS leaves expenses whose input tax is disallowed out of Box 5)
- **AND** NR, EP and OP are not reported

#### Scenario: No GST direction
- **WHEN** a line is coded NA
- **THEN** it has no direction and is not reported

### Requirement: Founders' balances by person
All amounts on 2500 and 2120 SHALL carry the person as a sub-ledger key (FR-EQ-01).

Source: PRD-001 §B.7

#### Scenario: Person key on founders' balances
- **WHEN** an amount is posted to 2500 or 2120
- **THEN** it carries the person as its sub-ledger key

### Requirement: Cost a founder paid personally, kept as a loan
A cost a founder paid personally and kept as a loan (FR-EQ-02) SHALL credit 2500 with the gross, instead of 2150, when the claim settles to the director's account.

Source: PRD-001 §B.7

#### Scenario: Legs when input tax is claimable
- **GIVEN** the claim meets the input-tax eligibility rule (FR-AP-20)
- **WHEN** the cost is posted
- **THEN** it posts Dr Expense (net); Dr 2200; Cr 2500 (gross, that director)

#### Scenario: Legs when input tax is not claimable
- **GIVEN** the claim does not meet the input-tax eligibility rule
- **WHEN** the cost is posted
- **THEN** no input tax is claimed and the GST is part of the cost
- **AND** 2500 is credited with the gross

#### Scenario: Instead of 2150
- **WHEN** the claim settles to the director's account
- **THEN** 2500 is credited instead of 2150

### Requirement: Deferred founder pay
Deferred founder pay (FR-EQ-04) SHALL move that director's net pay from 2100 to 2120 after the normal pay-run posting, leaving 2110 unchanged.

Source: PRD-001 §B.7

#### Scenario: Deferred pay legs
- **GIVEN** a director whose choice is "deferred"
- **WHEN** the pay run is posted
- **THEN** it also posts Dr 2100 (that director's net pay); Cr 2120 (that director), after the normal pay-run posting

#### Scenario: CPF and SDL unchanged
- **WHEN** deferred pay is posted
- **THEN** 2110 is unchanged
- **AND** the CPF and SDL stay on 2110 and are paid as usual

### Requirement: Conversion of a founder's balance to shares
A conversion of a founder's balance to shares (FR-EQ-06) SHALL move the converted amount from 2500 or 2120 to 3010 or 3020 with no cash, on the date ACRA registered the allotment, never backdated.

Source: PRD-001 §B.7

#### Scenario: Carrying-amount basis (default)
- **WHEN** a balance is converted on the carrying-amount basis
- **THEN** it posts Dr 2500 or 2120, converted amount; Cr 3010 or 3020, converted amount
- **AND** n = `RND-SHARE`(amount, price), converted amount = round(n × price, 2), and the remainder stays

#### Scenario: Posting date
- **WHEN** a conversion is posted
- **THEN** its posting date is the date ACRA registered the allotment, never backdated

#### Scenario: Fair-value basis (accountant's choice)
- **GIVEN** the accountant chose the fair-value basis (IFRIC 19 ¶9; SFRS for Small Entities ¶22.15A–C)
- **WHEN** a balance is converted
- **THEN** it posts Dr 2500 or 2120 at the converted amount, and a loss when the shares' fair value is higher
- **AND** Cr 3010 or 3020 at the shares' fair value, and a gain when it is lower
- **AND** the gain and loss accounts are assigned in the CoA review

### Requirement: Waiver of a founder's balance
A waiver of a founder's balance (FR-EQ-07) SHALL debit 2500 or 2120 and credit 3300 Capital contribution or other income, as the accountant chooses, with a reason.

Source: PRD-001 §B.7

#### Scenario: Waiver legs
- **WHEN** a waiver is posted
- **THEN** it posts Dr 2500 or 2120; Cr 3300 Capital contribution, or other income
- **AND** the accountant's choice of credit is recorded with a reason

### Requirement: Share issue for cash
A share issue for cash (FR-EQ-17) SHALL post only from the equity register (INV-13), on the effective date, Dr Bank and Cr 3010 or 3020.

Source: PRD-001 §B.7

#### Scenario: Cash on or after the effective date
- **GIVEN** the cash arrives on or after the issue's effective date
- **WHEN** the register posts the issue
- **THEN** it posts Dr Bank; Cr 3010 or 3020, with the effective date as posting date

#### Scenario: Cash received earlier
- **GIVEN** the cash arrived before the effective date
- **WHEN** the issue is posted
- **THEN** it debits share application monies instead of Bank

### Requirement: Cash received before an allotment takes effect
Cash received before an allotment takes effect SHALL wait in share application monies pending allotment, a liability per subscriber, whose code and classification are set in the CoA review [accountant to confirm].

Source: PRD-001 §B.7

#### Scenario: Cash received for a pending issue
- **WHEN** a bank line is matched to a pending share issue in the register
- **THEN** it posts Dr Bank (on the bank date); Cr share application monies pending allotment (per subscriber)

#### Scenario: Allotment takes effect
- **WHEN** the effective date is reached
- **THEN** the register posts Dr share application monies; Cr 3010 or 3020 (INV-13 unchanged)

#### Scenario: Issue does not proceed
- **WHEN** the issue does not proceed
- **THEN** the cash is refunded from the same account

### Requirement: SAFE or note received and converted
A SAFE or note (FR-EQ-18) SHALL post to 2700 when received and move the converted amount to 3010 or 3020 when converted, the remainder staying on 2700 until settled under the terms.

Source: PRD-001 §B.7

#### Scenario: SAFE or note received
- **WHEN** a SAFE or note is received
- **THEN** it posts Dr Bank; Cr 2700

#### Scenario: SAFE or note converted
- **WHEN** a SAFE or note converts
- **THEN** it posts Dr 2700 (converted amount); Cr 3010 or 3020
- **AND** the conversion price is per the terms (FR-EQ-18) and the shares follow `RND-SHARE`

#### Scenario: Remainder stays on 2700
- **WHEN** the converted amount is less than the balance
- **THEN** the remainder stays on 2700 until settled under the terms

### Requirement: Founders' balances in the statements
Founders' balances SHALL be presented by person at month end without a posting, and conversions and waivers SHALL be shown as non-cash financing transactions.

Source: PRD-001 §B.7

#### Scenario: Month-end presentation of a net debit
- **GIVEN** a person's net balance on 2500 + 2120 is a debit at a month end
- **WHEN** the balance sheet is produced
- **THEN** it shows under 1250
- **AND** no posting is made

#### Scenario: Conversions and waivers in the cash flow
- **WHEN** the cash flow statement is produced
- **THEN** conversions and waivers are not cash flows
- **AND** they are listed as non-cash financing transactions (FR-EQ-10)
