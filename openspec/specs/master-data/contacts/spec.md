# master-data/contacts Specification

## Purpose
How customers and vendors are recorded in Fyniq: their details, their live balances and activity, the protection of vendor bank details, and the detection of duplicate contacts. Derived from PRD-001 §8.3.

## Requirements

### Requirement: FR-CT-01 Contacts
The system SHALL keep contacts that are a customer, a vendor or both, each with a legal name, UEN or tax ID, Peppol ID, billing and remittance emails, phone, addresses, currency, payment terms, default category, country and default GST code.

Source: PRD-001 §8.3 · Priority M · Phase P1

#### Scenario: Contact role
- **WHEN** a contact is created
- **THEN** it can be marked as a customer, a vendor or both

#### Scenario: Contact details are recorded
- **WHEN** a contact is saved with a legal name, UEN or tax ID, Peppol ID, billing and remittance emails, phone, addresses, currency, payment terms, default category, country and default GST code
- **THEN** each of those values is stored on the contact

### Requirement: FR-CT-02 Live contact balances and activity
The system SHALL show, for each contact, its live balances (owed to us, we owe), its sales and spend for the financial year, its ageing and an activity timeline.

Source: PRD-001 §8.3 · Priority M · Phase P1

#### Scenario: Balances are live
- **GIVEN** a contact with open invoices and bills
- **WHEN** a document of that contact is posted or settled
- **THEN** the contact's "owed to us" and "we owe" balances include it at once

#### Scenario: Financial-year totals and ageing
- **WHEN** a user opens a contact
- **THEN** the contact's sales and spend for the financial year and its ageing are shown

#### Scenario: Activity timeline
- **WHEN** a user opens a contact
- **THEN** an activity timeline of that contact is shown

### Requirement: FR-CT-03 Vendor bank details are high-risk data
The system SHALL treat vendor bank details as high-risk data: a change MUST be dual-controlled (SOD-1, FM-1), MUST notify all founders and MUST freeze payments to that vendor until it is approved.

Source: PRD-001 §8.3 · Priority M · Phase P1

#### Scenario: Bank detail change needs dual control
- **WHEN** a user changes a vendor's bank details
- **THEN** the change takes effect only after a second person approves it (SOD-1, FM-1)

#### Scenario: Founders are notified
- **WHEN** a change to a vendor's bank details is submitted
- **THEN** all founders are notified

#### Scenario: Payments are frozen until approval
- **GIVEN** a change to a vendor's bank details awaits approval
- **WHEN** a payment to that vendor is attempted
- **THEN** the payment is frozen until the change is approved

#### Scenario: Payee name check where available
- **GIVEN** the bank offers a payee name check (P2)
- **WHEN** a vendor's bank details are changed
- **THEN** the payee name is checked against the bank

#### Scenario: One approved account per currency (rev 0.152)
- **WHEN** a vendor's bank account is added or changed
- **THEN** it is a Singapore account (SWIFT/BIC, account number, account name) or an overseas one (SWIFT/BIC, account number or IBAN, account name, the bank's country), with no PayNow proxy
- **AND** the vendor holds at most one approved account per currency

#### Scenario: The freeze before payment batches (rev 0.152)
- **GIVEN** a change to a vendor's bank details awaits approval and payment batches do not exist yet
- **WHEN** someone plans a payment date for one of the vendor's bills, or records a payment already made to the vendor
- **THEN** planning the date is refused
- **AND** recording the payment is allowed with a warning

#### Scenario: Account numbers sealed and masked (rev 0.152)
- **WHEN** a vendor's account number is kept, shown or audited
- **THEN** it is sealed at the application level (SEC-11), shown with its last four digits only, and audited as its masked value and keyed fingerprint (AUD-02)
- **AND** a holder of C or A on "Vendor bank details" reveals the full number only after step-up, and the reveal is audited
- **AND** the approver of a change sees the old and new numbers in full

### Requirement: FR-CT-04 Duplicate contact detection
The system SHALL detect duplicate contacts on name, UEN, email and bank account.

Source: PRD-001 §8.3 · Priority S · Phase P1

#### Scenario: Possible duplicate contact
- **GIVEN** an existing contact
- **WHEN** a contact is created or edited with the same name, UEN, email or bank account as that contact
- **THEN** the system flags it as a possible duplicate of the existing contact
