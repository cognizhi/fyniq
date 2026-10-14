# people/self-service Specification

## Purpose
Employee self-service in Nemura: My home, My pay, My details, requests and the claims link. Derived from PRD-002 §10.

## Requirements

### Requirement: NM-ESS-01 My home
My home SHALL show action tiles for what people do most, the leave balance, the next pay day, open requests and the next public holiday, mobile-first with one primary action per screen.

Source: PRD-002 §10 · Priority M · Phase P1

#### Scenario: What I need first
- **WHEN** an employee signs in
- **THEN** My home shows tiles to apply for leave, see their payslip, request a letter, make a claim, update details and ask a question, with their balance and next pay day

### Requirement: NM-ESS-02 My pay
My pay SHALL list the person's payslips by month with view, print and PDF, their IR8A or AIS form when ready, and their own pay history, never company figures.

Source: PRD-002 §10 · Priority M · Phase P1

#### Scenario: Payslips
- **WHEN** an employee opens My pay
- **THEN** they see their payslips by month and can view, print or download each one

### Requirement: NM-ESS-03 My details
Employees SHALL update contact details, emergency contacts and address directly; legal name, NRIC or FIN, bank account and residency changes SHALL need evidence and HR approval.

Source: PRD-002 §10 · Priority M · Phase P1

#### Scenario: Direct change
- **WHEN** an employee changes their phone number
- **THEN** it is saved at once and audited

#### Scenario: Approved change
- **WHEN** an employee changes their legal name
- **THEN** the change waits for evidence and HR approval

### Requirement: NM-ESS-04 Requests
Letters, flexible-work requests, details changes and HR questions SHALL be requests with a type, status, track, due date and reply that the employee can follow.

Source: PRD-002 §10 · Priority M · Phase P1

#### Scenario: Where is my letter
- **WHEN** an employee opens My requests
- **THEN** each request shows its step (received, with HR, ready) and its due date

### Requirement: NM-ESS-05 Claims link
When linked to Fyniq, the Claims tile SHALL open Fyniq's personal workspace with the same sign-in, never the finance workspace; a person not yet invited SHALL be told how to get access; otherwise it SHALL link to the company's expense tool or be hidden.

Source: PRD-002 §10 · Priority M · Phase P1

#### Scenario: Linked
- **GIVEN** Nemura is linked to Fyniq
- **WHEN** an employee taps Claims
- **THEN** Fyniq's My claims opens in the personal workspace without another sign-in (PRD-001 FR-UX-39)

#### Scenario: Not invited yet
- **GIVEN** an employee without a Fyniq account
- **WHEN** they tap Claims
- **THEN** they see "You don't have Fyniq access yet" with a button that asks HR, never a refused page

### Requirement: NM-ESS-06 After leaving
A leaver SHALL keep a narrow, authenticated account for their own documents for the record-keeping period, with re-issue on request, and email links SHALL only deliver documents.

Source: PRD-002 §10 · Priority M · Phase P1

#### Scenario: Leaver account
- **GIVEN** Chen Yu's last day was Mon 30 Nov 2026
- **WHEN** he signs in to his leaver account in March 2027
- **THEN** he sees only his own payslips, IR8A and letters, with no other access

#### Scenario: Retention ends
- **WHEN** the record-keeping period ends
- **THEN** the account ends, and the documents follow their retention (NM-SEC-05)
