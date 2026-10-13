# people/features-and-packs Specification

## Purpose
The optional features of Nemura and the country and region packs that hold the law, calendars and rules, with company policy on top. Derived from PRD-002 §5, §6 and Appendix A.

## Requirements

### Requirement: Singapore pack facts
The Singapore pack SHALL hold the facts of PRD-002 Appendix A, each with its source, effective date and [verify] status.

Source: PRD-002 §A

#### Scenario: Values to confirm
- **WHEN** a value from Appendix A is still marked [verify]
- **THEN** the pack screen shows it as "to confirm" with its source

### Requirement: NM-FEAT-01 Feature registry
People records SHALL always be on, and each optional feature SHALL be turned on or off per organisation (payroll with the Owner and a second approver); a feature that is off SHALL NOT be loaded.

Source: PRD-002 §5 · Priority M · Phase P1

#### Scenario: Off means not loaded
- **WHEN** a feature is off
- **THEN** it has no routes, menu items, jobs, events or assistant skills

#### Scenario: Payroll needs two people
- **WHEN** someone turns payroll on or off
- **THEN** the change needs the Owner and a second approver

### Requirement: NM-FEAT-02 Dependencies
Feature dependencies SHALL be declared and enforced, refusing a change that breaks one with `NM_FEATURE_REQUIRED`.

Source: PRD-002 §5 · Priority M · Phase P1

#### Scenario: Payslips need payroll
- **GIVEN** self-service payslips are on
- **WHEN** the HR administrator turns payroll off
- **THEN** the change is refused with `NM_FEATURE_REQUIRED` and the reason

### Requirement: NM-FEAT-03 External system per feature
A company SHALL be able to use an external system for payroll, leave or time instead of Nemura's feature, which then turns off and records the system that holds that data.

Source: PRD-002 §5 · Priority M · Phase P1

#### Scenario: External payroll
- **WHEN** the HR administrator chooses an external payroll system
- **THEN** Nemura's payroll is off, the external system is recorded as the holder of payroll data, and Fyniq takes the books' side from it

### Requirement: NM-FEAT-04 Turning off keeps data
Turning a feature off SHALL hide it and stop its jobs while its records stay under their retention rules.

Source: PRD-002 §5 · Priority M · Phase P1

#### Scenario: Back on
- **GIVEN** leave was turned off for a month
- **WHEN** it is turned on again
- **THEN** the leave records are there as they were

### Requirement: NM-PACK-01 Packs per country and region
A pack SHALL hold a country's leave types and entitlements, holiday calendar and observance rules, working-time rules, payroll rules, document requirements, record-keeping periods and retirement ages, and regions SHALL add or replace calendars.

Source: PRD-002 §6 · Priority M · Phase P1

#### Scenario: Region calendar
- **GIVEN** a worksite in a region that has its own holidays
- **WHEN** the calendar is built
- **THEN** the region's holidays are added to or replace the country's

### Requirement: NM-PACK-02 Effective-dated, sourced and tested
Every pack value SHALL have an effective date, a source URL and a checked date, SHALL be pinned by tests, and SHALL change only through a new pack version that the HR administrator accepts.

Source: PRD-002 §6 · Priority M · Phase P1

#### Scenario: New version
- **WHEN** a pack update arrives
- **THEN** the HR administrator sees what changes and accepts it before it applies

#### Scenario: To confirm
- **WHEN** a value is marked [verify]
- **THEN** it shows as "to confirm"

### Requirement: NM-PACK-03 Worksites
Each person SHALL work at a worksite whose country and region pick the pack and calendar; P1 supports Singapore only.

Source: PRD-002 §6 · Priority M · Phase P1 / P3 (several countries)

#### Scenario: Pack from worksite
- **WHEN** a person's leave or pay is calculated
- **THEN** the pack and calendar of their worksite apply

### Requirement: NM-PACK-04 Company policy on top
Company policy SHALL be effective-dated and SHALL NOT give less than the pack's statutory minimum, which is refused with `NM_POLICY_BELOW_STATUTE`.

Source: PRD-002 §6 · Priority M · Phase P1

#### Scenario: Below the law
- **WHEN** the HR administrator saves annual leave of 5 days for people with two years' service
- **THEN** it is refused with `NM_POLICY_BELOW_STATUTE` and the rule it breaks

#### Scenario: Better than the law
- **WHEN** a policy gives more than the statutory minimum
- **THEN** it is saved and marked as above the minimum

### Requirement: NM-PACK-05 Next year's calendar
The Singapore pack SHALL get next year's gazetted public holidays when MOM publishes them, and Checks SHALL warn from 1 Dec if they are missing.

Source: PRD-002 §6 · Priority M · Phase P1

#### Scenario: Missing calendar
- **GIVEN** next year's holidays are not loaded
- **WHEN** it is 1 Dec
- **THEN** Checks shows ⚠ (N.1 #8)

### Requirement: NM-PACK-06 The Singapore pack and the next packs
The Singapore pack SHALL hold Appendix A's facts at go-live, and a Malaysia pack SHALL be the first P3 example.

Source: PRD-002 §6 · Priority M · Phase P1 / P3 (Malaysia)

#### Scenario: Singapore at go-live
- **WHEN** Nemura goes live
- **THEN** the Singapore pack holds every Appendix A topic with its source and status
