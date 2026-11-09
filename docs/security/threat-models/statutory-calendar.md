# Threat model: the statutory calendar and the GST registration monitor

| | |
|---|---|
| Feature and requirement IDs | The GST registration monitor and its forecasts: FR-TAX-05, C.1 #37 (PRD-001 rev 0.208, rev 0.210), `p1-gst` slice 2. The statutory calendar, its filing records and Appendix D's Statutory calendar row: FR-TAX-07, CMP-02, FR-DOC-06 (rev 0.211), slice 3a. The registers' deadlines, C.2 #13, C.2 #15 and the reminders: FR-NTF-01 (rev 0.212), slice 3b. The screens (rev 0.213), slice 4. |
| Author, reviewer, date | Claude Code for the founders, 7 Nov 2026; reviewer: a founder at sign-off (second human review waived before production, AGENTS.md) |
| Data and its class | The calendar's items, generated from the sg-deadlines pack and the organisation's years, GST periods and settings (internal); filing records with their references and reasons (internal); acknowledgements as evidence (internal; a CPF Board acknowledgement can name employees: personal); GST forecasts with what they rest on and their contracts as evidence (confidential) |
| Who acts | On the calendar, the Owner, the Controller, the Accountant and the Tax agent record (C); the Administrator, the Auditor and the DPO read (Appendix D's Statutory calendar row). On the monitor, the Accountant and the Tax agent prepare forecasts (C on GST returns); the Owner and the Controller read. The worker sends the reminders. No AI or MCP tool reads or writes any of it |

## 1. What we are building

The calendar is generated on each read, never stored. What is stored is a person's record of an item (filed, not
required, or the withdrawal of a record) and a person's forecast, both documents with their evidence. Trust
boundaries:
- the recorder → API: a record or a forecast is hostile input;
- the API → the database: one live record per item, whoever writes;
- the API → readers: who sees the calendar, the records and their files;
- the worker → members: reminders by email and in the notification centre;
- the pack → the calendar: every date comes from a pack row.

## 2. What can go wrong (STRIDE per boundary)

| # | Boundary or step | Threat (STRIDE) | Impact | Likelihood |
|---|---|---|---|---|
| C1 | recorder → API | Tampering: an item recorded as filed with no acknowledgement, so a missed filing looks done | A statutory deadline is missed unnoticed | Medium |
| C2 | recorder → database | Tampering: two live records of one item, or a withdrawal of a record still live, written straight to the database or by two racing requests | The calendar says two things; a withdrawal undoes nothing | Low |
| C3 | records | Repudiation: a record is changed in place after the fact, or withdrawn without a reason | Who said what was filed, and why it was taken back, is lost | Low |
| C4 | readers | Information disclosure: someone without a right reads the records, or opens an acknowledgement that names employees | Personal data of employees exposed | Medium |
| C5 | recorder → API | Elevation: a reader (Administrator, Auditor, DPO), an Employee or the AI records or withdraws an item | Records by people who may not make them | Low |
| C6 | forecasts | Tampering: a forecast below the threshold recorded on no evidence, or a recorded forecast changed, to keep the company out of the prospective test | Late registration and IRAS's penalties | Medium |
| C7 | pack → calendar | Tampering: a wrong rule row or holiday moves every deadline | Every reminder and check is wrong | Low |
| C8 | worker → members | Information disclosure: a reminder emailed to someone who may not see the calendar | Statutory affairs known outside the role | Low |
| C9 | API | Denial of service: a calendar or monitor asked for a long window reads the whole ledger | A slow page for everyone in the organisation | Low |
| C10 | registers → calendar | Tampering: a register's deadline recorded as done on the calendar, so the register's own check clears without the register changing | A controller or a share lodgement stays wrong while the calendar says done | Low |

## 3. What we do about it

| # | Mitigation | Where | Verified by |
|---|---|---|---|
| C1 | A filed record is drafted, given its acknowledgement and recorded: refused without one while `evidence.statutory_filing` is required (DOC-10, the default from FR-DOC-06's table); under a recommended rule, C.2 #30 lists it | `internal/api/calendar.go`, `internal/domain/checks/evidence.go` | `TestTheStatutoryCalendarThroughTheAPI` |
| C2 | FY299: the guard keeps a record's fields; posting one takes a lock on its item's key and refuses a second live record; a withdrawal names a reversed record of the same item, once | `db/migrations/000274_statutory_filings.sql` | `TestAttackStatutoryFilingsKeepTheirRules` |
| C3 | Records are documents: append-only, every version audited; a withdrawal is a record of its own with its reason, beside the reversed one | `internal/domain/deadlines/filings.go` | `TestTheStatutoryCalendarThroughTheAPI` |
| C4 | The Statutory calendar row decides who reads records and files; an acknowledgement is evidence under FR-DOC-09's data classes | `internal/authz/policy.go` | `TestAuthorisationMatrix` |
| C5 | Recording and withdrawing need C on the row; the AI's column is — | `internal/authz/policy.go` | `TestAuthorisationMatrix`, `TestTheGrantsAreAppendixD` |
| C6 | A forecast needs its evidence to be recorded (GST-31) and never changes once recorded (GST-32); FY298 keeps its value and keys; the latest recorded is the test's, and every forecast stays listed | `internal/domain/gstmonitor/forecast.go`, `db/migrations/000272_gst_forecasts.sql` | `TestTheGSTRegistrationMonitorThroughTheAPI`, `TestAttackGSTForecastsKeepTheirRules` |
| C7 | Every rule and holiday cites its official page; 39 pinned cases worked from the sources' own examples; activation by two people (SOD-10); a year without holidays is Not assessed | `packs/sg-deadlines/2026.1/` | `TestADeadlinesPackKeepsItsRules`, the pinned run |
| C8 | Reminders go to holders of C on the row only (`notify.Audience`) | `internal/notify/reminders/deadlines.go` | `TestTheCalendarsChecksAndReminders` |
| C9 | A calendar window is at most 18 months (CAL-04); the monitor reads two calendar years | `internal/domain/deadlines/deadlines.go` | `TestTheStatutoryCalendarThroughTheAPI` |
| C10 | A register's deadline has no key a record accepts (CAL-01) and links to its record; C.2 #15 leaves it to C.2 #22 and C.2 #27 | `internal/domain/deadlines/registers.go` | `TestTheCalendarsChecksAndReminders` |

## 4. What is left

- The monitor reads every entry of the ledger to sum two years of taxable supplies. That is fine for a start-up's
  books; a large ledger needs a summary per document by month [live-verify on the dev cloud].
- Filing with IRAS, CPF Board or ACRA from Fyniq is P2 (`p2-tax-filings`) and would be a new boundary; no AI or MCP
  tool may ever file (§9.6).
