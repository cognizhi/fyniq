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

#### Scenario: Approved once by the Owner (rev 0.66)
- **WHEN** the Recruiter asks for a requisition and the Owner decides it
- **THEN** it is decided once, what was asked for never changes, and only an approved requisition's post is published

#### Scenario: Who may ask (rev 0.69)
- **WHEN** the HR administrator or the Owner asks for a requisition, and an employee, a payroll administrator or the Administrator tries to
- **THEN** the first two are accepted and the others are refused; only the Owner decides, and in founder mode the Owner may decide a requisition they asked for

#### Scenario: The hiring manager reads their own roles (rev 0.74)
- **GIVEN** a requisition that names a person with a sign-in as hiring manager, and another that names no one
- **WHEN** that person reads Roles I am hiring for
- **THEN** they see only the first, with where it stands, its post's dates and the number of candidates at each stage, and no salary and no candidate's name

### Requirement: NM-REC-02 Job posts
Job posts SHALL be published on a careers page, MyCareersFuture postings SHALL be recorded with their date as evidence, and for a covered Employment Pass or S Pass hire the offer letter SHALL be blocked until 14 consecutive days of advertising have passed unless an exemption is recorded.

Source: PRD-002 §12 · Priority M · Phase P2

#### Scenario: Fair Consideration Framework
- **GIVEN** an Employment Pass or S Pass hire covered by the framework, posted on MyCareersFuture on Mon 5 Oct
- **WHEN** a recruiter tries to issue an offer on Fri 16 Oct
- **THEN** the offer letter is blocked until the 14 days have passed, unless an exemption is recorded

#### Scenario: The earliest offer day (rev 0.66)
- **GIVEN** a role that needs a pass, advertised from Mon 5 Oct
- **WHEN** the post is read
- **THEN** it shows Mon 19 Oct as the first day an offer may be made, and no offer day while neither the advert nor an exemption is recorded

#### Scenario: Material change
- **WHEN** the advert's salary changes
- **THEN** a new advert is needed, and its 14 days start again

#### Scenario: A new advert replaces the old as evidence (rev 0.70)
- **WHEN** the occupation, salary, number of vacancies or UEN changes and a later advert is recorded
- **THEN** the old advert is kept with what changed, the new day starts the 14 days again, and an advert closed more than 3 months ago is refused

#### Scenario: The careers page (rev 0.70)
- **WHEN** anyone reads a company's careers page that is switched on
- **THEN** they see each published, approved post's title, team, text and day, no salary and no person, while a page that is off or unknown shows nothing

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

#### Scenario: A criterion kept on purpose (rev 0.66)
- **WHEN** a post names a flagged criterion and a genuine occupational requirement of 10 or more characters is recorded
- **THEN** it is published with the requirement kept in its record

#### Scenario: Interview questions (rev 0.71)
- **WHEN** an interview kit has a question that names a flagged criterion
- **THEN** the kit is saved only with the genuine occupational requirement recorded, and the findings are shown either way

### Requirement: NM-REC-04 Pipeline
Candidates SHALL move through stages on a board, with profiles, interview kits, scorecards and a recorded decision and reason, every move audited.

Source: PRD-002 §12 · Priority M · Phase P2

#### Scenario: Decision
- **WHEN** a candidate is moved to "not progressing"
- **THEN** a reason is recorded and the move is audited

#### Scenario: The board's moves (rev 0.71)
- **WHEN** a candidate at applied is moved to offer, or any candidate is moved to hired by a move
- **THEN** it is refused: the board moves step by step and a candidate is hired by the offer

#### Scenario: An interviewer's view (rev 0.71)
- **WHEN** an interviewer reads a candidate assigned to them, and one who is not
- **THEN** the first is shown without the hiring team's notes and can score a criterion of the kit from 1 to 5, and the second is told there is no such candidate

#### Scenario: An interviewer's own page (rev 0.74)
- **WHEN** an interviewer opens My interviews
- **THEN** they see only the candidates assigned to them, with the kit's criteria and the criteria they scored, and the hiring team assigns an interviewer only from the members who hold the Interviewer role today

#### Scenario: A candidate looks at their own data (rev 0.74)
- **GIVEN** a link the hiring team made for one candidate, working for 1 to 30 days
- **WHEN** the candidate opens it with no sign-in
- **THEN** they see what they gave, the role, where they stand and the day their data is due for deletion, never the team's notes or scores, and an expired, revoked or unknown link is answered alike with nothing

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

#### Scenario: Reviewed deletion (rev 0.71)
- **WHEN** a candidate not progressing without the consent to be kept passes six months, and a person has reviewed the report
- **THEN** the candidate, their notes and scores are deleted with a proof, while one who consented to be kept is not due, and a withdrawn consent restarts the six months

#### Scenario: A hired candidate's record (rev 0.74)
- **WHEN** six months have passed since a hired candidate's start date, and a person has reviewed the report
- **THEN** the candidate's CV, notes, scores, interviewers and links are deleted with a proof, and the person record and the fact of the hire stay

#### Scenario: The candidate withdraws their consent (rev 0.74)
- **WHEN** a candidate with a live link withdraws the consent to be kept for future roles
- **THEN** it is withdrawn once, only after it was given, and for a candidate not progressing the six months run from the withdrawal

#### Scenario: Opinions
- **WHEN** a candidate asks for their data
- **THEN** opinions formed only to evaluate them are not released (NM-SEC-07)

#### Scenario: A hired candidate is not kept for future roles (rev 0.77)
- **WHEN** the consent to be kept for future roles is given for a hired candidate
- **THEN** it is refused with the reason, because they have joined and their record is final

### Requirement: NM-REC-06 Offer to hire
An accepted offer SHALL turn the candidate into a person record with a start date and an onboarding plan, and SHALL track the work pass application date.

Source: PRD-002 §12 · Priority M · Phase P2

#### Scenario: Accepted
- **WHEN** a candidate accepts an offer
- **THEN** a person record and an onboarding plan are created with the start date

#### Scenario: A pass role waits (rev 0.72)
- **GIVEN** a role that needs a pass, advertised from Mon 25 Feb, with no exemption
- **WHEN** a recruiter moves a candidate to the offer stage before Mon 11 Mar
- **THEN** it is refused until the advert's 14 days have run, and the hire records the day the pass application was made, once

#### Scenario: The offer letter is made for the person at the hire (rev 0.77)
- **GIVEN** a company that issues offer letters from a published template
- **WHEN** an HR administrator hires a candidate and asks for the offer letter
- **THEN** a letter request is made for the new person from their role, team, kind of employment and first day, with no pay, and another member of the people team checks it and the Owner signs it

#### Scenario: A company that issues none refuses the whole hire (rev 0.77)
- **WHEN** a hire asks for an offer letter and the company has not switched the type on or published a template
- **THEN** the hire is refused with the reason and no person record is made
