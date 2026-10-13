# ai/guardrails Specification

## Purpose
The threats to the AI assistant, on OWASP LLM and agentic top-10 themes, and the control that answers each: prompt injection, excessive agency, sensitive information disclosure, hallucinated figures, over-reliance, denial of wallet, model and supply-chain change, poisoned retrieval sources, and exfiltration or manipulation through output. Derived from PRD-001 §9.8.

## Requirements

### Requirement: AI-18 Prompt injection via bank descriptions, emails, PDFs
Untrusted content SHALL be passed to the model as quoted data in tool results, never as instructions, and prompts and documents SHALL be screened by Model Armor, against prompt injection via bank descriptions, emails and PDFs.

Source: PRD-001 §9.8

#### Scenario: Untrusted content arrives as quoted data
- **WHEN** a tool result contains a bank description, an email or PDF text
- **THEN** that content is passed to the model as quoted data, never as instructions

#### Scenario: The system prompt says so
- **WHEN** the system prompt is built
- **THEN** it tells the model that untrusted content in tool results is data, not instructions

#### Scenario: The catalogue and human approval bound the damage
- **GIVEN** an injection gets past the other defences
- **WHEN** it leads the AI to act
- **THEN** the AI can only propose a catalogue task type, which a human must approve

#### Scenario: Model Armor screens prompts and documents
- **WHEN** a prompt or a document is sent to the model
- **THEN** it is screened by Model Armor, which is available in Singapore with in-region processing

### Requirement: AI-19 Excessive agency
The AI SHALL have no write tools and SHALL act through proposals only, bounded by the tiered catalogue, per-proposal limits and the kill switch, against excessive agency.

Source: PRD-001 §9.8

#### Scenario: No write tools
- **WHEN** the AI's tools are listed
- **THEN** none of them writes

#### Scenario: Proposals only, from a tiered catalogue
- **WHEN** the AI wants a change
- **THEN** it can only propose it, as a task type from the tiered catalogue

#### Scenario: Per-proposal limits
- **WHEN** the AI proposes a change
- **THEN** the proposal is bounded by its task type's per-proposal limits

#### Scenario: Kill switch
- **WHEN** the Owner uses the kill switch
- **THEN** the assistant, the propose tool or the task type it covers is disabled (AI-13)

### Requirement: AI-20 Sensitive information disclosure
The system SHALL apply the AI exclusions (AI-02), masking, on-behalf-of authorisation and an output filter for NRIC and bank-number patterns, against sensitive information disclosure.

Source: PRD-001 §9.8

#### Scenario: Exclusions and masking on input
- **WHEN** the AI reads data
- **THEN** the AI exclusions (AI-02) and masking are applied

#### Scenario: On-behalf-of authorisation
- **WHEN** the AI reads data for a user
- **THEN** it sees only what that user may see

#### Scenario: Output filter for NRIC and bank-number patterns
- **WHEN** assistant output contains a string matching an NRIC or bank-number pattern
- **THEN** the output filter stops it from being shown

### Requirement: AI-21 Hallucinated figures
The system SHALL apply numeric grounding (AI-08) and citations linking every figure to its report or records, against hallucinated figures.

Source: PRD-001 §9.8

#### Scenario: Numeric grounding
- **WHEN** the AI answers with figures
- **THEN** the numeric grounding check (AI-08) runs on them

#### Scenario: Every figure is cited
- **WHEN** the AI shows a figure
- **THEN** a citation links it to its report or records

### Requirement: AI-22 Over-reliance
AI content SHALL be visually marked (accent and an "AI" badge), proposals SHALL show evidence and impact, and approvers SHALL see "what could go wrong" notes for tier 2, against over-reliance.

Source: PRD-001 §9.8

#### Scenario: AI content is visually marked
- **WHEN** AI content is shown
- **THEN** it is marked with the accent and an "AI" badge

#### Scenario: Proposals show evidence and impact
- **WHEN** a proposal is shown
- **THEN** it shows its evidence and its impact

#### Scenario: Tier-2 approvers see what could go wrong
- **GIVEN** a tier-2 proposal
- **WHEN** an approver reviews it
- **THEN** they see "what could go wrong" notes

### Requirement: AI-23 Denial of wallet
The system SHALL enforce token budgets, rate limits, a maximum number of tool calls per turn and a maximum conversation length with compaction, against denial of wallet.

Source: PRD-001 §9.8

#### Scenario: Token budgets and rate limits
- **WHEN** the assistant is used
- **THEN** token budgets and rate limits apply

#### Scenario: Maximum tool calls per turn
- **WHEN** a turn reaches the maximum number of tool calls
- **THEN** no further tool call is made in that turn

#### Scenario: Maximum conversation length with compaction
- **WHEN** a conversation reaches its maximum length
- **THEN** it is compacted

### Requirement: AI-24 Supply chain and model change
Model IDs SHALL be pinned per environment, and a model or prompt change SHALL run the eval suite (QA-AI) before rollout, against supply-chain and model-change risk.

Source: PRD-001 §9.8

#### Scenario: Model IDs are pinned per environment
- **WHEN** an environment calls a model
- **THEN** it uses the model ID pinned for that environment

#### Scenario: The eval suite runs before rollout
- **WHEN** a model or prompt change is prepared
- **THEN** the eval suite (QA-AI) runs before it is rolled out

### Requirement: AI-25 Data poisoning of retrieval sources
The knowledge base (user guide, policies, statutory notes) SHALL be curated and versioned, and organisation documents SHALL be labelled untrusted, against data poisoning of retrieval sources.

Source: PRD-001 §9.8

#### Scenario: Curated, versioned knowledge base
- **WHEN** the knowledge base changes
- **THEN** the change is curated and versioned

#### Scenario: Organisation documents are labelled untrusted
- **WHEN** an organisation document is used as a source
- **THEN** it is labelled untrusted

#### Scenario: Compiled pages inherit taint and data class
- **WHEN** a page is compiled
- **THEN** it inherits the taint and data class of its sources

#### Scenario: P1 compiles trusted sources only
- **GIVEN** phase P1
- **WHEN** pages are compiled
- **THEN** only trusted sources are compiled

#### Scenario: Pages from documents need approval
- **WHEN** a page is compiled from documents
- **THEN** it needs approval before it is published (AI-54)

### Requirement: AI-69 Exfiltration and manipulation through output
Assistant output SHALL be rendered from a fixed Markdown subset, and authorisation, egress and execution checks SHALL run outside the model, against exfiltration and manipulation through output.

Source: PRD-001 §9.8

#### Scenario: No raw HTML, images or remote fetches
- **WHEN** assistant output is rendered
- **THEN** it uses the fixed Markdown subset, with no raw HTML and no images or other remote fetches

#### Scenario: Links only to the Fyniq origin or cited sources
- **WHEN** assistant output contains a link
- **THEN** it is rendered only if it points to the Fyniq origin or to a user-visible source the answer cites
- **AND** external URLs are shown in full

#### Scenario: Checks outside the model hold
- **WHEN** the model outputs anything
- **THEN** authorisation, egress and execution checks run outside the model and hold whatever it outputs

#### Scenario: Review screen comes from platform code
- **WHEN** a proposal is shown on the review screen
- **THEN** its summary and impact come from platform code (AI-07)
- **AND** the model's rationale is labelled as AI text

### Requirement: External agents and MCP clients cannot abuse identity or tools
Against identity and privilege abuse and tool misuse by external agents and MCP clients (OWASP agentic ASI03, ASI02), the system SHALL apply client classes with agents proposal-only (AI-34), audience-bound short-lived tokens, no token passthrough, allowlisted clients and the MCP red-team suite (AI-35…46, QA-MCP-01).

Source: PRD-001 §9.8

#### Scenario: Agent clients only propose
- **WHEN** an agent-class client acts
- **THEN** it can create proposals but never write, approve or execute

#### Scenario: Tokens are audience-bound and never passed through
- **WHEN** an MCP client calls the API
- **THEN** its token is short-lived and bound to the MCP audience, and is exchanged rather than passed through

#### Scenario: Only allowlisted clients connect
- **WHEN** a client not on the allowlist connects
- **THEN** it is refused
