## Context

See proposal.md for why. What exists today:
- `cmd/ai` is a shell (`app.Serve("ai", nil)`); no package calls a model, and no model ID, list or price is in the code.
- The connector framework (`internal/connector`, `connectors/<id>/`, FR-INT-25…27) gives manifests with capability
  kinds, data classes, regions, destinations, secrets by reference and health checks, and a dual-approved lifecycle;
  but a connector installs at most once per organisation (`organisation_connector_live`).
- Every outbound request goes through `internal/egress` (SEC-36): each configured connection reaches only its own
  hosts, never a private or metadata address; a self-hosted endpoint is a registered private endpoint (SEC-37). An
  architecture test refuses any other HTTP client.
- Appendix D's row "AI providers and model routes" (Owner A + second approver, Administrator C, Controller, Auditor and
  DPO V, the AI nothing) is encoded as `authz.AIProviders`. Appendix D has no row of its own for the kill switch and the
  budgets.
- ADR-T2 (§12.5) was accepted for Option A, Singapore-only models, at go-live, "revisited when AI is enabled";
  `[verify]`: Opus 4.6's earliest retirement is 5 Feb 2027, and Sonnet 4.6's Singapore processing was confirmed by two
  of Google's three pages (rechecked 14 Oct 2026).
- The database role `fyniq_ai` holds only `USAGE` on the schema.

## Goals / Non-Goals

**Goals:**
- Every model call, by any agent, goes through one router that admits it (switch, budgets), checks its data classes,
  calls the route's step, fails over only to an eligible step, and records what answered and what it cost.
- Connections, model lists, prices, routes and qualification are data, each change audited, the widening ones
  dual-controlled; nothing about models is compiled into the code.
- Local mode works offline: replay cassettes by default, live calls only on opt-in.

**Non-Goals:**
- The chat loop, read tools, semantic layer, guardrails and their eval suites (`p1-ai-assistant`); scheduled agents
  (`p1-scheduled-agents`); proposals and the task catalogue (`p1-ai-proposals`, after go-live). This change builds the
  hooks they plug into (agents, qualification records, the activity log's entry kinds, the switch's keys).
- A user's own plan (AI-73 c, P2): `my-ai-plan` shows the option as not offered.
- Batch APIs (AI-28: not on Google's Claude); hosted MCP connectors and file APIs (AI-28).

## Decisions

**D1. One port, adapters on plain HTTP (AI-70, AI-71, ADR-T8).**
- `ports.ModelProvider`: `Call` and `Stream` (messages; JSON Schema tools; a strict output schema; PDF and image
  attachments; max tokens; thinking effort; cache breakpoints) answering content, tool calls, usage (input, output,
  cache read and write, reasoning tokens), the model and version the provider reports, and the stop reason;
  `ListModels` (one page at a time); `Probe`; `Capabilities`.
- Four adapters in `internal/adapters/`: `anthropic` (the Messages API, direct or through Agent Platform with Google
  credentials from the `gcp` adapter), `openai` (the Responses API), `gemini` (Agent Platform), `openaicompat` (chat
  completions, with presets). They speak JSON over the egress guard's clients: no provider SDK, so no second HTTP
  stack, nothing to vendor and the images stay small. The alternative, SDKs inside the adapters, is allowed by ADR-T8
  but would bypass the egress guard and grow the images.
- A conformance suite runs each adapter against a fake of its service and proves each declared capability (strict
  schemas, tool calls, streaming, caching usage fields, PDF and image input, reasoning controls); what it cannot prove
  counts as absent (AI-71).

**D2. Connections are records of their own, on a connector of kind `model-provider` (AI-71, AI-72).**
- Each adapter is a connector with a manifest (`connectors/anthropic`, `openai`, `gemini`, `openai-compatible`) of a
  new kind, `model-provider`, declaring its capabilities, regions and the data it sends.
- A connection is an `ai_connection` row: the adapter, a preset (for the OpenAI-compatible one: Kimi, Grok,
  OpenRouter, LiteLLM, vLLM, Ollama, LM Studio), the base URL and region, the auth method, the credential by
  reference in the secret store, parameters, timeouts, retries, rate limits, the billing mode and quota, the
  data-handling declaration with its evidence, the allow-list of models, its state and version. Several connections
  may share an adapter, which `organisation_connector` cannot hold.
- Creating, changing, enabling and adding a model are requests of a new approval object, `ai_connection`, on the row
  "AI providers and model routes" (the Owner and a second approver; FM-1). Disabling is never held back: whoever may
  ask may switch a connection off at once, audited.
- The credential is stored through the connector secret store (write-only; shown once, then masked; rotated; never
  logged, never in a URL, never sent to a model, the browser or an MCP client; error text scrubbed) (AI-79).
  Consumer-app tokens, chat sessions, cookies and command-line seats are refused by the auth methods offered (AI-73).
- Each connection's egress is its base URL's host only; a self-hosted endpoint must be a registered private endpoint
  (SEC-37).

**D3. Model lists, read whole or not trusted (AI-80, AI-81, AI-84).**
- `ai_model_list_read` (connection, time, `complete` / `partial` / `failed`, pages, the models added and removed) and
  `ai_model` (connection, the provider's ID, state: new, available, deprecated with its date, not offered here, no
  longer listed, not callable; the provider's hints; alias target; shutdown date).
- Read when a credential is stored, a connection is tested or enabled, daily at 06:00 SGT by the worker, after a
  not-found error, and on "Refresh from provider", every page to a page cap; only a complete read marks a model
  unlisted; a passed shutdown date removes it whatever the read; the last good list stays in use after a failed read.
- A picked model is probed (a few tokens of synthetic text) before it joins the allow-list; there is no typed model ID.

**D4. Prices with their source, as exact decimals (AI-82, AI-85).**
- `ai_price_catalogue` (source, fetched at, digest, licence check, the raw document kept) and `ai_price` versions
  (model, source: provider list, Google Cloud Billing Catalog, LiteLLM, or the Owner's with a second approver; match
  exact or normalised; per-million-token rates by kind and tier, in their currency, parsed from decimal text).
- The worker reads LiteLLM's public catalogue daily through the egress guard; Google's catalogue is read by the `gcp`
  adapter `[live-verify]`. A normalised match needs an Owner's confirmation; a suspicious drop keeps the old price
  until reviewed; an Owner's price is never overwritten.
- As built (task 4.2): the downloads are a global table, `ai_price_catalogue`, like the exchange-rate feed, read through
  a view and written only by the worker through `record_ai_price_catalogue`; the document is kept in the file store
  under its SHA-256, and a document that no longer matches its digest is never used. A manifest names how the catalogue
  names its provider (`price_catalogue`, by authentication or preset) `[verify: each name in LiteLLM's catalogue]`; a
  provider it does not name stays unpriced. A key with the provider's own prefix is an exact match. A normalised match
  waits for the Owner each time its price changes, and a price the Owner declined is not proposed again. A rate the
  catalogue gives as zero leaves the model unpriced; batch, flex, priority, audio and search rates are listed, not used.
  The sources are taken in AI-82's order: a provider's own list (OpenRouter's pricing, kept on the model as decimal text;
  a discounted or overridden price is not used; xAI's list prices are not read `[verify]`), Google's Cloud Billing
  Catalog for Agent Platform (input and output token SKUs found by the model's listed name in its location, always held
  for the Owner's confirmation, and none where a long-context SKU exists `[live-verify]`), then LiteLLM's catalogue. An
  Owner's price, entered with a second approver, is withdrawn at once by the Owner.
- Metering amounts are not ledger money: each call's cost is kept exactly (`NUMERIC(24,10)`) in the price's currency
  and in SGD at the organisation's rate for the day, never as a binary float; screens round by RND-DISPLAY; provider
  invoices are entered as bills, where the ledger's rules apply (AI-77).

**D5. Data classes travel with the content (AI-75, AI-83, AI-88).**
- Classes: know-how, business, personal, payroll, restricted. `ai_data_policy` maps each to the connections allowed;
  the default allows personal and payroll only on a connection whose Singapore processing is evidenced (AI-88) or a
  self-hosted model in the organisation's Singapore network.
- Every request carries the union of its parts' classes, set deterministically by whoever adds the part; the router
  checks the connection (and any gateway) against it before each call and each fallback, and a connection without a
  complete list read within its stale limit (14 days) or with a version-less alias serves no personal or payroll
  class (AI-84, AI-87). Classes never drop within a conversation.

**D6. Routes per agent, presets as configuration (AI-74, AI-26).**
- `ai_agent` lists the agents (the chat loop, document capture, bank categorisation, each task type's drafting, report
  commentary, the knowledge-base compiler, each scheduled agent, the eval runner, and `probe`); later changes add
  theirs. `ai_route` versions hold ordered steps (connection, pinned model, effort, caching) and the agent's classes.
- A preset (A Singapore-only, B global, C by data class) names a provider, region and model line and pins the newest
  active, qualified model of that line from the discovered list.
- The Owner or Administrator sets a route; one that sends an agent's data to a connection it did not use before is an
  `ai_route` approval request (the Owner and a second approver).

**D7. One router (AI-76, AI-27, AI-28).**
- `internal/ai` (domain): admit (the switch, budgets), check classes, call the step through the port, fail over on an
  error, timeout, rate limit or exhausted quota only to a step that meets the classes and capabilities, otherwise stop
  with a plain message and a code; record every attempt. The tool loop of later agents runs in our orchestrator on
  top of it (AI-28).
- Strict schemas, adaptive thinking, caching of the stable prompt and tools, and streaming are used where the step's
  capabilities prove them; an agent that needs one is not routed to a connection without it.

**D8. Budgets as admission control (AI-77, AI-86, AI-13).**
- `ai_budget` (scope: organisation, agent, connection, person; amount in SGD per month) and `ai_reservation`.
- Before a call the router reserves its maximum cost against every applicable budget in one database function, under
  row locks, refusing what any budget cannot cover; after the call it settles to the actual cost; an unknown outcome
  keeps its reservation until the provider's usage reconciles it. Probes, evals and retries count. Alerts at 80% go to
  the Owner; at 100% nothing new is admitted; accounting, checks and deadlines keep working.
- As built (task 5.3): budgets are in the base currency per calendar month in Singapore; the most a call could cost is
  its input estimated at a token per three bytes (and a fixed allowance per image) plus all its output limit, at the
  step's price and the day's exchange rate. Costs in the base currency are exact (`NUMERIC(43,26)`: a cost of up to 16
  places at a rate of up to 10), never rounded. A connection's spent budget skips to the next step; the organisation's,
  the agent's or the person's stops the call, as does a cost with no exchange rate where a budget applies. Probes go
  through the router, reserving and recorded as the probe agent's calls outside any route (route version 0). The 80%
  alert is the notification `ai.budget` to the Owners. `ai_reserve` and `ai_settle` are the API's and the worker's
  (see D11). The usage view is `GET /v1/ai/usage`, by connection (with a subscription's quota), model, agent, person
  and organisation.

**D9. The activity log and the switch (AI-12, AI-13; rev 0.275).**
- `ai_call` (append-only) records each attempt: agent, route version and step, connection, the model requested, the
  model and version reported, the person, classes, tokens by kind, cost and price version, outcome, latency, the step
  it fell back from. The activity log reads calls and, as later changes add them, turns, tool calls and proposals;
  each person sees their own, the Owner and compliance all; restricted classes are masked without the right, and
  reading them is audited (AUD-05). Retained as audit data.
- The switch is the organisation setting `ai.enabled` (and, with `p1-ai-proposals`, the propose tool's and each task
  type's). Turning it off is immediate and needs no second person; turning it on is dual-controlled (golden rule 6).
  The router reads it at admission (cached at most 30 s) and every 10 s during a stream, so new calls are refused at
  once and running ones stop within 60 s.

**D10. Qualification gates every model (AI-78, AI-87, CMP-AI-03, CTL-AI-05).**
- `ai_qualification` (agent, connection, model version, eval suite and version, thresholds, result, run, time). A step
  serves an agent only on a passing qualification of that model version; a reported version that differs invalidates
  it; a moving alias is re-qualified weekly and shows ⚠ until it passes.
- This change ships the machinery and the `probe` agent's synthetic suite; each agent's own suite arrives with it
  (QA-AI-01…03 with the assistant). Automatic qualification of new models is off by default.
- A route, model or prompt change is applied only through its approval request carrying a passing eval run (the
  dual-approved change record, CMP-AI-03); CI's `make evals` job is CTL-AI-05's gate, with its report kept as evidence.

**D11. Where it runs; the AI writes nothing of the books (golden rule 5).**
- The router runs wherever a model is called: the AI service for agents, the worker for list and price reads, probes
  and evals. Configuration is written by the API's role under the approvals.
- `fyniq_ai` gains SELECT on the AI configuration and EXECUTE on `ai_reserve`, `ai_settle` and `record_ai_call`
  (`SECURITY DEFINER`, append-only, its own organisation), and nothing else: no business table, no configuration
  write. Security review #4 reviews it.
- As built (task 5.2): **superseded in part, pending the owner's decision.** PRD-001's CMP-AI-01 (and its control test,
  `TestTheAIRoleHoldsNoWritePrivilege`) lets the AI's role write nothing but proposals, through a function either, and
  the PRD wins over this design. So `record_ai_call` (and, with task 5.3, `ai_reserve` and `ai_settle`) are granted to
  the API and the worker only, and the router records and reserves as their roles. How the AI service records the calls
  its agents make is an open owner decision, needed before `p1-ai-assistant` runs agents there: (a) the AI service asks
  the API or the worker to run the router for it, keeping CMP-AI-01 as it is; (b) CMP-AI-01 widens to append-only
  metering functions in the caller's organisation, a PRD revision with the AI-surface second review; (c) the AI service
  connects with a separate metering role holding only those functions.

**D12. Local mode (DEV-09, "Local AI in replay and live mode").**
- Replay by default: a `replay` provider answers from cassettes in `evals/cassettes/` (beside the AI suites, as
  §12.4's layout puts them), keyed by the hash of the prompt, tools and model version; a miss is
  an error naming the key, so CI never calls out.
- Live on `FYNIQ_AI_LIVE=1`: the real adapters with a developer's key from the environment, synthetic data only.
- DEV-09's adapters, in the `local` build only: they call the developer's own `claude` or `codex` command-line agent as
  a subprocess, never read its sign-in, name the model from its configuration and declare what they lack
  `[verify: whether the Agent SDK accepts a developer's own subscription sign-in]`.
- A `Screening` port with a local stub that flags the red-team corpus patterns, standing in for Model Armor
  `[live-verify]`; the guardrails wire it in with the assistant.

**D13. The API and the screens.**
- `/v1/ai/connections` (and each one's models, refresh, probe, credential, enable and disable), `/v1/ai/presets`,
  `/v1/ai/routes` and `/v1/ai/routes/{agent}`, the prices (on each connection's models, `/v1/ai/price-catalogues` and
  `/v1/ai/prices/{id}/review`; as built in task 4.2), `/v1/ai/usage`, `/v1/ai/activity`, `/v1/ai/settings`
  (the switch, budgets): admin operations, on the never-list for the AI and MCP (§9.6).
- Settings › AI: `ai-settings`, `ai-providers`, `ai-provider-connection`, `ai-model-routes`, `ai-route-editor`,
  `ai-usage`, `ai-activity-log`; `my-ai-plan` under Profile, showing P2's option as not offered.

**D14. Audit events and checks.**
- Audit events (AUD-01): `ai_connection.requested`, `.enabled`, `.disabled`, `.changed`, `.credential_stored` (never
  the value); `ai_model.listed` (system), `.allowed`, `.removed`, `.probed`; `ai_price.changed` (system user, or the
  Owner); `ai_route.changed`; `ai_qualification.recorded`; `ai_budget.changed`; `ai.switched_on`, `ai.switched_off`;
  `ai_activity.read` for a restricted entry (AUD-05).
- C.2 #55 flags a route that falls back past its daily threshold, an alias not re-qualified this week, a budget or
  quota within 20%, a model retiring within 30 days, retired or unlisted, and a connection whose list was not read for
  3 days.
- The AI inventory in the Compliance Center lists connections, models, routes and, later, prompts and tool versions,
  with owners and data categories, beside the AI DPIA in the DPIA register (CMP-AI-02).

## Risks / Trade-offs

- [Option A's primary model retires near go-live `[verify]`] → presets pin from the live list and switching is a
  qualified route change, not a release; C.2 #55 warns 30 days ahead; the owner revisits ADR-T2 at enablement.
- [Plain-HTTP adapters drift from the providers' APIs] → each adapter's conformance suite runs against its fake in
  CI, and against the live API in `FYNIQ_AI_LIVE` runs before a model is qualified.
- [A poisoned or broken price catalogue] → digest, licence check, decimal parsing, suspicious drops held for review,
  Owner prices never overwritten.
- [Budget races] → one database function reserves under row locks; the concurrent-spend scenario is a test.
- [The AI role gains writes] → only append-only call and reservation functions in its own organisation; the grants
  snapshot and an attack test prove nothing else.

## Migration Plan

- Additive migrations; every switch and connection off by default, so a release changes nothing until the Owner
  enables a connection and the AI with a second approver.

## Open Questions

- ADR-T2's revisit at enablement (the owner, before go-live): which preset the founders' routes start on. Option A stays
  the accepted default; it does not change what is built.
- Appendix D has no row for the kill switch and the budgets; this design puts them under "AI providers and model
  routes" until a review gives them one.
- `[live-verify]`: Agent Platform's auth and endpoints, the Cloud Billing Catalog, Model Armor.
