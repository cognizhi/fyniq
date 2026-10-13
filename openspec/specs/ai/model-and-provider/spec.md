# ai/model-and-provider Specification

## Purpose
Which models the AI assistant runs on and how they are chosen, configured, changed and used on Google Cloud: the ADR-T2 model options, structured outputs, caching and streaming, the Google Cloud constraints, and a provider interface for a second model family. Derived from PRD-001 §9.9.

## Requirements

### Requirement: AI-26 Models per ADR-T2
The models SHALL follow ADR-T2 (§12.5), and model IDs and endpoints SHALL live in configuration, change only with dual approval, and pass the eval suite (QA-AI-01…03) before use.

Source: PRD-001 §9.9 · Priority M · Phase P1

#### Scenario: Option A is the default at go-live
- **WHEN** the assistant goes live with the default option
- **THEN** it uses Claude Opus 4.6 for the chat and agent loop and Sonnet 4.6 for high-volume classification, on the Singapore endpoint

#### Scenario: Option B
- **WHEN** Option B is chosen
- **THEN** the assistant uses Claude Opus 5.5 (`claude-opus-5-5`) and Haiku 5.5 (`claude-haiku-5-5`), on the global endpoint

#### Scenario: Model IDs and endpoints change only with dual approval
- **WHEN** a model ID or endpoint is changed
- **THEN** the change is made in configuration
- **AND** it needs dual approval

#### Scenario: The eval suite gates a model before use
- **WHEN** a model ID or endpoint is to be used
- **THEN** it must first pass the eval suite (QA-AI-01…03)

#### Scenario: G4-088 Retirement of the primary regional model
- **GIVEN** the primary regional model is announced for retirement
- **WHEN** the retirement date approaches
- **THEN** the eval-gated fallback model is switched in by dual-approved configuration before the retirement date

#### Scenario: The options are route presets
- **WHEN** an agent's route is set (AI-74)
- **THEN** Options A, B and C are available as presets over the provider connections
- **AND** Option A stays the default route at go-live and for personal and payroll data

#### Scenario: Presets pin from the discovered list
- **WHEN** a preset is applied to a route
- **THEN** it names a provider, a region and a model line, and pins the newest active, qualified model of that line from the connection's discovered list (AI-80)
- **AND** no model ID is compiled into code or seed data

### Requirement: AI-27 Structured outputs, thinking, caching and streaming
The system SHALL use structured outputs and strict tool schemas for every tool and for proposal payloads, adaptive thinking with effort tuned per route, prompt caching of the stable system prompt and tool definitions, and streaming responses to the UI.

Source: PRD-001 §9.9 · Priority M · Phase P1

#### Scenario: Strict schemas for tools and proposals
- **WHEN** the model calls a tool or produces a proposal payload
- **THEN** structured outputs with a strict schema are used

#### Scenario: Adaptive thinking per route
- **WHEN** a request is sent to the model
- **THEN** it uses adaptive thinking with the effort tuned for its route

#### Scenario: Stable prompt and tools are cached
- **WHEN** a request is sent to the model
- **THEN** the stable system prompt and the tool definitions are prompt-cached

#### Scenario: Responses stream to the UI
- **WHEN** the model responds
- **THEN** the response is streamed to the UI

#### Scenario: A missing capability rules a provider out
- **GIVEN** a provider connection that lacks strict schemas, tool calling, caching or streaming
- **WHEN** an agent that needs that capability is routed
- **THEN** the route cannot use that connection (AI-71, AI-74)

### Requirement: AI-28 Google Cloud constraints designed in
The design SHALL work within Google Cloud's constraints for Claude: the tool loop SHALL run in our orchestrator, and document reading SHALL use PDF and image input in the request.

Source: PRD-001 §9.9 · Priority M · Phase P1

#### Scenario: The tool loop runs in the orchestrator
- **WHEN** the assistant uses tools
- **THEN** the tool loop runs in our orchestrator, because Anthropic's hosted MCP connector, Files API and Batches API are not available for Claude on Google Cloud

#### Scenario: Documents are read from PDF and image input
- **WHEN** the assistant reads a document such as an invoice
- **THEN** it sends the PDF or image in the request, because Document AI's invoice parser is not offered in Singapore

