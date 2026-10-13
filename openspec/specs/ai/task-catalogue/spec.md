# ai/task-catalogue Specification

## Purpose
The approved task catalogue: the only task types the AI may propose, what each entry defines, the never-list the proposal service always rejects, the AI tasks page, and the initial task types with their tier, domain command, limits and approver. Derived from PRD-001 §9.6 and Appendix E.

## Requirements

### Requirement: Catalogue as versioned, dual-controlled data
The catalogue SHALL list every task type the AI may propose, as versioned data changed only through a dual-controlled approval (SOD-8).

Source: PRD-001 §9.6

#### Scenario: Only catalogue task types can be proposed
- **WHEN** the AI proposes a task type that the catalogue does not list
- **THEN** it cannot be proposed

#### Scenario: A catalogue change needs dual-controlled approval
- **WHEN** someone changes the catalogue
- **THEN** the change takes effect only through a dual-controlled approval (SOD-8)
- **AND** it creates a new catalogue version

### Requirement: Catalogue entry attributes
Each catalogue entry SHALL define its ID and name, the description shown to the AI and the approver, a strict input JSON Schema, the domain command it maps to, its risk tier, limits, approver roles, expiry and enabled flag.

Source: PRD-001 §9.6

#### Scenario: Identity, description, schema and command
- **WHEN** an entry is defined, for example ID `bank.categorise_lines` named "Categorise bank lines"
- **THEN** it has a description shown to both the AI and the approver ("Assign category, contact, GST code and dimensions to unreconciled bank lines")
- **AND** a strict input JSON Schema (line IDs, account codes, contact IDs, GST codes, dimension values)
- **AND** the domain command it maps to (`CategoriseBankLines`)

#### Scenario: Limits
- **WHEN** an entry's limits are defined
- **THEN** they can set the maximum items per proposal, the maximum amount per item and in total, the allowed accounts and the entities

#### Scenario: Expiry
- **WHEN** an entry is defined
- **THEN** its proposals expire after 72 h

#### Scenario: Enabled per organisation, off by default
- **WHEN** an organisation starts using the catalogue
- **THEN** each entry has an enabled flag per organisation
- **AND** the flag is off by default (AI-32)

### Requirement: Risk tiers and approver roles
Each task type SHALL have a risk tier: 1 (routine: reversible with AI-31 undo and no cash movement, though it may post), 2 (posts to the ledger or changes master data) or 3 (high-risk: never allowed today, reserved), and approver roles set per task type in Appendix E.

Source: PRD-001 §9.6

#### Scenario: Tier 1 may post reversible entries
- **WHEN** a tier-1 task type such as bank categorisation or matching executes
- **THEN** it may post ledger entries (Appendix B.1) that AI-31 can undo, and it never moves cash

#### Scenario: Approver roles per task type
- **WHEN** approver roles are set for a task type
- **THEN** they are the roles Appendix E names for it: a role holding the target permission and, above the task's limit, the role named there
- **AND** the human who asked is the preparer (AI-10)

#### Scenario: Tier 3 is reserved
- **WHEN** a task type of tier 3 is proposed
- **THEN** it is not allowed, because tier 3 is reserved and never allowed today

### Requirement: Initial catalogue
The initial catalogue (P1) SHALL contain the tier-1 and tier-2 task types below, listed in full in Appendix E.

Source: PRD-001 §9.6

#### Scenario: Tier-1 task types
- **WHEN** the initial catalogue is installed
- **THEN** its tier-1 task types are: categorise bank lines; match payments to invoices or bills; create or update a bank rule; attach a document to a record; draft an invoice from a quote, order or timesheets; draft a bill or expense claim from a document; draft reminder emails (sent only after approval); save a forecast scenario; draft board-pack commentary

#### Scenario: Tier-2 task types
- **WHEN** the initial catalogue is installed
- **THEN** its tier-2 task types are: draft and post an accrual, prepayment or reclassification journal within limits; post a draft invoice or bill the AI prepared; update contact details other than bank details; create a recurring template; propose a budget version

### Requirement: Never in the catalogue
The proposal service SHALL reject every task type on the never-list, even if someone adds it to the catalogue, and a control test SHALL assert it.

Source: PRD-001 §9.6

#### Scenario: The never-list
- **WHEN** a task type would do any of the following
- **THEN** it is never in the catalogue: approving or rejecting anything; releasing or initiating payments; creating or changing payee bank details; changing users, roles, team mode, approval policies, the AI catalogue or AI settings; changing statutory parameters; reopening periods; voiding or deleting posted records; filing with IRAS, CPF or ACRA; exporting the tenant; anything touching the audit trail; issuing or transferring shares, changing holders, instruments or investor rights, or repaying, converting or waiving a director's balance; filing or lodging anything, changing officers or registers, or marking an obligation as met (scheduled agents, AI-66)

#### Scenario: The AI never approves or rejects
- **WHEN** a proposal is approved or rejected
- **THEN** a named human does it in the web app, never the AI
- **AND** for `ar.post_invoice` and `ap.post_bill` that approval is the document's approval, by someone other than the requester (AI-10)

#### Scenario: Rejected even if added to the catalogue
- **GIVEN** someone has added a never-listed task type to the catalogue
- **WHEN** the AI proposes it
- **THEN** the proposal service rejects it

#### Scenario: A control test asserts the never-list
- **WHEN** the control tests run
- **THEN** a control test asserts that the proposal service rejects every never-listed task type

#### Scenario: Founders' expense claims and round scenarios stay possible
- **WHEN** the AI drafts a founder's expense claim (`exp.draft_claim`) or runs round scenarios
- **THEN** the equity and director's-balance exclusions do not prevent it

### Requirement: AI tasks page
The *AI tasks* page ("List of approved tasks") SHALL show the catalogue with each task type's tier, limits and status, and the history of every proposal.

Source: PRD-001 §9.6

#### Scenario: Catalogue view
- **WHEN** a user opens the AI tasks page
- **THEN** it shows the catalogue with each task type's tier, limits and status

#### Scenario: Proposal history
- **WHEN** a user opens the proposal history on the AI tasks page
- **THEN** it shows every proposal (pending, approved, executed, rejected, expired)
- **AND** the history is filterable and each proposal links to the records it changed

### Requirement: bank.categorise_lines Categorise bank lines
The catalogue SHALL let the AI propose assigning a category, contact, GST code and dimensions to bank lines.

Source: PRD-001 §E

#### Scenario: Tier 1, posting only within its limits
- **WHEN** the task type's tier is read
- **THEN** it is tier 1: categorisation does post (Appendix B.1), within the limits below, reversible with AI-31 undo and with no cash movement

#### Scenario: Maps to CategoriseBankLines
- **GIVEN** an approved proposal of this task type
- **WHEN** it executes
- **THEN** it runs the domain command `CategoriseBankLines`

#### Scenario: Default limits
- **WHEN** the AI proposes a categorisation
- **THEN** by default it covers at most 100 lines, each at most S$10,000
- **AND** at most S$50,000 in total per proposal (recommended default, FR-ORG-08)

#### Scenario: Approver is Bookkeeper or above
- **WHEN** the proposal is approved
- **THEN** the approver is a Bookkeeper or above (Bookkeeper+), which may be the requesting user if they hold that permission

### Requirement: bank.match_payments Match payments to invoices and bills
The catalogue SHALL let the AI propose matching bank lines to open invoices or bills, including partial matches and a settlement difference within tolerance.

Source: PRD-001 §E

#### Scenario: Tier 1, posting only within its limits
- **WHEN** the task type's tier is read
- **THEN** it is tier 1: matching does post (Appendix B.1), within the limits below, reversible with AI-31 undo and with no cash movement

#### Scenario: Maps to MatchPayments
- **GIVEN** an approved proposal of this task type
- **WHEN** it executes
- **THEN** it runs the domain command `MatchPayments`

#### Scenario: Default limits
- **WHEN** the AI proposes matches
- **THEN** by default a proposal holds at most 100 matches
- **AND** its total is limited as for `bank.categorise_lines`

#### Scenario: Settlement differences only within the B.1 tolerances
- **WHEN** a proposed match leaves a settlement difference
- **THEN** the difference is within the tolerances of Appendix B.1

#### Scenario: Approver is Bookkeeper or above
- **WHEN** the proposal is approved
- **THEN** the approver is a Bookkeeper or above (Bookkeeper+), which may be the requesting user if they hold that permission

### Requirement: bank.rule_upsert Create or edit a bank rule
The catalogue SHALL let the AI propose creating or editing a bank rule, with its back-test result.

Source: PRD-001 §E

#### Scenario: Tier 1
- **WHEN** the task type's tier is read
- **THEN** it is tier 1: routine and reversible with AI-31 undo, with no cash movement

#### Scenario: Maps to UpsertBankRule
- **GIVEN** an approved proposal of this task type
- **WHEN** it executes
- **THEN** it runs the domain command `UpsertBankRule`

#### Scenario: Back-test required
- **WHEN** the AI proposes a bank rule
- **THEN** the proposal includes the rule's back-test result

#### Scenario: Never sets auto-apply
- **WHEN** the AI proposes a bank rule
- **THEN** the rule never has auto-apply set

#### Scenario: Approver is Bookkeeper or above
- **WHEN** the proposal is approved
- **THEN** the approver is a Bookkeeper or above (Bookkeeper+), which may be the requesting user if they hold that permission

### Requirement: doc.attach Attach a document to a record
The catalogue SHALL let the AI propose attaching an existing document to a record.

Source: PRD-001 §E

#### Scenario: Tier 1
- **WHEN** the task type's tier is read
- **THEN** it is tier 1: no ledger impact

#### Scenario: Maps to AttachDocument
- **GIVEN** an approved proposal of this task type
- **WHEN** it executes
- **THEN** it runs the domain command `AttachDocument`

#### Scenario: Only existing documents
- **WHEN** the AI proposes an attachment
- **THEN** the document is one that already exists

#### Scenario: Approver is the record owner
- **WHEN** the proposal is approved
- **THEN** the approver is the owner of the record

### Requirement: ar.draft_invoice Draft an invoice
The catalogue SHALL let the AI propose drafting an invoice from a quote, order, timesheets or a description.

Source: PRD-001 §E

#### Scenario: Tier 1
- **WHEN** the task type's tier is read
- **THEN** it is tier 1: no ledger impact

#### Scenario: Maps to DraftInvoice
- **GIVEN** an approved proposal of this task type
- **WHEN** it executes
- **THEN** it runs the domain command `DraftInvoice`

#### Scenario: Draft only
- **WHEN** the proposal executes
- **THEN** the invoice it creates is a draft only

#### Scenario: Approver is AR or above
- **WHEN** the proposal is approved
- **THEN** the approver is AR+ (the AR role or above), which may be the requesting user if they hold that permission

### Requirement: ap.draft_bill Draft a bill
The catalogue SHALL let the AI propose drafting a bill from an uploaded or emailed document.

Source: PRD-001 §E

#### Scenario: Tier 1
- **WHEN** the task type's tier is read
- **THEN** it is tier 1: no ledger impact

#### Scenario: Maps to DraftBill
- **GIVEN** an approved proposal of this task type
- **WHEN** it executes
- **THEN** it runs the domain command `DraftBill`

#### Scenario: Draft only
- **WHEN** the proposal executes
- **THEN** the bill it creates is a draft only

#### Scenario: Approver is AP or above
- **WHEN** the proposal is approved
- **THEN** the approver is AP+ (the AP role or above), which may be the requesting user if they hold that permission

### Requirement: exp.draft_claim Draft an expense claim
The catalogue SHALL let the AI propose drafting an expense claim from a receipt.

Source: PRD-001 §E

#### Scenario: Tier 1
- **WHEN** the task type's tier is read
- **THEN** it is tier 1: no ledger impact

#### Scenario: Maps to DraftClaim
- **GIVEN** an approved proposal of this task type
- **WHEN** it executes
- **THEN** it runs the domain command `DraftClaim`

#### Scenario: Draft only, own claims
- **WHEN** the AI proposes an expense claim
- **THEN** it is a draft only
- **AND** it is a claim of the requesting employee's own

#### Scenario: Approver is the employee, for their own claim
- **WHEN** the proposal is approved
- **THEN** the approver is the employee whose own claim it is

### Requirement: ar.draft_reminders Draft reminder emails
The catalogue SHALL let the AI propose drafting reminder emails for overdue invoices.

Source: PRD-001 §E

#### Scenario: Tier 1
- **WHEN** the task type's tier is read
- **THEN** it is tier 1: no ledger impact

#### Scenario: Maps to DraftReminders
- **GIVEN** an approved proposal of this task type
- **WHEN** it executes
- **THEN** it runs the domain command `DraftReminders`

#### Scenario: Sent only after approval
- **GIVEN** reminder emails the AI drafted
- **WHEN** the proposal has not been approved
- **THEN** no reminder is sent

#### Scenario: Approver is AR or above
- **WHEN** the proposal is approved
- **THEN** the approver is AR+ (the AR role or above), which may be the requesting user if they hold that permission

### Requirement: plan.save_scenario Save a forecast scenario
The catalogue SHALL let the AI propose saving a forecast scenario the user simulated.

Source: PRD-001 §E

#### Scenario: Tier 1
- **WHEN** the task type's tier is read
- **THEN** it is tier 1: no ledger impact

#### Scenario: Maps to SaveScenario
- **GIVEN** an approved proposal of this task type
- **WHEN** it executes
- **THEN** it runs the domain command `SaveScenario`

#### Scenario: Only a scenario the user simulated
- **WHEN** the AI proposes saving a scenario
- **THEN** it is a forecast scenario the user simulated

#### Scenario: Approver is the requesting user
- **WHEN** the proposal is approved
- **THEN** the approver is the user who asked

### Requirement: rpt.draft_commentary Draft board-pack commentary
The catalogue SHALL let the AI propose drafting board-pack commentary.

Source: PRD-001 §E

#### Scenario: Tier 1
- **WHEN** the task type's tier is read
- **THEN** it is tier 1: no ledger impact

#### Scenario: Maps to DraftCommentary
- **GIVEN** an approved proposal of this task type
- **WHEN** it executes
- **THEN** it runs the domain command `DraftCommentary`

#### Scenario: Text only
- **WHEN** the AI proposes commentary
- **THEN** the proposal contains text only

#### Scenario: Approver is Controller or above
- **WHEN** the proposal is approved
- **THEN** the approver is a Controller or above (Controller+), which may be the requesting user if they hold that permission

### Requirement: gl.post_journal Post an accrual, prepayment, reclassification or correction journal
The catalogue SHALL let the AI propose an accrual, prepayment, reclassification or correction journal.

Source: PRD-001 §E

#### Scenario: Tier 2
- **WHEN** the task type's tier is read
- **THEN** it is tier 2: it posts to the ledger

#### Scenario: Maps to PostJournal
- **GIVEN** an approved proposal of this task type
- **WHEN** it executes
- **THEN** it runs the domain command `PostJournal`

#### Scenario: Approver up to S$5,000 per entry
- **GIVEN** a journal entry of at most S$5,000 (the default limit)
- **WHEN** the proposal is approved
- **THEN** the approver is a user with the target permission, Accountant or above (Accountant+)

#### Scenario: Controller above the limit
- **GIVEN** a journal entry above S$5,000
- **WHEN** the proposal is approved
- **THEN** the approver is a Controller

### Requirement: ar.post_invoice Post a draft invoice the AI prepared
The catalogue SHALL let the AI propose posting a draft invoice the AI prepared; the human who approves the proposal approves the invoice, and the AI never approves.

Source: PRD-001 §E

#### Scenario: Tier 2
- **WHEN** the task type's tier is read
- **THEN** it is tier 2: it posts to the ledger

#### Scenario: Maps to PostInvoice
- **GIVEN** an approved proposal of this task type
- **WHEN** it executes
- **THEN** it runs the domain command `PostInvoice`

#### Scenario: Approver up to S$20,000
- **GIVEN** an invoice of at most S$20,000 (the default limit)
- **WHEN** the proposal is approved
- **THEN** the approver is a user with the target permission, AR or above (AR+), other than the requester

#### Scenario: Controller above the limit
- **GIVEN** an invoice above S$20,000
- **WHEN** the proposal is approved
- **THEN** the approver is a Controller

### Requirement: ap.post_bill Post a draft bill the AI prepared
The catalogue SHALL let the AI propose posting a draft bill the AI prepared; the human who approves the proposal approves the bill.

Source: PRD-001 §E

#### Scenario: Tier 2
- **WHEN** the task type's tier is read
- **THEN** it is tier 2: it posts to the ledger

#### Scenario: Maps to PostBill
- **GIVEN** an approved proposal of this task type
- **WHEN** it executes
- **THEN** it runs the domain command `PostBill`

#### Scenario: Approver up to S$10,000
- **GIVEN** a bill of at most S$10,000 (the default limit)
- **WHEN** the proposal is approved
- **THEN** the approver is a user with the target permission, AP or above (AP+), other than the requester

#### Scenario: Controller above the limit
- **GIVEN** a bill above S$10,000
- **WHEN** the proposal is approved
- **THEN** the approver is a Controller

### Requirement: ct.update_contact Update contact details
The catalogue SHALL let the AI propose updating a contact's details, other than its bank details.

Source: PRD-001 §E

#### Scenario: Tier 2
- **WHEN** the task type's tier is read
- **THEN** it is tier 2: it changes master data

#### Scenario: Maps to UpdateContact
- **GIVEN** an approved proposal of this task type
- **WHEN** it executes
- **THEN** it runs the domain command `UpdateContact`

#### Scenario: Excludes bank fields
- **WHEN** the AI proposes a contact update
- **THEN** the proposal cannot include any bank field

#### Scenario: Approver is AR or AP or above
- **WHEN** the proposal is approved
- **THEN** the approver is a user with the target permission, AR or AP or above (AR/AP+)

### Requirement: recur.create_template Create a recurring template
The catalogue SHALL let the AI propose creating a recurring template.

Source: PRD-001 §E

#### Scenario: Tier 2
- **WHEN** the task type's tier is read
- **THEN** it is tier 2: it changes master data

#### Scenario: Maps to CreateRecurring
- **GIVEN** an approved proposal of this task type
- **WHEN** it executes
- **THEN** it runs the domain command `CreateRecurring`

#### Scenario: Monthly equivalent limit
- **WHEN** the AI proposes a recurring template
- **THEN** by default its monthly equivalent is at most S$5,000

#### Scenario: Approver is Accountant or above
- **GIVEN** a monthly equivalent of at most S$5,000
- **WHEN** the proposal is approved
- **THEN** the approver is a user with the target permission, Accountant or above (Accountant+)

#### Scenario: Controller above the limit
- **GIVEN** a monthly equivalent above S$5,000
- **WHEN** the proposal is approved
- **THEN** the approver is a Controller

### Requirement: plan.propose_budget Propose a budget version
The catalogue SHALL let the AI propose creating a budget version, as a draft.

Source: PRD-001 §E

#### Scenario: Tier 2
- **WHEN** the task type's tier is read
- **THEN** it is tier 2

#### Scenario: Maps to CreateBudgetVersion
- **GIVEN** an approved proposal of this task type
- **WHEN** it executes
- **THEN** it runs the domain command `CreateBudgetVersion`

#### Scenario: Draft, with activation separate
- **WHEN** the proposal executes
- **THEN** the budget version it creates is a draft
- **AND** activating it is a separate step outside this task type

#### Scenario: Approver is a Controller
- **WHEN** the proposal is approved
- **THEN** the approver is a Controller

### Requirement: kb.propose_page Propose a knowledge-base page
The catalogue SHALL let the AI propose a know-how page for the organisation knowledge base, drafted from a conversation and without figures, from P2.

Source: PRD-001 §E

#### Scenario: Tier 1
- **WHEN** the task type's tier is read
- **THEN** it is tier 1

#### Scenario: Maps to ProposeKnowledgePage
- **GIVEN** an approved proposal of this task type
- **WHEN** it executes
- **THEN** it runs the domain command `ProposeKnowledgePage`

#### Scenario: Draft only, no organisation figures
- **WHEN** the AI proposes a page
- **THEN** the page is a draft only and contains no organisation figures (AI-53, AI-59)

#### Scenario: Approver is Owner or Administrator
- **WHEN** the proposal is approved
- **THEN** the approver is the Owner or an Administrator

### Requirement: sec.create_task Create a compliance task
The catalogue SHALL let the AI propose a compliance task, with an owner and a due date, from a finding.

Source: PRD-001 §E

#### Scenario: Tier 1
- **WHEN** the task type's tier is read
- **THEN** it is tier 1

#### Scenario: Maps to CreateComplianceTask
- **GIVEN** an approved proposal of this task type
- **WHEN** it executes
- **THEN** it runs the domain command `CreateComplianceTask`

#### Scenario: Only tasks the model suggests
- **WHEN** a deterministic finding raises a task
- **THEN** the task is created directly (AI-65), not through this task type
- **AND** proposals of this type cover tasks the model suggests

#### Scenario: Deduplicated per finding
- **GIVEN** a finding that already has a proposed or open task
- **WHEN** the AI proposes another task for it
- **THEN** no duplicate task is proposed

#### Scenario: Approver is Owner or Company secretary
- **WHEN** the proposal is approved
- **THEN** the approver is the Owner or the Company secretary

### Requirement: sec.draft_document Draft a corporate document
The catalogue SHALL let the AI propose a draft resolution, notice, minutes or the annual-return pack from templates (FR-CS-05, FR-CS-07).

Source: PRD-001 §E

#### Scenario: Tier 1
- **WHEN** the task type's tier is read
- **THEN** it is tier 1

#### Scenario: Maps to DraftCorporateDocument
- **GIVEN** an approved proposal of this task type
- **WHEN** it executes
- **THEN** it runs the domain command `DraftCorporateDocument`

#### Scenario: Draft only
- **WHEN** the document is drafted
- **THEN** it is a draft only and is never filed or signed by the AI

#### Scenario: Approver is Owner or Company secretary
- **WHEN** the proposal is approved
- **THEN** the approver is the Owner or the Company secretary

### Requirement: sec.draft_reminder Draft a reminder about an obligation
The catalogue SHALL let the AI propose a reminder to a director, the secretary or the accountant about an obligation.

Source: PRD-001 §E

#### Scenario: Tier 1
- **WHEN** the task type's tier is read
- **THEN** it is tier 1

#### Scenario: Maps to DraftReminder
- **GIVEN** an approved proposal of this task type
- **WHEN** it executes
- **THEN** it runs the domain command `DraftReminder`

#### Scenario: Sent only after approval
- **WHEN** the AI drafts a reminder
- **THEN** it is sent only after a person approves it

#### Scenario: Approver is Owner or Company secretary
- **WHEN** the proposal is approved
- **THEN** the approver is the Owner or the Company secretary
