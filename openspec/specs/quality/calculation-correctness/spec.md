# quality/calculation-correctness Specification

## Purpose
How Fyniq proves that every figure it calculates is right: the independent reference ledger and the differential, property, mutation, invariant-attack, replay and pinned statutory tests; report and export verification; workbook parity; production verification and the response to a calculation defect; and the workbook data sets and pinned figures ported as golden fixtures. Derived from PRD-001 §16.2 and Appendix G (§G.1, §G.3).

## Requirements

### Requirement: QA-ENG-01 Independent reference ledger
A second implementation of the posting rules, the independent reference ledger, SHALL exist: written from the rules in §10 and Appendix B (not by translating the engine code), in a different language from the engine. The workbook's `tests/reference.py` is the starting point.

Source: PRD-001 §16.2

#### Scenario: Written from the rules, not from the engine
- **WHEN** the reference ledger is written or changed
- **THEN** it is derived from the rules in §10 and Appendix B, not translated from the engine code
- **AND** it is in a different language from the engine

#### Scenario: Starts from the workbook's reference
- **WHEN** the reference ledger is first built
- **THEN** it starts from the workbook's `tests/reference.py`

#### Scenario: Gate on every engine change
- **WHEN** a change to the engine is proposed
- **THEN** it is checked against the independent reference ledger, and the merge is blocked until engine and reference agree to the cent

### Requirement: QA-ENG-02 Differential testing
For every golden data set and for thousands of generated scenarios per CI run, the engine's trial balance, every subledger and every report figure SHALL equal the reference ledger's to the cent.

Source: PRD-001 §16.2

#### Scenario: Golden data sets agree to the cent
- **WHEN** the engine and the reference ledger process the same golden data set
- **THEN** the trial balance, every subledger and every report figure are equal to the cent

#### Scenario: Generated scenarios on every CI run
- **WHEN** a CI run happens
- **THEN** thousands of generated scenarios are processed by both the engine and the reference ledger
- **AND** their trial balances, subledgers and report figures are equal to the cent

#### Scenario: Any difference blocks the merge
- **GIVEN** a pull request after which any compared figure differs between the engine and the reference ledger
- **WHEN** the differential tests run
- **THEN** the merge is blocked, since the gate requires 100% equality

### Requirement: QA-ENG-03 Property-based tests on the engine
Property-based tests SHALL check the engine's properties on generated inputs: balance, reversal, idempotent reposting, allocation, FX conversion, settlement and locked periods. A failing property SHALL block the merge.

Source: PRD-001 §16.2

#### Scenario: Entries balance and reversals net to zero
- **WHEN** the property tests generate entries
- **THEN** every entry balances
- **AND** a reversal plus its original nets to zero

#### Scenario: Reposting is idempotent
- **WHEN** a document is reposted
- **THEN** the result is the same as posting it once

#### Scenario: Allocation parts sum to the whole
- **WHEN** an `ALLOC-*` policy splits a positive or a negative amount
- **THEN** the parts sum to the whole
- **AND** each part stays within the bounds stated in §10.4

#### Scenario: FX conversion is monotonic and rounding-bounded
- **WHEN** amounts are converted at an FX rate
- **THEN** the conversion is monotonic and its rounding stays bounded

#### Scenario: Settlement and AR
- **WHEN** a settlement is applied
- **THEN** it never leaves AR negative without a credit

#### Scenario: Locked periods never change
- **WHEN** generated operations run against a locked period
- **THEN** the locked period never changes

#### Scenario: A failing property blocks the merge
- **GIVEN** a pull request for which a property fails
- **WHEN** the property tests run
- **THEN** the merge is blocked

### Requirement: QA-ENG-04 Mutation testing
Mutation testing SHALL run on the posting engine, the rounding policies and the statutory calculators, scoped to these packages with surviving mutants reviewed by hand, since Go mutation tooling is pre-1.0. It SHALL be part of `make ci`, not of the fast `make test` loop.

Source: PRD-001 §16.2

#### Scenario: When mutation testing runs
- **WHEN** a change touches the posting engine, the rounding policies or the statutory calculators, or the nightly run happens
- **THEN** mutation testing runs on those packages

#### Scenario: Part of make ci only
- **WHEN** `make ci` runs
- **THEN** mutation testing is included
- **AND** it is not part of `make test`

#### Scenario: Surviving mutants reviewed by hand
- **WHEN** a mutant survives
- **THEN** it is reviewed by hand

#### Scenario: Score below 90% blocks the merge
- **GIVEN** a pull request after which the mutation score on those packages is below 90%
- **WHEN** mutation testing runs
- **THEN** the merge is blocked

### Requirement: QA-ENG-05 Database invariant attack tests
Attack tests SHALL bypass the application and attempt every write the database invariants forbid, and every attempt MUST fail. Race tests on independent connections SHALL check that exactly one of two incompatible operations commits.

Source: PRD-001 §16.2

#### Scenario: Ledger attacks fail
- **WHEN** the tests, bypassing the application, try to insert unbalanced entries, update or delete posted lines, or post into locked periods
- **THEN** every attempt fails

#### Scenario: Tenant and role attacks fail
- **WHEN** the tests try to cross tenants, write as the AI role, reuse a pooled connection after another tenant's transaction without setting the tenant, or insert a row that references another tenant's existing UUID
- **THEN** every attempt fails

#### Scenario: Value, privilege and access-model attacks fail
- **WHEN** the tests insert `NaN` and out-of-range values into every numeric column, TRUNCATE any table as an application role, or write a global table outside its access model
- **THEN** every attempt fails

#### Scenario: Race tests
- **GIVEN** independent connections with a barrier between the steps
- **WHEN** a posting races a close of the same period, two allocations race to one invoice, a report races a back-dated posting, or a reopen races a posting
- **THEN** exactly one incompatible operation commits
- **AND** the other gets its stable error
- **AND** a re-run report is unchanged

#### Scenario: G4-013 NaN inserted directly into amount and FX-rate columns
- **WHEN** `NaN` is inserted directly into an amount column and an FX-rate column, bypassing the application
- **THEN** both inserts fail

#### Scenario: A successful attack blocks the merge
- **GIVEN** a pull request after which an attack attempt succeeds or a race test fails
- **WHEN** the attack tests run
- **THEN** the merge is blocked

### Requirement: QA-ENG-06 Replay tests
Replay tests SHALL re-run each rule version over the stored documents, and the result MUST reproduce the stored entries byte-for-byte.

Source: PRD-001 §16.2

#### Scenario: Replay reproduces stored entries
- **WHEN** a rule version is re-run over the documents it posted
- **THEN** the entries produced equal the stored entries byte-for-byte

#### Scenario: Nightly and at each release
- **WHEN** the nightly run happens or a release is prepared
- **THEN** the replay tests run
- **AND** any byte difference fails them: at night it pages the platform owner, and at a release it blocks the release

### Requirement: QA-ENG-07 Golden data sets
The workbook sample company and all workbook edge-case data sets (Appendix G) SHALL be ported as fixtures with their verified expected results, and every new feature SHALL add new sets.

Source: PRD-001 §16.2

#### Scenario: Workbook sets ported with their expected results
- **WHEN** the golden fixtures run
- **THEN** the sample company and every edge-case data set of Appendix G produce their verified expected results

#### Scenario: New feature adds a set
- **WHEN** a new feature is added
- **THEN** it adds new golden data sets with verified expected results

#### Scenario: Gate at every release
- **WHEN** a release is prepared
- **THEN** every golden data set must produce its expected results

### Requirement: QA-ENG-08 Pinned statutory tests
Each parameter-pack version SHALL have hand-calculated CPF, SDL, SHG and GST test cases, derived from the official tables independently of the pack data (the workbook's `PINNED_PAY` approach) and cross-checked with the CPF Board contribution calculator where possible.

Source: PRD-001 §16.2

#### Scenario: Cases independent of the pack data
- **WHEN** pinned cases are written for a pack version
- **THEN** their expected values are hand-calculated from the official tables, not read from the pack data

#### Scenario: Cross-checked with the CPF Board calculator
- **WHEN** a CPF case can be entered in the CPF Board contribution calculator
- **THEN** its expected value is cross-checked with the calculator

#### Scenario: Pack activation blocked
- **GIVEN** a pack version whose pinned statutory tests are missing or fail
- **WHEN** it is activated
- **THEN** the activation is blocked

### Requirement: QA-RPT-01 Every visible number verified
Each figure returned by a report, dashboard, KPI or export API SHALL be compared with an independent expectation in at least one test, and a coverage report SHALL list the unverified figures.

Source: PRD-001 §16.2

#### Scenario: Coverage report of unverified figures
- **WHEN** the test suite runs
- **THEN** a coverage report lists every figure returned by a report, dashboard, KPI or export API that no test compares with an independent expectation

#### Scenario: An unverified figure blocks the merge
- **GIVEN** a pull request after which a returned figure is not verified by any test
- **WHEN** the coverage report is produced
- **THEN** the merge is blocked, since the gate requires 100%

### Requirement: QA-RPT-02 Every validation message triggered
Each cannot-post (✖) and please-review (⚠) rule and each error code SHALL be produced by at least one fixture (the workbook's status-coverage test).

Source: PRD-001 §16.2

#### Scenario: Status-coverage test
- **WHEN** the fixtures run
- **THEN** every cannot-post (✖) and please-review (⚠) rule and every error code is produced at least once

#### Scenario: An untriggered rule blocks the merge
- **GIVEN** a pull request that adds a rule or an error code no fixture produces
- **WHEN** the status-coverage test runs
- **THEN** the merge is blocked, since the gate requires 100%

### Requirement: QA-RPT-03 Export round-trip
Exports SHALL be round-trip tested: generated XLSX re-read and compared figure by figure, PDF text extracted and compared, and CSV totals reconciled.

Source: PRD-001 §16.2

#### Scenario: XLSX compared figure by figure
- **WHEN** a generated XLSX file is re-read
- **THEN** every figure equals the figure exported

#### Scenario: Precision beyond 15 significant digits
- **WHEN** an XLSX export includes a value at 15 significant digits and one above it
- **THEN** only the value above 15 significant digits is written as text, and the 15-digit value stays a number
- **AND** both values survive the round trip

#### Scenario: PDF and CSV
- **WHEN** a PDF and a CSV export are checked
- **THEN** the PDF's extracted text is compared with the source figures
- **AND** the CSV totals are reconciled

#### Scenario: Gate at every release
- **WHEN** a release is prepared
- **THEN** the export round-trip tests must pass

### Requirement: QA-RPT-04 Snapshot and visual-regression tests
Snapshot and visual-regression tests SHALL cover the reports and the key pages on the sample company, in light and dark themes.

Source: PRD-001 §16.2

#### Scenario: Reports and key pages in both themes
- **WHEN** the snapshot and visual-regression tests run on the sample company
- **THEN** each report and key page is compared with its snapshot in the light and the dark theme

#### Scenario: Gate at every release
- **WHEN** a release is prepared
- **THEN** the snapshot and visual-regression tests must pass

### Requirement: QA-MIG-01 Workbook parity test
After importing the workbook sample and edge-case files, the platform trial balance SHALL equal the workbook's for every account and month, except the items in the signed migration difference register (FR-MIG-02), and the Checks status SHALL match except where the register explains it.

Source: PRD-001 §16.2

#### Scenario: Trial balance parity
- **WHEN** the workbook sample and edge-case files are imported
- **THEN** the platform trial balance equals the workbook's for every account and month, except the items in the signed migration difference register

#### Scenario: Spreads and depreciation
- **WHEN** spread and depreciation lines are compared
- **THEN** they equal `ALLOC-EVEN` applied to the workbook's schedules exactly

#### Scenario: Unsigned register item fails the gate
- **GIVEN** an item in the migration difference register that is not signed
- **WHEN** the parity test runs
- **THEN** the gate fails

#### Scenario: Checks status matches
- **WHEN** the Checks status is compared with the workbook's
- **THEN** it matches, except where the register explains a difference

#### Scenario: Workbook forced-paid documents
- **GIVEN** a workbook document marked fully paid
- **WHEN** it is imported
- **THEN** its workbook outcome is imported as an approved write-off with the reason "migrated: workbook forced-paid"
- **AND** G.3 (June 7030: 0.60 − 120.00) still holds

#### Scenario: Settlements made in Fyniq
- **WHEN** a settlement is made in Fyniq after the import
- **THEN** FR-BNK-08 applies to it

#### Scenario: P1 release gate
- **WHEN** the P1 release is prepared
- **THEN** the workbook parity test must pass

### Requirement: QA-PROD-01 Production verification
Production SHALL be verified by the nightly ledger verification (FR-CTL-05), audit-chain verification and synthetic end-to-end transactions in a canary organisation, with an alert on any failure.

Source: PRD-001 §16.2

#### Scenario: Nightly ledger and audit-chain verification
- **WHEN** the nightly run happens in production
- **THEN** the ledger verification (FR-CTL-05) and the audit-chain verification run

#### Scenario: Synthetic transactions in a canary organisation
- **WHEN** production verification runs
- **THEN** synthetic end-to-end transactions run in a canary organisation

#### Scenario: Any failure alerts
- **WHEN** any production verification fails
- **THEN** an alert is raised

### Requirement: QA-REL-01 Calculation defect found after release
A calculation defect found after release SHALL trigger, as policy: a failing regression test first, a fix, a root-cause review, and a check that the oracles would now catch the class of defect.

Source: PRD-001 §16.2

#### Scenario: Regression test before the fix
- **WHEN** a calculation defect is found after release
- **THEN** a failing regression test is written before the fix

#### Scenario: Fix, root cause and oracle check
- **WHEN** the defect is fixed
- **THEN** a root-cause review is held
- **AND** it is checked that the oracles would now catch that class of defect

#### Scenario: Gate is policy
- **GIVEN** a calculation defect found after release
- **WHEN** its fix is closed without the failing regression test, the fix, the root-cause review or the oracle check
- **THEN** it breaches the policy gate

### Requirement: Golden data set, sample company
The workbook's sample company (`src/cfh/sample.py`, `tests/reference.py`, `tests/ref_*.py`) SHALL be ported as a golden fixture covering every report figure of a realistic year, verified cell by cell.

Source: PRD-001 §G.1

#### Scenario: Every report figure of a realistic year
- **WHEN** the sample company fixture runs
- **THEN** every report figure of its year equals the verified expected value, cell by cell

### Requirement: Golden data set, posting edge cases
The workbook's posting edge cases (`tests/edge_posting.py`, `tests/test_edge_posting.py`) SHALL be ported as a golden fixture with their verified expected results.

Source: PRD-001 §G.1

#### Scenario: Status branches and GST codes
- **WHEN** the posting edge-case fixture runs
- **THEN** it covers every status branch and each GST code

#### Scenario: Spreads
- **WHEN** the posting edge-case fixture runs
- **THEN** it covers the annual plan 13,080 spread over 12, insurance 1,200 over 6, and a revenue spread from April

#### Scenario: Foreign-currency rates
- **WHEN** the posting edge-case fixture runs
- **THEN** it covers USD invoices at row rate 1.35 and table rate 1.34, and a duplicated FX month averaging 1.34

#### Scenario: Settlements
- **WHEN** the posting edge-case fixture runs
- **THEN** it covers a settlement short 0.60 and one short 20 within 3%, an overpayment of 110, a payment after the as-of date, and a forced-paid bill with 120.00 to FX

#### Scenario: Payroll
- **WHEN** the posting edge-case fixture runs
- **THEN** it covers every CPF age band, the OW ceiling, a low wage, overrides and a duplicate employee

#### Scenario: Assets
- **WHEN** the posting edge-case fixture runs
- **THEN** it covers assets with a residual value, a disposal, a fully depreciated asset, a depreciate-from date and an asset bought before books start

### Requirement: Golden data set, non-posting edge cases
The workbook's non-posting edge cases (`tests/edge_nonposting.py`: accounts, people, sales, bank, plan, recurring) SHALL be ported as a golden fixture with their verified expected results.

Source: PRD-001 §G.1

#### Scenario: Accounts, people and FX
- **WHEN** the non-posting edge-case fixture runs
- **THEN** it covers account validation, contacts and employee warnings, FX table problems, the GST threshold window and double-counted salaries

#### Scenario: Sales, projects and bank
- **WHEN** the non-posting edge-case fixture runs
- **THEN** it covers items and lines, quote health, project budgets, timesheets, bank reconciliation of cards, and import duplicates including mirrored transfers

#### Scenario: Close, names, plans and recurring
- **WHEN** the non-posting edge-case fixture runs
- **THEN** it covers the close log, wildcard-like names (`Star*`, `<Walk-in customer>`), budget comments, feasibility verdicts and recurring month-end anchors (29 Jan, 28 Feb)

### Requirement: Golden data set, document numbers
The workbook's document-number cases (`tests/test_edge_docnumbers.py`) SHALL be ported as a golden fixture with their verified expected results.

Source: PRD-001 §G.1

#### Scenario: Special characters in document numbers
- **WHEN** document numbers contain `*`, `?`, `~` or `<`
- **THEN** the verified expected results hold for each of them

#### Scenario: Numeric and text numbers
- **WHEN** document numbers are numeric or text
- **THEN** the verified expected results hold for both kinds

#### Scenario: Each payment settles only its own document
- **WHEN** payments are matched to documents
- **THEN** each payment settles only its own document

### Requirement: Golden data set, regressions
The workbook's regression cases (`tests/test_regressions.py`) SHALL be ported as a golden fixture with their verified expected results.

Source: PRD-001 §G.1

#### Scenario: Reference with surrounding spaces
- **WHEN** a payment reference has surrounding spaces
- **THEN** it still settles its document

#### Scenario: Whitespace-only reference
- **WHEN** a reference contains only whitespace
- **THEN** a warning is shown

#### Scenario: Journal with two dates
- **WHEN** a journal has two dates
- **THEN** it does not post

### Requirement: Golden data set, miscellaneous cases
The workbook's miscellaneous cases (`tests/test_edge_misc.py`) SHALL be ported as a golden fixture with their verified expected results.

Source: PRD-001 §G.1

#### Scenario: Transfers, FX and statements
- **WHEN** the miscellaneous fixture runs
- **THEN** it covers a transfer from the other account, a USD reminder, a foreign carry-forward, unrealised FX 210 and a statement that reconciles

#### Scenario: Costs, budgets and averages
- **WHEN** the miscellaneous fixture runs
- **THEN** it covers the hourly cost with the CPF cap, a discount budget, an overdraft at month-end and 3-month averages

### Requirement: Pinned document totals
The golden data sets SHALL produce these pinned document totals.

Source: PRD-001 §G.3

#### Scenario: Invoice and credit-note totals
- **WHEN** the golden data sets are posted
- **THEN** the totals are INV-E01 10,900; INV-E20 13,500; INV-E21 6,700; INV-E26 3,422.60; CN-E15 −1,090; INV-E31 1,000; INV-E32 1,340

#### Scenario: Supplier bill totals
- **WHEN** the golden data sets are posted
- **THEN** the totals are SB-E21 2,720; SB-E22 2,616; SB-E23 3,270

### Requirement: Pinned FX and rounding on account 7030
The golden data sets SHALL produce these pinned amounts on account 7030 FX & rounding.

Source: PRD-001 §G.3

#### Scenario: June and August
- **WHEN** the golden data sets are posted
- **THEN** account 7030 FX & rounding shows June: 0.60 − 120.00
- **AND** August: 20.00

### Requirement: Pinned asset depreciation
The golden data sets SHALL produce these pinned asset figures (monthly depreciation / accumulated depreciation / net book value).

Source: PRD-001 §G.3

#### Scenario: Asset figures
- **WHEN** the golden data sets are depreciated
- **THEN** the figures (monthly / accumulated / NBV) are laptop 100 / 700 / 2,900; old phone 50 / 250 / 0; licence 300 / 900 / 0; fit-out 225 / 1,125 / 4,875; server bought before books start 200 / 3,200 / 8,800

#### Scenario: Account 6610 in September
- **WHEN** the golden data sets are depreciated
- **THEN** account 6610 in September is 625

### Requirement: Pinned unrealised FX revaluation
The unrealised FX memo SHALL be 240 − 30 = 210, and Fyniq SHALL post it as a revaluation entry (FR-GL-04) that reverses on day 1.

Source: PRD-001 §G.3

#### Scenario: Memo figure
- **WHEN** the unrealised FX of the golden data sets is calculated
- **THEN** it is 240 − 30 = 210

#### Scenario: Posted as a revaluation and reversed
- **WHEN** Fyniq accounts for it
- **THEN** it posts a revaluation entry (FR-GL-04)
- **AND** reverses it on day 1

#### Scenario: Parity test compares memo and revaluation
- **WHEN** the parity test runs
- **THEN** it compares the workbook memo with the posted revaluation as a class (b) item in the migration difference register (FR-MIG-02)

### Requirement: Pinned hourly cost
The pinned hourly cost SHALL be (10,000 + 17% × 8,000) ÷ 173.33.

Source: PRD-001 §G.3

#### Scenario: Hourly cost formula
- **WHEN** the hourly cost of the golden case is calculated
- **THEN** it equals (10,000 + 17% × 8,000) ÷ 173.33

### Requirement: Pinned conversion of a balance to shares (RND-SHARE)
Converting a balance to shares under `RND-SHARE` SHALL produce these pinned figures.

Source: PRD-001 §G.3

#### Scenario: At S$1.20 a share
- **WHEN** a balance of 12,345.67 is converted at S$1.20 a share
- **THEN** 10,288 shares are issued
- **AND** the entry is Dr 2500 12,345.60, Cr 3010 12,345.60
- **AND** 0.07 stays owed

#### Scenario: At S$0.0375 a share
- **WHEN** a balance of 12,345.67 is converted at S$0.0375 a share
- **THEN** 329,217 shares are issued
- **AND** 12,345.64 is converted
- **AND** 0.03 stays owed

### Requirement: Pinned SAFE conversion and dilution
SAFE conversion and dilution (FR-EQ-18, FR-EQ-21) SHALL produce these pinned figures for a pre-money and a post-money SAFE.

Source: PRD-001 §G.3

#### Scenario: Pre-money SAFE conversion
- **GIVEN** 1,000,000 founder shares and a SAFE of S$125,000 with a S$2,500,000 pre-money cap and 20% discount
- **WHEN** a round at S$3.00 a share brings a new investor's S$600,000
- **THEN** the conversion price is min(2.50, 2.40) = S$2.40
- **AND** the SAFE converts into 52,083 shares (S$124,999.20, S$0.80 stays on 2700 until settled under the terms)
- **AND** the investor gets 200,000 shares, for a total of 1,252,083

#### Scenario: Pre-money ownership and dilution
- **WHEN** the pre-money round has converted
- **THEN** the ownership is founders 79.87%, investor 15.97%, SAFE 4.16% (sum 100.00)
- **AND** the founders are diluted by 20.13 points

#### Scenario: The pre-money cap price basis
- **WHEN** the pre-money cap price is calculated
- **THEN** it is the cap ÷ the 1,000,000 shares before the round

#### Scenario: Post-money SAFE conversion
- **GIVEN** the same round with a post-money SAFE with the same S$2,500,000 cap
- **WHEN** the SAFE converts
- **THEN** the cap price wins with or without the 20% discount, since 2.375 < 2.40: cap price 2,500,000 ÷ 1,052,631.58 = S$2.375
- **AND** the SAFE converts into 52,631 shares (S$124,998.63; S$1.37 stays on 2700), for a total of 1,252,631
- **AND** the ownership is founders 79.83%, investor 15.97%, SAFE 4.20%
