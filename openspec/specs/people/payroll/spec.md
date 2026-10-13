# people/payroll Specification

## Purpose
Payroll in Nemura: PRD-001 §8.7's Singapore rules implemented, the inputs and calendar of a run, payslips to self-service, the hand-off to the books and final pay. Derived from PRD-002 §9.

## Requirements

### Requirement: NM-PAY-01 The rules are PRD-001's
Nemura's payroll SHALL implement PRD-001 FR-PAY-01…16 (except FR-PAY-12) and Appendix F's formulas, tested by the shared oracle.

Source: PRD-002 §9 · Priority M · Phase P1

#### Scenario: Same results
- **WHEN** a pay run is calculated in Nemura
- **THEN** CPF, SDL, SHG and net pay follow PRD-001's rules and equal the oracle's to the cent

### Requirement: NM-PAY-02 Inputs collected for the run
A pay run SHALL collect joiners, leavers, pay changes, unpaid leave and encashment, Part IV overtime, one-off allowances and bonuses, and payroll-paid claims, each linked to its source record.

Source: PRD-002 §9 · Priority M · Phase P1

#### Scenario: Linked inputs
- **WHEN** the October run is prepared
- **THEN** each input (for example a joiner's prorated pay or approved overtime) links to the record it came from

### Requirement: NM-PAY-03 Payroll calendar
Each pay group SHALL have a pay day, an input cut-off and a latest approval date that respect the pack's payment deadlines, and Checks SHALL warn when a run is late.

Source: PRD-002 §9 · Priority M · Phase P1

#### Scenario: Late run
- **WHEN** a run is not approved by its latest approval date
- **THEN** Checks shows ⏰ (N.1 #2)

### Requirement: NM-PAY-04 Payslips to self-service
Payslips SHALL be published to each person's My pay with the salary or within 3 working days of it, with print and download, and leavers SHALL keep access through an expiring link.

Source: PRD-002 §9 · Priority M · Phase P1

#### Scenario: Published on pay day
- **WHEN** salaries are paid on Wed 28 Oct
- **THEN** each person's payslip is in My pay by then, or within 3 working days

#### Scenario: Leaver
- **WHEN** a person who has left asks for a payslip
- **THEN** they get an expiring link, for the record-keeping period

### Requirement: NM-PAY-05 Hand-off to the books
An approved pay run SHALL post to a linked Fyniq as PRD-001 FR-PPL-04 describes, or produce journal and payment files when standalone, and SHALL record what the books received.

Source: PRD-002 §9 · Priority M · Phase P1

#### Scenario: Linked
- **WHEN** a run is approved and Nemura is linked
- **THEN** the summary journal by department and the salary payment lines go to Fyniq, and the run shows the posting reference

#### Scenario: Mismatch
- **WHEN** the books received totals different from the run's
- **THEN** Checks shows ✖ (N.1 #6)

### Requirement: NM-PAY-06 Final pay
A leaver's last salary, leave settlement, notice pay and IR21 clearance hold SHALL be prepared in one place, linked to offboarding.

Source: PRD-002 §9 · Priority M · Phase P1

#### Scenario: Leaver with tax clearance
- **GIVEN** an Employment Pass holder leaving on 30 Nov
- **WHEN** final pay is prepared
- **THEN** it includes the leave settlement and holds all monies due until IRAS releases them (PRD-001 FR-PAY-10)
