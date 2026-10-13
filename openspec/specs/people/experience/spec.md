# people/experience Specification

## Purpose
Roles, landing pages and the experience in Nemura: its own design system, ease of use, the workspace switch, help and print-ready documents, with the permission matrix. Derived from PRD-002 §3, §19 and Appendix B.

## Requirements

### Requirement: Permission matrix
Each Nemura role SHALL hold only the rights PRD-002 Appendix B gives it.

Source: PRD-002 §B

#### Scenario: Employee
- **WHEN** an employee uses Nemura
- **THEN** they act only on their own profile, balances, payslips and requests, and see the directory

#### Scenario: AI
- **WHEN** the assistant acts for a user
- **THEN** it never exceeds that user's rights and only drafts letters, posts and summaries

### Requirement: NM-ROLE-01 Roles are Nemura's own
Nemura's roles SHALL be assigned per organisation with Appendix B's rights, SHALL never grant rights in Fyniq or the reverse, and a person with rights in both SHALL see the workspace switch.

Source: PRD-002 §3 · Priority M · Phase P1

#### Scenario: No crossover
- **WHEN** a person is made HR administrator in Nemura
- **THEN** they gain no right in Fyniq

### Requirement: NM-ROLE-02 Landing by role
Each role SHALL land on its page, HR roles, employees and managers SHALL never see company financial figures in Nemura, and pay SHALL show only to its owner and payroll roles.

Source: PRD-002 §3 · Priority M · Phase P1

#### Scenario: HR landing
- **WHEN** an HR officer signs in
- **THEN** they land on People home, which shows no ledger, bank, revenue or cash figure

### Requirement: NM-ROLE-03 Line managers
Line managers SHALL come from the effective-dated reporting line, approve their reports' leave and requests, and see their reports' leave, schedule and goals but never pay.

Source: PRD-002 §3 · Priority M · Phase P1

#### Scenario: No pay for managers
- **WHEN** a line manager opens a report's profile
- **THEN** they see leave, schedule and goals, and no pay

### Requirement: NM-ROLE-04 Segregation
The preparer of a pay run SHALL NOT approve it, the person who changes a salary bank account SHALL NOT approve the change, and the Owner SHALL be the second person when HR is one person.

Source: PRD-002 §3 · Priority M · Phase P1

#### Scenario: Preparer cannot approve
- **WHEN** the person who prepared a pay run tries to approve it
- **THEN** the approval is refused (SOD-4)

### Requirement: NM-UX-01 Nemura's own design system
Nemura SHALL use its own design system with Fyniq's semantic token names, tokens only, contrast validated in four themes, and AI content always violet with the AI badge.

Source: PRD-002 §19 · Priority M · Phase P1

#### Scenario: Contrast
- **WHEN** Nemura's tokens change
- **THEN** `make check-tokens` validates them in light, dark and both high-contrast themes

### Requirement: NM-UX-02 Easy for everyone
Nemura SHALL use plain words, one primary action per screen, 44 px touch targets, mobile-first self-service, PRD-001's five device frames, WCAG 2.2 AA and switchable motion, with one simple mode for employees.

Source: PRD-002 §19 · Priority M · Phase P1

#### Scenario: Touch targets
- **WHEN** self-service is used on a phone
- **THEN** every target is at least 44 px

### Requirement: NM-UX-03 Workspace switch
People with rights in Fyniq and Nemura SHALL see a Finance | People switch that changes app, keeps each app's last page and never changes rights.

Source: PRD-002 §19 · Priority M · Phase P1

#### Scenario: Switch
- **WHEN** the Owner switches from People to Finance
- **THEN** Fyniq opens on the page she last used there

### Requirement: NM-UX-04 Help on every screen
Every Nemura screen SHALL have help keys, tooltips, guide pages in `docs/user-guide/nemura/` and a first-run tour for employees and for the people team.

Source: PRD-002 §19 · Priority M · Phase P1

#### Scenario: Tour
- **WHEN** an employee signs in for the first time
- **THEN** a short tour shows My home's tiles

### Requirement: NM-UX-05 Print-ready documents
Payslips and letters SHALL have a print and PDF view on the company letterhead, readable in black and white.

Source: PRD-002 §19 · Priority M · Phase P1

#### Scenario: Print
- **WHEN** an employee prints a payslip
- **THEN** it prints on the letterhead and reads clearly in black and white
