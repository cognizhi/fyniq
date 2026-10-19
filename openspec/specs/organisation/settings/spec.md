# organisation/settings Specification

## Purpose
How an organisation is set up in Fyniq: its profile, financial year and base currency, accounting framework and policies, GST status, defaults, statutory parameter packs, feature switches, data region and retention, recommended defaults, and its logo and brand. Derived from PRD-001 §8.1.

## Requirements

### Requirement: FR-ORG-01 Organisation profile
The system SHALL keep an organisation profile with the legal name, UEN, GST registration number and date, addresses, contact details, logo, bank and PayNow details for documents, and document footers.

Source: PRD-001 §8.1 · Priority M · Phase P1

#### Scenario: Profile details are recorded
- **WHEN** the organisation profile is saved with a legal name, UEN, GST registration number and date, addresses, contact details and a logo
- **THEN** each of those values is stored on the organisation profile

#### Scenario: Documents take payment details and footers from the profile
- **WHEN** a document is produced
- **THEN** the bank and PayNow details and the document footer it carries come from the organisation profile

### Requirement: FR-ORG-02 Financial year and base currency
The system SHALL record the organisation's financial year, books start date, base currency and time zone.

Source: PRD-001 §8.1 · Priority M · Phase P1

#### Scenario: Financial year starts in any month
- **WHEN** the financial year is set to start in any month of the calendar year
- **THEN** that start month is accepted

#### Scenario: First financial year is short or long
- **WHEN** the first financial year is set shorter or longer than twelve months
- **THEN** it is accepted

#### Scenario: Base currency defaults to SGD
- **GIVEN** a new organisation
- **WHEN** no base currency is chosen
- **THEN** the base currency is SGD

#### Scenario: Books start date and time zone are recorded
- **WHEN** the books start date and the time zone are set
- **THEN** both are stored with the financial year and base currency

### Requirement: FR-ORG-10 Accounting framework and policies
The system SHALL record the accounting framework (FRS, SFRS(I) or SFRS for Small Entities) and its edition, effective from a financial year, with the accountant's confirmation of eligibility, and the accounting policies the engine applies, each policy with its approver and date.

Source: PRD-001 §8.1 · Priority M · Phase P1

#### Scenario: Framework applies from a financial year
- **WHEN** a framework and its edition are recorded effective from a financial year
- **THEN** they apply from that financial year
- **AND** the accountant's confirmation of eligibility is recorded with them

#### Scenario: Policies the engine applies
- **WHEN** the accounting policies are recorded
- **THEN** they cover revenue (including subscriptions and fixed-fee projects), government grants, leases, development costs, impairment, classification of SAFEs and notes, measurement basis for conversions, and capitalisation threshold
- **AND** each policy carries its approver and date

#### Scenario: Policy-dependent rules read the recorded policy
- **WHEN** a rule depends on an accounting policy
- **THEN** it reads that policy from the organisation's recorded policies

#### Scenario: Transaction no policy covers
- **WHEN** a transaction is not covered by any recorded policy
- **THEN** it goes to an accountant-approved journal

#### Scenario: Statements show the framework and edition
- **GIVEN** the accountant has approved the policies for the year
- **WHEN** statements for that year are produced
- **THEN** they show the framework and edition

#### Scenario: Management statements until the policies are approved
- **GIVEN** the accountant has not approved the policies for the year
- **WHEN** statements for that year are produced
- **THEN** they are titled "management statements" (open question 31)

#### Scenario: G4-017 First close before the framework and policies are approved
- **GIVEN** the accountant has not yet approved the framework and policies
- **WHEN** the first close produces statements
- **THEN** the statements are titled "management statements" and name no framework
- **AND** after approval they show the framework and edition
- **AND** a financial year beginning on or after 1 Jan 2027 under SFRS for Small Entities shows the third edition

### Requirement: FR-ORG-03 GST status and accounting basis
The system SHALL record the organisation's GST status with effective dates (registered from / deregistered from), its return frequency and period alignment, and its accounting basis: invoice basis, or cash basis only under IRAS's Cash Accounting Scheme.

Source: PRD-001 §8.1 · Priority M · Phase P1

#### Scenario: GST status follows its effective dates
- **WHEN** the organisation is recorded as GST-registered from one date and deregistered from a later date
- **THEN** its GST status on any date follows those effective dates

#### Scenario: Return frequency and period alignment
- **WHEN** the return frequency and period alignment are set
- **THEN** the GST return periods follow them

#### Scenario: Invoice basis
- **WHEN** the GST accounting basis is set
- **THEN** invoice basis can be selected

#### Scenario: Cash basis under the Cash Accounting Scheme
- **GIVEN** the rules of IRAS's Cash Accounting Scheme are pinned (ENG-45)
- **WHEN** the organisation chooses cash basis
- **THEN** it is allowed only with taxable supplies up to S$1m and IRAS approval
- **AND** it applies from the start of the next return period, for three years

#### Scenario: Cash basis not offered before its rules are pinned
- **GIVEN** the rules of the Cash Accounting Scheme are not yet pinned (ENG-45)
- **WHEN** the GST accounting basis is set
- **THEN** cash basis is not offered

### Requirement: FR-ORG-04 Organisation defaults
The system SHALL let the organisation set defaults for payment terms, document number sequences per document type, rounding and FX tolerances, and the capitalisation threshold.

Source: PRD-001 §8.1 · Priority M · Phase P1

#### Scenario: Number sequence per document type
- **WHEN** a document number sequence is set up for a document type
- **THEN** it has its own prefix, next number and gap-free option

#### Scenario: Gap-free sequence
- **GIVEN** the gap-free option is on for a document type's sequence
- **WHEN** documents of that type are numbered
- **THEN** the sequence leaves no gaps

#### Scenario: A gap-free sequence numbers a document when it posts
- **GIVEN** a document type's sequence is gap-free
- **WHEN** a document of that type posts, or its posting fails
- **THEN** it is numbered in the transaction that posts it, under the sequence's lock (ENG-47), so a failed posting takes no number

#### Scenario: Another sequence numbers a document when it is saved
- **GIVEN** a document type's sequence is not gap-free
- **WHEN** a document of that type is saved
- **THEN** it is numbered then, so an abandoned draft leaves a gap
- **AND** a number never changes and never repeats within its document type

#### Scenario: Other defaults are stored
- **WHEN** the payment terms, the rounding and FX tolerances and the capitalisation threshold are set
- **THEN** they are stored as the organisation's defaults

#### Scenario: The defaults' recommended values
- **WHEN** an organisation has not changed its defaults
- **THEN** invoices and bills of a contact with no payment terms of its own fall due 30 days after their date
- **AND** the capitalisation threshold is S$1,000, under FM-1 (rev 0.34, provisional until sign-off)

### Requirement: FR-ORG-05 Effective-dated statutory parameters
The system SHALL maintain effective-dated statutory parameters (GST rates and codes, CPF rate tables and ceilings, SDL, SHG, CIT tiers and rebates) as versioned packs with source URL, effective date, verifier and dual approval (§10.6).

Source: PRD-001 §8.1 · Priority M · Phase P1

#### Scenario: A pack version records its provenance
- **WHEN** a version of a statutory parameter pack is added
- **THEN** it records its source URL, effective date and verifier
- **AND** it needs dual approval

#### Scenario: The version effective on a date applies
- **GIVEN** two versions of a pack with different effective dates
- **WHEN** a statutory parameter is read for a date
- **THEN** the value comes from the version effective on that date

### Requirement: FR-ORG-06 Feature switches
The system SHALL provide organisation-level feature switches for payroll, projects, multi-currency, the AI assistant and portals.

Source: PRD-001 §8.1 · Priority S · Phase P1

#### Scenario: The UI shows only what is used
- **GIVEN** payroll, projects, multi-currency, the AI assistant or portals is switched off for the organisation
- **WHEN** a user of that organisation uses the UI
- **THEN** the UI does not show that feature

#### Scenario: The switches' recommended values reach the session
- **WHEN** an organisation has not changed its feature switches
- **THEN** multi-currency is on, and payroll, projects, portals and the AI assistant are off
- **AND** the session carries the switches in effect today (rev 0.34, provisional until sign-off)

#### Scenario: The AI assistant switches on only under dual control
- **WHEN** someone switches the AI assistant on
- **THEN** the change needs FM-1's dual approval: it waits as an approval request until another member holding A approves it (rev 0.36)

### Requirement: FR-ORG-07 Data region and retention visible to the Owner
The system SHALL show the data region and retention settings to the Owner.

Source: PRD-001 §8.1 · Priority S · Phase P1

#### Scenario: Owner sees region and retention
- **WHEN** the Owner opens the organisation settings
- **THEN** the data region and the retention settings are visible

#### Scenario: Platform-fixed settings are read-only
- **GIVEN** the data region or a retention setting is fixed by the platform
- **WHEN** the Owner views it
- **THEN** it is shown read-only

#### Scenario: The records' retention only lengthens
- **GIVEN** records are kept 7 years after the financial year, the recommended value
- **WHEN** a change would shorten it, from its date or by a change already recorded for later
- **THEN** the change is refused, by the database as well as the application (rev 0.34)
- **AND** AI conversations are kept 365 days, and both retention settings are under FM-1

### Requirement: FR-ORG-08 Recommended defaults, configurable
Every policy the founders were asked to decide (§20, recommended defaults) and every evidence rule (FR-DOC-06) SHALL ship set to its recommended value, with a one-line reason and a link to the guide, and SHALL stay configurable.

Source: PRD-001 §8.1 · Priority M · Phase P1

#### Scenario: Setting shows whether it is recommended
- **WHEN** a setting that has a recommended value is shown
- **THEN** it shows **Recommended** while it holds the recommended value
- **AND** it shows **Changed from recommended** otherwise

#### Scenario: Reset to recommended
- **GIVEN** a setting changed from its recommended value
- **WHEN** the user chooses "Reset to recommended"
- **THEN** the setting returns to its recommended value

#### Scenario: Changes are effective-dated and audited
- **WHEN** one of these settings is changed
- **THEN** the change is effective-dated
- **AND** the change is audited

#### Scenario: Dual approval where FM-1 applies
- **GIVEN** a setting to which FM-1 applies (thresholds, retention, AI and MCP settings, approval policies)
- **WHEN** it is changed
- **THEN** the change needs dual approval

#### Scenario: A change never takes effect in the past
- **WHEN** a setting is changed
- **THEN** the change takes effect from today in the organisation's time zone, or a later date, never an earlier one
- **AND** earlier values are kept with their dates, so the value on any day can be read again (rev 0.34)

#### Scenario: Reset records the recommended value
- **WHEN** the user chooses "Reset to recommended"
- **THEN** the recommended value is recorded as a new change, and nothing is deleted (rev 0.34)

#### Scenario: FM-1 settings wait for a second approver
- **WHEN** a setting under FM-1 is changed
- **THEN** the change waits as an approval request, and the setting keeps its value until another member holding A approves it (rev 0.36, superseding rev 0.34's refusal; provisional until sign-off)

### Requirement: FR-ORG-09 Company logo and brand, applied everywhere at once
The Owner or Administrator SHALL be able to upload the company logo (SVG or PNG; light and dark variants; a square icon for the browser tab and app icon) and, optionally, an accent colour, and the change SHALL apply everywhere at once.

Source: PRD-001 §8.1 · Priority M · Phase P1

#### Scenario: SVG logo is reduced to a safe subset
- **WHEN** the Owner or Administrator uploads an SVG logo
- **THEN** it is reduced to a safe subset with no scripts, event handlers, external references or embedded fonts
- **AND** it is rasterised to PNG for emails and PDFs

#### Scenario: The change takes effect immediately
- **WHEN** a new logo or accent colour is saved
- **THEN** it takes effect immediately in the app header, the sign-in page, emails, the default theme of document templates (§8.25), PDF reports and board packs, and the customer portal

#### Scenario: Preview before saving
- **WHEN** the Owner or Administrator changes the logo or accent colour
- **THEN** a preview of each of those places is shown before saving

#### Scenario: Accent colour is limited to documents and emails
- **WHEN** an accent colour is chosen
- **THEN** it is chosen from contrast-checked options
- **AND** it applies to documents and emails only

#### Scenario: The app keeps the design-system colours
- **GIVEN** an accent colour is set
- **WHEN** the app itself is shown
- **THEN** it keeps the design-system colours
- **AND** violet still marks AI content

#### Scenario: Issued documents keep their logo
- **GIVEN** a document was issued with an earlier logo
- **WHEN** the logo changes
- **THEN** that document keeps the logo it was issued with (FR-DOC-04)

#### Scenario: Logo and brand changes are versioned and audited
- **WHEN** the logo or accent colour changes
- **THEN** the change is versioned
- **AND** the change is audited

#### Scenario: G4-099 Unsafe SVG logo and hostile uploads
- **WHEN** an SVG logo with a script and an external reference, an XLSX ZIP bomb and a DOCX with an external entity are uploaded
- **THEN** the SVG is sanitised and rasterised for emails and PDFs
- **AND** the bomb is rejected within the size and ratio limits
- **AND** the entity is not resolved
