# ai/proposals Specification

## Purpose
The principles the AI assistant works under and the proposal layer through which every change the AI wants reaches a human: what a proposal stores, its lifecycle, approval and execution, numeric grounding, the AI activity log, kill switches, undo and approval-quality metrics. Derived from PRD-001 §9.1 and §9.5. Also PRD-001 §9.3.

## Requirements

### Requirement: AI-P1 AI reads freely within the user's permissions
The AI SHALL read freely within the user's permissions and SHALL answer with cited figures.

Source: PRD-001 §9.1

#### Scenario: Reads go through the semantic layer on behalf of the user
- **WHEN** the AI reads data
- **THEN** it reads through read tools on a curated semantic layer, with on-behalf-of authorisation (§9.4)

#### Scenario: Answers cite their figures
- **WHEN** the AI answers with figures
- **THEN** each figure is cited

### Requirement: AI-P2 AI never changes data directly
The AI SHALL never change data directly: it SHALL have no write path to the ledger, documents, settings or users.

Source: PRD-001 §9.1

#### Scenario: The AI runtime can only insert proposals
- **WHEN** the privileges of the AI runtime's service account and database role are inspected
- **THEN** they hold no write privilege except inserting proposals

#### Scenario: A control test proves it on every build and nightly
- **WHEN** a build runs, and every night
- **THEN** a control test proves the AI runtime has no write privilege except inserting proposals (CMP-AI-01)

#### Scenario: The rule covers every agent-class client
- **WHEN** an `agent`-class client acts
- **THEN** it has no write path to the ledger, documents, settings or users, as for our AI runtime

#### Scenario: An AI using an integration credential is bounded by that client
- **GIVEN** an AI that uses an `integration` credential
- **WHEN** it acts
- **THEN** it is bounded by that client's scopes and approval policies (AI-34)

### Requirement: AI-P3 Every change the AI wants is a catalogue proposal
Every change the AI wants SHALL be a proposal of a type from the approved task catalogue, and anything not in the catalogue SHALL NOT be proposable.

Source: PRD-001 §9.1

#### Scenario: A task type outside the catalogue is rejected
- **WHEN** the AI proposes a task type that is not in the approved task catalogue
- **THEN** the proposal service rejects it

#### Scenario: The payload is validated against the task type's schema
- **WHEN** the AI proposes a catalogue task type
- **THEN** the proposal service validates the task type and the payload against its JSON Schema

### Requirement: AI-P4 A human approves each proposal, every time
A human SHALL approve each AI proposal, every time, with no standing approvals, no "always allow" and no auto-approve rules for AI proposals, in any team mode.

Source: PRD-001 §9.1

#### Scenario: No standing or automatic approval in any team mode
- **WHEN** anyone tries to set up a standing approval, an "always allow" or an auto-approve rule for AI proposals, in any team mode
- **THEN** it cannot be set up

#### Scenario: Policies cannot remove the approval step
- **WHEN** an approval policy is configured
- **THEN** AI proposals keep their mandatory approval step in the approval engine

### Requirement: AI-P5 What is approved is exactly what is executed
What is approved SHALL be exactly what is executed.

Source: PRD-001 §9.1

#### Scenario: The approval signs the payload hash
- **WHEN** a human approves a proposal
- **THEN** the approval signs the proposal's payload hash

#### Scenario: The executor refuses a mismatch
- **WHEN** the payload to be executed does not match the approved payload hash
- **THEN** the executor refuses to execute it

#### Scenario: The executor refuses stale base data
- **WHEN** the base data has changed since the proposal's dry run
- **THEN** the executor refuses to execute it

### Requirement: AI-P6 AI does not do arithmetic in prose
The AI SHALL NOT do arithmetic in prose: figures SHALL come from tools, and calculations SHALL go through a deterministic calculator tool.

Source: PRD-001 §9.1

#### Scenario: Calculations go through the calculator tool
- **WHEN** an answer needs a calculation
- **THEN** the calculation is made by the deterministic calculator tool

#### Scenario: Every answer is checked for grounding
- **WHEN** the AI gives an answer
- **THEN** the numeric grounding check (AI-08) runs on it

### Requirement: AI-P7 Untrusted content stays data
Bank descriptions, emails, uploaded documents and web content SHALL never instruct the assistant: untrusted content SHALL stay data.

Source: PRD-001 §9.1

#### Scenario: Instructions inside untrusted content are not followed
- **WHEN** a bank description, email, uploaded document or web page contains an instruction
- **THEN** the assistant treats it as data, under the prompt-injection defences of §9.8

#### Scenario: The catalogue and approval gate are the backstop
- **GIVEN** injected content gets past the prompt-injection defences
- **WHEN** it leads the AI to want a change
- **THEN** the change can still only be a catalogue proposal that a human approves

### Requirement: AI-P8 Everything the AI does is audited
Everything the AI does SHALL be audited and visible to the user (their own) and to the Owner and Compliance (all).

Source: PRD-001 §9.1

#### Scenario: AI actions are in the AI activity log
- **WHEN** the AI does anything
- **THEN** it is recorded in the AI activity log (AI-12)
- **AND** the user sees their own records, and the Owner and Compliance see all of them

### Requirement: Proposal lifecycle
A proposal SHALL move through the states Draft, Invalid, Pending, Approved, Rejected, Expired, Superseded, Executing, Executed, Stale and Failed, by the transitions below only.

Source: PRD-001 §9.5

#### Scenario: A proposal starts as Draft
- **WHEN** the AI calls the `propose` tool
- **THEN** a proposal is created in Draft

#### Scenario: Validation fails
- **GIVEN** a Draft proposal
- **WHEN** schema or domain validation fails
- **THEN** the proposal becomes Invalid

#### Scenario: Dry run succeeds
- **GIVEN** a Draft proposal
- **WHEN** its dry run succeeds
- **THEN** the impact is stored
- **AND** the proposal becomes Pending

#### Scenario: A human decides
- **GIVEN** a Pending proposal
- **WHEN** a human approves it, with step-up authentication for tiers 2 and 3
- **THEN** it becomes Approved
- **AND** if instead a human rejects it with a reason, it becomes Rejected

#### Scenario: No decision within the expiry
- **GIVEN** a Pending proposal
- **WHEN** no decision is made before it expires
- **THEN** it becomes Expired

#### Scenario: The proposal is revised
- **GIVEN** a Pending proposal
- **WHEN** the AI or the user revises it
- **THEN** it becomes Superseded

#### Scenario: Execution outcomes
- **GIVEN** an Approved proposal that moves to Executing
- **WHEN** the execution commits
- **THEN** it becomes Executed
- **AND** it becomes Stale if base data changed since the dry run, and Failed on a validation or lock error

#### Scenario: Executed and Stale are final
- **WHEN** a proposal is Executed or Stale
- **THEN** it leaves that state no more

### Requirement: Proposal content and payload hash
A proposal SHALL store its `task_type` and `task_version`, its `payload` (JSON) and `payload_hash` (SHA-256), and for batch proposals its `items[]`.

Source: PRD-001 §9.5

#### Scenario: Task type and version
- **WHEN** a proposal is stored
- **THEN** `task_type` and `task_version` identify its catalogue entry and the version of that entry's schema and rules

#### Scenario: The payload is exactly what will execute
- **WHEN** a proposal is stored
- **THEN** its `payload` and its SHA-256 `payload_hash` are exactly what will execute

#### Scenario: Batch items are selectable and partial approval re-hashes
- **GIVEN** a batch proposal (e.g. 40 categorisations)
- **WHEN** the approver approves only some items
- **THEN** each item was individually selectable
- **AND** the selected subset is re-hashed

### Requirement: Proposal rationale, evidence and dry-run impact
A proposal SHALL store the AI's `rationale` and `evidence_refs[]`, the dry-run `impact` and the `base_versions` of the records it relied on.

Source: PRD-001 §9.5

#### Scenario: Rationale with evidence
- **WHEN** a proposal is stored
- **THEN** it records why the AI proposes it, with links to the records and documents it relied on

#### Scenario: Dry-run impact
- **WHEN** a proposal's dry run succeeds
- **THEN** its `impact` holds the ledger lines that would post, balances before and after, checks that change, and documents created or changed (diff)

#### Scenario: Base versions for optimistic concurrency
- **WHEN** a proposal is stored
- **THEN** `base_versions` holds the row versions of every record read or changed, for optimistic concurrency

### Requirement: Proposal tier, originator, approvals and execution record
A proposal SHALL store its `risk_tier` and `required_approvals`, its `originator`, its `approvals[]`, its `execution` and its `expires_at`.

Source: PRD-001 §9.5

#### Scenario: Tier and required approvals
- **WHEN** a proposal is stored
- **THEN** its `risk_tier` and `required_approvals` come from the catalogue and the approval policies

#### Scenario: Originator
- **WHEN** a proposal is stored
- **THEN** its `originator` records the AI principal, the user who asked, the conversation ID, the model ID, and the prompt and tool versions

#### Scenario: Approvals
- **WHEN** a proposal is approved
- **THEN** `approvals[]` records the approver, time, authentication method and approved envelope hash
- **AND** for passkeys, the stored assertion (FR-APR-03, FR-APR-10)

#### Scenario: Execution record
- **WHEN** a proposal is executed
- **THEN** `execution` records the executor run ID, the resulting record IDs, the ledger entry IDs and the error if any

#### Scenario: Default expiry of 72 hours
- **WHEN** a proposal is stored
- **THEN** `expires_at` defaults to 72 hours later

### Requirement: AI-06 Explicit human approval
Each proposal SHALL require an explicit human approval, and there SHALL be no way to configure automatic approval of AI proposals.

Source: PRD-001 §9.5 · Priority M · Phase P1

#### Scenario: Nothing executes without an explicit human approval
- **GIVEN** a pending proposal
- **WHEN** no human has explicitly approved it
- **THEN** it is not executed

#### Scenario: Automatic approval cannot be configured
- **WHEN** a setting or policy that would approve AI proposals automatically is attempted
- **THEN** it cannot be configured
- **AND** a control test verifies this

### Requirement: AI-07 What the approver sees
The approver SHALL see the plain-language summary, the item list, the ledger impact, the diff and the AI's evidence, and approving SHALL need step-up authentication for risk tiers 2 and 3.

Source: PRD-001 §9.5 · Priority M · Phase P1

#### Scenario: The review shows the whole proposal
- **WHEN** an approver opens a proposal
- **THEN** they see the plain-language summary, the item list, the ledger impact, the diff and the AI's evidence

#### Scenario: The summary is generated by platform code
- **WHEN** the plain-language summary is shown
- **THEN** it was generated by platform code from the payload and the dry-run impact, not from model text

#### Scenario: The AI's rationale is labelled as AI text
- **WHEN** the AI's rationale is shown
- **THEN** it is shown separately from the summary and labelled as AI text

#### Scenario: Step-up authentication for tiers 2 and 3
- **GIVEN** a proposal of risk tier 2 or 3
- **WHEN** the approver approves it
- **THEN** step-up authentication is required

#### Scenario: G4-095 Misleading AI rationale
- **GIVEN** an in-catalogue proposal carries a misleading AI rationale
- **WHEN** the approver reviews it
- **THEN** the review screen shows the platform-generated summary and impact
- **AND** the true effect is visible whatever the rationale says

### Requirement: AI-08 Numeric grounding check
Every number in an AI answer or proposal summary SHALL match a value returned by a tool in that turn (after normalisation) or come from the calculator tool, and SHALL be bound to its fact (AI-68).

Source: PRD-001 §9.5 · Priority M · Phase P1

#### Scenario: A number must come from a tool or the calculator
- **WHEN** an AI answer or proposal summary contains a number
- **THEN** it matches, after normalisation, a value a tool returned in that turn, or it comes from the calculator tool

#### Scenario: Unmatched numbers are regenerated
- **WHEN** a number matches no tool value of that turn and does not come from the calculator tool
- **THEN** it is regenerated

#### Scenario: Attribution must match the bound fact
- **WHEN** a figure matches a tool value
- **THEN** it is bound to a fact (AI-68)
- **AND** the measure, entity, period and currency the sentence gives it must match that fact

#### Scenario: First-party answers render figures from the fact
- **WHEN** a first-party answer shows a figure
- **THEN** platform code renders the figure from the cited fact

#### Scenario: A figure reused from an earlier turn
- **WHEN** an answer reuses a figure from an earlier turn
- **THEN** the figure keeps its fact ID or is read again

#### Scenario: Missing fact or failed tool
- **WHEN** a needed fact is missing or a tool failed
- **THEN** the answer says what it could not establish and gives no figure for it
- **AND** it never presents a partial result as complete

#### Scenario: G4-089 Right value, wrong customer or month
- **GIVEN** the assistant is asked for August revenue from customer A
- **AND** the tool also returned customer B's revenue and July's figure, and one of them equals the right value
- **WHEN** the assistant answers
- **THEN** the figure is bound to the fact for customer A, August, SGD
- **AND** an answer that attributes B's or July's fact fails grounding and is regenerated or withheld

#### Scenario: G4-090 A tool fails during a multi-part question
- **GIVEN** one of three read tools fails during a multi-part question
- **AND** one figure comes from an earlier turn
- **WHEN** the assistant answers
- **THEN** the answer says which part it could not establish and gives no figure for it
- **AND** the reused figure keeps its fact ID or is read again

### Requirement: AI-09 Two approvers for tier 3
Tier 3 task types SHALL need two different human approvers, in every team mode.

Source: PRD-001 §9.5 · Priority M · Phase P1

#### Scenario: One approval is not enough for tier 3
- **GIVEN** a tier-3 proposal, in any team mode
- **WHEN** one human has approved it
- **THEN** it is not approved until a second, different human approves it

### Requirement: AI-10 Proposer of record and the requester as preparer
The AI SHALL be the proposer of record, and for segregation of duties (SOD-1…10) and Founder-mode self-approval (FM-2) the human who asked SHALL be treated as the preparer; the AI is never the preparer for these rules.

Source: PRD-001 §9.5 · Priority M · Phase P1

#### Scenario: Four roles recorded separately
- **WHEN** a proposal is approved and executed
- **THEN** the audit trail records the originator, the requester, the approver and the executor separately

#### Scenario: The requester may approve tier 1
- **WHEN** the human who asked approves a tier-1 proposal
- **THEN** the approval is allowed

#### Scenario: The requester may approve a tier-2 draft
- **GIVEN** a tier-2 proposal whose effect is a draft
- **WHEN** the human who asked approves it
- **THEN** the approval is allowed

#### Scenario: A tier-2 proposal that posts or changes a record
- **GIVEN** a tier-2 proposal that posts or changes a record (for example `gl.post_journal`, `ap.post_bill`)
- **WHEN** it is approved
- **THEN** the approver must be someone who could approve that record under its own policy if the requester had prepared it

#### Scenario: Standard mode needs someone else
- **GIVEN** Standard mode and a tier-2 proposal that posts or changes a record
- **WHEN** the human who asked tries to approve it
- **THEN** someone else must approve it

#### Scenario: Founder mode applies the FM-2 threshold and FM-3 review
- **GIVEN** Founder mode and a tier-2 proposal that posts or changes a record
- **WHEN** the human who asked approves it
- **THEN** the FM-2 threshold, the self-approved flag and the FM-3 review apply

#### Scenario: G4-058 Self-approving an AI-drafted accrual journal
- **GIVEN** a user asks the AI for a S$4,000 accrual journal
- **WHEN** they try to approve it themselves
- **THEN** in Standard mode it is refused and another eligible approver is needed
- **AND** in Founder mode it is refused above the FM-2 threshold, and otherwise allowed, flagged "self-approved" and queued for the FM-3 review

### Requirement: AI-11 Idempotent, atomic execution
Executions SHALL be idempotent (proposal ID as key) and atomic (one database transaction), and SHALL never bypass period locks, approval policies or validations.

Source: PRD-001 §9.5 · Priority M · Phase P1

#### Scenario: Repeating an execution changes nothing
- **GIVEN** a proposal that has been executed
- **WHEN** its execution is run again
- **THEN** the proposal ID, as the idempotency key, prevents a second effect

#### Scenario: One database transaction
- **WHEN** an execution fails part-way
- **THEN** none of its changes are committed, because it runs in one database transaction

#### Scenario: Locks, policies and validations hold
- **WHEN** a proposal executes
- **THEN** period locks, approval policies and validations apply exactly as they do to any other change

### Requirement: AI-12 AI activity log
The system SHALL keep an AI activity log of every conversation turn, tool call (inputs, output hash, row counts), model, token usage, cost, proposals and outcomes, retained as audit data.

Source: PRD-001 §9.5 · Priority M · Phase P1

#### Scenario: What a log entry records
- **WHEN** the assistant handles a conversation turn
- **THEN** the log records the turn, each tool call with its inputs, output hash and row counts, the model, token usage, cost, proposals and their outcomes

#### Scenario: Knowledge-base page versions used
- **WHEN** an answer used knowledge-base pages
- **THEN** the log records the page versions it used (AI-51)

#### Scenario: Users see their own entries, the Owner and Compliance see all
- **WHEN** the log is viewed
- **THEN** a user sees their own entries
- **AND** the Owner and Compliance see all entries

#### Scenario: Restricted data stays masked
- **GIVEN** a log entry that contains payroll or other restricted data
- **WHEN** someone without the matching right views it
- **THEN** the entry has inherited that data class and is shown masked (FR-PAY-13)

#### Scenario: Reading restricted entries is audited
- **WHEN** someone reads a log entry that contains restricted data
- **THEN** the read is audited (AUD-05)

#### Scenario: Retained as audit data
- **WHEN** retention is applied to the AI activity log
- **THEN** its entries are retained as audit data

### Requirement: AI-13 Kill switch and token budgets
The Owner SHALL be able to disable the assistant, the propose tool or individual task types, with new requests refused at once and existing access stopped within 60 seconds, and the system SHALL keep organisation and per-user token budgets with alerts.

Source: PRD-001 §9.5 · Priority M · Phase P1

#### Scenario: Owner disables the assistant
- **WHEN** the Owner disables the assistant
- **THEN** new assistant requests are refused from the moment the switch is saved

#### Scenario: Owner disables proposing
- **WHEN** the Owner disables the `propose` tool
- **THEN** new proposals are refused from the moment the switch is saved

#### Scenario: Owner disables one task type
- **WHEN** the Owner disables an individual task type
- **THEN** new proposals of that task type are refused from the moment the switch is saved

#### Scenario: Existing access stops within 60 seconds
- **WHEN** a kill switch is saved
- **THEN** existing tokens, streams and queued tool calls stop within 60 seconds (AI-38, AI-43)

#### Scenario: Token budgets with alerts
- **WHEN** token use reaches the organisation's or a user's token budget
- **THEN** an alert is raised

### Requirement: AI-14 Rejection reasons fed back
A rejection SHALL carry a reason, which SHALL be fed back into the conversation so the AI can revise.

Source: PRD-001 §9.5 · Priority S · Phase P1

#### Scenario: Rejection reason returns to the conversation
- **WHEN** a human rejects a proposal with a reason
- **THEN** the reason is fed back into the conversation
- **AND** the AI can revise the proposal

### Requirement: AI-30 Approval of the AI's work does not replace business approval
Approving the AI's work SHALL NOT replace business approval: an executed proposal SHALL create or change records that then follow the normal approval policies.

Source: PRD-001 §9.5 · Priority M · Phase P1

#### Scenario: An AI-drafted bill still needs its business approvals
- **GIVEN** a bookkeeper accepted an AI proposal that drafted a bill
- **WHEN** the proposal has executed
- **THEN** the bill still needs the bill approval and the payment controls

### Requirement: AI-31 Undo an executed proposal
A human SHALL be able to reverse an executed proposal in one action while its period is open, under the same approval policy as the original, and the undo SHALL be audited.

Source: PRD-001 §9.5 · Priority S · Phase P1

#### Scenario: Undo posts reversals and restores master data
- **GIVEN** an executed proposal whose period is open
- **WHEN** a human undoes it in one action
- **THEN** the platform posts reversing entries and restores the previous master-data versions

#### Scenario: Undo follows the original approval policy
- **WHEN** an undo is requested
- **THEN** it needs approval under the same approval policy as the original proposal

#### Scenario: Undo is audited
- **WHEN** an undo is executed
- **THEN** it is audited

#### Scenario: No one-action undo once the period is closed
- **GIVEN** the executed proposal's period is no longer open
- **WHEN** a human looks for the one-action undo
- **THEN** it is not available

### Requirement: AI-32 Task types off by default; bulk approval for tier 1 only
Every task type SHALL be off by default and enabled per organisation by the Owner under dual control, and bulk approval SHALL be allowed only for tier-1 task types.

Source: PRD-001 §9.5 · Priority M · Phase P1

#### Scenario: Task types start disabled
- **GIVEN** a new organisation
- **WHEN** the AI tries to propose any task type
- **THEN** it cannot, because every task type is off by default

#### Scenario: The Owner enables a task type under dual control
- **WHEN** the Owner enables a task type for the organisation
- **THEN** the change needs dual control

#### Scenario: Bulk approval for tier 1
- **GIVEN** a tier-1 proposal with several items
- **WHEN** the approver approves all items in one action
- **THEN** the bulk approval is allowed

#### Scenario: No bulk approval above tier 1
- **GIVEN** a tier-2 or tier-3 proposal with several items
- **WHEN** the approver tries to approve all items in one action
- **THEN** the bulk approval is not allowed

### Requirement: AI-33 Approval-quality metrics
The system SHALL measure approval quality per task type and approver, to guard against rubber-stamping (per IMDA's agentic-AI framework).

Source: PRD-001 §9.5 · Priority S · Phase P2

#### Scenario: Metrics per task type and approver
- **WHEN** approval-quality metrics are produced
- **THEN** they show the approval rate, the edit-before-approve rate, the median time to decide and the reversals after approval, per task type and approver

#### Scenario: Monthly review in Founder mode
- **GIVEN** Founder mode
- **WHEN** a month ends
- **THEN** the approval-quality metrics are reviewed

### Requirement: The orchestrator holds no write credential
The AI orchestrator SHALL run the tool loop with an on-behalf-of token, hold no database write credential and have no permission to call domain commands; the proposal service SHALL be the only place AI output can land.

Source: PRD-001 §9.3

#### Scenario: Reads on behalf of the user
- **WHEN** the orchestrator calls a read tool
- **THEN** it passes an on-behalf-of token, and the read API applies the user's permissions minus AI exclusions

#### Scenario: No write path from the orchestrator
- **WHEN** the orchestrator tries to write to the database or call a domain command
- **THEN** it has no credential or permission to do so

#### Scenario: Proposals validated and dry-run before storage
- **WHEN** the proposal service receives a `task_type` and parameters
- **THEN** it validates them against the catalogue's schema and the domain validators
- **AND** performs a dry run (the domain command in a rolled-back transaction or a pure simulation)
- **AND** stores the proposal with its computed impact

### Requirement: The executor runs approved proposals as the approving human
A separate proposal executor identity SHALL run only approved proposals, re-validate them against current data, check the dry run's base versions, and execute the frozen payload as the approving human, with the AI recorded as originator.

Source: PRD-001 §9.3

#### Scenario: Only approved proposals execute
- **WHEN** a proposal has not been approved
- **THEN** the executor does not run it

#### Scenario: Stale base versions block execution
- **GIVEN** a record read by the dry run has changed since
- **WHEN** the executor checks the base versions
- **THEN** the proposal does not execute

#### Scenario: Executed as the approver
- **WHEN** an approved proposal executes
- **THEN** the change is made as the approving human
- **AND** the AI is recorded as originator
