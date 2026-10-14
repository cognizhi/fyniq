# people/performance Specification

## Purpose
Performance and KPIs in Nemura: goals, KPIs, check-ins, review cycles and pay outcomes under payroll approval. Derived from PRD-002 §13.

## Requirements

### Requirement: NM-PRF-01 Goals
Nemura SHALL keep company, team and personal goals with measurable key results, owners, due dates and progress, aligned where the owner chooses.

Source: PRD-002 §13 · Priority M · Phase P2

#### Scenario: Aligned goal
- **WHEN** a person adds a goal aligned to a team goal
- **THEN** its progress shows under the team goal

### Requirement: NM-PRF-02 KPIs
Role KPIs SHALL have targets and a source, entered or read from Fyniq's permitted metrics, and a KPI built on a company financial figure SHALL show only its status in Nemura.

Source: PRD-002 §13 · Priority S · Phase P2

#### Scenario: Financial KPI hidden
- **GIVEN** a KPI reads revenue from Fyniq
- **WHEN** anyone views the goals page in Nemura
- **THEN** the KPI shows only its status (on track, behind), and its figure is in Fyniq

### Requirement: NM-PRF-03 Check-ins and feedback
One-to-one notes and feedback SHALL be private to the two people and HR unless shared.

Source: PRD-002 §13 · Priority S · Phase P2

#### Scenario: Private
- **WHEN** a manager writes a check-in note
- **THEN** only the person, the manager and HR can read it

### Requirement: NM-PRF-04 Review cycles
A review cycle SHALL have dates, a form for self, manager and optional peers, a calibration step, and results released to each person, with ratings restricted.

Source: PRD-002 §13 · Priority M · Phase P2

#### Scenario: Release
- **WHEN** calibration is done and HR releases the cycle
- **THEN** each person sees their own result, and ratings stay restricted

### Requirement: NM-PRF-05 Pay outcomes
A recommended increment or bonus SHALL become a pay change or additional-wage input only after the Payroll administrator prepares it and the Owner approves it.

Source: PRD-002 §13 · Priority M · Phase P2

#### Scenario: Bonus
- **WHEN** a review recommends a bonus
- **THEN** it reaches the pay run only after the Payroll administrator prepares it and the Owner approves it
