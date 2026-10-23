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

#### Scenario: The theme's data (rev 0.95)
- **WHEN** a document template's page and theme are saved
- **THEN** the paper is A4 or Letter with margins of 10 to 40 mm, the font one the render service ships (Inter, IBM Plex Sans, IBM Plex Serif), and the logo the organisation's, shown or not
- **AND** an accent colour below 4.5:1 against white is refused (TPL-05), as is a block the document type does not take or one repeated (TPL-06)

#### Scenario: The live preview as built (rev 0.97)
- **WHEN** content, saved or not, is previewed
- **THEN** it is filled with made-up sample data of its document type: a document rendered by the render service as PDF/A-2a and PDF/UA-1, with the IRAS particulars it would lack, or an email's subject and body
- **AND** nothing is saved, and the same content gives the same bytes

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

#### Scenario: The render service's sandbox (rev 0.96)
- **WHEN** the private `render` service compiles a document, for the api or the worker only
- **THEN** it runs in a fresh folder that is Typst's root, with only its own source, the layout as JSON and the logo, the shipped fonts, no packages, an empty environment and a time limit
- **AND** texts are written as text, never read as markup or code, and the same version, data and issue time give the same bytes

#### Scenario: Fields in P1, conditions and columns in P2
- **WHEN** the template editor ships in P1
- **THEN** it offers the typed field catalogue and its formats
- **AND** conditional blocks and the column chooser follow in P2 (open question 44, decided 16 Oct 2026)

#### Scenario: Fields and formats as built (rev 0.95)
- **WHEN** a text names a field as `{{customer.name}}` or `{{document.date|long}}`
- **THEN** the field is one of the document type's typed catalogue (text, date, money or number), with a date formatted medium (the default), long, short or iso and an amount code (the default), symbol or plain
- **AND** a field outside the catalogue, a format its type does not take, or braces that name no field are refused (TPL-09)

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

#### Scenario: The locked blocks as built (rev 0.95)
- **WHEN** an invoice or credit note template is saved
- **THEN** its header (an invoice's only), our details, the customer's, the document details, the lines and the totals are present and shown (TPL-07)
- **AND** an invoice's title is the renderer's ("Tax Invoice" when GST-registered), never a text, as IRAS's "Invoicing customers" requires

#### Scenario: Test documents at publication as built (rev 0.97)
- **WHEN** an invoice or credit note template is published
- **THEN** test documents of the sample data (a full and a simplified tax invoice, or a credit note) are rendered and the particulars found in each
- **AND** the publication is refused when one lacks a particular (TPL-16), or when no render service is configured (TPL-17)

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

#### Scenario: Versions and rollback as built (rev 0.95)
- **WHEN** a template is published, or rolled back to an earlier version
- **THEN** the draft's content, or the earlier version's, becomes the next numbered version with its SHA-256, who, when and a note, and never changes (FY176)
- **AND** a rollback leaves the draft as it is, and content that is the version in force is refused (TPL-12)

#### Scenario: Publishing under approval as built (rev 0.95)
- **WHEN** a template's publication is approved
- **THEN** in Founder mode a person who holds A publishes alone, listed for the weekly review (FM-3)
- **AND** otherwise another person who holds A approves the request, never the one who saved the draft (TPL-15), and a change to the template voids it

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

#### Scenario: Templates by type and kind (rev 0.95)
- **WHEN** a template is made
- **THEN** it serves one document type as a document or an email, and starts from that type's default content
- **AND** an email template's subject is one line and its body uses the type's fields (TPL-10, TPL-11)

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
