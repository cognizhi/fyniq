## Context

- `p1-ai-providers` (archived 14 Nov 2026) built the model layer in the API's and the worker's processes: the
  `ModelProvider` port and its adapters, connections, the data-class policy, routes per agent with the `chat` agent
  already listed, the router (`internal/ai.Router.Run`, streaming, failover, budgets, the AI switch), qualification,
  the activity log of model calls and the `Screening` port (Model Armor adapter, local stub with the red-team corpus).
- `cmd/ai` is a shell. The AI's database role, `fyniq_ai`, holds no grant at all (CMP-AI-01; `TestAttackTheAIRoleCannot
  ReadOrWrite`), and the AI tool registry the archtest `TestTheAIToolsAreReadOnly` checks does not exist yet.
- The API already serves the figures the assistant needs: the statements (`/v1/reports/statements/{kind}`), the
  general ledger, budget against actual, schedules, cash position and projection with its what-if, KPIs, checks,
  statutory packs, ledger entries and every record's read operations, all under RLS and Appendix D.
- `authn.Principal` has an `agent` class: the API audits its calls as the AI on behalf of the person
  (`internal/api/api.go`, AUD-01), and agent-class callers are proposal-only (AI-34).
- The owner's decisions of 14 Nov 2026 (PRD-001 rev 0.277) shape it: the browser reaches the assistant through the API
  with a delegated read token per turn; the orchestrator's model calls go through the API's router (D11 (a) of
  `p1-ai-providers`); each eval suite comes with what it evaluates; the assistant's own rules (AI-P1, AI-P2, AI-P6,
  AI-P7, AI-P8, AI-08, the orchestrator's) are built here.

## Goals / Non-Goals

**Goals:**
- One tool loop, one registry and one set of guardrails that the scheduled agents and the company secretary reuse.
- Every figure shown comes from a typed fact a tool returned in that turn, rendered by platform code.
- The AI service can be compromised without exposing a key, a write or another person's data.

**Non-Goals:**
- Proposals, the `propose` tool, the Prepare lane and "Save as proposal" (`p1-ai-proposals`); the knowledge-base tools
  and how-to answers (`p1-knowledge-base`); MCP (`p1-mcp-first-party`); scheduled agents (`p1-scheduled-agents`).
- A SQL sandbox (AI-03's P2 option), embeddings or vector search (AI-58).
- Nemura's assistant (PRD-002 NM-AI-*), which reuses this loop later.

## Decisions

**D1. Reached through the API (rev 0.277; FR-INT-06, golden rule 4).**
- `/v1/assistant/conversations` (create, list the person's own, read one with its turns, delete their own), `POST
  /v1/assistant/conversations/{id}/turns` (a question and its optional context, FR-UX-17), answered as server-sent
  events (status, text, tool progress, facts, the grounded answer, the end with its usage) and kept as the turn; `POST
  .../turns/{turn_id}/feedback` (QA-AI-05). Each is in the OpenAPI contract and on the never-list for MCP and agent
  clients (§9.6).
- Admission in the API before anything reaches the AI service: the AI switch (`features.ai_assistant`), the person's
  rate limit (D9), the conversation's length (D9); the router then applies budgets and routes per call.
- Alternative: the load balancer's `/ai` route straight to the AI service (the PRD's earlier drawing). Rejected by the
  owner: a second public surface with its own sign-in, CSRF and limits, outside the reviewed contract.

**D2. A delegated read token per turn (AI-01, AI-36, AI-34).**
- The API mints it for the turn: an Ed25519-signed token (issuer and audience the API's own; the key in the secret
  store, rotated) naming the person, the organisation, the turn, the AI tools' scope and an expiry of at most 5 minutes,
  `jti` single-turn. The API verifies it as a principal of class `agent` with the person's roles, so its calls are
  audited as the AI on behalf of the person (AUD-01) and any write is refused (AI-34); the session never leaves the API.
- Alternatives: Hydra's JWT-bearer grant, as the MCP spike does (`internal/mcpdelegation`): a network hop and a client
  registration per turn for a token the API both issues and verifies; forwarding the session: token passthrough,
  refused by AI-36.

**D3. The orchestrator in `cmd/ai`, stateless and keyless (AI-P2, "The orchestrator holds no write credential",
AI-28).**
- `internal/assistant` runs the tool loop: it builds the context (the system prompt from `contracts/ai/prompts/`, the
  conversation's earlier turns as the API sends them, the context chip's record), asks the model through the API, runs
  each tool call through the API with the delegated token, applies the guardrails (D7, D8) and streams events back.
- It has no database access (no DSN in its configuration; `fyniq_ai` keeps no grant) and no provider key: everything it
  reads or calls goes through the API. The API reaches it on an internal address only (Cloud Run internal ingress with
  IAM invoker in the cloud `[live-verify]`; the Compose network locally), and it calls back on the API's internal
  listener (D4).
- Alternative: run the loop inside the API process: rejected, as the API holds write credentials (SEC-03, golden rule 5).

**D4. The AI service's calls into the API.**
- An internal listener of the API (its own port, never behind the load balancer) serves two operations for the AI
  service's service identity only, each also carrying the turn's delegated token: `model-call` (agent, classes,
  messages, tools, stream) runs `ai.Router.Run` as the person, so the reservation, the call, the record and the AI
  switch are the router's (D11 (a)); `tool` runs one registry tool as the person (D5). Its contract is
  `contracts/openapi/fyniq-internal.v1.yaml`, generated from the code beside the public one.
- Alternative: the public `/v1` operations for tools too: possible for most, but the internal operation applies the
  registry's limits (D6) in one place, and the model call has no public form.

**D5. One read-tool registry (AI-49's first part, AI-01, AI-P1, AI-03, AI-68).**
- `contracts/ai/tools/<name>.yaml`: name, description, input JSON Schema (`additionalProperties: false`), output
  shape, limits, scope and version; it generates the model's tool definitions and, later, MCP's. Launch tools:
  `metrics.query` (the semantic layer), `records.get` and `records.search` (allow-listed record types and columns),
  `lineage.explain` (a fact's or a report cell's entries and source documents), `checks.list` and `checks.explain`,
  `packs.get` (statutory values in force on a date, with pack key and version), `calc`. `kb.*` (p1-knowledge-base) and
  `propose` (p1-ai-proposals) are added by those changes. None writes (`TestTheAIToolsAreReadOnly` then checks real
  definitions).
- `calc` (AI-P6) runs in the AI service itself, exact decimals (apd), on facts by ID and constants, returning its result
  as a fact with its input facts' IDs; every other tool reads through the API.

**D6. The semantic layer and typed facts (AI-03, AI-68, AI-04).**
- `internal/semantic` defines named measures (revenue, cost of sales, gross profit, operating expenses, net profit,
  net burn, cash, runway in months, receivables and payables outstanding and overdue, GST payable) and dimensions
  (month, quarter, financial year, account, customer, vendor, department, currency), each compiled by platform code to
  the balance service (ENG-04) or the report that already computes it, so a measure equals the statement line it names;
  `POST /v1/metrics:query` exposes it (FR-INT, API-first).
- A fact: ID (`F` + a hash of the measure, dimensions, filters, period and ledger revision), measure, dimensions and
  filters, period or date, value as a decimal string, currency and unit, sign convention, ledger revision (ENG-46) and
  its `lineage.explain` link. Counts, dates and percentages are typed facts too; statutory values carry pack key and
  version.
- Limits per tool call (AI-04): a row cap (50), column allow-lists per record type, a statement timeout (5 s), and a
  token budget per result (a larger result is summarised by the tool, with the facts kept and the rest counted).

**D7. Grounding and rendering (AI-08, AI-21, AI-69, AI-22).**
- The model answers in a structured output: segments of text and references `{fact: F…}` or `{calc: …}`; it writes no
  digits in text except dates and ordinals the schema allows. Platform code renders each figure from its fact, with
  RND-DISPLAY, and checks the sentence's attribution (measure, entity, period, currency) against the fact; an
  unmatched number or a wrong attribution regenerates the answer once with the failure named, then withholds the figure
  with what could not be established (G4-089, G4-090). A figure from an earlier turn keeps its fact ID or is read again.
- Output is a fixed Markdown subset rendered by the web app (paragraphs, lists, emphasis, tables of facts, links shown
  in full; no images, no HTML, no remote fetch) (AI-69); every AI message carries the violet accent and the AI badge
  (AI-22).
- Alternative: free text checked after the fact by matching numbers: weaker against right-value-wrong-fact (G4-089).

**D8. Exclusions, untrusted text and screening (AI-02, AI-20, AI-18, AI-P7, AI-25, CTL-AI-06).**
- The exclusion policy runs in the API on every AI-principal read, before data leaves for the AI service: never-send
  fields (NRIC/FIN, full bank account numbers, secrets, authentication data, full home addresses) are removed or masked
  at the field level by a registry of sensitive fields per record type; payroll needs the new setting
  `ai.payroll_access` (off, dual controlled) and the person's payroll right; an Owner may add exclusions (dual
  controlled) and never remove the never-send list. An output filter in the AI service and the API catches NRIC and
  bank-number shapes in anything bound for the model or the person.
- Free text from outside (bank descriptions, email bodies, document text, counterparties' names) reaches the model as
  quoted `untrusted_text` in tool results, never in the system or user role, and is screened through the `Screening`
  port with prompts and answers; an incomplete screening fails closed (`ports.ErrScreeningIncomplete`).
- CTL-AI-06 runs in `make test` and weekly: planted NRIC, bank numbers and payroll in the sample company must reach no
  recorded model request; its report is evidence.

**D9. Denial of wallet and limits (AI-23).**
- Per person: 20 turns a minute and 200 a day (settings, dual controlled); per turn: at most 8 tool calls and the
  route's output cap; per conversation: 40 turns, compacting the oldest into a summary that keeps their facts by ID
  past 20. Token and money budgets are the router's (AI-13, AI-86).

**D10. Prompts and models gated (AI-24, CMP-AI-03).**
- System prompts are versioned files in `contracts/ai/prompts/` with their hash recorded on every turn; a prompt change
  is a reviewed pull request whose `make evals` run must pass, as a route change already needs a qualified model.

**D11. Conversations, turns and the log (AI-12's turns, AI-P8, retention).**
- `ai_conversation`, `ai_turn` (question, context, answer segments, facts cited, status, prompt hash, the run of model
  calls), `ai_tool_call` (tool, version, inputs, an output hash, row counts, duration, outcome) and `ai_feedback`,
  append-only under FORCE RLS, written by the API's role in the turn's unit of work; the activity log lists turns and
  tool calls with their classes and masks (FR-PAY-13). The purge job removes conversations older than
  `retention.ai_conversations_days` (365) and records it (AUD-01).
- Audit events: `ai_conversation.created`, `.deleted`, `ai_turn.answered`, `ai_feedback.given`, `ai_conversation.
  purged`, setting changes for `ai.payroll_access` and the limits.

**D12. The panel (FR-UX-17, screen `ai-assistant-panel`).**
- A side panel from the shell (⌘J / Ctrl+J; "Ask about this" on records, report cells and figures), docked, a sheet or
  full screen by device class; the context chip; streaming; citation chips that open the record, the report or the
  entry; Explain on a figure calls `lineage.explain`; Simulate runs the cash projection's what-if, read-only; the
  disabled, budget, excluded-data and tool-failure states; thumbs up or down with a reason; Expert mode shows the
  facts' accounts and query details. Prepare and "Save as proposal" stay hidden until `p1-ai-proposals`.

**D13. Evals (QA-AI-01, QA-AI-03, QA-AI-04, QA-AI-05; rev 0.277).**
- `cmd/evals` gains assistant suites: the golden questions (`evals/golden/`, at least 200 on the sample company, each
  with its expected facts computed from the seed and checked against the oracle's figures) and the red team
  (`evals/redteam/`: injections planted in sample bank lines, bills and documents; exclusion probes; accumulated
  disclosure; misleading requests), run through the real orchestrator against recorded model answers. Release needs
  100% numeric accuracy and 0 red-team successes; a missing recording fails a release run and is reported in replay.
- The harness takes the suites `p1-scheduled-agents` (QA-AI-07), `p1-ai-proposals` (QA-AI-02) and `p1-knowledge-base`
  (QA-AI-06) add.
- Monitoring: per turn, grounding failures, refusals, tool errors, screening flags and cost, as activity-log fields and
  OpenTelemetry counters, summarised on AI usage; approval rates arrive with proposals. Feedback is reviewed by an
  Owner or Administrator before it joins the golden set.

**D14. Local mode (DEV-09, ADR-T4).**
- The AI service runs in the Compose stack; replay answers from `evals/cassettes/`; live runs only with
  `FYNIQ_AI_LIVE=1` and a developer's key, or DEV-09's own-plan adapter, on synthetic data. Model Armor's local stub
  screens. Nothing local is in the release images.

Ports and adapters touched (ADR-T8): `ModelProvider` and `Screening` (used, not changed), a `Secrets` key for D2's
signing key, the storage port's new conversation stores (postgres adapter). ADRs: ADR-T2 (models and residency,
unchanged), ADR-T3 (the delegated token is the API's own, not Hydra's), ADR-T7 (the purge as a scheduled job), ADR-T8.

## Risks / Trade-offs

- [The stream is relayed twice (AI service → API → browser)] → server-sent events end to end with flushing; a turn's
  latency budget is measured locally and `[live-verify]` on Cloud Run.
- [A delegated token leaks from the AI service] → it reads only, through the registry's allow-list, as one person, for
  one turn, for at most 5 minutes, and the AI service is reachable only internally.
- [Structured answers read less naturally] → the renderer joins segments into prose; the golden set's helpfulness
  rating (PRD target ≥ 95%) is tracked with feedback.
- [Golden figures drift from the reports] → each measure is tested equal to its statement line, and the golden set's
  expectations are computed, not typed.
- [No model's answers are recorded offline] → replay reports it; release runs record live on synthetic data first.

## Migration Plan

- Additive migrations; the assistant stays off until the AI switch is on and a route serves `chat`, so a release changes
  nothing for an organisation that has not enabled it.

## Open Questions

- The per-person limits of D9 and the 8-tool cap are first values, settings an Owner can change; the golden set's live
  runs tune them before go-live.
