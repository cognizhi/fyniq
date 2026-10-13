# compliance/ai-and-api-controls Specification

## Purpose
The AI, public-API, integration-client and MCP controls of the control registry (`compliance/controls/`): each control, its automated check and frequency, the evidence kept, and the frameworks it maps to (AG = IMDA agentic-AI framework; ISO = ISO/IEC 27001:2022 Annex A; SOC2 = SOC 2 Trust Services Criteria; ASVS = OWASP ASVS 5.0). Derived from PRD-001 §F.7 and §F.7a.

## Requirements

### Requirement: CTL-AI-01 AI identities hold no write privilege except proposal insert
AI identities SHALL hold no write privilege except inserting proposals, and the knowledge-base compiler job writing knowledge-base drafts. The control is checked on every change in CI and nightly, with a grant snapshot kept as evidence.

Source: PRD-001 §F.7

#### Scenario: Grant tests on every change and nightly
- **WHEN** a change runs through CI or the nightly check runs
- **THEN** the IAM and database grant tests (CMP-AI-01) confirm that the AI identities have no write grant except inserting proposals
- **AND** a grant snapshot is kept as evidence

#### Scenario: The knowledge-base compiler job writes drafts only
- **WHEN** the knowledge-base compiler job writes
- **THEN** it writes knowledge-base drafts only

#### Scenario: Framework mapping
- **WHEN** the control is listed in the control registry
- **THEN** it is mapped to AG (bound risks) and OWASP LLM06 / ASI02–03

### Requirement: CTL-AI-02 Every executed proposal has a matching human approval
Every executed proposal SHALL have a matching human approval with a valid payload hash. The control is checked nightly, with the reconciliation log kept as evidence.

Source: PRD-001 §F.7

#### Scenario: Nightly reconciliation
- **WHEN** the nightly reconciliation runs
- **THEN** every executed proposal is matched to a human approval whose payload hash is valid
- **AND** the reconciliation log is kept as evidence

#### Scenario: An exception raises an alert
- **GIVEN** an executed proposal without a matching human approval or with an invalid payload hash
- **WHEN** the nightly reconciliation runs
- **THEN** an alert is raised, the target being 0 exceptions

#### Scenario: Framework mapping
- **WHEN** the control is listed in the control registry
- **THEN** it is mapped to AG (human checkpoints) and ETA

### Requirement: CTL-AI-03 No automatic approval of AI proposals is configurable
No setting SHALL make it possible to approve AI proposals automatically. The control is checked on every change in CI, with the test report kept as evidence.

Source: PRD-001 §F.7

#### Scenario: Policy-engine test on every change
- **WHEN** a change runs through CI
- **THEN** the policy-engine test confirms that no configuration approves an AI proposal automatically
- **AND** the test report is kept as evidence

#### Scenario: Framework mapping
- **WHEN** the control is listed in the control registry
- **THEN** it is mapped to AG

### Requirement: CTL-AI-04 Catalogue changes dual-approved; excluded task types rejected
Every change to the AI task catalogue SHALL be dual-approved, and an excluded task type SHALL be rejected. The control is checked on every change in CI, with the change records kept as evidence.

Source: PRD-001 §F.7

#### Scenario: Catalogue change needs dual approval
- **WHEN** the catalogue is changed
- **THEN** the change needs dual approval
- **AND** the change records are kept as evidence

#### Scenario: Excluded task type rejected
- **WHEN** an excluded task type is added to the catalogue
- **THEN** it is rejected

#### Scenario: Catalogue tests on every change
- **WHEN** a change runs through CI
- **THEN** the catalogue tests check the dual approval and the rejection of excluded task types

#### Scenario: Framework mapping
- **WHEN** the control is listed in the control registry
- **THEN** it is mapped to AG and ISO 8.32

### Requirement: CTL-AI-05 Evals and red-team suite pass before any model, prompt or tool change
The evals and the red-team suite SHALL pass before any model, prompt or tool change. The control is a CI gate, with the eval report kept as evidence.

Source: PRD-001 §F.7

#### Scenario: Model, prompt or tool change gated
- **GIVEN** a change to a model, a prompt or a tool
- **WHEN** the CI gate runs
- **THEN** the change goes ahead only if the evals and the red-team suite pass
- **AND** the eval report is kept as evidence

#### Scenario: Framework mapping
- **WHEN** the control is listed in the control registry
- **THEN** it is mapped to AG (testing), the IMDA Starter Kit and OWASP LLM01/02/06/09

### Requirement: CTL-AI-06 AI exclusions enforced
The AI exclusions SHALL be enforced: no NRIC, no bank numbers and no payroll data unless enabled. The control is checked on every change in CI and weekly, with the reports kept as evidence.

Source: PRD-001 §F.7

#### Scenario: Redaction tests on every change
- **WHEN** a change runs through CI
- **THEN** the redaction tests confirm that no NRIC or bank number reaches the AI
- **AND** that payroll data reaches it only when enabled

#### Scenario: Weekly trace scans
- **WHEN** the weekly trace scans run (CTL-DP-03)
- **THEN** they confirm that the AI traces hold no excluded data
- **AND** the reports are kept as evidence

#### Scenario: Framework mapping
- **WHEN** the control is listed in the control registry
- **THEN** it is mapped to PDPA and OWASP LLM02

### Requirement: CTL-AI-07 Approval-quality metrics reviewed
The approval-quality metrics of AI proposals SHALL be reviewed every month, with the signed review kept as evidence.

Source: PRD-001 §F.7

#### Scenario: Monthly report and sign-off
- **WHEN** a month's review falls due
- **THEN** a report of the approval-quality metrics is produced and signed off
- **AND** the signed review is kept as evidence

#### Scenario: Framework mapping
- **WHEN** the control is listed in the control registry
- **THEN** it is mapped to AG (automation bias)

### Requirement: CTL-AI-08 The knowledge base is grounded and access-filtered
The knowledge base SHALL be grounded and access-filtered: published pages pass lint, the index is filtered per reader, and a recompile SHALL never overwrite a human edit. The control runs on every change in CI, nightly and before each publish.

Source: PRD-001 §F.7

#### Scenario: Knowledge-base lint on every change
- **WHEN** a change runs through CI
- **THEN** `make kb-lint` confirms that published pages have no stale sources, every number is in a cited source and every citation resolves
- **AND** the lint reports are kept as evidence

#### Scenario: Nightly reader-filter test
- **WHEN** the nightly test runs
- **THEN** it confirms that the index is filtered per reader

#### Scenario: Eval before each publish
- **WHEN** the knowledge base is about to be published
- **THEN** QA-AI-06 runs first
- **AND** the eval reports are kept as evidence

#### Scenario: Human edits survive a recompile
- **GIVEN** a page with a human edit
- **WHEN** the knowledge base is recompiled
- **THEN** the human edit is not overwritten

#### Scenario: Framework mapping
- **WHEN** the control is listed in the control registry
- **THEN** it is mapped to AG (data quality) and OWASP LLM04 (data and model poisoning)

### Requirement: CTL-AI-09 Scheduled agents stay inside their skill
Scheduled agents SHALL stay inside their skill: tool calls only from the pinned allowlist; outputs only reports, tasks and proposals; schedule and enablement changes audited and dual-controlled; and runs reproducible. The control is checked on every change in CI and nightly.

Source: PRD-001 §F.7

#### Scenario: Skill hash and never-list on every change
- **WHEN** a change runs through CI
- **THEN** each agent's skill hash is pinned and the never-list test passes for each agent
- **AND** the pinned hashes are kept as evidence

#### Scenario: Nightly tool-call check
- **WHEN** the nightly check runs
- **THEN** every run's tool calls are in its agent's allowlist
- **AND** the run reports are kept as evidence

#### Scenario: Outputs limited
- **WHEN** a scheduled agent runs
- **THEN** its only outputs are reports, tasks and proposals

#### Scenario: Schedule and enablement changes
- **WHEN** an agent's schedule or enablement is changed
- **THEN** the change is audited and needs dual control

#### Scenario: Runs are reproducible
- **WHEN** a past run is examined
- **THEN** it can be reproduced

#### Scenario: Framework mapping
- **WHEN** the control is listed in the control registry
- **THEN** it is mapped to AG (bound autonomy) and OWASP LLM06 (excessive agency)

### Requirement: CTL-API-01 The public API is the only API
The public API SHALL be the only API, and its contracts SHALL be complete and backwards-compatible. The control is checked on every change in CI, with the contract report kept as evidence.

Source: PRD-001 §F.7a

#### Scenario: Contract checks on every change
- **WHEN** a change runs through CI
- **THEN** QA-API-01…03 and the Spectral lint (NFR-API-01) pass
- **AND** the contract report is kept as evidence

#### Scenario: Framework mapping
- **WHEN** the control is listed in the control registry
- **THEN** it is mapped to SOC2 CC8.1 and PI1 and ASVS V13

### Requirement: CTL-API-02 Integration clients have an owner, a class, an expiry and least-privilege scopes
Every integration client SHALL have an owner, a class, an expiry and least-privilege scopes. The control is checked nightly and reviewed quarterly, with the client inventory kept as evidence.

Source: PRD-001 §F.7a

#### Scenario: Nightly client check
- **WHEN** the nightly check runs
- **THEN** the number of expired or ownerless clients is 0
- **AND** the client inventory is kept as evidence

#### Scenario: Quarterly review
- **WHEN** a quarter's review falls due
- **THEN** the integration clients and their scopes are reviewed

#### Scenario: Framework mapping
- **WHEN** the control is listed in the control registry
- **THEN** it is mapped to ISO 5.15 and 5.18 and SOC2 CC6.1–6.3

### Requirement: CTL-MCP-01 Agent-class tokens cannot write except by propose
An agent-class token SHALL NOT write except by `propose` (AI-34). The control is checked on every change in CI and nightly, with the test report kept as evidence.

Source: PRD-001 §F.7a

#### Scenario: MCP security suite on every change
- **WHEN** a change runs through CI
- **THEN** QA-MCP-01 confirms that an agent-class token cannot write except by `propose`
- **AND** the test report is kept as evidence

#### Scenario: Nightly sample of agent-class writes
- **WHEN** the nightly check samples writes
- **THEN** the number of agent-class writes is 0

#### Scenario: Framework mapping
- **WHEN** the control is listed in the control registry
- **THEN** it is mapped to AG (bound risks) and OWASP ASI02–03

### Requirement: CTL-MCP-02 MCP tool surface is the registry minus the never-list
The MCP tool surface SHALL equal the tool registry minus the never-list, and the tool definitions SHALL match their pinned hashes (AI-40). The control is checked on every change in CI, with the tool manifest kept as evidence.

Source: PRD-001 §F.7a

#### Scenario: Tool surface checked on every change
- **WHEN** a change runs through CI
- **THEN** QA-MCP-01 confirms that the exposed tools are the registry minus the never-list
- **AND** that every tool definition matches its pinned hash
- **AND** the tool manifest is kept as evidence

#### Scenario: Framework mapping
- **WHEN** the control is listed in the control registry
- **THEN** it is mapped to AG and OWASP LLM01 and LLM06

### Requirement: CTL-MCP-03 MCP off by default; enabling and allowlisting dual-approved
MCP SHALL be off by default, and enabling it and allowlisting clients SHALL each be dual-approved (AI-35). The control is checked on every change in CI and on each enablement event, with the approval records kept as evidence.

Source: PRD-001 §F.7a

#### Scenario: Off by default
- **GIVEN** an organisation in which nobody has enabled MCP
- **WHEN** an MCP client connects
- **THEN** MCP is off for that organisation

#### Scenario: Enabling and allowlisting need dual approval
- **WHEN** MCP is enabled or a client is allowlisted
- **THEN** the change needs dual approval
- **AND** the approval records are kept as evidence

#### Scenario: Policy-engine test and enablement audit
- **WHEN** a change runs through CI
- **THEN** the policy-engine test confirms the default and the dual approval
- **AND** each enablement event is audited when it happens

#### Scenario: Framework mapping
- **WHEN** the control is listed in the control registry
- **THEN** it is mapped to AG (human checkpoints) and ISO 5.15

### Requirement: CTL-MCP-04 Tokens audience-bound, short-lived and revoked within 60 s
MCP tokens SHALL be audience-bound and valid for at most 60 minutes, and SHALL be revoked within 60 s of a role change or a kill switch (AI-36, AI-38). The control is checked on every change in CI, with the test report kept as evidence.

Source: PRD-001 §F.7a

#### Scenario: Token audience and lifetime
- **WHEN** an MCP token is issued
- **THEN** it is bound to its audience
- **AND** it is valid for at most 60 minutes

#### Scenario: Revocation timing test
- **GIVEN** a role change or a kill switch
- **WHEN** the QA-MCP-01 revocation timing test runs in CI
- **THEN** the affected tokens are revoked within 60 s
- **AND** the test report is kept as evidence

#### Scenario: Framework mapping
- **WHEN** the control is listed in the control registry
- **THEN** it is mapped to ASVS V10 (OAuth) and SOC2 CC6.1
