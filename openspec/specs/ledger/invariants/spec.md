# ledger/invariants Specification

## Purpose
The ledger invariants that the database enforces (constraints, triggers, privileges) and that tests and the nightly verification job verify, including the workbook's own checks carried over. Derived from PRD-001 §10.7.

## Requirements

### Requirement: INV-01 Every journal entry balances
Each journal entry SHALL balance: Σ debits = Σ credits in base currency, and per transaction currency for single-currency entries.

Source: PRD-001 §10.7

#### Scenario: Unbalanced entry fails at commit
- **GIVEN** a journal entry whose debits and credits differ in base currency
- **WHEN** its transaction commits
- **THEN** the deferred constraint trigger rejects the commit

#### Scenario: Single-currency entry balances in its currency
- **GIVEN** a single-currency journal entry that balances in base currency but not in its transaction currency
- **WHEN** its transaction commits
- **THEN** the deferred constraint trigger rejects the commit

### Requirement: INV-02 Posted entries are immutable
Posted journal entries and lines SHALL be immutable: no UPDATE and no DELETE.

Source: PRD-001 §10.7

#### Scenario: Update or delete of posted history fails
- **WHEN** an UPDATE or DELETE of a posted journal entry or line is attempted
- **THEN** it fails at the database
- **AND** it is blocked both by revoked privileges and by a trigger

### Requirement: INV-03 No posting into a closed period
No entry SHALL have a posting date in a closed period (the hard lock; soft locks are in FR-GL-09).

Source: PRD-001 §10.7

#### Scenario: Entry dated in a closed period fails
- **GIVEN** a closed period
- **WHEN** an entry with a posting date in that period is inserted
- **THEN** the trigger against the period table rejects it

#### Scenario: Trigger follows the lock order
- **WHEN** the trigger checks the period
- **THEN** it runs under the ENG-47 lock order
- **AND** it reads the period row `FOR SHARE`, not `FOR KEY SHARE`, which does not block the close's status update

#### Scenario: Backstop holds for a writer that skips post_entry()
- **GIVEN** a writer that inserts entries without calling `post_entry()`
- **WHEN** it inserts an entry dated in a closed period
- **THEN** the trigger still rejects it

### Requirement: INV-04 Lines post to active leaf accounts of the organisation
Every journal line SHALL post to an active, postable (leaf) account that exists in the organisation.

Source: PRD-001 §10.7

#### Scenario: Inactive or non-leaf account is rejected
- **WHEN** a line posts to an inactive account or to an account that is not postable (not a leaf)
- **THEN** a trigger rejects it

#### Scenario: Account of another organisation is rejected
- **WHEN** a line references an account that does not exist in the line's organisation
- **THEN** the composite tenant foreign keys (§11.4) reject it

#### Scenario: G4-067 Cross-tenant references and TRUNCATE fail at the database
- **GIVEN** the application role
- **WHEN** it inserts a journal line or evidence link that references another organisation's existing account or document UUID
- **AND** it attempts to TRUNCATE a tenant table
- **THEN** both fail at the database

### Requirement: INV-05 Amounts at minor-unit scale and consistent with RND-FX
Amounts SHALL have the currency's minor-unit scale, and base amounts SHALL be consistent with amount × rate under `RND-FX`, with two exceptions: base-only lines from FX revaluation, and the residual a payment of bills clears on accounts payable.

Source: PRD-001 §10.7

#### Scenario: Amount beyond the currency's scale is rejected
- **WHEN** a line's amount has more decimal places than its currency's minor units
- **THEN** the database rejects it through its CHECK constraints and trigger

#### Scenario: Base amount inconsistent with RND-FX is rejected
- **WHEN** a line's base amount differs from amount × rate rounded under `RND-FX`
- **THEN** the database rejects it through its CHECK constraints and trigger

#### Scenario: Base amount that rounds to zero
- **GIVEN** a non-zero line whose base amount rounds to 0.00 under `RND-FX`
- **WHEN** it is posted
- **THEN** it is consistent and posts with base amount 0.00
- **AND** a manual journal line whose amount is zero is refused (ENG-24)

#### Scenario: Base-only line from FX revaluation is accepted
- **GIVEN** a base-only line (transaction-currency amount 0, base amount not 0)
- **WHEN** it comes from the FX-revaluation source or its automatic reversal, on an account flagged as a monetary foreign-currency item (FR-GL-04), in an entry whose base-only lines are offset by the unrealised-FX account
- **THEN** it is accepted

#### Scenario: Any other base-only line is rejected
- **GIVEN** a base-only line
- **WHEN** it comes from another source, or posts to an account not flagged as a monetary foreign-currency item, or its entry's base-only lines are not offset by the unrealised-FX account
- **THEN** a separate trigger for base-only lines rejects it

#### Scenario: The residual of a bill paid in parts (rev 0.77)
- **GIVEN** a base-only line from a payment of bills
- **WHEN** it is on the accounts-payable account and at most 1.00 in the base currency, or comes on the same terms from a credit application (rev 0.81)
- **THEN** `journal_line_base_only` accepts it
- **AND** on any other account, or above 1.00, it refuses it (FY008)

#### Scenario: Built as a check at commit (rev 0.72)
- **WHEN** an entry with a base-only line commits
- **THEN** `journal_line_base_only` refuses it (FY008) unless its source is `fx_revaluation`, the line's account is marked revalue, the line is in a foreign currency, and every other line is a base-currency line on the unrealised-FX account
- **AND** the row check accepts a base-only line only in a foreign currency

### Requirement: INV-06 Tenant isolation on every row
Every row SHALL carry its organisation ID and SHALL be visible only through row-level security.

Source: PRD-001 §10.7

#### Scenario: Every row carries its organisation
- **WHEN** a row is stored
- **THEN** it carries its organisation ID

#### Scenario: Rows of another organisation are invisible
- **GIVEN** a session for one organisation
- **WHEN** it reads any table
- **THEN** row-level security policies show it only that organisation's rows
- **AND** tests verify the policies

### Requirement: INV-07 Subledgers equal control accounts
Each subledger SHALL equal its control account: AR, AP, payroll liabilities, the fixed asset register, the GST control, prepayment and deferred-revenue schedules, and expense claims, verified by the nightly verification and on Checks.

Source: PRD-001 §10.7

#### Scenario: Receivables and payables
- **WHEN** the subledgers are verified
- **THEN** AR (open documents and unapplied customer credits) equals 1100
- **AND** AP (open documents and unapplied supplier credits) equals 2010

#### Scenario: Payroll liabilities and fixed assets
- **WHEN** the subledgers are verified
- **THEN** the payroll liabilities equal their control accounts
- **AND** the fixed asset register equals the cost and accumulated depreciation accounts

#### Scenario: Schedules and expense claims
- **WHEN** the subledgers are verified
- **THEN** open prepayment schedules equal 1210 and open deferred-revenue schedules equal 2400 (FR-GL-03)
- **AND** open expense claims equal 2150 (FR-AP-07)

#### Scenario: GST control rolls forward per return period
- **WHEN** a GST return period is verified
- **THEN** opening balance + output tax − input tax ± approved adjustments − payments to IRAS + refunds from IRAS = closing balance of the GST control
- **AND** Box 6 and Box 7 equal the tax postings of the period plus adjustments
- **AND** Box 8 = Box 6 − Box 7

#### Scenario: Verified nightly and on Checks
- **WHEN** the nightly verification runs or Checks are shown
- **THEN** each subledger is compared with its control account

#### Scenario: Manual journal to a control account
- **WHEN** a manual journal posts to a control account
- **THEN** it posts
- **AND** from P1 it shows ⚠ naming the subledger it moves (ENG-21)

#### Scenario: Accounts payable at the close (rev 0.78)
- **WHEN** a period's close checks run
- **THEN** the AP sub-ledger, read from the posted bills dated by the period's end and not voided by then, less their allocations by then, and the payments' unapplied amounts, is compared with 2010's balance by source type, per currency and in carrying amounts
- **AND** a difference no source explains is ✖ and blocks the close, and amounts on 2010 from opening balances or manual journals are ⚠

#### Scenario: G4-036 GST roll-forward over a quarter
- **GIVEN** a quarter with output tax, input tax, a payment to IRAS and a refund
- **WHEN** the quarter's GST control is verified
- **THEN** the 2200 roll-forward reconciles
- **AND** Box 8 = Box 6 − Box 7
- **AND** payments and refunds never appear in the boxes

#### Scenario: G4-105 Schedule out of step with its control account
- **GIVEN** a deferred-revenue schedule's open balance differs from account 2400 by S$10.00 at a month end
- **AND** separately, a supplier billed in each of the last three months has no bill for the month being closed
- **WHEN** the month is closed
- **THEN** INV-07 verification and C.2 #2 show ✖ and the close cannot complete
- **AND** in P2, C.2 #52 shows ⚠ for the missing bill

### Requirement: INV-08 Materialised balances equal recomputation
Materialised balances SHALL equal the recomputation from journal lines.

Source: PRD-001 §10.7

#### Scenario: Nightly recomputation matches
- **WHEN** the nightly verification runs
- **THEN** each materialised balance equals the balance recomputed from the journal lines

### Requirement: INV-09 Trial balance nets to zero
The trial balance SHALL net to zero for every period (workbook TB_Check).

Source: PRD-001 §10.7

#### Scenario: Trial balance of any period
- **WHEN** the trial balance of any period is produced
- **THEN** it nets to zero
- **AND** this follows from INV-01 and is verified

### Requirement: INV-10 Balance sheet balances and statements include every account
The balance sheet SHALL balance for every period (BS_Chk), and the P&L and balance sheet SHALL include every account (PL_Chk, BS_Uncl).

Source: PRD-001 §10.7

#### Scenario: Balance sheet balances
- **WHEN** the balance sheet of any period is verified
- **THEN** it balances
- **AND** the result is shown on Checks

#### Scenario: No account left out
- **WHEN** the P&L and balance sheet are verified
- **THEN** every account appears in one of them
- **AND** the result is shown on Checks

### Requirement: INV-11 Cash flow and equity reconcile
The cash flow SHALL agree to the money-account balances (CF_Chk), and the movement in equity SHALL reconcile (BS_EqChk).

Source: PRD-001 §10.7

#### Scenario: Cash flow agrees to money accounts
- **WHEN** the cash flow of a period is verified
- **THEN** it agrees to the movement in the money-account balances
- **AND** the result is shown on Checks

#### Scenario: Equity movement reconciles
- **WHEN** the movement in equity of a period is verified
- **THEN** it reconciles
- **AND** the result is shown on Checks

### Requirement: INV-12 Audit chain unbroken
The audit chain SHALL be unbroken and SHALL match its signed anchors.

Source: PRD-001 §10.7

#### Scenario: Nightly verification of the audit chain
- **WHEN** the nightly verification runs (§11.5)
- **THEN** the audit chain is unbroken
- **AND** it matches its signed anchors

### Requirement: INV-13 Share capital only from the register
Share-capital accounts SHALL change only through the register of share issues or opening balances, and the per-person sub-ledgers SHALL equal 2500 and 2120.

Source: PRD-001 §10.7

#### Scenario: Share capital posted from another source is rejected
- **WHEN** a line posts to a share-capital account from a source other than the register of share issues or opening balances
- **THEN** the trigger on the posting source rejects it

#### Scenario: Only those two sources (rev 0.49)
- **WHEN** a posting source other than the register of share issues and the opening balances is added
- **THEN** the trigger refuses its lines to share capital without being told of it

#### Scenario: Per-person sub-ledgers equal 2500 and 2120
- **WHEN** the nightly verification runs
- **THEN** the per-person sub-ledgers equal accounts 2500 and 2120

#### Scenario: Manual journal to share capital, from P0
- **WHEN** a manual journal line posts to a share-capital account (3010, 3020)
- **THEN** it is refused, from P0
- **AND** the chart of accounts marks which accounts are share capital
