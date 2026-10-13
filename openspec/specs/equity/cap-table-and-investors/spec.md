# equity/cap-table-and-investors Specification

## Purpose
How Fyniq keeps the equity register from equity transactions, so the founders know at any date who owns what, what a new round would do to each holder, and what every investor holds and is entitled to. ACRA's electronic register of members is the legal register; Fyniq mirrors it, adds the history, the instruments ACRA does not hold (SAFEs, notes, options) and the evidence, and reconciles to it. Derived from PRD-001 §8.23.

## Requirements

### Requirement: FR-EQ-15 Holders and investors
Every shareholder, SAFE or note holder and option holder SHALL be a contact with the role investor or shareholder, holding their details, holdings by class, instruments, rights and documents.

Source: PRD-001 §8.23 · Priority M · Phase P1

#### Scenario: Holder record
- **WHEN** a holder is recorded
- **THEN** the contact has the role investor or shareholder and shows under that Contacts filter
- **AND** it holds whether the holder is an individual or an entity, identifiers, address, contact people, holdings by class, instruments, rights and documents

#### Scenario: Identifiers are protected
- **WHEN** a holder's identifiers are stored or shown
- **THEN** they are protected like employee identifiers (CMP-PD-07)

#### Scenario: Changes are versioned, audited and approved
- **WHEN** a holder record is changed
- **THEN** the change is versioned, audited and approved

#### Scenario: Identity documents are evidence
- **WHEN** a holder's identity document is uploaded
- **THEN** it is kept as evidence (FR-DOC-05) with the personal-data class

### Requirement: FR-EQ-16 Share classes
The system SHALL keep share classes, ordinary and preference (series), each with structured rights, and the constitution and shareholders' agreement attached as evidence.

Source: PRD-001 §8.23 · Priority M · Phase P1

#### Scenario: Structured rights of a class
- **WHEN** a share class is set up
- **THEN** it records votes per share, liquidation preference (multiple, participating or not, seniority), dividend rights, conversion ratio, anti-dilution (none, broad-based weighted average, full ratchet), pro-rata and redemption rights

#### Scenario: Governing documents as evidence
- **WHEN** a share class is set up
- **THEN** the constitution and the shareholders' agreement are attached as evidence

#### Scenario: Rights are recorded, not computed, in P1
- **GIVEN** phase P1
- **WHEN** equity figures are calculated
- **THEN** only SAFE and note conversions (FR-EQ-18) and round dilution (FR-EQ-21) are calculated
- **AND** no anti-dilution adjustment or liquidation outcome is computed

### Requirement: FR-EQ-17 Equity transactions
Equity transactions SHALL be append-only and dated by their effective date; each SHALL need approval (FM-1, FM-9) and evidence (FR-DOC-06) and open its ACRA tasks (FR-EQ-09).

Source: PRD-001 §8.23 · Priority M · Phase P1

#### Scenario: Kinds of transaction
- **WHEN** an equity transaction is recorded
- **THEN** it is an issue for cash (Dr Bank, Cr 3010/3020), an issue for non-cash consideration (FR-EQ-06), a transfer between holders, a conversion between classes, a SAFE or note conversion (FR-EQ-18) or an option exercise (FR-EQ-19)
- **AND** cancellation, buy-back, capital reduction, split and consolidation are C/P2

#### Scenario: Approval, evidence and ACRA tasks
- **WHEN** an equity transaction is submitted
- **THEN** it needs approval (FM-1, FM-9) and evidence (FR-DOC-06)
- **AND** it opens its ACRA tasks (FR-EQ-09)

#### Scenario: Transfer between holders
- **WHEN** shares are transferred between holders
- **THEN** no entry is made in the company's books
- **AND** share duty on the transfer document is the parties' matter, on the higher of price and net asset value [verify rate]

#### Scenario: Transfer takes effect at ACRA
- **WHEN** a transfer's ACRA tasks open
- **THEN** they ask for a proper instrument of transfer, then the company's notice of transfer (s126), which ACRA's guidance expects within 14 days
- **AND** the transfer takes effect when ACRA updates the register of members

#### Scenario: Corrected by reversal, never edited
- **WHEN** a recorded equity transaction is wrong
- **THEN** it is corrected by a reversing transaction, never edited

#### Scenario: A reversal corrects Fyniq's record only
- **GIVEN** Fyniq's record was wrong and ACRA's register is right
- **WHEN** the transaction is reversed
- **THEN** the reversal cites the ACRA extract as evidence (FR-EQ-26)

#### Scenario: Changing the legal position is a new transaction
- **WHEN** the legal position must change (cancellation, buy-back, capital reduction; C/P2)
- **THEN** it is a new legal transaction with its own resolutions and lodgements, never a reversal

#### Scenario: Cash before the effective date
- **WHEN** cash for an issue arrives before the issue's effective date
- **THEN** it is held as share application monies (Appendix B.7)

#### Scenario: G4-043 Investor pays before the allotment takes effect
- **GIVEN** an investor pays S$600,000.00 on 26 Feb, the allotment resolution is passed on 5 Mar and ACRA's register is updated (effective date) on 12 Mar
- **WHEN** the transactions are recorded
- **THEN** on 26 Feb the posting is Dr Bank · Cr share application monies (not 2999, no C.1 #30 warning)
- **AND** the February balance sheet shows the liability and unchanged share capital
- **AND** on 12 Mar the register posts Dr share application monies · Cr 3010
- **AND** the cap table changes on 12 Mar only
- **AND** INV-13 holds

### Requirement: FR-EQ-18 SAFEs and convertible notes
The system SHALL record SAFEs and convertible notes, posted to 2700, and at a priced round SHALL compute their conversion and create the share issues as drafts for approval.

Source: PRD-001 §8.23 · Priority M · Phase P1

#### Scenario: Terms recorded
- **WHEN** a SAFE or note is recorded
- **THEN** it holds the holder, amount, date, valuation cap, discount, most-favoured-nation clause, interest and maturity (notes), conversion trigger and the shares it converts into
- **AND** it is posted to 2700

#### Scenario: Form, denominator and classification
- **WHEN** an instrument is recorded
- **THEN** it records its form (post-money SAFE with a cap, pre-money SAFE, discount only, MFN, convertible note), the conversion denominator its terms define (for a post-money SAFE, the company capitalisation including all SAFEs and the option pool as the terms say), how note interest converts and the precision of the conversion price
- **AND** it records the accountant's liability-or-equity classification from its terms [verify per instrument]

#### Scenario: Conversion at a priced round
- **WHEN** a priced round triggers conversion
- **THEN** the engine computes the conversion price (the lower of the cap price and the discounted round price, as the terms say) and the shares with `RND-SHARE`
- **AND** it creates the share issues as drafts for approval

#### Scenario: Automatic conversion only with an approved template
- **GIVEN** an instrument whose form has no approved template and pinned fixture, or with any other term the engine does not cover
- **WHEN** a priced round triggers conversion
- **THEN** a draft is created for the accountant to complete, with a please-review warning (open question 33)

#### Scenario: Workbook limitation fixed
- **WHEN** a SAFE converts
- **THEN** the converted SAFE shares are computed with whole-share rounding, fixing the workbook limitation (Appendix A)

#### Scenario: G4-044 Post-money SAFE converts at the round
- **GIVEN** a post-money SAFE (S$125,000, S$2,500,000 cap)
- **WHEN** it converts at the G.3 round
- **THEN** it converts into 52,631 shares at S$2.375 (S$124,998.63; S$1.37 stays on 2700)
- **AND** ownership is founders 79.83%, investor 15.97%, SAFE 4.20%
- **AND** an instrument without an approved template creates a draft for the accountant

### Requirement: FR-EQ-19 Option pool and grants
The system SHALL keep the option pool, sized by resolution, and the grants made from it.

Source: PRD-001 §8.23 · Priority S · Phase P1 (register) / P2 (expense, IR8A)

#### Scenario: Pool size set by resolution
- **WHEN** the option pool is created or resized
- **THEN** its size is set by a resolution

#### Scenario: Grant details
- **WHEN** an option grant is recorded
- **THEN** it holds the holder, number, exercise price, grant date, vesting (cliff, monthly, acceleration) and expiry

#### Scenario: Pool updates
- **WHEN** options are exercised, forfeited or expire
- **THEN** the pool is updated

#### Scenario: Employees see their own grants
- **WHEN** an employee opens their equity view
- **THEN** they see their own grants

#### Scenario: Expense and IR8A data in P2
- **GIVEN** phase P2
- **WHEN** grants exist
- **THEN** share-based payment expense schedules (FR-EQ-13) and the employee share-gains data for IR8A (Appendix 8B [verify]) are produced

### Requirement: FR-EQ-20 Cap table at any date
The system SHALL compute the cap table at any date from the equity transactions; it is never edited directly.

Source: PRD-001 §8.23 · Priority M · Phase P1

#### Scenario: Issued and fully diluted views
- **WHEN** the cap table is produced for a date
- **THEN** it shows holdings by holder and class, issued and fully diluted (granted options, the unallocated pool, SAFEs and notes as converted at a stated price), with ownership and voting percentages

#### Scenario: Two dates compared
- **WHEN** a user compares the cap table at two dates
- **THEN** both are shown side by side

#### Scenario: Drill-down and export
- **WHEN** a cap table figure is opened
- **THEN** it drills down to each transaction and its evidence
- **AND** the cap table exports to XLSX and PDF

#### Scenario: Exact percentages, shown to 2 decimals
- **WHEN** percentages are computed
- **THEN** they are computed exactly and shown to 2 decimals (`RND-DISPLAY`)
- **AND** the oracle computes the same table (QA-ENG-02)

#### Scenario: No direct edits
- **WHEN** a user tries to change a cap table figure directly
- **THEN** the change is not possible; holdings change only through equity transactions

### Requirement: FR-EQ-21 Round modelling and dilution
The system SHALL model funding rounds and their dilution in a sandbox that never posts.

Source: PRD-001 §8.23 · Priority M · Phase P1

#### Scenario: Round inputs
- **WHEN** a round is modelled
- **THEN** the inputs are the pre-money valuation or the price per share, new money per investor, an option-pool top-up before or after the money, SAFE and note conversions, and pro-rata take-up

#### Scenario: Round outputs
- **WHEN** a round is calculated
- **THEN** it shows the price per share, shares per investor, the pro-forma cap table, each holder's ownership before and after and the dilution in percentage points, and the post-money valuation

#### Scenario: Scenarios side by side
- **WHEN** several round scenarios are modelled
- **THEN** they are shown side by side

#### Scenario: Modelling never posts
- **WHEN** a round is modelled
- **THEN** nothing is posted to the ledger or the equity register

#### Scenario: Approved scenario creates draft issues
- **WHEN** a round scenario is approved
- **THEN** it can create draft share issues, which then follow FR-EQ-17

#### Scenario: AI runs round scenarios
- **WHEN** the AI is asked to model a round
- **THEN** it may run scenarios in the read lane, like FR-PLN-08

### Requirement: FR-EQ-22 Investor records and obligations
The system SHALL keep, per investor, the investments, the rights from the agreements and the documents as evidence.

Source: PRD-001 §8.23 · Priority M · Phase P1

#### Scenario: Investments
- **WHEN** an investor's record is viewed
- **THEN** it lists their investments with the round, instrument, amount, price and date

#### Scenario: Rights from the agreements
- **WHEN** an investor's rights are recorded
- **THEN** they cover board seat, observer, information rights, pro-rata and consent matters

#### Scenario: Documents as evidence
- **WHEN** an investor's subscription agreement, shareholders' agreement or side letters are uploaded
- **THEN** they are kept as evidence

### Requirement: FR-EQ-28 Investor obligation tasks
Investors' information rights SHALL become calendar tasks with the delivered pack as evidence.

Source: PRD-001 §8.23 · Priority S · Phase P2

#### Scenario: Information rights become tasks
- **GIVEN** an investor's information right, for example quarterly accounts within 45 days
- **WHEN** the right is recorded
- **THEN** it becomes a calendar task

#### Scenario: Delivered pack as evidence
- **WHEN** the information is delivered
- **THEN** the delivered pack is kept as the task's evidence

### Requirement: FR-EQ-23 Investor updates
The system SHALL build a monthly or quarterly investor update from the board pack (FR-RPT-06) and send it to a distribution list as access-controlled links.

Source: PRD-001 §8.23 · Priority S · Phase P2

#### Scenario: Update content
- **WHEN** an investor update is built
- **THEN** it contains KPIs, cash, runway, highlights and asks from the board pack

#### Scenario: Commentary drafted by the AI
- **WHEN** the update's commentary is prepared
- **THEN** the AI drafts it (`rpt.draft_commentary`) and a human edits it

#### Scenario: Sent as access-controlled links
- **WHEN** the update is sent
- **THEN** each person on the distribution list receives an access-controlled link
- **AND** read status is tracked and the update is kept in an archive

### Requirement: FR-EQ-24 Investor access
The Board / investor viewer role SHALL see the investor updates, the board pack and their own holdings by default, and the full cap table only when an Owner grants it.

Source: PRD-001 §8.23 · Priority S · Phase P2

#### Scenario: Board or investor viewer by default
- **WHEN** a user with the Board / investor viewer role signs in
- **THEN** they see the investor updates, the board pack and their own holdings
- **AND** they do not see the full cap table

#### Scenario: Owner grants the full cap table
- **GIVEN** an Owner has granted the full cap table to a board or investor viewer
- **WHEN** that viewer opens equity
- **THEN** they see the full cap table

#### Scenario: Employees see only their own grants
- **WHEN** an employee opens equity
- **THEN** they see only their own option grants

### Requirement: FR-EQ-25 Linked statutory checks
Every holder change SHALL re-evaluate exempt-private-company status and flag any person who crosses the controller threshold for a register-of-controllers update (FR-TAX-07).

Source: PRD-001 §8.23 · Priority M · Phase P1

#### Scenario: Exempt status re-evaluated
- **WHEN** a holder change adds a corporate holder or a 21st member
- **THEN** exempt-private-company status ends (FR-EQ-11)

#### Scenario: Controller threshold crossed
- **WHEN** a person's shares or votes cross the controller threshold (more than 25% of the shares or of the voting power; confirmed on ACRA's registrable-controllers guidance, 29 Jan 2026)
- **THEN** they are flagged for a register-of-controllers update: the company's own register within 7 days, then the central register within 2 business days (ACRA guidance; s386AG–AH)

#### Scenario: Other control tests come from recorded rights
- **WHEN** the controller check runs
- **THEN** it tests only shares and votes from the cap table
- **AND** the other control tests (the right to appoint or remove directors holding a majority of board votes; significant influence or control) come from recorded rights (FR-EQ-22: board seats, consent matters) and the person's confirmation (open question 41)

#### Scenario: Not assessed without recorded rights
- **GIVEN** those rights are not recorded
- **WHEN** the controller check runs
- **THEN** it reports "Not assessed", never OK

#### Scenario: Annual notices to controllers
- **WHEN** the register of controllers is kept
- **THEN** it records the notice sent to each controller at least once a year, the 30-day reply period and the update within 7 days after it

#### Scenario: G4-101 Control through a board-appointment right
- **GIVEN** a person controls the company through a right to appoint the majority of directors but holds under 25% of the shares, and share-rights data is missing
- **WHEN** the controller check runs
- **THEN** it reports Not assessed, as FR-EQ-25 requires when share rights are not recorded, never OK
- **AND** the run report gives no "all clear"

### Requirement: FR-EQ-26 Integrity
Paid-up capital in the register SHALL equal 3010 + 3020 in the ledger (INV-13), the register SHALL reconcile to an uploaded ACRA register-of-members extract, and equity data SHALL be restricted.

Source: PRD-001 §8.23 · Priority M · Phase P1

#### Scenario: Register equals the ledger
- **WHEN** the register and the ledger are compared
- **THEN** paid-up capital in the register equals 3010 + 3020 in the ledger (INV-13)

#### Scenario: Reconciliation to ACRA
- **GIVEN** an uploaded ACRA register-of-members extract (business profile)
- **WHEN** the register is reconciled to it
- **THEN** any differences show on Checks

#### Scenario: Equity data is restricted
- **WHEN** an AI or MCP client requests equity data
- **THEN** it needs the `fyniq.read.equity` scope, which is never granted by default (AI-37)

### Requirement: FR-EQ-29 Exit waterfall
The system SHALL model an exit waterfall in a sandbox that never posts, built only once a share class with a liquidation preference exists.

Source: PRD-001 §8.23 · Priority C · Phase P2

#### Scenario: Not available without a liquidation preference
- **GIVEN** no share class has a liquidation preference
- **WHEN** a user looks for the exit waterfall
- **THEN** it is not available

#### Scenario: Proceeds for an exit value
- **WHEN** an exit value is entered
- **THEN** the proceeds per class and holder under the recorded preferences (multiple, participating or not, seniority) are shown next to the as-converted split
- **AND** each holder's better choice is shown
- **AND** nothing posts

#### Scenario: Only confirmed structures
- **WHEN** a preference structure is used in the waterfall
- **THEN** it is one the company's lawyer or accountant has confirmed, with hand-pinned cases and the oracle (QA-ENG-02)

### Requirement: FR-EQ-27 Basic and Expert
The equity features SHALL be split between Basic and Expert modes (§7.8).

Source: PRD-001 §8.23 · Priority S · Phase P1

#### Scenario: Basic mode
- **WHEN** a user opens equity in Basic mode
- **THEN** they see "Who owns the company" (an ownership chart with a table view, my holding, the latest round) and the round simulator with a few inputs

#### Scenario: Expert mode
- **WHEN** a user opens equity in Expert mode
- **THEN** they also see classes and rights, instruments, the option pool, transactions, fully diluted views and reconciliation
