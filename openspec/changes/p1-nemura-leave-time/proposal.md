# Proposal

## Why

Nemura holds people, their classification, the Singapore pack and a company policy checked against it (`p1-nemura-records-packs`). It does not yet let a
person take leave. Leave is the first thing most people do in Nemura and the first place a statutory mistake costs somebody a day: the right number of
days for the year of service, sick leave that steps up at 3, 4, 5 and 6 months and shares one ceiling with hospitalisation leave, a public holiday that
moves for a rest day, a child's age and citizenship deciding a family scheme, and a ledger in which two requests cannot both take the last day. This
change builds leave and working patterns on the people record and the packs, without a pay run: payroll and the Fyniq link take leave from here.

Timing (PRD-002 question 12, decided 16 Oct 2026): built after Fyniq's go-live and before the first paid person; none of it is a launch gate. The leave
year is the calendar year, prorated (question 3, decided 16 Oct 2026); the month-end leave liability accrual is on and built in P1 (question 4).

## What Changes

- **Working patterns** (NM-TIM-01): hours and days per person, effective-dated, that leave counts days by and Fyniq later reads for available hours.
- **Leave types, entitlements and the ledger** (NM-LV-01, 02, 11, 13): types from the pack and from company policy, each with its eligibility, unit, paid
  or unpaid, who pays and how it is counted; a per-person, per-type ledger of grants, accruals, reservations, uses, releases, expiries, encashments,
  transfers and corrections, with the balance check and the reservation in one transaction.
- **Applying and approving** (NM-LV-03, 04, 05): apply in under a minute with the balance after the request, public holidays and rest days excluded
  where the type counts working days, the clash warning, the line manager's approval with a reason for a refusal, and the team calendar that shows
  "Away" to people without the right to see the type.
- **Sick leave** (NM-LV-06, 12): notified, not applied for; one ceiling for outpatient and hospitalisation leave, stepping up on the date of absence;
  the medical certificate kept as a restricted document and verified by HR.
- **Government-paid leave and flexible work** (NM-LV-07, 10, 14): each scheme keeps its own pack rules, the government-paid portion and the employer's
  reimbursement claim with its deadline (filed on the portal by a person), and flexible-work requests with their two-month reply deadline.
- **Into pay and the books** (NM-LV-08, 09, NM-TIM-02, 04): unpaid leave and encashment as pay-run inputs, the month-end leave liability accrual for
  approval, and Part IV overtime and hours worked without clocking. These three need payroll or the link to prove in full; the inputs and the accrual
  are built here and handed over (design D8).
- **Screens:** `nm-apply-leave`, `nm-my-leave`, `nm-team-leave`, `nm-report-sick`, `nm-govt-claims`, `nm-people-home` (the leave tiles).

## Capabilities

### New Capabilities

None.

### Modified Capabilities

None (`skip_specs: true`). It builds requirements already in the main specs: `people/leave` (NM-LV-01 to 14) and `people/time` (NM-TIM-01, 02, 04).

## Impact

- **Code:** `internal/nemura/{leave,patterns,calendar,claims}`; `internal/adapters/postgres/nemura`; `db/nemura/migrations/` from the next number;
  `contracts/openapi/nemura/`; `apps/nemura-web`; a second version of the pack (`sg-people@2026.2`) with the leave schemes of Appendix A.
- **Data:** leave ledger rows, working patterns, requests, certificates (restricted documents), claims; the data inventory and the retention schedule
  gain each record type.
- **Docs:** user-guide and knowledge-base pages for the leave screens.
- **Needs the owner:** none blocking; design O1 is a default to confirm. It depends on `p1-nemura-records-packs` (the people record, the pack, the policy).
