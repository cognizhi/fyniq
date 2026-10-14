# compliance/financial-controls Specification

## Purpose
The ledger-integrity, financial, audit-trail and statutory-deadline controls of the control registry (`compliance/controls/`): each control, its automated check and frequency, the evidence kept, and the frameworks it maps to (ISO = ISO/IEC 27001:2022 Annex A; SOC2 = SOC 2 Trust Services Criteria). Derived from PRD-001 §F.1, §F.2 and §F.8.

## Requirements

### Requirement: CTL-FIN-01 Every journal entry balances
Every journal entry SHALL balance. The control is checked on every change in CI and nightly, and its test reports and verification log are kept as evidence.

Source: PRD-001 §F.1

#### Scenario: The database rejects an unbalanced entry
- **WHEN** a journal entry whose debits and credits do not balance is committed
- **THEN** the database deferred trigger rejects it

#### Scenario: Property tests on every change
- **WHEN** a change runs through CI
- **THEN** property tests check that every journal entry balances
- **AND** the test reports are kept as evidence

#### Scenario: Nightly trial balance recomputation
- **WHEN** the nightly verification runs
- **THEN** the trial balance is recomputed
- **AND** the verification log is kept as evidence

#### Scenario: Framework mapping
- **WHEN** the control is listed in the control registry
- **THEN** it is mapped to SOC2 PI1 and COSO P10

### Requirement: CTL-FIN-02 Posted ledger and audit tables are immutable
The posted ledger and the audit tables SHALL be immutable. The control is checked on every change in CI and nightly, with a grant snapshot and a test report kept as evidence.

Source: PRD-001 §F.1

#### Scenario: Application roles lack destructive privileges
- **WHEN** a change runs through CI
- **THEN** the check confirms that the application roles lack UPDATE, DELETE and TRUNCATE privileges
- **AND** a grant snapshot is kept as evidence

#### Scenario: Attack tests fail to change posted rows
- **WHEN** the database invariant attack tests run (QA-ENG-05)
- **THEN** every attempt to change the posted ledger or the audit tables fails
- **AND** the test report is kept as evidence

#### Scenario: Checked on every change and nightly
- **WHEN** the control's checks are scheduled
- **THEN** they run on every change in CI and nightly, the nightly run being the grant snapshot and immutability verification (§10.7)

#### Scenario: Framework mapping
- **WHEN** the control is listed in the control registry
- **THEN** it is mapped to ISO 5.33 and 8.15, SOC2 PI1 and CC7.2, and Companies Act s199

### Requirement: CTL-FIN-03 Locked periods cannot change
A locked period SHALL NOT change. The control is checked on every change in CI and nightly, with the snapshot hashes kept as evidence.

Source: PRD-001 §F.1

#### Scenario: Trigger test on every change
- **WHEN** a change runs through CI
- **THEN** a trigger test confirms that the database rejects a change to a locked period

#### Scenario: Nightly comparison with the signed close snapshots
- **WHEN** the nightly check runs
- **THEN** each locked period is compared with its signed close snapshot
- **AND** the snapshot hashes are kept as evidence

#### Scenario: Framework mapping
- **WHEN** the control is listed in the control registry
- **THEN** it is mapped to SOC2 PI1 and COSO P10

### Requirement: CTL-FIN-04 Subledgers agree with control accounts
The subledgers (AR, AP, payroll, assets, GST) SHALL agree with their control accounts. The control is checked nightly, with the verification log kept as evidence.

Source: PRD-001 §F.1

#### Scenario: Nightly verification job
- **WHEN** the nightly verification job runs
- **THEN** the AR, AP, payroll, assets and GST subledgers are each compared with their control account
- **AND** the verification log is kept as evidence

#### Scenario: Framework mapping
- **WHEN** the control is listed in the control registry
- **THEN** it is mapped to SOC2 PI1

### Requirement: CTL-FIN-05 Engine equals the independent reference ledger
The posting engine's results SHALL equal those of the independent reference ledger. The control is checked on every change in CI, with the test report kept as evidence.

Source: PRD-001 §F.1

#### Scenario: Differential tests on every change
- **WHEN** a change runs through CI
- **THEN** differential tests on the golden and generated data compare the engine's results with the reference ledger's
- **AND** the test report is kept as evidence

#### Scenario: Framework mapping
- **WHEN** the control is listed in the control registry
- **THEN** it is mapped to SOC2 PI1

### Requirement: CTL-FIN-06 Statutory parameter packs are sourced, dual-approved and pinned-tested
Every statutory parameter pack SHALL be sourced, dual-approved and covered by pinned tests. The control is checked on every change in CI and each January, with the pack record and a test report kept as evidence.

Source: PRD-001 §F.1

#### Scenario: CI fails on a bad current-year pack
- **GIVEN** the current year's pack is missing, unsourced or fails its pinned tests
- **WHEN** a change runs through CI
- **THEN** CI fails

#### Scenario: Checked each January
- **WHEN** January begins
- **THEN** the control is checked against the pack for the new current year

#### Scenario: Pack evidence
- **WHEN** the control is checked
- **THEN** the pack record and the test report are kept as evidence

#### Scenario: Framework mapping
- **WHEN** the control is listed in the control registry
- **THEN** it is mapped to IRAS, the CPF Act and SOC2 PI1

### Requirement: CTL-FIN-07 Segregation of duties per team mode
Segregation of duties SHALL hold for each team mode, and in Founder mode the compensating controls SHALL operate. The control is checked on every change in CI, nightly and weekly, with scan output and review sign-offs kept as evidence.

Source: PRD-001 §F.1

#### Scenario: Authorisation-matrix tests on every change
- **WHEN** a change runs through CI
- **THEN** the authorisation-matrix tests check the segregation-of-duties rules of each team mode

#### Scenario: Nightly segregation-of-duties conflict scan
- **WHEN** the nightly scan runs
- **THEN** it looks for segregation-of-duties conflicts
- **AND** its scan output is kept as evidence

#### Scenario: Weekly overdue-review check
- **WHEN** the weekly check runs
- **THEN** it looks for overdue reviews of the Founder-mode compensating controls (FM-3)
- **AND** the review sign-offs are kept as evidence

#### Scenario: Framework mapping
- **WHEN** the control is listed in the control registry
- **THEN** it is mapped to ISO 5.3, SOC2 CC6.1 and COSO P10

### Requirement: CTL-FIN-08 Payments and payee bank changes are dual-controlled
Payments and changes to payee bank details SHALL be dual-controlled. The control is checked on every change in CI and nightly, with the approval records kept as evidence.

Source: PRD-001 §F.1

#### Scenario: Self-approval rejected
- **WHEN** a change runs through CI
- **THEN** the self-approval rejection test confirms that a user cannot approve their own payment or payee bank change

#### Scenario: Nightly check of released payments
- **WHEN** the nightly check runs
- **THEN** every released payment has two distinct approvers, or is a founder break-glass release within FM-8's limits with its post-review open or completed on time
- **AND** the approval records are kept as evidence

#### Scenario: A released payment without two approvers fails the check
- **GIVEN** a released payment with a single approver that is not a founder break-glass release within FM-8's limits
- **WHEN** the nightly check runs
- **THEN** the check fails for that payment

#### Scenario: Framework mapping
- **WHEN** the control is listed in the control registry
- **THEN** it is mapped to COSO P8 and SOC2 CC6.1

### Requirement: CTL-FIN-09 Monthly close with clean checks, signed reconciliations and a non-preparer attestation
A monthly close SHALL complete only when the checks are clean, the reconciliations are signed and someone other than the preparer has attested it. The control acts at each close, with the close record kept as evidence.

Source: PRD-001 §F.1

#### Scenario: Close blocked by open items
- **GIVEN** a check is not clean or a reconciliation is not signed
- **WHEN** the monthly close is completed
- **THEN** the close cannot complete

#### Scenario: Close blocked without a non-preparer attestation
- **GIVEN** the close has no attestation by someone other than the preparer
- **WHEN** the monthly close is completed
- **THEN** the close cannot complete

#### Scenario: Close record kept
- **WHEN** a monthly close completes
- **THEN** its close record is kept as evidence

#### Scenario: Framework mapping
- **WHEN** the control is listed in the control registry
- **THEN** it is mapped to COSO P16 and SOC2 PI1

### Requirement: CTL-FIN-10 Monthly review of manual journals and unusual entries
Manual journals and unusual entries SHALL be reviewed every month. The control runs monthly, with the signed review kept as evidence.

Source: PRD-001 §F.1

#### Scenario: Monthly report generated
- **WHEN** a month is reviewed
- **THEN** a report of its manual journals and unusual entries is generated

#### Scenario: Sign-off due date monitored
- **GIVEN** the monthly review has a sign-off due date
- **WHEN** the due date passes without a sign-off
- **THEN** the monitor detects the missing sign-off

#### Scenario: Signed review kept
- **WHEN** the review is signed off
- **THEN** the signed review is kept as evidence

#### Scenario: Framework mapping
- **WHEN** the control is listed in the control registry
- **THEN** it is mapped to ISA 240 expectations and COSO P16

### Requirement: CTL-FIN-11 Founders' balances approved independently and reconciled
Settlements, conversions and waivers of founders' balances SHALL be approved by someone other than the beneficiary (FM-9) with the resolutions attached; share capital SHALL change only through the register of share issues; and the per-person sub-ledgers SHALL agree.

Source: PRD-001 §F.1

#### Scenario: Approval and trigger tests on every change
- **WHEN** a change runs through CI
- **THEN** the approval-matrix tests confirm that the beneficiary cannot approve the settlement, conversion or waiver of their own balance
- **AND** the INV-13 trigger tests confirm that share capital changes only through the register of share issues

#### Scenario: Nightly sub-ledger reconciliation
- **WHEN** the nightly reconciliation runs
- **THEN** the per-person sub-ledgers are reconciled to accounts 2500 and 2120 (INV-13)

#### Scenario: Allotment lodgement monitored
- **WHEN** shares are allotted
- **THEN** the allotment-lodgement monitor tracks the lodgement of the allotment

#### Scenario: Evidence kept
- **WHEN** the control is checked, on every change in CI, nightly and on each event
- **THEN** the approval records, the resolutions and the lodgement evidence are kept as evidence

#### Scenario: Framework mapping
- **WHEN** the control is listed in the control registry
- **THEN** it is mapped to COSO P10, SOC2 PI1 and Companies Act s63, s63B, s161 and s162

### Requirement: CTL-FIN-12 Equity register integrity
The cap table SHALL be computed only from approved, evidenced equity transactions; the register's paid-up capital SHALL equal accounts 3010 + 3020; and the register SHALL reconcile to ACRA's register of members.

Source: PRD-001 §F.1

#### Scenario: Trigger tests on every change
- **WHEN** a change runs through CI
- **THEN** the INV-13 trigger tests pass

#### Scenario: Nightly reconciliation
- **WHEN** the nightly reconciliation runs
- **THEN** the register's paid-up capital is reconciled to accounts 3010 + 3020
- **AND** the reconciliation log is kept as evidence

#### Scenario: Each uploaded ACRA extract is checked
- **WHEN** an ACRA extract is uploaded
- **THEN** the register is checked against ACRA's register of members in that extract

#### Scenario: Cap table only from evidenced transactions
- **WHEN** the cap table is computed
- **THEN** it uses only approved, evidenced equity transactions
- **AND** the transaction evidence is kept as evidence

#### Scenario: Framework mapping
- **WHEN** the control is listed in the control registry
- **THEN** it is mapped to COSO P10, SOC2 PI1 and the Companies Act (ACRA's electronic register of members)

### Requirement: CTL-AUD-01 Every state change emits an audit event in the same transaction
Every state change SHALL emit an audit event in the same transaction. The control is checked on every change in CI and nightly, with the test report kept as evidence.

Source: PRD-001 §F.2

#### Scenario: Test per command on every change
- **WHEN** a change runs through CI
- **THEN** a test for each command confirms that it emits its audit event in the same transaction as its state change
- **AND** the test report is kept as evidence

#### Scenario: Nightly row-capture reconciliation
- **WHEN** the nightly reconciliation runs
- **THEN** the captured row changes are reconciled with the audit events

#### Scenario: Framework mapping
- **WHEN** the control is listed in the control registry
- **THEN** it is mapped to ISO 8.15, SOC2 CC7.2 and Companies Act s199

### Requirement: CTL-AUD-02 Audit hash chain intact and anchored hourly
The audit hash chain SHALL stay intact and SHALL be anchored every hour with a signed anchor on WORM storage. The control is checked nightly, with the verification log kept as evidence.

Source: PRD-001 §F.2

#### Scenario: Hourly signed anchor
- **WHEN** an hour passes
- **THEN** the audit hash chain is anchored with a signed anchor kept on WORM storage

#### Scenario: Nightly chain verification
- **WHEN** the nightly verification runs
- **THEN** the audit hash chain is verified against the KMS-signed anchors
- **AND** the verification log is kept as evidence

#### Scenario: Framework mapping
- **WHEN** the control is listed in the control registry
- **THEN** it is mapped to ISO 8.15 and 5.33 and SOC2 CC7.2

### Requirement: CTL-AUD-03 Records retained at least 7 years after FY end
Records SHALL be retained for at least 7 years after the end of their financial year, and their buckets SHALL be retention-locked. The control is checked daily, with a configuration snapshot kept as evidence.

Source: PRD-001 §F.2

#### Scenario: Daily bucket check
- **WHEN** the daily check runs
- **THEN** each bucket has `isLocked=true` and a retention of at least the policy

#### Scenario: Daily check of newly committed objects
- **WHEN** the daily check runs
- **THEN** every object committed since the last run has retention mode Locked and a retain-until date no earlier than the expiry computed from its record

#### Scenario: No orphans and locked log buckets
- **WHEN** the daily check runs
- **THEN** it finds no orphaned objects
- **AND** the log buckets are locked
- **AND** a configuration snapshot is kept as evidence

#### Scenario: Framework mapping
- **WHEN** the control is listed in the control registry
- **THEN** it is mapped to ISO 5.33, IRAS, GST and the Companies Act

### Requirement: CTL-AUD-04 Data Access logs enabled and sunk to the locked audit bucket
Data Access logs SHALL be enabled for Cloud SQL, KMS, Storage and Secret Manager, with their sinks writing to the locked audit bucket. The control is checked daily, with a configuration snapshot kept as evidence.

Source: PRD-001 §F.2

#### Scenario: Daily configuration check
- **WHEN** the daily configuration check runs
- **THEN** it confirms that Data Access logs are enabled for Cloud SQL, KMS, Storage and Secret Manager
- **AND** that their sinks write to the locked audit bucket
- **AND** a configuration snapshot is kept as evidence

#### Scenario: Framework mapping
- **WHEN** the control is listed in the control registry
- **THEN** it is mapped to ISO 8.15 and 8.16 and SOC2 CC7.2

### Requirement: CTL-AUD-05 Exports and sensitive reads are logged
Every export and every sensitive read SHALL be logged. The control is checked on every change in CI and nightly, with the audit events kept as evidence.

Source: PRD-001 §F.2

#### Scenario: Logging tested on every change
- **WHEN** a change runs through CI
- **THEN** a test confirms that exports and sensitive reads are logged

#### Scenario: Nightly sample reconciliation
- **WHEN** the nightly reconciliation runs
- **THEN** a sample of exports and sensitive reads is reconciled with the audit events
- **AND** the audit events are kept as evidence

#### Scenario: Framework mapping
- **WHEN** the control is listed in the control registry
- **THEN** it is mapped to ISO 8.15 and PDPA protection

### Requirement: CTL-AUD-06 Evidence is append-only once approved
Evidence SHALL be append-only once its record is approved, hashed in the audit trail and retention-locked. The control is checked on every change in CI and nightly, with the verification log kept as evidence.

Source: PRD-001 §F.2

#### Scenario: Removing evidence from an approved record fails
- **GIVEN** an approved record with evidence attached
- **WHEN** the CI test tries to remove that evidence
- **THEN** the removal fails

#### Scenario: Nightly hash re-verification
- **WHEN** the nightly verification runs
- **THEN** the evidence hashes are re-verified against the audit trail (C.2 #31)
- **AND** the verification log is kept as evidence

#### Scenario: Framework mapping
- **WHEN** the control is listed in the control registry
- **THEN** it is mapped to ISO 5.33 and 8.15 and SOC2 CC7.2

### Requirement: CTL-STAT-01 GST F5 filed by the due date
The GST F5 return SHALL be filed by its due date, once every in-scope return item has an IRAS acknowledgement ID or a recorded exclusion, or a contingency filing is approved (FR-TAX-11). The control is checked daily, with the filing record kept as evidence.

Source: PRD-001 §F.8

#### Scenario: Daily calendar monitor
- **WHEN** the daily calendar monitor runs
- **THEN** it tracks the GST F5 due date until the return is filed

#### Scenario: Filing gate holds an incomplete return
- **GIVEN** an in-scope return item has neither an IRAS acknowledgement ID nor a recorded exclusion
- **AND** no contingency filing is approved
- **WHEN** the GST F5 is filed
- **THEN** the filing gate stops the filing

#### Scenario: Approved contingency filing passes the gate
- **GIVEN** a contingency filing is approved (FR-TAX-11)
- **WHEN** the GST F5 is filed
- **THEN** the filing gate lets it through
- **AND** the filing record is kept as evidence

#### Scenario: Framework mapping
- **WHEN** the control is listed in the control registry
- **THEN** it is mapped to the GST Act and IRAS's InvoiceNow requirement

### Requirement: CTL-STAT-02 CPF paid by the 14th
CPF SHALL be paid by the 14th. The control is monitored daily from the 10th, with the payment record kept as evidence.

Source: PRD-001 §F.8

#### Scenario: Daily monitor from the 10th
- **GIVEN** the CPF payment is not yet recorded as paid
- **WHEN** the daily monitor runs on or after the 10th
- **THEN** it tracks the CPF payment due by the 14th

#### Scenario: Payment record kept
- **WHEN** CPF is paid
- **THEN** the payment record is kept as evidence

#### Scenario: Framework mapping
- **WHEN** the control is listed in the control registry
- **THEN** it is mapped to the CPF Act

### Requirement: CTL-STAT-03 AIS by 1 March and IR21 at least one month before cessation
The AIS SHALL be submitted by 1 March, and an IR21 SHALL be filed at least one month before the cessation it concerns (FR-PAY-10). The control is checked daily, with the submission record kept as evidence.

Source: PRD-001 §F.8

#### Scenario: Daily calendar monitor for the AIS
- **WHEN** the daily calendar monitor runs
- **THEN** it tracks the AIS due date of 1 March

#### Scenario: IR21 gate
- **GIVEN** an employee's cessation needs an IR21
- **WHEN** the date one month before the cessation approaches
- **THEN** the IR21 gate applies until the IR21 is filed

#### Scenario: Submission record kept
- **WHEN** the AIS or an IR21 is submitted
- **THEN** the submission record is kept as evidence

#### Scenario: Framework mapping
- **WHEN** the control is listed in the control registry
- **THEN** it is mapped to the Income Tax Act (IRAS)

### Requirement: CTL-STAT-04 ECI, Form C-S/C, ACRA annual return and AGM on time
The ECI, Form C-S/C, ACRA annual return and AGM SHALL each be done on time. The control is checked daily by the calendar monitor, with the filing record kept as evidence.

Source: PRD-001 §F.8

#### Scenario: Daily calendar monitor
- **WHEN** the daily calendar monitor runs
- **THEN** it tracks the due dates of the ECI, Form C-S/C, ACRA annual return and AGM

#### Scenario: Filing record kept
- **WHEN** one of them is filed or held
- **THEN** the filing record is kept as evidence

#### Scenario: Framework mapping
- **WHEN** the control is listed in the control registry
- **THEN** it is mapped to the Income Tax Act and the Companies Act
