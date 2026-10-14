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
Overtime for employees covered by Part IV (workmen up to S$4,500 and non-workmen up to S$2,600 basic monthly salary) SHALL be requested and approved, paid at not less than 1.5 times the hourly basic rate within the monthly limit, and paid within 14 days of the salary period's end.

Source: PRD-002 §15 · Priority M · Phase P1

#### Scenario: Rate
- **GIVEN** a non-workman earning S$2,400 basic a month
- **WHEN** 4 hours of overtime are approved
- **THEN** they are paid at not less than 1.5 times the hourly basic rate in the next pay run

#### Scenario: Hours from records
- **WHEN** overtime is paid
- **THEN** its hours come from NM-TIM-04's records

### Requirement: NM-TIM-03 Clocking and timesheets
Clocking on a phone or kiosk, shift rosters and timesheets SHALL feed overtime, and project time SHALL stay in Fyniq when linked.

Source: PRD-002 §15 · Priority C · Phase P3

#### Scenario: Timesheet to overtime
- **WHEN** a timesheet with approved overtime is submitted
- **THEN** the overtime becomes a pay-run input

### Requirement: NM-TIM-04 Hours worked and special days
For Part IV employees, overtime, rest-day and public-holiday work SHALL be recorded per day, approved and used once by a pay run, and paid by its branch; work beyond the monthly limit SHALL stay recorded and paid and be flagged for HR.

Source: PRD-002 §15 · Priority M · Phase P1

#### Scenario: Used once
- **WHEN** approved hours are in the October run
- **THEN** they cannot enter another run

#### Scenario: Rest-day work
- **GIVEN** a Part IV employee works on their designated rest day at the employer's request
- **WHEN** their pay is calculated
- **THEN** they earn 1 day's salary for up to half their normal daily hours, 2 days' salary for more, and overtime pay beyond their normal hours (MOM)

#### Scenario: Beyond the monthly limit
- **WHEN** a month's overtime goes beyond 72 hours
- **THEN** the hours stay recorded and are paid, and HR sees ⚠ to act on
