# people/time Specification

## Purpose
Working patterns, Part IV overtime and, later, clocking and timesheets. Derived from PRD-002 §15.

## Requirements

### Requirement: NM-TIM-01 Working patterns
Each person SHALL have a working pattern of hours and days, used by leave and by available hours in Fyniq.

Source: PRD-002 §15 · Priority M · Phase P1

#### Scenario: Part-time
- **GIVEN** a person who works Monday to Wednesday
- **WHEN** they take a week of leave
- **THEN** 3 days are deducted

### Requirement: NM-TIM-02 Overtime for Part IV employees
Overtime for employees covered by Part IV SHALL be requested and approved, paid at not less than 1.5 times the hourly basic rate within the monthly limit, and paid within 14 days of the salary period's end.

Source: PRD-002 §15 · Priority M · Phase P1

#### Scenario: Rate
- **GIVEN** a non-workman earning S$2,400 basic a month
- **WHEN** 4 hours of overtime are approved
- **THEN** they are paid at not less than 1.5 times the hourly basic rate in the next pay run

### Requirement: NM-TIM-03 Clocking and timesheets
Clocking on a phone or kiosk, shift rosters and timesheets SHALL feed overtime, and project time SHALL stay in Fyniq when linked.

Source: PRD-002 §15 · Priority C · Phase P3

#### Scenario: Timesheet to overtime
- **WHEN** a timesheet with approved overtime is submitted
- **THEN** the overtime becomes a pay-run input
