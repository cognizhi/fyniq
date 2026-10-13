# ai/model-providers Specification

## Purpose
Let the assistant and every agent run on models from several providers, each connection with its own configuration, billing mode and data-handling declaration, and each agent on its own route, without changing the AI write path. Derived from PRD-001 §9.9.1 (rev 0.12).

## Requirements

### Requirement: AI-70 One model port
Every model call SHALL go through one provider-neutral `ModelProvider` port, with the orchestrator, tools, proposal service and guardrails unchanged whichever provider answers.

Source: PRD-001 §9.9.1 · Priority M · Phase P1

#### Scenario: Every agent uses the port
- **WHEN** the chat loop, extraction, classification, drafting, the knowledge-base compiler, a scheduled agent or the eval runner calls a model
- **THEN** the call goes through the `ModelProvider` port with messages, JSON Schema tool definitions, a structured-output schema, attachments, streaming and limits

#### Scenario: Same tools and guardrails
- **WHEN** a provider answers
- **THEN** the same read tools, proposal service and guardrails (§9.8) apply as for any other provider

#### Scenario: SDKs stay in adapters
- **WHEN** the code is built
- **THEN** no provider SDK is imported outside its adapter (ADR-T8)

### Requirement: AI-71 Provider connectors and capabilities
Each provider SHALL be a connector (FR-INT-25) that declares its capabilities, and P1 SHALL ship the Anthropic Messages API, OpenAI API, Gemini on Agent Platform (formerly Vertex AI) and a generic OpenAI-compatible adapter.

Source: PRD-001 §9.9.1 · Priority M · Phase P1

#### Scenario: Declared capabilities
- **WHEN** a provider connector is registered
- **THEN** it declares tool calling, strict structured output, streaming, PDF and image input, prompt caching, context window, reasoning controls, batch and the regions it processes in

#### Scenario: OpenAI-compatible presets
- **WHEN** an OpenAI-compatible connection is created
- **THEN** presets exist for Moonshot Kimi, xAI Grok, OpenRouter, a LiteLLM gateway and self-hosted vLLM, Ollama or LM Studio servers

#### Scenario: Claude on Agent Platform
- **WHEN** Claude is used through Google Cloud's Agent Platform (formerly Vertex AI)
- **THEN** the Anthropic Messages API adapter serves it

#### Scenario: Unproven capabilities count as absent
- **GIVEN** a capability the adapter cannot prove in its conformance suite
- **WHEN** routes are checked
- **THEN** the capability counts as absent

### Requirement: AI-72 Per-provider configuration
Each provider connection SHALL hold its own configuration, be off by default, and change only with the Owner and a second approver.

Source: PRD-001 §9.9.1 · Priority M · Phase P1

#### Scenario: What a connection holds
- **WHEN** a connection is configured
- **THEN** it has a base URL and region, an authentication method (API key, cloud service account, OAuth grant, or none for a private self-hosted endpoint), an allow-list of pinned model versions, default parameters, timeouts, retries, rate limits, a billing mode and quota, a data-handling declaration with evidence, and an enabled flag

#### Scenario: Off by default and dual-controlled
- **WHEN** a connection is created, changed or enabled, or a model is added to its allow-list
- **THEN** the Owner and a second approver must approve it

#### Scenario: No agent before qualification
- **GIVEN** a model added to a connection
- **WHEN** it has not passed qualification (AI-78)
- **THEN** it serves no agent

### Requirement: AI-73 Billing modes
The system SHALL support usage-based and subscription or commitment billing per connection, a user's own plan as a P2 option where the provider allows it, and SHALL refuse consumer-plan tokens reserved for the provider's own apps.

Source: PRD-001 §9.9.1 · Priority M · Phase P1 (a, b); P2 (c)

#### Scenario: Usage-based cost per request
- **GIVEN** a usage-based connection
- **WHEN** a request completes
- **THEN** its cost is computed from the provider's usage fields (input, output, cached and reasoning tokens) and price table, in the provider's currency and in SGD

#### Scenario: Subscription or commitment quota
- **GIVEN** a connection on a fixed fee with a quota (provisioned throughput, a plan that allows application use, prepaid gateway credits or a self-hosted model)
- **WHEN** requests run
- **THEN** usage is metered against the quota and its reset window, and the fee is spread over the window in cost reports

#### Scenario: Quota exhausted
- **WHEN** a quota runs out
- **THEN** the agent moves to its next route step or stops with a plain message
- **AND** it never switches to pay-per-token silently

#### Scenario: A user's own plan (P2)
- **GIVEN** a provider OAuth grant that lets a subscriber spend their plan in third-party apps, and the Owner has enabled it with a second approver
- **WHEN** a user links their plan
- **THEN** it serves only that user's interactive assistant, never scheduled agents, MCP clients or excluded data classes

#### Scenario: Consumer-app tokens refused
- **WHEN** someone tries to connect consumer-plan credentials whose terms reserve them for the provider's own apps, or a chat session or cookie
- **THEN** the connection is refused

### Requirement: AI-74 Per-agent routes
Every agent SHALL run on a route of a primary provider and model with ordered fallbacks and the data classes it handles, set by the Owner or Administrator.

Source: PRD-001 §9.9.1 · Priority M · Phase P1

#### Scenario: Agents with routes
- **WHEN** routes are listed
- **THEN** there is one for the chat and tool loop, document capture, bank categorisation and matching, each catalogue task type's drafting, report commentary, the knowledge-base compiler, each scheduled agent and the eval runner

#### Scenario: Widening needs a second approver
- **WHEN** a route change would send an agent's data to a connection it did not use before
- **THEN** the Owner and a second approver must approve it

#### Scenario: Presets
- **WHEN** a route is set
- **THEN** the ADR-T2 presets (A Singapore-only, B global, C by data class) may be used

#### Scenario: Provenance on every run
- **WHEN** an agent runs, answers or proposes
- **THEN** the provider, model version and route step are recorded (AI-12, AI-06)

### Requirement: AI-75 Data handling per provider
Each connection SHALL declare where prompts are processed, retention, training use and sub-processors, and a policy SHALL map data classes to the connections allowed.

Source: PRD-001 §9.9.1 · Priority M · Phase P1

#### Scenario: Declaration with evidence
- **WHEN** a connection is enabled
- **THEN** its processing location, retention (including zero data retention where offered), training use and sub-processors are recorded with the provider's terms as evidence

#### Scenario: Transfer assessment outside Singapore
- **GIVEN** a connection that processes outside Singapore
- **WHEN** it is enabled
- **THEN** the Owner has recorded the PDPA transfer assessment (CMP-PD-06) and the data inventory lists it (SEC-13)

#### Scenario: Default for personal data and payroll
- **WHEN** an agent handles personal data or payroll
- **THEN** by default it may use only in-Singapore processing (ADR-T2 Option A) or a self-hosted model in the organisation's Singapore network

#### Scenario: Exclusions apply everywhere
- **WHEN** any provider is called
- **THEN** AI-02's exclusions apply
- **AND** no route can point an agent at a connection its data classes do not allow

### Requirement: AI-76 Failover never lowers protection
On an error, timeout, rate limit or exhausted quota, the router SHALL try the next route step only if it meets the agent's data classes and capabilities, and otherwise stop with a plain message.

Source: PRD-001 §9.9.1 · Priority M · Phase P1

#### Scenario: Eligible fallback
- **GIVEN** the next route step meets the agent's data classes and capabilities
- **WHEN** the current step fails
- **THEN** the router uses it and records the fallback on the AI activity log

#### Scenario: No eligible fallback
- **GIVEN** no remaining step meets the agent's data classes or capabilities
- **WHEN** the current step fails
- **THEN** the run stops with a plain message and an error code

#### Scenario: Frequent fallbacks
- **WHEN** a route falls back more than its daily threshold
- **THEN** C.2 #55 shows ⚠

### Requirement: AI-77 Usage, cost and budgets
The system SHALL meter requests, tokens, cost, quota, fallbacks and errors per connection, model, agent, user and organisation, with budgets, alerts and hard stops.

Source: PRD-001 §9.9.1 · Priority M · Phase P1

#### Scenario: Metering
- **WHEN** usage is viewed
- **THEN** requests, tokens by kind, cost, quota used and its reset, fallbacks and errors show per connection, model, agent, user and organisation

#### Scenario: Alerts and hard stops
- **WHEN** an agent, connection or organisation reaches 80% of its budget
- **THEN** an alert is raised
- **AND** at 100% further calls stop (AI-13, AI-67)

#### Scenario: Provider invoices are bills
- **WHEN** a provider invoices the organisation
- **THEN** it is entered as a bill, and the usage view helps check it

### Requirement: AI-78 Qualification before use
A provider and model SHALL serve an agent only after passing that agent's evals at the required thresholds, recorded per model version and route.

Source: PRD-001 §9.9.1 · Priority M · Phase P1

#### Scenario: Evals gate each agent
- **WHEN** a model is proposed for an agent's route
- **THEN** it must pass that agent's evals (QA-AI-01…03, QA-AI-06, QA-AI-07) first, and the result is stored per model version

#### Scenario: Pinned versions
- **WHEN** the provider allows a pinned model version
- **THEN** the route uses it

#### Scenario: Moving aliases
- **GIVEN** a model name the provider re-points to newer models
- **WHEN** it is allowed on a route
- **THEN** it is re-qualified at least weekly, and the route shows ⚠ until it passes

### Requirement: AI-79 Credentials and network
Provider credentials SHALL be kept in Secret Manager, write-only and rotated, never logged or exposed, and calls SHALL leave only through an egress allow-list per connection.

Source: PRD-001 §9.9.1 · Priority M · Phase P1

#### Scenario: Credentials never exposed
- **WHEN** a credential is saved
- **THEN** it is encrypted with the organisation's key, shown once and then masked, rotated on a schedule, never logged (SEC-15) and never passed to the model, the browser or an MCP client

#### Scenario: Egress allow-list
- **WHEN** an agent calls a provider
- **THEN** the call leaves only to that connection's allow-listed endpoint over TLS, and a self-hosted endpoint is reached over a private network or mutual TLS

#### Scenario: Local mode
- **WHEN** Fyniq runs in local mode
- **THEN** it uses a deterministic stub or an OpenAI-compatible server on the machine, never a cloud provider key (DEV-07)
