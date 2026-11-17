## Context

`p1-ai-assistant` built the AI service (`cmd/ai`), which reads as the person through the API's internal operations with a
delegated read token per turn and writes nothing (CMP-AI-01, golden rule 5). The approval engine (`internal/domain/approval`,
`p1-approval-engine`) signs envelopes of object types and runs an operation on the final approval. `cmd/executor` is a P0
shell with its own service identity, its own outbox class (`storage.ClassExecutor`) and its own database role
(`fyniq_executor`), but no kinds. The catalogue's rows exist in the main specs (`ai/task-catalogue`, Appendix E). The
skill `fyniq-ai-proposals` states the target conventions. Rev 0.279 brings the machinery to launch; the task types come
with what they act on.

## Goals / Non-Goals

**Goals:**
- One AI write path: a catalogue proposal, validated and dry-run when proposed, approved by a named person, and executed
  as that person by the executor.
- Everything closed by default: no task type enabled, no proposal tool offered, no approval automatable.
- Built so each task type is a file, a domain command, its dry run, its summary, its evals and its grants, added by the
  change that builds what it acts on.

**Non-Goals:**
- Any task type of Appendix E (`sec.*` with `p1-scheduled-agents` and `p1-company-secretary`, the others with
  `p1-ai-proposals`).
- Tier 3 and two approvers (AI-09), undo (AI-31) and approval-quality metrics (CTL-AI-07), which follow after go-live.
- Proposals from MCP and agent-class clients (`p1-mcp-first-party`, AI-34).

## Decisions

**D1. The catalogue is files in the release; enabling is the organisation's (§9.6, AI-32, CTL-AI-04).**
- One `contracts/ai/tasks/<id>.json` per task type, in the shape the read tools use. Each file holds: ID, version,
  name, description (shown to the model and the approver), a strict input JSON Schema (closed, bounded, decimal
  strings for money), the domain command, tier, limits, approver roles and expiry (72 hours).
- `contracts/ai/tasks.lock.json` pins each version by SHA-256, as `tools.lock.json` does. A change to a file is a new
  version, through a reviewed pull request and the AI change gate (CMP-AI-03): that is the dual control of SOD-8.
- `internal/proposal` loads the catalogue and refuses at load, and again when proposing, a type on the never-list:
  approve, reject, pay, payee bank details, users, roles, team mode, policies, AI catalogue or settings, statutory
  parameters, reopening, voiding or deleting, filing, tenant export, the audit trail. The check is by the command's
  name and by the operations its command maps to.
- Each organisation enables a type by the setting `ai.task_types` (a list, dual controlled, off at once when emptied),
  as `ai.payroll_access` is.
- Alternative: the catalogue as rows an Owner edits. Rejected: a task type is code (its command, dry run and summary),
  so its definition ships with it; the organisation only switches it on.

**D2. The proposal store (§9.5).**
- Migration: `ai_proposal` keeps:
  - what it is: task type and version, payload, SHA-256 hash, items;
  - why: rationale, evidence references, impact and base versions;
  - who and how: tier, required approvals, originator (AI principal, requester, conversation and turn or agent run,
    model, prompt and tool versions), the approval request, status, reason, execution record and expiry.
- Every row is append-only in what it proposed. A guard trigger lets status move only along the lifecycle's
  transitions, keeps the payload and its hash fixed once Pending, and refuses deletion. FORCE RLS, audited in its
  transaction (AUD-01).
- Grants:
  - the API's role inserts Drafts and moves Draft → Invalid / Pending, Pending → Superseded / Expired;
  - the approval engine moves Pending → Approved / Rejected;
  - the executor's role moves Approved → Executing → Executed / Stale / Failed and writes the execution record;
  - the AI's role holds nothing (CMP-AI-01).
- A proposal of several items records each item; a partial approval approves the chosen subset, whose payload is
  re-hashed and signed (tier 1 only, AI-32).

**D3. Proposing is an internal operation with the turn's token (AI-P3, AI-01).**
- `POST /internal/v1/proposals` takes the AI service's own token and the turn's delegated token. It checks, in order:
  the AI switch; the task type in the catalogue, not on the never-list, and enabled; the payload against its schema
  and limits; and the requester's own right to what the command does (Appendix D).
- It then dry-runs the command in a rolled-back transaction (the storage preview the public API already uses for
  `Prefer: return=preview`) and stores the impact and base versions. Draft becomes Pending, with its approval request.
  A failure is Invalid with the reason in plain words.
- The delegated grant gains one capability, `propose`, for that turn only and only when a task type is enabled. It
  permits creating proposals and nothing else: no approval, no execution, no other write.
- Alternative: the AI service inserts proposals itself. Rejected: it would hold a database credential (CMP-AI-01) and
  could skip the checks.

**D4. A proposal is an approval request (AI-P4, AI-P5, AI-06, AI-07, AI-10, AI-30, AI-32).**
- A new object type `ai_proposal`, with the operation `ai_proposal.execute`. Its envelope holds the organisation, the
  proposal and its version, the task type and version, the payload hash, the tier and the item count, so the
  signature signs exactly what will execute.
- Its built-in policy is one step of the task type's approver roles; the requester counts as preparer (AI-10), and
  tier 1 lets the requester approve.
- The approval engine refuses any policy for `ai_proposal` with no step or an automatic rule, in every team mode
  (AI-P4, CTL-AI-03). Step-up is required for tiers 2 and 3.
- On the final approval the operation marks the proposal Approved and enqueues its execution in the executor's own
  queue, in the same transaction. The API never executes it.
- A rejection's reason is kept on the proposal and returned to its source (D8).
- Alternative: a separate proposal approval mechanism. Rejected: two approval engines would drift; signed envelopes,
  team modes, step-up and the review log are already the engine's.

**D5. The summary is platform code (AI-07, G4-095).**
- Each task type supplies a summariser from its payload and its dry-run impact; the review shows that summary, the
  items, the impact (ledger lines, balances, checks, document diff) and the evidence links.
- The model's rationale is shown apart, violet with the AI badge, as AI text.
- No model text reaches the summary.

**D6. The executor (AI-11, "The executor runs approved proposals as the approving human").**
- `cmd/executor` gains the kind `ai_proposal.execute`. It:
  1. locks the proposal and refuses unless it is Approved with the envelope's hash equal to the stored payload's
     (AI-P5);
  2. moves it to Executing and compares the base versions with today's (Stale on any change);
  3. runs the task type's command as the approving human, with the AI recorded as originator and the requester as
     on-behalf-of;
  4. marks it Executed with the records it made, in one transaction, idempotently by proposal ID.
- A validation or lock error is Failed with the reason. Locks, policies and validations hold as for any write (AI-11).
- The executor's role gains each task type's grants with that type, never broader.
- Business approval still applies (AI-30): a draft the executor creates follows its own policy.

**D7. The proposal tool in the AI service (AI-P3).**
- When the turn's grant carries `propose`, the AI service offers the model one `propose_<task type>` tool per enabled
  type, with that type's schema, built from `contracts/ai/tasks`. It calls the internal operation and relays the
  result as a tool result: the proposal's ID and its summary, or why it is Invalid.
- The read-tool registry stays read-only (`TestTheAIToolsAreReadOnly`): proposal tools are not read tools, and the
  AI service never sees an approval.

**D8. Rejections feed back (AI-14).**
- A rejected proposal's reason is shown, quoted and labelled, as context to the next turn of its conversation, or to
  its agent's next run (for `p1-scheduled-agents`).
- The AI may then propose a revision, which supersedes nothing already decided.

**D9. Expiry and the check (C.2 #19).**
- The worker's job `ai.proposals.expire` (every 15 minutes) moves Pending proposals past `expires_at` to Expired and
  voids their approval requests, audited.
- The checks engine adds C.2 #19, "AI proposals pending approval older than 48 hours" (⚠).

**D10. Screens (`ai-proposal-review`, `ai-tasks`).**
- The approval page shows an `ai_proposal` request as the review: the platform summary, the items (each selectable
  for tier 1), the impact and diff, the evidence, the AI's rationale, and approve, or reject with a reason.
- Settings → AI → AI tasks lists the catalogue: each type's tier, limits and status, and enabling it, which is
  dual controlled. It also shows the proposal history, filterable by state and linking to what each one changed.
- The fragments are updated locally.

**D11. Evals (QA-AI-02).**
- `evalsuite` gains a `proposal` kind. A case names the question and the proposal a right answer makes: its task type
  and the payload fields it must hold.
- `TestTheAssistantSuites` judges them through the real orchestrator, as for the golden set, with the same soundness
  check.
- The core ships the harness and fixture cases on a test-only task type; each task type adds its own suite.

**D12. Controls (CTL-AI-02, CTL-AI-03, CTL-AI-04).**
- `verify ai.proposals-approved` runs nightly: every proposal Executed in the window has an approved request whose
  envelope hash equals its payload hash, a named human signer and no signer that is an AI principal (target 0
  exceptions).
- CTL-AI-03 is a policy-engine test; CTL-AI-04 is the catalogue's dual control and never-list tests.

**D13. Audit and the activity log (AUD-01, AI-12).**
- The events are `ai_proposal.proposed`, `.invalid`, `.pending`, `.superseded`, `.expired`, `.executed`, `.stale` and
  `.failed`, plus the approval engine's own decisions.
- The AI activity log lists each proposal with its turn or run.

## Risks / Trade-offs

- [The AI write path opens at launch] → closed by default at every layer: no type enabled, no tool offered without one,
  the never-list in code, a human signature over the exact payload hash, a separate executor identity, nightly
  reconciliation. Threat model `docs/security/threat-models/ai-proposals.md` for security review #4.
- [Approval fatigue: many tier-1 proposals] → bulk approval for tier 1 only, C.2 #19 for the stale queue, and the summary
  in plain words; approval-quality metrics follow (CTL-AI-07).
- [A dry run that does not match execution] → base versions block a changed world (Stale), and the executor runs the
  same command as the dry run.
- [No real task type in the core] → a test-only fixture type exercises every path; the first real types arrive with
  `p1-scheduled-agents` immediately after.

## Migration Plan

Additive: new tables, grants, a setting and kinds. Nothing is enabled. Rolling back removes the kinds and the tool; the
proposals stay as records.

## Open Questions

- None for the owner. The task types' approver roles are Appendix E's (Owner, Company secretary for `sec.*`).
