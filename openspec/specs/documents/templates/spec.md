# documents/templates Specification

## Purpose
How the invoices, quotes and other documents customers see are rendered from templates that people edit: the block-based visual editor, fields and conditions, the compliance guard, versioning and approval, document and email template types, code mode for experts, the starter gallery and portability, and languages. Derived from PRD-001 §8.25.

## Requirements

### Requirement: FR-TPL-01 Visual, block-based editor
The system SHALL provide a visual, block-based template editor with a theme and a live preview: in P1 the theme, texts and optional blocks, and in P2 drag-and-drop layout.

Source: PRD-001 §8.25 · Priority M · Phase P1 (theme, texts, optional blocks); P2 (layout)

#### Scenario: Blocks available
- **WHEN** a user edits a template
- **THEN** blocks are available for the header and logo, our and the customer's details, document details, the line-item table, totals and GST summary, payment details (bank, PayNow QR, payment link), notes and terms, a signature or stamp image, the footer, and free text or image blocks

#### Scenario: Arrange and configure blocks
- **WHEN** a user arranges blocks
- **THEN** blocks can be dragged to reorder
- **AND** each block has its own settings for show, alignment and labels

#### Scenario: Theme
- **WHEN** a user sets the template theme
- **THEN** it covers the logo, colours (contrast checked), a self-hosted font, A4 or Letter paper, and margins

#### Scenario: Live preview
- **WHEN** a user edits a template
- **THEN** a live preview shows it with sample data or any real document

#### Scenario: Basic and Expert mode
- **WHEN** the editor is used in Basic mode
- **THEN** it offers a theme, logo, colours and texts
- **AND** Expert mode adds layout, columns and conditions

#### Scenario: What ships in P1 and what in P2
- **WHEN** the editor ships in P1
- **THEN** it offers the theme, logo, colours, font and texts, showing or hiding optional blocks, page settings and the live preview
- **AND** drag-and-drop layout and per-block layout settings, the Expert part, follow in P2 (open question 44, decided 16 Oct 2026)

### Requirement: FR-TPL-02 Fields and conditions
Templates SHALL use fields from a typed catalogue with formats for dates and money and, from P2, conditional blocks from a fixed list and a column chooser; the visual editor SHALL allow no free code.

Source: PRD-001 §8.25 · Priority M · Phase P1 (fields); P2 (conditions, columns)

#### Scenario: Typed field catalogue
- **WHEN** a user inserts a field
- **THEN** it comes from the typed catalogue (customer name, due date, amount due, SGD equivalents, and so on)
- **AND** dates and money take a chosen format

#### Scenario: Conditional blocks from a fixed list
- **WHEN** a user makes a block conditional
- **THEN** the condition is one of GST-registered, foreign currency, overdue, discount present or PayNow available

#### Scenario: Column chooser
- **WHEN** a user edits the line table
- **THEN** a column chooser sets its columns

#### Scenario: No free code in the visual editor
- **WHEN** a user works in the visual editor
- **THEN** no free code can be entered

#### Scenario: Every template renders in the sandbox
- **WHEN** any template, visual or code, is rendered
- **THEN** it renders in the `render` job's sandbox (FR-TPL-06), with no network, no secrets, and only the template's own assets and a JSON data file

#### Scenario: Fields in P1, conditions and columns in P2
- **WHEN** the template editor ships in P1
- **THEN** it offers the typed field catalogue and its formats
- **AND** conditional blocks and the column chooser follow in P2 (open question 44, decided 16 Oct 2026)

### Requirement: FR-TPL-03 Compliance guard
For tax invoices, simplified tax invoices and credit notes, the IRAS particulars (FR-AR-05) SHALL be locked blocks that cannot be removed or hidden.

Source: PRD-001 §8.25 · Priority M · Phase P1

#### Scenario: Particulars cannot be removed or hidden
- **WHEN** a user edits a tax invoice, simplified tax invoice or credit note template
- **THEN** the blocks carrying the IRAS particulars cannot be removed or hidden

#### Scenario: Publishing checks the particulars
- **WHEN** such a template is published
- **THEN** test documents are rendered
- **AND** publishing fails if a particular is missing

#### Scenario: InvoiceNow is not affected
- **WHEN** an InvoiceNow document is sent
- **THEN** it is structured data and the PDF template does not affect it

### Requirement: FR-TPL-04 Template versioning
Saving a template SHALL make a draft, and publishing SHALL make an immutable numbered version recording who, when and a note; the side-by-side visual diff follows in P2.

Source: PRD-001 §8.25 · Priority M · Phase P1 (versions); P2 (visual diff)

#### Scenario: Compare versions
- **WHEN** a user compares any two versions of a template
- **THEN** they are shown side by side, with a visual diff and a list of the changed settings

#### Scenario: Rollback
- **WHEN** a user rolls back to an earlier version
- **THEN** that version is republished as a new version

#### Scenario: Reprints are identical
- **WHEN** a document is issued
- **THEN** it stores the template version and a hash (FR-DOC-04)
- **AND** a reprint is identical

#### Scenario: Publishing is approved
- **WHEN** a template version is published
- **THEN** publishing is an approval object (FR-APR-01)
- **AND** in Founder mode it is self-approved with the weekly review

#### Scenario: Versions in P1, the visual diff in P2
- **WHEN** template versioning ships in P1
- **THEN** it offers drafts, immutable published versions, rollback, and the version and hash on every issued document
- **AND** the side-by-side visual diff follows in P2 (open question 44, decided 16 Oct 2026)

### Requirement: FR-TPL-05 Document types
The system SHALL provide templates for invoices, quotes, credit notes, receipts, customer statements, reminder letters, remittance advice, purchase orders and, in P2, payslips (used by Nemura through the shared engine; Fyniq issues none), plus email templates.

Source: PRD-001 §8.25 · Priority M · Phase P1

#### Scenario: Email templates
- **WHEN** an email template is edited
- **THEN** it has a subject and a body using the same fields as document templates

#### Scenario: Default per type and per-customer override
- **GIVEN** a document type has a default template and a customer has an override for it
- **WHEN** a document of that type is produced for that customer
- **THEN** the customer's override is used
- **AND** other customers get the default template

### Requirement: FR-TPL-06 Code mode for experts
Templates SHALL compile to Typst (§12.2), and an expert SHALL be able to edit the Typst source in a sandbox.

Source: PRD-001 §8.25 · Priority S · Phase P2

#### Scenario: The sandbox
- **WHEN** an expert's Typst source is rendered
- **THEN** it has no network, only the template's own assets, and time and memory limits

#### Scenario: Packages ship with the render service
- **GIVEN** Typst fetches `@preview` packages from the network on demand
- **WHEN** a template uses a package
- **THEN** only the allowed packages shipped with the `render` service's image are available
- **AND** downloads are blocked

#### Scenario: Data as a JSON file
- **WHEN** a template is rendered
- **THEN** its data goes in as a JSON file, not as `sys.inputs` strings

#### Scenario: Code changes are versioned and approved
- **WHEN** an expert changes a template's code
- **THEN** the change is versioned and approved like any other template change

#### Scenario: Output stays accessible
- **WHEN** a code-mode template is rendered
- **THEN** the output stays PDF/UA-1, with tagged headings, reading order and alternative text for the logo

### Requirement: FR-TPL-07 Starter gallery and portability
The system SHALL offer 3–4 starter templates built with the design system, and SHALL export and import templates as a versioned bundle (JSON plus assets).

Source: PRD-001 §8.25 · Priority S · Phase P1

#### Scenario: Starter gallery
- **WHEN** a user creates a template
- **THEN** they can start from one of 3–4 starter templates built with the design system

#### Scenario: Export and import
- **WHEN** a template is exported
- **THEN** it is a versioned bundle of JSON plus assets
- **AND** the bundle can be imported

### Requirement: FR-TPL-08 Languages
Templates SHALL support labels per language, and each customer SHALL have a language.

Source: PRD-001 §8.25 · Priority C · Phase P3

#### Scenario: Customer's language
- **GIVEN** a customer whose language is set
- **WHEN** a document is produced for that customer
- **THEN** it uses the labels in that language
