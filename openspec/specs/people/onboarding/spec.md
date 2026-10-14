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
Every joiner who may claim expenses SHALL be invited to Fyniq with the Employee role only, by a person with the right to invite, from a task raised when the person is added (until onboarding plans exist) or listed when the link starts; Nemura SHALL never grant Fyniq roles itself.

Source: PRD-002 §14 · Priority M · Phase P1

#### Scenario: Invite task
- **GIVEN** Joanne Lee is added in Nemura, starting Mon 2 Nov
- **WHEN** the record is saved
- **THEN** People home shows the task "Invite to Fyniq for My claims" for Sarah Koh, who may invite, due Mon 2 Nov
- **AND** the invitation gives the Employee role only; Nemura grants nothing in Fyniq

#### Scenario: When the link starts
- **WHEN** Nemura is linked to Fyniq
- **THEN** the people without a Fyniq account are listed for invitation

#### Scenario: Leaving
- **WHEN** a person leaves
- **THEN** their Fyniq access is removed at their leaving date (NM-ONB-03)

### Requirement: NM-ONB-03 Offboarding
Offboarding SHALL cover the last day, final pay, leave settlement, the IR21 trigger, equipment, access removal in Nemura and a reminder for Fyniq, the exit interview and the service letter.

Source: PRD-002 §14 · Priority M · Phase P1 (final pay, IR21, access) / P2 (plans)

#### Scenario: Leaver
- **WHEN** a resignation is recorded with a last day
- **THEN** final pay, leave settlement, the IR21 trigger where it applies, access removal and the service letter are scheduled

#### Scenario: Clocks kept apart
- **WHEN** a resignation or termination is recorded
- **THEN** the date the company learned of it, the last day and, for a retrenchment, the date the employees were told are kept apart, each starting its own clock
