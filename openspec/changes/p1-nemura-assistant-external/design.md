# Design

## Decisions

Decided on the recommended option by the builder while the owner is away (20 Nov 2026), each recorded with why; the owner may reverse any.

### D1. Nemura's assistant is Nemura's own code

| Option | Pros | Cons |
|---|---|---|
| **A. A small `internal/nemura/assistant` of Nemura's own: catalogue, never-list, tools, grounding, a model port** | Respects ADR-N1 and the archtest (Nemura imports nothing of Fyniq's assistant); one image can leave the repository; the people never-list differs from the ledger's | The rules exist twice, so a conformance test must keep them alike |
| B. Share Fyniq's `internal/assistant` and `neverlist` through `NemuraShared` | One implementation | Drags the ledger's tools and catalogue into the people image, and widening the shared list is a reviewed change to the boundary |

**Chosen: A.** The two never-lists are tested against the same PRD sentences: Nemura's refuses everything NM-AI-04 names, and a test fails if a word of PRD-001 §9.6 that concerns money movement or the audit trail is missing from it.

### D2. Off by default, and the route is evidence

The assistant is off for an organisation until its Owner turns it on in AI settings, with the audit event of the change. There is one model route in this change: local mode's scripted model, which reads the question and the tool results and answers from templates, so every evaluation runs offline. A real route (a model in Singapore, AI-88) is configuration the cloud must evidence `[live-verify]`; until then the release build has no route and answers "the assistant is not available" in words.

### D3. The person asking is the only authority

Every read tool runs in the asking person's session, through the same row-level security and role checks as the web app, so the assistant can read exactly what the person can and a manager's question is limited to their reports. The assistant's own database role can insert proposals and read nothing of the people tables: the runtime holds no table grant, it calls the tools through the API as the person. A request's text is data: it is wrapped as untrusted, never read as an instruction, and a tool call or a proposal that the never-list refuses is refused whatever the text says.

### D4. Own pay: rules, not figures

By default the assistant explains a payslip line from the pay item's rule (what the line is, how it is worked out, which pack rule applies) and the app shows the person's own figures beside it; no pay figure reaches a model. Own-pay explanations, which send only the asking person's own payslip figures on a route allowed for payroll data, need the Owner and a second approver; the second approver arrives with the Fyniq link (`p1-nemura-fyniq-link`), so until then the setting cannot be turned on and says so.

### D5. Proposals, executor, expiry

`nemura.ai_proposal` rows are inserted by `nemura_assistant` (insert-only), decided by a human with the task's approver role (a tier 1 draft: the asker may decide it, as AI-10 allows; both are audited), and applied by `nemura_executor` through the letters domain commands (a draft letter request; a reply draft on a flexible-work request) only after approval. A proposal expires after 72 hours and an expired one cannot be approved. The approved text is what the human saw and edited: the executor applies the stored final text and its hash, never a regenerated one.

### D6. External systems stay Fyniq's

NM-EXT-01 to 03 are decided on the Fyniq side (FR-PPL-01, 05; the import is idempotent per period and version, a draft that an Accountant approves). The Nemura side is that a feature an external system holds is off in Nemura (NM-FEAT-01 to 05), which `p1-nemura-platform` built; this change adds the test that a switched-off feature's assistant tools are absent too.

## Risks

- A prompt injection in a request's text (a flexible-work reason, a letter's purpose) that asks for approval, a pay figure or another person's record: the red-team suite carries each and must see a refusal and no proposal.
- The rules drifting between the two apps: the conformance test in D1 and the shared PRD sentences.
- A model route that stores data outside Singapore: none is configured until evidenced (AI-88).

## Decisions made while building slice 1

- **D7 The approver of both P1 drafts is the HR team for now.** The PRD lets a line manager decide a flexible-work reply; that needs the manager's scope on the subject person (a database-checked relation), so slice 3 adds it with the draft. Until then the HR officer, the HR administrator and the Owner decide both (`MayDecide`), which is the narrower set.
- **D8 The runtime holds no read grant.** `nemura_assistant` can insert a proposal and its audit event and read only the AI settings: not the people tables and not its own proposals. The grants golden and a test of the role (`permission denied` on a read of a person and on any update) keep it so; the schema test names the assistant's one write.
- **D9 Own pay is a database check, not only a refusal.** `ai_settings_own_pay_needs_a_second_approver` forbids a row with it on; the migration that adds the second approver (with the Fyniq link) is the one that relaxes it.
- **D10 Expiry is computed.** A pending proposal past its 72 hours shows as expired and cannot be approved (the trigger refuses an approval dated after it); nothing needs to write a row to expire it.

## Decisions made while building slice 2

- **D11 The tool names avoid the never-list's words.** The never-list is checked on a tool's name as on a task's, so `my_payday`, `slip_line_rule` and `rule_answer` are the names, not `my_pay_day`, `payslip_line_rule` and `policy_answer`. Every tool of the allowlist is tested against the list.
- **D12 The scripted model plans by words and a refusal comes first.** What the assistant never does is matched in the question before any tool is planned (approve, change pay, issue, restricted data, another person's pay, an injected instruction); a match answers in words with no tool run. A model's own tool choices are not trusted: one outside the allowlist or that the never-list names is dropped, and a result that carries pay, an NRIC, a bank account or a figure of money is refused before the model sees it.
- **D13 Rules are cited, not remembered.** A pack rule is answered from the pack in force today with its version and the official source, and always with the note to confirm it with HR, because every value of the pack is researched and none yet verified a second time (NM-PACK-07). A payslip line is explained from a fixed rule text with no figure; the knowledge base is not read at run time because the people image does not carry `kb/` (left: a KB reader once the knowledge base is shared).

- **D14 The asker may decide a tier 1 draft.** Slice 1 forbade it by a constraint; that would leave a company with one HR officer unable to approve a draft the officer asked for, which Fyniq's own AI-10 allows for tier 1. The constraint is dropped from migration 000036 (not yet deployed anywhere); the asker, the decider and both times stay in the proposal and its audit events.

## Decisions made while building slice 3

- **D15 Applied means attached, not performed.** The executor inserts the approved text into `nemura.ai_draft` beside the request and marks the proposal applied. It does not call `CheckLetter`, `SignLetter` or `ReplyFlex`: those decide, and the assistant never decides. The executor's grants are INSERT on `ai_draft`, UPDATE of two columns of `ai_proposal` and SELECT of the two request tables' rows, with no grant on a request's columns and none on the people tables; a test runs the forbidden updates and sees `permission denied`.
- **D16 The approved text is re-hashed before it is attached.** `ApplyProposal` refuses when the final text no longer hashes to what was approved, when it no longer fits the task's shape, or when the request is closed; an approved proposal for a closed request stays approved and is reported, never applied.
