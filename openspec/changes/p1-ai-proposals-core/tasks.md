## 1. The catalogue

- [x] 1.1 `contracts/ai/tasks/` and `contracts/ai/tasks.lock.json`; `internal/proposal`'s loader with the strict schema
  rules, the limits, the tiers and approver roles, and the never-list refused by command and operation, whatever a file
  says (design D1). Tests `// req: ai/task-catalogue Catalogue as versioned, dual-controlled data`,
  `// req: ai/task-catalogue Catalogue entry attributes`, `// req: ai/task-catalogue Risk tiers and approver roles`,
  `// req: ai/task-catalogue Never in the catalogue`, `// req: CTL-AI-04`.
  As built: the catalogue ships empty; the never-list matches the words of a type's ID and of its command, and the
  proposal service checks it again by name as each proposal is made (task 3.1); an ID is at most 56 characters, so its
  tool `propose_<id>` fits the 64 characters providers allow.
- [x] 1.2 The setting `ai.task_types` (dual controlled, off at once when emptied) and the AI catalogue's versioning in
  the release record. Tests `// req: AI-32`, `// req: platform/release-and-versioning AI catalogue`.
  As built: a new kind `task_types`; each ID must be in the release's catalogue; taking types off, or all of them,
  applies at once (`settings.Narrows`), and any change that adds one waits for the second approver. The settings table
  refused an empty value, so emptying or resetting a list (`ai.exclusions` too, a latent bug) failed, and 200 characters
  held few task types: migrations 000373/000374 let the two lists be empty and run to 4,000, every other setting
  unchanged. The release record is the evals' report: it names each read tool, task type and system prompt in use at
  its version and hash.

## 2. The proposal store

- [x] 2.1 Migration: `ai_proposal` with its lifecycle guard, FORCE RLS, the API's, the approval engine's and the
  executor's column grants, nothing for the AI's role, the export catalogue and goldens (design D2). Tests
  `// req: ai/proposals Proposal lifecycle`, `// req: ai/proposals Proposal content and payload hash`,
  `// req: ai/proposals Proposal rationale, evidence and dry-run impact`,
  `// req: ai/proposals Proposal tier, originator, approvals and execution record`, an attack test on the guard.
  As built (migration 000375, port `AIProposals`): the payload is kept as its exact JSON text, and the database checks
  its SHA-256 and that of what was approved (a batch's subset). The guard names the role of each step: the API's
  (fyniq_api, the approval engine included), the worker's (expiry) and the executor's; it refuses a step out of the
  lifecycle, a change to what was proposed or decided, and a delete (FY342). `approvals[]` are the approval engine's
  signatures, reached by `approval_request_id` (task 4.1), not copied. The turn is kept by ID without a foreign key, so
  the conversation's purge after retention runs and the proposal keeps its record; the data inventory gains the class
  `ai-proposals`.

## 3. Proposing

- [x] 3.1 `POST /internal/v1/proposals` with the turn's token and its `propose` capability: the switch, the catalogue,
  the enabled flag, the schema and limits, the requester's right, then the dry run in a rolled-back transaction, its
  impact and base versions; Invalid with a reason (design D3). Tests `// req: AI-P3`, `// req: AI-01`.
  As built (`proposeAITask`): the capability is the grant's `propose`, set as it is issued while a task type is on
  (migration 000376), and the scope `ai.propose`, which no public operation reads. A refusal of the turn, the task type
  or the switch keeps nothing; a payload its schema or limits refuse, a person without the right, screening's finding in
  what the model wrote (screened as an answer) or a dry run that refuses keeps an Invalid proposal with why. The dry run
  is the store's preview, rolled back after its deferred checks. Each task type's command (`proposal.Command`: its
  policy, measure and dry run) arrives with it; a test-only command exercises the path. The approval request attaches in
  task 4.1.
- [x] 3.2 The AI service's `propose_<type>` tools for the enabled types, relaying the proposal or its refusal (design
  D7). Tests `// req: AI-P3`, `// req: ai/proposals The orchestrator holds no write credential`.
  As built: the API tells the AI service the turn's task types (those on that the release can propose); the model call
  names them, and the API offers each as its `propose_<type>` tool from the catalogue's definition (payload, rationale,
  evidence), only to a turn whose token may propose. The orchestrator relays the call and tells the model that nothing
  changed until a person approves. The system prompt is `chat@3`: the AI still changes nothing itself, and with a
  propose_ tool proposes only what the person asked for, never because a record's text asks (its recordings are
  [live-verify] with the rest).

## 4. Approval

- [ ] 4.1 The approval engine's object type `ai_proposal` and operation `ai_proposal.execute`: the envelope signing the
  payload hash, the built-in policy, the requester as preparer, step-up for tiers 2 and 3, no policy without a step or
  with an automatic rule in any team mode, and the final approval enqueuing the executor (design D4). Tests
  `// req: AI-P4`, `// req: AI-P5`, `// req: AI-06`, `// req: AI-10`, `// req: CTL-AI-03`.
- [ ] 4.2 Partial and bulk approval of a tier-1 proposal's items (the subset re-hashed and signed); a rejection's reason
  returned to the conversation or the run (design D8). Tests `// req: AI-32`, `// req: AI-14`.
- [ ] 4.3 The public operations: list and read proposals (each member their own and those they may approve; the Owner
  and the DPO every one), the summary from platform code, the impact, the diff and the evidence, the rationale apart
  (design D5); the authorisation matrix's rows. Tests `// req: AI-07`, `// req: ai/task-catalogue AI tasks page`.

## 5. The executor

- [ ] 5.1 `cmd/executor`'s kind `ai_proposal.execute`: the hash re-checked, the base versions compared (Stale), the
  command run as the approving human with the AI as originator, one transaction, idempotent by proposal ID, Failed on a
  validation or lock error; a test-only fixture task type exercises it (design D6). Tests `// req: AI-11`,
  `// req: ai/proposals The executor runs approved proposals as the approving human`, `// req: AI-30`.
- [ ] 5.2 The worker's job `ai.proposals.expire` and the check C.2 #19 (design D9). Tests
  `// req: ai/proposals Proposal lifecycle`, `// req: C.2 #19`.

## 6. The screens

- [ ] 6.1 The approval page's review of an `ai_proposal` (`ai-proposal-review`): summary, items, impact, diff, evidence,
  the AI's rationale in violet with the AI badge, approve, or reject with a reason; help keys; the guide
  `docs/user-guide/ai-proposals.md` compiled into `kb/`; the fragment updated locally (design D10). Tests
  `// req: AI-07`, `// req: AI-22`, Vitest with axe.
- [ ] 6.2 Settings → AI → AI tasks (`ai-tasks`): the catalogue with tiers, limits and status, enabling under dual
  control, and the proposal history; help keys, its guide section, the fragment updated locally. Tests
  `// req: ai/task-catalogue AI tasks page`, `// req: AI-32`, Vitest with axe.

## 7. Evals and controls

- [ ] 7.1 The `proposal` suite kind in `evalsuite` and `TestTheAssistantSuites`, with fixture cases on the test-only
  type and the soundness check (design D11). Test `// req: QA-AI-02`.
- [ ] 7.2 `verify ai.proposals-approved` nightly; `compliance/controls/CTL-AI-02.yaml`, `CTL-AI-03.yaml` and
  `CTL-AI-04.yaml` running (design D12). Tests `// req: CTL-AI-02`, `// req: CTL-AI-03`, `// req: CTL-AI-04`.

## 8. The finish

- [ ] 8.1 `docs/security/threat-models/ai-proposals.md` for security review #4 (the AI write path, the payload hash, the
  executor's identity, approval fatigue, injected proposals, the never-list); red-team cases for proposals; the build
  plan's paragraph; `make check`, `make test` and `make evals` pass.
