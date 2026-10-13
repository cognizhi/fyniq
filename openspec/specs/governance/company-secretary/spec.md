# governance/company-secretary Specification

## Purpose
How Fyniq keeps the company's corporate records and obligations in order with the AI company secretary assistant, which checks, explains, reminds and drafts while the named human secretary and the directors decide, sign and file: the corporate profile reconciled to ACRA, the ACRA pack, the cross-checks, the workspace, document drafts, the annual-return pack and the Company secretary role. Derived from PRD-001 §9.12.

## Requirements

### Requirement: FR-CS-01 Corporate profile
The system SHALL keep the corporate profile as effective-dated records, reconciled to the ACRA business profile; the registers of directors, secretaries and auditors are kept by ACRA, so the profile mirrors them.

Source: PRD-001 §9.12 · Priority M · Phase P1

#### Scenario: What the profile holds
- **WHEN** the corporate profile is viewed
- **THEN** it holds, effective-dated, the UEN, name, registered office, industry code (SSIC), FY end and exempt-private-company status (FR-EQ-11, FR-EQ-25); the directors, secretary and auditors with their appointment and cessation dates; the registrable controllers; the nominee directors; and the nominee shareholders

#### Scenario: Nominee shareholders
- **WHEN** a nominee shareholder is recorded or changes
- **THEN** the company's register of nominee shareholders is due for update within 7 days, then filing to ACRA's Central ROND & RONS within 2 business days (required since 16 Jun 2025)

#### Scenario: Reconciled to the ACRA business profile
- **WHEN** the profile is reconciled
- **THEN** in P1 it is reconciled to an uploaded ACRA extract
- **AND** from P2 the business profile is fetched through ACRA's API Marketplace (Entity Information Query and Business Profile API; Corppass account, quota-based), read-only, since ACRA offers no filing API

#### Scenario: A change opens its lodgement checklist
- **WHEN** an officer, an officer's particulars or the registered office changes
- **THEN** a lodgement checklist opens with the deadline from the ACRA pack (FR-CS-02): 14 days (s173A, s143)

#### Scenario: G4-102 Nominee filing and controller notice overdue
- **GIVEN** a nominee shareholder is recorded, but the central RONS filing is still missing 2 business days after the private register was updated, and a controller has had no notice for 12 months
- **WHEN** the company secretary checks run
- **THEN** each shows as due or overdue, with the rule and the ACRA source

### Requirement: FR-CS-02 ACRA pack
The system SHALL keep corporate deadlines and rules as an effective-dated pack `sg-acra@<year>.<n>`, sourced and dual-approved like the statutory packs (§10.6).

Source: PRD-001 §9.12 · Priority M · Phase P1

#### Scenario: What the pack covers
- **WHEN** the ACRA pack is loaded
- **THEN** it covers AGM and financial-statement timing (including the private-company route that dispenses with the AGM), the annual return, changes of officers, address and controllers, share-issue approvals and their lapse, exempt-private-company solvency and XBRL filing categories, and audit-exemption criteria

#### Scenario: Rules checked against their sources
- **WHEN** the first version of the pack is prepared
- **THEN** its rules are those checked against the Companies Act and ACRA on 12 Oct 2026 (source notes: docs/prd/research/secretary-xero-integrations-2026-10-12.md)

#### Scenario: Guidance kept apart from the Act
- **WHEN** the Act and ACRA's guidance differ on a point (for example a 14-day expectation for returns of allotment and share transfers that the Act does not state)
- **THEN** the point is stored as guidance with its source

### Requirement: FR-CS-03 Cross-checks
The system SHALL run the company secretary's cross-checks, each also a check in Appendix C so it works without the agent, and SHALL report a check whose required input is missing or out of date as Not assessed, never as OK.

Source: PRD-001 §9.12 · Priority M · Phase P1

#### Scenario: Checks run
- **WHEN** the cross-checks run
- **THEN** they cover books closed for the FY; financial statements approved by the directors, and their dates; the deadlines for sending or laying them; annual-return consistency (ledger share capital = equity register = ACRA; officers and registered office = ACRA profile); exempt-private-company status and solvency indicators (net assets, runway); the audit-exemption classifier; directors' accounts in debit (FR-EQ-11); share-issue approvals about to lapse (FR-EQ-09); controller changes (FR-EQ-25); the GST threshold monitor (FR-TAX-05); ECI and Form C-S status; CPF and IR8A; filings without their acknowledgement (FR-DOC-06); and unconfirmed related-party balances (FR-EQ-12)

#### Scenario: Checks work without the agent
- **GIVEN** the company secretary agent is not running
- **WHEN** the checks engine runs
- **THEN** each cross-check still runs as a check in Appendix C

#### Scenario: Missing or out-of-date input
- **GIVEN** a check's required input is missing or out of date, for example no FY end, no GST status, no controller data, or an ACRA extract older than the last recorded change
- **WHEN** the check runs
- **THEN** it reports Not assessed and names the input, never OK

#### Scenario: No "all clear" with unassessed items
- **GIVEN** any check reports Not assessed
- **WHEN** a run report or summary is produced
- **THEN** it never says "all clear"
- **AND** it lists those items first

#### Scenario: The same with the model off
- **GIVEN** the model is off
- **WHEN** the checks run
- **THEN** the Not assessed and no-"all clear" rules still hold (AI-64, AI-67)

#### Scenario: G4-110 Incomplete profile with the model unavailable
- **GIVEN** the corporate profile has no FY end and no controller data
- **WHEN** the secretary agent runs with the model unavailable
- **THEN** the affected checks report Not assessed and name the missing inputs
- **AND** the summary never says "all clear"
- **AND** CMP-01 shows not assessed

### Requirement: FR-CS-04 Company secretary workspace
The system SHALL provide a company secretary workspace at Compliance → Company secretary.

Source: PRD-001 §9.12 · Priority M · Phase P1

#### Scenario: Workspace contents
- **WHEN** a user opens Compliance → Company secretary
- **THEN** it shows the latest run report, open findings, the obligations calendar, drafts awaiting approval, the history of runs with "new since last run", and the schedule settings

#### Scenario: Basic mode summary
- **WHEN** the workspace is shown in Basic mode
- **THEN** it shows a plain summary (for example "2 things need you this week")

#### Scenario: Expert mode findings
- **WHEN** the workspace is shown in Expert mode
- **THEN** it adds every finding with its rule and source

### Requirement: FR-CS-05 Document drafts
The system SHALL draft company secretarial documents from templates filled from the records; drafts are proposals.

Source: PRD-001 §9.12 · Priority S · Phase P1

#### Scenario: Documents drafted
- **WHEN** a draft is requested
- **THEN** it can be a directors' resolution (approve the financial statements, allot shares, appoint or remove an officer, change the registered office), a members' written resolution, an AGM notice and minutes, or the papers for dispensing with the AGM, filled from the records

#### Scenario: Signed drafts become evidence
- **GIVEN** a draft, which is a proposal
- **WHEN** it is signed and uploaded
- **THEN** it becomes evidence (FR-DOC-05)

#### Scenario: Minute book
- **WHEN** minutes or a written resolution are signed
- **THEN** they go in the minute book (minutes within one month, s188)
- **AND** the agent reminds until they do

#### Scenario: Members' written resolutions
- **WHEN** a members' written resolution is drafted
- **THEN** it follows s184A–G (ordinary: a majority; special: at least 75% of total voting rights)
- **AND** it is not used for a resolution that needs special notice

#### Scenario: Notice of general meetings
- **WHEN** a general meeting notice is drafted
- **THEN** it gives 14 days' notice (s177)

### Requirement: FR-CS-06 The named human secretary
The corporate profile SHALL record the appointed company secretary, a person, internal or from a corporate secretarial firm, with the Company secretary role; the agent addresses its findings to them and to the directors.

Source: PRD-001 §9.12 · Priority M · Phase P1

#### Scenario: Findings addressed to the secretary and directors
- **WHEN** the agent reports findings
- **THEN** they are addressed to the recorded secretary and to the directors

#### Scenario: No secretary after 6 months
- **GIVEN** no secretary is recorded 6 months after incorporation
- **WHEN** the checks run
- **THEN** it is an error (ACRA guidance; the office may not stay vacant for more than 6 months, s171(4A))

#### Scenario: Sole director as secretary
- **GIVEN** a sole director is recorded as the secretary
- **WHEN** the checks run
- **THEN** it is an error (s171(1E))

### Requirement: FR-CS-07 Annual-return pack
The system SHALL assemble an annual-return pack by filing category for the secretary to file in BizFile+.

Source: PRD-001 §9.12 · Priority S · Phase P2

#### Scenario: Pack contents
- **WHEN** the annual-return pack is assembled
- **THEN** it holds the officers, members, share capital and financial-statement figures the annual return asks for, by filing category

#### Scenario: XBRL readiness
- **WHEN** the filing category needs XBRL
- **THEN** the pack shows XBRL readiness with FR-TAX-08

### Requirement: FR-CS-08 Company secretary role
The system SHALL provide a Company secretary role for an internal or external user.

Source: PRD-001 §9.12 · Priority M · Phase P1

#### Scenario: What the role sees and does
- **WHEN** a user with the Company secretary role signs in
- **THEN** they see the corporate profile, the registers, the equity register, and the AI company secretary's runs and drafts
- **AND** they can upload evidence and record filings

#### Scenario: No transaction, payroll or bank detail unless granted
- **GIVEN** the role has not been granted more
- **WHEN** the company secretary looks for transaction, payroll or bank detail
- **THEN** it is not shown

#### Scenario: External access is time-boxed
- **GIVEN** the company secretary is an external user
- **WHEN** their access is granted
- **THEN** it is time-boxed (FR-ADM-01)
