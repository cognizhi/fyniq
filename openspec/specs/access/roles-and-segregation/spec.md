# access/roles-and-segregation Specification

## Purpose
Who may do what in Fyniq: the standard finance roles and what each can and cannot do, the permission model with attribute conditions and field masking, the segregation-of-duties conflicts SOD-1 to SOD-10, and the permission matrix of Standard mode. Derived from PRD-001 §6.2, PRD-001 §6.3 and PRD-001 §D (Appendix D).

## Requirements

### Requirement: Finance roles and role combinations
Roles SHALL follow common finance practice (maker–checker, segregation of preparation, approval, payment, reconciliation and administration), and a user SHALL be able to hold several roles, with the team mode (§6.4) deciding which combinations are allowed.

Source: PRD-001 §6.2

#### Scenario: Several roles per user
- **WHEN** a second role is assigned to a user
- **THEN** the user holds both roles

#### Scenario: Team mode decides combinations
- **WHEN** a combination of roles is assigned to a user
- **THEN** whether it is allowed is decided by the organisation's team mode (§6.4)

### Requirement: Permission model
Permissions SHALL combine role-based access control with attribute conditions (organisation, entity, department, amount, document type, data sensitivity), mask sensitive fields, and be checked in the API layer and again by database row-level security.

Source: PRD-001 §6.2

#### Scenario: Attribute conditions
- **GIVEN** a user whose role is limited to one department and an amount
- **WHEN** they act on a record of another department, or above that amount
- **THEN** the action is refused

#### Scenario: Field-level masking
- **WHEN** a user without the right to see them views salaries, NRIC/FIN, full bank account numbers or personal addresses
- **THEN** those fields are masked

#### Scenario: Checked twice
- **WHEN** a request reaches data
- **THEN** its permissions are checked in the API layer
- **AND** checked again by database row-level security

#### Scenario: Full matrix
- **WHEN** the rights of each role over each capability are needed
- **THEN** they are those of the permission matrix (Appendix D)

### Requirement: Owner role
The Owner SHALL be able to do everything the Administrator can, plus manage the subscription, export the whole tenant's data, use break-glass and close the organisation.

Source: PRD-001 §6.2

#### Scenario: Owner rights
- **WHEN** the Owner acts
- **THEN** they can do everything in Administrator, manage the subscription, export the whole tenant's data, use break-glass and close the organisation

#### Scenario: Owner actions are dual-controlled
- **WHEN** the Owner takes any action
- **THEN** it is dual-controlled and audited, except founder break-glass within FM-8's limits
- **AND** founder break-glass is exempt from dual control only: it is always audited and reviewed (FM-8)
- **AND** no other Standard-mode restriction applies to the Owner

### Requirement: Administrator role
The Administrator SHALL manage users, roles, the team mode, approval policies, integrations and settings, and SHALL NOT post or approve financial transactions in Standard mode.

Source: PRD-001 §6.2

#### Scenario: Administrator rights
- **WHEN** the Administrator acts
- **THEN** they can manage users, roles, the team mode, approval policies, integrations and settings

#### Scenario: No financial transactions
- **GIVEN** Standard mode
- **WHEN** the Administrator tries to post or approve a financial transaction
- **THEN** it is refused

### Requirement: Finance controller role
The Finance controller / CFO SHALL approve journals, bills and payments above thresholds, close and reopen periods with a second approver, approve statutory parameter changes and sign off reconciliations, and SHALL NOT release payments they prepared in Standard mode.

Source: PRD-001 §6.2

#### Scenario: Finance controller rights
- **WHEN** the Finance controller acts
- **THEN** they can approve journals, bills and payments above thresholds, approve statutory parameter changes and sign off reconciliations

#### Scenario: Close and reopen with a second approver
- **WHEN** the Finance controller closes or reopens a period
- **THEN** a second approver is required

#### Scenario: No release of own payments
- **GIVEN** Standard mode
- **WHEN** the Finance controller tries to release payments they prepared
- **THEN** it is refused

### Requirement: Accountant role
The Accountant SHALL prepare and post journals within limits and do reconciliations, accruals, prepayments, FX revaluation, fixed assets and tax working papers, and SHALL NOT approve their own journals above the threshold or close periods in Standard mode.

Source: PRD-001 §6.2

#### Scenario: Accountant rights
- **WHEN** the Accountant acts
- **THEN** they can prepare and post journals within limits, and do reconciliations, accruals, prepayments, FX revaluation, fixed assets and tax working papers

#### Scenario: Accountant limits
- **GIVEN** Standard mode
- **WHEN** the Accountant tries to approve their own journal above the threshold, or to close a period
- **THEN** it is refused

### Requirement: Bookkeeper role
The Bookkeeper SHALL record transactions, categorise bank lines, draft invoices and bills, attach documents and manage bank rules, and SHALL NOT approve, post journals or change vendor bank details in Standard mode.

Source: PRD-001 §6.2

#### Scenario: Bookkeeper rights
- **WHEN** the Bookkeeper acts
- **THEN** they can record transactions, categorise bank lines, draft invoices and bills, attach documents and manage bank rules

#### Scenario: Bookkeeper limits
- **GIVEN** Standard mode
- **WHEN** the Bookkeeper tries to approve anything, post a journal or change vendor bank details
- **THEN** it is refused

### Requirement: AR specialist role
The AR specialist SHALL handle quotes, orders, invoices, credit notes, reminders and customer statements and apply receipts, and SHALL NOT write off receivables or approve credit notes above their limit in Standard mode.

Source: PRD-001 §6.2

#### Scenario: AR specialist rights
- **WHEN** the AR specialist acts
- **THEN** they can handle quotes, orders, invoices, credit notes, reminders and customer statements, and apply receipts

#### Scenario: AR specialist limits
- **GIVEN** Standard mode
- **WHEN** the AR specialist tries to write off a receivable or approve a credit note above their limit
- **THEN** it is refused

### Requirement: AP specialist role
The AP specialist SHALL handle purchase orders, bills and vendor credits and prepare payment batches, and SHALL NOT approve bills, release payments, or change vendor bank details without a second approver in Standard mode.

Source: PRD-001 §6.2

#### Scenario: AP specialist rights
- **WHEN** the AP specialist acts
- **THEN** they can handle purchase orders, bills and vendor credits, and prepare payment batches

#### Scenario: AP specialist limits
- **GIVEN** Standard mode
- **WHEN** the AP specialist tries to approve a bill, release a payment, or change vendor bank details without a second approver
- **THEN** it is refused

### Requirement: Payroll administrator role
The Payroll administrator SHALL handle employees, pay runs, CPF/SDL/SHG, payslips and IR8A, and SHALL NOT approve their own pay run or release salary payments in Standard mode.

Source: PRD-001 §6.2

#### Scenario: Payroll administrator rights
- **WHEN** the Payroll administrator acts
- **THEN** they can handle employees, pay runs, CPF/SDL/SHG, payslips and IR8A

#### Scenario: Payroll administrator limits
- **GIVEN** Standard mode
- **WHEN** the Payroll administrator tries to approve their own pay run or release salary payments
- **THEN** it is refused

### Requirement: Payment releaser role
The Payment releaser (treasury) SHALL release approved payment batches to the bank and manage bank connections, and SHALL NOT create or approve the payments they release in Standard mode.

Source: PRD-001 §6.2

#### Scenario: Payment releaser rights
- **WHEN** the Payment releaser acts
- **THEN** they can release approved payment batches to the bank and manage bank connections

#### Scenario: Payment releaser limits
- **GIVEN** Standard mode
- **WHEN** the Payment releaser tries to release a payment they created or approved
- **THEN** it is refused

### Requirement: Approver role
The Approver (budget holder) SHALL approve POs, bills and claims within their department and limits, and nothing outside their department or limit.

Source: PRD-001 §6.2

#### Scenario: Approver rights
- **WHEN** the Approver approves a PO, bill or claim of their department within their limit
- **THEN** the approval is accepted

#### Scenario: Approver limits
- **GIVEN** Standard mode
- **WHEN** the Approver tries to act outside their department or above their limit
- **THEN** it is refused

### Requirement: Employee role
The Employee SHALL submit expense claims and receipts and see their own payslips and claims, and SHALL NOT see anyone else's data.

Source: PRD-001 §6.2

#### Scenario: Employee rights
- **WHEN** the Employee acts
- **THEN** they can submit expense claims and receipts and see their own payslips and claims

#### Scenario: No one else's data
- **WHEN** the Employee tries to see another person's data
- **THEN** it is refused

### Requirement: Auditor role
The external Auditor SHALL read everything in scope, the audit trail and evidence, and export, through time-boxed access with an expiry date, and SHALL make no change.

Source: PRD-001 §6.2

#### Scenario: Auditor rights
- **WHEN** the Auditor acts within their access
- **THEN** they can read everything in scope, the audit trail and evidence, and export

#### Scenario: Time-boxed access
- **WHEN** the expiry date of the Auditor's access passes
- **THEN** the access ends

#### Scenario: Auditor limits
- **GIVEN** Standard mode
- **WHEN** the Auditor tries to make any change, or to see payroll personal data that was not explicitly granted
- **THEN** it is refused

### Requirement: Tax agent role
The Tax agent / external accountant SHALL have the Accountant's rights plus GST and tax working papers, through time-boxed access, and SHALL NOT administer users.

Source: PRD-001 §6.2

#### Scenario: Tax agent rights
- **WHEN** the Tax agent acts within their access
- **THEN** they have the Accountant's rights plus GST and tax working papers
- **AND** the access is time-boxed

#### Scenario: No user administration
- **GIVEN** Standard mode
- **WHEN** the Tax agent tries to administer users
- **THEN** it is refused

### Requirement: Board viewer role
The Board / investor viewer SHALL see the board pack, dashboards and the cap table as granted, and SHALL NOT see transaction-level data unless granted.

Source: PRD-001 §6.2

#### Scenario: Board viewer rights
- **WHEN** the Board viewer acts
- **THEN** they can see the board pack, dashboards and the cap table as granted

#### Scenario: No transaction-level data
- **GIVEN** Standard mode and no grant of transaction-level data
- **WHEN** the Board viewer tries to see transaction-level data
- **THEN** it is refused

### Requirement: Super admin role
The Super admin SHALL manage installation-wide settings (sign-in providers, email and messaging connections, announcements to everyone, platform health; FR-ADM-06/07), and SHALL NOT read an organisation's books without being a member or act alone once a second Super admin exists.

Source: PRD-001 §6.2

#### Scenario: Super admin rights
- **WHEN** the Super admin acts
- **THEN** they can manage sign-in providers, email and messaging connections, announcements to everyone and platform health

#### Scenario: Super admin limits
- **WHEN** the Super admin tries to read an organisation's books without being a member, or to act alone once a second Super admin exists
- **THEN** it is refused

### Requirement: Company secretary role
The Company secretary SHALL handle the corporate profile, registers, the equity register, the AI company secretary's runs and drafts, evidence and filing records (FR-CS-08), and SHALL NOT handle transactions, payroll or bank detail unless granted.

Source: PRD-001 §6.2

#### Scenario: Company secretary rights
- **WHEN** the Company secretary acts
- **THEN** they can handle the corporate profile, registers, the equity register, the AI company secretary's runs and drafts, evidence and filing records

#### Scenario: Company secretary limits
- **GIVEN** Standard mode and no grant
- **WHEN** the Company secretary tries to handle transactions, payroll or bank detail
- **THEN** it is refused

### Requirement: Compliance officer role
The Compliance officer / DPO SHALL handle the Compliance Center, control evidence, the audit trail, data-subject requests and retention holds, and SHALL NOT post or approve transactions.

Source: PRD-001 §6.2

#### Scenario: Compliance officer rights
- **WHEN** the Compliance officer acts
- **THEN** they can handle the Compliance Center, control evidence, the audit trail, data-subject requests and retention holds

#### Scenario: No transactions
- **GIVEN** Standard mode
- **WHEN** the Compliance officer tries to post or approve a transaction
- **THEN** it is refused

### Requirement: AI assistant principal
The AI assistant, a non-human principal, SHALL read only within the delegating user's permissions minus the AI exclusions (§9.4) and create proposals from the approved task catalogue, and SHALL NOT execute, approve or reject anything or hold a write credential.

Source: PRD-001 §6.2

#### Scenario: AI reads within the user's rights
- **WHEN** the AI assistant reads data for a user
- **THEN** it reads only what the delegating user may read, minus the AI exclusions (§9.4)

#### Scenario: AI proposes from the catalogue
- **WHEN** the AI assistant wants a change made
- **THEN** it can only create a proposal from the approved task catalogue

#### Scenario: AI never executes or decides
- **WHEN** the AI assistant tries to execute, approve or reject anything
- **THEN** it is refused
- **AND** it holds no write credential

### Requirement: Integration principal
An integration, a service principal, SHALL have narrow, scoped API rights for one integration (bank feed, Peppol access point, Stripe), and nothing outside its scope.

Source: PRD-001 §6.2

#### Scenario: Scoped integration
- **WHEN** an integration calls the API within its scope
- **THEN** the call is allowed

#### Scenario: Outside its scope
- **WHEN** an integration calls the API outside its scope
- **THEN** the call is refused

### Requirement: SOD-1 Vendor bank details and payments to that vendor
The same person SHALL NOT both create or change a vendor's bank details and approve or release payments to that vendor, to prevent payment fraud.

Source: PRD-001 §6.3

#### Scenario: Bank detail changer pays the vendor
- **GIVEN** the team mode blocks conflicts (§6.4)
- **WHEN** a user who created or changed a vendor's bank details tries to approve or release a payment to that vendor
- **THEN** it is blocked

### Requirement: SOD-2 Prepare and release a payment batch
The same person SHALL NOT both prepare a payment batch and release it, to prevent payment fraud.

Source: PRD-001 §6.3

#### Scenario: Preparer releases the batch
- **GIVEN** the team mode blocks conflicts (§6.4)
- **WHEN** the user who prepared a payment batch tries to release it
- **THEN** it is blocked

### Requirement: SOD-3 Prepare and approve a journal above the threshold
The same person SHALL NOT both prepare a journal above the threshold and approve it, to prevent misstatement.

Source: PRD-001 §6.3

#### Scenario: Preparer approves the journal
- **GIVEN** the team mode blocks conflicts (§6.4)
- **WHEN** the user who prepared a journal above the threshold tries to approve it
- **THEN** it is blocked

### Requirement: SOD-4 Prepare a pay run and approve or pay it
The same person SHALL NOT both prepare a pay run and approve it or release salary payments, to prevent payroll fraud.

Source: PRD-001 §6.3

#### Scenario: Preparer approves or pays the pay run
- **GIVEN** the team mode blocks conflicts (§6.4)
- **WHEN** the user who prepared a pay run tries to approve it or release its salary payments
- **THEN** it is blocked

### Requirement: SOD-5 Record bank transactions and sign off the reconciliation
The same person SHALL NOT both record bank transactions and sign off the bank reconciliation, to prevent concealment.

Source: PRD-001 §6.3

#### Scenario: Recorder signs off the reconciliation
- **GIVEN** the team mode blocks conflicts (§6.4)
- **WHEN** a user who recorded bank transactions tries to sign off that bank reconciliation
- **THEN** it is blocked

### Requirement: SOD-6 Administer users and post or approve transactions
The same person SHALL NOT both administer users or roles and post or approve transactions, to prevent privilege abuse.

Source: PRD-001 §6.3

#### Scenario: Administrator posts or approves
- **GIVEN** the team mode blocks conflicts (§6.4)
- **WHEN** a user who administers users or roles tries to post or approve a transaction
- **THEN** it is blocked

### Requirement: SOD-7 Close a period and reopen it alone
The same person SHALL NOT both close a period and reopen it alone, to prevent tampering with reported results.

Source: PRD-001 §6.3

#### Scenario: Closer reopens alone
- **GIVEN** the team mode blocks conflicts (§6.4)
- **WHEN** the user who closed a period tries to reopen it without anyone else
- **THEN** it is blocked

### Requirement: SOD-8 Propose and approve a control change
The same person SHALL NOT both propose a change to the AI task catalogue or approval policies and approve it, to prevent weakening controls.

Source: PRD-001 §6.3

#### Scenario: Proposer approves the change
- **WHEN** the user who proposed a change to the AI task catalogue or an approval policy tries to approve it
- **THEN** it is blocked

### Requirement: SOD-9 Write off a receivable and receive cash from that customer
The same person SHALL NOT both write off a receivable and receive cash from that customer, to prevent teeming and lading.

Source: PRD-001 §6.3

#### Scenario: Write-off and receipt by one person
- **GIVEN** the team mode blocks conflicts (§6.4)
- **WHEN** a user who wrote off a customer's receivable tries to receive cash from that customer, or the reverse
- **THEN** it is blocked

### Requirement: SOD-10 Change and approve statutory parameters
The same person SHALL NOT both change statutory parameters (GST, CPF rates) and approve the change, to prevent silent mis-statement.

Source: PRD-001 §6.3

#### Scenario: Changer approves the parameter change
- **GIVEN** the team mode blocks conflicts (§6.4)
- **WHEN** the user who changed a statutory parameter tries to approve that change
- **THEN** it is blocked

### Requirement: Permission matrix legend
The permission matrix SHALL describe Standard mode with the rights C (create/prepare), A (approve), P (post/execute), R (release, money movement), V (view), M (view masked) and — (none), and the AI SHALL act on behalf of a user and never exceed that user's rights.

Source: PRD-001 §D

#### Scenario: Founder mode
- **GIVEN** an organisation in Founder mode
- **WHEN** rights are checked
- **THEN** the same people may hold several roles
- **AND** the FM controls of §6.4 apply

#### Scenario: AI never exceeds the user
- **WHEN** the AI acts on behalf of a user
- **THEN** it never has more rights than that user, whatever the matrix lists for the AI

#### Scenario: Masked view
- **WHEN** a role with M views a capability
- **THEN** it sees the data with sensitive fields masked

#### Scenario: Two letters in a cell
- **GIVEN** a role whose cell reads "C/A"
- **WHEN** it prepares an item
- **THEN** it may not also approve that item: SOD-1…10 apply per item
- **AND** a Controller who changes a statutory parameter cannot approve that change (SOD-10)

#### Scenario: The Owner holds both sides of SOD-6
- **WHEN** the Owner acts on users, roles, approval policies or bank details
- **THEN** the action is dual-controlled (§6.2), so no single Owner grants a right and uses it unreviewed
- **AND** the FM-3 weekly review covers the rest

#### Scenario: Integration clients are not roles
- **WHEN** an integration client (FR-INT-02) acts
- **THEN** it acts only within the scopes the Owner granted
- **AND** never beyond the user or service account behind it (AI-34)

### Requirement: Permission matrix: Users, roles, team mode
In Standard mode, each role SHALL hold only the rights over users, roles and the team mode that the permission matrix gives it.

Source: PRD-001 §D

#### Scenario: Approve
- **WHEN** a role acts on users, roles and the team mode
- **THEN** the Owner can approve (A)

#### Scenario: Create or prepare
- **WHEN** a role acts on users, roles and the team mode
- **THEN** the Administrator can create or prepare (C)

#### Scenario: View
- **WHEN** a role acts on users, roles and the team mode
- **THEN** the Finance controller, the Auditor and the Compliance officer / DPO can view (V)

#### Scenario: No access
- **WHEN** a role acts on users, roles and the team mode
- **THEN** the Accountant, the Bookkeeper, the AR specialist, the AP specialist, the Payroll administrator, the Payment releaser, the Approver, the Employee, the Tax agent, the Board viewer and the AI assistant have no right over it (—)

### Requirement: Permission matrix: Approval policies, AI catalogue
In Standard mode, each role SHALL hold only the rights over approval policies and the AI task catalogue that the permission matrix gives it.

Source: PRD-001 §D

#### Scenario: Approve
- **WHEN** a role acts on approval policies and the AI task catalogue
- **THEN** the Owner and the Finance controller can approve (A)

#### Scenario: Create or prepare
- **WHEN** a role acts on approval policies and the AI task catalogue
- **THEN** the Administrator can create or prepare (C)

#### Scenario: View
- **WHEN** a role acts on approval policies and the AI task catalogue
- **THEN** the Accountant, the Auditor and the Compliance officer / DPO can view (V)

#### Scenario: No access
- **WHEN** a role acts on approval policies and the AI task catalogue
- **THEN** the Bookkeeper, the AR specialist, the AP specialist, the Payroll administrator, the Payment releaser, the Approver, the Employee, the Tax agent, the Board viewer and the AI assistant have no right over it (—)

### Requirement: Permission matrix: Statutory parameters
In Standard mode, each role SHALL hold only the rights over statutory parameters that the permission matrix gives it.

Source: PRD-001 §D

#### Scenario: Approve
- **WHEN** a role acts on statutory parameters
- **THEN** the Owner can approve (A)

#### Scenario: Create and approve
- **WHEN** a role acts on statutory parameters
- **THEN** the Finance controller can create or prepare and approve (C/A)

#### Scenario: Create or prepare
- **WHEN** a role acts on statutory parameters
- **THEN** the Accountant and the Tax agent can create or prepare (C)

#### Scenario: View
- **WHEN** a role acts on statutory parameters
- **THEN** the Payroll administrator, the Auditor and the AI assistant can view (V)

#### Scenario: No access
- **WHEN** a role acts on statutory parameters
- **THEN** the Administrator, the Bookkeeper, the AR specialist, the AP specialist, the Payment releaser, the Approver, the Employee, the Board viewer and the Compliance officer / DPO have no right over it (—)

### Requirement: Permission matrix: Chart of accounts, dimensions
In Standard mode, each role SHALL hold only the rights over the chart of accounts and dimensions that the permission matrix gives it.

Source: PRD-001 §D

#### Scenario: Approve
- **WHEN** a role acts on the chart of accounts and dimensions
- **THEN** the Owner and the Finance controller can approve (A)

#### Scenario: Create or prepare
- **WHEN** a role acts on the chart of accounts and dimensions
- **THEN** the Accountant and the Tax agent can create or prepare (C)

#### Scenario: View
- **WHEN** a role acts on the chart of accounts and dimensions
- **THEN** the Bookkeeper, the AR specialist, the AP specialist, the Payroll administrator, the Payment releaser, the Approver, the Auditor and the AI assistant can view (V)

#### Scenario: No access
- **WHEN** a role acts on the chart of accounts and dimensions
- **THEN** the Administrator, the Employee, the Board viewer and the Compliance officer / DPO have no right over it (—)

### Requirement: Permission matrix: Bank transactions, categorise
In Standard mode, each role SHALL hold only the rights over bank transactions and their categorisation that the permission matrix gives it.

Source: PRD-001 §D

#### Scenario: View
- **WHEN** a role acts on bank transactions and their categorisation
- **THEN** the Owner, the AR specialist, the AP specialist, the Payment releaser, the Auditor and the Tax agent can view (V)

#### Scenario: Approve
- **WHEN** a role acts on bank transactions and their categorisation
- **THEN** the Finance controller can approve (A)

#### Scenario: Create and post
- **WHEN** a role acts on bank transactions and their categorisation
- **THEN** the Accountant and the Bookkeeper can create or prepare and post (C/P)

#### Scenario: AI proposals
- **WHEN** a role acts on bank transactions and their categorisation
- **THEN** the AI assistant can only create a proposal (Proposal)

#### Scenario: No access
- **WHEN** a role acts on bank transactions and their categorisation
- **THEN** the Administrator, the Payroll administrator, the Approver, the Employee, the Board viewer and the Compliance officer / DPO have no right over it (—)

### Requirement: Permission matrix: Bank reconciliation sign-off
In Standard mode, each role SHALL hold only the rights over the bank reconciliation sign-off that the permission matrix gives it.

Source: PRD-001 §D

#### Scenario: View
- **WHEN** a role acts on the bank reconciliation sign-off
- **THEN** the Owner, the Auditor and the Tax agent can view (V)

#### Scenario: Approve
- **WHEN** a role acts on the bank reconciliation sign-off
- **THEN** the Finance controller can approve (A)

#### Scenario: Create or prepare
- **WHEN** a role acts on the bank reconciliation sign-off
- **THEN** the Accountant and the Bookkeeper can create or prepare (C)

#### Scenario: No access
- **WHEN** a role acts on the bank reconciliation sign-off
- **THEN** the Administrator, the AR specialist, the AP specialist, the Payroll administrator, the Payment releaser, the Approver, the Employee, the Board viewer, the Compliance officer / DPO and the AI assistant have no right over it (—)

### Requirement: Permission matrix: Invoices, credit notes
In Standard mode, each role SHALL hold only the rights over invoices and credit notes that the permission matrix gives it.

Source: PRD-001 §D

#### Scenario: View
- **WHEN** a role acts on invoices and credit notes
- **THEN** the Owner, the Auditor and the Tax agent can view (V)

#### Scenario: Approve
- **WHEN** a role acts on invoices and credit notes
- **THEN** the Finance controller can approve (A)

#### Scenario: Create and post
- **WHEN** a role acts on invoices and credit notes
- **THEN** the Accountant and the AR specialist can create or prepare and post (C/P)

#### Scenario: Create or prepare
- **WHEN** a role acts on invoices and credit notes
- **THEN** the Bookkeeper can create or prepare (C)

#### Scenario: Approve within a limit
- **WHEN** a role acts on invoices and credit notes
- **THEN** the Approver can approve within their limit (A, limit)

#### Scenario: AI proposals
- **WHEN** a role acts on invoices and credit notes
- **THEN** the AI assistant can only create a proposal (Proposal)

#### Scenario: No access
- **WHEN** a role acts on invoices and credit notes
- **THEN** the Administrator, the AP specialist, the Payroll administrator, the Payment releaser, the Employee, the Board viewer and the Compliance officer / DPO have no right over it (—)

### Requirement: Permission matrix: Bills, vendor credits
In Standard mode, each role SHALL hold only the rights over bills and vendor credits that the permission matrix gives it.

Source: PRD-001 §D

#### Scenario: View
- **WHEN** a role acts on bills and vendor credits
- **THEN** the Owner, the Auditor and the Tax agent can view (V)

#### Scenario: Approve
- **WHEN** a role acts on bills and vendor credits
- **THEN** the Finance controller can approve (A)

#### Scenario: Create and post
- **WHEN** a role acts on bills and vendor credits
- **THEN** the Accountant can create or prepare and post (C/P)

#### Scenario: Create or prepare
- **WHEN** a role acts on bills and vendor credits
- **THEN** the Bookkeeper and the AP specialist can create or prepare (C)

#### Scenario: Approve within a limit
- **WHEN** a role acts on bills and vendor credits
- **THEN** the Approver can approve within their limit (A, limit)

#### Scenario: AI proposals
- **WHEN** a role acts on bills and vendor credits
- **THEN** the AI assistant can only create a proposal (Proposal)

#### Scenario: No access
- **WHEN** a role acts on bills and vendor credits
- **THEN** the Administrator, the AR specialist, the Payroll administrator, the Payment releaser, the Employee, the Board viewer and the Compliance officer / DPO have no right over it (—)

### Requirement: Permission matrix: Vendor bank details
In Standard mode, each role SHALL hold only the rights over vendor bank details that the permission matrix gives it.

Source: PRD-001 §D

#### Scenario: Approve
- **WHEN** a role acts on vendor bank details
- **THEN** the Owner and the Finance controller can approve (A)

#### Scenario: Create or prepare
- **WHEN** a role acts on vendor bank details
- **THEN** the Accountant and the AP specialist can create or prepare (C)

#### Scenario: View masked
- **WHEN** a role acts on vendor bank details
- **THEN** the Auditor and the Tax agent can view masked values only (M)

#### Scenario: No access
- **WHEN** a role acts on vendor bank details
- **THEN** the Administrator, the Bookkeeper, the AR specialist, the Payroll administrator, the Payment releaser, the Approver, the Employee, the Board viewer, the Compliance officer / DPO and the AI assistant have no right over it (—)

### Requirement: Permission matrix: Payment batches
In Standard mode, each role SHALL hold only the rights over payment batches that the permission matrix gives it.

Source: PRD-001 §D

#### Scenario: View
- **WHEN** a role acts on payment batches
- **THEN** the Owner, the Auditor and the Tax agent can view (V)

#### Scenario: Approve
- **WHEN** a role acts on payment batches
- **THEN** the Finance controller can approve (A)

#### Scenario: Create or prepare
- **WHEN** a role acts on payment batches
- **THEN** the Accountant and the AP specialist can create or prepare (C)

#### Scenario: Prepare salary payments
- **WHEN** a role acts on payment batches
- **THEN** the Payroll administrator can create or prepare salary payments (C, salaries)

#### Scenario: Release
- **WHEN** a role acts on payment batches
- **THEN** the Payment releaser can release (R)

#### Scenario: No access
- **WHEN** a role acts on payment batches
- **THEN** the Administrator, the Bookkeeper, the AR specialist, the Approver, the Employee, the Board viewer, the Compliance officer / DPO and the AI assistant have no right over it (—)

### Requirement: Permission matrix: Expense claims
In Standard mode, each role SHALL hold only the rights over expense claims that the permission matrix gives it.

Source: PRD-001 §D

#### Scenario: View
- **WHEN** a role acts on expense claims
- **THEN** the Owner, the Accountant, the Bookkeeper, the AP specialist, the Auditor and the Tax agent can view (V)

#### Scenario: Approve
- **WHEN** a role acts on expense claims
- **THEN** the Finance controller and the Approver can approve (A)

#### Scenario: Own items
- **WHEN** a role acts on expense claims
- **THEN** the Employee can create their own only (C, own)

#### Scenario: AI drafts
- **WHEN** a role acts on expense claims
- **THEN** the AI assistant can only propose a draft (Proposal, draft)

#### Scenario: No access
- **WHEN** a role acts on expense claims
- **THEN** the Administrator, the AR specialist, the Payroll administrator, the Payment releaser, the Board viewer and the Compliance officer / DPO have no right over it (—)

### Requirement: Permission matrix: Director accounts, settlements, share issues
In Standard mode, each role SHALL hold only the rights over director accounts, settlements and share issues that the permission matrix gives it.

Source: PRD-001 §D

#### Scenario: Approve
- **WHEN** a role acts on director accounts, settlements and share issues
- **THEN** the Owner and the Finance controller can approve (A)

#### Scenario: Create and post
- **WHEN** a role acts on director accounts, settlements and share issues
- **THEN** the Accountant can create or prepare and post (C/P)

#### Scenario: View
- **WHEN** a role acts on director accounts, settlements and share issues
- **THEN** the Bookkeeper, the Auditor and the Tax agent can view (V)

#### Scenario: View deferred pay
- **WHEN** a role acts on director accounts, settlements and share issues
- **THEN** the Payroll administrator can view deferred pay (V, deferred pay)

#### Scenario: Release repayments
- **WHEN** a role acts on director accounts, settlements and share issues
- **THEN** the Payment releaser can release repayments (R, repayments)

#### Scenario: View own
- **WHEN** a role acts on director accounts, settlements and share issues
- **THEN** the Employee can view their own only (V, own)

#### Scenario: View share issues
- **WHEN** a role acts on director accounts, settlements and share issues
- **THEN** the Board viewer can view share issues (V, share issues)

#### Scenario: Ask only
- **WHEN** a role acts on director accounts, settlements and share issues
- **THEN** the AI assistant can only answer questions about it (ask only)

#### Scenario: No access
- **WHEN** a role acts on director accounts, settlements and share issues
- **THEN** the Administrator, the AR specialist, the AP specialist, the Approver and the Compliance officer / DPO have no right over it (—)

### Requirement: Permission matrix: Cap table, holders, investors, equity transactions
In Standard mode, each role SHALL hold only the rights over the cap table, holders, investors and equity transactions that the permission matrix gives it.

Source: PRD-001 §D

#### Scenario: Approve
- **WHEN** a role acts on the cap table, holders, investors and equity transactions
- **THEN** the Owner and the Finance controller can approve (A)

#### Scenario: Create and post
- **WHEN** a role acts on the cap table, holders, investors and equity transactions
- **THEN** the Accountant can create or prepare and post (C/P)

#### Scenario: View own grants
- **WHEN** a role acts on the cap table, holders, investors and equity transactions
- **THEN** the Employee can view their own grants (V, own grants)

#### Scenario: View
- **WHEN** a role acts on the cap table, holders, investors and equity transactions
- **THEN** the Auditor and the Tax agent can view (V)

#### Scenario: View own holding
- **WHEN** a role acts on the cap table, holders, investors and equity transactions
- **THEN** the Board viewer can view their own holding, and all holdings if granted (V)

#### Scenario: View masked
- **WHEN** a role acts on the cap table, holders, investors and equity transactions
- **THEN** the Compliance officer / DPO can view masked values only (M)

#### Scenario: AI scenarios only
- **WHEN** a role acts on the cap table, holders, investors and equity transactions
- **THEN** the AI assistant can only run scenarios, and only if `fyniq.read.equity` is granted

#### Scenario: No access
- **WHEN** a role acts on the cap table, holders, investors and equity transactions
- **THEN** the Administrator, the Bookkeeper, the AR specialist, the AP specialist, the Payroll administrator, the Payment releaser and the Approver have no right over it (—)

### Requirement: Permission matrix: Evidence: add, supersede
In Standard mode, each role SHALL hold only the rights over evidence (adding and superseding it) that the permission matrix gives it.

Source: PRD-001 §D

#### Scenario: Add and supersede
- **WHEN** a role acts on evidence (adding and superseding it)
- **THEN** the Owner, the Finance controller, the Accountant, the Bookkeeper, the AR specialist, the AP specialist, the Payment releaser, the Approver and the Tax agent can add and supersede evidence (C)

#### Scenario: Payroll evidence
- **WHEN** a role acts on evidence (adding and superseding it)
- **THEN** the Payroll administrator can add and supersede payroll evidence (C, payroll)

#### Scenario: Own items
- **WHEN** a role acts on evidence (adding and superseding it)
- **THEN** the Employee can add and supersede their own evidence only (C, own)

#### Scenario: View
- **WHEN** a role acts on evidence (adding and superseding it)
- **THEN** the Auditor and the Compliance officer / DPO can view (V)

#### Scenario: AI proposal to attach
- **WHEN** a role acts on evidence (adding and superseding it)
- **THEN** the AI assistant can only propose attaching evidence (Proposal, `doc.attach`)

#### Scenario: No access
- **WHEN** a role acts on evidence (adding and superseding it)
- **THEN** the Administrator and the Board viewer have no right over it (—)

### Requirement: Permission matrix: Knowledge base: compiled pages and index
In Standard mode, each role SHALL hold only the rights over the knowledge base's compiled pages and index that the permission matrix gives it.

Source: PRD-001 §D

#### Scenario: View, edit and publish
- **WHEN** a role acts on the knowledge base's compiled pages and index
- **THEN** the Owner and the Administrator can view, edit and publish (V/C)

#### Scenario: View
- **WHEN** a role acts on the knowledge base's compiled pages and index
- **THEN** the Auditor can view (V)

#### Scenario: AI reads
- **WHEN** a role acts on the knowledge base's compiled pages and index
- **THEN** the AI assistant reads it filtered to the user (AI-54)

#### Scenario: No access
- **WHEN** a role acts on the knowledge base's compiled pages and index
- **THEN** the Finance controller, the Accountant, the Bookkeeper, the AR specialist, the AP specialist, the Payroll administrator, the Payment releaser, the Approver, the Employee, the Tax agent, the Board viewer and the Compliance officer / DPO have no right over it (—)

### Requirement: Permission matrix: AI company secretary: enable, schedule, runs and drafts
In Standard mode, each role SHALL hold only the rights over the AI company secretary (enabling, scheduling, runs and drafts) that the permission matrix gives it.

Source: PRD-001 §D

#### Scenario: Enable and schedule
- **WHEN** a role acts on the AI company secretary (enabling, scheduling, runs and drafts)
- **THEN** the Owner can approve enabling it with a second approver, and set the schedule (A + second approver; C, schedule)

#### Scenario: Set the schedule
- **WHEN** a role acts on the AI company secretary (enabling, scheduling, runs and drafts)
- **THEN** the Administrator can set the schedule (C, schedule)

#### Scenario: View
- **WHEN** a role acts on the AI company secretary (enabling, scheduling, runs and drafts)
- **THEN** the Finance controller, the Accountant, the Auditor and the Tax agent can view (V)

#### Scenario: View summaries
- **WHEN** a role acts on the AI company secretary (enabling, scheduling, runs and drafts)
- **THEN** the Board viewer can view summaries (V, summaries)

#### Scenario: AI runs as the agent principal
- **WHEN** a role acts on the AI company secretary (enabling, scheduling, runs and drafts)
- **THEN** the AI assistant runs as the agent principal (AI-63)

#### Scenario: No access
- **WHEN** a role acts on the AI company secretary (enabling, scheduling, runs and drafts)
- **THEN** the Bookkeeper, the AR specialist, the AP specialist, the Payroll administrator, the Payment releaser, the Approver, the Employee and the Compliance officer / DPO have no right over it (—)

### Requirement: Permission matrix: Organisation integrations: email senders, Slack, Zoho Cliq, inbound capture
In Standard mode, each role SHALL hold only the rights over organisation integrations (email senders, Slack, Zoho Cliq, inbound capture) that the permission matrix gives it.

Source: PRD-001 §D

#### Scenario: Approve with a second approver
- **WHEN** a role acts on organisation integrations (email senders, Slack, Zoho Cliq, inbound capture)
- **THEN** the Owner can approve, with a second approver (A + second approver)

#### Scenario: Create or prepare
- **WHEN** a role acts on organisation integrations (email senders, Slack, Zoho Cliq, inbound capture)
- **THEN** the Administrator can create or prepare (C)

#### Scenario: View
- **WHEN** a role acts on organisation integrations (email senders, Slack, Zoho Cliq, inbound capture)
- **THEN** the Finance controller, the Auditor and the Compliance officer / DPO can view (V)

#### Scenario: No access
- **WHEN** a role acts on organisation integrations (email senders, Slack, Zoho Cliq, inbound capture)
- **THEN** the Accountant, the Bookkeeper, the AR specialist, the AP specialist, the Payroll administrator, the Payment releaser, the Approver, the Employee, the Tax agent, the Board viewer and the AI assistant have no right over it (—)

### Requirement: Permission matrix: Announcements to the organisation
In Standard mode, each role SHALL hold only the rights over announcements to the organisation that the permission matrix gives it.

Source: PRD-001 §D

#### Scenario: Create and approve
- **WHEN** a role acts on announcements to the organisation
- **THEN** the Owner can create or prepare and approve (C/A)

#### Scenario: Create or prepare
- **WHEN** a role acts on announcements to the organisation
- **THEN** the Administrator can create or prepare (C)

#### Scenario: View
- **WHEN** a role acts on announcements to the organisation
- **THEN** the Auditor can view (V)

#### Scenario: No access
- **WHEN** a role acts on announcements to the organisation
- **THEN** the Finance controller, the Accountant, the Bookkeeper, the AR specialist, the AP specialist, the Payroll administrator, the Payment releaser, the Approver, the Employee, the Tax agent, the Board viewer, the Compliance officer / DPO and the AI assistant have no right over it (—)

### Requirement: Permission matrix: Own notifications and preferences
In Standard mode, each role SHALL hold only the rights over their own notifications and preferences that the permission matrix gives it.

Source: PRD-001 §D

#### Scenario: Create or prepare
- **WHEN** a user manages their own notifications and preferences
- **THEN** the Owner, the Administrator, the Finance controller, the Accountant, the Bookkeeper, the AR specialist, the AP specialist, the Payroll administrator, the Payment releaser, the Approver, the Employee, the Auditor, the Tax agent, the Board viewer and the Compliance officer / DPO can create or prepare (C)

#### Scenario: No access
- **WHEN** a user manages their own notifications and preferences
- **THEN** the AI assistant have no right over it (—)

### Requirement: Permission matrix: Document templates: edit, publish
In Standard mode, each role SHALL hold only the rights over document templates (editing and publishing them) that the permission matrix gives it.

Source: PRD-001 §D

#### Scenario: Approve
- **WHEN** a role acts on document templates (editing and publishing them)
- **THEN** the Owner and the Finance controller can approve (A)

#### Scenario: Create or prepare
- **WHEN** a role acts on document templates (editing and publishing them)
- **THEN** the Accountant, the AR specialist and the AP specialist can create or prepare (C)

#### Scenario: View
- **WHEN** a role acts on document templates (editing and publishing them)
- **THEN** the Auditor can view (V)

#### Scenario: No access
- **WHEN** a role acts on document templates (editing and publishing them)
- **THEN** the Administrator, the Bookkeeper, the Payroll administrator, the Payment releaser, the Approver, the Employee, the Tax agent, the Board viewer, the Compliance officer / DPO and the AI assistant have no right over it (—)

### Requirement: Permission matrix: Timesheets and project time
In Standard mode, each role SHALL hold only the rights over timesheets and project time that the permission matrix gives it.

Source: PRD-001 §D

#### Scenario: View
- **WHEN** a role acts on timesheets and project time
- **THEN** the Owner, the Accountant, the AR specialist, the Auditor and the Tax agent can view (V)

#### Scenario: Approve
- **WHEN** a role acts on timesheets and project time
- **THEN** the Finance controller can approve (A)

#### Scenario: Approve as project manager
- **WHEN** a role acts on timesheets and project time
- **THEN** the Approver can approve as project manager (A, project manager)

#### Scenario: Own items
- **WHEN** a role acts on timesheets and project time
- **THEN** the Employee can create their own only (C, own)

#### Scenario: AI draft invoice
- **WHEN** a role acts on timesheets and project time
- **THEN** the AI assistant can only propose a draft invoice (Proposal, draft invoice)

#### Scenario: No access
- **WHEN** a role acts on timesheets and project time
- **THEN** the Administrator, the Bookkeeper, the AP specialist, the Payroll administrator, the Payment releaser, the Board viewer and the Compliance officer / DPO have no right over it (—)

### Requirement: Permission matrix: Project reports and cost rates
In Standard mode, each role SHALL hold only the rights over project reports and cost rates that the permission matrix gives it.

Source: PRD-001 §D

#### Scenario: View
- **WHEN** a role acts on project reports and cost rates
- **THEN** the Owner and the Payroll administrator can view (V)

#### Scenario: View with role-average rates
- **WHEN** a role acts on project reports and cost rates
- **THEN** the Finance controller, the Accountant, the Auditor and the Tax agent can view, with role-average cost rates (V)

#### Scenario: View revenue and hours
- **WHEN** a role acts on project reports and cost rates
- **THEN** the AR specialist can view revenue and hours (V)

#### Scenario: View own projects
- **WHEN** a role acts on project reports and cost rates
- **THEN** the Approver can view their own projects, with role-average cost rates (V)

#### Scenario: AI same as the user
- **WHEN** a role acts on project reports and cost rates
- **THEN** the AI assistant has the same rights as the user, with person-level rates only with payroll access (FR-PAY-13, AI-02)

#### Scenario: No access
- **WHEN** a role acts on project reports and cost rates
- **THEN** the Administrator, the Bookkeeper, the AP specialist, the Payment releaser, the Employee, the Board viewer and the Compliance officer / DPO have no right over it (—)

### Requirement: Permission matrix: Bank data, payment-provider and ticketing connections
In Standard mode, each role SHALL hold only the rights over bank data, payment-provider and ticketing connections that the permission matrix gives it.

Source: PRD-001 §D

#### Scenario: Approve with a second approver
- **WHEN** a role acts on bank data, payment-provider and ticketing connections
- **THEN** the Owner can approve, with a second approver (A + second approver)

#### Scenario: Create or prepare
- **WHEN** a role acts on bank data, payment-provider and ticketing connections
- **THEN** the Administrator can create or prepare (C)

#### Scenario: View
- **WHEN** a role acts on bank data, payment-provider and ticketing connections
- **THEN** the Finance controller, the Auditor and the Compliance officer / DPO can view (V)

#### Scenario: No access
- **WHEN** a role acts on bank data, payment-provider and ticketing connections
- **THEN** the Accountant, the Bookkeeper, the AR specialist, the AP specialist, the Payroll administrator, the Payment releaser, the Approver, the Employee, the Tax agent, the Board viewer and the AI assistant have no right over it (—)

### Requirement: Permission matrix: Journals
In Standard mode, each role SHALL hold only the rights over journals that the permission matrix gives it.

Source: PRD-001 §D

#### Scenario: View
- **WHEN** a role acts on journals
- **THEN** the Owner and the Auditor can view (V)

#### Scenario: Approve
- **WHEN** a role acts on journals
- **THEN** the Finance controller can approve (A)

#### Scenario: Create and post within a limit
- **WHEN** a role acts on journals
- **THEN** the Accountant can create or prepare and post within their limit (C/P, limit)

#### Scenario: Create or prepare
- **WHEN** a role acts on journals
- **THEN** the Tax agent can create or prepare (C)

#### Scenario: AI proposals
- **WHEN** a role acts on journals
- **THEN** the AI assistant can only create a proposal (Proposal)

#### Scenario: No access
- **WHEN** a role acts on journals
- **THEN** the Administrator, the Bookkeeper, the AR specialist, the AP specialist, the Payroll administrator, the Payment releaser, the Approver, the Employee, the Board viewer and the Compliance officer / DPO have no right over it (—)

### Requirement: Permission matrix: Pay runs, employee data
In Standard mode, each role SHALL hold only the rights over pay runs and employee data that the permission matrix gives it.

Source: PRD-001 §D

#### Scenario: Approve
- **WHEN** a role acts on pay runs and employee data
- **THEN** the Owner can approve (A)

#### Scenario: Controller view
- **WHEN** a role acts on pay runs and employee data
- **THEN** the Finance controller can view masked values, or view in full if granted (M; V if granted, FR-PAY-13)

#### Scenario: View masked
- **WHEN** a role acts on pay runs and employee data
- **THEN** the Accountant, the Auditor, the Tax agent and the Compliance officer / DPO can view masked values only (M)

#### Scenario: Create or prepare
- **WHEN** a role acts on pay runs and employee data
- **THEN** the Payroll administrator can create or prepare (C)

#### Scenario: Release salaries
- **WHEN** a role acts on pay runs and employee data
- **THEN** the Payment releaser can release salaries (R, salaries)

#### Scenario: View own
- **WHEN** a role acts on pay runs and employee data
- **THEN** the Employee can view their own only (V, own)

#### Scenario: AI only if enabled
- **WHEN** a role acts on pay runs and employee data
- **THEN** the AI assistant has access only if enabled (AI-02)

#### Scenario: No access
- **WHEN** a role acts on pay runs and employee data
- **THEN** the Administrator, the Bookkeeper, the AR specialist, the AP specialist, the Approver and the Board viewer have no right over it (—)

### Requirement: Permission matrix: Fixed assets
In Standard mode, each role SHALL hold only the rights over fixed assets that the permission matrix gives it.

Source: PRD-001 §D

#### Scenario: View
- **WHEN** a role acts on fixed assets
- **THEN** the Owner, the Bookkeeper, the Auditor and the Tax agent can view (V)

#### Scenario: Approve
- **WHEN** a role acts on fixed assets
- **THEN** the Finance controller can approve (A)

#### Scenario: Create and post
- **WHEN** a role acts on fixed assets
- **THEN** the Accountant can create or prepare and post (C/P)

#### Scenario: No access
- **WHEN** a role acts on fixed assets
- **THEN** the Administrator, the AR specialist, the AP specialist, the Payroll administrator, the Payment releaser, the Approver, the Employee, the Board viewer, the Compliance officer / DPO and the AI assistant have no right over it (—)

### Requirement: Permission matrix: Period close / reopen
In Standard mode, each role SHALL hold only the rights over period close and reopen that the permission matrix gives it.

Source: PRD-001 §D

#### Scenario: Approve
- **WHEN** a role acts on period close and reopen
- **THEN** the Owner can approve (A)

#### Scenario: Create and approve
- **WHEN** a role acts on period close and reopen
- **THEN** the Finance controller can create or prepare and approve (C/A)

#### Scenario: Create or prepare
- **WHEN** a role acts on period close and reopen
- **THEN** the Accountant can create or prepare (C)

#### Scenario: View
- **WHEN** a role acts on period close and reopen
- **THEN** the Auditor and the Tax agent can view (V)

#### Scenario: No access
- **WHEN** a role acts on period close and reopen
- **THEN** the Administrator, the Bookkeeper, the AR specialist, the AP specialist, the Payroll administrator, the Payment releaser, the Approver, the Employee, the Board viewer, the Compliance officer / DPO and the AI assistant have no right over it (—)

### Requirement: Permission matrix: GST return, filing
In Standard mode, each role SHALL hold only the rights over the GST return and its filing that the permission matrix gives it.

Source: PRD-001 §D

#### Scenario: Approve
- **WHEN** a role acts on the GST return and its filing
- **THEN** the Owner and the Finance controller can approve (A)

#### Scenario: Create or prepare
- **WHEN** a role acts on the GST return and its filing
- **THEN** the Accountant and the Tax agent can create or prepare (C)

#### Scenario: View
- **WHEN** a role acts on the GST return and its filing
- **THEN** the Auditor can view (V)

#### Scenario: Ask only
- **WHEN** a role acts on the GST return and its filing
- **THEN** the AI assistant can only answer questions about it (ask only)

#### Scenario: No access
- **WHEN** a role acts on the GST return and its filing
- **THEN** the Administrator, the Bookkeeper, the AR specialist, the AP specialist, the Payroll administrator, the Payment releaser, the Approver, the Employee, the Board viewer and the Compliance officer / DPO have no right over it (—)

### Requirement: Permission matrix: Reports, dashboards
In Standard mode, each role SHALL hold only the rights over reports and dashboards that the permission matrix gives it.

Source: PRD-001 §D

#### Scenario: View
- **WHEN** a role acts on reports and dashboards
- **THEN** the Owner, the Administrator, the Finance controller, the Accountant, the Bookkeeper, the Payment releaser, the Auditor, the Tax agent, the Compliance officer / DPO and the AI assistant can view (V)

#### Scenario: View sales
- **WHEN** a role acts on reports and dashboards
- **THEN** the AR specialist can view sales reports (V, sales)

#### Scenario: View purchases
- **WHEN** a role acts on reports and dashboards
- **THEN** the AP specialist can view purchases reports (V, purchases)

#### Scenario: View payroll
- **WHEN** a role acts on reports and dashboards
- **THEN** the Payroll administrator can view payroll reports (V, payroll)

#### Scenario: View department
- **WHEN** a role acts on reports and dashboards
- **THEN** the Approver can view their department's reports (V, dept)

#### Scenario: View board pack
- **WHEN** a role acts on reports and dashboards
- **THEN** the Board viewer can view the board pack (V, board pack)

#### Scenario: No access
- **WHEN** a role acts on reports and dashboards
- **THEN** the Employee have no right over it (—)

### Requirement: Permission matrix: Budgets, forecasts
In Standard mode, each role SHALL hold only the rights over budgets and forecasts that the permission matrix gives it.

Source: PRD-001 §D

#### Scenario: Approve
- **WHEN** a role acts on budgets and forecasts
- **THEN** the Owner and the Finance controller can approve (A)

#### Scenario: Create or prepare
- **WHEN** a role acts on budgets and forecasts
- **THEN** the Accountant can create or prepare (C)

#### Scenario: Create for the department
- **WHEN** a role acts on budgets and forecasts
- **THEN** the Approver can create for their department (C, dept)

#### Scenario: View
- **WHEN** a role acts on budgets and forecasts
- **THEN** the Auditor, the Tax agent and the Board viewer can view (V)

#### Scenario: AI scenario proposal
- **WHEN** a role acts on budgets and forecasts
- **THEN** the AI assistant can only propose a scenario (Proposal, scenario)

#### Scenario: No access
- **WHEN** a role acts on budgets and forecasts
- **THEN** the Administrator, the Bookkeeper, the AR specialist, the AP specialist, the Payroll administrator, the Payment releaser, the Employee and the Compliance officer / DPO have no right over it (—)

### Requirement: Permission matrix: Audit trail
In Standard mode, each role SHALL hold only the rights over the audit trail that the permission matrix gives it.

Source: PRD-001 §D

#### Scenario: View
- **WHEN** a role acts on the audit trail
- **THEN** the Owner, the Administrator, the Finance controller, the Accountant, the Auditor, the Tax agent and the Compliance officer / DPO can view (V)

#### Scenario: No access
- **WHEN** a role acts on the audit trail
- **THEN** the Bookkeeper, the AR specialist, the AP specialist, the Payroll administrator, the Payment releaser, the Approver, the Employee, the Board viewer and the AI assistant have no right over it (—)

### Requirement: Permission matrix: Compliance Center
In Standard mode, each role SHALL hold only the rights over the Compliance Center that the permission matrix gives it.

Source: PRD-001 §D

#### Scenario: View
- **WHEN** a role acts on the Compliance Center
- **THEN** the Owner, the Administrator, the Finance controller, the Accountant and the Auditor can view (V)

#### Scenario: Create and approve
- **WHEN** a role acts on the Compliance Center
- **THEN** the Compliance officer / DPO can create or prepare and approve (C/A)

#### Scenario: No access
- **WHEN** a role acts on the Compliance Center
- **THEN** the Bookkeeper, the AR specialist, the AP specialist, the Payroll administrator, the Payment releaser, the Approver, the Employee, the Tax agent, the Board viewer and the AI assistant have no right over it (—)

### Requirement: Permission matrix: Tenant export
In Standard mode, each role SHALL hold only the rights over tenant export that the permission matrix gives it.

Source: PRD-001 §D

#### Scenario: Approve with a second approver
- **WHEN** a role acts on tenant export
- **THEN** the Owner can approve, with a second approver (A + second approver)

#### Scenario: Create or prepare
- **WHEN** a role acts on tenant export
- **THEN** the Administrator can create or prepare (C)

#### Scenario: View
- **WHEN** a role acts on tenant export
- **THEN** the Compliance officer / DPO can view (V)

#### Scenario: No access
- **WHEN** a role acts on tenant export
- **THEN** the Finance controller, the Accountant, the Bookkeeper, the AR specialist, the AP specialist, the Payroll administrator, the Payment releaser, the Approver, the Employee, the Auditor, the Tax agent, the Board viewer and the AI assistant have no right over it (—)

### Requirement: Permission matrix: AI providers and model routes
In Standard mode, each role SHALL hold only the rights over AI providers and model routes (connections, credentials, billing, data-class policy, per-agent routes; AI-72…AI-75) that the permission matrix gives it.

Source: PRD-001 §D

#### Scenario: Approve with a second approver
- **WHEN** a role acts on AI providers and model routes
- **THEN** the Owner can approve, with a second approver (A + second approver)

#### Scenario: Create or prepare
- **WHEN** a role acts on AI providers and model routes
- **THEN** the Administrator can create or prepare (C)

#### Scenario: View
- **WHEN** a role acts on AI providers and model routes
- **THEN** the Finance controller, the Auditor and the Compliance officer / DPO can view (V)

#### Scenario: No access
- **WHEN** a role acts on AI providers and model routes
- **THEN** the Accountant, the Bookkeeper, the AR specialist, the AP specialist, the Payroll administrator, the Payment releaser, the Approver, the Employee, the Tax agent, the Board viewer and the AI assistant have no right over it (—)

### Requirement: Permission matrix: Connected apps: enable MCP, allowlist clients, grant integration scopes
In Standard mode, each role SHALL hold only the rights over connected apps (enabling MCP, allowlisting clients, granting integration scopes) that the permission matrix gives it.

Source: PRD-001 §D

#### Scenario: Approve with a second approver
- **WHEN** a role acts on connected apps (enabling MCP, allowlisting clients, granting integration scopes)
- **THEN** the Owner can approve, with a second approver (A + second approver)

#### Scenario: Create or prepare
- **WHEN** a role acts on connected apps (enabling MCP, allowlisting clients, granting integration scopes)
- **THEN** the Administrator can create or prepare (C)

#### Scenario: View
- **WHEN** a role acts on connected apps (enabling MCP, allowlisting clients, granting integration scopes)
- **THEN** the Finance controller, the Auditor and the Compliance officer / DPO can view (V)

#### Scenario: No access
- **WHEN** a role acts on connected apps (enabling MCP, allowlisting clients, granting integration scopes)
- **THEN** the Accountant, the Bookkeeper, the AR specialist, the AP specialist, the Payroll administrator, the Payment releaser, the Approver, the Employee, the Tax agent, the Board viewer and the AI assistant have no right over it (—)

### Requirement: Permission matrix: Own MCP connection (opt in, consent, revoke)
In Standard mode, each role SHALL hold only the rights over their own MCP connection (opting in, consenting, revoking) that the permission matrix gives it.

Source: PRD-001 §D

#### Scenario: Create or prepare
- **WHEN** a user manages their own MCP connection (opting in, consenting, revoking)
- **THEN** the Owner, the Administrator, the Finance controller, the Accountant, the Bookkeeper, the AR specialist, the AP specialist, the Payroll administrator, the Payment releaser, the Approver, the Tax agent and the Compliance officer / DPO can create or prepare (C)

#### Scenario: No access
- **WHEN** a user manages their own MCP connection (opting in, consenting, revoking)
- **THEN** the Employee, the Auditor, the Board viewer and the AI assistant have no right over it (—)

### Requirement: Super admin outside the permission matrix
The Super admin (FR-ADM-06/07) SHALL be an installation role outside the permission matrix that manages sign-in providers, installation-wide email and messaging connections and announcements to everyone, and SHALL see an organisation's data only as a member.

Source: PRD-001 §D

#### Scenario: Super admin manages installation-wide settings
- **WHEN** the Super admin acts
- **THEN** they can manage sign-in providers, installation-wide email and messaging connections and announcements to everyone
- **AND** they see platform health (FR-ADM-07)

#### Scenario: Super admin sees data only as a member
- **GIVEN** a Super admin who is not a member of an organisation
- **WHEN** they try to see that organisation's data
- **THEN** it is refused

### Requirement: Company secretary in the permission matrix
The Company secretary role (FR-CS-08) SHALL have the Board viewer's rights, plus C on the corporate profile, registers and evidence, V on the equity register and the AI company secretary runs, and A on the `sec.*` drafts.

Source: PRD-001 §D

#### Scenario: Company secretary rights
- **WHEN** the Company secretary acts
- **THEN** they have the Board viewer's rights
- **AND** they can create or prepare the corporate profile, registers and evidence (C), view the equity register and the AI company secretary runs (V) and approve the `sec.*` drafts (A)

#### Scenario: No transactions, payroll or bank detail unless granted
- **GIVEN** no grant of transactions, payroll or bank detail
- **WHEN** the Company secretary tries to see or act on them
- **THEN** it is refused
