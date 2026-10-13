# documents/attachments-and-evidence Specification

## Purpose
How files get into Fyniq and stay trustworthy: attachments on any record, safe-file rules, two-phase upload, retention-locked storage, issued documents kept with a hash, the evidence panel, the evidence rules and their recommended defaults, evidence integrity, personal data in evidence, finding and exporting evidence, evidence through the API, and smart document capture. Derived from PRD-001 §8.16.

## Requirements

### Requirement: FR-DOC-01 Attachments on any record
The system SHALL let users attach files (PDF, images, email messages) to any record.

Source: PRD-001 §8.16 · Priority M · Phase P1

#### Scenario: Attach and preview
- **WHEN** a user attaches a PDF, an image or an email message to a record
- **THEN** the file is attached to that record
- **AND** it can be previewed inline

#### Scenario: Virus scan on upload
- **WHEN** a file is uploaded
- **THEN** it is virus-scanned

### Requirement: FR-DOC-02 Retention-locked storage
Files SHALL be stored encrypted in Cloud Storage with retention locks matching the statutory retention period (§14), and SHALL be deleted only by retention expiry, never by users.

Source: PRD-001 §8.16 · Priority M · Phase P1

#### Scenario: Users cannot delete stored files
- **WHEN** a user tries to delete a committed file
- **THEN** the file is not deleted
- **AND** it is deleted only when its retention expires

#### Scenario: Draft files can still be removed
- **GIVEN** a file on a draft, still in staging
- **WHEN** a user removes it
- **THEN** it is removed: the rule applies to committed files (FR-DOC-07, FR-DOC-13)

#### Scenario: Per-object retain-until
- **WHEN** a file is committed
- **THEN** it gets a per-object retain-until date in locked mode (Object Retention Lock)
- **AND** the date is the end of the financial year in which the related transactions are completed plus the retention setting (§11.9)

#### Scenario: Retain-until is extended, never shortened
- **WHEN** a linked record moves the date later, or the company is struck off
- **THEN** the retain-until is extended, and never shortened
- **AND** after a strike-off records are kept at least 5 years after dissolution (IRAS)

#### Scenario: Bucket policy is only a floor
- **WHEN** retention is computed for a file
- **THEN** the bucket retention policy, which counts from upload, serves only as a backstop floor

#### Scenario: Legal hold
- **WHEN** a legal hold is placed on a file
- **THEN** it uses a temporary hold
- **AND** the hold does not restart the retention clock

#### Scenario: Object metadata is never authoritative
- **WHEN** a file's classification, financial year or parent is needed
- **THEN** it is read from the audited database row (FR-DOC-07, FR-DOC-09), never from the object metadata

#### Scenario: G4-068 Retain-until for a bill and a multi-year contract
- **WHEN** a bill for a December financial year is uploaded on 4 January, and a 2027–2029 contract is uploaded in 2027
- **THEN** the bill's retain-until is at least 31 December of its financial year plus the retention setting
- **AND** the contract's retain-until extends to the financial year in which its transactions complete
- **AND** neither is ever shortened
- **AND** CTL-AUD-03 passes

### Requirement: FR-DOC-04 Issued documents kept with a hash
Generated documents (invoices, payslips, reports) SHALL be stored as issued, with a hash, so a re-issued copy can be proven identical.

Source: PRD-001 §8.16 · Priority M · Phase P1

#### Scenario: Stored as issued
- **WHEN** an invoice, payslip or report is issued
- **THEN** the document is stored as issued, with its hash

#### Scenario: Re-issued copy is proven identical
- **WHEN** a copy of an issued document is produced again
- **THEN** its hash can be compared with the stored hash to prove it is identical

### Requirement: FR-DOC-05 Evidence on every important activity
The system SHALL show an Evidence panel on every record and on every important activity, holding several files per item, each with an evidence type, a short note and the date it relates to.

Source: PRD-001 §8.16 · Priority M · Phase P1

#### Scenario: Where the Evidence panel appears
- **WHEN** a record or an important activity is opened
- **THEN** it has an Evidence panel, including on the FR-APR-01 approval objects, filings and payments to IRAS, CPF and ACRA, close tasks, reconciliations, equity transactions, investor-record changes, founders' settlements, AI-proposal decisions, access reviews and manual control evidence

#### Scenario: Ways to add evidence
- **WHEN** a user adds evidence
- **THEN** they can drag and drop, use the file picker, paste a screenshot from the clipboard, use the phone camera (PWA, P2) or forward an email

#### Scenario: Several typed files per item
- **WHEN** files are added to an item's evidence
- **THEN** several files can be added
- **AND** each has an evidence type (e.g. "ACRA acknowledgement", "board resolution", "bank letter", "screenshot"), a short note and the date it relates to

### Requirement: FR-DOC-06 Evidence rules
The system SHALL apply evidence rules per activity type, marking each as required, recommended or optional with the evidence types expected.

Source: PRD-001 §8.16 · Priority M · Phase P1

#### Scenario: Missing required evidence blocks completion
- **GIVEN** an activity type with a required evidence item
- **WHEN** the activity is completed without that item
- **THEN** completion is blocked
- **AND** the message says in plain words what is missing and how to add it

#### Scenario: Missing recommended evidence is a warning
- **GIVEN** an activity type with a recommended evidence item
- **WHEN** the activity is completed without that item
- **THEN** a warning is shown and completion is not blocked

#### Scenario: Recommended rules are defaults and settings
- **WHEN** a new organisation is created
- **THEN** the recommended evidence rules are set to their defaults
- **AND** they are organisation settings (FR-ORG-08)

### Requirement: Default evidence for purchases, payee changes and payments
By default the system SHALL require and recommend evidence for bills, expense claims, founder costs paid personally, payee bank-detail changes and payment batch releases as the §8.16 evidence rules table sets out.

Source: PRD-001 §8.16

#### Scenario: Bill, expense claim or founder cost paid personally
- **WHEN** a bill, expense claim or founder cost paid personally is completed
- **THEN** a receipt or invoice is required
- **AND** a business purpose note is recommended

#### Scenario: Payee bank-detail change
- **WHEN** a payee's bank details are changed
- **THEN** a bank letter, or a screenshot of the vendor's confirmation through a known channel, is required
- **AND** a call-back note (who, when, number used) is recommended

#### Scenario: Payment batch release
- **WHEN** a payment batch is released
- **THEN** no evidence is required
- **AND** a bank confirmation screenshot or file is recommended

### Requirement: Default evidence for journals, reconciliations and close
By default the system SHALL require and recommend evidence for manual journals, bank reconciliation sign-offs and period closes as the §8.16 evidence rules table sets out.

Source: PRD-001 §8.16

#### Scenario: Manual journal
- **WHEN** a manual journal is completed
- **THEN** a supporting calculation or document is required above S$1,000
- **AND** an explanation is recommended

#### Scenario: Bank reconciliation sign-off
- **WHEN** a bank reconciliation is signed off
- **THEN** the bank statement for the period is required (FM-5)

#### Scenario: Period close
- **WHEN** a period is closed
- **THEN** a signed checklist (generated) and the reconciliations are required
- **AND** for each bank, provider-clearing, control (AR, AP, GST, payroll), deferral and director account with a balance, its support is required: a statement, or a subledger or schedule (INV-07)
- **AND** flux explanations (FR-GL-14) are recommended

#### Scenario: Year-end close
- **WHEN** the year-end close is completed
- **THEN** the director-account confirmation (FR-EQ-12) is also required

### Requirement: Default evidence for filings, statutory payments and grant claims
By default the system SHALL require and recommend evidence for statutory filings, CPF payments and grant claims as the §8.16 evidence rules table sets out.

Source: PRD-001 §8.16

#### Scenario: Statutory filings
- **WHEN** a GST F5, Form C-S/C, ECI or AIS filing is completed
- **THEN** the submission acknowledgement is required
- **AND** working papers are recommended

#### Scenario: CPF payment
- **WHEN** a CPF payment is completed
- **THEN** the payment acknowledgement is required
- **AND** the CPF submission file is recommended

#### Scenario: Grant claim
- **GIVEN** the organisation holds a grant (open question 43)
- **WHEN** a grant claim is completed
- **THEN** the letter of offer or award, and paid invoices and proof of payment for each claimed cost, are required
- **AND** evidence of each deliverable and the claim working paper are recommended

### Requirement: Default evidence for equity and investor records
By default the system SHALL require and recommend evidence for share issues and conversions, share transfers, SAFEs, notes and option grants, founder settlements and waivers, and investor record changes as the §8.16 evidence rules table sets out.

Source: PRD-001 §8.16

#### Scenario: Share issue or conversion
- **WHEN** a share issue or conversion (FR-EQ-06, FR-EQ-17) is completed
- **THEN** the shareholders' and board resolutions, a written agreement for non-cash consideration and the ACRA acknowledgement are required
- **AND** the valuation or price basis is recommended

#### Scenario: Share transfer
- **WHEN** a share transfer is completed
- **THEN** the signed transfer form and the ACRA acknowledgement are required
- **AND** the stamp duty certificate is recommended

#### Scenario: SAFE, note or option grant
- **WHEN** a SAFE, note or option grant is completed
- **THEN** the signed instrument or grant letter is required
- **AND** a board resolution is recommended

#### Scenario: Founder settlement or waiver
- **WHEN** a founder settlement or waiver (FR-EQ-05…07) is completed
- **THEN** the founder's balance confirmation and a deed or resolution are required

#### Scenario: Investor record change
- **WHEN** an investor record is changed
- **THEN** no evidence is required
- **AND** a source document (e.g. an updated agreement) is recommended

### Requirement: Default evidence for governance changes and control reviews
By default the system SHALL require and recommend evidence for period reopens, parameter and role changes, access reviews and manual control evidence as the §8.16 evidence rules table sets out.

Source: PRD-001 §8.16

#### Scenario: Period reopen, parameter change or role change
- **WHEN** a period is reopened, a parameter is changed or a role is changed
- **THEN** a reason (text) is required
- **AND** a supporting document is recommended

#### Scenario: Access review or manual control evidence
- **WHEN** an access review or a manual control evidence item is completed
- **THEN** a review record or screenshot is required

### Requirement: FR-DOC-07 Evidence integrity
For each evidence file the system SHALL write its SHA-256, uploader, time, source (upload, paste, email, API) and parent record to the audit trail in the same transaction (AUD-01).

Source: PRD-001 §8.16 · Priority M · Phase P1

#### Scenario: Hashes of the original and derived files
- **WHEN** a file is converted, stripped of metadata or previewed
- **THEN** the SHA-256 of the original as received and of each derived file are recorded
- **AND** the original and its derived files are linked to each other

#### Scenario: Files can be removed while the parent is a draft
- **GIVEN** the parent record is a draft
- **WHEN** a user removes an evidence file
- **THEN** the file is removed

#### Scenario: Evidence is append-only once the parent is final
- **GIVEN** the parent record is approved, posted or completed
- **WHEN** a user wants to change its evidence
- **THEN** a file can only be superseded by a newer one with a reason
- **AND** both files are kept

#### Scenario: Retention lock and nightly re-verification
- **WHEN** evidence is stored
- **THEN** it is under the retention lock of FR-DOC-02
- **AND** a nightly job re-verifies its hashes

### Requirement: FR-DOC-08 Safe files
The system SHALL accept only PDF, PNG, JPEG, HEIC (converted to JPEG), WebP, CSV, XLSX, DOCX, TXT, EML and MSG files, up to 25 MB per file (setting), malware-scanned before they are attached (ClamAV, SEC-24).

Source: PRD-001 §8.16 · Priority M · Phase P1

#### Scenario: Type decided from content
- **WHEN** a file is uploaded
- **THEN** its type is decided from its content, not its extension
- **AND** a type outside the accepted list is rejected

#### Scenario: Size limit
- **WHEN** a file larger than the size setting (25 MB by default) is uploaded
- **THEN** it is rejected

#### Scenario: Scan, conversion and metadata stripping
- **WHEN** a file is uploaded
- **THEN** it is malware-scanned before it is attached
- **AND** a HEIC image is converted to JPEG
- **AND** image metadata such as location is stripped

#### Scenario: Office files with macros
- **WHEN** an office file with macros is uploaded
- **THEN** it is rejected

#### Scenario: ZIP-based files are checked before parsing
- **WHEN** an XLSX or DOCX file is uploaded
- **THEN** its entry count, total uncompressed size and compression ratio are checked before it is parsed

#### Scenario: XML without DTDs or external entities
- **WHEN** XML in an uploaded file is parsed
- **THEN** DTDs and external entities are off

#### Scenario: Safe PDF previews
- **WHEN** a PDF is previewed
- **THEN** the preview is rendered server-side to images, without scripts, forms or external links

#### Scenario: File names are data
- **WHEN** a file is downloaded
- **THEN** its original file name is kept as data
- **AND** a safe name replaces it in the download headers

#### Scenario: Files are untrusted for the AI
- **WHEN** the AI reads an uploaded file
- **THEN** the file is treated as untrusted data (AI-18)

### Requirement: FR-DOC-13 Two-phase upload
Every upload SHALL land first in a staging bucket with no retention lock and be attached only when clean, and a file SHALL be copied into the locked bucket only when its parent is approved, posted or completed.

Source: PRD-001 §8.16 · Priority M · Phase P1

#### Scenario: Staging, scanning and attaching
- **WHEN** a file is uploaded
- **THEN** it lands in the staging bucket, with no retention lock
- **AND** it is scanned, converted (HEIC) and stripped of metadata there
- **AND** only a clean file is attached, with its SHA-256 recorded in the attaching transaction (FR-DOC-07)

#### Scenario: Draft parent keeps the file in staging
- **GIVEN** the parent record is a draft
- **WHEN** a file is attached
- **THEN** the file stays in staging and can be removed

#### Scenario: Final parent moves the file into the locked bucket
- **WHEN** the parent is approved, posted or completed
- **THEN** the file is copied into the locked bucket
- **AND** that transaction records its object generation and retain-until (FR-DOC-02)
- **AND** from then on the file can only be superseded

#### Scenario: Staging objects without a row
- **GIVEN** a staging object with no database row (failed scan, rejected or removed file, rolled-back transaction)
- **WHEN** anyone tries to read it through the app or API
- **THEN** it is not readable
- **AND** the Scheduler tick's sweep deletes it after 24 hours

#### Scenario: Orphans in the locked bucket
- **WHEN** an object in the locked bucket has no database row
- **THEN** an alert is raised
- **AND** the object is listed for review

#### Scenario: G4-069 Failed scan, removed file and rolled-back posting
- **WHEN** one upload fails the malware scan; a clean file is attached to a draft bill and then removed; a third file is copied into the locked bucket at posting but the posting transaction rolls back
- **THEN** none is readable through the app or API
- **AND** the first two never enter the locked bucket and are gone from staging within 24 hours of losing their row
- **AND** the third is reported as a locked orphan within one tick
- **AND** files of the bill once posted carry a retain-until

### Requirement: FR-DOC-09 Personal data in evidence
Each evidence file SHALL carry a data class: general, personal data, bank details, payroll or identity document.

Source: PRD-001 §8.16 · Priority M · Phase P1 (class) / P2 (redaction)

#### Scenario: Payroll and identity evidence
- **WHEN** a user opens evidence classed as payroll or identity document
- **THEN** access follows the payroll permissions (FR-PAY-13)

#### Scenario: Unmasked NRIC is flagged
- **WHEN** an evidence file is scanned for the NRIC pattern
- **THEN** unmasked identifiers are flagged

#### Scenario: Redacted copy
- **WHEN** a user redacts an evidence file with the redaction tool (P2)
- **THEN** a redacted copy is created
- **AND** the original is kept, restricted

### Requirement: FR-DOC-10 Find and export evidence
The system SHALL let users search evidence by activity, type, date and uploader, see a "missing evidence" list, and export an evidence pack.

Source: PRD-001 §8.16 · Priority S · Phase P1 (search) / P2 (packs)

#### Scenario: Search evidence
- **WHEN** a user searches evidence by activity, type, date or uploader
- **THEN** the matching evidence is listed

#### Scenario: Missing evidence list
- **WHEN** a user opens the "missing evidence" list
- **THEN** the items missing expected evidence are listed

#### Scenario: Evidence pack
- **WHEN** a user exports an evidence pack for a period, a filing, an investor or an audit
- **THEN** the pack includes a manifest of file hashes
- **AND** it feeds the auditor pack of FR-RPT-10

### Requirement: FR-DOC-11 Evidence through the API
Integrations SHALL upload evidence with `POST /v1/{collection}/{id}/evidence` (multipart, `Idempotency-Key`), under the same scanning and rules as any other upload.

Source: PRD-001 §8.16 · Priority M · Phase P1

#### Scenario: Integration uploads evidence
- **WHEN** an integration posts a multipart upload with an `Idempotency-Key` to `POST /v1/{collection}/{id}/evidence`
- **THEN** the file goes through the same scanning and rules as any other upload

#### Scenario: AI and MCP clients only propose
- **WHEN** an AI or MCP client wants evidence attached
- **THEN** it cannot upload directly
- **AND** it can only propose attaching an existing document (`doc.attach`, tier 1), which a human approves

### Requirement: FR-DOC-12 Smart document capture
The system SHALL capture receipts, bills, sales invoices, statements and contracts through one pipeline that classifies each document, extracts its fields and creates drafts only (`ap.draft_bill`, `exp.draft_claim`, tier 1).

Source: PRD-001 §8.16 · Priority M · Phase P1 (upload, paste, email-in) / P2 (camera, statements)

#### Scenario: Ways in
- **WHEN** a document is captured
- **THEN** it can come in by upload, drag and drop, paste, an email-in address per organisation and per user, InvoiceNow inbound (FR-AP-05) or the PWA camera (P2)

#### Scenario: Fields extracted with a confidence each
- **WHEN** the pipeline reads a document
- **THEN** it extracts the supplier, UEN, GST registration number, number, dates, currency, lines, GST, totals and the GST in SGD as printed, each with a confidence
- **AND** it uses the model's PDF and image input (AI-28)

#### Scenario: Checks, duplicates and matching
- **WHEN** fields have been extracted
- **THEN** the pipeline checks the IRAS tax-invoice particulars (FR-AR-05), detects duplicates and matches receipts to card and bank lines

#### Scenario: Drafts only
- **WHEN** the pipeline finishes
- **THEN** it creates drafts only (`ap.draft_bill`, `exp.draft_claim`, tier 1)

#### Scenario: Input-tax facts are proposed, not decided
- **WHEN** a captured document shows GST
- **THEN** capture proposes the input-tax facts (FR-AP-20)
- **AND** it never sets eligibility from an extracted GST amount alone

#### Scenario: Low-confidence fields are highlighted
- **WHEN** a person reviews the draft
- **THEN** the low-confidence fields are highlighted

#### Scenario: Original kept, text untrusted
- **WHEN** a document is captured
- **THEN** the original is kept as evidence (FR-DOC-05)
- **AND** its extracted text is untrusted data (AI-18)

#### Scenario: One pipeline replaces the earlier features
- **WHEN** the document inbox (FR-DOC-03) or AI extraction of bills (FR-AP-04) is used
- **THEN** it runs as part of this pipeline

#### Scenario: G4-029 Captured receipt where OCR reads a GST amount
- **WHEN** a captured receipt's OCR reads a GST amount
- **THEN** the draft shows the facts
- **AND** eligibility stays "to confirm" until the approver confirms it
- **AND** no claim is made from the GST figure alone
