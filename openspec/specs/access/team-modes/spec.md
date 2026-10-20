# access/team-modes Specification

## Purpose
How Fyniq fits its segregation of duties to the size of the team: the Founder, Standard and Enterprise team modes, the mandatory compensating controls of Founder mode (FM-1 to FM-9, including break-glass) and how a team switches mode. Derived from PRD-001 §6.4.

## Requirements

### Requirement: Founder mode
For a team of 1–3 people who are all founders, Founder mode SHALL allow segregation-of-duties conflicts only with the compensating controls FM-1 to FM-9, all of which are mandatory.

Source: PRD-001 §6.4

#### Scenario: Conflicts allowed with compensating controls
- **GIVEN** an organisation in Founder mode
- **WHEN** a founder holds a combination of roles that conflicts under §6.3
- **THEN** the combination is allowed
- **AND** every compensating control FM-1 to FM-9 applies

#### Scenario: Default at go-live
- **WHEN** Cognizhi goes live
- **THEN** its team mode is Founder mode

### Requirement: Standard mode
For a team of 4–15 people with some finance staff, Standard mode SHALL block segregation-of-duties conflicts.

Source: PRD-001 §6.4

#### Scenario: Conflicts are blocked
- **GIVEN** an organisation in Standard mode
- **WHEN** a user would hold a combination of roles or actions that conflicts under §6.3
- **THEN** it is blocked

#### Scenario: Override of a conflict
- **GIVEN** an organisation in Standard mode
- **WHEN** a conflict is overridden
- **THEN** the override needs a reason and a second approver
- **AND** the override is reported

### Requirement: Enterprise mode
For a team of 15+ people or several entities, Enterprise mode SHALL block segregation-of-duties conflicts and SHALL add approval matrices per entity and department, SSO and SCIM, and quarterly access reviews.

Source: PRD-001 §6.4

#### Scenario: Conflicts are blocked
- **GIVEN** an organisation in Enterprise mode
- **WHEN** a user would hold a combination of roles or actions that conflicts under §6.3
- **THEN** it is blocked

#### Scenario: Approval matrices per entity and department
- **GIVEN** an organisation in Enterprise mode
- **WHEN** approval policies are set up
- **THEN** approval matrices can be defined per entity and per department

#### Scenario: SSO, SCIM and access reviews
- **GIVEN** an organisation in Enterprise mode
- **WHEN** its users are managed
- **THEN** sign-in through SSO and provisioning through SCIM are available
- **AND** access is reviewed quarterly

### Requirement: Mode switching
Switching the team mode SHALL be dual-controlled and audited.

Source: PRD-001 §6.4

#### Scenario: Switch needs dual control and is audited
- **WHEN** a user switches the organisation's team mode
- **THEN** a second person must approve the switch
- **AND** the switch is recorded in the audit trail

#### Scenario: Switch to Standard mode with conflicting users
- **GIVEN** users whose role combinations conflict under Standard mode
- **WHEN** a switch to Standard mode is requested
- **THEN** the platform lists every such user
- **AND** blocks the switch until each conflict is resolved or explicitly overridden

#### Scenario: Founder mode until the first switch, asked for and approved by different people (rev 0.42)
- **GIVEN** an organisation that has never switched its team mode
- **THEN** it is in Founder mode
- **WHEN** a member who holds C on "Users, roles, team mode" (the Administrator) asks for a switch
- **THEN** the answer is an approval request, and the switch is made only when a member who holds A on that row (an Owner), never the requester, approves it
- **AND** the database keeps each version of the team mode only with the approved request that holds it, and never changes one

#### Scenario: The conflict a combination of roles makes (rev 0.42)
- **WHEN** one person holds the Administrator role together with a role that holds P, A or R on a transaction row of Appendix D (Owner, Controller, Accountant, Bookkeeper, AR specialist, Payment releaser or Approver)
- **THEN** their roles hold SOD-6
- **AND** the Owner role alone, which holds both sides, is not a conflict, as Appendix D explains
- **AND** the other conflicts of §6.3 concern one item and are checked when the action happens

#### Scenario: Overrides and invitations in a switch (rev 0.42)
- **WHEN** a switch to a mode that blocks conflicts is asked for
- **THEN** each member's conflict is resolved or overridden with a reason, which the switch's approver accepts as the second approver
- **AND** a pending invitation whose roles would conflict blocks the switch until it is withdrawn
- **AND** an override lasts for that version of the team mode and is reported with it

#### Scenario: A switch voids pending requests (rev 0.42)
- **WHEN** a switch of the team mode is approved
- **THEN** every pending approval request of the organisation becomes void, since each was asked for under the old mode

#### Scenario: Conflicts while the mode blocks them (rev 0.42)
- **GIVEN** an organisation in a mode that blocks conflicts
- **WHEN** a role change would make a conflict
- **THEN** it needs an override with its reason, approved with the change
- **AND** an invitation whose roles conflict is refused
- **AND** a person whose roles hold SOD-6 without an override for the mode in force is refused when they post

#### Scenario: Enterprise mode is not available yet (rev 0.42)
- **WHEN** a switch to Enterprise mode is asked for
- **THEN** it is refused until approval matrices per entity and department, SSO, SCIM and quarterly access reviews exist

### Requirement: FM-1 Two-person rule for high-risk actions
In Founder mode, the approval engine SHALL require a second founder for every high-risk action regardless of amount, with step-up authentication (passkey or TOTP) for both founders.

Source: PRD-001 §6.4

#### Scenario: High-risk actions need a second founder
- **WHEN** a founder releases payments, adds or changes payee bank details, approves a pay run, reopens a period, changes roles, policies or the AI task catalogue, changes statutory parameters, voids a posted document, edits opening balances, repays, converts or waives a director's balance, or records a share issue (FR-EQ-05…08)
- **THEN** the approval engine requires a second founder to approve it

#### Scenario: Small amounts are not exempt
- **WHEN** a high-risk action is for an amount below the self-approval threshold
- **THEN** it still needs a second founder

#### Scenario: Step-up for both founders
- **WHEN** a high-risk action is initiated and approved
- **THEN** both founders pass step-up authentication with a passkey or TOTP

#### Scenario: The founder bootstrap is the one exception
- **GIVEN** the organisation has only one active member, its Owner
- **WHEN** the Owner changes roles, their own included, or invites someone with roles
- **THEN** no second founder is needed, because there is none yet (FR-ADM-01, rev 0.35)
- **AND** the change is audited and listed for the weekly review once another founder joins

#### Scenario: Settings and role changes wait for a second founder
- **GIVEN** the organisation has two or more active members
- **WHEN** a founder changes a setting under FM-1, changes a role or invites someone with roles
- **THEN** the answer is an approval request that another member holding A on the object's Appendix D row approves, and the change is made then
- **AND** the requester never approves their own, which the database refuses too (rev 0.36)

### Requirement: FM-2 Self-approval below a threshold
In Founder mode, a founder SHALL be allowed to self-approve routine items at or below a threshold (default S$2,000 per document; configurable), and each self-approval SHALL be flagged "self-approved" and queued for review.

Source: PRD-001 §6.4

#### Scenario: Routine item below the threshold
- **GIVEN** the threshold is S$2,000 per document
- **WHEN** a founder approves their own routine document of S$1,500
- **THEN** the approval is allowed
- **AND** it is flagged "self-approved" and queued for review

#### Scenario: Item exactly at the threshold
- **GIVEN** the threshold is S$2,000 per document
- **WHEN** a founder approves their own routine document of S$2,000.00
- **THEN** the approval is allowed ("below" means at or below), flagged "self-approved" and queued for review

#### Scenario: Item not below the threshold
- **GIVEN** the threshold is S$2,000 per document
- **WHEN** a founder tries to approve their own routine document of S$2,500
- **THEN** self-approval is not allowed

#### Scenario: Threshold is configurable
- **WHEN** the self-approval threshold is changed
- **THEN** the new threshold applies instead of the S$2,000 default

### Requirement: FM-3 Weekly review
In Founder mode, another founder SHALL review the log of self-approved and high-risk items every week, as an Inbox task.

Source: PRD-001 §6.4

#### Scenario: Weekly review task
- **WHEN** a week passes
- **THEN** an Inbox task asks a founder other than the one who acted to review that week's self-approved and high-risk log

#### Scenario: Unreviewed items older than 14 days
- **WHEN** an item in the log is still unreviewed 14 days after it was logged
- **THEN** it shows on Checks as "included, please review" (⚠)

### Requirement: FM-4 Monthly close attestation
In Founder mode, each monthly close SHALL be attested by a founder who did not prepare the close, as part of the close checklist.

Source: PRD-001 §6.4

#### Scenario: Close needs the attestation
- **WHEN** a monthly close has no attestation
- **THEN** the close cannot complete

#### Scenario: Preparer cannot attest
- **WHEN** the founder who prepared the close tries to sign its attestation
- **THEN** the attestation is refused

### Requirement: FM-5 Independent bank reconciliation evidence
In Founder mode, the statement balances of a bank reconciliation SHALL come from the bank feed or an uploaded statement, never typed.

Source: PRD-001 §6.4

#### Scenario: Typed statement balance refused
- **GIVEN** an organisation in Founder mode
- **WHEN** a user types a statement balance into a reconciliation
- **THEN** the reconciliation refuses it

#### Scenario: Balance from the feed or a statement
- **GIVEN** an organisation in Founder mode
- **WHEN** a reconciliation is prepared
- **THEN** its statement balance is taken from the bank feed or an uploaded statement

### Requirement: FM-6 AI anomaly review
In Founder mode, the AI SHALL review journals, vendor changes and payments for anomalies as an advisory control, and SHALL never approve.

Source: PRD-001 §6.4

#### Scenario: Unusual items flagged into the weekly review
- **WHEN** the AI finds an unusual journal, vendor change or payment
- **THEN** it flags the item into the weekly review (FM-3)

#### Scenario: The AI never approves
- **WHEN** the AI has reviewed an item
- **THEN** it does not approve it

### Requirement: FM-7 Accountant review
In Founder mode, the external accountant SHALL review at least quarterly, through time-boxed access with read access to the full audit trail, and each review SHALL be a record per period with its sign-off.

Source: PRD-001 §6.4

#### Scenario: Time-boxed access with the full audit trail
- **WHEN** the accountant is given access for a review
- **THEN** the access is time-boxed
- **AND** it includes read access to the full audit trail

#### Scenario: At least quarterly
- **WHEN** a quarter passes
- **THEN** at least one accountant review covers it

#### Scenario: Review record per period
- **WHEN** an accountant review is recorded for a period
- **THEN** it holds the close snapshot it covers (AUD-04), the accountant's queries with management's answers, any adjusting journals and the sign-off

#### Scenario: Adjusting journals
- **WHEN** the review raises an adjusting journal
- **THEN** it goes through FR-GL-01 approval
- **AND** in a closed period it follows FR-GL-08

#### Scenario: Reopening a reviewed period
- **WHEN** a reviewed period is reopened
- **THEN** its review is marked superseded
- **AND** both versions are kept

#### Scenario: G4-109 Quarterly review with queries and an adjusting journal
- **GIVEN** the accountant's quarterly review raises two queries and one adjusting journal for a closed period
- **WHEN** the review is recorded
- **THEN** the queries and answers are recorded against that period's close snapshot
- **AND** the journal follows FR-GL-01 approval and the FR-GL-08 reopen rule
- **AND** a later reopen marks the review superseded

### Requirement: FM-8 Break-glass for a single available founder
In Founder mode, when only one founder is available (for example the other is unreachable), an Owner SHALL be able to use break-glass, with a reason, step-up and a 24-hour expiry, for the allowed actions only, followed by automatic notification and a mandatory post-review.

Source: PRD-001 §6.4

#### Scenario: Starting break-glass
- **WHEN** an Owner starts break-glass
- **THEN** they must give a reason and pass step-up
- **AND** break-glass expires 24 hours later

#### Scenario: Allowed actions
- **GIVEN** break-glass is active
- **WHEN** the Owner releases payments of already-approved bills, claims and pay runs or statutory payments (IRAS, CPF), or approves a pay run so salaries are paid on time
- **THEN** the action is allowed

#### Scenario: Payees with a recent bank change
- **GIVEN** break-glass is active
- **WHEN** the Owner releases a payment to a payee whose bank details changed in the last 30 days
- **THEN** the release is refused

#### Scenario: Total per break-glass
- **GIVEN** break-glass is active
- **WHEN** a release would take the payments under this break-glass above the total set for it (setting, open question 35)
- **THEN** the release is refused

#### Scenario: Never allowed under break-glass
- **GIVEN** break-glass is active
- **WHEN** the Owner tries to add or change payee bank details; change users, roles, team mode, approval policies, sign-in methods, the AI catalogue or AI settings; change statutory parameters; reopen a period; void; edit opening balances; pay or settle in favour of the founder using break-glass (FM-9 still applies); or export the tenant
- **THEN** the action is refused

#### Scenario: Break-glass cannot extend itself
- **WHEN** an extension or renewal of an active break-glass is attempted under that break-glass
- **THEN** it is refused

#### Scenario: Banner and notifications
- **WHEN** break-glass starts
- **THEN** every Owner and the external accountant are notified immediately
- **AND** a banner shows on every page while it is active

#### Scenario: Post-review
- **WHEN** break-glass has been used
- **THEN** the other founder must review it within 7 days of their return
- **AND** the external accountant (FM-7) reviews it instead if the other founder is still unavailable after 7 days

#### Scenario: Unreviewed break-glass
- **WHEN** a break-glass has not been post-reviewed in time
- **THEN** it shows on Checks as "cannot post until fixed" (✖)
- **AND** it blocks the monthly close attestation (FM-4)

#### Scenario: G4-062 Break-glass with the other founder unreachable
- **GIVEN** the other founder is unreachable
- **WHEN** a founder uses break-glass to release approved bills to unchanged payees, then tries to change a payee's bank details
- **THEN** the release succeeds under a banner and break-glass expires in 24 hours
- **AND** the bank change is refused
- **AND** the Owners and the accountant are notified
- **AND** a break-glass still unreviewed after 7 days shows on Checks as "cannot post until fixed" (✖) and blocks the close attestation

#### Scenario: G4-063 Break-glass to settle the founder's own balance
- **WHEN** break-glass is used to settle the founder's own director balance
- **THEN** it is refused
- **AND** FM-9 applies

### Requirement: FM-9 A founder never approves the settlement of their own balance
A founder SHALL never approve the repayment, conversion or waiver of their own balance: another founder SHALL approve it, and the approval engine SHALL exclude the beneficiary automatically.

Source: PRD-001 §6.4

#### Scenario: Another founder approves
- **WHEN** a founder's balance is to be repaid, converted or waived
- **THEN** the approval engine excludes that founder as an approver
- **AND** another founder's approval is needed

#### Scenario: Every available approver benefits
- **GIVEN** every available approver benefits (for example, all founders convert together)
- **WHEN** the settlement is approved
- **THEN** the approval engine adds a step for the external accountant's recorded review automatically
- **AND** the settlement does not post before that review is recorded

#### Scenario: Resolutions attached
- **WHEN** the settlement of a founder's balance is approved
- **THEN** the resolutions are attached (FR-EQ-05, FR-EQ-06)
