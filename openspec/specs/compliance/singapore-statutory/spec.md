# compliance/singapore-statutory Specification

## Purpose
The Singapore statutory rules Fyniq implements and checks automatically: record keeping, IRAS audit files, GST, tax invoices, bad-debt relief, InvoiceNow, corporate income tax, ACRA filings, share issues and directors' balances, the company secretary, CPF, SDL, SHG and levies, MOM employment records, AIS and IR21, electronic signatures, and MAS as reference only. Derived from PRD-001 §14.2.

## Requirements

### Requirement: Record keeping and retention
The system SHALL keep accounting, tax and GST records as an immutable ledger, audit trail and documents for at least their statutory periods, with a default retention of 7 years after the FY end and legal hold (NFR-RET-01, §11.9), retrievable also after strike-off.

Source: PRD-001 §14.2

#### Scenario: Statutory retention periods are covered
- **WHEN** the retention of a record is set
- **THEN** it covers Companies Act s199 (accounting records kept ≥ 5 years after the FY end; ACRA, Feb 2026), income tax (5 years from the relevant Year of Assessment, 5 to 6 years after the FY end depending on the FY-end month; electronic records allowed; IRAS, Jan 2026) and GST (5 years from the end of the period; IRAS, Sep 2026)

#### Scenario: IRAS income tax examples
- **GIVEN** a FY to 31 Dec 2018 and a FY to 30 Sep 2018
- **WHEN** their income tax retention is computed
- **THEN** the records of both are kept until 31 Dec 2023

#### Scenario: Default retention and legal hold
- **WHEN** a record is retained
- **THEN** its default retention is 7 years after the FY end
- **AND** a legal hold keeps it beyond that

#### Scenario: Retrievable after strike-off
- **GIVEN** the company has been struck off
- **WHEN** its records are needed
- **THEN** they are still retrievable

#### Scenario: Original currency and SGD stored
- **WHEN** a foreign-currency transaction is recorded
- **THEN** both the original currency amount and the SGD amount are stored

#### Scenario: Automated retention checks
- **WHEN** CI runs
- **THEN** it checks that app roles have no UPDATE or DELETE on posted and audit tables
- **AND** a daily check confirms the bucket retention is locked and every committed object's retain-until is ≥ its computed expiry
- **AND** a release check confirms the GL export re-imports and reproduces the TB

### Requirement: IRAS audit files and listings
The system SHALL provide the GL, sales listing and purchase listing exports (FR-RPT-10, FR-TAX-04) for IRAS GST audits, with no dependency on ASR+.

Source: PRD-001 §14.2

#### Scenario: Listings in Excel or CSV
- **WHEN** IRAS asks for audit data under its current GST audit page
- **THEN** the sales and purchase listings are exported in Excel or CSV
- **AND** the GL can be exported with them

#### Scenario: IRAS Audit File only if republished
- **WHEN** audit exports are offered
- **THEN** an IRAS Audit File (IAF), which appears only in older guidance, is produced only if IRAS republishes a specification for it

#### Scenario: No dependency on ASR+
- **GIVEN** the Accounting Software Register Plus (ASR+) is closed to new applications while IRAS rebrands it (Aug 2026)
- **WHEN** any audit or filing function runs
- **THEN** it does not depend on ASR+

#### Scenario: Export round-trip tests
- **WHEN** the tests run
- **THEN** export round-trip tests (QA-RPT-03) verify the exports

### Requirement: GST rate and registration threshold
The system SHALL apply GST from an effective-dated GST pack and SHALL monitor the GST registration threshold (FR-TAX-05).

Source: PRD-001 §14.2

#### Scenario: Rate by date
- **WHEN** GST is computed for a supply dated 31 Dec 2023
- **THEN** the rate is 8%
- **AND** for a supply dated 1 Jan 2024 or later the rate is 9%

#### Scenario: Golden tests per code and date
- **WHEN** the GST tests run
- **THEN** golden tests per code and date pin 31 Dec 2023 → 8% and 1 Jan 2024 → 9%

#### Scenario: Registration threshold tests
- **WHEN** the threshold monitor assesses the registration threshold of S$1m
- **THEN** it applies the retrospective test by calendar year (apply by 30 Jan; effective 1 Mar) and the prospective test (apply within 30 days; effective 2 months later from 1 Jul 2025)

#### Scenario: Alert as turnover nears the threshold
- **WHEN** rolling turnover nears S$1m
- **THEN** an alert is raised

#### Scenario: Voluntary registrants stay registered
- **GIVEN** the organisation registered for GST voluntarily
- **WHEN** its registration is recorded
- **THEN** it stays registered for ≥ 2 years

### Requirement: GST returns
The system SHALL run the GST return workflow with F7 corrections and a filing record, using the filing API where access is granted (FR-TAX-02/03).

Source: PRD-001 §14.2

#### Scenario: F5 due one month after the period end
- **WHEN** a GST period ends
- **THEN** its F5 is due 1 month after the period end
- **AND** extensions are generally not granted and late filing penalties apply (IRAS, Oct 2026)

#### Scenario: Deadline monitor
- **WHEN** an F5 is 14 days and again 3 days from its due date
- **THEN** the deadline monitor raises an alert

#### Scenario: Corrections and the final return
- **WHEN** an error in a filed return is corrected, or the final return is due
- **THEN** the correction is made with an F7
- **AND** the final return is an F8

#### Scenario: Return reconciles before filing
- **WHEN** a GST return is prepared
- **THEN** the GST control roll-forward reconciles (INV-07)
- **AND** the value boxes reconcile to the tax-coded ledger lines

#### Scenario: Filing API where access is granted
- **GIVEN** IRAS has granted filing API access
- **WHEN** a return is filed
- **THEN** it is filed through the filing API
- **AND** the filing record is kept

### Requirement: Tax invoices
Tax invoices SHALL carry the required particulars and SHALL round GST with `RND-GST-EXCL`, per line or per subtotal by an organisation-level setting applied consistently, with the total to the nearest cent.

Source: PRD-001 §14.2

#### Scenario: Issued within 30 days
- **WHEN** a taxable supply is made
- **THEN** its tax invoice is to be issued within 30 days

#### Scenario: Simplified invoice
- **WHEN** a tax invoice is up to S$1,000
- **THEN** a simplified invoice is allowed

#### Scenario: Consistent GST rounding
- **GIVEN** the organisation has chosen per-line or per-subtotal rounding
- **WHEN** GST is computed on its invoices
- **THEN** the chosen method is applied consistently
- **AND** the total is rounded to the nearest cent

#### Scenario: Foreign-currency invoices show SGD
- **WHEN** a foreign-currency tax invoice is issued
- **THEN** it shows the net, GST and gross amounts in SGD (FR-AR-05; IRAS, Jul 2026)

#### Scenario: Particulars and rounding tests
- **WHEN** the tests run
- **THEN** a template schema test checks the required particulars
- **AND** rounding property tests check the rounding

### Requirement: GST bad-debt relief
The system SHALL keep a bad-debt queue with eligibility dates (FR-AR-11) for GST bad-debt relief.

Source: PRD-001 §14.2

#### Scenario: Eligibility for relief
- **WHEN** a debt is assessed for bad-debt relief
- **THEN** it is eligible once it is written off and 12 months have passed (or the debtor is insolvent)
- **AND** the claim is made within 5 years of the supply (IRAS, Sep 2026)

#### Scenario: Eligibility rule tests
- **WHEN** the tests run
- **THEN** eligibility rule tests check these conditions

### Requirement: InvoiceNow (Peppol)
The system SHALL exchange invoice data through an Access Point connector with transmission status per document, inbound and outbound (FR-AR-12, FR-AP-05), and the connector MUST be live before Cognizhi registers voluntarily for GST.

Source: PRD-001 §14.2

#### Scenario: Phase-in dates
- **WHEN** the InvoiceNow obligation of a registrant is determined
- **THEN** it is mandatory for new voluntary GST registrants from 1 Apr 2026 (from 1 Nov 2025 for voluntary registrants within 6 months of incorporation); for new compulsory registrants and existing registrants ≤ S$200k from 1 Apr 2028; ≤ S$1m 2029; ≤ S$4m 2030; all others 2031

#### Scenario: Data covered
- **WHEN** invoice data is submitted
- **THEN** it covers the supplies and purchases reported in the return, simplified tax invoices, receipts and credit/debit notes included
- **AND** it covers them whether or not the counterparty is on the network, the business then submitting the data itself

#### Scenario: Exclusions and aggregation
- **WHEN** invoice data is selected for submission
- **THEN** it excludes, among others, reverse-charge transactions, import permits, purchases from non-registered suppliers and purchases with blocked input tax
- **AND** only POS, simplified-invoice and petty-cash data (staff claims, card purchases) may be aggregated (IRAS e-Tax Guide, 2nd edition, 9 Mar 2026)

#### Scenario: Submission deadline and route
- **WHEN** invoice data for a return period is submitted
- **THEN** it is submitted by the earlier of the filing date and the due date, through an InvoiceNow-Ready solution or an IMDA-accredited Access Point (IRAS, 8 Oct 2026)

#### Scenario: Transmission status per document
- **WHEN** a document is sent or received through the Access Point
- **THEN** its transmission status is recorded on the document

#### Scenario: Return items reconcile before filing
- **WHEN** a GST return is filed
- **THEN** every return item reconciles to an IRAS acknowledgement ID (proof of receipt) or a recorded exclusion before filing
- **AND** outages follow FR-TAX-11

#### Scenario: Live before voluntary GST registration
- **WHEN** Cognizhi registers voluntarily for GST
- **THEN** the Access Point connector is already live

### Requirement: Corporate income tax
The system SHALL support corporate income tax with a YA-keyed tax pack, tax-adjustment tags on accounts, a capital allowance register, a tax computation working paper and a form selector (FR-TAX-06).

Source: PRD-001 §14.2

#### Scenario: Rate and exemption schemes
- **WHEN** the tax for a YA is computed
- **THEN** the rate is 17%, with SUTE (first 3 YAs: 75% of first S$100k, 50% of next S$100k; conditions apply) or PTE (75% of first S$10k, 50% of next S$190k)

#### Scenario: YA2026 rebate
- **WHEN** the tax for YA2026 is computed
- **THEN** the rebate is 50% capped at S$40k, including a S$2k cash grant that needs a non-shareholder local employee

#### Scenario: Estimated chargeable income
- **WHEN** a FY ends
- **THEN** the ECI is due within 3 months of the FY end
- **AND** it is waived if revenue ≤ S$5m and the ECI is nil

#### Scenario: Form selection
- **WHEN** the form for a YA is selected
- **THEN** it is Form C-S (revenue ≤ S$5m), C-S Lite (≤ S$200k), or Form C otherwise

#### Scenario: Filing deadline and alerts
- **WHEN** the filing deadline for a YA is set
- **THEN** it is 30 Nov (15 Dec when filed via ASR+ software, YA2023–2027; IRAS, Aug–Sep 2026)
- **AND** deadline alerts are raised at 30 and 7 days

#### Scenario: Pinned computations and the current YA's pack
- **WHEN** CI runs
- **THEN** pinned tax computations per YA are checked
- **AND** CI fails if the current YA's pack is missing or unsourced

### Requirement: ACRA annual filings
The system SHALL support ACRA filings with the statutory calendar, filing-category and audit-exemption classifiers with configurable thresholds, FS templates per framework edition, and a BizFinx data pack, then XBRL (FR-TAX-07/08).

Source: PRD-001 §14.2

#### Scenario: Annual return and AGM deadlines
- **WHEN** a FY ends
- **THEN** the annual return is due within 7 months of the FY end
- **AND** the AGM is due within 6 months unless exempt

#### Scenario: Annual return confirms particulars
- **WHEN** an annual return is filed from 20 Oct 2026
- **THEN** it confirms officer and member particulars

#### Scenario: XBRL filing category
- **WHEN** the filing category is classified
- **THEN** solvent exempt private companies file a solvency declaration, simplified XBRL applies if revenue and assets are ≤ S$500k, and full XBRL otherwise
- **AND** XBRL Taxonomy 2026 applies from 25 Feb 2026, and XBRL is validated against the taxonomy (P3)

#### Scenario: Audit exemption under review
- **WHEN** audit exemption is classified
- **THEN** the small-company test (2 of 3: revenue ≤ S$10m, assets ≤ S$10m, ≤ 50 employees, two consecutive years), which is **under review**, uses configurable thresholds

#### Scenario: Framework edition and register of controllers
- **WHEN** financial statements or the register of controllers are prepared
- **THEN** SFRS for Small Entities 3rd edition applies from 1 Jan 2027 through the FS template of that edition
- **AND** the register of controllers is updated within 7 days (ACRA, 2025–2026)

#### Scenario: Classifier boundary tests
- **WHEN** the tests run
- **THEN** classifier boundary tests check the filing-category and audit-exemption thresholds

### Requirement: Share issues and directors' balances
The system SHALL keep director accounts with the origin of each movement, the founder pay choice, settlement routes, a register of share issues with effective dates and an exempt-private-company attribute (FR-EQ-01…14, §8.22).

Source: PRD-001 §14.2

#### Scenario: Prior approval of a share issue
- **WHEN** shares are issued
- **THEN** they need the company's prior approval in general meeting (s161; a private company may pass it as a written resolution, s184A), lodged with ACRA within 14 days (s161(5), s186)
- **AND** the approval lapses at the next AGM, and shares issued without it are void
- **AND** the lodgement and lapse monitor (C.2 #22) checks this

#### Scenario: Allotment date in a private company
- **WHEN** a private company allots shares
- **THEN** the allotment takes effect on the date the return of allotment is filed, once ACRA has updated the electronic register of members, and backdating is generally not allowed (the 14-day return deadline in s63A is for public companies)

#### Scenario: Shares not paid in cash and no par value
- **WHEN** shares are not paid in cash
- **THEN** the written contract, or a statement of particulars, is lodged with the return (s63B)
- **AND** the shares have no par value (s62A)

#### Scenario: Loans to directors
- **WHEN** a director's account goes into debit
- **THEN** loans to directors are banned except in exempt private companies and for approved advances for company expenses (s162)
- **AND** the director-debit check (C.2 #21) cites s162 when the company is not exempt

#### Scenario: Share duty
- **WHEN** shares change hands
- **THEN** share duty (0.2%) applies to transfer documents, and IRAS's pages do not charge allotments of new shares

#### Scenario: Founder pay and CPF
- **WHEN** founder pay is recorded
- **THEN** salary is taxed in the year it is due, paid or not, and director's fees when the director becomes entitled
- **AND** CPF is due at the month end on wages due, with no CPF on fees voted at a general meeting or on shares in kind
- **AND** the pay-choice gate (C.2 #24) applies

#### Scenario: Share capital only from the register
- **WHEN** share capital is posted
- **THEN** it comes only from the register of share issues (INV-13, C.2 #25)

#### Scenario: Re-check after the 2026 amendments
- **WHEN** the Companies Act amendments commencing 20 Oct 2026 take effect
- **THEN** these rules are re-checked against the Act (SSO, ACRA 30 Apr 2026, IRAS and CPF Board 2024–2026)

### Requirement: Company secretary and corporate changes
The system SHALL track company secretary and corporate-change obligations through the ACRA pack (FR-CS-02), the corporate profile (FR-CS-01) and the AI company secretary (§9.12).

Source: PRD-001 §14.2

#### Scenario: Company secretary
- **WHEN** the company's secretary is checked
- **THEN** a company secretary is appointed within 6 months (ACRA guidance), is a natural person whose principal or only residence is in Singapore, and is not the sole director
- **AND** the office is never vacant more than 6 months (s171)

#### Scenario: AGM or its dispensation
- **WHEN** a FY ends
- **THEN** the AGM is held within 6 months after the FY end, or a private company dispenses with it by sending the financial statements to members within 5 months, by all members' resolution, or when dormant and exempt (s175, s175A)

#### Scenario: Annual return
- **WHEN** the annual return is due
- **THEN** it is filed within 7 months after the FY end, with a fee of S$60 and a late penalty of S$300 (up to 3 months) or S$600
- **AND** from 20 Oct 2026 it confirms ACRA's records of officers, members and the registered office and asks about a CEO (s197)

#### Scenario: Changes of officers and registered office
- **WHEN** an officer, an officer's particulars or the registered office changes
- **THEN** the change is lodged within 14 days (s173A, s143)

#### Scenario: Registrable controllers and nominees
- **WHEN** a registrable controller or nominee changes
- **THEN** the company's own register is updated within 7 days and the central register within 2 business days
- **AND** for nominee directors the steps are 30 days, then 7 days, then 2 business days
- **AND** for nominee shareholders the company's register is updated within 7 days, then the central register within 2 business days (since 16 Jun 2025)

#### Scenario: Yearly notice to registrable controllers
- **WHEN** a year passes
- **THEN** a notice has been sent to each registrable controller at least once, with 30 days to reply

#### Scenario: Minutes
- **WHEN** a meeting is held
- **THEN** its minutes are kept within one month (s188)

#### Scenario: No ACRA filing API
- **WHEN** an ACRA filing is made
- **THEN** it is made outside Fyniq, because ACRA offers read-only APIs and no filing API (SSO and ACRA, Jan–Oct 2026)

#### Scenario: Checks and scenarios per rule
- **WHEN** these rules are checked
- **THEN** checks C.2 #15 and #32–35 apply
- **AND** QA-AI-07 has scenarios for each rule

### Requirement: CPF contributions
The system SHALL compute CPF from an effective-dated CPF pack keyed by residency, PR year, age band and wage band, with year-to-date AW ceiling tracking and a CPF submission file (FR-PAY-02/09).

Source: PRD-001 §14.2

#### Scenario: 2026 rates by age band
- **WHEN** CPF is computed for 2026
- **THEN** the employer/employee rates are ≤55: 17/20; 55–60: 16/18; 60–65: 12.5/12.5; 65–70: 9/7.5; >70: 7.5/5

#### Scenario: 2027 rates for senior workers
- **WHEN** wages earned from 1 Jan 2027 are paid to an employee above 55
- **THEN** above 55 to 60 pays 16.5/19 and above 60 to 65 pays 13/13, the other bands unchanged

#### Scenario: Age band change
- **WHEN** an employee has a birthday that moves them into a new age band
- **THEN** the new band applies from the first day of the month after the birthday

#### Scenario: Wage ceilings
- **WHEN** CPF is computed
- **THEN** the OW ceiling is S$8,000 a month and the annual salary ceiling is S$102,000 (AW ceiling = S$102,000 − OW subject to CPF), tracked year to date

#### Scenario: Graduated rates and PR tables
- **WHEN** wages are between S$50 and S$750, or the employee is a first- or second-year PR
- **THEN** the graduated rates or the first/second-year PR tables apply

#### Scenario: Rounding
- **WHEN** a CPF contribution is computed
- **THEN** the total is rounded to the dollar and the employee share is rounded down

#### Scenario: Due date and payment
- **WHEN** CPF for a month is due
- **THEN** it is due at the month end and paid by the 14th of the next month to avoid interest, via EZPay or One-Stop Payroll, there being no public API (CPF Board, Oct 2026)
- **AND** a CPF-not-paid alert is raised on the 10th

#### Scenario: Golden payslips and yearly tables
- **WHEN** the tests run
- **THEN** hand-pinned golden payslips cover every band, the graduated range, the AW ceiling and birthday transitions
- **AND** January CI fails if the year's tables are missing

### Requirement: SDL, SHG and levies
The system SHALL compute SDL and SHG from their tables and SHALL record the levy as an expense (FR-PAY-03/04/05), with pinned tests.

Source: PRD-001 §14.2

#### Scenario: Skills Development Levy
- **WHEN** SDL is computed
- **THEN** it is 0.25% of remuneration, min S$2, max S$11.25, for all employees including foreigners

#### Scenario: Self-help group funds
- **WHEN** SHG contributions are computed
- **THEN** the SHG funds (CDAC, ECF, MBMF, SINDA) use tiered amounts remitted with CPF

#### Scenario: S Pass levy
- **WHEN** the S Pass levy is recorded from 1 Sep 2025
- **THEN** it is S$650 a month (CPF Board, MOM), recorded as an expense

#### Scenario: Pinned tests
- **WHEN** the tests run
- **THEN** pinned tests check SDL, SHG and levy amounts

### Requirement: MOM employment records
The system SHALL produce payslips and key employment terms from templates that meet MOM's rules, and SHALL deliver payslips through the MFA-protected portal or an expiring link (FR-PAY-08).

Source: PRD-001 §14.2

#### Scenario: Itemised payslips
- **WHEN** salary is paid
- **THEN** an itemised payslip with 12 items is given with the salary payment or within 3 working days of it
- **AND** on termination or dismissal it is given with the outstanding salary

#### Scenario: Key employment terms
- **WHEN** an employee is hired
- **THEN** the key employment terms (17 items, plus the optional place of work) are given within 14 days
- **AND** this covers employees under the Employment Act employed since 1 Apr 2016 on a contract of 2 weeks or longer, part-timers included

#### Scenario: Employment records kept
- **WHEN** employment records are retained
- **THEN** they are kept 2 years (and 1 year after an employee leaves) (MOM)

#### Scenario: Payslip schema test
- **WHEN** the tests run
- **THEN** a payslip schema test checks the 12 items

### Requirement: IR8A, AIS and IR21
The system SHALL produce the AIS file and run an IR21 workflow with a clearance hold on all monies due, on every payment path (FR-PAY-10).

Source: PRD-001 §14.2

#### Scenario: AIS deadline and obligation
- **WHEN** the AIS (IR8A, Appendix 8A/8B) is prepared
- **THEN** it is due 1 March and mandatory with 5 or more employees (directors included) or on notice; once in, always in
- **AND** an AIS deadline monitor tracks it

#### Scenario: IR21 for non-citizen employees
- **WHEN** a non-citizen employee (PRs included) is ceasing employment
- **THEN** IR21 is filed at least 1 month before cessation and all monies are withheld (exemptions apply; IRAS, 2026)

#### Scenario: Clearance hold on every payment path
- **GIVEN** an IR21 clearance is pending
- **WHEN** any payment path would pay monies due to that employee
- **THEN** the clearance hold stops it
- **AND** an IR21 gate test proves this

### Requirement: Electronic signatures on approvals
Approvals SHALL store the user, authentication method (MFA claim), time, statement and document hash, and any edit SHALL void the approval (FR-APR-03/05), so they meet the Electronic Transactions Act.

Source: PRD-001 §14.2

#### Scenario: Electronic Transactions Act conditions
- **WHEN** an approval is given electronically
- **THEN** it identifies the signer and intent with appropriate reliability
- **AND** the record is accessible and accurate, with metadata

#### Scenario: Edit after approval voids it
- **GIVEN** a record has been approved
- **WHEN** the record is edited
- **THEN** the approval is void and the status resets, as a secure signature is invalidated if the record changes
- **AND** an edit-after-approve test proves this

#### Scenario: Database requires hash and MFA claim
- **WHEN** an approval without a document hash or an MFA claim is written
- **THEN** a database constraint rejects it

### Requirement: MAS guidelines as reference only
MAS TRM Guidelines, the Cyber Hygiene Notice and the Outsourcing Guidelines SHALL be reference only and not in scope, and SHALL be revisited if the company becomes a financial institution or serves FIs.

Source: PRD-001 §14.2

#### Scenario: Not in scope
- **GIVEN** the company is not a financial institution and does not serve FIs
- **WHEN** the scope of compliance is set
- **THEN** the MAS guidelines are not in scope

#### Scenario: Revisit when serving FIs
- **WHEN** the company becomes a financial institution or starts serving FIs
- **THEN** the MAS guidelines are revisited
