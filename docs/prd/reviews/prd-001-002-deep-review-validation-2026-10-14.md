# PRD-001 and PRD-002 second deep review: validation and dispositions · 14 Oct 2026

| | |
|---|---|
| Review validated | *Fyniq and Nemura: second deep PRD review* of 14 Oct 2026 (51 findings F2-01…13, X2-01…14, N2-01…24; 12 opportunities O2-01…12; 34 acceptance scenarios A2-01…34), of PRD-001 revision 0.15 (SHA-256 `025a3d24…54467e`) and PRD-002 revision 0.1 (SHA-256 `425ada88…719dde`), commit `cde261b`. The report and its register were supplied as local files and are not kept in the repository |
| Result | PRD-002 revision **0.2** and PRD-001 revision **0.16** apply every finding that held, in the form recorded below; the OpenSpec specs and the screens follow |
| Validator | Claude Code (Opus 5.5), at the founders' request: "analyse the report against our PRD and enhance further if it's strongly justified and validated" |
| Method | Each finding split into its claims. PRD claims were checked against the text by line. External facts were checked on the official page (MOM, MSF, CPF Board, OpenAI, PDPC) on 14 Oct 2026. Pages were read through a fetch-and-summarise tool, not captured raw, so the pack publisher's own verification (NM-PACK-07) is still real work. The PDPC guidance was read from the PDF the reviewer supplied (pp. 42–47). The whole of both PRDs was searched for text that already covered a point, and the 13 Oct validation (F01–F50) for duplicates. Design-inference findings were accepted only where they close a money, statutory, privacy or access gap and have a clear home in the PRD |

## 1. Verdict

The review is **sound and adopted almost entirely**. Its citations of both PRDs are accurate: every
quoted gap, contradiction and wrong reference was found where it said. The statutory facts it relies
on match the official pages in every case checked, and four of them correct statements PRD-002
revision 0.1 made from secondary sources.

- **Accepted as written (A):** 43 findings.
- **Accepted with a modified solution (A·m):** 7. X2-04 keeps PRD-001's existing correction model
  (B.3 posts the employee-month difference) instead of choosing between delta and replacement.
  N2-06 separates the publisher's verification from the customer's acceptance, and lets a recorded
  manual decision stand in for an unverified value. N2-23 makes payslip explanations deterministic
  by default, with an opt-in own-pay route. F2-05 adds a stale limit only for restricted data
  classes. F2-13 does not assert which accounting standard applies (the framework is the
  accountant's decision, FR-ORG-10). N2-16's lawful-basis analysis is recorded as DPO-to-confirm,
  because the PDPC guidance supplied is the 2021 revision. N2-20 names the special-day pay branches
  but keeps their rates [verify], because the pages read did not state them.
- **Partly accepted (P):** N2-24 (launch economics and the combined launch subset are founder
  decisions; the PRD gains open questions and a pay-day target, not new scope).
- **Rejected:** none.

Counts reproduce: PRD-002 has 98 NM-* rows (the review: 98). Rows that are M and name P1 are 75 by a
plain table count, 73 in the review; the difference is split-phase rows such as NM-PACK-03 and
NM-ONB-03, and does not change the conclusion that Nemura is a material addition to P1.

Missed by the reviewer and fixed in passing: PRD-002 Appendix A said annual leave is 7 days "after
the first year"; MOM gives 7 days **in** the first year once 3 months are served, prorated by
completed months with halves rounded up. Appendix A said paternity leave became "mandatory for
employers"; MSF's page describes an entitlement and a default block, so that word keeps [verify].
MOM confirms the 2026 and 2027 holiday dates, so the country-pack screen's "to confirm" on them
(added on 13 Oct) is lifted.

## 2. Official facts checked on 14 Oct 2026

| Fact | Official page | Result | Effect |
|---|---|---|---|
| Maternity leave: a continuous block includes non-working days, rest days and public holidays; the flexible portion (last 8 of 16 weeks, or last 4 of 12, by agreement, within 12 months) does not | [MOM](https://www.mom.gov.sg/employment-practices/leave/maternity-leave/planning-your-leave) | Confirmed (N2-01) | NM-LV-11; NM-LV-03 changed |
| Sick leave: 5/15, 8/30, 11/45, 14/60 days at 3, 4, 5, 6+ months; the 60 hospitalisation days include the 14 outpatient days; inform or try to inform the employer within 48 hours; certified by a registered practitioner; MC on return | [MOM](https://www.mom.gov.sg/employment-practices/leave/sick-leave/eligibility-and-entitlement) | Confirmed (N2-03) | NM-LV-12; Appendix A |
| Public holiday on a **rest day**: the next working day is a paid holiday; on a **non-working day**: another day off or an extra day's pay | [MOM FAQ](https://www.mom.gov.sg/faq/public-holidays/is-an-employee-entitled-to-another-day-off-if-a-public-holiday-falls-on-a-rest-day-or-non-working-day) | Confirmed (N2-21); PRD-002 had it wrong | Appendix A corrected; NM-LV-11 |
| 2026 and 2027 public holidays, including Deepavali Sun 8 Nov 2026 (Mon 9 Nov "if your rest day falls on" the Sunday) and Vesak Day Thu 20 May 2027 | [MOM calendar](https://www.mom.gov.sg/employment-practices/public-holidays) | Confirmed | Appendix A without [verify]; screens |
| Employment records: current employees, the latest 2 years; former employees, the last 2 years, kept for 1 year after leaving | [MOM](https://www.mom.gov.sg/employment-practices/employment-records) | Confirmed (N2-17) | Appendix A; NM-SEC-04 |
| Mandatory retrenchment notification: employers with at least 10 employees, within 5 working days after the employees are told | [MOM](https://www.mom.gov.sg/employment-practices/retrenchment/mandatory-retrenchment-notifications) | Confirmed (N2-08) | Appendix A; NM-PAY-08 |
| Shared parental leave: 10 weeks (born from 1 Apr 2026), 6 weeks (1 Apr 2025–31 Mar 2026); half each by default; changes in the first 4 weeks without the employer's paper, later with its written agreement; within 12 months; without agreement one continuous block within 26 weeks; reimbursement capped at S$2,500 **per week**; employer claims within 3 months after the last SPL day | [MSF](https://www.profamilyleave.msf.gov.sg/schemes/shared-parental-leave) | Confirmed (N2-07) | NM-LV-14; Appendix A |
| Paternity leave: 4 weeks GPPL from 1 Apr 2025; by agreement continuous or not within 12 months; cap S$2,500 a week, up to S$10,000; claim within 3 months after the last day. "Mandatory for employers" is **not** on the page | [MSF](https://www.profamilyleave.msf.gov.sg/schemes/paternity-leave) | Partly confirmed | Appendix A keeps [verify] on "mandatory" |
| Childcare: GPCL 6 days a year per parent for a Singapore Citizen child under 7, regardless of the number of children, 42 days per child in total; employer pays days 1–3, the government reimburses days 4–6 (S$500 a day, S$1,500 a year); ECL 2 days for ages 7–12, both reimbursed (S$500 a day, S$1,000 a year); claim within 3 months after the calendar year. Non-citizen child: 2 days a year (Employment Act), employer-paid, 14 days per child | [MOM](https://www.mom.gov.sg/employment-practices/leave/childcare-leave/eligibility-and-entitlement), [MSF](https://www.profamilyleave.msf.gov.sg/schemes/childcare-leave) | Confirmed (N2-02, N2-07); PRD-002 omitted the non-citizen branch | NM-PPL-07, NM-LV-14; Appendix A |
| Annual leave: from 3 months' service; 7 days in the 1st year to 14 from the 8th; first-year proration by completed months, below half rounded down, half or more up; Part IV employees may carry unused leave forward 12 months; leave on a half working day counts as a full day unless the employer chooses otherwise | [MOM](https://www.mom.gov.sg/employment-practices/leave/annual-leave/eligibility-and-entitlement), [MOM](https://www.mom.gov.sg/employment-practices/leave/annual-leave/special-situations) | Confirmed (N2-05); PRD-002's "after the first year" was wrong | Appendix A; NM-PACK-08 |
| Salary within 7 days after the salary period, overtime within 14; final salary: resigns with notice, on the last day; without notice, within 7 days; dismissal for misconduct or termination by the employer (retrenchment included), on the last day, or within 3 working days if not feasible | [MOM](https://www.mom.gov.sg/employment-practices/salary/paying-salary) | Confirmed (N2-08) | NM-PAY-08; Appendix A |
| CPF: Ordinary Wages are for the month's employment **and** payable by the 14th of the next month; everything else is Additional Wages. Commission frequency is not on the page | [CPF Board](https://www.cpf.gov.sg/employer/employer-obligations/what-constitutes-wages-for-cpf-contributions) | Confirmed (F2-02); the commission example stays [verify] | FR-PAY-17; B.3 |
| Senior workers above 55 to 65: CPF rates rose 1.5 percentage points from **1 Jan 2026** | [CPF Board](https://www.cpf.gov.sg/member/infohub/educational-resources/the-2026-cpf-retirement-planning-checklist) | Confirmed (N2-18); PRD-002's "start in 2027, not 2026" was wrong | Appendix A corrected |
| Retirement age 64 and re-employment age 69 from 1 Jul 2026, by birth cohort; citizens and PRs; 2 years' service for hires at 55 and above | [MOM](https://www.mom.gov.sg/employment-practices/re-employment) | Confirmed | Appendix A; NM-PPL-08 |
| FCF advertising applies to **Employment Pass and S Pass** applications; at least 14 consecutive days; no offer during it; EP exemptions: fewer than 10 employees, fixed monthly salary S$22,500 or more, roles of 1 month or less, local or intra-corporate transferees; a new advert when UEN, occupation, salary or vacancies change; adverts expired or closed over 3 months cannot be used | [MOM FCF](https://www.mom.gov.sg/employment-practices/fair-consideration-framework), [MOM EP](https://www.mom.gov.sg/passes-and-permits/employment-pass/consider-all-candidates-fairly) | Confirmed (N2-19); S Pass exemptions not read | NM-REC-02; Appendix A |
| A Dependant's Pass holder works for an employer on their own EP, S Pass or DP-Work Permit; a Letter of Consent is for running their own business | [MOM](https://www.mom.gov.sg/passes-and-permits/dependants-pass/working-in-singapore) | Confirmed (N2-19); NM-PPL-03 was wrong | NM-PPL-03 changed |
| Workplace Fairness Act: in force at the end of 2027; firms with fewer than 25 employees exempt "for a start", reviewed five years after | [MOM, 4 Nov 2025](https://www.mom.gov.sg/newsroom/speeches/2025/1104-round-up-speech-by-minister-for-wf) | Confirmed (N2-22) | Appendix A; NM-SEC-06 |
| PDPA: a voluntary job application gives deemed consent for assessing it; opinion data kept solely for an evaluative purpose need not be given on an access request | PDPC Advisory Guidelines on Selected Topics (rev. 4 Oct 2021) §5.2, §5.9–5.10 | Confirmed for that edition (N2-16) | NM-REC-05, NM-SEC-07; DPO to confirm the current edition |
| OpenAI: the Models list has an optional `shutdown_date`; the Singapore region offers regional **storage** but not regional **processing**; plan use goes through the SDK's `chatgpt.listModels()` on the active connection, needs the `chatgpt.tokens.use.direct` grant (identity sign-in alone grants none), and is open to open-source, local personal and selected private apps (others join a waitlist) | [Models](https://developers.openai.com/api/reference/resources/models/methods/list), [data controls](https://developers.openai.com/api/docs/guides/your-data), [plan guide](https://developers.openai.com/cookbook/articles/sign-in-with-chatgpt) | Confirmed (F2-09, F2-11) | AI-73, AI-80, AI-88 |
| Key employment terms: in writing within 14 days of the start | [MOM FAQ](https://www.mom.gov.sg/faq/key-employment-terms/why-must-i-issue-written-kets-within-14-days-of-my-employees-first-day-of-work-instead-of-before-their-first-day-of-work) | The 14 days confirmed; who the rule covers (employment of 14 days or more, from 1 Apr 2016) not on the pages read | Appendix A gains [verify] on coverage, matching NM-LTR-06; the pack screen's KETs chip is "To confirm: who it covers" (8 of 22 topics open) |
| Unpaid infant care leave (12 days) | MOM page returned 404 | Not checked | Keeps [verify] |
| Rest-day pay rates, Part IV monthly overtime cap, maternity pay split | Pages did not state them | Not checked | Keep [verify] |

## 3. Findings

Disposition: **A** accepted, **A·m** accepted with a modified solution, **P** partly accepted, **D**
deferred with an owner. "New" lists new requirement IDs; "Changed" lists rows tagged (rev 0.2) in
PRD-002 or (rev 0.16) in PRD-001.

### Fyniq (F2)

| ID | Sev | Disp. | Validation | Applied as |
|---|---|---|---|---|
| F2-01 | High | A | Confirmed: §7.1's Payroll row (line 498) still lists employees, pay runs and payslips in Fyniq; §7.1 promises "My payslips" in Fyniq from P2; §18's Payroll row lists payslips and leave; NM-PAY-01 lumps FR-PAY-01…16 as M/P1 while FR-PAY-08/09/10 are P2 in PRD-001 | New NM-PAY-07 (delivery map: rule, where it runs, Fyniq's part, phase per FR-PAY row). Changed §7.1, §8.7 intro, §18, NM-PAY-01 |
| F2-02 | High | A | Confirmed: B.3 makes "bonus/commission" AW by name; CPF Board defines OW by two facts | New FR-PAY-17. Changed B.3 (OW, AW rows), NM-PAY-02 |
| F2-03 | High | A | Confirmed: B.3 net = gross − EE CPF − SHG and posts no deduction or reimbursement branch, while FR-PAY-16 allows deductions and FR-AP-17 allows reimbursements in the pay run. The review's arithmetic holds (balanced entry worked in the B.3 row) | New FR-PAY-18. Changed B.3 (net pay, postings). Oracle work |
| F2-04 | High | A | Design inference, sound: AI-74/75 check a route's declared classes, not what a conversation actually holds | New AI-83 |
| F2-05 | High | A·m | Confirmed: AI-80 reads "with a page cap" and AI-81 removes any model "no longer listed", so a capped or failed later page retires good models. Stale limit applied to personal and payroll classes only; other classes keep the ⚠ | New AI-84. Changed AI-81 |
| F2-06 | High | A | Confirmed: AI-82 takes prices from a moving community file; JSON numbers parsed naively are binary floats (golden rule 1); the catalogue carries tiered, cache and modality prices | New AI-85. Changed AI-82 |
| F2-07 | High | A | Design inference, sound: AI-77's "hard stop at 100%" is checked after spend | New AI-86. Changed AI-77 |
| F2-08 | High | A | Design inference, sound: AI-78 allows moving aliases with weekly re-qualification | New AI-87. Changed AI-78 |
| F2-09 | Medium | A | Confirmed on OpenAI's pages (§2). AI-73's "may offer no model list" and AI-80's "identifiers only" are out of date | Changed AI-73, AI-80 |
| F2-10 | High | A | Confirmed: AI-79 reaches self-hosted models over a private network; SEC-36 refuses private addresses | New SEC-37. Changed SEC-36, AI-79 |
| F2-11 | High | A | Confirmed on OpenAI's page: Singapore storage, no Singapore processing | New AI-88. Changed AI-75, NM-AI-01 |
| F2-12 | Medium | A | Confirmed: FR-PRJ-12 subtracts leave "once FR-PAY-12 exists" (superseded) and could subtract a holiday inside a leave interval twice | Changed FR-PRJ-12, FR-PPL-07 |
| F2-13 | High | A·m | Confirmed that "optional" accrual and the open-question default "Off" conflate automation with recognition. Whether untaken leave must be recognised depends on the framework the accountant confirms (FR-ORG-10); the PRD does not assert IAS 19 | Changed FR-PPL-07, NM-LV-09, PRD-002 §23 Q4 |

### Cross-product (X2)

| ID | Sev | Disp. | Validation | Applied as |
|---|---|---|---|---|
| X2-01 | High | A | Design gap, sound: one identity tenant does not by itself end sessions in two origins; FR-PPL-03 and NM-INT-02 promise "signing out of one signs out of both" without a mechanism | New FR-PPL-18, NM-INT-12. Changed ADR-N3, NM-ARC-04, NM-INT-02, FR-PPL-03. Passkey relying-party ID: PRD-002 §23 Q10 |
| X2-02 | **Critical** | A | Confirmed: FR-PPL-04 accepts "the pay-run approval in Nemura" as the authority, and Appendix E sends an approver name with no envelope or proof; FR-APR-10 requires an envelope rebuilt and re-checked at execution | New FR-PPL-09, NM-INT-06. Changed FR-PPL-04, Appendix E |
| X2-03 | **Critical** | A | Confirmed: SOD-4 covers "approve it or release salary payments"; NM-ROLE-04 repeats only the first half; no preparer reaches Fyniq | New FR-PPL-10, NM-INT-07. Changed NM-ROLE-04 |
| X2-04 | **Critical** | A·m | Confirmed that run/version idempotency leaves corrections undefined, and that NM-ARC-05 ("corrected by reversal") disagrees with B.3 ("posts only the difference"). Solution: keep B.3's model; each version states the cumulative totals of its wage month and the version it supersedes, and Fyniq posts the difference | New FR-PPL-11, NM-PAY-09. Changed NM-ARC-05, Appendix E |
| X2-05 | High | A | Confirmed: Appendix E returns only "posted"; FR-BNK-12 marks a batch paid from the bank feed but tells Nemura nothing | New FR-PPL-12, NM-PAY-10, N.1 #14. Changed NM-PAY-04, Appendix E |
| X2-06 | **Critical** | A | Confirmed: FR-PAY-10 holds every payment path from awareness of cessation; NM-ONB-03 owns that trigger; FR-PPL-06 and Appendix E carry no hold | New FR-PPL-13, NM-INT-08. Changed FR-PAY-10, FR-PPL-06, Appendix E |
| X2-07 | High | A | Confirmed: Appendix E's payment lines carry account details but no version; FR-APR-05 blocks a batch after a vendor bank change but has nothing to compare for salaries | New FR-PPL-14, NM-INT-09 |
| X2-08 | High | A | Confirmed: ADR-N5 and NM-INT-01 promise source ID and version; Appendix E omits versions on every event but the run | New FR-PPL-15, NM-INT-10. Changed Appendix E |
| X2-09 | High | A | Confirmed: NM-ARC-04 "unlinking stops the sync and keeps both sides' data" with no settlement of in-flight work | New FR-PPL-16, NM-INT-11 |
| X2-10 | High | A | Confirmed: salary payment lines (name, account, amount) reach Fyniq's batch, whose approvers need not hold payroll rights; NM-PRF-02 lets a financial KPI appear in Nemura while NM-ROLE-02 says never | New FR-PPL-17. Changed FR-PAY-13, NM-PRF-02 |
| X2-11 | High | A | Design gap, sound: one image, but the PRD gives no service identities, so the people assistant could share the payroll writer's credential; NM-SEC-01 names only ASVS L2 while SEC-20 requires L3 on money and approval flows | New NM-ARC-06. Changed NM-SEC-01; standalone identity components: PRD-002 §23 Q11 |
| X2-12 | Medium | A | Confirmed: NM-ARC-01 promises files "for any accounting system and bank"; NM-EXT-02 commits a P1 connector before its API is qualified | Changed NM-ARC-01, NM-EXT-02 |
| X2-13 | High | A | Design gap, sound: two databases restored independently can re-create a payment intent | New FR-PPL-19, NM-INT-13. Changed DR-06 |
| X2-14 | High | A | Confirmed: QA-TRACE-01 reads one PRD; NM-AI-04 cites QA-AI-02 (proposal evals) for red-team tests, which are QA-AI-03 | New QA-API-04. Changed QA-TRACE-01, NM-AI-04 |

### Nemura (N2)

| ID | Sev | Disp. | Validation | Applied as |
|---|---|---|---|---|
| N2-01 | High | A | Confirmed on MOM's page (§2); NM-LV-03 excludes holidays and rest days for every request | New NM-LV-11. Changed NM-LV-03 |
| N2-02 | High | A | Confirmed on MOM's and MSF's pages; the non-citizen 2-day branch and the per-child totals were missing | New NM-PPL-07. Changed NM-LV-01, Appendix A |
| N2-03 | High | A | Confirmed on MOM's page | New NM-LV-12. Changed NM-LV-06 |
| N2-04 | High | A | Design gap, sound: balances have no reservation, cancellation or correction rules | New NM-LV-13. Changed NM-LV-02 |
| N2-05 | High | A | Confirmed: NM-PACK-04 compares one number; MOM's Part IV carry-forward and half-day rules are rules, not amounts | New NM-PACK-08. Changed NM-PACK-04 |
| N2-06 | High | A·m | Confirmed: N.1 #8 only warns when a live policy uses a [verify] value; ENG-30…32 require a verifier, an approver and pinned tests for Fyniq's packs. Solution: value states (researched, verified, released), publisher verification by two people (SOD-10), the organisation's acceptance separate; a calculation needing an unverified value stops unless HR records the value it applies, with evidence | New NM-PACK-07, N.1 #13. Changed NM-PACK-02, N.1 #8 |
| N2-07 | High | A | Confirmed on MSF's pages (§2): the cap is per week, deadlines differ by scheme | New NM-LV-14. Changed NM-LV-07, Appendix A |
| N2-08 | High | A | Confirmed on MOM's pages (§2) | New NM-PAY-08, N.1 #15. Changed NM-PAY-06, NM-ONB-03, Appendix A |
| N2-09 | Medium | A | Confirmed: ADR-N2, NM-PAY-01 and NM-LV-08 cite "Appendix F's formulas"; PRD-001's Appendix F is the control registry; payroll is Appendix B.3 | Changed ADR-N2, NM-PAY-01, NM-LV-08 |
| N2-10 | High | A | Confirmed: NM-FEAT-01 says a feature that is off is "not loaded: no routes … jobs"; NM-SEC-05's deletion jobs and NM-PAY-04's leaver access would stop with it | New NM-FEAT-05. Changed NM-FEAT-01, NM-FEAT-04 |
| N2-11 | High | A | Confirmed: NM-FEAT-02 (self-service payslips need Payroll), NM-FEAT-03 (external turns Payroll off) and NM-EXT-04 (external payroll with Nemura self-service) cannot all hold | New NM-EXT-05. Changed NM-FEAT-02, NM-EXT-04 |
| N2-12 | Medium | A | Sound: a passkey approval proves who approved inside the platform, not to a bank or an embassy | New NM-LTR-07. Changed NM-LTR-03, PRD-002 §23 Q5 |
| N2-13 | High | A | Confirmed: NM-LTR-04 lets the HR administrator mark a type for signing with "the company's pre-approved signature", which is the signatory's authority | Changed NM-LTR-04 |
| N2-14 | High | A | Sound: "only the letter's own facts" can be salary or immigration status | Changed NM-LTR-05 |
| N2-15 | High | A | Confirmed: Appendix B has 8 columns for 9 roles (no Administrator, no Interviewer) and one grouped "restricted fields" row | New NM-ROLE-05. Changed Appendix B |
| N2-16 | High | A·m | Confirmed on PDPC's 2021 guidance (§2); DPO to confirm against the current edition | New NM-SEC-07. Changed NM-SEC-02, NM-REC-05 |
| N2-17 | High | A | Confirmed on MOM's page (§2) | New NM-ESS-06. Changed NM-SEC-04, NM-SEC-05, NM-PAY-04, Appendix A |
| N2-18 | High | A | Confirmed: the CPF timing error (§2); NM-PACK-03 picks rules by worksite only | New NM-PPL-08. Changed NM-PPL-01, NM-PACK-03, Appendix A |
| N2-19 | High | A | Confirmed on MOM's pages (§2) | Changed NM-PPL-03, NM-REC-02, Appendix A |
| N2-20 | High | A·m | Sound; the special-day rates were not on the pages read, so the row names the branches and keeps the rates [verify] | New NM-TIM-04. Changed NM-TIM-02 |
| N2-21 | High | A | Confirmed on MOM's FAQ and calendar (§2) | Changed Appendix A, NM-PACK-05, NM-LV-05 |
| N2-22 | Medium | A | Confirmed on MOM's speech (§2) | Changed NM-LV-10, NM-SEC-06, Appendix A, PRD-002 §23 Q9 |
| N2-23 | High | A·m | Confirmed: AI-02 sends salary only with the organisation switch **and** the payroll role; NM-AI-02 lets every employee have payslip lines explained; NM-AI-05's "never uses a protected characteristic" would forbid explaining childcare eligibility. Default: deterministic explanations, no pay figures to the model; an Owner may enable own-pay explanations | New NM-AI-06. Changed AI-02, NM-AI-02, NM-AI-05 |
| N2-24 | Medium | P | Measures without denominators and a 24-hour link alarm before pay day confirmed; economics and the launch subset are founder decisions | New NM-NFR-05. Changed NM-INT-04, §2 measures; PRD-002 §23 Q12–13 |

## 4. Opportunities O2-01…12

| ID | Disposition | Why |
|---|---|---|
| O2-01 Payroll variance view | **Adopted** as NM-PAY-11 (S, P1) | An approver can only judge a run by what changed; FR-APR-02 already requires the impact to be shown |
| O2-02 Outcome and exception console | Covered by NM-PAY-10 and N.1 #14 | The states and the check are the control; a richer console waits for use |
| O2-03 Government-paid leave claim workbench | Covered for P1 by NM-LV-14 (records, caps, deadlines, partial receipts); the receivable journal is P2 | — |
| O2-04 HR migration preflight | Partly covered by FR-PPL-16 / NM-INT-11 (year-to-date and opening state at a source change); the rest **deferred** to the migration plan (FR-MIG) | — |
| O2-05 Effective-date impact preview | **Deferred** (P2 candidate), founders | Useful; not needed for a correct P1 |
| O2-06 Policy acknowledgements | **Deferred** (P2 candidate), HR | No Singapore duty found that needs it for P1 |
| O2-07 Confidential cases with alternative routing | Adopted into NM-SEC-06 (P2) | — |
| O2-08 Bank-file preview and manifest | Partly in FR-PPL-12 (a file is never a payment confirmation); preview UI **deferred** | — |
| O2-09 Cross-product close evidence pack | **Deferred** (P2), Controller | Extends FR-RPT-13 later |
| O2-10 Headcount-to-cash bridge | **Deferred** (P2), founders | No evidence of need yet |
| O2-11 Own total-rewards statement | **Deferred** (P3 at earliest) | Close to the benefits non-goal; validate demand first |
| O2-12 Obligation readiness dashboard | Covered for P1 by NM-PACK-07 and N.1 #13 ("no all clear" while a value is unverified) | — |

## 5. Acceptance scenarios A2-01…34

They become `#### Scenario:` blocks under the new and changed requirements in `openspec/specs/`, not a
new PRD appendix, so each one tests the requirement it belongs to. The specs carry the scenarios
without their A2 numbers (the spec tooling would read `A2-07` as a requirement ID), so this list is
the only place the mapping lives: A2-01/02 → FR-PPL-18 and
NM-INT-12; A2-03 → FR-PPL-09; A2-04 → FR-PPL-10; A2-05 → FR-PAY-17; A2-06 → FR-PAY-18; A2-07/08 →
FR-PPL-11 and NM-PAY-09; A2-09 → FR-PPL-12 and NM-PAY-10; A2-10 → FR-PPL-13 and NM-INT-08; A2-11 →
FR-PPL-14; A2-12 → FR-PPL-16 and NM-FEAT-05; A2-13 → FR-PPL-19; A2-14 → NM-LV-11; A2-15 → NM-PPL-07;
A2-16 → NM-LV-12; A2-17 → NM-LV-13; A2-18 → NM-PACK-08; A2-19 → NM-PACK-07; A2-20 → NM-LV-11 and
FR-PRJ-12; A2-21 → NM-PAY-08; A2-22 → NM-LTR-04; A2-23 → NM-LTR-05; A2-24 → NM-SEC-07; A2-25 →
NM-EXT-05; A2-26 → AI-83; A2-27 → AI-84; A2-28 → AI-85; A2-29 → AI-86; A2-30 → AI-87; A2-31 →
SEC-37; A2-32 → NM-ARC-06; A2-33 → NM-ESS-06; A2-34 → FR-PPL-07.

## 6. Calls made while applying (for the founders, the accountant and the DPO to confirm)

1. **Corrections** follow B.3: every run version states the cumulative totals of its wage month,
   and Fyniq posts the difference from what it already posted for that run (FR-PPL-11). A run
   withdrawn before payment is reversed.
2. **Closed periods**: a correction to a closed month posts in the current open month, with the
   original wage month recorded; Nemura keeps the statutory wage month (FR-PPL-11).
3. **Passkeys**: both apps use one WebAuthn relying-party ID (their shared registrable domain) and
   one passkey registry, so Fyniq can verify a Nemura approval itself (PRD-002 §23 Q10).
4. **Tax-clearance hold**: Nemura is the hold authority when linked; Fyniq checks it at release and
   fails closed for the people in the batch when it cannot get an answer within 5 seconds
   (FR-PPL-13).
5. **Own-pay AI**: off by default; payslip explanations are deterministic (NM-AI-06).
6. **Leave liability**: Off stays the default for the automatic accrual, but the close needs a
   recorded policy decision and, where the policy requires recognition, a valuation (FR-PPL-07).
7. **Price quarantine**: a catalogue price that falls by more than half, or to zero or below, keeps
   the previous price until an Owner reviews it (AI-85, setting).
8. **Restricted model data**: a connection whose model list has not been read completely for 14 days
   stops serving personal and payroll classes until it is (AI-84, setting).
9. **Pay-day link alarm**: from the input cut-off to pay day, a failing link alerts within 1 hour
   instead of 24 (NM-INT-04).

## 7. Still [verify] after this validation

Who the KETs rule covers (employment of 14 days or more, from 1 Apr 2016); unpaid infant care leave (MOM page not found); paternity leave being "mandatory for employers"; the
maternity pay split; the Part IV monthly overtime cap; rest-day and public-holiday work pay rates;
how CPF classifies commission paid at different frequencies; S Pass FCF exemptions; whether the
FR-PAY-16 deduction limits apply to final pay (G4-007, unchanged); the PDPC guidance edition; the
Talenox API; Google's processing locations per model (F44 in the 13 Oct record, unchanged).

## 8. Second reviewer

PRD-001 revision 0.16 and PRD-002 revision 0.2 change payroll posting rules (B.3), approvals and
their evidence across two apps, salary payment release and bank details, tax-clearance holds, the
AI data path and egress, and tenancy-adjacent identity linking. Under AGENTS.md "Safety" they need a
second human reviewer before anyone builds on them.
