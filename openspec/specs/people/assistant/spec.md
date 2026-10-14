# people/assistant Specification

## Purpose
The people assistant: Fyniq's AI rules applied to people work, its skills, its knowledge, its never-list, fairness, and its task catalogue. Derived from PRD-002 §18 and Appendix D.

## Requirements

### Requirement: NM-AI-01 Same rules as Fyniq's AI
The people assistant SHALL read through tools and propose only, and SHALL use model routes whose data-class policy keeps personal and payroll data in Singapore processing by default.

Source: PRD-002 §18 · Priority M · Phase P1

#### Scenario: Data stays in Singapore
- **WHEN** the assistant answers a question that needs a person's pay
- **THEN** the request goes only to a route allowed for payroll data, by default in-Singapore processing (AI-75)

#### Scenario: Storage-only region
- **GIVEN** a provider region that stores data in Singapore but processes it elsewhere
- **WHEN** a payroll question is routed
- **THEN** that region is not used (PRD-001 AI-88)

### Requirement: NM-AI-02 Skills baked in
The assistant SHALL answer leave, pay-day, payslip and policy questions from the person's records and the cited pack and handbook, and SHALL draft letters, job posts, interview questions, review summaries and flexible-work replies as proposals.

Source: PRD-002 §18 · Priority M · Phase P1 / P2 (hiring, reviews)

#### Scenario: Balance question
- **WHEN** an employee asks how much annual leave they have left
- **THEN** the answer comes from their leave record, with the figure cited

#### Scenario: Draft, not send
- **WHEN** an HR officer asks for a letter
- **THEN** the assistant drafts it as a proposal for the HR officer to edit and approve

#### Scenario: Payslip lines
- **WHEN** an employee asks about a payslip line
- **THEN** NM-AI-06 applies

### Requirement: NM-AI-03 Knowledge
The pack, the handbook and policies, and Nemura's guide SHALL be compiled into the knowledge base and cited; statutory rules SHALL come from the pack, and a [verify] rule SHALL be answered with "to confirm with HR".

Source: PRD-002 §18 · Priority M · Phase P1

#### Scenario: To confirm
- **WHEN** an employee asks about a rule marked [verify] in the pack
- **THEN** the answer says it is to be confirmed with HR and cites the pack entry

### Requirement: NM-AI-04 Never-list for people work
The assistant SHALL never approve or refuse leave or requests, change pay, bank accounts, roles or records, issue or sign letters, move candidates, decide hiring, ratings, promotions or terminations, or read restricted data beyond the user's rights.

Source: PRD-002 §18 · Priority M · Phase P1

#### Scenario: Approve refused
- **WHEN** a manager asks the assistant to approve a leave request
- **THEN** it refuses and points to the approval in the inbox

#### Scenario: Red team
- **WHEN** the red-team suite runs
- **THEN** no never-list action succeeds (PRD-001 QA-AI-03)

### Requirement: NM-AI-05 Fairness
The assistant SHALL never use a protected characteristic to screen, rank, score or evaluate anyone, SHALL flag such criteria in drafts, and SHALL take statutory eligibility from the pack's deterministic tools, which return the result and the rule.

Source: PRD-002 §18 · Priority M · Phase P2

#### Scenario: Ranking refused
- **WHEN** a recruiter asks the assistant to rank candidates
- **THEN** it refuses and offers an interview kit instead

#### Scenario: Eligibility explained
- **WHEN** an employee asks whether they can take childcare leave
- **THEN** the answer comes from the pack's tool, with the result and the rule, and no detail about other people or facts not needed

### Requirement: NM-AI-06 Own pay
By default the assistant SHALL explain a payslip line from its rule while the app shows the person's own figures, sending no pay figure to a model; an Owner with a second approver MAY enable own-pay explanations that send only the asking person's figures on a payroll-allowed route.

Source: PRD-002 §18 · Priority M · Phase P1

#### Scenario: Default
- **WHEN** Daniel Lim asks why his employee CPF is S$1,520.00
- **THEN** the assistant explains the rule and the app shows his figure, and no figure is sent to a model

#### Scenario: Enabled
- **GIVEN** own-pay explanations are enabled
- **WHEN** he asks
- **THEN** only his own payslip figures are sent, on a route allowed for payroll data

#### Scenario: Someone else's pay
- **WHEN** he asks about a colleague's pay
- **THEN** the assistant refuses

### Requirement: people.draft_letter Draft a letter
The catalogue SHALL include `people.draft_letter`: a letter from a template for a request, with fields filled from the record, approved by an HR officer.

Source: PRD-002 §D · Phase P1

#### Scenario: Approval
- **WHEN** the assistant proposes a letter for a request
- **THEN** an HR officer reviews and approves it before the normal issue steps (NM-LTR-03)

### Requirement: people.draft_fwa_reply Draft a flexible-work reply
The catalogue SHALL include `people.draft_fwa_reply`: a written reply to a flexible-work request with the reason for a refusal, approved by the line manager or an HR officer.

Source: PRD-002 §D · Phase P1

#### Scenario: Reason included
- **WHEN** the proposed reply refuses the request
- **THEN** it states the business reason, and the line manager or HR officer approves it before it is sent

### Requirement: people.draft_job_post Draft a job post
The catalogue SHALL include `people.draft_job_post` in P2: a job post from a requisition with fair-wording flags, approved by a recruiter.

Source: PRD-002 §D · Phase P2

#### Scenario: Flags
- **WHEN** the proposed post contains a prohibited criterion
- **THEN** the proposal shows the flag, and the recruiter must change it before approving

### Requirement: people.draft_interview_kit Draft an interview kit
The catalogue SHALL include `people.draft_interview_kit` in P2: interview questions and a scorecard for a role, approved by a recruiter.

Source: PRD-002 §D · Phase P2

#### Scenario: Kit
- **WHEN** a recruiter asks for an interview kit
- **THEN** the proposal holds questions and a scorecard tied to the role's requirements, for the recruiter to approve

### Requirement: people.draft_review_summary Draft a review summary
The catalogue SHALL include `people.draft_review_summary` in P2: a summary of a person's goals, check-ins and feedback for their manager, approved by the line manager.

Source: PRD-002 §D · Phase P2

#### Scenario: No rating
- **WHEN** the summary is proposed
- **THEN** it summarises evidence and proposes no rating
