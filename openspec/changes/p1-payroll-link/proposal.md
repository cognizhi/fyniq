# Proposal

## Why

Payroll lives in Nemura (PRD-002 ADR-N2; owner decision 16 Oct 2026: built after Fyniq's go-live and before the first paid person), and Fyniq keeps
the books' side of it. Fyniq has no payroll code at all today: no employee projection, no payroll posting rule, no salary payment batch, no People and
HR settings. The line manager that personal approvals need (`p1-approval-engine` 5.1) waits for the projection. This change builds what Fyniq must do so
that a pay run approved in Nemura can be posted and paid in Fyniq with no one able to forge the authority, double-post a version, pay a held person or
pay a changed account, and so that a company without Nemura can still post payroll from a provider's journal or by hand under the go-live gate.

Nemura's own side (the payroll engine, the people-side state of the link, one sign-in and the workspace switch) is the paired change
`p1-nemura-fyniq-link` and `p1-nemura-payroll`. Contract fixtures stand in for pay runs here.

## What Changes

- **The contract** (FR-PPL-15, FR-PPL-03): the events and calls of PRD-002 Appendix E as operations in `contracts/openapi/` and event schemas in
  `contracts/events/`, one envelope (IDs, versions, organisations, epoch, times, correlation, digest, signature) with one canonical serialisation, and
  fixtures both apps test against (`contracts/peoplelink/`).
- **The link in Fyniq** (FR-PPL-01, 16, 08): Settings → People and HR, per-feature source, link states and the epoch, linking and unlinking with the Owner
  and a second approver, the link client registered with the link's own scopes only, link health and its alerts.
- **A durable inbox and the employee projection** (FR-PPL-15, 06, 18): events applied once, per-object versions, tombstones, gap repair; the projection
  (person, name, email, department, cost centre, dates, status, line manager, hold state and version, bank-detail version) that claim routing and the
  approvers' department figures read.
- **Verified pay runs and their posting** (FR-PPL-04, 09, 10, 11; FR-PAY-07; the payroll posting rules): the checks in one transaction, the posting rule
  `payroll_run@1` by department with no per-person lines, the difference of a corrected version, the closed-month rule, the oracle's half and a
  differential scenario.
- **Salary payments** (FR-PAY-11, FR-PPL-12, 13, 14, 17; C.2 #44): the salary batch on the payment batch, need-to-know lines, state per line, bank-detail
  versions, the hold check at every release.
- **Without Nemura** (FR-PPL-05, FR-PAY-15, C.1 #19 to 21, C.2 #14, #24, #45): a provider's journal or a file import as a draft, the payroll go-live gate,
  and the payroll checks.
- **Screens:** `people-hr-settings`, `payroll-overview`, `pay-run`, `salary-batch`, `employee-record` (the projection's view).

## Capabilities

### New Capabilities

None.

### Modified Capabilities

None (`skip_specs: true`). It builds requirements already in the main specs: `integrations/people-platform`, `payroll/pay-runs`,
`controls/checks`, `controls/platform-checks` and the `ledger/posting-rules` payroll rows (the plan's row for `p1-payroll-link`).

## Impact

- **Code:** `internal/domain/peoplelink`, `internal/domain/ledger` (payroll rule), `internal/domain/payroll`, `internal/adapters/postgres`,
  `internal/api`, `contracts/openapi`, `contracts/events`, `contracts/peoplelink`, `oracle/src/oracle/sources`, `internal/differential`, `apps/web`.
- **Data:** migrations 000900 to 000949: the link, its inbox, the projection, receipts and the salary batch lines (sealed).
- **Docs:** PRD revision recording the decisions of design D3, D4 and D11 (owner to confirm); user-guide and knowledge-base pages for the screens.
- **Needs the owner:** design D3 (who verifies the approver's passkey) and D11 are security-posture defaults to confirm. The SDL difference (question 27)
  and the `[verify]` tags of FR-PAY-10 and FR-PAY-16 stay open and are not decided here.
