# people/fyniq-integration Specification

## Purpose
How Nemura works with Fyniq when linked: the sync, one sign-in, pay runs to the ledger, link health and the minimum data shared, with the events of Appendix E. Derived from PRD-002 §16 and Appendix E.

## Requirements

### Requirement: Events and data shared with Fyniq
Nemura and Fyniq SHALL exchange only the events, fields and scopes of PRD-002 Appendix E.

Source: PRD-002 §E

#### Scenario: Scopes
- **WHEN** Nemura calls Fyniq
- **THEN** it uses only the scopes Appendix E lists (`people.projection.write`, `payroll.journal.submit`, `payroll.payments.submit`, `people.availability.write`, `people.holds.write`)
- **AND** Fyniq reads hold status with `people.holds.read`

#### Scenario: Payment lines
- **WHEN** salary payment lines go to Fyniq
- **THEN** they are encrypted in transit and at rest and are not kept as contacts

#### Scenario: Envelope
- **WHEN** any event or call passes between the apps
- **THEN** it carries NM-INT-10's envelope

#### Scenario: Payment status comes back
- **WHEN** Fyniq learns a salary payment's state
- **THEN** it sends `payroll.payments.status` to Nemura

#### Scenario: Line manager in the person event
- **WHEN** a person's line manager changes in Nemura
- **THEN** `people.person.upserted` carries the new manager's person ID, so Fyniq routes their next claim to that manager

### Requirement: NM-INT-01 Sync
When linked, Nemura SHALL send its events through its outbox for Fyniq to apply idempotently, departments and cost centres SHALL come from Fyniq, and people and their changes SHALL go to Fyniq's employee projection.

Source: PRD-002 §16 · Priority M · Phase P1

#### Scenario: Departments from Fyniq
- **WHEN** the accountant renames a department in Fyniq
- **THEN** Nemura shows the new name, and the department's salary account stays Fyniq's

#### Scenario: Idempotent
- **WHEN** the same event arrives twice
- **THEN** Fyniq applies it once

### Requirement: NM-INT-02 One sign-in, one switch
A person signed in to either app SHALL be signed in to both, the Finance | People switch SHALL keep their place in each, and signing out of one SHALL sign out of both.

Source: PRD-002 §16 · Priority M · Phase P1

#### Scenario: Sign out
- **WHEN** a person signs out of Nemura
- **THEN** they are signed out of Fyniq too

#### Scenario: How sessions work
- **WHEN** a person signs in to either app
- **THEN** NM-INT-12's sessions apply

### Requirement: NM-INT-03 Pay runs to the ledger
An approved pay run SHALL be sent once with its approval evidence (NM-INT-06), Fyniq's posting reference SHALL show on the run, and a refused posting SHALL come back with Fyniq's error code and a plain message while the run waits.

Source: PRD-002 §16 · Priority M · Phase P1

#### Scenario: Locked period
- **WHEN** Fyniq refuses the posting because the period is locked
- **THEN** the run shows Fyniq's error code and a plain message, and waits

### Requirement: NM-INT-04 Health
The link SHALL show its state, last successful sync, waiting or failed events and a retry, and a sync failing for 24 hours SHALL be ⚠ on both apps' Checks.

Source: PRD-002 §16 · Priority M · Phase P1

#### Scenario: Failing for a day
- **WHEN** the sync has failed for 24 hours
- **THEN** Checks shows ⚠ in Nemura (N.1 #7) and Fyniq notifies its Owner and Payroll administrator

#### Scenario: Near pay day
- **GIVEN** a run between its input cut-off and its pay day
- **WHEN** the link fails
- **THEN** the Payroll administrator and the Owner are alerted within 1 hour

### Requirement: NM-INT-05 Minimum data to Fyniq
Nemura SHALL send Fyniq only Appendix E's fields: never NRIC or FIN, medical, disciplinary or review data, and pay only as department totals and salary payment lines.

Source: PRD-002 §16 · Priority M · Phase P1

#### Scenario: No restricted data
- **WHEN** a person is synced to Fyniq
- **THEN** no NRIC or FIN, medical, disciplinary or review data is sent

### Requirement: NM-INT-06 Approved runs carry their proof
Nemura's pay-run approval SHALL be an approval envelope over the run's canonical figures, and `payroll.run.approved` SHALL carry the envelope, its SHA-256, the approver's signed record and Nemura's link signature for Fyniq to verify before posting.

Source: PRD-002 §16 · Priority M · Phase P1

#### Scenario: What the envelope holds
- **WHEN** a run is approved
- **THEN** the envelope holds the organisation, run, version and wage month, totals by department and pay item, the pack, policy and account-mapping versions, the preparer and the approver

#### Scenario: Refused by Fyniq
- **WHEN** Fyniq refuses the run (PRD-001 FR-PPL-09)
- **THEN** Nemura shows the reason, and the run waits

### Requirement: NM-INT-07 One person across both apps
The preparer and approver SHALL be identified by issuer and subject in both apps, and the run's preparer SHALL NOT approve the run or approve or release its salary batch in either app; no emergency path SHALL change a salary account, lift a hold or pay the person using it.

Source: PRD-002 §16 · Priority M · Phase P1

#### Scenario: Preparer in Fyniq
- **GIVEN** Mei Lin Tan prepared the October run
- **WHEN** she tries to approve its salary batch in Fyniq
- **THEN** it is refused (PRD-001 SOD-4, FR-PPL-10)

#### Scenario: Emergency path
- **WHEN** an emergency path is used
- **THEN** it cannot change a salary account, lift a tax-clearance hold or pay the person using it

### Requirement: NM-INT-08 Tax-clearance holds
When linked, Nemura SHALL be the hold authority: it SHALL set a versioned hold on all monies due from the date the company learns of a covered cessation, send it at once, and lift it only on IRAS's release or directive recorded with evidence.

Source: PRD-002 §16 · Priority M · Phase P1

#### Scenario: Hold sent at once
- **GIVEN** Chen Yu's resignation is recorded on Thu 1 Oct
- **WHEN** his hold is set
- **THEN** `people.tax_hold.changed` goes to Fyniq at once, ahead of ordinary sync
- **AND** his final pay and any claim are held

#### Scenario: Nemura's own bank file
- **GIVEN** Nemura is not linked
- **WHEN** the bank file is exported
- **THEN** held people's lines are left out

#### Scenario: Release
- **WHEN** IRAS's release notice is recorded with evidence
- **THEN** the hold lifts

#### Scenario: Unlinking
- **WHEN** the link is removed
- **THEN** open holds move to Fyniq

### Requirement: NM-INT-09 Bank details travel with their version
Each salary payment line SHALL carry its bank-detail record ID, version, digest and approval with the account masked, and a later approved change SHALL send the new version, never the number, so that Fyniq blocks the person's unreleased lines.

Source: PRD-002 §16 · Priority M · Phase P1

#### Scenario: Changed after the batch was approved
- **GIVEN** Daniel Lim's new bank account is approved after the batch was approved
- **WHEN** Nemura sends `people.bank_details.changed`
- **THEN** Fyniq blocks his line until the batch is approved again with the current version

### Requirement: NM-INT-10 Event envelope
Every event and call between the apps SHALL carry the envelope (IDs, versions, organisations, epoch, times, correlation, digest) signed with the link key, and each side SHALL dedupe by event ID, ignore older versions, repair gaps and keep tombstones, never logging pay or bank payloads.

Source: PRD-002 §16 · Priority M · Phase P1

#### Scenario: A late older version
- **GIVEN** version 2 of Wei Jie Ong's record, with a new manager, was applied
- **WHEN** version 1 arrives late
- **THEN** it does not bring back the old manager

#### Scenario: A repeated salary-ready event
- **WHEN** `payroll.payments.ready` arrives twice
- **THEN** one set of payment instructions exists

#### Scenario: Cancelled leave
- **WHEN** leave is cancelled
- **THEN** a tombstone with a version is sent, and Fyniq's available hours are restored

#### Scenario: Replay
- **WHEN** an event's signature fails, or its timestamp is more than 5 minutes off
- **THEN** it is refused

### Requirement: NM-INT-11 Link lifecycle
The link SHALL have states and an epoch, refusing earlier epochs, SHALL settle work in flight before unlinking, and a feature source change SHALL import year-to-date state, opening leave and mappings and cancel the old source's queued work.

Source: PRD-002 §16 · Priority M · Phase P1

#### Scenario: Unlinking with an unknown outcome
- **GIVEN** a payment whose outcome is unknown
- **WHEN** the Owner starts to unlink
- **THEN** unlinking waits, and no other channel pays it meanwhile

#### Scenario: Old credentials
- **WHEN** a credential from an earlier epoch is used
- **THEN** it is refused

#### Scenario: Source change in July
- **GIVEN** payroll moves to Nemura on 1 Jul
- **WHEN** the July run is calculated
- **THEN** January–June ordinary and additional wages are there

### Requirement: NM-INT-12 Identity and sessions across the apps
People SHALL be matched by issuer, subject and tenant, never by email alone; each app SHALL keep its own audience-bound session and CSRF protection; sign-out or deactivation in either SHALL revoke both within 60 seconds; linking an existing tenant SHALL map people explicitly.

Source: PRD-002 §16 · Priority M · Phase P1

#### Scenario: HR only
- **GIVEN** Sarah Koh has Nemura rights only
- **WHEN** she follows a link to Fyniq
- **THEN** she lands on People home, with no financial figures

#### Scenario: Same email, two tenants
- **WHEN** an organisation that ran on its own tenant is linked
- **THEN** each person is mapped explicitly and confirms it, and no account is merged by email

#### Scenario: Deactivated
- **WHEN** a person is deactivated in Fyniq
- **THEN** their Nemura session ends within 60 seconds

### Requirement: NM-INT-13 Restore and reconcile
Both apps SHALL keep each run's receipts and a reconciliation checkpoint; after either database is restored, cross-app work after the restore point SHALL be held as unknown and reconciled before anything is sent or paid, and privacy deletions SHALL be applied again.

Source: PRD-002 §16 · Priority M · Phase P1

#### Scenario: Nemura restored
- **GIVEN** Nemura is restored to before the October run was sent
- **WHEN** it restarts
- **THEN** the run's hand-off is reconciled with Fyniq's receipts, and nothing is sent twice

#### Scenario: Deleted data comes back
- **WHEN** a restore brings back a personal field that had been deleted
- **THEN** it is deleted again before anyone can see it
