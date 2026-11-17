## Why

The owner put the AI on the launch path on 13 Nov 2026 (PRD-001 rev 0.274): the company secretary in full, with the
scheduled agents and the assistant they run on. `p1-ai-providers` built the model layer: connections, routes, the
router, budgets, qualification and the activity log. Nothing can yet ask the books a question: `cmd/ai` is a shell,
there is no read-tool registry, no semantic layer, no grounding and no panel. This change builds the in-app assistant
that answers from the books with every figure cited to its source, inside the person's own permissions, without ever
changing data. The scheduled agents (`p1-scheduled-agents`) and the company secretary then run on the same tool loop,
tools and guardrails.

## What Changes

- **The orchestrator (`cmd/ai`; AI-P2, "The orchestrator holds no write credential", AI-28)**: the tool loop runs in
  our AI service, with no database access at all and no provider key. Per the owner's decisions of 14 Nov 2026 (PRD-001
  rev 0.277), the browser reaches it only through the API: `POST /v1/assistant/conversations/{id}/turns` (and the
  conversation operations) admit a turn and hand it to the AI service with a short-lived, audience-bound, read-only
  delegated token for that person, never the session; its model calls go through an internal API operation that runs
  `p1-ai-providers`' router (design D11 (a)), so keys, egress, budgets and metering stay in one place.
- **On-behalf-of read tools (AI-01, AI-P1, AI-04)**: one read-tool registry in `contracts/ai/tools/` (the source of the
  model's tool definitions, and later of MCP's): `metrics.query`, `records.get`, `records.search`, `lineage.explain`,
  `checks.list`, `checks.explain`, `packs.get` and `calc`. Each reads through the public API as the person, minus the AI
  exclusions, bounded by row caps, column allow-lists, statement timeouts and a token budget per result. The
  knowledge-base tools come with `p1-knowledge-base`, `propose` with `p1-ai-proposals`.
- **The semantic layer (AI-03, AI-68)**: named measures and dimensions (revenue, expenses, profit, net burn, cash,
  runway, receivables and payables outstanding, GST, by month, quarter, year, account, customer, vendor, department),
  compiled by platform code over the balance service and the reports, behind `POST /v1/metrics:query`. Every figure is a
  typed fact: an ID, the measure, its dimensions and filters, the period or date, currency and unit, sign convention,
  the ledger revision and a lineage link.
- **AI exclusions (AI-02, AI-20, CTL-AI-06)**: never NRIC/FIN, full bank account numbers, secrets, authentication data or
  full home addresses to a model; payroll only with the organisation's new "AI payroll access" setting (off, dual
  controlled) and the person's payroll right; an Owner adds exclusions under dual control but never removes the
  never-send items; an output filter for NRIC and bank-number shapes; CTL-AI-06's check on every change and weekly.
- **Grounding and the calculator (AI-08, AI-P6, AI-21)**: the model never writes a figure: it cites facts by ID, and
  platform code renders each figure from its fact; a number with no fact, or a fact attributed to the wrong measure,
  entity, period or currency, fails and is regenerated once or withheld with what could not be established; arithmetic
  goes through `calc`, exact decimals, returning its result with its input facts.
- **Guardrails against the OWASP LLM risks (AI-18, AI-19, AI-22, AI-23, AI-24, AI-25, AI-69, AI-P7)**: untrusted text
  (bank descriptions, emails, document text) reaches the model as quoted data and is screened (Model Armor in the cloud,
  the local stub offline), failing closed; no write tool; AI content marked in violet with the AI badge; per-person rate
  limits, a cap on tool calls per turn and on conversation length with compaction; prompts versioned and gated by evals
  like models; a fixed Markdown subset for output, with authorisation, egress and execution outside the model.
- **Conversations and the log (AI-12's turns and tool calls, AI-P8, "Retention of AI conversations")**: conversations,
  turns and tool calls kept append-only under row-level security, shown in the AI activity log with their masks, and
  purged after `retention.ai_conversations_days` (365).
- **The assistant panel (FR-UX-17, AI-05; screen `ai-assistant-panel`)**: ⌘J or "Ask about this" on any record, report
  or figure opens it with that context; Ask, Explain (a figure's lineage) and Simulate (the cash projection's what-if,
  read-only); citation chips that open the record, report or entry; "could not establish" instead of a partial figure;
  thumbs up or down with a reason (QA-AI-05). Its Prepare lane and "Save as proposal" arrive with `p1-ai-proposals`, its
  how-to answers from the guide with `p1-knowledge-base`.
- **Evals (QA-AI-01, QA-AI-03, QA-AI-04, QA-AI-05)**: the eval harness that `p1-scheduled-agents`, `p1-ai-proposals` and
  `p1-knowledge-base` add their suites to (rev 0.277); the golden question set (at least 200 questions on the sample
  company with exact expected figures, 100% numeric accuracy to release); the red-team suite (0 successes);
  production monitoring of grounding failures, refusals, tool errors and cost per conversation; reviewed feedback.

## Capabilities

### New Capabilities

None: every requirement is approved in the main specs already (`skip_specs: true`).

### Modified Capabilities

None at the spec level. Rev 0.277 records where requirements are built, as scenarios in `ai/evaluation`,
`ai/guardrails` and `ai/proposals`, and the assistant's path through the API as a scenario of "The orchestrator holds no
write credential"; no requirement's text changed.

## Impact

- **Requirements** (29, PRD-001 rev 0.277): AI-01, AI-02, AI-03, AI-04, AI-05, AI-68 (`ai/data-access`); QA-AI-01,
  QA-AI-03, QA-AI-04, QA-AI-05 (`ai/evaluation`); AI-18 to AI-25, AI-69 (`ai/guardrails`); AI-P1, AI-P2, AI-P6, AI-P7,
  AI-P8, AI-08 and "The orchestrator holds no write credential" (`ai/proposals`); CTL-AI-06; "Retention of AI
  conversations" (`data/backup-and-recovery`); FR-UX-17. PRD-001 §7.2, §9.3 to §9.5, §9.8, §9.10, §12.3, §14, F.7.
- **Screens** (`docs/design/screens/inventory.json`): `ai-assistant-panel` (its fragment `AiPanel` exists; updated
  locally for what launches; no canvas publishing until the design account is back).
- **Code**: `cmd/ai` and `internal/assistant` (the orchestrator, the tool loop, grounding, the calculator, the output
  filter); `contracts/ai/tools/` and `contracts/ai/prompts/`; `internal/semantic` and `POST /v1/metrics:query`;
  `/v1/assistant/...` operations, an internal API listener for the AI service's model calls and tool reads, and the
  delegated read token; migrations for conversations, turns, tool calls and feedback, under row-level security; the
  exclusion policy and the "AI payroll access" setting; the activity log's turns; the retention purge; `cmd/evals`
  suites; the web app's panel.
- **Security boundary**: the AI service holds no database access, no provider key and no write credential (golden rule
  5, CMP-AI-01 unchanged); the delegated token reads only, through the AI tools' allow-list; every new operation is on the
  never-list for MCP and agent clients (§9.6). Security review #4 applies; the threat model is part of the change.
- **Outside**: live model runs for the golden set's recordings need a developer's key or DEV-09's own-plan adapter on
  synthetic data; Model Armor and the Singapore endpoint check (AI-05's G4-087) are `[live-verify]` in the dev cloud.
