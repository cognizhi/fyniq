# ledger/fixed-assets Specification

## Purpose
How Fyniq keeps the fixed asset register: assets captured from bills or transactions, monthly depreciation posted by the scheduler, disposals and write-offs, and the asset schedules the financial statements and tax computation need. Derived from PRD-001 §8.9.

## Requirements

### Requirement: FR-FA-01 Asset register
The system SHALL keep an asset register built from bills or transactions, recording each asset's category, cost, date, useful life, residual value, depreciation method and depreciation start.

Source: PRD-001 §8.9 · Priority M · Phase P1

#### Scenario: Capitalisation threshold prompt
- **WHEN** a bill or transaction line costs at or above the capitalisation threshold (FR-ORG-04)
- **THEN** the user is prompted to add it to the asset register

#### Scenario: Asset details are recorded
- **WHEN** an asset is registered from a bill or transaction
- **THEN** its category, cost, date, useful life, residual value, method and depreciation start are stored

#### Scenario: The account's default useful life is offered (rev 0.151)
- **WHEN** an asset is registered on an account that has a default useful life (FR-COA-02)
- **THEN** the form offers that life, and the person may change it

#### Scenario: An asset registered by mistake is voided (rev 0.151)
- **WHEN** an asset registered by mistake is voided with a reason
- **THEN** each of its months of depreciation is reversed on its own date (FR-GL-12), the asset leaves the register, and its line can be registered again
- **AND** the void is refused while a disposal of the asset stands, or while a month to reverse is in a closed period
- **AND** in Founder mode another person approves it (FM-1) (the owner's decision of 27 Oct 2026)

#### Scenario: Depreciation methods by phase
- **WHEN** a depreciation method is chosen
- **THEN** straight-line is available in P1
- **AND** reducing balance is available from P2

### Requirement: FR-FA-02 Monthly depreciation
The scheduler SHALL post depreciation monthly to 6610 and 1590 (per workbook), from the later of the depreciation start and the books start.

Source: PRD-001 §8.9 · Priority M · Phase P1

#### Scenario: Monthly depreciation posting
- **WHEN** the scheduler runs depreciation for a month
- **THEN** each asset's depreciation for the month debits 6610 Depreciation expense and credits 1590 Accumulated depreciation

#### Scenario: Depreciation starts at the later date
- **GIVEN** an asset whose depreciation start is before the books start
- **WHEN** depreciation is posted
- **THEN** it is posted from the books start, the later of the two dates

### Requirement: FR-FA-03 Disposal and write-off
The system SHALL record the disposal and write-off of an asset with the gain or loss, including partial disposal.

Source: PRD-001 §8.9 · Priority M · Phase P1

#### Scenario: Disposal with gain or loss
- **WHEN** an asset is disposed of
- **THEN** the gain or loss on disposal is recorded

#### Scenario: Write-off
- **WHEN** an asset is written off
- **THEN** the write-off is recorded with its loss

#### Scenario: Partial disposal
- **WHEN** part of an asset is disposed of
- **THEN** that part is removed from the register with its gain or loss, and the rest stays in the register

#### Scenario: Proceeds received or invoiced (rev 0.149)
- **WHEN** an asset is sold
- **THEN** its proceeds are money received into a money account, recorded with the disposal and matched by bank import, or a sales invoice to the buyer whose line goes to the disposal clearing account (`assets.disposal_clearing_account`, which the organisation names [proposed]), which the disposal clears
- **AND** a write-off has no proceeds

#### Scenario: Where the gain or loss goes (rev 0.149)
- **WHEN** a disposal's gain or loss is posted
- **THEN** it goes to the account the setting `assets.disposal_account` names, recommended 7050, under dual control [verify in the CoA review] (the owner's decision of 27 Oct 2026)

#### Scenario: The recommended clearing account (rev 0.151)
- **WHEN** an organisation has not changed `assets.disposal_clearing_account`
- **THEN** it is 1160 Asset disposal clearing (FR-COA-01) [accountant to confirm], an account with no system role, so the setting may name another

#### Scenario: GST on proceeds received with the disposal (rev 0.155)
- **GIVEN** a disposal with proceeds received into a money account while the organisation is GST-registered
- **WHEN** it is posted
- **THEN** 2200 takes `RND-GST-INCL` of the amount received at the proceeds' GST code's rate, the money account the amount received, and the gain or loss is worked on the rest [accountant to confirm]
- **AND** proceeds billed by an invoice, and a write-off, post no GST on the disposal

#### Scenario: Giving an asset away is not a disposal in P1 (rev 0.155)
- **WHEN** an asset is given away
- **THEN** Fyniq posts no disposal for it in P1, since the gift is a deemed supply at its open market value [accountant to confirm]

### Requirement: FR-FA-04 Asset reports and capital allowances
The system SHALL report NBV, accumulated depreciation and the movement schedule (for the financial statements), and SHALL produce a tax capital allowance schedule (S19, S19A).

Source: PRD-001 §8.9 · Priority S · Phase P2

#### Scenario: Asset schedule for the financial statements
- **WHEN** the asset report is run for a period
- **THEN** it shows each asset's NBV and accumulated depreciation and the movement schedule for the financial statements

#### Scenario: Capital allowance schedule
- **WHEN** the tax capital allowance schedule is produced
- **THEN** it follows S19 (working life, 20% initial allowance), S19A(1) (3 years) or S19A(10A) (assets up to S$5,000, at most S$30,000 a year)
