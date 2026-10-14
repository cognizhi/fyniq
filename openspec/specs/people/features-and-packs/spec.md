# people/features-and-packs Specification

## Purpose
The optional features of Nemura and the country and region packs that hold the law, calendars and rules, with company policy on top. Derived from PRD-002 §5, §6 and Appendix A.

## Requirements

### Requirement: Singapore pack facts
The Singapore pack SHALL hold the facts of PRD-002 Appendix A, each with its source, effective date and [verify] status, citing the official page for facts checked on it.

Source: PRD-002 §A

#### Scenario: Values to confirm
- **WHEN** a value from Appendix A is still marked [verify]
- **THEN** the pack screen shows it as "to confirm" with its source

#### Scenario: Corrected observance
- **GIVEN** an employee whose rest day is Monday
- **WHEN** a public holiday falls on a Monday
- **THEN** their next working day is the paid holiday (Appendix A, corrected in rev 0.2)

#### Scenario: Official source cited
- **WHEN** a fact was checked on the official page (for example MOM's 2027 holiday calendar)
- **THEN** the pack cites that page and the date it was checked

#### Scenario: Maternity split
- **GIVEN** a mother of a Singapore Citizen 2nd child on government-paid maternity leave
- **WHEN** the claim is worked out
- **THEN** the employer pays the first 8 weeks and claims the last 8, up to S$10,000 per 4 weeks and S$20,000 in all (MOM)

### Requirement: NM-FEAT-01 Feature registry
People records SHALL always be on, and each optional feature SHALL be turned on or off per organisation (payroll with the Owner and a second approver); a feature that has never been on SHALL NOT be loaded.

Source: PRD-002 §5 · Priority M · Phase P1

#### Scenario: Off means not loaded
- **WHEN** a feature has never been on
- **THEN** it has no routes, menu items, jobs, events or assistant skills
- **AND** turning off a feature that was used follows NM-FEAT-05

#### Scenario: Payroll needs two people
- **WHEN** someone turns payroll on or off
- **THEN** the change needs the Owner and a second approver

### Requirement: NM-FEAT-02 Dependencies
Feature dependencies SHALL be declared and enforced, refusing a change that breaks one with `NM_FEATURE_REQUIRED`.

Source: PRD-002 §5 · Priority M · Phase P1

#### Scenario: Payslips need payroll
- **GIVEN** self-service payslips are on, and no external payroll source supplies pay documents
- **WHEN** the HR administrator turns payroll off
- **THEN** the change is refused with `NM_FEATURE_REQUIRED` and the reason

#### Scenario: Pay documents from an external payroll
- **GIVEN** an external payroll source supplies payslips (NM-EXT-05)
- **WHEN** Nemura's payroll is off
- **THEN** payslips in self-service stay on

### Requirement: NM-FEAT-03 External system per feature
A company SHALL be able to use an external system for payroll, leave or time instead of Nemura's feature, which then turns off and records the system that holds that data.

Source: PRD-002 §5 · Priority M · Phase P1

#### Scenario: External payroll
- **WHEN** the HR administrator chooses an external payroll system
- **THEN** Nemura's payroll is off, the external system is recorded as the holder of payroll data, and Fyniq takes the books' side from it

### Requirement: NM-FEAT-04 Turning off keeps data
Turning a feature off SHALL hide it and stop its new work while its records stay under their retention rules, readable by people with the right (NM-FEAT-05).

Source: PRD-002 §5 · Priority M · Phase P1

#### Scenario: Back on
- **GIVEN** leave was turned off for a month
- **WHEN** it is turned on again
- **THEN** the leave records are there as they were

### Requirement: NM-FEAT-05 Off means no new work, not no obligations
A feature SHALL be on, off or external; off SHALL stop new transactions while records stay readable by the right roles and retention, holds, statutory deadlines and approved work continue, and turning off SHALL first list what is outstanding and record each replacement, or be refused.

Source: PRD-002 §5 · Priority M · Phase P1

#### Scenario: Payroll turned off
- **GIVEN** leavers whose payslips are kept
- **WHEN** payroll is turned off
- **THEN** no new run can start
- **AND** the leavers still reach their payslips, and retention and deletion jobs keep running

#### Scenario: Letters off with KETs due
- **GIVEN** Joanne Lee starts on Mon 2 Nov with KETs due
- **WHEN** the HR administrator tries to turn Letters off
- **THEN** it is refused with `NM_FEATURE_OBLIGATIONS_OPEN`, unless a replacement route for KETs is recorded

### Requirement: NM-PACK-01 Packs per country and region
A pack SHALL hold a country's leave types and entitlements, holiday calendar and observance rules, working-time rules, payroll rules, document requirements, record-keeping periods and retirement ages, and regions SHALL add or replace calendars.

Source: PRD-002 §6 · Priority M · Phase P1

#### Scenario: Region calendar
- **GIVEN** a worksite in a region that has its own holidays
- **WHEN** the calendar is built
- **THEN** the region's holidays are added to or replace the country's

### Requirement: NM-PACK-02 Effective-dated, sourced and tested
Every pack value SHALL have an effective date, a source URL and a checked date, SHALL be pinned by tests, and SHALL change only through a new pack version that the HR administrator accepts; accepting a version SHALL NOT verify its values (NM-PACK-07).

Source: PRD-002 §6 · Priority M · Phase P1

#### Scenario: New version
- **WHEN** a pack update arrives
- **THEN** the HR administrator sees what changes and accepts it before it applies

#### Scenario: To confirm
- **WHEN** a value is marked [verify]
- **THEN** it shows as "to confirm"
- **AND** it stays researched until the publisher verifies it (NM-PACK-07)

### Requirement: NM-PACK-03 Worksites
Each person SHALL work at a worksite whose country and region pick the pack and calendar, P1 supporting Singapore only; whether a rule applies SHALL come from the person's legal classification (NM-PPL-08), each rule on its own effective-date basis.

Source: PRD-002 §6 · Priority M · Phase P1 / P3 (several countries)

#### Scenario: Pack from worksite
- **WHEN** a person's leave or pay is calculated
- **THEN** the pack and calendar of their worksite apply

#### Scenario: Classification decides
- **GIVEN** a contractor on a contract for service at the Singapore worksite
- **WHEN** entitlements are worked out
- **THEN** no statutory employee entitlement is granted

#### Scenario: Each rule's own clock
- **WHEN** retirement age (by birth cohort) and CPF rates (from 1 January) change
- **THEN** each change applies on its own basis

### Requirement: NM-PACK-04 Company policy on top
Company policy SHALL be effective-dated and SHALL NOT give less than the pack's statutory minimum, which is refused with `NM_POLICY_BELOW_STATUTE`.

Source: PRD-002 §6 · Priority M · Phase P1

#### Scenario: Below the law
- **WHEN** the HR administrator saves annual leave of 5 days for people with two years' service
- **THEN** it is refused with `NM_POLICY_BELOW_STATUTE` and the rule it breaks

#### Scenario: Better than the law
- **WHEN** a policy gives more than the statutory minimum
- **THEN** it is saved and marked as above the minimum

#### Scenario: Rule by rule
- **WHEN** a policy is saved
- **THEN** it is checked against each statutory rule (NM-PACK-08)

### Requirement: NM-PACK-05 Next year's calendar
The Singapore pack SHALL get next year's gazetted public holidays when MOM publishes them, and Checks SHALL warn from 1 Dec if they are missing.

Source: PRD-002 §6 · Priority M · Phase P1

#### Scenario: Missing calendar
- **GIVEN** next year's holidays are not loaded
- **WHEN** it is 1 Dec
- **THEN** Checks shows ⚠ (N.1 #8)

#### Scenario: Beyond the loaded calendar
- **GIVEN** 2028's holidays are not yet published
- **WHEN** someone applies for leave in January 2028
- **THEN** the form says holidays are not yet published
- **AND** the request is recounted when they are

### Requirement: NM-PACK-06 The Singapore pack and the next packs
The Singapore pack SHALL hold Appendix A's facts at go-live, and a Malaysia pack SHALL be the first P3 example.

Source: PRD-002 §6 · Priority M · Phase P1 / P3 (Malaysia)

#### Scenario: Singapore at go-live
- **WHEN** Nemura goes live
- **THEN** the Singapore pack holds every Appendix A topic with its source and status
- **AND** facts checked on 14 Oct 2026 cite the official page

### Requirement: NM-PACK-07 Verified before it decides anything
Each pack value SHALL be researched, verified (official page captured and checked by a second person at the publisher) or released; an organisation's acceptance SHALL verify nothing, and a calculation needing an unverified value SHALL stop unless HR records the value applied with evidence.

Source: PRD-002 §6 · Priority M · Phase P1

#### Scenario: Unverified statutory input
- **GIVEN** a leave entitlement in the pack is still researched
- **WHEN** an employee applies for that leave
- **THEN** the request shows that the entitlement is not verified and waits for HR
- **AND** Checks shows ✖ (N.1 #13) and gives no "all clear"

#### Scenario: Company decision
- **WHEN** HR records the value the company applies, with a reason and evidence
- **THEN** calculations use it, labelled "company decision", and each use is audited

#### Scenario: Two people at the publisher
- **WHEN** the publisher verifies a value
- **THEN** a second person checks it before it is released (PRD-001 SOD-10)

#### Scenario: Unrelated work continues
- **WHEN** one value is unverified
- **THEN** calculations that do not use it go ahead

### Requirement: NM-PACK-08 Policies checked rule by rule
A company policy SHALL be checked against each statutory rule for the people it covers (coverage, start of the right, amount, unit, use, carry-forward, half days, proration, settlement on leaving), and a change SHALL never remove rights already earned.

Source: PRD-002 §6 · Priority M · Phase P1

#### Scenario: Carry-forward for Part IV
- **GIVEN** Part IV employees with unused annual leave
- **WHEN** the HR administrator saves a policy that forfeits unused leave at the year's end
- **THEN** it is refused with `NM_POLICY_BELOW_STATUTE`, naming the 12-month carry-forward rule

#### Scenario: Generous but late
- **WHEN** a policy gives 14 days but only after 12 months' service
- **THEN** it is refused, because the statutory right starts at 3 months

#### Scenario: Proration
- **GIVEN** a joiner with 4 completed months in the first year
- **WHEN** first-year leave is prorated
- **THEN** it is 4/12 of 7 days, 2.33, rounded down to 2 days

#### Scenario: Earned rights kept
- **WHEN** a policy changes from 1 Jan 2027
- **THEN** leave earned before then is kept
