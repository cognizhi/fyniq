# controls/checks Specification

## Purpose
How Fyniq keeps the books trustworthy without a person re-checking every figure: the checks engine with the workbook's 37 checks carried over, the books health score, the audit trail explorer, period lock and approval controls, continuous nightly ledger verification and the user access review. Derived from PRD-001 §8.15 and Appendix C.1.

## Requirements

### Requirement: FR-CTL-01 Checks engine
The system SHALL run a checks engine with every live workbook check (36, because C.1 #34 is retired) plus the new platform checks (Appendix C), each with a severity, a plain-English explanation, a drill-down to the offending items, and an owner.

Source: PRD-001 §8.15 · Priority M · Phase P1

#### Scenario: A finding explains itself and has an owner
- **WHEN** a check finds a problem
- **THEN** it shows a plain-English explanation of the problem and a drill-down to the offending items
- **AND** it names the check's owner

#### Scenario: Blocking severity blocks the close
- **WHEN** a check whose severity is blocking finds a problem
- **THEN** the problem blocks the close

#### Scenario: Review severity needs review
- **WHEN** a check whose severity is review finds a problem
- **THEN** the offending items need review

#### Scenario: Checks run on every relevant change
- **WHEN** a change relevant to a check is saved
- **THEN** that check runs again

#### Scenario: Checks run nightly
- **WHEN** the nightly run starts
- **THEN** every check runs

### Requirement: FR-CTL-02 Books health score
The system SHALL show a books health score on Home (OK / review / problems) with counts by severity.

Source: PRD-001 §8.15 · Priority M · Phase P1

#### Scenario: Home shows the books health
- **WHEN** a user opens Home
- **THEN** the books health score shows OK, review or problems
- **AND** it shows the count of findings for each severity

### Requirement: FR-CTL-03 Audit trail explorer
The system SHALL provide an audit trail explorer that filters the audit trail, shows before/after values, exports it and verifies its integrity (hash chain) on demand.

Source: PRD-001 §8.15 · Priority M · Phase P1

#### Scenario: Filter the audit trail
- **WHEN** a user filters the audit trail by user, AI, record, field, time or action
- **THEN** only the matching audit events are shown

#### Scenario: Before and after values
- **WHEN** a user opens an audit event
- **THEN** its before and after values are shown

#### Scenario: Export the audit trail
- **WHEN** a user exports from the audit trail explorer
- **THEN** the audit trail is exported

#### Scenario: Verify integrity on demand
- **WHEN** a user asks to verify the audit trail's integrity
- **THEN** the hash chain is verified
- **AND** the result is shown

### Requirement: FR-CTL-04 Period lock, approvals, SoD, team mode
The system SHALL enforce the period lock, approvals, segregation of duties (SoD) and team mode as specified in §6 and FR-GL-07/08.

Source: PRD-001 §8.15 · Priority M · Phase P1

#### Scenario: Period close and lock follow FR-GL-07/08
- **WHEN** a period is closed, or a posting is dated in a closed period
- **THEN** the close and the period lock behave as FR-GL-07 and FR-GL-08 specify

#### Scenario: Approvals, SoD and team mode follow §6
- **WHEN** an action needs an approval or is subject to SoD or the team mode
- **THEN** the approval, SoD and team-mode rules of §6 apply to it

### Requirement: FR-CTL-05 Continuous ledger verification
The system SHALL verify the ledger every night and show the result on Checks and in the Compliance Center.

Source: PRD-001 §8.15 · Priority M · Phase P1

#### Scenario: Balances are recomputed from journal lines
- **WHEN** the nightly verification runs
- **THEN** it recomputes balances from the journal lines and compares them with the materialised balances

#### Scenario: Subledgers are compared with control accounts
- **WHEN** the nightly verification runs
- **THEN** it compares the subledgers (AR, AP, payroll liabilities, fixed assets, GST, prepayments, deferred revenue, expense claims) with their control accounts

#### Scenario: Hash chain and anchors are verified
- **WHEN** the nightly verification runs
- **THEN** it verifies the audit hash chain and its anchors

#### Scenario: Result is shown
- **WHEN** the nightly verification completes
- **THEN** its result is shown on Checks and in the Compliance Center

### Requirement: FR-CTL-06 User access review report
The system SHALL produce a user access review report (who has what, last sign-in, SoD conflicts) with quarterly attestation.

Source: PRD-001 §8.15 · Priority S · Phase P1

#### Scenario: Report contents
- **WHEN** the user access review report is produced
- **THEN** it shows each user's access, their last sign-in and any SoD conflicts

#### Scenario: Quarterly attestation
- **WHEN** the access review for a quarter is completed
- **THEN** it is attested for that quarter

### Requirement: C.1 #1 Debits equal credits
The system SHALL check, on every relevant change and nightly, that debits equal credits in the books; a failure blocks the close.

Source: PRD-001 §C.1

#### Scenario: Debits and credits differ
- **WHEN** the debits in the books do not equal the credits
- **THEN** the check fails and blocks the close

#### Scenario: Debits equal credits
- **WHEN** the debits equal the credits
- **THEN** the check passes

### Requirement: C.1 #2 Balance sheet balances every month
The system SHALL check, on every relevant change and nightly, that the balance sheet balances for every month; a failure blocks the close.

Source: PRD-001 §C.1

#### Scenario: A month's balance sheet does not balance
- **WHEN** the balance sheet for any month does not balance
- **THEN** the check fails for that month and blocks the close

#### Scenario: Every month balances
- **WHEN** the balance sheet balances for every month
- **THEN** the check passes

### Requirement: C.1 #3 Cash flow agrees to bank balances
The system SHALL check, on every relevant change and nightly, that the cash flow agrees to the bank balances; a failure blocks the close.

Source: PRD-001 §C.1

#### Scenario: Cash flow differs from the bank balances
- **WHEN** the cash flow does not agree to the bank balances
- **THEN** the check fails and blocks the close

#### Scenario: Cash flow agrees
- **WHEN** the cash flow agrees to the bank balances
- **THEN** the check passes

### Requirement: C.1 #4 P&L includes every account
The system SHALL check, on every relevant change and nightly, that the P&L includes every account that belongs in it; a failure blocks the close.

Source: PRD-001 §C.1

#### Scenario: An account is missing from the P&L
- **WHEN** an account that belongs in the P&L is not included in it
- **THEN** the check fails and blocks the close

#### Scenario: Every account is included
- **WHEN** the P&L includes every account that belongs in it
- **THEN** the check passes

### Requirement: C.1 #5 Balance sheet includes every account
The system SHALL check, on every relevant change and nightly, that the balance sheet includes every account that belongs in it; a failure blocks the close.

Source: PRD-001 §C.1

#### Scenario: An account is missing from the balance sheet
- **WHEN** an account that belongs in the balance sheet is not included in it
- **THEN** the check fails and blocks the close

#### Scenario: Every account is included
- **WHEN** the balance sheet includes every account that belongs in it
- **THEN** the check passes

### Requirement: C.1 #6 Opening balances balance
The system SHALL check, on every relevant change and nightly, that the opening balances balance; a failure blocks the close.

Source: PRD-001 §C.1

#### Scenario: Opening balances do not balance
- **WHEN** the opening balances do not balance
- **THEN** the check fails and blocks the close

#### Scenario: Opening balances balance
- **WHEN** the opening balances balance
- **THEN** the check passes

### Requirement: C.1 #7 System accounts exist
The system SHALL check, on every relevant change and nightly, that the system accounts exist; a failure blocks the close.

Source: PRD-001 §C.1

#### Scenario: A system account is missing
- **WHEN** a system account does not exist in the chart of accounts
- **THEN** the check fails and blocks the close

#### Scenario: Every system account exists
- **WHEN** every system account exists
- **THEN** the check passes

### Requirement: C.1 #8 Chart of accounts problems
The system SHALL check the chart of accounts for problems on every relevant change and nightly; depending on the problem, a finding blocks the close or flags it for review.

Source: PRD-001 §C.1

#### Scenario: A posting, default or rule points to a bad account
- **WHEN** a posting, default or rule points to a missing, inactive or non-postable account
- **THEN** the check reports ✖ and blocks the close

#### Scenario: Mapping or naming gaps
- **WHEN** an account has no statement mapping, or two accounts share a name
- **THEN** the check reports ⚠ for review

#### Scenario: No chart of accounts problems
- **WHEN** the chart of accounts has no problem
- **THEN** the check passes

### Requirement: C.1 #9 Contacts, employees and FX rates problems
The system SHALL check contacts, employees and FX rates for problems on every relevant change and nightly, and flag each problem for review.

Source: PRD-001 §C.1

#### Scenario: A contact, employee or FX rate has a problem
- **WHEN** a contact, an employee or an FX rate has a problem
- **THEN** the check flags it for review

#### Scenario: Duplicate entries in the monthly FX table
- **GIVEN** the organisation's monthly FX table has duplicate entries for a currency and month
- **WHEN** the check runs
- **THEN** it flags the duplicate for review until it is resolved (ENG-40)

#### Scenario: No problems
- **WHEN** no contact, employee or FX rate has a problem
- **THEN** the check passes

### Requirement: C.1 #10 Transactions that cannot post
The system SHALL list, on every relevant change and nightly, the transactions that cannot post; any such item blocks the close.

Source: PRD-001 §C.1

#### Scenario: A transaction cannot post
- **WHEN** a transaction cannot post
- **THEN** the check lists it as unable to post
- **AND** it blocks the close

#### Scenario: Every transaction can post
- **WHEN** every transaction can post
- **THEN** the check passes

### Requirement: C.1 #11 Transactions to review
The system SHALL flag for review, on every relevant change and nightly, the transactions that need review.

Source: PRD-001 §C.1

#### Scenario: A transaction needs review
- **WHEN** a transaction needs review
- **THEN** the check flags it for review

#### Scenario: Nothing to review
- **WHEN** no transaction needs review
- **THEN** the check passes

### Requirement: C.1 #12 Dated in the future
The system SHALL flag for review, on every relevant change and nightly, transactions dated in the future.

Source: PRD-001 §C.1

#### Scenario: A transaction is dated in the future
- **WHEN** a transaction is dated after today
- **THEN** the check flags it for review

#### Scenario: No future-dated transactions
- **WHEN** no transaction is dated after today
- **THEN** the check passes

### Requirement: C.1 #13 Invoices that cannot post
The system SHALL list, on every relevant change and nightly, the invoices that cannot post; any such invoice blocks the close.

Source: PRD-001 §C.1

#### Scenario: An invoice cannot post
- **WHEN** an invoice cannot post
- **THEN** the check lists it as unable to post
- **AND** it blocks the close

#### Scenario: Every invoice can post
- **WHEN** every invoice can post
- **THEN** the check passes

### Requirement: C.1 #14 Invoices to review
The system SHALL flag for review, on every relevant change and nightly, the invoices that need review.

Source: PRD-001 §C.1

#### Scenario: An invoice needs review
- **WHEN** an invoice needs review
- **THEN** the check flags it for review

#### Scenario: No invoice to review
- **WHEN** no invoice needs review
- **THEN** the check passes

### Requirement: C.1 #15 Invoices overdue more than 90 days
The system SHALL flag for review, on every relevant change and nightly, invoices overdue by more than 90 days.

Source: PRD-001 §C.1

#### Scenario: An invoice is overdue by more than 90 days
- **WHEN** an invoice is overdue by more than 90 days
- **THEN** the check flags it for review

#### Scenario: An invoice overdue by 90 days or fewer
- **WHEN** an invoice is overdue by 90 days or fewer
- **THEN** the check does not flag it

### Requirement: C.1 #16 Bills that cannot post
The system SHALL list, on every relevant change and nightly, the bills that cannot post; any such bill blocks the close.

Source: PRD-001 §C.1

#### Scenario: A bill cannot post
- **WHEN** a bill cannot post
- **THEN** the check lists it as unable to post
- **AND** it blocks the close

#### Scenario: Every bill can post
- **WHEN** every bill can post
- **THEN** the check passes

### Requirement: C.1 #17 Bills to review
The system SHALL flag for review, on every relevant change and nightly, the bills that need review.

Source: PRD-001 §C.1

#### Scenario: A bill needs review
- **WHEN** a bill needs review
- **THEN** the check flags it for review

#### Scenario: No bill to review
- **WHEN** no bill needs review
- **THEN** the check passes

### Requirement: C.1 #18 Journals to fix or review
The system SHALL check journals on every relevant change and nightly: a journal that cannot post blocks the close, and a journal that needs review is flagged for review.

Source: PRD-001 §C.1

#### Scenario: A journal cannot post
- **WHEN** a journal cannot post
- **THEN** the check lists it as unable to post
- **AND** it blocks the close

#### Scenario: A journal needs review
- **WHEN** a journal needs review
- **THEN** the check flags it for review

#### Scenario: No journal problems
- **WHEN** every journal can post and none needs review
- **THEN** the check passes

### Requirement: C.1 #19 Pay lines that cannot post
The system SHALL list, on every relevant change and nightly, the pay lines that cannot post; any such pay line blocks the close.

Source: PRD-001 §C.1

#### Scenario: A pay line cannot post
- **WHEN** a pay line cannot post
- **THEN** the check lists it as unable to post
- **AND** it blocks the close

#### Scenario: Every pay line can post
- **WHEN** every pay line can post
- **THEN** the check passes

### Requirement: C.1 #20 Pay lines to review
The system SHALL flag for review, on every relevant change and nightly, the pay lines that need review.

Source: PRD-001 §C.1

#### Scenario: A pay line needs review
- **WHEN** a pay line needs review
- **THEN** the check flags it for review

#### Scenario: No pay line to review
- **WHEN** no pay line needs review
- **THEN** the check passes

### Requirement: C.1 #21 Salaries possibly counted twice
The system SHALL flag for review, on every relevant change and nightly, salaries possibly counted twice: posted both by a pay run and by direct postings to salary accounts.

Source: PRD-001 §C.1

#### Scenario: Pay run and direct postings to salary accounts
- **WHEN** salaries are posted by a pay run and there are also direct postings to salary accounts
- **THEN** the check flags them for review as salaries possibly counted twice

#### Scenario: Salaries posted only by pay runs
- **WHEN** there are no direct postings to salary accounts
- **THEN** the check passes

### Requirement: C.1 #22 Asset register problems
The system SHALL check the asset register for problems on every relevant change and nightly; depending on the problem, a finding blocks the close or flags it for review.

Source: PRD-001 §C.1

#### Scenario: Register and ledger disagree, or depreciation overruns
- **WHEN** the register disagrees with the cost and accumulated-depreciation accounts (INV-07), or depreciation runs past disposal or below residual value
- **THEN** the check reports ✖ and blocks the close

#### Scenario: Missing asset data
- **WHEN** an asset has no category, useful life or depreciation start
- **THEN** the check reports ⚠ for review

#### Scenario: No asset register problems
- **WHEN** the asset register has no problem
- **THEN** the check passes

### Requirement: C.1 #23 Movement in equity reconciles
The system SHALL check, on every relevant change and nightly, that the movement in equity reconciles; a failure blocks the close.

Source: PRD-001 §C.1

#### Scenario: Movement in equity does not reconcile
- **WHEN** the movement in equity does not reconcile
- **THEN** the check fails and blocks the close

#### Scenario: Movement in equity reconciles
- **WHEN** the movement in equity reconciles
- **THEN** the check passes

### Requirement: C.1 #24 Sales document line problems
The system SHALL flag for review, on every relevant change and nightly, problems on sales document lines.

Source: PRD-001 §C.1

#### Scenario: A sales document line has a problem
- **WHEN** a sales document line has a problem
- **THEN** the check flags it for review

#### Scenario: No line problems
- **WHEN** no sales document line has a problem
- **THEN** the check passes

### Requirement: C.1 #25 Quotes and orders to follow up
The system SHALL flag for review, on every relevant change and nightly, quotes and orders to follow up.

Source: PRD-001 §C.1

#### Scenario: A quote or order needs follow-up
- **WHEN** a quote or an order needs follow-up
- **THEN** the check flags it for review

#### Scenario: Nothing to follow up
- **WHEN** no quote or order needs follow-up
- **THEN** the check passes

### Requirement: C.1 #26 Projects over budget or loss-making
The system SHALL flag for review, on every relevant change and nightly, projects that are over budget or loss-making.

Source: PRD-001 §C.1

#### Scenario: A project is over budget
- **WHEN** a project is over budget
- **THEN** the check flags it for review

#### Scenario: A project is loss-making
- **WHEN** a project is loss-making
- **THEN** the check flags it for review

#### Scenario: Projects within budget and profitable
- **WHEN** no project is over budget or loss-making
- **THEN** the check passes

### Requirement: C.1 #27 Timesheet problems
The system SHALL flag for review, on every relevant change and nightly, timesheet problems.

Source: PRD-001 §C.1

#### Scenario: A timesheet has a problem
- **WHEN** a timesheet has a problem
- **THEN** the check flags it for review

#### Scenario: No timesheet problems
- **WHEN** no timesheet has a problem
- **THEN** the check passes

### Requirement: C.1 #28 Recurring entries due but not created
The system SHALL check, on every relevant change and nightly, for recurring entries that are due but not created, which should be none with the scheduler; a finding is flagged for review, and blocks the close if the scheduler failed.

Source: PRD-001 §C.1

#### Scenario: A recurring entry is due but not created
- **WHEN** a recurring entry is due but has not been created
- **THEN** the check flags it for review

#### Scenario: The scheduler failed
- **GIVEN** the scheduler failed
- **WHEN** a recurring entry is due but has not been created
- **THEN** the check fails and blocks the close

#### Scenario: The scheduler created every due entry
- **WHEN** every recurring entry that is due has been created
- **THEN** the check finds none and passes

#### Scenario: Recurring journals, as built (rev 0.60)
- **GIVEN** an active recurring journal whose occurrence on or before the period's end has no draft
- **WHEN** the period's close checks run
- **THEN** they warn (⚠), or refuse the close (✖) when the occurrence is more than a day before today, as the hourly scheduler then failed
- **AND** the recurring journal itself carries the warning "Due but not created" while an occurrence before today has no draft

### Requirement: C.1 #29 Recurring templates incomplete
The system SHALL check, on every relevant change and nightly, that recurring templates are complete; an incomplete template is an error on itself and, while active, a warning at the close (rev 0.61).

Source: PRD-001 §C.1

#### Scenario: A recurring template is incomplete
- **WHEN** a recurring template is incomplete
- **THEN** the check fails on the template, and the close warns of it (the founders' decision of rev 0.61)

#### Scenario: Every template is complete
- **WHEN** every recurring template is complete
- **THEN** the check passes

#### Scenario: Recurring journals, as built (rev 0.60)
- **GIVEN** a recurring journal whose content the posting rules refuse
- **WHEN** it is saved or read
- **THEN** it is kept, with each problem as an error status on it
- **AND** while it is active, the close checks warn of it (rev 0.61)

### Requirement: C.1 #30 Suspense balance not zero
The system SHALL flag for review, on every relevant change and nightly, a bank suspense balance that is not zero.

Source: PRD-001 §C.1

#### Scenario: Suspense balance is not zero
- **WHEN** the suspense balance is not zero
- **THEN** the check flags it for review

#### Scenario: Suspense balance is zero
- **WHEN** the suspense balance is zero
- **THEN** the check passes

### Requirement: C.1 #31 Negative month-end bank or cash balance
The system SHALL flag for review, on every relevant change and nightly, a bank or cash balance below zero at a month end.

Source: PRD-001 §C.1

#### Scenario: Balance below zero at a month end
- **WHEN** a bank or cash account's balance at a month end is below zero
- **THEN** the check flags it for review

#### Scenario: Balance zero or above at every month end
- **WHEN** every bank and cash balance is zero or above at every month end
- **THEN** the check passes

### Requirement: C.1 #32 Reconciliations not matching
The system SHALL flag for review, on every relevant change and nightly, bank reconciliations that do not match.

Source: PRD-001 §C.1

#### Scenario: A reconciliation does not match
- **WHEN** a bank reconciliation does not match
- **THEN** the check flags it for review

#### Scenario: Every reconciliation matches
- **WHEN** every bank reconciliation matches
- **THEN** the check passes

### Requirement: C.1 #33 Closed periods changed after closing
The system SHALL check, on every relevant change and nightly, that no closed period changed after closing, which should be impossible; a finding means the database lock was bypassed and blocks the close.

Source: PRD-001 §C.1

#### Scenario: A closed period changed after closing
- **WHEN** a closed period has changed after it was closed
- **THEN** the check fails and blocks the close
- **AND** the finding shows that the database lock was bypassed

#### Scenario: No closed period changed
- **WHEN** no closed period has changed after it was closed
- **THEN** the check passes

### Requirement: C.1 #34 Workbook fast mode stale, replaced
The workbook's check for a stale fast mode SHALL NOT be carried over as a check of its own and has no severity; C.2 #12 replaces it.

Source: PRD-001 §C.1

#### Scenario: No fast-mode staleness check
- **WHEN** the checks catalogue is listed
- **THEN** it has no check for a stale fast mode
- **AND** a scheduled job that failed or is late is reported by C.2 #12 instead

### Requirement: C.1 #35 Year with postings but no budget
The system SHALL flag for review, on every relevant change and nightly, a year that has postings but no budget.

Source: PRD-001 §C.1

#### Scenario: A year with postings has no budget
- **WHEN** a year has postings and no budget
- **THEN** the check flags it for review

#### Scenario: The year has a budget
- **WHEN** every year with postings has a budget
- **THEN** the check passes

### Requirement: C.1 #36 Cash runway below 6 months
The system SHALL flag for review, on every relevant change and nightly, a cash runway below 6 months.

Source: PRD-001 §C.1

#### Scenario: Runway below 6 months
- **WHEN** the cash runway is below 6 months
- **THEN** the check flags it for review

#### Scenario: Runway of 6 months or more
- **WHEN** the cash runway is 6 months or more
- **THEN** the check passes

### Requirement: C.1 #37 GST registration threshold reached or expected
The system SHALL flag for review, on every relevant change and nightly, a GST registration threshold that is reached or expected to be reached.

Source: PRD-001 §C.1

#### Scenario: Threshold reached
- **WHEN** the GST registration threshold is reached
- **THEN** the check flags it for review

#### Scenario: Threshold expected
- **WHEN** the GST registration threshold is expected to be reached
- **THEN** the check flags it for review

#### Scenario: Threshold neither reached nor expected
- **WHEN** the GST registration threshold is neither reached nor expected to be reached
- **THEN** the check passes
