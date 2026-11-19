# Tasks

Each numbered group is one pull request (a "slice"), in this order, and each ends with the gates green: `make check`, `make test` and, for an image
change, `make images`. A slice never leaves a test red or a requirement tagged that its tests do not fully prove. `[live-verify]` marks what only the
cloud or the government portal can show. Slices 1 to 3 need no owner decision; O1 in design.md is a default the policy form takes.

Load before each slice: `fyniq-feature-change`, `fyniq-sg-statutory` (1, 3), `fyniq-api-integrations`, `fyniq-design-system` and `fyniq-knowledge-base`
(every screen), `fyniq-security-compliance` (2), `fyniq-release` (a PRD revision).

## 1. Working patterns, leave types and the ledger (slice 1)

- [ ] 1.1 `sg-people@2026.2`: maternity, paternity, shared parental, infant care, extended childcare and adoption leave from MOM and MSF (days or weeks,
  who pays, the reimbursement cap, the claim window), each with its source, quote and pinned cases; researched. `// req: NM-PACK-06` once Appendix A is covered.
- [ ] 1.2 Migration: `working_pattern` (effective-dated hours and days per week), `leave_type` (from the pack by key, or the company's), `leave_ledger`
  (append-only, D1) and the per-person-and-type head row for the lock (D2); forced RLS, audit, goldens. Tests: no edit or delete, no negative
  balance for a type that forbids it. `// req: NM-LV-13, NM-TIM-01`
- [ ] 1.3 Entitlement and balance (NM-LV-02): grants and accruals from the pack and the policy by year of service and the legal classification, proration
  in the first and last year, carry-forward and expiry (O1), the four sums and the calculation on request; counting basis per type (NM-LV-11).
  `// req: NM-LV-01, NM-LV-02, NM-LV-11`
- [ ] 1.4 API and the screen `nm-my-leave`, guide and kb pages; the data inventory and retention rows.
- [ ] 1.5 Verify: `make check`, `make test`, `make openapi`, `make api-check`.

## 2. Applying, approving, the calendar and sick leave (slice 2)

- [ ] 2.1 Apply (NM-LV-03): the chargeable intervals by pattern and calendar (D3), the balance after the request, the clash warning, the refusal beyond the
  balance, the reservation in one transaction (D2) with a concurrent test. `// req: NM-LV-03`
- [ ] 2.2 Approval (NM-LV-04, D6): the line manager, HR where the type needs it, a reason for a refusal, every decision audited, a notice queued.
- [ ] 2.3 The team calendar (NM-LV-05): per month, holidays per person, "Away" for those without the right; screens `nm-apply-leave`, `nm-team-leave`.
- [ ] 2.4 Sick leave (NM-LV-06, 12, D5): notified, one ceiling, the step-up by date of absence, the certificate as a restricted document, HR's verification;
  screen `nm-report-sick`. `// req: NM-LV-06, NM-LV-12`
- [ ] 2.5 Verify as slice 1, plus the attack tests: a manager reading a non-report's leave type, a second request on the last day.

## 3. Government-paid leave and flexible work (slice 3)

- [ ] 3.1 Per-scheme rules from the pack (NM-LV-14, D7): shared parental split and its changes, the unit and period, the cap; the dependants of
  `p1-nemura-records-packs` decide eligibility. `// req: NM-LV-14`
- [ ] 3.2 The government-paid portion and the employer's reimbursement claim with its deadline and evidence (NM-LV-07); N.1 #10; screen `nm-govt-claims`.
  The portal filing is a person's, `[live-verify]`.
- [ ] 3.3 Flexible work requests with the two-month reply deadline and the written reply (NM-LV-10); N.1 #5 for this part.
- [ ] 3.4 N.1 #4 (a negative balance, a request waiting more than 3 working days) and the people home's leave tiles (`nm-people-home`).

## 4. Into pay and the books (slice 4)

- [ ] 4.1 Unpaid leave and encashment as pay-run inputs (NM-LV-08) and the correction that becomes an input to the next run (NM-PAY-09): built as
  the inputs; proved with payroll.
- [ ] 4.2 The month-end leave liability accrual as a draft journal for approval (NM-LV-09); available hours for Fyniq. Proved with the link.
- [ ] 4.3 Part IV overtime and hours worked, rest-day and public-holiday work, without clocking (NM-TIM-02, 04): recorded per day, approved, used once by a
  pay run; the monthly overtime limit and the 1.5 times rate from the pack.
- [ ] 4.4 Verify, and tag only what is fully proven.

## Coverage

| Requirement | Built in | Status | Left to |
|---|---|---|---|
| NM-LV-01 Leave types | 1.3 | complete for the Singapore types | other countries: P3 |
| NM-LV-02 Entitlement and balance | 1.3 | complete | |
| NM-LV-03 Apply | 2.1 | complete | |
| NM-LV-04 Approval | 2.2 | complete (the notice is queued; delivery with notifications) | |
| NM-LV-05 Team calendar | 2.3 | complete | |
| NM-LV-06 Medical certificates | 2.4 | complete | |
| NM-LV-07 Government-paid leave | 3.2 | complete except the portal filing | the portal filing: a person's, `[live-verify]` |
| NM-LV-08 Leave into pay | 4.1 | the inputs | the pay run that uses them: `p1-nemura-payroll` |
| NM-LV-09 Leave into the books | 4.2 | the draft journal | posting and available hours: `p1-nemura-fyniq-link` |
| NM-LV-10 Flexible work requests | 3.3 | complete | |
| NM-LV-11 Counting basis | 1.3, 2.1 | complete | |
| NM-LV-12 Sick leave rules | 2.4 | complete | |
| NM-LV-13 Leave ledger | 1.2, 2.1 | complete | |
| NM-LV-14 Government-paid leave per scheme | 3.1 | complete | |
| NM-TIM-01 Working patterns | 1.2 | complete | available hours in Fyniq: `p1-nemura-fyniq-link` |
| NM-TIM-02 Overtime for Part IV | 4.3 | the records and the rates | the pay run: `p1-nemura-payroll` |
| NM-TIM-04 Hours worked and special days | 4.3 | the records | the pay run: `p1-nemura-payroll` |
| Screens nm-apply-leave, nm-my-leave, nm-team-leave, nm-report-sick, nm-govt-claims, nm-people-home | 1.4, 2.3, 2.4, 3.2, 3.4 | complete | |
