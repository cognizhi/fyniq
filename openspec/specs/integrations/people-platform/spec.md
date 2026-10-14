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

#### Scenario: An employee who only claims
- **GIVEN** an employee with only the Employee role in Fyniq
- **WHEN** they use Nemura
- **THEN** they see no Finance | People switch; Nemura's Claims tile opens Fyniq's personal workspace, whose header has Back to People

### Requirement: FR-PPL-03 One sign-in with Nemura
A linked Nemura SHALL use Fyniq's identity provider tenant, so that one session covers both apps, and SHALL call Fyniq only as an integration client with audience-bound tokens and the scopes of PRD-002 Appendix E.

Source: PRD-001 §8.26 · Priority M · Phase P1

#### Scenario: One session
- **WHEN** a person signs in to Fyniq or Nemura
- **THEN** they are signed in to both, and signing out of one signs them out of both

#### Scenario: No token passthrough
- **WHEN** Nemura calls Fyniq
- **THEN** it uses its own audience-bound client token with only its listed scopes, never a user's Fyniq token (AI-36)

#### Scenario: Sessions and identities
- **WHEN** a person uses both apps
- **THEN** their sessions and identity follow FR-PPL-18

### Requirement: FR-PPL-04 Pay runs from Nemura
Fyniq SHALL post an approved Nemura pay run as FR-PAY-07's posting only after verifying its approval and segregation (FR-PPL-09, FR-PPL-10), attributed to its approver via Nemura, under the same period locks, checks and balance rules as any posting, and SHALL receive its salary payment lines as a batch approved and released in Fyniq.

Source: PRD-001 §8.26 · Priority M · Phase P1

#### Scenario: Posting
- **WHEN** Nemura sends an approved pay run
- **THEN** Fyniq posts salary expense by department account and dimensions, employer CPF and SDL against 2100 Salaries payable and 2110 CPF & SDL payable, with no per-person lines
- **AND** the entry is attributed to the pay run's approver "via Nemura": the Nemura approval under SOD-4 is the human authority once Fyniq has verified it (FR-PPL-09, FR-INT-27)

#### Scenario: Sent twice
- **WHEN** the same run ID and version arrives again
- **THEN** nothing is posted twice
- **AND** what a new version of the run means is FR-PPL-11

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
- **AND** it keeps each person's tax-clearance hold state and version (FR-PPL-13) and bank-detail version (FR-PPL-14), never the account number
- **AND** it keeps each person's line manager (a person ID), for claim routing (FR-AP-17)

#### Scenario: Cost rates stay
- **WHEN** a project cost rate is set (FR-PRJ-06)
- **THEN** it stays in Fyniq, visible only with payroll rights (FR-PAY-13)

### Requirement: FR-PPL-07 Leave for the books
Leave taken SHALL update people's available hours without the leave type, counted once against holidays, an optional monthly leave-liability accrual SHALL arrive as a draft journal for approval, and whether untaken leave is recognised SHALL follow the accounting policy (FR-ORG-10), not a switch.

Source: PRD-001 §8.26 · Priority S · Phase P1 / P2 (accrual)

#### Scenario: Available hours
- **WHEN** leave is taken in Nemura or an external system
- **THEN** the person's available hours in Fyniq drop by those hours (FR-PRJ-12), and Fyniq does not learn the leave type

#### Scenario: Leave liability (P2)
- **GIVEN** the company's accounting policy accrues untaken leave
- **WHEN** a month ends
- **THEN** the accrual arrives as a draft journal for approval

#### Scenario: Default accrual policy
- **GIVEN** the default policy: a monthly accrual of carried-forward leave
- **WHEN** the month's accrual is below the accountant's materiality floor
- **THEN** no journal is proposed

#### Scenario: Leave obligation at close
- **GIVEN** the accounting policy requires untaken leave to be recognised, and the automatic accrual is off
- **WHEN** the month is closed
- **THEN** the close checklist (FR-GL-07) has a task for a valuation and an approved journal with evidence
- **AND** the close needs that task done, by the manual valuation or the automatic accrual

#### Scenario: Not expensed twice
- **WHEN** leave that was accrued is later taken
- **THEN** the accrual reverses or rolls forward, so the leave is not expensed twice

### Requirement: FR-PPL-08 Link health
Settings → People and HR SHALL show each feature's source, the last sync, events waiting or failed and a retry, and a link failing for 24 hours SHALL notify the Owner and the Payroll administrator.

Source: PRD-001 §8.26 · Priority M · Phase P1

#### Scenario: Health shown
- **WHEN** someone with the right opens Settings → People and HR
- **THEN** they see each feature's source, the last successful sync and any events waiting or failed, with a retry

#### Scenario: Failing for a day
- **WHEN** the link has failed for 24 hours
- **THEN** the Owner and the Payroll administrator are notified

#### Scenario: Near pay day
- **GIVEN** a run between its input cut-off and its pay day
- **WHEN** the link fails
- **THEN** the Owner and the Payroll administrator are alerted within 1 hour (PRD-002 NM-INT-04)

### Requirement: FR-PPL-09 Verified payroll authority
Before posting a Nemura run, Fyniq SHALL verify Nemura's link signature and epoch, the payload digest against the approval envelope, the approver's signed record against their passkey, that the approver is not the preparer and held the right, the mapping version, and that the envelope is unused; any failure SHALL refuse the run with no side effects.

Source: PRD-001 §8.26 · Priority M · Phase P1

#### Scenario: Forged payroll authority
- **GIVEN** Nemura's valid integration token
- **WHEN** a submission carries altered totals with an earlier approval reference
- **THEN** it is refused with a stable error code
- **AND** no journal and no salary batch is created, and the refusal is audited

#### Scenario: Wrong organisation or authority
- **WHEN** the approval was signed in another organisation, or the approver did not hold the payroll-approval right in Nemura when they signed
- **THEN** the run is refused

#### Scenario: Account mapping changed
- **WHEN** the envelope's account-mapping version is not Fyniq's current mapping (FR-EMP-02)
- **THEN** the run is refused with the mapping named

#### Scenario: Attribution
- **WHEN** a verified run posts
- **THEN** the entry records the human approver and the service origin "via Nemura" separately

#### Scenario: Imports stay drafts
- **WHEN** an external system's run is imported
- **THEN** it follows FR-PPL-05's draft path

### Requirement: FR-PPL-10 Segregation across both apps
The preparer of a Nemura run SHALL NOT approve or release its salary batch in Fyniq under any account, the batch's approvers and releaser SHALL be checked against the run as one chain, and no emergency path SHALL change a salary account, lift a hold or pay the person using it.

Source: PRD-001 §8.26 · Priority M · Phase P1

#### Scenario: Same person, two apps
- **GIVEN** Mei Lin Tan prepared the October run in Nemura
- **WHEN** she tries to approve or release its salary batch from her Fyniq account
- **THEN** it is refused (SOD-4)

#### Scenario: Identified by issuer and subject
- **WHEN** the run's preparer and approver reach Fyniq
- **THEN** they are identity-provider issuer and subject, which name the same person in both apps

#### Scenario: Emergency path
- **WHEN** a founder uses break-glass (FM-8)
- **THEN** it cannot change a salary account, lift a tax-clearance hold or pay that founder

### Requirement: FR-PPL-11 Corrections from Nemura
Each Nemura run version SHALL state its wage month's full figures and the version it supersedes; Fyniq SHALL post only the difference from what it already posted for that run, refuse a stale version, post a closed month's difference in the open month, and pay only net still unpaid.

Source: PRD-001 §8.26 · Priority M · Phase P1

#### Scenario: A correction converges
- **GIVEN** version 1 of a run posted S$10,000 of wages
- **WHEN** version 2 states S$10,200, and both versions are retried and arrive out of order
- **THEN** the wages recognised for the run total S$10,200
- **AND** only the net pay still unpaid becomes a supplementary salary payment line

#### Scenario: Stale version
- **WHEN** version 1 arrives after version 2 was posted
- **THEN** it is refused as stale

#### Scenario: Closed wage month
- **GIVEN** the wage month is closed (FR-GL-08)
- **WHEN** a correction arrives
- **THEN** the difference posts in the current open month with the original wage month recorded
- **AND** no entry in the closed month changes

#### Scenario: Withdrawn before payment
- **WHEN** a run is withdrawn before it is paid
- **THEN** its posting is reversed (FR-GL-12)

### Requirement: FR-PPL-12 Paid means paid
Each salary payment line SHALL have a state with its instruction ID, amount and date; "paid" SHALL come only from the bank feed or recorded evidence, each change SHALL go back to Nemura, and an unknown outcome SHALL block any new instruction for that line.

Source: PRD-001 §8.26 · Priority M · Phase P1

#### Scenario: Split bank outcome
- **GIVEN** a salary batch of 6 lines
- **WHEN** 4 are paid, 1 is rejected and 1 has no outcome
- **THEN** each line shows its own state, and Nemura receives each one (`payroll.payments.status`)
- **AND** the line with no outcome cannot be paid again, by file or by API, until it is resolved

#### Scenario: A file is not a payment
- **WHEN** a bank file is exported or the journal is posted
- **THEN** no line becomes "paid"

#### Scenario: Recorded outcome
- **WHEN** a person records a payment outcome
- **THEN** it needs evidence and is audited

### Requirement: FR-PPL-13 Tax-clearance hold at release
When linked, releasing any payment to an employee SHALL ask Nemura for the people's hold status; a held person's lines SHALL NOT be released, lines SHALL wait when Nemura cannot answer in time, and only Nemura's release after IRAS's notice or directive SHALL clear a hold.

Source: PRD-001 §8.26 · Priority M · Phase P1

#### Scenario: Resignation on pay day
- **GIVEN** a foreign employee resigns at 09:00 on pay day, while their salary line and a claim are queued
- **WHEN** the batch is released at 10:00, before the ordinary sync
- **THEN** their lines are not released, because the check at release finds the hold
- **AND** the rest of the batch can go

#### Scenario: Nemura does not answer
- **WHEN** Nemura does not answer within 5 seconds, or answers with a version older than the projection's newest hold event
- **THEN** those people's lines wait

#### Scenario: Unlinking with a hold open
- **WHEN** the link is removed while a hold is open
- **THEN** the hold moves into Fyniq (FR-PAY-10) and stays until released

### Requirement: FR-PPL-14 Salary bank details by version
Each salary payment line SHALL keep its bank-detail record ID, version, digest and approval in the batch's approval envelope, and a newer approved version SHALL block that person's unreleased lines until the batch is approved again; a line SHALL never be refreshed silently.

Source: PRD-001 §8.26 · Priority M · Phase P1

#### Scenario: Account changed after approval
- **GIVEN** a salary batch approved on Monday
- **WHEN** on Tuesday Nemura reports a newer approved version of Daniel Lim's bank details
- **THEN** his line cannot be released until the batch is approved again with the current details (FR-APR-05)
- **AND** the line is not refreshed silently

#### Scenario: Masked for approvers
- **WHEN** approvers review the batch
- **THEN** account numbers are masked

### Requirement: FR-PPL-15 Events between the apps
Fyniq SHALL send and consume Nemura events with the envelope of PRD-002 NM-INT-10, with a durable inbox, per-object versions, gap repair, tombstones and signed webhooks, and SHALL never log pay or bank payloads.

Source: PRD-001 §8.26 · Priority M · Phase P1

#### Scenario: A late older version
- **GIVEN** version 2 of a person's record was applied
- **WHEN** version 1 arrives late
- **THEN** it is ignored and recorded, and the person's manager is not set back

#### Scenario: A repeated event
- **WHEN** the same salary-ready event arrives twice
- **THEN** only one salary batch exists

#### Scenario: A gap
- **WHEN** a version is missing
- **THEN** Fyniq reads the object's current state through the API

#### Scenario: Replay
- **WHEN** a webhook's signature fails, or its timestamp is more than 5 minutes off
- **THEN** it is refused

### Requirement: FR-PPL-16 Link lifecycle and source changes
The link SHALL have states and an epoch every event carries, refusing earlier epochs; unlinking SHALL settle work in flight before revoking credentials; and a change of payroll source SHALL carry year-to-date state and cancel the old source's queued work.

Source: PRD-001 §8.26 · Priority M · Phase P1

#### Scenario: Unlinking with work in flight
- **GIVEN** a salary instruction whose bank outcome is unknown
- **WHEN** the Owner starts to unlink
- **THEN** unlinking waits until the outcome is resolved
- **AND** no other channel pays those people meanwhile

#### Scenario: An earlier epoch
- **WHEN** an event or credential from an earlier epoch arrives
- **THEN** it is refused

#### Scenario: Source change mid-year
- **GIVEN** payroll moves from an external system to Nemura on 1 Jul
- **WHEN** the July run is calculated
- **THEN** ordinary and additional wages from 1 Jan are there (FR-PAY-14)

### Requirement: FR-PPL-17 Salary payments on a need-to-know basis
A salary batch's lines SHALL be visible only to roles with the salary-payments right; others SHALL see the total, count and state, and bank-feed lines, exports, search, notifications, cached reports and AI tools SHALL apply the same rule.

Source: PRD-001 §8.26 · Priority M · Phase P1

#### Scenario: Approver without the right
- **GIVEN** a Controller who approves payment batches without the salary-payments right
- **WHEN** they open a salary batch
- **THEN** they see its total, the number of people and the release state, and no names or amounts

#### Scenario: Bank-feed line
- **WHEN** a bank debit is matched to a salary batch
- **THEN** people without the right see it as that batch

#### Scenario: Small group
- **WHEN** a department, project or cost centre's salary cost covers fewer than 3 people
- **THEN** Checks warns (FR-PAY-13)

### Requirement: FR-PPL-18 Sessions and identities with Nemura
People SHALL be matched by issuer, subject and tenant, never by email alone; each app SHALL keep its own audience-bound session and CSRF protection; signing out or deactivation in either SHALL revoke both apps' sessions and streams within 60 seconds.

Source: PRD-001 §8.26 · Priority M · Phase P1

#### Scenario: Two existing tenants
- **GIVEN** a standalone Nemura organisation on its own tenant, a Fyniq organisation on another, and one person using the same email in both
- **WHEN** they are linked
- **THEN** each person is mapped explicitly and confirms it
- **AND** no account is merged by email and no Fyniq role is granted

#### Scenario: HR-only session
- **GIVEN** an HR officer with no Fyniq financial rights
- **WHEN** they open a Fyniq URL, call its API, search or open live events
- **THEN** they land on Nemura's People home, and every financial read is refused

#### Scenario: Sign out of one, out of both
- **WHEN** a person signs out of Nemura, or is deactivated
- **THEN** their Fyniq session and live streams end within 60 seconds

#### Scenario: One passkey for both apps
- **WHEN** a person registers a passkey
- **THEN** it works in both apps, which share one relying-party ID and registry (PRD-002 §23 Q10)

### Requirement: FR-PPL-19 Restores across the two databases
Both apps SHALL keep each run's receipts and a reconciliation checkpoint, and after either database is restored, cross-app work after the restore point SHALL be held as unknown and reconciled before anything is sent or paid.

Source: PRD-001 §8.26 · Priority M · Phase P1

#### Scenario: Nemura restored
- **GIVEN** Nemura is restored to before a run was sent, while Fyniq has posted and paid it
- **WHEN** Nemura restarts
- **THEN** the run's hand-off is held as unknown and reconciled against Fyniq's receipts
- **AND** no journal or payment is created twice

#### Scenario: Fyniq restored
- **GIVEN** Fyniq is restored to before a posting Nemura has a receipt for
- **WHEN** Fyniq restarts
- **THEN** the gap is found from the receipts and reconciled before anything is posted again

#### Scenario: Privacy deletions after a restore
- **WHEN** either database is restored
- **THEN** the deletions in the privacy register are applied again (CMP-PD-02)
