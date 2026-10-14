# people/recruitment Specification

## Purpose
Hiring in Nemura: requisitions, job posts with the Fair Consideration Framework timer, fair wording, the pipeline, candidate consent and retention, and offer to hire. Derived from PRD-002 §12.

## Requirements

### Requirement: NM-REC-01 Requisitions
A requisition SHALL hold the role, team, reason, salary range and start date, approved by the Owner or budget holder, and when linked SHALL show whether the role is in Fyniq's plan without other financial figures.

Source: PRD-002 §12 · Priority M · Phase P2

#### Scenario: In the plan
- **GIVEN** Nemura is linked to Fyniq
- **WHEN** a requisition is opened
- **THEN** it shows whether the role is in the plan, and no other financial figure

### Requirement: NM-REC-02 Job posts
Job posts SHALL be published on a careers page, MyCareersFuture postings SHALL be recorded with their date as evidence, and for a covered Employment Pass or S Pass hire the offer letter SHALL be blocked until 14 consecutive days of advertising have passed unless an exemption is recorded.

Source: PRD-002 §12 · Priority M · Phase P2

#### Scenario: Fair Consideration Framework
- **GIVEN** an Employment Pass or S Pass hire covered by the framework, posted on MyCareersFuture on Mon 5 Oct
- **WHEN** a recruiter tries to issue an offer on Fri 16 Oct
- **THEN** the offer letter is blocked until the 14 days have passed, unless an exemption is recorded

#### Scenario: Material change
- **WHEN** the advert's salary changes
- **THEN** a new advert is needed, and its 14 days start again

#### Scenario: Old advert
- **WHEN** an advert closed more than 3 months ago is offered as evidence
- **THEN** it is refused

#### Scenario: Exemption
- **GIVEN** a company with fewer than 10 employees
- **WHEN** it hires an Employment Pass holder
- **THEN** the exemption is recorded with its evidence, and the offer is not blocked

#### Scenario: Other exemptions
- **WHEN** the hire is an Employment Pass at a fixed monthly salary of S$22,500 or more, a role of 1 month or less, or a local or intra-corporate transferee
- **THEN** the exemption is recorded with its evidence, and the offer is not blocked

#### Scenario: S Pass exemption
- **GIVEN** a company with fewer than 10 employees
- **WHEN** it hires an S Pass holder
- **THEN** the same exemption applies and is recorded with its evidence

### Requirement: NM-REC-03 Fair wording
Job posts and interview questions SHALL be checked for criteria that the fair-employment guidelines and the Workplace Fairness Act prohibit, unless a genuine occupational requirement is recorded.

Source: PRD-002 §12 · Priority M · Phase P2

#### Scenario: Flagged criterion
- **WHEN** a job post asks for candidates "below 35"
- **THEN** the wording is flagged with the rule, and the post cannot be published until it is changed or a genuine occupational requirement is recorded

### Requirement: NM-REC-04 Pipeline
Candidates SHALL move through stages on a board, with profiles, interview kits, scorecards and a recorded decision and reason, every move audited.

Source: PRD-002 §12 · Priority M · Phase P2

#### Scenario: Decision
- **WHEN** a candidate is moved to "not progressing"
- **THEN** a reason is recorded and the move is audited

### Requirement: NM-REC-05 Candidate consent and retention
Candidates SHALL consent to the use of their data per role and, separately, for future roles, and unsuccessful candidates' data SHALL be deleted after the retention period unless they consented to keep it.

Source: PRD-002 §12 · Priority M · Phase P2

#### Scenario: Retention
- **WHEN** an unsuccessful candidate's data passes 6 months without consent to keep it
- **THEN** it is queued for the reviewed deletion, and Checks shows ⚠ if it waits (N.1 #11)

#### Scenario: Basis
- **WHEN** a candidate applies
- **THEN** their data is used to assess the application on deemed consent (PDPC Selected Topics guidelines §6.2, revised 23 May 2024)
- **AND** keeping them for future roles needs separate consent, which they can withdraw

#### Scenario: Opinions
- **WHEN** a candidate asks for their data
- **THEN** opinions formed only to evaluate them are not released (NM-SEC-07)

### Requirement: NM-REC-06 Offer to hire
An accepted offer SHALL turn the candidate into a person record with a start date and an onboarding plan, and SHALL track the work pass application date.

Source: PRD-002 §12 · Priority M · Phase P2

#### Scenario: Accepted
- **WHEN** a candidate accepts an offer
- **THEN** a person record and an onboarding plan are created with the start date
