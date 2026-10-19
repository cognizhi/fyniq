# tax/statutory-parameters Specification

## Purpose
How Fyniq keeps Singapore statutory parameters (GST, CPF, SDL, SHG, corporate income tax) as effective-dated, versioned packs with their sources and approvals, selects the version in force at each tax point or pay month, and fixes each document's tax point and GST dates. Derived from PRD-001 §10.6.

## Requirements

### Requirement: ENG-30 Statutory parameters as versioned packs
The system SHALL store statutory parameters as versioned packs, each version with its effective dates, official source, provenance, approver, the conditions under which it applies and a review date.

Source: PRD-001 §10.6 · Priority M · Phase P1

#### Scenario: Parameters kept in packs
- **WHEN** statutory parameters are stored
- **THEN** the packs cover the GST rate and codes, F5 mapping and registration threshold; CPF rates by residency, age band and PR year, the wage floor and the OW and AW ceilings; the SDL rate, minimum and maximum; the SHG tables; and the CIT rate, SUTE/PTE tiers and rebates

#### Scenario: A pack version records its source and approval
- **WHEN** a pack version is stored
- **THEN** it records `effective_from`, `effective_to`, the official source URL, the retrieval date, the verifier and the approver
- **AND** the source section, the kind of source (law, regulation, official guidance, contract term, product policy), the conditions under which the rule applies, and a review date

#### Scenario: A version's layout and loading (rev 0.37, provisional)
- **WHEN** a pack version is published
- **THEN** it is a directory `packs/<pack>/<year>.<n>/` with `pack.toml`, CSV tables whose rows name their sources, and `pinned/`, its hand-pinned statutory tests, with values as exact decimal strings
- **AND** the release loads it into global tables that only the migrator writes and that never change once loaded; a correction is a new version

#### Scenario: An incomplete or unverified version is refused (rev 0.37)
- **GIVEN** a version missing a source URL, a verifier or a review date, or holding a value still marked [verify]
- **WHEN** it is loaded
- **THEN** the whole version is refused

#### Scenario: Verifier and approver (rev 0.37, provisional)
- **WHEN** a version is published
- **THEN** its verifier is whoever checked each value against its official page, which may be its author
- **AND** the approver is recorded when an organisation activates it (ENG-32), not in the file

#### Scenario: Pack past its review date needs review (C.2 #16)
- **GIVEN** a pack version past its review date
- **WHEN** Checks are shown
- **THEN** the pack is shown with a warning (⚠) for review (C.2 #16)

### Requirement: ENG-31 Version in force at the tax point or pay month
The engine SHALL select the parameter version in force at each document's tax point or pay month.

Source: PRD-001 §10.6 · Priority M · Phase P1

#### Scenario: Document uses the version in force at its tax point
- **GIVEN** two versions of a pack with different effective dates
- **WHEN** a document is posted
- **THEN** the engine uses the version in force at the document's tax point

#### Scenario: Pay run uses the version in force in its pay month
- **WHEN** a pay run is posted
- **THEN** the engine uses the parameter version in force in its pay month

#### Scenario: A future rate never changes the past
- **WHEN** a rate effective in the future is changed
- **THEN** no figure of a closed or past period changes

### Requirement: ENG-32 Dual approval and pinned tests before activation
A new pack version SHALL require dual approval (SOD-10) and SHALL pass the pinned statutory tests for its effective date before activation.

Source: PRD-001 §10.6 · Priority M · Phase P1

#### Scenario: Activation needs a second approver
- **GIVEN** a new pack version approved by one person
- **WHEN** activation is attempted
- **THEN** it is not activated until a second approver approves it (SOD-10)

#### Scenario: Activation needs the pinned tests to pass
- **GIVEN** a new pack version whose pinned statutory tests for its effective date fail
- **WHEN** activation is attempted
- **THEN** it is not activated

#### Scenario: Activation is the organisation's own record (rev 0.37, provisional)
- **WHEN** an organisation activates a pack version
- **THEN** a row of its own names the proposer, an approver who is not the proposer, and the approval engine's record

#### Scenario: No activation before the approval engine (rev 0.37, provisional)
- **GIVEN** `p1-approval-engine` has not landed
- **WHEN** any organisation, the local sample company included, tries to activate a pack version
- **THEN** it is refused

### Requirement: ENG-33 Published pack updates
The platform team SHALL publish pack updates (e.g. every January and after each Budget), and each organisation SHALL see a "parameter update available" task with a diff.

Source: PRD-001 §10.6 · Priority S / M (update marking) · Phase P2 / P1 (update marking)

#### Scenario: Update available task with a diff
- **WHEN** the platform team publishes a pack update
- **THEN** each organisation sees a "parameter update available" task
- **AND** the task comes with a diff

#### Scenario: Mandatory update not yet accepted
- **GIVEN** a mandatory pack update that the organisation has not accepted
- **WHEN** a figure or check that depends on it is shown
- **THEN** it shows "based on <pack version>; update pending"
- **AND** no "all clear" is given (this part is M/P1, with C.2 #16)

#### Scenario: Correction to a past period
- **WHEN** a correction to a past period is published
- **THEN** it opens an assessment task listing the affected documents
- **AND** it never re-posts history by itself (ENG-31)

### Requirement: ENG-45 Tax point and GST dates
Every taxable document SHALL record its document date, tax point (time of supply), posting date and payment dates, and the tax point SHALL follow a named, versioned rule in the `sg-gst` pack.

Source: PRD-001 §10.6 · Priority M · Phase P1

#### Scenario: Earlier of invoice date and payment
- **WHEN** the tax point of a supply is determined
- **THEN** it is the earlier of the invoice date and the payment received

#### Scenario: Deposit that is part payment
- **WHEN** a deposit that is part payment is received
- **THEN** it is taxed when received

#### Scenario: Security deposit
- **WHEN** a security deposit is received
- **THEN** it is taxed only when it is applied to the price

#### Scenario: Continuous services
- **WHEN** the tax point of a continuous service is determined
- **THEN** it follows the same earlier-of rule

#### Scenario: Values taken from the tax point
- **WHEN** a taxable supply is posted
- **THEN** its GST rate, the registration status and the output-tax return period come from its tax point

#### Scenario: Input-tax return period from the claim-date policy
- **WHEN** input tax is claimed
- **THEN** its return period comes from the organisation's claim-date policy: the invoice date, or the date the invoice is recorded, used consistently

#### Scenario: Supplies spanning a registration, deregistration or rate change
- **WHEN** a supply spans a GST registration, deregistration or rate change
- **THEN** it follows the transitional rules in the pack

#### Scenario: Spreads never move GST
- **WHEN** a document's revenue or cost is spread over several periods
- **THEN** its GST stays in the period of its tax point

#### Scenario: Pinned cases
- **WHEN** the tax point rules are tested (QA-ENG-08)
- **THEN** pinned cases cover an advance payment before the invoice, a registration mid-service, a rate change and a late-recorded supplier invoice

#### Scenario: G4-018 Deposit received before the invoice
- **GIVEN** a consulting deposit (part payment) received in Q1 and the invoice issued in Q2
- **WHEN** the GST returns are prepared
- **THEN** output GST on the deposit is in Q1
- **AND** the Q2 invoice reports only the balance [verify]

#### Scenario: G4-019 Security deposit applied later
- **GIVEN** a refundable security deposit received and applied to the price three months later
- **WHEN** the GST returns are prepared
- **THEN** there is no GST on receipt
- **AND** output GST is in the period it is applied

#### Scenario: G4-020 Registration mid-service
- **GIVEN** a service invoiced monthly and a GST registration that takes effect mid-service
- **WHEN** the invoices are posted
- **THEN** invoices with a tax point before the registration date carry no GST
- **AND** later ones do, under the pack's transitional rule

#### Scenario: G4-021 Supplier invoice recorded after the return was filed
- **GIVEN** a supplier invoice dated in Q1 and recorded in Q2 after the Q1 return was filed
- **WHEN** the claim-date policy is "date recorded"
- **THEN** the input tax is in Q2 with no correction
- **AND** with the policy "invoice date" an adjustment-queue item is created (FR-TAX-12)
