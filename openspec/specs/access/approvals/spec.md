# access/approvals Specification

## Purpose
How Fyniq approves money movements, journals, pay runs, policy changes and AI proposals: approval policies with ordered steps, what the approver sees, signed approvals bound to an approval envelope, step-up, expiry and invalidation, delegation, approval channels, reminders and the versioning of policies. Derived from PRD-001 §6.5.

## Requirements

### Requirement: FR-APR-01 Approval policies per object type
The system SHALL let approval policies be defined per object type, each with conditions and ordered steps.

Source: PRD-001 §6.5 · Priority M · Phase P1

#### Scenario: Object types that take an approval policy
- **WHEN** an approval policy is defined
- **THEN** its object type can be a bill, payment batch, journal, pay run, expense claim, PO, credit note, write-off, vendor bank change, period reopen, parameter change, role change, AI proposal, director-balance settlement or share issue (FR-EQ-05/08)

#### Scenario: Policy conditions
- **WHEN** the conditions of an approval policy are set
- **THEN** they can use the amount, entity, department, account, vendor and risk tier
- **AND** the policy applies to the records of its object type that meet those conditions

#### Scenario: Ordered steps with a quorum
- **WHEN** the steps of an approval policy are set
- **THEN** each step names a role or a named user and a quorum
- **AND** the steps are taken in their order

#### Scenario: The built-in policy until the organisation changes it
- **GIVEN** the organisation has not changed the policy of an object type
- **WHEN** a setting under FM-1, a role change or an invitation after the founder bootstrap waits for approval
- **THEN** its policy is the built-in one, with a version string the envelope records: one step, a quorum of one, signed by a member holding A on the object's Appendix D row (rev 0.36; rev 0.40 makes policies data, provisional until sign-off)

#### Scenario: Steps narrow who signs and never add anyone
- **WHEN** an organisation's policy is set (rev 0.40)
- **THEN** it has one to five ordered steps, each a quorum of one to five people who hold A on the object's Appendix D row
- **AND** a step may narrow them to some roles or to named members, never both, and never names a role or member without that right

#### Scenario: A person signs a request once
- **GIVEN** a policy with several steps or a quorum above one
- **WHEN** a person who signed a request tries to sign it again, at any step
- **THEN** the signature is refused with APR-11, and the step needs someone else (rev 0.40)

### Requirement: FR-APR-02 The approver sees exactly what is approved
The system SHALL show the approver exactly what is approved: the document, the ledger impact (debits and credits), the affected balances and checks, and a diff against the previous version.

Source: PRD-001 §6.5 · Priority M · Phase P1

#### Scenario: Approval view
- **WHEN** an approver opens an item waiting for their approval
- **THEN** the document, its ledger impact as debits and credits, the balances it affects and its checks are shown

#### Scenario: Diff against the previous version
- **GIVEN** the record being approved has a previous version
- **WHEN** the approver opens it
- **THEN** a diff against the previous version is shown

### Requirement: FR-APR-03 Signed approval records
Each approval SHALL be a signed record of the approver, the time, the method of authentication and a SHA-256 hash of the approval envelope (FR-APR-10), and executing a payload whose hash differs from the approved hash MUST be impossible.

Source: PRD-001 §6.5 · Priority M · Phase P1

#### Scenario: Approval record
- **WHEN** an approval is given
- **THEN** the approval record holds the approver, the time, the method of authentication and the SHA-256 hash of the approval envelope (FR-APR-10)

#### Scenario: The database refuses an unsigned approval
- **WHEN** a signed approval lacks the envelope's SHA-256 or a second factor at most 5 minutes old, carries another hash, is made by the requester, or is made on a request that is not pending or has expired
- **THEN** the database refuses it, and a request is approved only with such a signature (rev 0.36)

#### Scenario: The session's step-up until passkeys and TOTP
- **GIVEN** passkeys and TOTP do not exist yet (FR-ADM-02)
- **WHEN** an approval is signed
- **THEN** its method is the session's step-up within 5 minutes, recorded as session_step_up (rev 0.36, provisional until sign-off)

#### Scenario: A payload that differs from the approved hash never executes
- **GIVEN** an approved payload
- **WHEN** a payload whose hash differs from the approved hash is submitted for execution
- **THEN** it does not execute

#### Scenario: Passkey approval signs exactly what was approved
- **GIVEN** an approver who uses a passkey
- **WHEN** they approve
- **THEN** the server creates a fresh WebAuthn challenge for that approval: a random nonce of at least 16 bytes followed by the envelope hash
- **AND** the approval is a cryptographic signature over exactly what was approved

#### Scenario: Challenge accepted once, within the ceremony timeout
- **WHEN** the server creates a passkey challenge for an approval
- **THEN** it stores the challenge
- **AND** it accepts the challenge once and only within the ceremony timeout

#### Scenario: Passkey assertion is verified
- **WHEN** a passkey assertion for an approval is received
- **THEN** the server verifies its type, challenge, origin, RP ID hash and user-verified flag (W3C WebAuthn)

#### Scenario: Assertion kept for later re-verification
- **WHEN** a passkey approval is recorded
- **THEN** the assertion (authenticator data, client data, signature) is kept with the approval so it can be re-verified later

#### Scenario: TOTP approval
- **GIVEN** an approver who uses TOTP
- **WHEN** they approve
- **THEN** the TOTP code proves the step-up only
- **AND** the approval's binding is the signed approval record

#### Scenario: G4-056 Replayed passkey assertion
- **WHEN** a passkey assertion from one approval is replayed for another organisation, another proposal, a different item subset, or after the challenge expired
- **THEN** it is rejected with a stable error code
- **AND** nothing executes

### Requirement: FR-APR-04 Step-up for high-risk approvals
The system SHALL require step-up authentication (passkey or TOTP) for high-risk approvals.

Source: PRD-001 §6.5 · Priority M · Phase P1

#### Scenario: High-risk approval needs step-up
- **WHEN** a user approves a high-risk item
- **THEN** they must pass step-up authentication with a passkey or TOTP before the approval is recorded

#### Scenario: A passkey step-up counts (rev 0.43)
- **WHEN** the requester or an approver stepped their session up with a passkey in the last 5 minutes
- **THEN** it satisfies the step-up, like the provider's second factor at sign-in

### Requirement: FR-APR-05 Approval expiry and invalidation
Approvals SHALL expire (default 7 days) and SHALL become invalid if the underlying record changes, including any dependency version in the approval envelope (FR-APR-10).

Source: PRD-001 §6.5 · Priority M · Phase P1

#### Scenario: Approval expires
- **GIVEN** the default expiry of 7 days
- **WHEN** an approval has not been executed 7 days after it was given
- **THEN** it has expired and cannot be executed

#### Scenario: Record changes after approval
- **WHEN** the underlying record changes after it was approved
- **THEN** the approval becomes invalid

#### Scenario: The database voids a request when its record changes
- **GIVEN** a pending request to change a setting or a member's roles
- **WHEN** the setting gets its next change, or the member's roles change, by any path
- **THEN** the database voids the request and its signatures, audited in the same transaction (rev 0.36)

#### Scenario: Expiry from the app's clock
- **WHEN** a request is made
- **THEN** it expires 7 days later by the app's clock (DEV-05), and one request waits per record at a time (rev 0.36)

#### Scenario: A dependency version changes
- **WHEN** a dependency version in the approval envelope changes: the payee's bank details, a parameter pack, the approval policy or an account mapping
- **THEN** the approval becomes invalid

#### Scenario: Payment batch approved before a vendor bank change
- **GIVEN** a payment batch approved before a vendor bank change
- **WHEN** the bank change itself is approved
- **THEN** the payment batch is still blocked for re-approval (FR-CT-03)

#### Scenario: The requester withdraws a pending request
- **GIVEN** a pending request, with or without signatures
- **WHEN** the person who asked for it withdraws it
- **THEN** it becomes void with the reason "withdrawn by its requester", its signatures are voided, and nothing changes
- **AND** anyone else trying to withdraw it is refused with APR-12 (rev 0.40)

#### Scenario: G4-055 Vendor bank details change after a batch is approved
- **GIVEN** a payment batch is approved
- **WHEN** the vendor's bank details change and that change is approved
- **THEN** execution of the batch is blocked
- **AND** the batch needs re-approval showing the new bank details

### Requirement: FR-APR-06 Delegation of approval authority
The system SHALL let a user delegate their approval authority with a start and an end date (for example during leave), and the delegation SHALL be audited.

Source: PRD-001 §6.5 · Priority S · Phase P2

#### Scenario: Delegate approves within the dates
- **GIVEN** an approver delegated their authority to another user with a start and an end date
- **WHEN** the delegate approves an item between those dates
- **THEN** the approval is made under the delegated authority

#### Scenario: Delegation outside its dates
- **WHEN** the end date of a delegation has passed or its start date has not arrived
- **THEN** the delegate cannot approve under it

#### Scenario: Delegation is audited
- **WHEN** a delegation is created, changed or used
- **THEN** it is recorded in the audit trail

### Requirement: FR-APR-07 Approve from the Inbox, email and the mobile PWA
The system SHALL let approvers approve or reject from the Inbox, from email (secure link plus sign-in) and from the mobile PWA.

Source: PRD-001 §6.5 · Priority S · Phase P1 (web) / P2 (email, PWA)

#### Scenario: Approve or reject from the Inbox
- **WHEN** an approver opens an approval in the web Inbox
- **THEN** they can approve or reject it there

#### Scenario: Approve or reject from email
- **WHEN** an approver follows the secure link in an approval email
- **THEN** they must sign in before they can approve or reject

#### Scenario: Approve or reject from the mobile PWA
- **WHEN** an approver opens an approval in the mobile PWA
- **THEN** they can approve or reject it there

### Requirement: FR-APR-08 Escalation and reminders
The system SHALL send reminders for pending approvals and escalate them.

Source: PRD-001 §6.5 · Priority S · Phase P2

#### Scenario: Pending approval is reminded and escalated
- **GIVEN** an approval is pending
- **WHEN** it stays pending
- **THEN** its approvers are reminded
- **AND** the approval is escalated

### Requirement: FR-APR-09 Approval policy changes are approval-gated and versioned
Changes to approval policies SHALL themselves need approval (SOD-8) and SHALL be versioned.

Source: PRD-001 §6.5 · Priority M · Phase P1

#### Scenario: Policy change needs approval by someone else
- **WHEN** a user proposes a change to an approval policy
- **THEN** the change takes effect only after it is approved
- **AND** the proposer cannot approve it (SOD-8)

#### Scenario: Policies are versioned
- **WHEN** an approval policy change is approved
- **THEN** it becomes a new version of the policy and earlier versions are kept

#### Scenario: Who asks for and who approves a policy change
- **WHEN** a member who holds C on "Approval policies, AI catalogue" (the Administrator) asks to change a policy
- **THEN** the answer is an approval request that a member who holds A on that row (the Owner or the Controller), never the requester, approves, and the version is added then (rev 0.40)

#### Scenario: The policy for approval policies is fixed
- **WHEN** someone asks to change the policy for approval policies
- **THEN** it is refused with APR-09, so no change can lock the organisation out of changing its policies (rev 0.40)

#### Scenario: Steps the members cannot sign are refused
- **WHEN** a policy change names a role or member who never approves the object, or a quorum more than the members who may sign
- **THEN** it is refused with APR-08 when asked for, and voided with APR-08 if the members changed before it executes (rev 0.40)

#### Scenario: The database keeps the versions
- **WHEN** a version of a policy is added
- **THEN** the database adds it only as the next version, with the approved request that holds exactly its steps, and never changes or deletes one (rev 0.40)

#### Scenario: G4-057 Policy or pack changes while an approval is pending
- **GIVEN** an approval is pending
- **WHEN** the approval policy or a parameter pack changes
- **THEN** the pending approval restarts under the new version
- **AND** the old approval cannot execute

### Requirement: FR-APR-10 Approval envelope
An approver SHALL sign an approval envelope in canonical JSON (RFC 8785 or an equivalent documented form), and execution MUST be blocked when the envelope rebuilt in the executing transaction differs from the one approved.

Source: PRD-001 §6.5 · Priority M · Phase P1

#### Scenario: Envelope contents
- **WHEN** an approval envelope is built
- **THEN** it holds the organisation; the object type, ID and version; the operation; the selected items; the amounts and currencies; the payee and the version of the payee's bank details; the versions of the parameter packs, approval policy and account mappings used; and the hash of the impact shown (FR-APR-02)

#### Scenario: Quorum signs one envelope
- **GIVEN** an approval step with a quorum of several approvers
- **WHEN** they approve
- **THEN** all of them sign the same envelope

#### Scenario: Pending approval keeps its policy version
- **GIVEN** an approval started under a version of the approval policy
- **WHEN** its remaining steps are taken
- **THEN** they follow the policy version it started under

#### Scenario: A policy change restarts a pending approval
- **GIVEN** an approval is pending
- **WHEN** the approval policy changes (FR-APR-09)
- **THEN** the pending approval restarts

#### Scenario: The database holds the steps' order and quorum
- **GIVEN** a request keeps the steps of the policy it started under
- **WHEN** a signature signs a step whose earlier steps lack their quorum, a step past its quorum, or a step the policy does not have
- **THEN** the database refuses it, and it approves the request only when every step has its quorum (rev 0.40)

#### Scenario: A new policy version voids the pending requests of its kind
- **WHEN** a new version of an object type's policy is added
- **THEN** the database voids every pending request of that object type, audited in the same transaction (rev 0.40)

#### Scenario: Envelope rebuilt and checked at execution
- **WHEN** an approved item executes
- **THEN** in the executing transaction the platform rebuilds the envelope from current data and compares its hash with the approved hash
- **AND** it re-checks that every approver still holds the authority

#### Scenario: Any difference blocks execution
- **WHEN** the rebuilt envelope's hash differs, or an approver no longer holds the authority
- **THEN** execution is blocked with a stable error code

#### Scenario: An earlier approver who lost their right
- **GIVEN** a request whose earlier step was signed by a member
- **WHEN** that member no longer holds A on the object's row, or is no longer one their step names, when the last step is signed
- **THEN** nothing executes, the request becomes void and APR-10 says so (rev 0.40)

#### Scenario: Each object type declares its dependencies
- **WHEN** the envelope of a setting change or a role change is built
- **THEN** it holds the dependencies its object type declares: a setting's version, the value in effect on the change's date and the changes scheduled after it; a member's status, version and live roles
- **AND** a dependency that changes without a new version, such as a role that expires, blocks execution with APR-04 and voids the request (rev 0.36)

#### Scenario: AI proposals use the same envelope
- **WHEN** an AI proposal is approved
- **THEN** it uses the same envelope, with the proposal's `base_versions` (§9.5) as its dependency versions
