## Why

The owner put the AI on the launch path on 13 Nov 2026 (PRD-001 rev 0.274): the company secretary in full, with the AI
secretary's scheduled checks, and with them the scheduled agents, the assistant and this change, the model layer
everything above it runs on. Nothing in Fyniq can call a model yet: `cmd/ai` is a shell. Every later AI change
(`p1-ai-assistant`, `p1-scheduled-agents`, `p1-company-secretary`, and after go-live the knowledge base, proposals,
MCP and document capture) needs one provider-neutral way to call a model that keeps data in its allowed place, never
spends past a budget, records what answered, and can be switched off.

## What Changes

- **One model port (AI-70)** in `internal/ports`, with adapters for the Anthropic Messages API (also serving Claude on
  Google's Agent Platform), the OpenAI API, Gemini on Agent Platform and a generic OpenAI-compatible adapter with
  presets for Moonshot Kimi, xAI Grok, OpenRouter, a LiteLLM gateway, vLLM, Ollama and LM Studio (AI-71). Each adapter is
  a connector of a new kind, `model-provider` (FR-INT-25), proving its capabilities in a conformance suite against a
  fake of its service; a capability it cannot prove counts as absent.
- **Provider connections (AI-72, AI-73, AI-79)**: several per adapter, each with its endpoint and region, its
  credential in the secret store by reference (write-only, masked, rotated, never logged or sent to a model, the
  browser or an MCP client, and only in headers), an egress allow-list of its own host, its billing mode and quota,
  and an enabled flag. Off by default; created, changed, enabled and given a model only with the Owner and a second
  approver. Consumer-app tokens and command-line seats are refused.
- **Model lists from the provider (AI-80, AI-81, AI-84)**: no model list ships with Fyniq. Each connection's models
  are read from the provider's own list endpoint, page by page, as complete, partial or failed reads, daily at 06:00
  SGT and on demand; a model is picked from the list and probed before it joins the allow-list; retired or unlisted
  models leave by themselves after a complete read.
- **Prices from a named source (AI-82, AI-85)**: the provider's list, Google's Cloud Billing Catalog, LiteLLM's public
  price catalogue (stored with its digest and licence check, read as decimal text with tiers), or an Owner's price
  with a second approver; a usage-based route needs a priced model.
- **Data handling and classes (AI-75, AI-83, AI-88)**: each connection's declaration of where prompts are processed,
  retention, training use and sub-processors, with dated evidence; a policy mapping data classes to connections
  (personal data and payroll only in Singapore by default); every call checked against the union of the classes it
  sends, before the call and before each fallback.
- **Per-agent routes (AI-74, AI-76, AI-26, AI-27)**: a primary model, ordered fallbacks and data classes per agent, set
  by the Owner or Administrator (widening needs a second approver), with ADR-T2's presets A, B and C; failover only to
  an eligible step, otherwise a plain stop; structured outputs, adaptive thinking, prompt caching and streaming
  wherever the route's model proves them (AI-27, AI-28).
- **Usage, cost and budgets (AI-77, AI-86, AI-87)**: every call metered (tokens by kind, cost in its currency and SGD,
  the price version, the model requested and the model and version that answered); budgets per agent, connection,
  person and organisation as admission control, reserved atomically before each call and settled after, with alerts at
  80% and a hard stop at 100%.
- **Qualification (AI-78, CMP-AI-03, CTL-AI-05)**: a model serves an agent only after passing that agent's evals,
  recorded per model version and route; moving aliases are re-qualified weekly; a model, route or prompt change needs a
  passing eval run and a dual-approved change record.
- **The AI activity log and the kill switch (AI-12, AI-13; moved here by the owner on 13 Nov 2026, rev 0.275)**: every
  model call and, as later changes add them, every turn, tool call and proposal, visible to the person (own) and the
  Owner and compliance (all), restricted classes masked; the Owner's switch that stops the AI within 60 seconds.
- **Checks and compliance**: C.2 #55 (a route that needs attention); the AI inventory and the AI DPIA in the Compliance
  Center (CMP-AI-02); CTL-AI-05's control row.
- **Local mode (DEV-09, the unnumbered "Local AI in replay and live mode")**: replay cassettes by default, live mode on
  `FYNIQ_AI_LIVE=1` with a developer's key, local-only adapters for a developer's own Claude or ChatGPT plan, and a local
  screening stub; nothing of these in release images.
- **Screens**: `ai-settings`, `ai-providers`, `ai-provider-connection`, `ai-model-routes`, `ai-route-editor`,
  `ai-usage` and `ai-activity-log`, Expert, under Settings › AI; and `my-ai-plan` (P2's own-plan option, shown off).

## Capabilities

### New Capabilities

None: every requirement is approved in the main specs already (`skip_specs: true`).

### Modified Capabilities

None at the spec level. AI-12 and AI-13 keep their text; their move to this change is recorded as a scenario each in
`ai/proposals` (rev 0.275).

## Impact

- **Requirements**: AI-26, AI-27, AI-28 (`ai/model-and-provider`); AI-70 to AI-88 (`ai/model-providers`); AI-12 and
  AI-13 (`ai/proposals`, rev 0.275); CTL-AI-05; CMP-AI-02, CMP-AI-03; C.2 #55; DEV-09; "Local AI in replay and live
  mode" (`platform/local-mode`); "Claude in Singapore tie" (`platform/portability-and-cost`). PRD-001 §9.5, §9.9,
  §9.9.1, §12.5 (ADR-T2), §12.8, §12.12, §14.4, C.2, F.7.
- **Screens** (`docs/design/screens/inventory.json`): `ai-settings`, `ai-providers`, `ai-provider-connection`,
  `ai-model-routes`, `ai-route-editor`, `ai-usage`, `ai-activity-log`, `my-ai-plan` (fragments updated locally; no
  canvas publishing until the design account is back).
- **Code**: a `ModelProvider` port and its adapters under `internal/adapters/`; `connectors/` for the four model-provider
  connectors; a router, metering and budgets package; migrations for connections, model lists, prices, routes,
  qualification, calls, reservations and budgets, under row-level security; the API's `/v1/ai/...` operations; the
  worker's daily list and catalogue reads; the checks engine (C.2 #55); the Compliance Center's AI inventory; the web
  app's Settings › AI.
- **Security boundary**: the AI service gains no write to business data (golden rule 5); it records its own calls and
  reservations only, append-only. Security review #4 applies; the threat model is part of the change.
- **Outside**: live provider calls need a developer's key locally and the founders' provider accounts at the cloud
  gate. Google's Agent Platform endpoints and the Cloud Billing Catalog are `[live-verify]` in the dev cloud.
- **Open for the owner before go-live, not before building**: ADR-T2's revisit "when AI is enabled": which preset the
  founders' routes start on (A stays the accepted default), with Opus 4.6's earliest retirement (5 Feb 2027) near
  go-live `[verify]`.
