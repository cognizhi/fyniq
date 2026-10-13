# ai/scheduled-agents Specification

## Purpose
The framework for scheduled agents, whose first agent is the AI company secretary assistant and its recommended default schedule: agents defined by versioned skill files, their schedules and triggers, the identity and permissions they run with, deterministic checks before any model call, the run reports, tasks and drafts they produce, what an agent never does, and their budgets and failure handling. Derived from PRD-001 §9.12.

## Requirements

### Requirement: AI company secretary assistant
The first scheduled agent SHALL be the AI company secretary assistant: on a schedule it checks the books and the company's details, cross-checks them against Singapore's corporate and tax obligations, and prepares what is due.

Source: PRD-001 §9.12

#### Scenario: The agent checks, explains, reminds and drafts
- **WHEN** the AI company secretary assistant runs
- **THEN** it checks, explains, reminds and drafts

#### Scenario: People decide, sign and file
- **WHEN** something must be decided, signed or filed
- **THEN** the named human secretary and the directors do it, not the agent

#### Scenario: An assistant to the secretary, not the secretary
- **GIVEN** in Singapore the company secretary must be a natural person whose principal or only place of residence is in Singapore, and a sole director cannot also be the secretary (Companies Act s171)
- **WHEN** the agent is named and presented
- **THEN** it is the AI company secretary assistant, never the company secretary

#### Scenario: The agent follows the AI principles
- **WHEN** the agent reads or wants a change
- **THEN** it follows every AI principle of §9.1: it reads within permissions
- **AND** it changes nothing except by proposals a person approves

### Requirement: Recommended default schedule
The AI company secretary's schedule SHALL default to the recommended schedule below, as settings under FR-ORG-08, each entry stating its "Model used?" value.

Source: PRD-001 §9.12

#### Scenario: Daily, 07:00 SGT
- **WHEN** it is 07:00 SGT on any day
- **THEN** the deadline and lodgement monitor (deterministic) runs
- **AND** the model is used only if something changed

#### Scenario: Weekly, Monday 08:00 SGT
- **WHEN** it is Monday 08:00 SGT
- **THEN** the full cross-check (FR-CS-03) runs, with a summary for the directors
- **AND** the model is used

#### Scenario: After each month's close
- **WHEN** a month is closed
- **THEN** the books-to-registers consistency check runs and reports new findings
- **AND** the model is used

#### Scenario: FY end + 1 month
- **WHEN** one month has passed since the FY end
- **THEN** the FY-end pack runs: financial statements route, AGM or dispensation, annual return, ECI
- **AND** the model is used

#### Scenario: Events
- **WHEN** shares are issued or transferred, an officer, address or controller changes, a filing is recorded, or a deadline is 30/14/7/1 days ahead
- **THEN** the agent runs for that event
- **AND** the model is used only for drafts

### Requirement: AI-61 Agent skills
Each scheduled agent SHALL be defined by a versioned skill file, `agents/<name>/SKILL.md`, holding its instructions, its checklist, the read tools it may call and the outputs it may produce.

Source: PRD-001 §9.12 · Priority M · Phase P1

#### Scenario: The skill file bounds the agent
- **WHEN** an agent runs
- **THEN** it follows its skill file's instructions and checklist
- **AND** it calls only the read tools and produces only the outputs its skill file allows

#### Scenario: Hash-pinned per release
- **WHEN** a release is built
- **THEN** each agent's skill file is hash-pinned for that release

#### Scenario: Changes are dual-approved and eval-gated
- **WHEN** a skill file is changed
- **THEN** the change needs dual approval and must pass the evals (CMP-AI-03, QA-AI-07), like a change to the task catalogue

### Requirement: AI-62 Schedules and triggers
Each organisation SHALL configure, per agent, its schedule and triggers, quiet hours, recipients and channels, escalation, "Run now" and pause, all with recommended defaults (FR-ORG-08).

Source: PRD-001 §9.12 · Priority M · Phase P1

#### Scenario: Cron-style schedule in Singapore time
- **WHEN** an agent's schedule is set
- **THEN** it is a cron-style schedule in Singapore time

#### Scenario: Event triggers
- **WHEN** the FY ends, a period closes, shares are issued or transferred, an officer or holder changes, a filing is recorded, or a deadline is 30, 14, 7 or 1 days ahead
- **THEN** the agent can be triggered by that event

#### Scenario: Quiet hours, recipients and channels
- **WHEN** an agent is configured
- **THEN** its quiet hours, recipients and channels (Inbox and email; Slack from P2) can be set

#### Scenario: Escalation before a deadline
- **WHEN** a deadline is 7 days away
- **THEN** the agent escalates it to the Owner

#### Scenario: Run now and pause
- **WHEN** a user chooses "Run now" or pauses the agent
- **THEN** the agent runs at once, or stops running until resumed

#### Scenario: Recommended defaults
- **GIVEN** a new organisation
- **WHEN** an agent's schedule and triggers are first shown
- **THEN** they hold their recommended defaults and stay configurable (FR-ORG-08)

#### Scenario: Schedules live in the database
- **WHEN** the Scheduler tick runs
- **THEN** it finds the due runs in the database and enqueues each as a named Cloud Task at its exact time
- **AND** there is no Scheduler job per schedule (ADR-T7)

#### Scenario: Each run enqueues its next slot
- **WHEN** a run finishes
- **THEN** it enqueues its next slot
- **AND** nothing runs between ticks

### Requirement: AI-63 Identity and permissions
An agent SHALL run as a named non-human principal (e.g. "Company secretary agent") on behalf of the Owner who enabled it, intersected with the agent's permission profile.

Source: PRD-001 §9.12 · Priority M · Phase P1

#### Scenario: The company secretary's permission profile
- **WHEN** the company secretary agent reads data
- **THEN** it can read ledger summaries, the statutory calendar, the equity register, officers and controllers, settings and evidence metadata, within what the enabling Owner can see

#### Scenario: No payroll detail unless AI payroll access is on
- **GIVEN** AI payroll access is off
- **WHEN** an agent reads data
- **THEN** it gets no payroll detail

#### Scenario: The AI-02 exclusions always apply
- **WHEN** an agent reads data
- **THEN** the AI-02 exclusions always apply

#### Scenario: Off by default, enabled under FM-1
- **GIVEN** a new organisation
- **WHEN** the Owner enables an agent
- **THEN** the agent was off until then
- **AND** enabling it is an AI-settings change under FM-1

### Requirement: AI-64 Deterministic first
A run SHALL execute the deterministic checks (checks engine, statutory calendar, reconciliations, registers) first, and SHALL call the model only to explain findings, cross-check what rules cannot, and draft.

Source: PRD-001 §9.12 · Priority M · Phase P1

#### Scenario: Deterministic checks run first
- **WHEN** a run starts
- **THEN** the checks engine, statutory calendar, reconciliation and register checks run before any model call

#### Scenario: What the model is called for
- **WHEN** the model is called during a run
- **THEN** it is only to explain findings, cross-check what rules cannot, or draft

#### Scenario: No changed input, no model step
- **WHEN** no input has changed since the last run
- **THEN** the model step is skipped

#### Scenario: Inputs include dates, windows, packs and the knowledge base
- **WHEN** a run decides whether an input changed
- **THEN** its inputs include the business date, every deadline window (30, 14, 7, 1 days), the pack versions and the knowledge-base snapshot, not only data

#### Scenario: Reproducibility covers the deterministic findings
- **WHEN** a run is reproduced (AI-65)
- **THEN** its deterministic findings are reproduced
- **AND** the model text is the text stored as written, not regenerated

#### Scenario: G4-076 Deadline window reached with no data change
- **GIVEN** a GST F5 deadline reaches 7 days with no data change
- **WHEN** the agent runs twice
- **THEN** the ⏰ finding and the reminder appear (the business date is an input)
- **AND** there is one Inbox task, updated, not two

### Requirement: AI-65 Outputs
A run SHALL produce a run report, Inbox tasks with an owner and a due date, and drafts as tier-1 proposals (documents and reminder emails, sent only after approval).

Source: PRD-001 §9.12 · Priority M · Phase P1

#### Scenario: What the run report shows for each finding
- **WHEN** a run report is produced
- **THEN** each finding shows its status (✔ OK, ⚠ please review, ✖ cannot post until fixed, ⏰ due or overdue), the rule and source it applied, links to the evidence and what to do next

#### Scenario: Not assessed when an input is missing or out of date
- **WHEN** an input to a finding is missing or out of date (for example no recorded control rights, an ACRA extract older than the last recorded change or than its age limit (setting), or a pack update not yet accepted)
- **THEN** the finding's status is ⚠ Not assessed

#### Scenario: No "all clear" while anything is open
- **WHEN** any item is ✖, ⏰ or Not assessed
- **THEN** the summary never says "all clear"

#### Scenario: Deterministic findings create tasks directly
- **WHEN** a deterministic check produces a finding
- **THEN** it creates its Inbox task directly through the system principal, as the statutory calendar does (FR-TAX-07)

#### Scenario: Model-suggested tasks are proposals
- **WHEN** the model suggests a task
- **THEN** it is a `sec.create_task` proposal

#### Scenario: One open task per finding
- **WHEN** a repeat run finds the same finding
- **THEN** it updates the finding's open task
- **AND** there is only one open task per finding

#### Scenario: Drafts are tier-1 proposals
- **WHEN** a run drafts a document or a reminder email
- **THEN** the draft is a tier-1 proposal
- **AND** it is sent only after approval

#### Scenario: Run reports are append-only and reproducible
- **WHEN** a run report is stored
- **THEN** it is append-only
- **AND** it records the ledger revision (ENG-46), the knowledge-base snapshot and the pack versions used, so the run can be reproduced (FR-RPT-08)

#### Scenario: G4-103 Stale ACRA extract and an unaccepted pack update
- **GIVEN** the latest ACRA extract predates the last share change
- **AND** a mandatory pack update has not been accepted
- **WHEN** the agent runs
- **THEN** the affected checks are Not assessed
- **AND** figures are labelled with the pack version
- **AND** there is no unqualified "all clear"

### Requirement: AI-66 What an agent never does
An agent SHALL never file or lodge with ACRA, IRAS or CPF, sign, approve, change officers, registers, holders or settings, send anything outside the organisation without approval, or mark an obligation as met.

Source: PRD-001 §9.12 · Priority M · Phase P1

#### Scenario: Forbidden actions
- **WHEN** an agent runs
- **THEN** it does not file or lodge with ACRA, IRAS or CPF, sign, approve, change officers, registers, holders or settings, or mark an obligation as met

#### Scenario: Nothing leaves the organisation without approval
- **WHEN** an agent has something to send outside the organisation
- **THEN** it is not sent without approval

#### Scenario: What an agent may do
- **WHEN** an agent acts
- **THEN** it only reports, has its deterministic findings create tasks (AI-65), and proposes drafts (§9.6)

### Requirement: AI-67 Budgets and failure
Each agent SHALL have a token budget per run and per month, a cap on tool calls and the Owner's instant pause (AI-13), and a failed, skipped or late run SHALL itself be ⚠ (please review) on Checks.

Source: PRD-001 §9.12 · Priority M · Phase P1

#### Scenario: Budgets and tool-call cap
- **WHEN** an agent runs
- **THEN** it is limited by its token budget per run and per month and by its cap on tool calls

#### Scenario: Owner's instant pause
- **WHEN** the Owner pauses an agent (AI-13)
- **THEN** the agent stops at once

#### Scenario: Failed, skipped or late runs show on Checks
- **WHEN** a run fails, is skipped or is late
- **THEN** it appears as ⚠ on Checks

#### Scenario: The deadline monitor keeps running
- **GIVEN** an agent is paused or over budget
- **WHEN** a deadline approaches
- **THEN** the deterministic deadline monitor (CMP-02) keeps running

#### Scenario: The tick is the watchdog
- **WHEN** the Scheduler tick runs
- **THEN** it compares expected and actual runs
- **AND** it raises the ⚠ where they differ
