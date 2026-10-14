# ai/model-providers Specification

## Purpose
Let the assistant and every agent run on models from several providers, each connection with its own configuration, billing mode and data-handling declaration, and each agent on its own route, with each connection's models read from the provider's own list, without changing the AI write path. Derived from PRD-001 §9.9.1 (rev 0.12, model lists rev 0.13).

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
- **THEN** it has a base URL and region, an authentication method (API key, cloud service account, OAuth grant, or none for a private self-hosted endpoint), an allow-list of pinned model versions picked from the models the provider lists (AI-80), default parameters, timeouts, retries, rate limits, a billing mode and quota, a data-handling declaration with evidence, and an enabled flag

#### Scenario: Off by default and dual-controlled
- **WHEN** a connection is created, changed or enabled, or a model is added to its allow-list
- **THEN** the Owner and a second approver must approve it

#### Scenario: No agent before qualification
- **GIVEN** a model added to a connection
- **WHEN** it has not passed qualification (AI-78)
- **THEN** it serves no agent

#### Scenario: Allow-list picked from the provider's list
- **WHEN** an Owner adds a model to a connection's allow-list
- **THEN** they pick it from the models the provider lists for that connection, and no one can type a model ID
- **AND** a model the provider retires or stops listing leaves the allow-list by itself (AI-81)

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

#### Scenario: A plan's models come from the user's grant (P2)
- **WHEN** a user links their own plan
- **THEN** its models are listed with the user's grant (AI-80)
- **AND** a plan whose grant cannot list models cannot be connected

#### Scenario: Per-model quota rates
- **GIVEN** a plan that draws its quota at different rates per model
- **WHEN** usage is metered against the quota
- **THEN** each model's rate is recorded with its source, the plan's published terms

#### Scenario: Command-line seats refused
- **WHEN** someone tries to connect a seat used through a provider's own command-line agent (Claude Code, Codex, Copilot CLI)
- **THEN** the connection is refused, because it has no list endpoint and is reserved for that agent

#### Scenario: Consumer-app tokens refused
- **WHEN** someone tries to connect consumer-plan credentials whose terms reserve them for the provider's own apps, or a chat session or cookie
- **THEN** the connection is refused

#### Scenario: OpenAI plan connections (P2)
- **WHEN** an OpenAI plan connection is linked
- **THEN** its models come from `GET /v1/models` with the user's access token, keeping the entries whose `visibility` is `list`
- **AND** inference needs the `chatgpt.tokens.use.direct` grant, and an identity sign-in alone grants none
- **AND** it stays gated on the terms, eligibility and data handling (checked 14 Oct 2026)

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

#### Scenario: Processing evidenced
- **WHEN** a connection is said to process in Singapore
- **THEN** that is evidenced per model, endpoint and feature (AI-88)

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
- **AND** at 100% no new call is admitted (AI-13, AI-67, AI-86)

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

#### Scenario: Aliases recognised from the list
- **WHEN** the provider's model list marks a name as an alias of another model, as OpenRouter's `alias_target` does for its `~latest` aliases
- **THEN** the route treats it as a moving alias without anyone having to say so

#### Scenario: The model that answered
- **WHEN** the provider reports a different version from the one qualified
- **THEN** AI-87 applies

### Requirement: AI-79 Credentials and network
Provider credentials SHALL be kept in Secret Manager, write-only and rotated, never logged or exposed, and calls SHALL leave only through an egress allow-list per connection.

Source: PRD-001 §9.9.1 · Priority M · Phase P1

#### Scenario: Credentials never exposed
- **WHEN** a credential is saved
- **THEN** it is encrypted with the organisation's key, shown once and then masked, rotated on a schedule, never logged (SEC-15) and never passed to the model, the browser or an MCP client

#### Scenario: Egress allow-list
- **WHEN** an agent calls a provider
- **THEN** the call leaves only to that connection's allow-listed endpoint over TLS, and a self-hosted endpoint is reached over a private network or mutual TLS as a registered private endpoint (SEC-37)

#### Scenario: Local mode
- **WHEN** Fyniq runs in local mode
- **THEN** it uses a deterministic stub or an OpenAI-compatible server on the machine, never a cloud provider key (DEV-07)

#### Scenario: Credentials only in headers
- **WHEN** Fyniq calls a provider to list models, probe or send a request
- **THEN** the credential travels in a request header, never in a URL
- **AND** any error text from the call is scrubbed of credentials before it is logged or shown

### Requirement: AI-80 Model lists come from the provider
The system SHALL ship no list of models: each connection's models SHALL come from the provider's own list endpoint, read with the connection's credential of any kind, and a connection whose credential cannot list models SHALL NOT be enabled.

Source: PRD-001 §9.9.1 · Priority M · Phase P1

#### Scenario: Listed with the connection's own credential
- **GIVEN** a connection authenticated by an API key, a cloud service account, a subscription or commitment grant, or (P2) a user's plan grant
- **WHEN** its models are listed
- **THEN** the adapter calls the provider's list endpoint with that credential (for example Anthropic and OpenAI `GET /v1/models`, Agent Platform `publishers.models.list`, OpenRouter `GET /api/v1/models`, xAI `GET /v1/language-models`, or `GET /v1/models` on Kimi, LiteLLM, vLLM, LM Studio and Ollama)

#### Scenario: When the list is read
- **WHEN** a credential is stored, a connection is tested or enabled, at 06:00 SGT each day for every connection with a stored credential, when a call fails because the provider no longer knows the model, or when an Owner or Administrator chooses Refresh from provider
- **THEN** the list is read without sending company data, and stored with its time and the models added and removed, as an audited system event

#### Scenario: No list, no connection
- **GIVEN** a credential that cannot list models
- **WHEN** someone tries to enable the connection
- **THEN** it is refused with `AI_MODEL_LIST_UNAVAILABLE`, and there is no manual model entry

#### Scenario: Probe before a model is allowed
- **WHEN** an Owner picks a listed model for a connection's allow-list
- **THEN** a probe on that connection (a few tokens of synthetic text) must succeed first, because a list says what exists, not what this credential, plan tier, project or region may call
- **AND** probes run only for picked models, never against the whole list

#### Scenario: Nothing hidden by its name
- **WHEN** a provider's list is shown
- **THEN** it is narrowed only by what the provider reports (capabilities, modalities, generation methods)
- **AND** where the provider reports nothing, every listed model is shown, and name patterns may sort the list but never remove a model from it

#### Scenario: Whole list, page by page
- **WHEN** a provider returns its list in pages
- **THEN** every page is read to the end, with a page cap
- **AND** whether the read was complete decides what it may remove (AI-84)

#### Scenario: A plan's roster from the plan's endpoint
- **GIVEN** a subscription plan connection
- **WHEN** its models are listed
- **THEN** the roster comes from the plan's own endpoint with the plan's key, before the connection is enabled, so the probe has a model to call

#### Scenario: Reported capabilities are hints
- **WHEN** the list reports a model's limits or capabilities
- **THEN** they are shown and used as hints, and the conformance suite still proves each capability (AI-71)

#### Scenario: Shutdown dates kept
- **WHEN** OpenAI's list gives a model's `shutdown_date`
- **THEN** it is kept with the model

### Requirement: AI-81 The list keeps itself current
Each listed model SHALL show its state, and a retired or unlisted model SHALL leave allow-lists and routes by itself; the last good list SHALL stay in use when a read fails; newly listed models MAY be qualified automatically.

Source: PRD-001 §9.9.1 · Priority M · Phase P1

#### Scenario: Model states
- **WHEN** a connection's models are shown
- **THEN** each is new (first listed in the last 7 days), available, deprecated with its retirement date where the provider gives one, not offered in this region, or no longer listed

#### Scenario: Retired or unlisted model
- **WHEN** a model on an allow-list is retired or no longer listed (after a complete read, AI-84)
- **THEN** it leaves the allow-list and every route step that names it is skipped, so the agent runs on its next step (AI-76)
- **AND** Checks shows ⚠ (C.2 #55), and its history and past runs stay

#### Scenario: A read fails
- **WHEN** reading the list fails
- **THEN** the last good list stays in use
- **AND** after 3 days without a successful read, Checks shows ⚠ (C.2 #55)

#### Scenario: Automatic qualification
- **GIVEN** automatic qualification is on for the connection (off by default)
- **WHEN** a model is newly listed
- **THEN** it is queued for the evals of every agent whose route uses that connection, on synthetic fixtures within the eval runner's budget, and the results appear on the connection
- **AND** adding it to the allow-list or to a route stays a human decision

#### Scenario: Not callable
- **WHEN** a listed model fails with an access or not-found error
- **THEN** it is shown as not callable and handled like a model no longer listed, until a read or a probe succeeds again

### Requirement: AI-82 Prices come from a named source
Each model's price SHALL record its source, in order: the provider's list, Google's billing catalogue, LiteLLM's public price catalogue, or an Owner-entered price with a second approver; a model with no price SHALL NOT serve a route on a usage-based connection.

Source: PRD-001 §9.9.1 · Priority M · Phase P1

#### Scenario: Price sources
- **WHEN** a model's price is needed
- **THEN** it comes from the provider's list (OpenRouter, xAI or a LiteLLM gateway's `/model/info`), else Google's Cloud Billing Catalog for Agent Platform, else LiteLLM's public model price catalogue, else a price the Owner entered with a second approver, labelled "entered by"
- **AND** the source is shown with the price

#### Scenario: No price, no usage-based route
- **GIVEN** a usage-based connection
- **WHEN** a model has no price from any source
- **THEN** it cannot serve a route, because its cost could not be budgeted (AI-77)
- **AND** subscription, commitment and self-hosted connections need no per-token price

#### Scenario: Price version kept per request
- **WHEN** a request's cost is computed
- **THEN** it keeps the price version it used, and the provider's invoice stays the record

#### Scenario: Catalogue match confidence
- **WHEN** a price comes from LiteLLM's catalogue, read daily in the background
- **THEN** the match is labelled exact or normalised (after removing a provider prefix or a version-date suffix)
- **AND** a normalised match needs an Owner's confirmation before it prices a usage-based route

#### Scenario: No guessed prices
- **WHEN** no source matches a model
- **THEN** it stays visibly unpriced

#### Scenario: Owner prices kept
- **WHEN** a later read finds a price for a model the Owner priced
- **THEN** the Owner's price is not overwritten, and each price change is one audited event by the system user

#### Scenario: Ingestion
- **WHEN** the catalogue is read
- **THEN** AI-85's rules apply

### Requirement: AI-83 Data classes follow the content
Every tool result, attachment, knowledge page and stored turn SHALL carry the data classes of its content, set deterministically; before every call and fallback the router SHALL check the connection and any gateway against the union of everything sent, skipping it or stopping without sending.

Source: PRD-001 §9.9.1 · Priority M · Phase P1

#### Scenario: Fallback after own-pay facts
- **GIVEN** a conversation that retrieved the user's own payslip figures
- **WHEN** the primary connection fails and the only fallback is a business-only or overseas connection
- **THEN** nothing from the conversation is sent to it
- **AND** the run stops with a plain message, and the stop is audited

#### Scenario: No declassification
- **WHEN** the route changes during a conversation
- **THEN** the classes already in it stay

#### Scenario: Traces and evals
- **WHEN** a trace or eval sink receives content
- **THEN** it is checked against the same classes

### Requirement: AI-84 Complete reads only
Each model-list read SHALL be complete, partial or failed; only a complete read SHALL mark a model no longer listed, and a connection without a complete read within its stale limit SHALL stop serving personal and payroll classes.

Source: PRD-001 §9.9.1 · Priority M · Phase P1

#### Scenario: A later page fails
- **GIVEN** a qualified model listed on page 3
- **WHEN** page 2 fails, or the page cap is reached
- **THEN** the read is partial and the model stays on the allow-list

#### Scenario: The provider's shutdown date
- **WHEN** the provider's retirement or shutdown date for a model has passed
- **THEN** the model leaves the allow-list whatever the read

#### Scenario: Stale limit
- **GIVEN** no complete read for 14 days (the default)
- **WHEN** a route carrying payroll data would use the connection
- **THEN** it is skipped until a complete read succeeds

### Requirement: AI-85 Price catalogue ingestion
Each downloaded price catalogue SHALL be stored with its digest, time, source and licence check, read as decimal text with its unit, currency and tiers, and a suspicious price drop SHALL keep the previous price until an Owner reviews it.

Source: PRD-001 §9.9.1 · Priority M · Phase P1

#### Scenario: Collision or corruption
- **WHEN** a new model matches an older one only after normalising, or a price is negative, zero or in an unknown unit
- **THEN** the model is unpriced, or keeps its previous price until an Owner reviews it
- **AND** nothing lowers its budget charge silently

#### Scenario: Tiers
- **GIVEN** a request longer than a context length at which the catalogue gives a higher rate
- **WHEN** it is priced
- **THEN** the higher rate applies, or the request is unpriced if that tier is missing

#### Scenario: Exact decimals
- **WHEN** a price is read
- **THEN** it is parsed from decimal text, never as a binary float

#### Scenario: Reproducible estimates
- **WHEN** a past request's estimate is recomputed
- **THEN** it uses the stored catalogue version

### Requirement: AI-86 Budgets as admission control
Before each call the router SHALL reserve its maximum cost against every applicable budget in one atomic step and refuse calls a budget cannot cover; reservations SHALL settle to actual usage, stay on unknown outcomes, and count probes, evals and retries.

Source: PRD-001 §9.9.1 · Priority M · Phase P1

#### Scenario: Concurrent spend
- **GIVEN** S$5 left in an agent's budget and ten calls that could each cost up to S$1
- **WHEN** they start together
- **THEN** at most five are admitted

#### Scenario: Unknown outcome
- **WHEN** a call's outcome is unknown
- **THEN** its reservation stays until the provider's usage is reconciled

#### Scenario: Deterministic work continues
- **WHEN** an AI budget is exhausted
- **THEN** accounting, checks and deadlines keep working

### Requirement: AI-87 The model that answered
Every call SHALL record the model requested and the model and version the provider reports; a different reported version SHALL invalidate that step's qualification, and a moving alias with no version SHALL serve only know-how and business classes unless the Owner and a second approver accept the risk.

Source: PRD-001 §9.9.1 · Priority M · Phase P1

#### Scenario: A moved alias
- **GIVEN** an alias qualified on Monday
- **WHEN** on Tuesday the provider reports a different version for it
- **THEN** the step's qualification is shown as invalid
- **AND** it is skipped for personal and payroll classes and ⚠ for the rest until re-qualified

#### Scenario: No version reported
- **GIVEN** an alias for which the provider reports no version
- **WHEN** a route carrying payroll data would use it
- **THEN** it is not allowed, unless the Owner and a second approver accepted the risk for that route

### Requirement: AI-88 Where processing happens is evidenced
Each connection SHALL record where prompts are processed, apart from where data is stored, per model, endpoint and feature with a dated official source or contract term; unclear evidence SHALL keep it from personal and payroll classes, and a probe SHALL never prove geography.

Source: PRD-001 §9.9.1 · Priority M · Phase P1

#### Scenario: Storage-only region
- **GIVEN** OpenAI's Singapore region, which stores data in Singapore and processes it elsewhere (checked 14 Oct 2026)
- **WHEN** a route carrying payroll data would use it
- **THEN** it is not allowed

#### Scenario: Evidence record
- **WHEN** a connection is enabled for personal or payroll data
- **THEN** a dated processing-evidence record is kept, covering gateways, logging, retention and sub-processors

#### Scenario: A probe proves nothing about place
- **WHEN** a probe succeeds
- **THEN** it is not taken as evidence of where the request was processed
