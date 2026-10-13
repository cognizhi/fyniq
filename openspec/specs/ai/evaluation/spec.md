# ai/evaluation Specification

## Purpose
How the AI assistant, its proposals, its knowledge base and its scheduled agents are evaluated before release and monitored in production: the golden question set, proposal evals, the red-team suite, production monitoring, user feedback, knowledge evals and agent evals. Derived from PRD-001 §9.10.

## Requirements

### Requirement: QA-AI-01 Golden question set
The system SHALL keep a golden question set on the sample company (≥ 200 questions with exact expected figures, built from the workbook's sample data and its verified expectations), and SHALL release a model or prompt change only at 100% numeric accuracy.

Source: PRD-001 §9.10 · Priority M · Phase P1

#### Scenario: 100% numeric accuracy gates a release
- **WHEN** a model or prompt change is evaluated on the golden question set
- **THEN** it is released only if numeric accuracy is 100%

#### Scenario: Value and attribution are both scored
- **WHEN** an answer is scored
- **THEN** it is scored on the value and on its attribution (measure, entity, period, currency)

#### Scenario: What the set includes
- **WHEN** the golden question set is built
- **THEN** it includes the right value for the wrong customer, month or currency, a tool that fails part-way, multi-step calculations through `calc`, and a figure reused from an earlier turn

### Requirement: QA-AI-02 Proposal evals
The system SHALL keep, for each task type, eval scenarios with the expected proposal payload, and SHALL measure the precision and recall of categorisation and matching against the sample ledger.

Source: PRD-001 §9.10 · Priority M · Phase P1

#### Scenario: Expected payload per task type
- **WHEN** the proposal evals run
- **THEN** each task type's scenarios compare the proposal produced with the expected proposal payload

#### Scenario: Precision and recall of categorisation and matching
- **WHEN** the proposal evals run on categorisation and matching
- **THEN** their precision and recall are measured against the sample ledger

### Requirement: QA-AI-03 Red-team suite
The red-team suite SHALL attack the assistant with injection, catalogue-excluded proposals, exfiltration, accumulated disclosure and misleading proposals, and 0 successes SHALL be required.

Source: PRD-001 §9.10 · Priority M · Phase P1

#### Scenario: What the suite attempts
- **WHEN** the red-team suite runs
- **THEN** it tries injection strings in bank descriptions, PDFs and emails, attempts to propose catalogue-excluded actions, attempts to exfiltrate excluded fields, Markdown-image and link exfiltration, and accumulated disclosure across many in-scope reads

#### Scenario: Injected text or a misleading rationale in an in-catalogue proposal
- **WHEN** the suite submits an in-catalogue proposal carrying injected text or a misleading rationale
- **THEN** the impact view still shows the true effect

#### Scenario: Zero successes required
- **WHEN** the red-team suite completes
- **THEN** it passes only with 0 successful attacks

### Requirement: QA-AI-04 Production monitoring
The system SHALL monitor in production the grounding failures, refusals, tool errors, approval and rejection rates per task type, edit-before-approve rate and cost per conversation.

Source: PRD-001 §9.10 · Priority S · Phase P1

#### Scenario: Monitored measures
- **WHEN** the assistant runs in production
- **THEN** grounding failures, refusals, tool errors, approval and rejection rates per task type, the edit-before-approve rate and the cost per conversation are monitored

### Requirement: QA-AI-05 User feedback
User feedback (thumbs up or down with a reason) SHALL feed the eval set after review.

Source: PRD-001 §9.10 · Priority S · Phase P1

#### Scenario: Feedback with a reason
- **WHEN** a user gives an answer a thumbs up or down
- **THEN** a reason is recorded with it

#### Scenario: Reviewed feedback feeds the eval set
- **WHEN** a feedback item has been reviewed
- **THEN** it feeds the eval set
- **AND** unreviewed feedback does not

### Requirement: QA-AI-06 Knowledge evals
The system SHALL keep knowledge evals of ≥ 100 golden help and policy questions with the expected guide pages and key facts, measuring answer accuracy, citation correctness and the stale-fallback rate.

Source: PRD-001 §9.10 · Priority M · Phase P1

#### Scenario: What is measured
- **WHEN** the knowledge evals run
- **THEN** they measure answer accuracy, citation correctness (every user-facing citation resolves and supports the claim) and the stale-fallback rate

#### Scenario: Knowledge evals gate publishing and compiler changes
- **WHEN** a knowledge-base publish (a platform release or an organisation publish) or a compiler model or prompt change is to go live
- **THEN** it must pass the knowledge evals first

#### Scenario: Retrieval misses are fixed by recompiling
- **WHEN** the knowledge evals find a retrieval miss
- **THEN** it is fixed by recompiling (summary, `aliases`, cross-links), never by adding vectors

#### Scenario: What the set includes
- **WHEN** the knowledge eval set is built
- **THEN** it includes paraphrases and synonyms, questions in languages other than English, negations and exceptions, conflicting effective dates, a stale guide against the pack in force, and a reader whose access was revoked

#### Scenario: Recall per category with an owner for weak categories
- **WHEN** the knowledge evals report
- **THEN** retrieval recall is reported per category
- **AND** a category below its threshold for two releases gets a named owner and a fix plan

### Requirement: QA-AI-07 Agent evals
The system SHALL evaluate each scheduled agent on golden companies with planted problems, requiring every planted ✖ and ⏰ found, no false "all clear", every draft matching its template, and no action from the red-team cases.

Source: PRD-001 §9.10 · Priority M · Phase P1

#### Scenario: Planted problems for the company secretary
- **WHEN** the company secretary agent is evaluated
- **THEN** its golden companies plant an annual return due, financial statements unapproved near their deadline, ledger share capital different from the register, an overdrawn director's account, a new holder above the controller threshold, a share issue with no ACRA acknowledgement, a controller through a board-appointment right only, a controller with no annual notice, a nominee shareholder not filed centrally, missing share-rights data, an ACRA extract older than the last share change, and an incomplete corporate profile with no FY end

#### Scenario: Every planted ✖ and ⏰ is found
- **WHEN** an agent is evaluated
- **THEN** it finds every planted ✖ (cannot post until fixed) and ⏰ (due or overdue) problem

#### Scenario: No false all clear
- **WHEN** an input is missing in a golden company
- **THEN** the agent gives Not assessed, never ✔
- **AND** it never gives a false "all clear"

#### Scenario: Drafts match their templates
- **WHEN** an agent produces drafts during the evals
- **THEN** every draft matches its template

#### Scenario: Red-team cases cause no action
- **WHEN** a golden company holds uploaded documents with injected text
- **THEN** the injected text causes no action
