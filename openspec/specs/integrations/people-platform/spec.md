# integrations/people-platform Specification

## Purpose
How Fyniq works with the people side: Nemura (PRD-002), Cognizhi's optional people platform, or a company's own HR and payroll system. Fyniq keeps the books' side: the People and HR settings, one sign-in and the Finance | People switch, pay runs posted from Nemura, imported payroll journals, the employee projection, leave for available hours and the link's health. Derived from PRD-001 §8.26.

## Requirements

### Requirement: FR-PPL-01 People and HR settings
Settings → People and HR SHALL record, per feature (people records, payroll, leave, letters and self-service), whether it lives in Nemura, an external system or nowhere, and linking Nemura or an external system SHALL need the Owner and a second approver.

Source: PRD-001 §8.26 · Priority M · Phase P1

#### Scenario: Source per feature
- **WHEN** the Owner opens Settings → People and HR
- **THEN** each feature shows where it lives: Nemura (linked), an external system through a connector or a file import, or none (payroll posted by manual journal)

#### Scenario: Fyniq never loads Nemura's features
- **WHEN** a feature lives in Nemura or an external system
- **THEN** Fyniq does not run that feature itself

#### Scenario: Linking needs two people
- **WHEN** someone links Nemura or chooses an external system
- **THEN** the change takes effect only after the Owner and a second approver approve it (golden rule 6)

### Requirement: FR-PPL-02 Workspace switch and HR landing
A person with rights in both Fyniq and Nemura SHALL see a Finance | People switch that keeps their place in each app and never changes rights, and a person with Nemura rights but no Fyniq financial rights SHALL land on Nemura's People home without financial figures.

Source: PRD-001 §8.26 · Priority M · Phase P1

#### Scenario: The switch
- **GIVEN** a person with rights in both apps
- **WHEN** they choose People in the header switch and later Finance
- **THEN** each app opens on the page they last used there, and their rights are unchanged

#### Scenario: HR landing
- **GIVEN** an HR officer with Nemura rights and no Fyniq financial rights
- **WHEN** they sign in through Fyniq
- **THEN** they land on Nemura's People home, with no ledger, bank or revenue figures

### Requirement: FR-PPL-03 One sign-in with Nemura
A linked Nemura SHALL use Fyniq's identity provider tenant, so that one session covers both apps, and SHALL call Fyniq only as an integration client with audience-bound tokens and the scopes of PRD-002 Appendix E.

Source: PRD-001 §8.26 · Priority M · Phase P1

#### Scenario: One session
- **WHEN** a person signs in to Fyniq or Nemura
- **THEN** they are signed in to both, and signing out of one signs them out of both

#### Scenario: No token passthrough
- **WHEN** Nemura calls Fyniq
- **THEN** it uses its own audience-bound client token with only its listed scopes, never a user's Fyniq token (AI-36)

### Requirement: FR-PPL-04 Pay runs from Nemura
Fyniq SHALL post an approved Nemura pay run as FR-PAY-07's posting, attributed to its approver via Nemura and idempotent on the run ID and version, under the same period locks, checks and balance rules as any posting, and SHALL receive its salary payment lines as a payment batch that is approved and released in Fyniq.

Source: PRD-001 §8.26 · Priority M · Phase P1

#### Scenario: Posting
- **WHEN** Nemura sends an approved pay run
- **THEN** Fyniq posts salary expense by department account and dimensions, employer CPF and SDL against 2100 Salaries payable and 2110 CPF & SDL payable, with no per-person lines
- **AND** the entry is attributed to the pay run's approver "via Nemura" (SOD-4 in Nemura is the human authority, FR-INT-27)

#### Scenario: Sent twice
- **WHEN** the same run ID and version arrives again
- **THEN** nothing is posted twice

#### Scenario: Refused posting
- **WHEN** the posting is refused (for example the period is locked)
- **THEN** Fyniq returns the error code to Nemura and posts nothing

#### Scenario: Salary payments
- **WHEN** the run's payment lines arrive
- **THEN** they form a salary payment batch (FR-PAY-11) that is approved and released in Fyniq like any other

#### Scenario: Public API only
- **WHEN** Nemura sends a pay run
- **THEN** it uses the public payroll-journal operation in `contracts/openapi/`, the same command an approved import uses, and no private endpoint

### Requirement: FR-PPL-05 External payroll and HR systems
Payroll journals from an external system, by connector or file import, SHALL arrive as draft journals for a Controller or Accountant to approve, idempotent per period and version with a preview of changes, and SHALL keep no per-person pay in the ledger.

Source: PRD-001 §8.26 · Priority M · Phase P1

#### Scenario: Draft for approval
- **WHEN** a payroll journal is imported from an external system
- **THEN** it is a draft journal until a Controller or Accountant approves it, because the external approval cannot be verified

#### Scenario: Re-import
- **WHEN** the same period is imported again
- **THEN** a preview shows what would change, and repeating the import adds nothing (FR-INT-27)

#### Scenario: No per-person pay
- **WHEN** an imported journal is posted
- **THEN** the ledger holds no per-person pay (FR-PAY-13)

### Requirement: FR-PPL-06 Employee projection
When people records live in Nemura or an external system, Fyniq SHALL keep only the employee projection accounting needs, and project cost rates SHALL stay in Fyniq, restricted.

Source: PRD-001 §8.26 · Priority M · Phase P1

#### Scenario: What Fyniq keeps
- **WHEN** a person is synced from Nemura or an external system
- **THEN** Fyniq keeps the person ID, display name, work email, department, cost centre, start and leaving dates and status, and nothing else from FR-EMP-01

#### Scenario: Cost rates stay
- **WHEN** a project cost rate is set (FR-PRJ-06)
- **THEN** it stays in Fyniq, visible only with payroll rights (FR-PAY-13)

### Requirement: FR-PPL-07 Leave for the books
Leave taken SHALL update people's available hours without the leave type, and an optional monthly leave-liability accrual SHALL arrive as a draft journal for approval.

Source: PRD-001 §8.26 · Priority S · Phase P1 / P2 (accrual)

#### Scenario: Available hours
- **WHEN** leave is taken in Nemura or an external system
- **THEN** the person's available hours in Fyniq drop by those hours (FR-PRJ-12), and Fyniq does not learn the leave type

#### Scenario: Leave liability (P2)
- **GIVEN** the company's accounting policy accrues untaken leave
- **WHEN** a month ends
- **THEN** the accrual arrives as a draft journal for approval

### Requirement: FR-PPL-08 Link health
Settings → People and HR SHALL show each feature's source, the last sync, events waiting or failed and a retry, and a link failing for 24 hours SHALL notify the Owner and the Payroll administrator.

Source: PRD-001 §8.26 · Priority M · Phase P1

#### Scenario: Health shown
- **WHEN** someone with the right opens Settings → People and HR
- **THEN** they see each feature's source, the last successful sync and any events waiting or failed, with a retry

#### Scenario: Failing for a day
- **WHEN** the link has failed for 24 hours
- **THEN** the Owner and the Payroll administrator are notified
