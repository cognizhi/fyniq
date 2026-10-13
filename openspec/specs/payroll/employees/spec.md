# payroll/employees Specification

## Purpose
The employee register that payroll and projects draw on: each employee's identity, residency, age, department, employment dates, pay, self-help group fund, salary bank account and hourly cost override, and the mapping of departments to salary accounts and dimensions. Derived from PRD-001 §8.3.

## Requirements

### Requirement: FR-EMP-01 Employee records
The system SHALL keep employee records: identity, residency status and PR start date, date of birth, department, job title, start and end dates, salary and allowances, SHG fund, bank account for salary and an hourly cost override, which is the one effective-dated cost-rate override of FR-PRJ-06.

Source: PRD-001 §8.3 · Priority M · Phase P1

#### Scenario: Employee details are recorded
- **WHEN** an employee is saved with their residency status and PR start date, date of birth, department, job title, start and end dates, salary and allowances, SHG fund, bank account for salary and an hourly cost override
- **THEN** each of those values is stored on the employee record

#### Scenario: NRIC/FIN only for statutory filings
- **WHEN** an employee's identity is recorded
- **THEN** the NRIC or FIN is collected only for statutory filings

#### Scenario: NRIC/FIN encrypted and masked
- **GIVEN** an employee's NRIC or FIN is recorded
- **WHEN** it is stored and shown
- **THEN** it is stored encrypted
- **AND** it is partially masked by default (CMP-PD-07)

#### Scenario: Date of birth sets the CPF age band
- **WHEN** an employee's date of birth is recorded
- **THEN** it determines the employee's CPF age band

#### Scenario: Records held in Nemura or an external system
- **GIVEN** people records live in Nemura or an external HR system (FR-PPL-01)
- **WHEN** Fyniq stores an employee
- **THEN** it keeps only the employee projection of FR-PPL-06, and those fields are held in the other system
- **AND** the hourly cost-rate override stays in Fyniq

### Requirement: FR-EMP-02 Departments mapped to accounts and dimensions
The system SHALL map each department to salary accounts and dimensions.

Source: PRD-001 §8.3 · Priority M · Phase P1

#### Scenario: Department mapping is recorded
- **WHEN** a department is set up
- **THEN** it is mapped to its salary accounts and dimension values

#### Scenario: Pay runs post by the department mapping
- **GIVEN** an employee in a department
- **WHEN** a pay run posts that employee's salary
- **THEN** the salary expense goes to the department's salary account and dimensions (FR-PAY-07)
