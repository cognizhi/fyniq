# Tasks

Each numbered group is one pull request (a "slice"), in this order, and each ends with the gates green: `make check`, `make test` and, for an image
change, `make images`. A slice never leaves a test red or a requirement tagged that its tests do not fully prove. `[live-verify]` marks what only the
cloud or the government portal can show. Slices 1 to 3 need no owner decision; O1 in design.md is a default the policy form takes.

Load before each slice: `fyniq-feature-change`, `fyniq-sg-statutory` (1, 3), `fyniq-api-integrations`, `fyniq-design-system` and `fyniq-knowledge-base`
(every screen), `fyniq-security-compliance` (2), `fyniq-release` (a PRD revision).

## 1. Working patterns, leave types and the ledger (slice 1)

- [x] 1.1 `sg-people@2026.2`: maternity, paternity and shared parental leave from MOM and MSF (weeks, who pays, the reimbursement cap, the claim window), each
  with its source, quote and pinned cases, keyed by the child's birth date; researched; the first version kept. `// req: NM-PACK-06` once Appendix A is covered.
  Left: unpaid infant care leave (MOM's page was not found at the address tried on 20 Nov: find it), adoption leave, Part IV and the key employment terms,
  in `sg-people@2026.3`; extended childcare is in 2026.1 already.
- [x] 1.2 Migration 000015: `working_pattern` (effective-dated weekdays and hours), `leave_ledger` (append-only, D1) and the per-person-type-year head row for
  the lock (D2, taken through `lock_leave_head`); forced RLS, audit, goldens; the worker holds no write grant yet. Tests: no edit, delete or truncate, no
  negative balance for a type that forbids it, ten requests at once for the last day book one. `// req: NM-LV-13, NM-TIM-01`
  Left: `leave_type` as a table (a company's own types) arrives with the policy form; annual leave is the one type until then.
- [x] 1.3 Entitlement and balance (NM-LV-02) for annual leave: the grant from the pack and the policy by year of service and the legal classification,
  MOM's completed-months proration in the first and last year, carry-forward and expiry at O1's default (12 months), the sums and the calculation shown with its
  sources; counting basis in the view. `// req: NM-LV-01, NM-LV-02`
  Left: the policy form's carry-forward and encashment fields (O1's default is the code's constant until then); childcare and parental leave grants
  by dependants (slice 3); NM-LV-11 for the other types.
- [x] 1.4 API (`getLeave`, `getMyLeave`, `setWorkingPattern`, `grantAnnualLeave`, `carryForwardLeave`, `NM_FEATURE_OFF`) and the screen `nm-my-leave`, guide
  and kb pages; the data inventory row.
- [x] 1.5 Verify: local gates for the touched packages, `make check`, `make api-check`, `make db-lint`, web typecheck and unit tests.

## 2. Applying, approving, the calendar and sick leave (slice 2)

- [x] 2.1 Apply (NM-LV-03): the chargeable days by pattern and calendar (D3: holidays observed on the next working day, half days, calendar counting for
  blocks), the balance after the request, who in the team is away, the refusal beyond the balance, the reservation in one transaction (D2) with a concurrent test;
  migration 000016 (`leave_request`, evidence, one way through its states, no overlap). `// req: NM-LV-03`
  Left: the attachment (only sick leave has one, 2.4); the half day is first and last day only; types other than annual (slice 3 and company types).
- [x] 2.2 Approval (NM-LV-04, D6): the line manager for reports, HR officer, HR administrator and Owner for anyone, never one's own (database check), a reason
  for a refusal, every decision audited, a notice queued (logged until notifications deliver); taken leave settled by the people team (`POST /v1/leave/settlement`).
  Left: HR approving where a type needs it (no such type yet); the worker job that settles (slice 4); notice delivery.
- [x] 2.3 The team calendar (NM-LV-05): per month, each person's holidays as their pattern observes them, "Away" for those without the right to the type, sick
  leave as such only to the person and the HR officer and administrator; on `nm-team-leave` and `nm-my-leave`, with `nm-apply-leave`'s "away in your team".
  Left: filters by team (the view is scoped by role and by manager or department); holidays per worksite (Singapore only).
- [x] 2.4 Sick leave (NM-LV-06, 12, D5): notified, one shared ceiling, the step-up by date of absence, the unpaid days recorded, the 48-hour flag, the
  certificate as a restricted document the HR officer and administrator see, HR's verification (once, never one's own); migration 000017; screen
  `nm-report-sick`. `// req: NM-LV-06, NM-LV-12`
  Left: the consultation-fee reimbursement route (FR-AP-17 or manual) and the sick-leave fee check (slice 3's claims); notice delivery.
- [x] 2.5 Verify as slice 1, plus the attack tests: a manager reading a non-report's leave type and a report's sick leave, a stranger's read of a request, ten
  requests on the last days, a person deciding or verifying their own, the type hidden in the calendar from teammates.

## 3. Government-paid leave and flexible work (slice 3)

- [x] 3.1 Per-scheme rules from the pack (NM-LV-14, D7) for maternity, paternity and shared parental leave: the rule for the child's birth date (the employer's and
  the Government's weeks, the caps, the claim window), the child's citizenship and the person's service before the birth, the scheme's window and weeks, a continuous
  block counted in calendar days, never overlapping other leave (migration 000018). `// req: NM-LV-14`
  Left: the shared parental split between the parents and its changes; childcare and extended childcare (their caps are not in the pack yet: `sg-people@2026.3`);
  adoption leave; ledger entries for the schemes' entitlements (NM-LV-13), which wait for a way to carry a block over a year end.
- [x] 3.2 The government-paid portion and the employer's reimbursement claim with its deadline (NM-LV-07): claims with amount, day and the portal's reference, partial
  and repeated claims, receipts and rejections as separate records, the guide maximum from the caps, N.1 #10; screen `nm-govt-claims`. The portal filing is a person's,
  `[live-verify]`. Left: the claim's evidence file (the restricted document framework takes it when the screen asks for one).
- [x] 3.3 Flexible work requests (NM-LV-10): a formal request with its receipt date and the reply deadline the pack gives (`sg-people@2026.3`, two months,
  clamped to the month's end), the written reply once and by someone other than the person, a reason for a refusal or an alternative, withdrawal while it waits;
  N.1 #5; migration 000019; screens `nm-my-requests` and the replies on `nm-team-leave`. `// req: NM-LV-10`
  Also in `sg-people@2026.3`: unpaid infant care leave (12 days a year per parent, 3 months' service, a citizen child under 2, at most 24 days per child), read from
  MOM on 20 Nov 2026. Left: infant care leave as a ledger type with its entitlement (it needs the child's age at each day), and letter requests on `nm-my-requests`.
- [~] 3.4 N.1 #4 (a negative balance, a request waiting more than 3 working days) is built, with #10; the people home's leave tiles (`nm-people-home`) are left.

## 4. Into pay and the books (slice 4)

- [x] 4.1 Unpaid leave and encashment as pay-run inputs (NM-LV-08): the days of a sick notification beyond the ceiling decided once as unpaid or taken from annual
  leave; encashment taking days from the ledger (never beyond the balance) with its pay input; each input linked to its source, listed for the pay run
  (`GET /v1/pay-inputs`, NM-PAY-02) and used once (migration 000020). Built as the inputs; proved with payroll. Left: the correction of leave an approved pay run
  already used becoming an input of the next run (NM-PAY-09: it needs the pay run); a screen for the decision and for encashment; unpaid leave requests as a type.
- [x] 4.2 The month-end leave liability accrual as a draft for approval (NM-LV-09): the carried-forward annual leave still untaken per person, drafted once a month,
  approved once by someone other than the drafter, never edited (migration 000021); available hours for Fyniq (`GET /v1/people/{id}/availability`: working days by
  pattern less holidays less leave, sick leave and government-paid leave). Proved with the link. Left: the amounts (the pay structure's), the journal and its export
  (the link's), the accountant's materiality floor, the scheduled monthly run (a worker job with its own grants), a screen.
- [x] 4.3 Part IV overtime and hours worked, rest-day and public-holiday work, without clocking (NM-TIM-02, NM-TIM-04): recorded per day, approved by someone other
  than the person, used once by a pay run (migration 000020); the monthly limit (72) and the 1.5 times rate from `sg-people@2026.4` (MOM's page, researched);
  work beyond the limit stays recorded and is flagged. Left: the basic-salary thresholds check (salary is the pay structure's), rest-day pay by branch and public-
  holiday pay (payroll), the N.1 finding for work beyond the limit (a PRD row to add), the screens, importing the hours.
- [x] 4.4 Verify: the local gates for each slice. Tags are only on what its tests fully prove; the requirements that need payroll or the link (NM-LV-08, NM-LV-09,
  NM-TIM-02, NM-TIM-04) are built as inputs and drafts, and their coverage rows say what waits.

## Coverage

| Requirement | Built in | Status | Left to |
|---|---|---|---|
| NM-LV-01 Leave types | 1.3, 3.1 | annual, sick (outpatient and hospitalisation), maternity, paternity and shared parental; childcare, extended childcare, infant care and adoption leave are in the pack or left | the ledger types for childcare and infant care, adoption (`sg-people@2026.5`), company types with the policy form; other countries: P3 |
| NM-LV-02 Entitlement and balance | 1.3 | annual leave: grant, proration, carry-forward at the O1 default, the four sums | the policy form's carry-forward and encashment fields |
| NM-LV-03 Apply | 2.1 | complete | |
| NM-LV-04 Approval | 2.2 | complete (the notice is queued; delivery with notifications) | |
| NM-LV-05 Team calendar | 2.3 | complete | |
| NM-LV-06 Medical certificates | 2.4 | complete | |
| NM-LV-07 Government-paid leave | 3.2 | complete except the portal filing | the portal filing: a person's, `[live-verify]` |
| NM-LV-08 Leave into pay | 4.1 | the inputs | the pay run that uses them: `p1-nemura-payroll` |
| NM-LV-09 Leave into the books | 4.2 | the days of the draft accrual, its approval, and available hours | the amounts, the journal and its export: `p1-nemura-fyniq-link` and `p1-nemura-payroll` |
| NM-LV-10 Flexible work requests | 3.3 | complete | |
| NM-LV-11 Counting basis | 1.3, 2.1 | complete | |
| NM-LV-12 Sick leave rules | 2.4 | complete | |
| NM-LV-13 Leave ledger | 1.2, 2.1 | complete for annual and sick leave | ledger entries for the government-paid schemes |
| NM-LV-14 Government-paid leave per scheme | 3.1 | maternity, paternity and shared parental, with windows, weeks, caps and deadlines | shared parental split and its changes; childcare and extended childcare caps in the pack |
| NM-TIM-01 Working patterns | 1.2, 4.2 | complete, with available hours as an API | Fyniq reading it: `p1-nemura-fyniq-link` |
| NM-TIM-02 Overtime for Part IV | 4.3 | the records, the rate and the limit from the pack | the salary thresholds check and the pay: `p1-nemura-payroll` |
| NM-TIM-04 Hours worked and special days | 4.3 | the records | the pay run: `p1-nemura-payroll` |
| Screens nm-apply-leave, nm-my-leave, nm-team-leave, nm-report-sick, nm-govt-claims, nm-people-home | 1.4, 2.3, 2.4, 3.2, 3.4 | built, except the leave tiles of nm-people-home | the people home (slice 3.4 left) |
