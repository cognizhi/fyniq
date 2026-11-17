# Threat model: AI proposals

| | |
|---|---|
| Feature and requirement IDs | The AI's write path: the task catalogue and its never-list, the task types turned on per organisation, proposals made by a person's turn, their checks and dry run, their approval request, approving some items or all, rejecting with a reason fed back, the executor, expiry, the proposal history, the AI tasks page, the proposal evals and the controls: AI-P3 to AI-P5, AI-06, AI-07, AI-10, AI-11, AI-13, AI-14, AI-22, AI-30, AI-32, AI-89, QA-AI-02, CTL-AI-02 to CTL-AI-04, C.2 #19 (PRD-001 rev 0.279 and 0.280; `p1-ai-proposals-core`) |
| Author, reviewer, date | Claude Code for the owner, 15 Nov 2026, for security review #4; second review waived before production (AGENTS.md), held with `needs-human-review` if wanted |
| Data and its class | Proposals: the payload that executes, its hash, the AI's rationale and evidence, the dry run's impact (confidential: business records, which may name people); approval requests and signatures (confidential); the turn's delegated token (secret); the AI service's token (secret); the executor's identity (secret) |
| Who acts | The model, which writes a proposal's payload and reasons; the AI service, relaying them as the person's turn; the API, checking, dry-running and keeping them; the person who asked; the approvers the task type names, each holding the command's right as a person; the Owner and the DPO, who see every proposal; the worker, expiring proposals; the executor, running approved ones; MCP and integration clients (none of these operations) |

## 1. What we are building

**A closed catalogue** (AI-32, AI-06, design D1). The release's task types are data in `contracts/ai/tasks`, each a
JSON Schema of its payload, a command, a tier, limits, approver roles and an expiry, pinned by hash in
`tasks.lock.json`. A task type whose ID or command names anything on §9.6's never-list is refused as the catalogue
loads and again as each proposal is made: approving, payments, payee bank details, users and roles, policies and the
AI's own settings, statutory parameters, reopening, voiding, filing, exports, the audit trail, equity and a director's
balance, officers and registers, and an obligation marked met. Each organisation turns task types on through the
setting `ai.task_types`, only with a second approver. Turning one off takes effect at once.

**Proposals, not writes** (AI-P3, AI-01, design D3). A turn may propose only when a task type is on. Its delegated
token then carries `ai.propose` and the model is offered `propose_<type>` tools for the types that are on, no others.
The API keeps each proposal as a Draft. It then runs these checks:
- the payload against the task type's schema and limits;
- the person's own right to do it;
- screening of what the model wrote;
- a dry run as the person, rolled back.
The proposal becomes Pending with Fyniq's summary, or Invalid with why. The AI's database role writes nothing else
(CMP-AI-01).

**A person approves exactly what runs** (AI-89, AI-P4, AI-10, AI-14, design D4, D5). A Pending proposal's approval
request is made in the same transaction:
- its envelope signs the payload's hash;
- its one step names the task type's approver roles, and each signer must also hold the command's right as a person;
- the person who asked may sign their own tier-1 proposal;
- no organisation may change the policy.
An approver may keep some items of a batch; the signature then names the selection's hash, and above tier 1 always
names the items. A rejection needs a reason, which the assistant reads with the person's next question, screened.

**The executor runs it once, as the approver** (AI-11, AI-P5, AI-30, design D6). `cmd/executor` runs the approved
payload under its own database role. It runs only when the proposal is Approved and its approved payload hashes as
approved. The request must be approved and signing the payload's hash, under the task type's version and hash, and the
approver must still hold the right. One unit of work covers it and the proposal's Executed. A record that moved since
the dry run makes it Stale; a refusal makes it Failed. It is audited as the approver, on behalf of the person who
asked, with the proposal as originator.

**Expiry, oversight and evidence** (§9.5, C.2 #19, CTL-AI-02). An undecided proposal expires after its 72 hours, its
request with it. Checks flags one waiting over 48. `verify ai.proposals-approved` reconciles every executed proposal
nightly with a person's approval of it.

Trust boundaries: model ↔ AI service (the payload and reasons the model writes); AI service ↔ API (the internal
propose operation, two tokens); API ↔ database (the guard, grants, RLS); approver's browser ↔ API (the review, the
signature); API ↔ outbox ↔ executor (the queued run); the edge ↔ the internal path.

## 2. What can go wrong (STRIDE per boundary)

| # | Boundary or step | Threat (STRIDE) | Impact | Likelihood |
|---|---|---|---|---|
| T1 | Records → model → proposal | Tampering / elevation: an injected proposal. A memo, bank line or name tells the model to propose a change the person did not ask for, a type that is off, or one on the never-list | A change the person never meant waits for approval, or worse runs | High |
| T2 | Proposal → approver | Tampering / spoofing: the review misleads. The AI's reasons pass for Fyniq's, the summary differs from what runs, or what is shown is not what is signed | A person approves something other than what executes | Medium |
| T3 | Approval → executor | Tampering: the payload swapped after approval, a selection widened, or a proposal run twice, under another task type's version, or after its approver lost the right | What runs is not what was approved | Medium |
| T4 | Inbox | Elevation (social): approval fatigue. A flood of proposals, or routine ones approved without reading | A wrong change approved by habit | Medium |
| T5 | AI and MCP → approval | Elevation: the AI or an MCP client approves, rejects or executes; the AI's tokens sign; the requester signs above tier 1; a policy automates approval | Golden rule 5 broken | Low |
| T6 | Catalogue and settings | Tampering / elevation: a never-list task type added to the catalogue, a task type changed without a new version, or one turned on without a second approver | The AI gains a write it must never have | Low |
| T7 | Executor identity | Elevation: the executor's role used beyond running approved proposals, or the API running a proposal itself | A write outside the approval path | Low |
| T8 | Proposal history | Information disclosure: another member's proposals, rationale or payloads read; an excluded value written into a payload | Business or personal data exposed | Medium |
| T9 | Database | Tampering / repudiation: a proposal's payload, hash, status or approver changed after the fact; an executed proposal with no approval; the audit of who approved lost | The record of the AI's writes cannot be trusted | Low |
| T10 | Rejection feedback | Tampering: a rejection reason that injects instructions into the next turn | The next answer manipulated | Low |
| T11 | Proposal stuck | Denial of service: a proposal never decided blocks work; expiry fails silently | Work waits without anyone knowing | Low |

SOD: the person who asked never signs a proposal above tier 1; each signer holds the command's right as a person; the
executor runs as the approver and never as the AI. RLS: `ai_proposal` has `organisation_id` and
`FORCE ROW LEVEL SECURITY`.

## 3. What we do about it

| Threat | Control | Test that proves it |
|---|---|---|
| T1 | A revision may replace only the person's own proposal of the same type that still waits, and it waits for approval in turn; the one it replaces is Superseded and its request void, so nothing is approved by being replaced. A turn is offered `propose_<type>` tools only for the types that are on. A call to any other is refused as an unknown tool, and nothing is kept. The never-list can never be in the catalogue. The model's answer, its tool inputs included, is screened as it arrives, and the API screens a proposal's words again. Every proposal is dry-run as the person, within the type's schema and limits. The person must hold the right themselves. Nothing runs without a person's approval | `TestAttackInjectedProposals`, `TestARevisionSupersedesTheProposalItReplaces`, `TestAPersonsTurnProposesThroughTheAIService`, `TestTheModelProposesOnlyWhatTheTurnMay`, `TestTheAIProposesAndNothingChanges`, `TestNothingOnTheNeverListCanBeProposed` |
| T2 | The review shows Fyniq's summary from the dry run, never the AI's words. The AI's reasons sit apart, in violet with the AI badge. The envelope signs the payload's hash, and the signature the selection's. The confirmation says which items it covers | `TestAProposalWaitsForAPersonsApproval`, `TestAnApproverApprovesSomeOfABatch`, the review's Vitest cases ("shows Fyniq's summary apart from the AI's reasons", "approves only the items kept") |
| T3 | The guard keeps the payload and its hash fixed. The approved payload must hash as the request's signature or the payload. The executor rechecks all of these before it runs: the hash; the request, approved and signing the payload's hash; the task type's version and hash; and the approver's right. It runs once, in one unit of work. Stale or Failed changes nothing else | `TestTheExecutorRunsWhatWasApproved`, `TestAttackAIProposalsKeepTheirCourse`, `TestASubsetOfABatchIsRehashed`, `TestAPayloadIsKeptAsItsCanonicalJSON` |
| T4 | A turn's propose calls count toward its reads (8 by default, at most 20). An answer past them makes none. Each type has a most items and amount. Above tier 1, each item approved is named. Checks flags a proposal waiting over 48 hours. A proposal expires after 72 hours. Turning a type off takes effect at once | `TestAttackInjectedProposals` (the flood), `TestAPayloadKeepsToItsLimits`, `TestAProposalWaitingTooLongIsFlagged`, `TestAProposalUndecidedExpires`, `TestTaskTypesAreOffUntilEnabledUnderDualControl` |
| T5 | `approverOf` refuses any AI principal. The approvals operations are a person's alone. The AI's database role writes nothing but proposal inserts. The requester's MFA `ai_turn` and the self-signature exception are `ai_proposal`'s alone, at tier 1. The ai_proposal policy is fixed in every team mode (CTL-AI-03) | `TestAttackInjectedProposals` (the AI's tokens), `TestAttackTheProposalsExceptionsAreTheirsAlone`, `TestAProposalWaitsForAPersonsApproval`, `TestTheAIRoleHoldsNoWritePrivilege` |
| T6 | The catalogue loads strictly. A changed task type without a new version fails CI (`tasks.lock.json`). The never-list is in code. Turning a type on waits for a second approver (CTL-AI-04) | `TestTheCatalogueLoadsStrictly`, `TestEveryChangeToATaskTypeIsANewVersion`, `TestNothingOnTheNeverListCanBeProposed`, `TestTaskTypesAreOffUntilEnabledUnderDualControl` |
| T7 | The executor role may only take a proposal from Approved to Executing, then Executed, Stale or Failed. It reads memberships and roles for the check, nothing more. The API only queues the approved proposal and never runs it | `TestAIProposalsMoveThroughTheirLifecycle`, `TestAttackAIProposalsKeepTheirCourse`, `TestEachRoleHoldsExactlyItsGrants` |
| T8 | A person sees the proposals made for them and those they may approve; the Owner and the DPO see all. Reads are on the AI activity row. The AI never reads an excluded value, so it cannot write one. A payload is screened, and a flagged one is Invalid | `TestTheProposalsAsPeopleReviewThem`, `TestTheAIProposesAndNothingChanges` |
| T9 | The guard refuses any step but the lifecycle's, by the right role: no changed payload, hash or approver, and no delete. Every step is audited in its transaction. `verify ai.proposals-approved` checks each executed proposal nightly. It needs an approved request of its own that signs the payload's hash. What was approved must hash as approved. A person must have approved it, and it must have run as one of them. No approval of it may be audited for the AI (target 0, CTL-AI-02) | `TestAttackAIProposalsKeepTheirCourse`, `TestEveryExecutedProposalHasAPersonsApproval`, `TestEachVerificationFindsItsBreach` |
| T10 | A rejection's reason is screened before it reaches the next turn. A flagged one is withheld in neutral words | `TestARejectionReachesTheNextTurn` |
| T11 | The worker expires undecided proposals every 15 minutes; the database voids their requests in the same transaction, audited. C.2 #19 flags one waiting over 48 hours. The job's slot raises the absence alert if it stops | `TestAProposalUndecidedExpires`, `TestAProposalWaitingTooLongIsFlagged` |

The proposal evals (QA-AI-02) judge each case's one proposal by its task type and payload. A fixture suite on the
tests' own type checks the harness: a right proposal passes, and a turn that proposes nothing fails
(`TestTheAssistantSuites`, `TestTheJudgeOfAProposal`).

## 4. What remains

- **The release's task types.** The core ships the path and a fixture type only. Each task type adds these:
  - its command and the executor's grants for it (`proposal.Release`);
  - its eval suite;
  - its red-team cases.
  The company secretary's `sec.create_task`, `sec.draft_reminder` and `sec.draft_document` come first, with
  `p1-scheduled-agents` and `p1-company-secretary`.
- **Approval quality.** The approval rate, edits before approval, time to decide and reversals measure approval
  fatigue. They arrive with `p1-ai-proposals` after go-live. Until then the waiting check, expiry and the limits stand
  in.
- **Two approvers for tier 3, and undo.** Tier 3 is reserved: no task type may use it. Undo arrives with
  `p1-ai-proposals`.
- **An injection that stays within the person's rights.** A turned model can propose only a type that is on, within
  its limits, as something the person may do. A person still reads Fyniq's summary and decides.
- **Model Armor.** In a release, screening of the model's answers and of a proposal's words runs through Model
  Armor's template in Singapore. Offline, the local stub with the red-team corpus stands in `[live-verify]`.
