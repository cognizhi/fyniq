# compliance/ai-governance Specification

## Purpose
How Fyniq governs its AI: a control test that keeps the AI service identities free of write grants and side-effecting tools, a current AI inventory and DPIA, and model, prompt and tool changes gated by evals and dual approval. Derived from PRD-001 §14.4.

## Requirements

### Requirement: CMP-AI-01 AI write-grant control test
A control test SHALL run in CI and nightly proving that the AI service identities have no write grants in IAM or the database except inserting proposals, and that every side-effecting tool is absent from the AI tool registry.

Source: PRD-001 §14.4 · Priority M · Phase P1

#### Scenario: Control test runs in CI and nightly
- **WHEN** CI runs, and every night
- **THEN** the AI write-grant control test runs

#### Scenario: Write grant found
- **WHEN** an AI service identity has a write grant in IAM or the database other than inserting proposals
- **THEN** the control test fails

#### Scenario: Side-effecting tool in the registry
- **WHEN** a side-effecting tool is present in the AI tool registry
- **THEN** the control test fails

#### Scenario: Knowledge-base compiler exception
- **WHEN** the knowledge-base compiler job's grants are checked
- **THEN** it may write knowledge-base draft versions only (AI-57)

### Requirement: CMP-AI-02 AI inventory and DPIA
The AI inventory (models, endpoints, prompts and tool versions, data categories, owners) and the AI DPIA SHALL be kept current in the Compliance Center.

Source: PRD-001 §14.4 · Priority M · Phase P1

#### Scenario: Inventory contents
- **WHEN** the AI inventory is viewed in the Compliance Center
- **THEN** it lists the models, endpoints, prompts and tool versions, data categories and owners

#### Scenario: Kept current
- **WHEN** a model, endpoint, prompt, tool version, data category or owner changes
- **THEN** the AI inventory and the DPIA are updated to match

### Requirement: CMP-AI-03 Gated AI changes
A model, prompt or tool change SHALL require the eval suite to pass and a dual-approved change record.

Source: PRD-001 §14.4 · Priority M · Phase P1

#### Scenario: Eval suite must pass
- **WHEN** a model, prompt or tool change fails the eval suite
- **THEN** the change is not made

#### Scenario: Dual-approved change record
- **WHEN** a model, prompt or tool change has no dual-approved change record
- **THEN** the change is not made

#### Scenario: Change goes ahead
- **GIVEN** the eval suite passes and the change record is dual-approved
- **WHEN** the model, prompt or tool change is applied
- **THEN** it takes effect
