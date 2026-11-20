# people/letters Specification

## Purpose
Letters and templates in Nemura: letter types, templates on Fyniq's engine, two-step issue with a passkey signature, automatic standard letters, verification and KETs. Derived from PRD-002 §11.

## Requirements

### Requirement: NM-LTR-01 Letter types
Each letter type SHALL set who may request it, which fields it shows, who signs it and how fast it is due, with salary shown only when the person asks.

Source: PRD-002 §11 · Priority M · Phase P1

#### Scenario: Salary only on request
- **WHEN** an employee requests an employment letter for a bank and asks to include salary
- **THEN** the letter shows the salary; otherwise it does not

### Requirement: NM-LTR-02 Templates
Letter templates SHALL be text blocks with typed people merge fields and no free code (Nemura's own in P1; Fyniq's editor with the Fyniq link), and a template change SHALL be versioned, a published version immutable, and approved by the HR administrator.

Source: PRD-002 §11 · Priority M · Phase P1

#### Scenario: Merge fields
- **WHEN** a template uses the person's start date and the company's UEN
- **THEN** the issued letter fills them from the record

#### Scenario: Approved change
- **WHEN** a template is edited
- **THEN** the new version applies only after the HR administrator approves it

### Requirement: NM-LTR-03 Issue in two steps
A letter SHALL be filled from the request, checked by an HR officer, signed by the authorised signatory with a passkey, delivered in My requests and by an expiring email link, and kept as evidence.

Source: PRD-002 §11 · Priority M · Phase P1

#### Scenario: Issue
- **WHEN** an HR officer checks a filled letter and the signatory signs it with a passkey
- **THEN** the employee gets it in My requests and by an expiring link, and it is kept as evidence

#### Scenario: Evidence kept
- **WHEN** a letter is issued
- **THEN** NM-LTR-07's evidence is kept

### Requirement: NM-LTR-04 Standard letters without waiting
Letter types an authorised signatory delegates for automatic issue SHALL be generated and signed as soon as they are requested, within the delegation's type, template version, requesters, limit and end date (never a letter that shows pay), and logged with the exact bytes and the requester; the signatory's own letter waits for the people team.

Source: PRD-002 §11 · Priority S · Phase P1

#### Scenario: Automatic
- **GIVEN** the signatory has delegated automatic issue of employment confirmation without salary, with a monthly limit and an end date at most 12 months away
- **WHEN** an employee requests it
- **THEN** it is ready at once and the issue is logged

#### Scenario: Delegation ended
- **GIVEN** the delegation expired yesterday
- **WHEN** an employee requests the letter
- **THEN** it goes through the normal two-step issue (NM-LTR-03)
- **AND** an AI draft never uses the automatic path

#### Scenario: Template changed
- **WHEN** the letter's template changes
- **THEN** the delegation ends

#### Scenario: Not eligible
- **GIVEN** an employee serving notice
- **WHEN** they request an active-employment confirmation
- **THEN** it is not issued automatically

### Requirement: NM-LTR-05 Verification
Every issued letter SHALL carry a reference and code that let a recipient check whether it is valid, replaced or withdrawn without seeing personal facts, which only the employee's expiring, revocable disclosure link SHALL show.

Source: PRD-002 §11 · Priority S · Phase P2

#### Scenario: Verify
- **WHEN** a bank enters the letter's reference and code
- **THEN** it sees that the letter is valid, with its type, date and company, and no salary or other personal fact

#### Scenario: Guessing references
- **WHEN** someone tries references without their codes
- **THEN** no letter is found, and the attempts are rate-limited and audited

#### Scenario: Revoking a share
- **WHEN** the employee revokes a disclosure link
- **THEN** the link stops working, and the letter stays valid

#### Scenario: Replacing a letter
- **WHEN** the employer replaces a wrong letter
- **THEN** both versions and their states stay traceable

### Requirement: NM-LTR-06 KETs
KETs SHALL be generated from the offer and the record and issued within 14 days of the start for employment of 14 days or more (under the Employment Act), with MOM's 17 items from the country pack, the pay terms written and read only by the Payroll administrator and the Owner, and missing KETs ✖ on Checks (⏰ in the 3 days before).

Source: PRD-002 §11 · Priority M · Phase P1

#### Scenario: Missing KETs
- **WHEN** a person started 14 days ago and has no issued KETs
- **THEN** Checks shows ✖ (N.1 #1)

#### Scenario: Short contract
- **GIVEN** a part-timer on a 3-week contract who works 6 days in it
- **WHEN** KETs are due
- **THEN** they are issued within 14 days, because the contract's length counts, not the days worked

#### Scenario: Letters off
- **GIVEN** Letters is off
- **WHEN** a person joins
- **THEN** KETs are still due, through Onboarding or a recorded manual route (NM-FEAT-05)

### Requirement: NM-LTR-07 What an issued letter proves
Each issued letter's exact PDF SHALL be hashed and kept with its template version, data, signatory, authority and signed envelope, SHALL tell recipients how to check it, and a passkey approval SHALL NOT be presented as a certificate signature.

Source: PRD-002 §11 · Priority M · Phase P1

#### Scenario: One byte changed
- **WHEN** one byte of an issued PDF changes
- **THEN** it no longer matches its evidence

#### Scenario: Revoked signatory
- **GIVEN** a signatory's authority was revoked
- **WHEN** they try to sign a new letter
- **THEN** it is refused

#### Scenario: What a recipient can check
- **WHEN** a letter is exported
- **THEN** it states its reference and how to check it, and claims no certificate signature

#### Scenario: Verified by reference and code (rev 0.61)
- **GIVEN** an issued letter whose footer carries a reference and a verification code
- **WHEN** a recipient enters both, with no sign-in
- **THEN** they learn only whether it is valid or withdrawn, its type, the day it was issued and the company, and a wrong code, an unknown reference and a letter not yet issued answer alike
- **AND** at most 10 tries for a reference in an hour are allowed, and each look-up of a found letter is audited

#### Scenario: Withdrawn by the employee (rev 0.61)
- **WHEN** the person a letter is about withdraws its verification
- **THEN** a look-up says withdrawn, and the letter itself stays as issued

#### Scenario: Verified by the document (rev 0.62)
- **GIVEN** a recipient who holds the letter as a file
- **WHEN** they choose it on the verification page
- **THEN** only the file's SHA-256 is sent, and they learn what a look-up by reference and code tells them
- **AND** a document that is not an issued letter, or changed by one byte, is not found

#### Scenario: The disclosure link (rev 0.62)
- **GIVEN** an issued letter whose verification stands
- **WHEN** the person it is about makes a link for 1 to 30 days and gives it to a recipient
- **THEN** the recipient sees the letter's facts about the person, never the company, the reference or what it is for, until the link ends or the person revokes it
- **AND** an unknown, expired or revoked link answers alike, and every look-up is audited without the facts

#### Scenario: Revoking and withdrawing (rev 0.62)
- **WHEN** the person revokes a link
- **THEN** the letter and its verification are unchanged
- **AND WHEN** they withdraw the verification, no link shows facts and no new link is made

#### Scenario: A limit every instance shares (rev 0.62)
- **WHEN** two API instances each count tries for one reference
- **THEN** together they allow 10 in an hour, because the count is kept in the database
