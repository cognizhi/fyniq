# tax/gst-and-filings Specification

## Purpose
How Fyniq meets its Singapore tax and statutory obligations: GST codes and the F5 return, filing and GST corrections, InvoiceNow submissions and the filing gate, IRAS audit listings, the GST registration monitor, corporate income tax support, the statutory calendar, the ACRA filing pack and packs for other countries. Derived from PRD-001 §8.11.

## Requirements

### Requirement: FR-TAX-01 Singapore GST codes
The system SHALL provide the Singapore GST codes (the workbook's 12 plus reverse-charge and other codes required by current IRAS guidance) with their F5 box mapping, maintained as an effective-dated pack.

Source: PRD-001 §8.11 · Priority M · Phase P1

#### Scenario: Codes carry their F5 box mapping
- **WHEN** the GST codes are listed
- **THEN** they include the workbook's 12 codes, the reverse-charge codes and the other codes current IRAS guidance requires, each with its F5 box mapping

#### Scenario: Codes come from the effective pack version
- **GIVEN** two versions of the GST code pack with different effective dates
- **WHEN** a code is applied to a transaction
- **THEN** the code and its F5 box mapping come from the pack version effective on the transaction's date

#### Scenario: Reverse charge on
- **GIVEN** the organisation is GST-registered
- **AND** the tax agent has set the effective-dated organisation setting that it is not entitled to full input tax credit (IRAS; open question 29)
- **WHEN** a purchase is coded on a date the setting is in effect
- **THEN** the reverse-charge codes are offered

#### Scenario: Reverse charge off
- **GIVEN** the organisation is GST-registered and the reverse-charge setting is off
- **WHEN** a purchase from an overseas vendor is coded
- **THEN** the reverse-charge codes are not offered
- **AND** the organisation gives the overseas vendor its GST registration number

#### Scenario: Overseas GST is a cost while not registered
- **GIVEN** the organisation is not GST-registered
- **WHEN** an overseas vendor charges GST on a purchase
- **THEN** that GST is part of the cost of the purchase

#### Scenario: G4-011 Reverse charge off, overseas purchase
- **GIVEN** a GST-registered organisation with reverse charge off
- **WHEN** it buys from an overseas vendor
- **THEN** no reverse-charge code is offered
- **AND** the GST number is given to the vendor
- **AND** no output or input tax is posted

### Requirement: FR-TAX-02 GST F5 return
The system SHALL produce the GST F5 return for any period, with F7 (corrections) and F8 (final) support and, from the first filed return, the adjustment queue (FR-TAX-12).

Source: PRD-001 §8.11 · Priority M · Phase P1 (F5) / P2 (F7, F8)

#### Scenario: Every box is computed
- **WHEN** the F5 return is produced for any period
- **THEN** every box is filled, including the boxes the workbook left manual

#### Scenario: Breakdown and drill-down
- **WHEN** the F5 return is viewed
- **THEN** it shows a breakdown by GST code
- **AND** each figure drills down to its transactions

#### Scenario: Reconciliation to the GST control account
- **WHEN** the F5 return is produced
- **THEN** it reconciles to the GST control account with a roll-forward (INV-07)

#### Scenario: Corrections and final returns
- **WHEN** a return needs correcting or the organisation files its final return
- **THEN** an F7 (corrections) or an F8 (final) return is supported

#### Scenario: Adjustment queue from the first filed return
- **GIVEN** the organisation has filed its first return
- **WHEN** a GST correction to a filed period arises
- **THEN** it goes to the adjustment queue (FR-TAX-12)

### Requirement: FR-TAX-03 Filing
The system SHALL lock the return period on filing, record the filing reference and keep the filed return as an immutable version; from P2 it SHALL submit through IRAS's GST return API.

Source: PRD-001 §8.11 · Priority M · Phase P1 (record) / P2 (API)

#### Scenario: Filing locks the return period
- **WHEN** a GST return is recorded as filed with its filing reference
- **THEN** the return period is locked
- **AND** the filing reference is stored with the return

#### Scenario: The filed return is an immutable version
- **WHEN** a return is filed
- **THEN** it is kept as an immutable version holding its boxes, the documents behind each box and the filing reference

#### Scenario: An F7 never overwrites the original
- **GIVEN** a filed return
- **WHEN** an F7 is filed for its period
- **THEN** the F7 is added as a new version
- **AND** the original version is unchanged

#### Scenario: GST return lock and ledger period lock are separate
- **WHEN** a GST return period is locked
- **THEN** that lock is separate from the ledger period lock (FR-GL-08)

#### Scenario: Submission through the IRAS API
- **GIVEN** phase P2 and access to IRAS's GST return API
- **WHEN** a return is filed
- **THEN** it is submitted through that API

### Requirement: FR-TAX-04 IRAS audit listings
The system SHALL produce sales and purchase listings in Excel/CSV for any period, as IRAS's current GST audit guidance asks for.

Source: PRD-001 §8.11 · Priority M · Phase P1

#### Scenario: Listings for any period
- **WHEN** the IRAS audit listings are requested for any period
- **THEN** the sales listing and the purchase listing are produced in Excel and CSV with the content IRAS's current GST audit guidance asks for

#### Scenario: IRAS Audit File only with a published specification
- **GIVEN** IRAS has not republished an IRAS Audit File (IAF) specification
- **WHEN** audit listings are requested
- **THEN** no IAF is produced (§14.2)

### Requirement: FR-TAX-05 GST registration monitor
The system SHALL monitor GST registration with retrospective and prospective tests against the threshold, as in the workbook.

Source: PRD-001 §8.11 · Priority M · Phase P1

#### Scenario: Retrospective test
- **WHEN** the registration monitor runs
- **THEN** it tests the calendar-year taxable supplies against the threshold

#### Scenario: Prospective test
- **WHEN** the registration monitor runs
- **THEN** it tests the forecast taxable supplies of the next 12 months against the threshold

### Requirement: FR-TAX-10 InvoiceNow submission population
When the InvoiceNow requirement applies (FR-AR-12), every document that feeds the GST return SHALL map to a submission, or to a recorded exclusion with its reason.

Source: PRD-001 §8.11 · Priority M before GST registration; else S · Phase As FR-AR-12

#### Scenario: Every GST document is accounted for
- **GIVEN** the InvoiceNow requirement applies (FR-AR-12)
- **WHEN** a document feeds the GST return
- **THEN** it maps to a submission, or to a recorded exclusion with its reason

#### Scenario: Kinds of submission
- **WHEN** submissions are prepared
- **THEN** they cover Peppol sales and purchases; solution-extracted sales (customer not on the network, including provider-issued invoices under FR-PSP-03); solution-extracted purchases (supplier not on the network: captured bills and receipts, FR-DOC-12); aggregated POS, simplified-invoice or petty-cash data (staff claims, card lines); credit and debit notes; and resubmissions with the same document UUID

#### Scenario: Submission status
- **WHEN** a submission is tracked
- **THEN** its status is one of queued, sent, IRAS acknowledgement ID received, or failed with retry

#### Scenario: Submission due date
- **WHEN** a submission belongs to a GST return
- **THEN** it is due by the earlier of the return's filing date and its due date

#### Scenario: Access Point support confirmed before registration
- **WHEN** the organisation prepares to register for GST
- **THEN** the Access Point's support for solution-extracted and aggregated submissions is confirmed before registration [verify]

#### Scenario: G4-030 Supplier not on the network and staff claims
- **GIVEN** a PDF bill from a GST-registered supplier not on the network and a month of staff claims
- **WHEN** the submissions are prepared
- **THEN** the bill goes as a solution-extracted purchase
- **AND** the staff claims may go aggregated as petty-cash data
- **AND** both reach an IRAS acknowledgement ID before filing

#### Scenario: G4-031 Provider-issued tax invoice under Model B
- **GIVEN** a provider-issued tax invoice to a consumer under Model B
- **WHEN** the submissions are prepared
- **THEN** it is submitted as a solution-extracted sale (aggregated only if it is a simplified tax invoice)
- **AND** it is included in the reconciliation

#### Scenario: G4-032 Credit note and a code correction
- **GIVEN** a credit note on an acknowledged invoice and a code correction on another invoice
- **WHEN** the submissions are prepared
- **THEN** the credit note is submitted as its own data
- **AND** the corrected invoice is resubmitted with the same document UUID

#### Scenario: G4-034 Documents with recorded exclusions
- **GIVEN** a purchase from a non-registered supplier, an import permit and a blocked motor-car expense
- **WHEN** the submission population is built
- **THEN** each has a recorded exclusion with its reason
- **AND** no submission is expected for them

### Requirement: FR-TAX-11 Filing gate with a contingency
F5 filing SHALL wait until every in-scope item has an IRAS acknowledgement ID or a recorded exclusion, with a dual-approved contingency for outages. The business stays responsible for complete submission; IRAS may extend due dates at its discretion.

Source: PRD-001 §8.11 · Priority M before GST registration; else S · Phase As FR-AR-12

#### Scenario: Filing waits for every in-scope item
- **GIVEN** an in-scope item has neither an IRAS acknowledgement ID nor a recorded exclusion
- **WHEN** a user tries to file the F5 return
- **THEN** filing waits

#### Scenario: Contingency filing during an outage
- **GIVEN** an Access Point or IRAS outage would make the return late
- **WHEN** the Owner and the tax agent file with the gap recorded
- **THEN** the filing needs dual approval, a reason, evidence of the outage and any IRAS correspondence

#### Scenario: Unsent items stay queued
- **GIVEN** a return was filed under the contingency
- **WHEN** service returns
- **THEN** the unsent items, which stayed queued, are resubmitted

#### Scenario: G4-033 Access Point outage before the due date
- **GIVEN** an Access Point outage two days before the filing due date
- **WHEN** the return is to be filed
- **THEN** filing is possible only through the dual-approved contingency with evidence
- **AND** unsent items stay queued and are resubmitted
- **AND** nothing is marked complete without an IRAS acknowledgement ID

### Requirement: FR-TAX-12 GST corrections from the first filed return
From the first filed return, the system SHALL post GST corrections to filed periods in the open ledger period and place them in an adjustment queue with the original tax period and the amount per box; the tax agent decides how each is filed.

Source: PRD-001 §8.11 · Priority M when GST-registered; else S · Phase P1

#### Scenario: Corrections post in the open period and join the queue
- **WHEN** a document whose tax point (or claim date) falls in a filed period, a changed GST code or amount on a filed document, or a refund is recorded
- **THEN** it posts in the open ledger period
- **AND** it joins the adjustment queue with the original tax period and the amount per box

#### Scenario: IRAS tests propose the route
- **WHEN** the queue assesses its items
- **THEN** it applies IRAS's tests from the `sg-gst` pack (net GST error over all affected periods up to S$3,000, and other boxes up to 5% of Box 4; never for Box 12 errors), re-checked each year
- **AND** it proposes "next F5" or "F7"

#### Scenario: The tax agent decides
- **WHEN** the queue proposes a route
- **THEN** the tax agent decides between "next F5" and "F7"

#### Scenario: Working paper for a manual F7
- **GIVEN** phase P1, where the in-app F7 is not yet available (it is P2)
- **WHEN** the tax agent decides on an F7
- **THEN** a correction working paper with evidence supports a manual F7

#### Scenario: Time limits shown
- **WHEN** the adjustment queue is viewed
- **THEN** it shows the five-year limit for claims and refunds and the penalty risk after one year

#### Scenario: G4-035 Wrong code found after the period is filed and closed
- **GIVEN** Q1 is filed and the ledger month closed
- **WHEN** a Q1 sales invoice coded SR should have been ZR
- **THEN** the filed Q1 return version is unchanged
- **AND** the correction posts in the open period tagged Q1
- **AND** the queue computes both IRAS tests and proposes "next F5" or "F7"
- **AND** the tax agent's decision and working paper are stored
- **AND** no ledger lock is broken

### Requirement: FR-TAX-06 Corporate income tax support
The system SHALL support corporate income tax with a tax-adjusted profit working paper, SUTE/PTE and rebate estimates, and an ECI and Form C-S/C-S Lite data pack for the tax agent.

Source: PRD-001 §8.11 · Priority S · Phase P2

#### Scenario: Tax-adjusted profit working paper
- **WHEN** the working paper is prepared for a year
- **THEN** it adjusts the profit for non-deductible expenses and capital allowances

#### Scenario: Exemption and rebate estimates
- **WHEN** the tax is estimated
- **THEN** the SUTE or PTE exemption and the rebate are estimated

#### Scenario: Data pack for the tax agent
- **WHEN** the tax agent requests the year's data
- **THEN** an ECI and Form C-S/C-S Lite data pack is produced

### Requirement: FR-TAX-07 Statutory calendar
The system SHALL keep a statutory calendar of the organisation's obligations, with dates derived from its FY and GST periods, reminders and Inbox tasks.

Source: PRD-001 §8.11 · Priority M · Phase P1

#### Scenario: Obligations covered
- **WHEN** the statutory calendar is built
- **THEN** it covers GST F5, CPF, IR8A/AIS, ECI, Form C-S/C, the ACRA annual return, the AGM where applicable, register-of-controllers updates, and share-issue approvals and returns of allotment (FR-EQ-09)

#### Scenario: Dates follow the FY and GST periods
- **WHEN** the organisation's FY or GST periods are set or change
- **THEN** the calendar dates are derived from them

#### Scenario: Reminders and Inbox tasks
- **WHEN** an obligation approaches its date
- **THEN** a reminder is sent and an Inbox task exists for it

#### Scenario: Overdue items on Checks
- **WHEN** an obligation passes its date unmet
- **THEN** it shows as overdue on Checks (CMP-02)

### Requirement: FR-TAX-08 ACRA filing pack
The system SHALL prepare an ACRA filing pack: a filing-category classifier, a BizFinx data pack, then XBRL (Taxonomy 2026) validated with Arelle (§14.2).

Source: PRD-001 §8.11 · Priority C · Phase P3

#### Scenario: Filing category
- **WHEN** the classifier runs for a financial year
- **THEN** it classifies the company as a solvent exempt private company, a simplified XBRL filer or a full XBRL filer

#### Scenario: BizFinx data pack
- **WHEN** the filing pack is prepared
- **THEN** a BizFinx data pack is produced

#### Scenario: XBRL validated with Arelle
- **WHEN** XBRL is generated
- **THEN** it uses Taxonomy 2026 and is validated with Arelle

### Requirement: FR-TAX-09 Country packs
The system SHALL support tax codes, return formats and payroll rules for other countries as new effective-dated packs.

Source: PRD-001 §8.11 · Priority C · Phase P3

#### Scenario: Another country added as packs
- **WHEN** another country is supported
- **THEN** its tax codes, return formats and payroll rules are added as new effective-dated packs
