# Design

## Context

`p1-nemura-records-packs` built the person, their effective-dated job and legal classification, the feature switches, `sg-people@2026.1` (annual and
sick leave, public holidays 2026 and 2027, childcare leave, salary timing, records and retirement ages, all researched) and the company policy
checked against it. The approvals primitive, the restricted-field framework and documents per person exist. The line manager's scope reads the members'
reporting line. The outbox and the worker exist; notifications do not, so a notice is queued and logged until they do.

## Goals / Non-Goals

**Goals:** working patterns; leave types, entitlements and an append-only ledger; apply, approve, calendar; sick leave notified; medical certificates;
government-paid schemes with claims; flexible-work requests; the inputs payroll and the books take from leave.

**Non-Goals:** the pay run, the payslip and the CPF rules (`p1-nemura-payroll`); the Fyniq link and one sign-in (`-fyniq-link`); letters and KETs
(`-self-service-letters`); clocking, rosters and timesheets (NM-TIM-03, P3); several countries (P3).

## Decisions

- **D1. The ledger is the balance.** `leave_ledger` is append-only: each entry has a person, a type, a leave year, a kind (grant, accrual, reservation,
  use, release, expiry, encashment, transfer, correction), a signed amount in the type's unit, its source (a request, a pack rule, a policy row) and its
  audit event. A balance is a sum over entries; "entitled, taken, booked, left" are four sums. A correction adds entries and never edits one. The
  database refuses an update or a delete and an entry that would take a type that does not allow it below zero.
- **D2. One transaction for the check and the reservation.** Applying locks the person-and-type's ledger head (a row per person and type), reads the
  balance, refuses beyond it, and inserts the reservation in the same transaction, so two requests cannot both take the last day. A test runs them at once.
- **D3. Days are counted by the person's pattern and the worksite's calendar.** A request is a set of chargeable intervals; for a type that counts working
  days the count skips the person's rest days and the public holidays of the pack, with the rest-day substitution of Appendix A (a holiday on a rest day
  makes the next working day the holiday); a type that counts calendar days counts them all (a 16-week maternity block is 112 days). The counting basis
  is part of the type (NM-LV-11). Dates beyond the loaded calendar answer "holidays not yet published" and are recounted when they are (NM-PACK-05).
- **D4. Types come from two places.** A statutory type's entitlement is read from the pack by function (`pack.AnnualLeaveDays`, `SickLeave`,
  `FamilyLeave`); a company type is a policy row. The company's policy is already checked against the statute; the ledger grants the greater of the two
  for the people the statute covers (NM-PPL-08) and the policy alone for the rest.
- **D5. Sick leave is notified.** A notification records the day, the certificate (a restricted document of `p1-nemura-records-packs`) and the 48-hour
  notice; outpatient and hospitalisation share one ceiling and the step-up date is the date of absence. HR verifies the certificate; verifying records
  who and when and never edits the certificate.
- **D6. Approval uses the manager's scope.** The line manager approves their reports' requests (the members' reporting line); HR is notified or approves
  where the type says so; a refusal needs a reason; every decision is audited. A manager never sees the leave type of someone who is not their report;
  the team calendar shows "Away".
- **D7. Government-paid schemes keep their own rules, from the pack.** Each scheme (maternity, paternity, shared parental, childcare, extended
  childcare, adoption) reads its unit, period, split and cap from `sg-people@2026.2`; the government-paid portion and the reimbursement claim are
  records with their deadlines; the claim is filed by a person on the portal, so Nemura tracks it and keeps the evidence (no API exists).
- **D8. Honest scope.** Leave into pay, the month-end accrual and Part IV hours are built as inputs and journals the payroll and the link consume; the
  requirement is tagged when its consumer exists (the rule of `p1-nemura-platform` D13). The accrual is a draft for approval, never posted by Nemura.
- **D9. The pack grows by a version, not an edit.** The maternity, paternity, shared parental, infant care, Part IV and KETs values are read from MOM and
  MSF into `sg-people@2026.2`, researched, with their sources and pinned cases; the company decides on each researched value on first use (owner
  decision O2 of `p1-nemura-records-packs`), and the publishers verify them before the first paid person (CTL-NM-07).

## Owner decisions

### O1. Default to confirm: leave not taken at the end of the year

The leave year is the calendar year, prorated (decided). What happens to unused annual leave at year end is not: carry-forward, encashment or expiry are
company policy (NM-LV-02), and a Part IV employee may carry unused leave forward 12 months (MOM).

| Option | Pros | Cons |
|---|---|---|
| **A. Carry forward for 12 months, then expire, for everyone; encashment off** | Matches the statutory right for Part IV and is the kindest default; the month-end accrual (decided on) then has a clear base | A small liability grows; the company must set a different rule if it wants one |
| **B. Expire at year end unless the policy says otherwise** | No liability; simple | Below the statutory right for Part IV employees, which the policy check would refuse |
| **C. No default: the company must choose before leave is switched on** | No assumption | Friction on first use |

Recommendation: **A**, as the default of the policy form; the company changes it there. It blocks nothing.

**Decided 20 Nov 2026 (owner away; coordinator's standing instruction: take the recommended option for small and medium choices): A.** Built as
`leave.DefaultCarryMonths` (12): unused annual leave is carried into the next year as a transfer with its last day, days taken in the new year use the carried
days first, and the rest lapses as an expiry; encashment is off. The policy form's carry-forward and encashment fields are left to a later slice, and
changing the default is then a policy change, not a code change. Why: it matches the statutory right of a Part IV employee, so the policy check never has to
refuse it, and it gives the month-end accrual a clear base.

## Risks / Trade-offs

- [A statutory count is wrong] → counts are pure functions over pack values with hand-pinned cases; a researched value needs a recorded decision; the
  ledger shows its calculation on request.
- [Two requests take the last day] → the lock and the reservation are one transaction, proved by a concurrent test.
- [A medical detail leaks] → the certificate is a restricted document and the calendar never shows a type to people without the right.
- [A rest-day holiday is mis-counted] → pinned cases for each branch of Appendix A's observance rule, including the 2026 and 2027 dates MOM lists.

## Migration plan

Additive migrations; no data to migrate. The pack's second version is added beside the first; the first is never edited.
