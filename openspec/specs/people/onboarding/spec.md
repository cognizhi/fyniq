# people/onboarding Specification

## Purpose
Onboarding and offboarding in Nemura: plans from templates, Fyniq accounts as a task, and offboarding with final pay, IR21 and access removal. Derived from PRD-002 §14.

## Requirements

### Requirement: NM-ONB-01 Onboarding plans
Onboarding plans SHALL come from templates, with tasks for HR, the manager, IT and the joiner due relative to the start date.

Source: PRD-002 §14 · Priority M · Phase P2

#### Scenario: Plan from start date
- **WHEN** a joiner's start date is Mon 2 Nov 2026
- **THEN** the plan's tasks are due relative to it, KETs by Mon 16 Nov

### Requirement: NM-ONB-02 Accounts
When linked, inviting a joiner to Fyniq with the Employee role SHALL be an onboarding task done by a person with the right to invite; Nemura SHALL never grant Fyniq roles itself.

Source: PRD-002 §14 · Priority S · Phase P2

#### Scenario: Invite task
- **WHEN** the onboarding plan reaches "Invite to Fyniq"
- **THEN** a person with the right sends the invite; Nemura grants nothing in Fyniq

### Requirement: NM-ONB-03 Offboarding
Offboarding SHALL cover the last day, final pay, leave settlement, the IR21 trigger, equipment, access removal in Nemura and a reminder for Fyniq, the exit interview and the service letter.

Source: PRD-002 §14 · Priority M · Phase P1 (final pay, IR21, access) / P2 (plans)

#### Scenario: Leaver
- **WHEN** a resignation is recorded with a last day
- **THEN** final pay, leave settlement, the IR21 trigger where it applies, access removal and the service letter are scheduled
