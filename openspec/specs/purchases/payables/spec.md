# purchases/payables Specification

## Purpose
How Fyniq handles what the company owes its suppliers: purchase orders, bills and their states, vendor credits, bill capture and automated bill entry, InvoiceNow inbound, ageing and paying bills, expense claims and card spend as payables, withholding tax on non-resident payments, input-tax eligibility, and supplier contracts. Derived from PRD-001 §8.6.

## Requirements

### Requirement: FR-AP-01 Purchase orders
The system SHALL keep purchase orders with lines and approval, convert them to bills, and offer an optional three-way match of PO, receipt and bill.

Source: PRD-001 §8.6 · Priority S · Phase P1 (PO) / P2 (match)

#### Scenario: Purchase order with lines and approval
- **WHEN** a purchase order is raised with lines
- **THEN** it goes through approval

#### Scenario: Convert to a bill
- **WHEN** a user converts a purchase order
- **THEN** a bill is created from it

#### Scenario: Optional three-way match (P2)
- **GIVEN** the organisation turns on three-way matching
- **WHEN** a bill is entered against a purchase order
- **THEN** the bill is matched against the purchase order and the receipt

### Requirement: FR-AP-02 Bills
The system SHALL keep bills as the mirror of invoices, with expense or asset accounts, prepayment periods, GST input tax codes and attachments, and with separate approval, ledger, payment and due states, as for invoices (FR-AR-03).

Source: PRD-001 §8.6 · Priority M · Phase P1

#### Scenario: Bill lines
- **WHEN** a bill line is entered
- **THEN** it takes an expense or asset account and a GST input tax code

#### Scenario: Prepayment period
- **WHEN** a bill carries a prepayment period
- **THEN** the period is recorded on the bill

#### Scenario: Attachments
- **WHEN** a file is attached to a bill
- **THEN** it is kept with the bill

#### Scenario: Approval, ledger and due states as for invoices
- **WHEN** a bill's approval, ledger or due state is shown
- **THEN** it follows the same states as an invoice's (FR-AR-03)

#### Scenario: Payment state
- **WHEN** a bill's payment state is shown
- **THEN** it is unscheduled, in a batch, paid or written off

### Requirement: FR-AP-03 Vendor credits and refunds
The system SHALL support vendor credits and refunds from vendors.

Source: PRD-001 §8.6 · Priority M · Phase P1

#### Scenario: Vendor credit
- **WHEN** a vendor credit is recorded
- **THEN** it can be applied to that vendor's bills

#### Scenario: Refund from a vendor
- **WHEN** a vendor refunds the company
- **THEN** the refund is recorded against the vendor credit it settles

### Requirement: FR-AP-04 Bill capture
The system SHALL capture bills forwarded to a per-organisation email address or uploaded or photographed, as part of FR-DOC-12 (now P1).

Source: PRD-001 §8.6 · Priority S · Phase P1

#### Scenario: Forwarded by email
- **WHEN** a bill is forwarded to the organisation's capture email address
- **THEN** it is captured

#### Scenario: Uploaded or photographed
- **WHEN** a bill is uploaded or photographed
- **THEN** it is captured

#### Scenario: Document AI drafts the bill
- **WHEN** a bill is captured
- **THEN** document AI extracts the vendor, dates, number, lines, GST and totals into a draft bill

#### Scenario: Duplicates blocked
- **WHEN** a captured bill duplicates one already captured or entered
- **THEN** it is blocked as a duplicate (FR-AP-12)

### Requirement: FR-AP-05 Receiving Peppol invoices
The system SHALL receive Peppol invoices (InvoiceNow inbound) into draft bills, because the InvoiceNow requirement covers purchase data as well as sales.

Source: PRD-001 §8.6 · Priority M with FR-AR-12 · Phase P2

#### Scenario: Inbound Peppol invoice becomes a draft bill
- **WHEN** a supplier's invoice arrives by InvoiceNow
- **THEN** a draft bill is created from it

### Requirement: FR-AP-06 AP ageing and cash requirement
The system SHALL report AP ageing, the bills to pay by due date and a cash requirement forecast.

Source: PRD-001 §8.6 · Priority M · Phase P1

#### Scenario: AP ageing
- **WHEN** AP ageing is produced
- **THEN** open bills are shown by age

#### Scenario: Bills to pay by due date
- **WHEN** a user views the bills to pay
- **THEN** they are listed by due date

#### Scenario: Cash requirement forecast
- **WHEN** a user views the cash requirement forecast
- **THEN** it shows the cash needed to pay the open bills as they fall due

### Requirement: FR-AP-09 Recurring bills
The system SHALL create recurring bills.

Source: PRD-001 §8.6 · Priority S · Phase P1

#### Scenario: Recurring bill is created on schedule
- **GIVEN** a recurring bill
- **WHEN** its next date arrives
- **THEN** a bill is created from it

### Requirement: FR-AP-10 Vendor statement reconciliation
The system SHALL reconcile a vendor's statement with the vendor's balance and documents in Fyniq.

Source: PRD-001 §8.6 · Priority C · Phase P2

#### Scenario: Vendor statement reconciled
- **WHEN** a vendor statement is reconciled
- **THEN** its documents and balance are compared with the vendor's documents and balance in Fyniq
- **AND** the differences are shown

### Requirement: FR-AP-11 Withholding tax on non-resident payments
The system SHALL keep records of withholding tax on payments to non-resident service providers and support the S45 form.

Source: PRD-001 §8.6 · Priority C · Phase P3

#### Scenario: Withholding tax records and S45 support
- **WHEN** tax is withheld on a payment to a non-resident service provider
- **THEN** the withholding is recorded and S45 form support is provided

### Requirement: FR-AP-19 Non-resident payments: withholding-tax applicability
The system SHALL record each supplier's tax residence and, on a bill from a non-resident supplier, the nature of the payment and the tax agent's decision, kept as evidence (FR-DOC-05) and reused for that supplier and nature.

Source: PRD-001 §8.6 · Priority M when paying non-resident suppliers; else S · Phase P1

#### Scenario: Nature of the payment
- **WHEN** a bill from a non-resident supplier is entered
- **THEN** it records the nature of the payment: goods; software bought for own use; software rights or royalties; services, and whether they are performed in Singapore or wholly outside it; interest; rent of movable property; or other

#### Scenario: Tax agent's decision kept and reused
- **WHEN** the tax agent decides a bill's withholding tax
- **THEN** the decision is one of: not applicable; withhold at the stated rate; reduced treaty rate with a certificate of residence
- **AND** it is kept as evidence (FR-DOC-05) and reused for that supplier and nature

#### Scenario: No decision yet
- **GIVEN** a bill from a non-resident supplier with no decision for that supplier and nature
- **WHEN** the bill is shown or added to a payment batch (FR-BNK-12)
- **THEN** the bill shows ⚠ and the payment batch holds it

#### Scenario: Ordinary remote SaaS is not blocked
- **WHEN** a "not applicable" decision applies to ordinary remote SaaS
- **THEN** the bill is never blocked or taxed

#### Scenario: Tax withheld
- **WHEN** tax is withheld on a bill
- **THEN** the bill posts the gross expense, the net to the supplier and the tax to a withholding-tax payable account
- **AND** a statutory-calendar task is created for the 15th of the second month after the date of payment

#### Scenario: S45 filing stays manual
- **WHEN** a withholding tax has to be filed
- **THEN** S45 filing stays manual until FR-AP-11

#### Scenario: G4-010 US SaaS bill and on-site non-resident consultant
- **GIVEN** a US SaaS subscription bill, and a non-resident consultant who works on site in Singapore for 5 days
- **WHEN** both bills are entered and paid
- **THEN** for the SaaS bill the decision "not applicable" is recorded and it is paid normally
- **AND** the consultant's bill shows ⚠ and is held until the tax agent decides
- **AND** if tax is withheld, the supplier is paid net and the tax is posted to the withholding-tax payable

### Requirement: FR-AP-12 Automated bill entry
The system SHALL turn every captured bill (FR-DOC-12) into a draft with the supplier, number, dates, currency, lines, GST and totals filled in, and SHALL send the draft into the normal approval policy (AI-30).

Source: PRD-001 §8.6 · Priority M · Phase P1

#### Scenario: Supplier defaults suggested
- **WHEN** a draft bill is created for a supplier
- **THEN** the supplier defaults (account, GST code, dimensions, terms) are suggested from that supplier's approved bills

#### Scenario: Confirmed defaults become a supplier rule
- **WHEN** a person confirms the suggested supplier defaults
- **THEN** they are saved as a supplier rule

#### Scenario: Duplicates blocked
- **WHEN** a captured bill has the same supplier, number and amount as an existing bill, with a date inside the date window
- **THEN** it is blocked as a duplicate

#### Scenario: Normal approval policy
- **WHEN** a draft bill is created from a capture
- **THEN** it goes into the normal approval policy (AI-30)

### Requirement: FR-AP-13 Track and pay bills
The system SHALL show a bills board by status (to review, awaiting approval, approved, scheduled, paid, overdue) and a due-date calendar, and SHALL let a user schedule a bill for a payment date.

Source: PRD-001 §8.6 · Priority M · Phase P1

#### Scenario: Bills board and due-date calendar
- **WHEN** a user opens the bills board
- **THEN** bills are shown by status (to review, awaiting approval, approved, scheduled, paid, overdue)
- **AND** a due-date calendar shows them by due date

#### Scenario: Scheduled bill joins that day's batch
- **WHEN** a bill is scheduled for a payment date
- **THEN** it joins that day's payment batch (FR-BNK-12)

#### Scenario: Reminders before due dates
- **GIVEN** an unpaid bill
- **WHEN** its due date approaches
- **THEN** a reminder is sent before the due date

#### Scenario: Early-payment discounts shown
- **WHEN** a bill offers an early-payment discount
- **THEN** the discount is shown

#### Scenario: Remittance advice
- **WHEN** a bill is paid
- **THEN** a remittance advice is emailed to the supplier

#### Scenario: Supplier statements reconciled
- **WHEN** a supplier statement is received
- **THEN** it is reconciled (FR-AP-10)

### Requirement: FR-AP-20 Input-tax eligibility
The system SHALL decide input-tax eligibility on bills, claims and card lines per line from recorded facts, never from an extracted GST amount alone, and SHALL record the condition that decided it: claimable, not claimable, or ⚠ for the tax agent.

Source: PRD-001 §8.6 · Priority M when GST-registered; else S · Phase P1

#### Scenario: Registration of the organisation and the supplier
- **WHEN** input tax on a line is decided
- **THEN** it is claimable only if the organisation is GST-registered at the claim date and the supplier is GST-registered

#### Scenario: Valid document
- **WHEN** input tax on a line is decided
- **THEN** the document must be a valid tax invoice addressed to the company ("Company c/o another" counts), a simplified tax invoice (total including GST up to S$1,000; no customer name needed), or an import permit naming the company

#### Scenario: Invoice in an employee's or director's name
- **GIVEN** an invoice in an employee's or director's name
- **WHEN** the person acted as the company's agent and the company reimbursed them
- **THEN** the invoice qualifies [verify for a claim kept as a director's loan, FR-EQ-02]

#### Scenario: Business purpose
- **WHEN** a purchase has a private share
- **THEN** the private share is apportioned or excluded

#### Scenario: Disallowed expenses
- **WHEN** the expense is disallowed under GST Regulations 26–27 (motor cars, club fees, medical expenses and insurance not required by law, benefits for staff families, games of chance)
- **THEN** the input tax is not claimable and the line takes code BL

#### Scenario: Extracted amount alone never decides
- **WHEN** document capture (FR-DOC-12) extracts a GST amount
- **THEN** the amount alone does not make the input tax claimable
- **AND** capture proposes the facts and the approver confirms them

#### Scenario: G4-026 Simplified tax invoice
- **WHEN** a receipt of S$54.50 shows the supplier's GST number and "price includes GST", with no customer name, for a business purpose
- **THEN** it is a simplified tax invoice: input tax S$4.50 claimable, reason recorded

#### Scenario: G4-027 Employee's mobile bill in their own name
- **WHEN** an employee's mobile bill is in their own name and the business share is reimbursed
- **THEN** it is claimable for the business share with agency and reimbursement evidence
- **AND** the private share is excluded

#### Scenario: G4-028 No GST number, motor-car rental, private purchase
- **WHEN** a receipt without a GST number, a motor-car rental and a private purchase are entered
- **THEN** each is not claimable, with its reason (no GST charged; regulation 27, code BL; private)

### Requirement: FR-AP-21 Supplier contracts and renewals
The system SHALL keep a register of supplier commitments (software and AI tools, cloud, office, services) with supplier, owner, start and end dates, renewal type, notice deadline, billing frequency, amount and currency, minimum commitment and agreed price changes.

Source: PRD-001 §8.6 · Priority S · Phase P2

#### Scenario: Renewal type and notice deadline
- **WHEN** a commitment is recorded
- **THEN** its renewal type says whether it auto-renews
- **AND** its notice deadline is the last day to cancel, often well before the renewal date

#### Scenario: Contract is evidence and links to spend
- **WHEN** a contract is added to the register
- **THEN** it is kept as evidence (FR-DOC-05)
- **AND** it links to its POs, recurring bills and bills

#### Scenario: Notice-deadline reminders
- **WHEN** a notice deadline is 30 days away and again when it is 7 days away (settings)
- **THEN** a reminder goes to the owner's Inbox

#### Scenario: The register posts nothing
- **GIVEN** a commitment with no bill or accrual
- **WHEN** the register is shown
- **THEN** the commitment is shown and not booked
