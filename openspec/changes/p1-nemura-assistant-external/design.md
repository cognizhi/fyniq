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

`nemura.ai_proposal` rows are inserted by `nemura_assistant` (insert-only), decided by a human with the task's approver role (a database constraint: the decider is not the asker), and applied by `nemura_executor` through the letters domain commands (a draft letter request; a reply draft on a flexible-work request) only after approval. A proposal expires after 72 hours and an expired one cannot be approved. The approved text is what the human saw and edited: the executor applies the stored final text and its hash, never a regenerated one.

### D6. External systems stay Fyniq's

NM-EXT-01 to 03 are decided on the Fyniq side (FR-PPL-01, 05; the import is idempotent per period and version, a draft that an Accountant approves). The Nemura side is that a feature an external system holds is off in Nemura (NM-FEAT-01 to 05), which `p1-nemura-platform` built; this change adds the test that a switched-off feature's assistant tools are absent too.

## Risks

- A prompt injection in a request's text (a flexible-work reason, a letter's purpose) that asks for approval, a pay figure or another person's record: the red-team suite carries each and must see a refusal and no proposal.
- The rules drifting between the two apps: the conformance test in D1 and the shared PRD sentences.
- A model route that stores data outside Singapore: none is configured until evidenced (AI-88).
