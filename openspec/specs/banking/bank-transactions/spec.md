# banking/bank-transactions Specification

## Purpose
How bank, card, wallet, payment-gateway and cash lines get into Fyniq from files, emailed reports, APIs and feeds, and become categorised, matched and reconciled records; and how approved payments go out in batches. Derived from PRD-001 §8.4 and PRD-001 §J.3.

## Requirements

### Requirement: FR-BNK-01 Money accounts
The system SHALL keep bank, card, wallet, payment-gateway and cash accounts in any currency.

Source: PRD-001 §8.4 · Priority M · Phase P1

#### Scenario: Kinds of money account in any currency
- **WHEN** a money account is created
- **THEN** it can be a bank, card, wallet, payment-gateway or cash account
- **AND** it can be in any currency

### Requirement: FR-BNK-02 Statement import
The system SHALL import statements for several accounts at once, from CSV/XLSX, OFX/QFX, CAMT.053 and MT940 files and, in P2, from PDF statements.

Source: PRD-001 §8.4 · Priority M · Phase P1 (CSV/XLSX/OFX) / P2

#### Scenario: Several accounts at once
- **WHEN** statements for several accounts are imported together
- **THEN** each statement's lines go to its own account

#### Scenario: CSV or XLSX with a saved mapping per bank
- **WHEN** a CSV or XLSX statement is imported
- **THEN** it uses the column mapping saved for that bank, with either a signed amount column or in and out columns

#### Scenario: Structured statement formats
- **WHEN** an OFX/QFX, CAMT.053 or MT940 statement is imported
- **THEN** its lines are read from that format

#### Scenario: PDF statements (P2)
- **WHEN** a PDF statement is imported
- **THEN** document AI extracts its lines

### Requirement: FR-BNK-03 Bank feeds
The system SHALL take bank feeds through bank APIs, using only the connector kinds of FR-BNK-15, for the Singapore banks Cognizhi uses (DBS, OCBC, UOB, and neo-banks such as Aspire, Airwallex, Wise; OCBC's and Aspire's account APIs to confirm with each bank), as detailed in FR-BNK-15…18.

Source: PRD-001 §8.4 · Priority S · Phase P2

#### Scenario: Feed from a bank API
- **WHEN** a feed is set up for one of these banks
- **THEN** it runs through one of the connector kinds of FR-BNK-15
- **AND** "or a licensed aggregator" is superseded (rev 0.11)

#### Scenario: No credential-sharing aggregators
- **WHEN** a feed source is chosen
- **THEN** a credential-sharing aggregator is never used

### Requirement: FR-BNK-04 Duplicate detection
The system SHALL detect duplicate bank lines across imports and feeds, including a transfer already recorded from the other account (workbook behaviour), using date, amount, reference and a fingerprint.

Source: PRD-001 §8.4 · Priority M · Phase P1

#### Scenario: Fingerprint fields
- **WHEN** a line is imported or fed
- **THEN** its fingerprint is the account, booking date, amount, currency, the bank's transaction reference where given, the counterparty, and the line's running balance or position in its statement

#### Scenario: Transfer already recorded from the other account
- **GIVEN** a transfer already recorded from the other account
- **WHEN** the matching line arrives on this account
- **THEN** it is detected as the same transfer

#### Scenario: Identical lines in one statement are both kept
- **GIVEN** two identical lines in the same statement
- **WHEN** the statement is imported
- **THEN** both lines are kept

#### Scenario: G4-042 Same-day look-alike receipts, re-import and overlapping feed
- **GIVEN** two legitimate S$500.00 receipts on the same booking date with the same reference in one statement with running balances
- **WHEN** the same file is imported again, and then a feed covering the same days arrives
- **THEN** both lines are kept
- **AND** the re-import and the overlapping feed add nothing
- **AND** where no bank reference or running balance tells lines apart, the look-alike is marked possible duplicate (C.2 #51), never dropped silently

### Requirement: FR-BNK-05 Bank rules
The system SHALL apply bank rules whose conditions (description contains or regex, amount range, account, counterparty) set the category, contact, GST code, dimensions and reference of a line.

Source: PRD-001 §8.4 · Priority M · Phase P1

#### Scenario: Rule sets a line's coding
- **WHEN** a line meets a rule's conditions
- **THEN** the rule sets the line's category, contact, GST code, dimensions and reference

#### Scenario: Rules are ordered
- **GIVEN** several rules
- **WHEN** they are applied to a line
- **THEN** they are applied in their set order

#### Scenario: Rule tested against history
- **WHEN** a user tests a rule
- **THEN** it is run against past lines and reports how many it would have matched ("this rule would have matched 23 lines")

### Requirement: FR-BNK-06 Categorise screen
The system SHALL provide a Categorise screen with the queue of unreconciled lines and, for each, suggested matches, rule hits and AI suggestions.

Source: PRD-001 §8.4 · Priority M · Phase P1

#### Scenario: Suggested matches
- **WHEN** a line is shown in the queue
- **THEN** open invoices and bills are suggested as matches by amount, reference and contact

#### Scenario: Actions on a line
- **WHEN** a user works a line
- **THEN** they can accept, edit or split it, or record it as a transfer

#### Scenario: Bulk accept
- **WHEN** a user selects several lines
- **THEN** they can accept them in bulk

### Requirement: FR-BNK-07 Matching payments to documents
The system SHALL match a payment to invoices or bills by reference, then by amount and contact, allowing one-to-many, many-to-one, partial payments and overpayments.

Source: PRD-001 §8.4 · Priority M · Phase P1

#### Scenario: Reference matching order
- **WHEN** a payment is matched by reference
- **THEN** an exact match is tried first, then a normalised match (Unicode NFKC, trimmed, non-breaking spaces removed, case-insensitive), then a match on amount and contact

#### Scenario: One payment to many documents and many to one
- **WHEN** a payment settles several documents, or several payments settle one document
- **THEN** each is matched

#### Scenario: Partial payment
- **WHEN** a payment is less than the document's open balance
- **THEN** it is matched as a partial payment

#### Scenario: Overpayment becomes a credit
- **WHEN** a payment is more than the documents it settles
- **THEN** the excess becomes a credit

### Requirement: FR-BNK-08 Settlement differences
The system SHALL compute realised FX on a settlement first and exactly, then clear a residual within tolerance to FX and rounding (7030) in the settlement month, as in the workbook.

Source: PRD-001 §8.4 · Priority M · Phase P1

#### Scenario: Realised FX first and exact
- **WHEN** a foreign-currency document is settled
- **THEN** realised FX = the document-currency amount settled × (settlement rate − the document's stored rate), with no tolerance (ENG-41)
- **AND** what remains is the residual

#### Scenario: Residual within tolerance
- **WHEN** the residual is within tolerance (Appendix B.1)
- **THEN** it clears to 7030 in the month of the last payment

#### Scenario: Larger residual needs a reason-coded adjustment
- **WHEN** the residual is above tolerance
- **THEN** the document stays open (part-paid) until a person records a reason-coded adjustment, each a write-off (FR-AR-17): agreed discount (credit note, GST adjusted), bank or provider charges (fees expense), bad debt, or an unexplained cross-currency difference with the bank's advice as evidence (7030)

#### Scenario: Overpayment is never income
- **WHEN** an overpayment is above S$1.00
- **THEN** it is never taken to income and stays a customer credit (FR-AR-18)

#### Scenario: Monthly cap on tolerance write-offs
- **WHEN** tolerance write-offs in a month exceed the monthly cap
- **THEN** check C.2 #49 is raised

#### Scenario: G4-038 USD invoice paid short and SGD bill forced paid
- **GIVEN** a USD invoice of USD 10,000 at 1.35 (S$13,500.00) paid USD 9,710 into the USD account at 1.32, and an SGD bill of S$1,000.00 marked fully paid after S$880.00
- **WHEN** both settlements are recorded
- **THEN** realised FX is posted exactly: 9,710 × (1.32 − 1.35) = −S$291.30 to 7030
- **AND** the USD 290 residual (S$391.50 at the document rate) is above the recommended S$50 cap, so the invoice stays part-paid until a reason-coded, approved write-off
- **AND** the bill's S$120.00 stays open likewise
- **AND** a migrated workbook forced-paid bill keeps G.3's June 7030 figure

#### Scenario: Settlement differences as built (rev 0.121)
- **WHEN** a payment or receipt recorded by hand settles a bill or an invoice in full with a little less
- **THEN** the difference's carrying amount clears to 7030 on a line of its own when within the tolerances in force (S$1.00; for another currency the greater of S$1.00 and 3%; at most S$50.00 a document and S$200.00 a month), and is refused above them (RECEIPT-11, BILLPAY-12)
- **AND** the allocation records it, a void negates it, and the close warns when the month's differences exceed a cap lowered since (C.2 #49)

#### Scenario: What the 3% and the month's cap count (rev 0.155)
- **GIVEN** a part recorded in full on a document in another currency
- **WHEN** its difference is compared with the tolerances
- **THEN** the 3% is of what the document owes on its control account at its stored rate, after any tax withheld or deposit deducted [accountant to confirm]
- **AND** the month's cap adds the absolute value of every difference recorded in full that month, receipts and payments together, so a gain never makes room for a loss

### Requirement: FR-BNK-09 Transfers between money accounts
The system SHALL record a transfer between money accounts once, matched on both sides, and SHALL track transfers in transit.

Source: PRD-001 §8.4 · Priority M · Phase P1

#### Scenario: Recorded once, matched on both sides
- **WHEN** a transfer appears on the paying and the receiving account
- **THEN** it is recorded once and matched to the line on each side

#### Scenario: Transfer in transit
- **GIVEN** a transfer has left one account
- **WHEN** it has not yet arrived in the other
- **THEN** it is shown as in transit

### Requirement: FR-BNK-10 Bank reconciliation
The system SHALL reconcile each account per period, showing the statement balance (from the feed or an uploaded statement), the reconciled book balance, the unreconciled items and the difference.

Source: PRD-001 §8.4 · Priority M · Phase P1

#### Scenario: Sign-off by a different person
- **WHEN** a reconciliation is signed off
- **THEN** the person signing off is not the preparer (FM-5, SOD-5)

#### Scenario: Reports locked with the period
- **WHEN** a period is locked
- **THEN** the reconciliation reports for that period are locked with it

### Requirement: FR-BNK-11 Suspense handling
The system SHALL let unmatched imported lines post to suspense (2999) with ⚠ until they are classified.

Source: PRD-001 §8.4 · Priority M · Phase P1

#### Scenario: Unmatched line posted to suspense
- **WHEN** an unmatched imported line is posted to suspense
- **THEN** it posts to 2999 and shows ⚠ until it is classified

#### Scenario: Non-zero suspense is flagged
- **WHEN** the suspense balance is not zero
- **THEN** Checks flags it

### Requirement: FR-BNK-12 Payment batches
The system SHALL build payment batches from approved bills, claims and pay runs, approved under dual control, and paid by a bank bulk-payment file or, in P2, through a bank API.

Source: PRD-001 §8.4 · Priority S · Phase P1 (file) / P2 (API)

#### Scenario: Batch built from approved items
- **WHEN** a payment batch is built
- **THEN** it takes approved bills, claims and pay runs

#### Scenario: Dual-control approval
- **WHEN** a payment batch is approved
- **THEN** the approval needs dual control

#### Scenario: Bulk-payment file or bank API
- **WHEN** an approved batch is paid
- **THEN** a bank bulk-payment file is exported in the formats of the banks used (DBS UFF; UOB UFF or ISO 20022; OCBC's from the relationship manager), or the payment is initiated through the bank API (P2)

#### Scenario: Marked paid from the feed
- **WHEN** the feed shows the debit for a batch
- **THEN** the batch is marked paid

#### Scenario: The DBS file first (rev 0.154)
- **WHEN** an approved batch is released
- **THEN** its file is DBS IDEAL's Universal File Format (UFF) for FAST and GIRO, written from DBS's own specification, each field [verify] with a test upload to IDEAL

#### Scenario: SGD to Singapore accounts (rev 0.154)
- **WHEN** a payment is added to a batch
- **THEN** it pays SGD from an SGD money account to a Singapore bank account by FAST or GIRO
- **AND** overseas and foreign-currency payments stay recorded by hand

#### Scenario: Bills and expense claims (rev 0.154)
- **WHEN** a batch is built
- **THEN** it takes approved bills, with their planned dates (FR-AP-13), and approved expense claims (FR-AP-07)
- **AND** salaries join with the payroll link

#### Scenario: Posted on release, matched later (rev 0.154)
- **WHEN** a batch is released
- **THEN** each payment posts as recording one by hand does (Dr 2010 or 2150 · Cr the money account) on the batch's payment date
- **AND** bank import matches it, and a payment the bank rejects is voided

#### Scenario: Who releases (rev 0.154)
- **GIVEN** Founder mode
- **WHEN** one founder prepared a batch and another approved it (FM-1)
- **THEN** either of them releases it after step-up, holding the Payment releaser role, and never one person alone
- **AND** in Standard mode the releaser is a third person (SOD-2)

#### Scenario: Remittance advice on the match (rev 0.154)
- **WHEN** a batch's payment to a supplier is matched to its bank statement line
- **THEN** remittance advice is emailed to the supplier's remittance address

### Requirement: FR-BNK-13 Daily cash position
The system SHALL show the daily cash position across accounts and currencies at the latest rates.

Source: PRD-001 §8.4 · Priority S · Phase P1

#### Scenario: Cash position across accounts and currencies
- **WHEN** a user views the cash position for a day
- **THEN** it totals all money accounts and currencies, converted at the latest rates

### Requirement: FR-BNK-14 Auto-reconcile, deterministically
The system SHALL auto-reconcile, by the system principal (FR-AUT-02) and without a click, lines that match exactly one open document, transfers matched on both feeds, and lines hit by a bank rule that an approver marked auto-apply.

Source: PRD-001 §8.4 · Priority M · Phase P1 (imports) / P2 (feeds)

#### Scenario: Line matching exactly one open document
- **WHEN** a line has the same amount and currency as exactly one open document, a unique reference and the expected contact
- **THEN** it is auto-reconciled to that document

#### Scenario: Transfer matched on both feeds
- **WHEN** a transfer is matched on both feeds
- **THEN** it is auto-reconciled

#### Scenario: Marking a rule auto-apply is an approval
- **WHEN** an approver marks a bank rule auto-apply
- **THEN** it is an approval object (FR-APR-01) that shows the back-test and the rule's hits since it was created

#### Scenario: Auto-apply of a rule from an AI proposal
- **GIVEN** a bank rule that came from an AI proposal
- **WHEN** its auto-apply is approved in Standard mode
- **THEN** the person who requested or approved that proposal cannot approve it

#### Scenario: Repeated coding is offered as a rule
- **WHEN** the organisation has coded the same pattern the same way several times (default 5; Xero calls this "memory")
- **THEN** it is offered as a rule to promote and never applied by itself

#### Scenario: Limits
- **WHEN** auto-reconciliation runs
- **THEN** it stays within an amount cap per line and per rule per day (settings with recommended defaults, FR-ORG-08)

#### Scenario: Auto-reconciled list and undo
- **WHEN** a line is auto-reconciled
- **THEN** it appears in an Auto-reconciled list with the rule or match that did it
- **AND** it can be undone while the period is open
- **AND** a sample enters the weekly review (FM-3)

#### Scenario: The recommended limits (rev 0.142)
- **WHEN** an organisation has not changed them
- **THEN** a line above S$500 is not auto-reconciled, and one rule auto-reconciles at most S$2,000 of lines a day (the owner's decision of 25 Oct 2026)

#### Scenario: The weekly sample (rev 0.142)
- **WHEN** a week ends
- **THEN** its 5 largest auto-reconciliations and 5 others at random enter the weekly review, once each

#### Scenario: Lines that arrive by feed wait for Reconcile now (rev 0.143)
- **GIVEN** lines imported from an emailed bank report or a connector sync in P1
- **WHEN** they are imported
- **THEN** they wait in the queue, and nothing posts by itself
- **AND** a person's Reconcile now auto-reconciles the waiting lines as the system principal, within the same limits (the owner's decision of 26 Oct 2026)

#### Scenario: Ambiguous lines and AI suggestions stay in the queue
- **WHEN** a line is ambiguous or has only an AI suggestion
- **THEN** it stays in the queue
- **AND** AI suggestions are never auto-applied and stay proposals (AI-P4)

#### Scenario: G4-059 AI-proposed rule marked auto-apply by its approver
- **WHEN** a user approves an AI-proposed bank rule and then tries to mark it auto-apply
- **THEN** in Standard mode it is refused for that user
- **AND** auto-apply is a separate approval showing the back-test
- **AND** no line is auto-reconciled before that approval

### Requirement: FR-BNK-15 Bank data connectors
The system SHALL bring bank data in through connectors (FR-INT-25) behind one model: account, balance, and transaction with bank reference, booking and value dates, counterparty, amount, currency and running balance.

Source: PRD-001 §8.4 · Priority M · Phase P1 (files, email, Wise and Airwallex APIs) / P2 (bank APIs, host-to-host)

#### Scenario: Five kinds of source
- **WHEN** a bank data source is connected
- **THEN** it is one of: file import (MT940 SWIFT and SAP, CSV, camt.053, OFX; FR-BNK-02); bank reports by email (scheduled daily reports and incoming-funds alerts sent to the organisation's bank-ingest address, FR-INT-22); bank APIs (DBS RAPID, UOB API Services, OCBC credit notifications); account APIs of EMIs and neo-banks (Wise, Airwallex; Aspire where offered); host-to-host files (DBS IDEAL Connect over SFTP with MT940 and MT942)

#### Scenario: No aggregators that share credentials or scrape
- **WHEN** a bank data source is chosen
- **THEN** no credential-sharing or screen-scraping aggregator is used

#### Scenario: Bank connectors are read-only
- **WHEN** a payment is made
- **THEN** it still goes out as an approved file (FR-BNK-12), because bank connectors are read-only, until an API payment connector is added under the same approvals ([source notes](research/bank-payments-cloud-2026-10-12.md))

### Requirement: FR-BNK-16 Sync schedule and completeness
The system SHALL pull bank data on the Scheduler tick (ADR-T7), accept pushed notifications on signed webhooks, check every statement for continuity, and show for each account its last sync and the date it is complete to.

Source: PRD-001 §8.4 · Priority M · Phase P1

#### Scenario: Pulls and pushes
- **WHEN** bank data is synced
- **THEN** pulls run on the Scheduler tick (ADR-T7)
- **AND** pushed notifications are accepted on signed webhooks

#### Scenario: Continuity check
- **WHEN** a statement arrives
- **THEN** each opening balance must equal the previous closing balance (MT940 `:60F:` and `:62F:`)
- **AND** gaps are flagged

#### Scenario: Duplicates removed only on proof
- **WHEN** a line overlaps one already held
- **THEN** it is removed (FR-BNK-04) only when the bank's own transaction reference matches or the statement's running balance proves the overlap

#### Scenario: Look-alike kept as possible duplicate
- **WHEN** a line looks like one already held without that proof
- **THEN** it is kept and marked possible duplicate for a person to resolve (C.2 #51)

#### Scenario: Stale account
- **WHEN** an account's data is stale
- **THEN** the account shows ⚠ (check C.2 #43)

#### Scenario: Alerts are pending signals
- **WHEN** an incoming-funds alert arrives
- **THEN** it is a pending signal that speeds up matching
- **AND** only statement data becomes reconciled bank lines

#### Scenario: Likely paid
- **GIVEN** a pending signal that matches an open invoice
- **WHEN** the invoice and Home are shown before the statement line arrives
- **THEN** they show "likely paid" (UC-BNK-02)
- **AND** the invoice's status and the ledger do not change

#### Scenario: An alert no line closes expires, or a person dismisses it (rev 0.145)
- **GIVEN** a pending signal whose invoice no statement line has been matched to
- **WHEN** `bank.signal_expiry_days` (7 by default, under dual control; the owner's decision of 26 Oct 2026) have passed, or a person who reconciles bank lines dismisses it with a reason
- **THEN** the signal expires, or is dismissed with the reason in the audit trail, and the invoice stops showing "likely paid"
- **AND** nothing is posted

#### Scenario: The likely invoice is the only one the alert can pay (rev 0.145)
- **WHEN** an alert at the bank-ingest address says money of an amount and currency arrived from a payer
- **THEN** its likely invoice is the one open invoice of that amount and currency whose customer the payer names, not already pointed to by another pending signal
- **AND** with none, or several, the signal waits without an invoice

### Requirement: FR-BNK-17 DBS, in two phases
The system SHALL connect DBS in two phases: Phase 1 (P1, no extra bank fee) from DBS IDEAL's emailed reports and alerts and file exports; Phase 2 (P2) through DBS RAPID or IDEAL Connect over SFTP.

Source: PRD-001 §8.4 · Priority M · Phase P1 / P2

#### Scenario: Phase 1 sources
- **WHEN** DBS is connected in Phase 1
- **THEN** DBS IDEAL's free scheduled daily account report and real-time incoming-funds alerts are emailed to the bank-ingest address
- **AND** MT940 (SWIFT) or CSV exports are uploaded or emailed
- **AND** the monthly e-statement ties out the closing balance

#### Scenario: Phase 2 sources
- **WHEN** DBS is connected in Phase 2
- **THEN** it uses DBS RAPID (balance enquiry, account-data enquiry, inward credit notification), applied for through the relationship manager in about 4–6 weeks with fees quoted by DBS, or IDEAL Connect over SFTP (MT940 daily, MT942 intraday)

#### Scenario: Phase 2 prerequisites
- **WHEN** Phase 2 is set up
- **THEN** it needs PGP keys in Secret Manager and KMS, a static egress IP (Cloud NAT) for the bank's allowlist, and an HTTPS endpoint for notifications (RAPID is REST/JSON with PGP; whether inward credit confirmations are pushed or polled is confirmed with DBS)

### Requirement: FR-BNK-18 Wise and Airwallex
The system SHALL connect Wise and Airwallex with read-only API keys (scoped where offered), reading balance statements and transactions and receiving signed balance webhooks.

Source: PRD-001 §8.4 · Priority S · Phase P1

#### Scenario: Read-only keys and statements
- **WHEN** a Wise or Airwallex account is connected
- **THEN** it uses a read-only API key, scoped where offered
- **AND** balance statements and transactions are read (Wise offers camt.053 and MT940 statements for Singapore profiles)

#### Scenario: Signed balance webhooks
- **WHEN** Wise or Airwallex sends a balance update
- **THEN** it arrives on a signed balance webhook

#### Scenario: Sandboxes in staging
- **WHEN** the connectors run in staging
- **THEN** they use the providers' sandboxes

### Requirement: Daily DBS sync without a bank API
DBS's scheduled daily account report SHALL be received at the bank-ingest address, verified, imported with continuity and duplicate checks, and auto-reconciled, with a month-end tie-out signed off by a different person (UC-BNK-01).

Source: PRD-001 §J.3

#### Scenario: Report received and verified
- **WHEN** DBS IDEAL emails the scheduled daily account report at 07:00 to the bank-ingest address
- **THEN** the inbound connector checks SPF, DKIM and DMARC and that the sender is DBS, and scans the attachment (FR-INT-22)

#### Scenario: Import, continuity and duplicates
- **WHEN** the report is accepted
- **THEN** the importer parses the transactions, checks that the opening balance equals the previous closing balance, and removes duplicates (FR-BNK-16)

#### Scenario: Auto-reconcile and the Categorise queue
- **WHEN** the transactions are imported
- **THEN** auto-reconcile clears exact matches and approved rules (FR-BNK-14)
- **AND** the rest wait in Categorise with suggestions

#### Scenario: Month-end tie-out
- **WHEN** at month end the bookkeeper uploads the MT940 export or the e-statement
- **THEN** the closing balances tie out
- **AND** a different person signs off the reconciliation (FR-BNK-10, FM-5)

#### Scenario: Missing day or broken balance chain
- **WHEN** a day is missing or the balance chain is broken
- **THEN** ⚠ is shown with the gap

#### Scenario: Unexpected sender quarantined
- **WHEN** a report arrives from an unexpected sender
- **THEN** it is quarantined

### Requirement: Incoming-funds alert speeds up matching
An incoming-funds alert SHALL be recorded as a pending signal that points to the likely invoice, posts nothing, and leaves the reconciliation to the statement line (UC-BNK-02).

Source: PRD-001 §J.3

#### Scenario: Alert shows "likely paid"
- **WHEN** a DBS alert says S$10,900 arrived from "ACME ROBOTICS"
- **THEN** the platform records a pending signal, finds invoice INV-0138 for that amount and customer, and shows "likely paid" on the invoice and on Home
- **AND** nothing is posted from the alert

#### Scenario: Statement line reconciles
- **WHEN** next morning's report brings the statement line
- **THEN** auto-reconcile matches it to INV-0138 and posts Dr Bank · Cr 1100 AR

### Requirement: Moving DBS to RAPID
Moving DBS from emailed reports to RAPID SHALL follow a written enquiry, signed bank agreements kept as evidence, and a connector installed with its keys, egress IP and notification endpoint and tested before it is enabled (UC-BNK-03).

Source: PRD-001 §J.3

#### Scenario: Enquiry and agreements
- **WHEN** the Owner moves DBS to RAPID
- **THEN** the Owner sends the relationship manager the written enquiry: the Singapore RAPID API list, push or poll, fees, test access, IP and PGP requirements
- **AND** the bank agreements are signed with a board resolution, attached as evidence (FR-DOC-05)

#### Scenario: Connector installed, tested, then enabled
- **WHEN** the Super admin installs the DBS RAPID connector
- **THEN** the PGP keys go in Secret Manager and KMS, the static egress IP is given to DBS and the notification endpoint is registered
- **AND** the connector is tested against DBS's test environment before it is enabled

#### Scenario: Sync switches to the API
- **WHEN** the RAPID connector is enabled
- **THEN** sync switches from emailed reports to API pulls and pushes
- **AND** the e-statement tie-out stays

#### Scenario: P1 builds the switch, not RAPID (rev 0.140)
- **WHEN** `p1-bank-import-reconcile` is built
- **THEN** a money account can move its data source from emailed reports to an API connector without a gap, keeping the e-statement tie-out and the bank agreements as evidence
- **AND** the RAPID connector itself is P2 (the owner's decision of 25 Oct 2026)

### Requirement: Wise or Airwallex account syncs itself
A Wise or Airwallex account SHALL sync its balances and transactions through a connector installed with a read-only API key (UC-BNK-04).

Source: PRD-001 §J.3

#### Scenario: Connector with a read-only key
- **WHEN** the Owner creates a read-only API key in Wise or Airwallex and installs the connector
- **THEN** balances and transactions sync on the tick

#### Scenario: Balance webhook prompts an early sync
- **WHEN** a balance webhook arrives
- **THEN** it prompts an early sync
