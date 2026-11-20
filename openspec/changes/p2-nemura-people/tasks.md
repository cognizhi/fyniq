# Tasks

Nemura's migrations are in `db/nemura/migrations` (continue from the highest number at the time). Tick a task only when fully built; add a "Built:" note and a "**Left:**" note. Each task names the requirement and the test.

## 1. Letter verification (NM-LTR-05)
- [ ] 1.1 A verification code printed on each issued letter and a public, rate-limited operation answering valid, revoked or not found; the employee revokes. `// req: NM-LTR-05`
- [ ] 1.2 The page `nm-verify-letter`, help keys, guide page and kb page. `// req: QA-UI-05`

## 2. Grievances (NM-SEC-06)
- [ ] 2.1 Inquiries with restricted access, routing away from a handler who is the subject, a written outcome; data-inventory class, Appendix B row, attack tests. `// req: NM-SEC-06`

## 3. Onboarding plans (NM-ONB-01)
- [ ] 3.1 Templates and plans with tasks for HR, the manager, IT and the joiner, due dates relative to the start date. `// req: NM-ONB-01`

## 4. Hiring (NM-REC-01 to 06, NM-AI-05)
- [ ] 4.1 Requisitions, approval, the plan flag when linked. `// req: NM-REC-01`
- [ ] 4.2 Job posts, the careers page, fair-wording checks. `// req: NM-REC-02, NM-REC-03`
- [ ] 4.3 Pipeline, interview kits, consent and retention (N.1 #11). `// req: NM-REC-04, NM-REC-05`
- [ ] 4.4 Offer to hire: the person record, the onboarding plan, the pass application date. `// req: NM-REC-06`
- [ ] 4.5 The assistant's `people.draft_job_post` and `people.draft_interview_kit`, and the fairness red-team cases. `// req: NM-AI-05`

## 5. Performance (NM-PRF-01 to 05)
- [ ] 5.1 Goals and KPIs. `// req: NM-PRF-01, NM-PRF-02`
- [ ] 5.2 Check-ins. `// req: NM-PRF-03`
- [ ] 5.3 Review cycles and `people.draft_review_summary`. `// req: NM-PRF-04`
- [ ] 5.4 Pay outcomes through the pay path. `// req: NM-PRF-05`

## 6. Mixed use (NM-EXT-04, NM-EXT-05)
- [ ] 6.1 Pay documents imported from an external payroll with source, version, as-of date and last refresh; salary letters state the as-of date. `// req: NM-EXT-05`

## Coverage

| Requirement | Built in | Status |
|---|---|---|
| NM-LTR-05 | 1 | planned |
| NM-SEC-06 | 2 | planned |
| NM-ONB-01 | 3 | planned |
| NM-REC-01…06, NM-AI-05 | 4 | planned |
| NM-PRF-01…05 | 5 | planned |
| NM-EXT-04, NM-EXT-05 | 6 | planned |
