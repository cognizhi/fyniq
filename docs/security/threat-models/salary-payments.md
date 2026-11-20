# Threat model: salary payments from a pay run, and payroll outside Nemura

| | |
|---|---|
| Feature and requirement IDs | The salary batch, paid means paid, the payroll go-live gate and a provider's journal as a draft: FR-PAY-11, FR-PAY-15, FR-PPL-05, FR-PPL-10, FR-PPL-12, FR-PPL-13, FR-PPL-14, FR-PPL-17, FM-1, SOD-2, SOD-4 (PRD-001 revisions 0.423 to 0.429) |
| Author, reviewer, date | Claude Code for the founders, 21 Nov 2026; second review at sign-off (waived before production, AGENTS.md) |
| Data and its class | Each person's pay and bank account (restricted payroll data: FR-PAY-13), sealed with the application's key; the batch's totals, counts and states (internal); the run's preparer and approver by issuer and subject |
| Who acts | The link's client (the payment lines), the Payroll administrator (prepares), the Owner (approves), the Payment releaser (releases), the worker (sends states to Nemura); `fyniq_api`, `fyniq_worker` |

## 1. What we are building

Nemura sends the payment lines of a pay run Fyniq has posted (`payroll.payments.ready`). They become a salary batch with sealed lines, are prepared, approved through the approval engine by a second person
who is not the run's preparer or approver in Nemura, and released by the Payment releaser in parts: a held person, one with new bank details and one with an unknown outcome wait. A line is paid only from the bank feed
or a person's evidence. Every state goes back to Nemura. Where payroll is not in Nemura, a gate holds a pay run until its statutory outputs are recorded, and a provider's journal is only a draft.

Trust boundaries: **Nemura ↔ link** (token, link key signature, epoch, 5-minute window); **person ↔ API** (Appendix D, step-up, SOD); **API ↔ database** (RLS, the guards of migration 000912, sealed columns); **worker ↔ Nemura** (the outbox, the link's token).

## 2. What can go wrong (STRIDE per boundary)

| # | Boundary or step | Threat (STRIDE) | Impact | Likelihood |
|---|---|---|---|---|
| T1 | Nemura ↔ link | Tampering: lines for a version Fyniq did not post, for other people than the run's, or for more than its net pay | Money paid that no approved run stands behind | Medium |
| T2 | Nemura ↔ link | Information disclosure: a person's name, account or pay reaches an audit event, a log, an outbox row, an error or a notification | A salary leaks to roles with no need (FR-PPL-17) | Medium |
| T3 | person ↔ API | Elevation: the run's preparer or approver in Nemura approves or releases its salary batch, in any team mode | One person controls pay end to end (SOD-4) | Medium |
| T4 | person ↔ API | Tampering: a line or a bank detail changes after the approval and is paid under it | Money paid to an account no one approved (FR-PPL-14) | Medium |
| T5 | person ↔ API | Elevation: a person held for tax clearance is paid, by approval or break-glass | IRAS's hold broken (FR-PPL-13) | Medium |
| T6 | person ↔ API | Tampering: a line is marked paid with no payment behind it | Pay shown paid that never left; an unknown outcome paid twice | Medium |
| T7 | API ↔ database | Tampering: a line, an event or a release is changed or deleted, or a released batch cancelled | History of a payment rewritten | Low |
| T8 | person ↔ API | Elevation: a role with no right to the lines reads them (a Releaser, a Controller, an Accountant, an Auditor) | Salaries visible beyond need (FR-PPL-17) | Medium |
| T9 | person ↔ API | Tampering: a provider's journal posts payroll that Nemura also posts, or is posted without a second person | The same salaries counted twice, or a payroll journal nobody verified | Medium |
| T10 | person ↔ API | Elevation: a person named as a claim's first approver by the projection reaches other approvals | Approval authority widened by a relationship | Low |

## 3. What we do about it

| Threat | Control | Test that proves it (`// req:`) |
|---|---|---|
| T1 | `receivePayrollPayments` needs the link's client, scope and signature; the run version must be the latest receipt, the parties the run's, the people in the projection, and the lines' sum at most 2100's posted total (PPL-23 to PPL-28) | `TestASalaryBatchIsApprovedThenReleasedWithTheHeldLeftWaiting` |
| T2 | Lines are sealed (AAD binds organisation, batch, line, version); the audit holds counts and IDs; the outbox row names a state's ID and the worker reads figures when it sends; the approval's envelope holds a total and a digest | `TestAPaymentIsPaidOnlyFromTheBankFeedOrAPersonsEvidenceAndEveryStateGoesBackToNemura` (the trail, the queued rows) |
| T3 | `salaryExecutable` and `releaseSalaryBatch` refuse a run party by issuer and subject; the database refuses it (FY912); FM-1 and SOD-2 on the requester and the preparer | `TestAttackNoOneWhoPreparedOrApprovedTheRunApprovesOrReleasesItsSalary` |
| T4 | The envelope holds a digest of every line with its bank-detail version and digest; a newer projection version blocks the line (SB-07); a revised line is a new version and the approval is stale (SB-05) | `TestAChangedBankDetailBlocksTheLineUntilTheBatchIsApprovedAgain` |
| T5 | `holdStatus` asks Nemura within five seconds at every release and fails closed (PPL-21, PPL-22); no break-glass reaches a salary; check C.2 #44 finds a late hold | `TestASalaryBatchIsApprovedThenReleasedWithTheHeldLeftWaiting`, `TestThePayrollChecksFindWhatTheyAreFor` |
| T6 | The database refuses paid, rejected, returned and unknown from a release or an arrival; a manual state needs evidence, a bank state a bank line; the legal moves are Nemura's; both need a person who is not a run party | `TestAPaymentIsPaidOnlyFromTheBankFeedOrAPersonsEvidenceAndEveryStateGoesBackToNemura` |
| T7 | Append-only guards and no truncate (FY912); `cancelSalaryBatch` refused with a release | `TestAttackNoOneWhoPreparedOrApprovedTheRunApprovesOrReleasesItsSalary` |
| T8 | The lines appear only with C or A on the pay-run row; every other role sees total, count and state; a release's lines and total only with that right; the tenant export carries the header only | `TestASalaryBatchIsApprovedThenReleasedWithTheHeldLeftWaiting` (the releaser's view), the matrix rows |
| T9 | A provider's journal is a draft under Journals, idempotent per system, period and version, refused while payroll lives in Nemura (PPL-29); the gate holds a pay run until its outputs are recorded (PPL-30) | `TestAProvidersPayrollJournalIsOnlyEverADraftOncePerPeriodAndVersion`, `TestThePayrollGoLiveGateHoldsAPayRunUntilItsArrangementsAreRecorded` |
| T10 | `NamedApprover` reaches four approvals operations only for a pending claim request that names the person as its first approver, never the requester, and the steps decide the rest | `TestALineManagerApprovesTheirReportsClaimFirstWithNoRole` |

## 4. What is left

The payment file reaches the Payment releaser, who sees totals in the screens (owner to confirm); the bank queue does not mask a line matched to a salary batch for people without the right (FR-PPL-17); a salary batch has
no evidence parent of its own; no sealing of the link's private keys under the platform key (`[live-verify]`).
