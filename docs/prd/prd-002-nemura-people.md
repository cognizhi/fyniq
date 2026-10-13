# PRD-002 · Nemura: the people platform

| Item | Value |
|---|---|
| Document | PRD-002 |
| Product | **Nemura**: people records, leave, payroll, self-service, letters, and later hiring, performance and onboarding, for small companies, Singapore first |
| Owner | Cognizhi founders (Mei Lin Tan, Arjun Rao) |
| Status | Draft for review |
| Revision | 0.1 (13 Oct 2026): founder request: an optional people platform with its own image, its own sign-in and seamless single sign-on with Fyniq; every feature switchable; country and region packs for calendars, law and policy; a people assistant; a landing without financial information for HR staff; external HR and payroll systems instead of Nemura when a company already has them |
| Date | 13 Oct 2026 |
| Related | [PRD-001 Fyniq](prd-001-fyniq-platform.md) (rev 0.15 adds the Fyniq side: §8.26, FR-PPL-01…08); design system `docs/design-system/nemura/`; specs `openspec/specs/people/` |

Statutory facts in this document were checked on 13 Oct 2026, mostly against secondary sources,
and carry [verify] until the official page is captured as evidence ([Appendix A](#appendix-a--singapore-pack-facts-as-found),
[Appendix F](#appendix-f--sources)). Packs, not code, hold them (NM-PACK-02).

## 1. Summary

Nemura is the people side of Cognizhi's platform. It keeps the company's people records, runs leave,
payroll and payslips, answers employees' everyday requests (a payslip, a letter for the bank, a
change of address, a flexible-work request) and, in later phases, hiring, performance and
onboarding. It runs on its own: its own container image, database and sign-in. When a company also
uses Fyniq, the two link with one sign-in and one switch between **Finance** and **People**, and
everything that touches the books (pay runs, salary payments, departments, leave taken) flows into
Fyniq's ledger through its API and events.

Three things shape it:

1. **Small now, its own product later.** P1 is what a small Singapore company needs every month;
   every feature is switchable, and a company that already uses another HR or payroll system keeps
   it, and Fyniq connects to that system instead (§17, PRD-001 FR-PPL-05).
2. **Law and policy are data.** Country and region packs (Singapore first) hold leave types,
   public-holiday calendars, working-time rules and record-keeping rules, effective-dated with a
   source. A company's own policy sits on top and can never fall below the law (§6).
3. **People first, no financial clutter.** HR staff and employees land on pages built for what they
   do, with no ledger, bank or revenue figures in sight (§19). Nemura has its own look: professional,
   modern and with depth.

## 2. Goals and non-goals

| Goal | Measure |
|---|---|
| An employee gets a payslip, a leave answer or a standard letter without asking anyone | 80% of payslip, balance and standard-letter needs met in self-service; standard letters issued within 1 working day |
| HR work for a 5–50 person company takes minutes a week | Leave approvals in 2 taps; a month's pay run prepared in under 30 minutes for 10 people |
| The books always agree with HR | Every approved pay run is in Fyniq's ledger, or in the imported journal, the same day; the reconciliation check (N.1 #6) never fails silently |
| Singapore rules are right and traceable | Every statutory value comes from a pack with a source and a test (PRD-001 §10.6) |
| Nemura can grow into its own product | It deploys, signs in and works with no Fyniq at all |

**Non-goals for P1–P2:** benefits administration and insurance enrolment; learning management;
expense claims (Fyniq owns claims, PRD-001 FR-AP-14…18; Nemura links to them); global employer of
record; automated hiring or firing decisions of any kind.

## 3. Users, roles and landing pages

| Role | Who | Lands on |
|---|---|---|
| Employee | Everyone with a Nemura account | **My home**: action tiles, balances, next pay day, requests |
| Line manager | Anyone with direct reports | My home, with **Team** (approvals, team calendar) |
| HR officer | Day-to-day people work (Sarah Koh) | **People home**: today's requests, who is away, joiners and leavers, checks |
| HR administrator | Configures features, packs, policies, templates, roles | People home |
| Payroll administrator | Prepares pay runs (PRD-001 role; restricted data, FR-PAY-13) | People home, with **Payroll** |
| Recruiter, interviewer (P2) | Hiring team | Hiring |
| Owner, Administrator | Founders; in Fyniq too | Their last workspace; the Finance \| People switch when they hold both |

| ID | Requirement | Pri | Phase |
|---|---|---|---|
| NM-ROLE-01 | **Roles are Nemura's own** (Employee, Line manager, HR officer, HR administrator, Payroll administrator, Recruiter, Interviewer, Owner, Administrator), assigned per organisation, with the permission matrix of Appendix B. A role in Nemura never grants a right in Fyniq, and the reverse; a person who holds rights in both sees the workspace switch (NM-UX-03) | M | P1 |
| NM-ROLE-02 | **Landing by role**: each role lands on the page in the table above. HR roles, employees and line managers never see ledger, bank, revenue, cash or company financial figures in Nemura, even when the organisation is linked to Fyniq; pay figures appear only to the person they belong to and to roles with payroll rights (NM-SEC-02) | M | P1 |
| NM-ROLE-03 | **Line managers** come from the reporting line in the people record (NM-PPL-01), effective-dated; a manager approves their reports' leave and requests, and sees only their reports' leave, schedule and goals, never pay | M | P1 |
| NM-ROLE-04 | **Segregation**: whoever prepares a pay run cannot approve it (PRD-001 SOD-4); whoever changes an employee's salary bank account cannot approve the change (NM-PPL-04); in a team of one HR person, the Owner is the second person, as in Fyniq's Founder mode (PRD-001 §6.4) | M | P1 |

## 4. Product shape and architecture decisions

| Decision | Choice | Why | Alternative kept open |
|---|---|---|---|
| ADR-N1 Deployment | One container image `nemura` (Go API and worker, static web app), its **own PostgreSQL database** with the same tenancy rules as Fyniq (row-level security, `organisation_id` on every row, PRD-001 §11.4), in the same monorepo (`cmd/nemura/`, `apps/nemura-web/`) | Standalone means separately deployable and separately sellable; one repository keeps contracts, design rules and CI shared | A separate repository once Nemura has its own team |
| ADR-N2 Payroll rules | Nemura's payroll feature **implements PRD-001 §8.7** (FR-PAY-01…16) and Appendix F's formulas, which stay in PRD-001 as the Singapore payroll rules; PRD-002 adds only what is new (§9) | The rules were reviewed twice (PRD-001 rev 0.8, 0.11) and have specs and oracle formulas; moving them gains nothing today | Move the rows into PRD-002 when Nemura becomes its own product |
| ADR-N3 Identity | Standalone: Nemura's own sign-in through the same **identity provider** Fyniq uses (Identity Platform, PRD-001 ADR-T3), one tenant per organisation, with email, Google, any OpenID Connect provider and passkeys for step-up. Linked to Fyniq: the same tenant, so one session works in both; service calls between them use audience-bound OAuth client credentials from Fyniq's authorisation server (PRD-001 ADR-T3, AI-36 pattern), never a user's token | One identity, no second password, and no token passthrough | A separate identity provider if Nemura is sold to companies that use another |
| ADR-N4 Data ownership | Nemura owns people data (records, pay, leave, letters, hiring, reviews). Fyniq owns the books. Fyniq keeps only an **employee projection** (PRD-001 FR-PPL-06) and receives postings; neither writes the other's tables | One owner per fact; the projection keeps payroll detail out of the ledger (FR-PAY-13) | — |
| ADR-N5 Integration | Domain events through each service's outbox (PRD-001 ADR-T7) and Fyniq's public API (PRD-001 §8.18); idempotent on the source ID and version; the events of Appendix E | No shared database, no private endpoints (golden rule 4) | — |
| ADR-N6 Look | Nemura's own design system (`docs/design-system/nemura/`): professional and modern with depth: cool slate surfaces, deep navy text and navigation, an ocean-blue action colour, soft-tinted tiles with glossy icon discs, layered shadows; Funnel Display and Funnel Sans, Spline Sans Mono; the same accessibility rules, device classes and AI marking as Fyniq | The people product should feel distinct from the ledger and easy for everyone, while staying professional | A warmer, flatter look (tried first and set aside) |

| ID | Requirement | Pri | Phase |
|---|---|---|---|
| NM-ARC-01 | **Standalone first**: Nemura deploys, signs people in and runs every feature with no Fyniq installation; in standalone mode the pay run produces a payroll journal file and a bank payment file for any accounting system and bank (NM-PAY-05) | M | P1 |
| NM-ARC-02 | **One image, own database**: `nemura` is a static Go binary with the web app, on `distroless/static:nonroot`, read-only root filesystem, pinned by digest, within the image size budget (PRD-001 §12.9); its own PostgreSQL database and migrations; local mode with the same Compose stack and local substitutes as Fyniq (PRD-001 golden rule 9) | M | P1 |
| NM-ARC-03 | **API-first**: every Nemura capability is an operation in `contracts/openapi/nemura/` before it is a screen, with Fyniq's conventions: RFC 9457 errors with stable codes, `Idempotency-Key` on writes, `ETag`/`If-Match` on versioned records, additive changes inside `/v1` (PRD-001 §8.18.1) | M | P1 |
| NM-ARC-04 | **Linking to Fyniq** is an organisation-level setting that an Owner of both turns on with a second approver: it binds one Nemura organisation to one Fyniq organisation, registers Nemura as an integration client in Fyniq with the scopes of Appendix E only, and starts the sync (NM-INT-01). Unlinking stops the sync and keeps both sides' data | M | P1 |
| NM-ARC-05 | **Shared rules, not shared code paths**: money in Nemura follows PRD-001 golden rule 1 (exact decimals, named rounding policies); every saved change is audited in the same transaction (AUD-01); posted payroll is corrected by reversal; the Go payroll engine is checked against the Python oracle to the cent (golden rule 3) | M | P1 |

## 5. Optional features

| ID | Requirement | Pri | Phase |
|---|---|---|---|
| NM-FEAT-01 | **Feature registry**: People records (always on) and the optional features Leave, Payroll, Self-service, Letters, Hiring (P2), Performance (P2), Onboarding (P2), Time and attendance (P3). The HR administrator turns each on or off per organisation; payroll needs the Owner and a second approver. A feature that is off is not loaded: no routes, menu items, jobs, events or assistant skills | M | P1 |
| NM-FEAT-02 | **Dependencies** are declared and enforced: Payroll needs People records; payslips in self-service need Payroll; leave deductions in payroll need Leave; Onboarding needs Letters for KETs. Turning a feature off that another needs is refused with the reason (`NM_FEATURE_REQUIRED`) | M | P1 |
| NM-FEAT-03 | **External system per feature**: instead of Nemura's own feature, a company may use an external system for payroll, leave, or time (§17). Choosing an external system turns Nemura's feature off and records which system holds that data; Fyniq then takes the books' side from that system (PRD-001 FR-PPL-05) | M | P1 |
| NM-FEAT-04 | **Turning off keeps data**: switching a feature off hides it and stops its jobs; its records stay, under their retention rules (NM-SEC-05), and come back when it is switched on again | M | P1 |

## 6. Country and region packs

| ID | Requirement | Pri | Phase |
|---|---|---|---|
| NM-PACK-01 | **Packs per country and region**: a pack holds the leave types and their statutory entitlements and eligibility, the public-holiday calendar and its observance rules, working-time and overtime rules, payroll rules (by reference to PRD-001's statutory packs for Singapore, §10.6), the required content and timing of documents (KETs, payslips), record-keeping periods, and retirement and re-employment ages. **Regions** inside a country (for example Malaysian states) add or replace calendars | M | P1 |
| NM-PACK-02 | **Effective-dated, sourced and tested**, like Fyniq's statutory packs (PRD-001 §10.6): every value has an effective date, a source URL and the date it was checked; a value marked [verify] shows as "to confirm" in the pack screen; each pack ships with pinned tests. Updates arrive as a new pack version, which the HR administrator reviews and accepts | M | P1 |
| NM-PACK-03 | **Worksites**: each person works at a worksite with a country and region; the worksite picks the pack and the holiday calendar. A company may have worksites in several countries (P3); P1 supports Singapore only | M | P1 / P3 (several countries) |
| NM-PACK-04 | **Company policy on top**: leave entitlements, carry-forward, encashment, waiting periods, holidays the company adds, and working hours are company policy, effective-dated. A policy that gives less than the pack's statutory minimum for the people it covers is refused with the rule it breaks (`NM_POLICY_BELOW_STATUTE`); a policy that gives more says so | M | P1 |
| NM-PACK-05 | **Next year's calendar** is loaded before it is needed: the Singapore pack gets the next year's gazetted public holidays when MOM publishes them; Checks warns from 1 Dec if next year's calendar is missing (N.1 #8) | M | P1 |
| NM-PACK-06 | **The Singapore pack** at go-live holds the facts of Appendix A, each with its source, effective date and [verify] status. A Malaysia pack (EPF, SOCSO, EIS, PCB, HRD Corp levy, state holidays) is the first P3 example | M | P1 / P3 (Malaysia) |

## 7. People records

| ID | Requirement | Pri | Phase |
|---|---|---|---|
| NM-PPL-01 | **People record**: legal and preferred name, contact details, worksite, department, job title, line manager, employment type (full-time, part-time, contract, intern), start date, probation end, and leaving date and reason; extends PRD-001 FR-EMP-01, whose identity, residency, age-band, SHG and salary fields Nemura now owns (PRD-001 FR-PPL-06). Job, pay, manager and department changes are **effective-dated** and kept as history | M | P1 |
| NM-PPL-02 | **Restricted fields**: NRIC or FIN (collected only for statutory filings, encrypted, masked by default, PRD-001 CMP-PD-07), salary and pay history, bank account, medical certificates and medical notes, and disciplinary records are visible only to roles with that right (Appendix B); reading one is audited (PRD-001 AUD-05) | M | P1 |
| NM-PPL-03 | **Work passes**: pass type (Employment Pass, S Pass, Work Permit, Dependant's Pass with letter of consent), number masked, issue and expiry dates, and the employer's renewal reminders at 90, 60 and 30 days; a pass ending without a renewal or a leaving date is ✖ on Checks (N.1 #3) | M | P1 |
| NM-PPL-04 | **Salary bank account changes are high-risk** (as PRD-001 FR-CT-03 for vendors): an employee may propose a change in self-service; it takes effect only after an HR administrator or Payroll administrator who did not make it approves it, the employee confirms it with a passkey, and all payroll approvers are notified; the next pay run shows the change | M | P1 |
| NM-PPL-05 | **Documents per person**: contracts, KETs, signed letters, pass copies, certificates, with a type, a retention rule (NM-SEC-05) and who may see them; stored as evidence where the type requires it (PRD-001 FR-DOC-05) | M | P1 |
| NM-PPL-06 | **Directory and org chart**: everyone sees names, job titles, departments, work emails and the reporting line; nothing restricted | S | P1 |

## 8. Leave and absence

| ID | Requirement | Pri | Phase |
|---|---|---|---|
| NM-LV-01 | **Leave types** come from the pack (annual, outpatient sick, hospitalisation, maternity, paternity, shared parental, childcare, extended childcare, unpaid infant care, adoption, NS) and from company policy (for example birthday, volunteer, compassionate, study, unpaid). Each type has its eligibility (service, child's citizenship and age), unit (day, half day, hour), paid or unpaid, who pays (employer or government, Appendix A), whether a document is needed, and whether a manager approves it | M | P1 |
| NM-LV-02 | **Entitlement and balance** per person and type: accrual or upfront grant, proration in the first and last year, the statutory service-based minimums (annual leave by years of service; sick leave from 3 months' service), carry-forward and its expiry, encashment rules; the balance shows entitled, taken, booked and left, with the calculation on request | M | P1 |
| NM-LV-03 | **Apply in under a minute**: type, dates, half days, a note and an attachment (a medical certificate photo); the form shows the balance after this request, public holidays and rest days excluded, and who else in the team is away; it warns on a clash and refuses a request beyond the balance unless the type allows negative balance | M | P1 |
| NM-LV-04 | **Approval**: the line manager approves; HR is notified, or approves when the type needs it (for example long unpaid leave); approvers act from the notification, the inbox or the team calendar; every decision is audited, and a refusal needs a reason | M | P1 |
| NM-LV-05 | **Team calendar** per month with leave, public holidays per worksite and working patterns (part-time days); filters by team; never shows the leave type to people without the right (shows "Away") | M | P1 |
| NM-LV-06 | **Medical certificates**: sick leave records the certificate (photo or e-MC) and its issuer; HR verifies it; medical details are restricted (NM-PPL-02) | M | P1 |
| NM-LV-07 | **Government-paid leave**: for maternity, paternity, shared parental, childcare, extended childcare and adoption leave, Nemura records the government-paid portion and tracks the employer's reimbursement claim (filed by a person on the Government-Paid Leave portal; no API was found [verify]): claim date, amount, status and evidence; Checks warns before the claim deadline (N.1 #10) | M | P1 |
| NM-LV-08 | **Leave into pay**: unpaid leave and leave encashment become pay-run inputs (PRD-001 FR-PAY-01, gross formula in Appendix F); this supersedes PRD-001 FR-PAY-12 | M | P1 |
| NM-LV-09 | **Leave into the books**: leave taken updates people's available hours in Fyniq (PRD-001 FR-PRJ-12); an optional month-end **leave liability** accrual for untaken annual leave, if the company's accounting policy wants it, goes to Fyniq as a journal for approval (PRD-001 FR-PPL-07) [verify the policy with the accountant] | S | P1 / P2 (accrual) |
| NM-LV-10 | **Flexible work requests** (Tripartite Guidelines on Flexible Work Arrangement Requests, from 1 Dec 2024): an employee submits a formal request (type, start and end, reason); the employer must reply in writing within 2 months and give a reason for a refusal; Nemura tracks the deadline and keeps the written reply (N.1 #5) | M | P1 |

## 9. Payroll

| ID | Requirement | Pri | Phase |
|---|---|---|---|
| NM-PAY-01 | **The rules are PRD-001's**: pay runs, CPF, SDL, SHG, FWL, overrides, itemised payslips, the CPF file, IR8A and AIS, IR21 and its clearance hold, salary deductions and the payroll go-live gate are FR-PAY-01…16 (except FR-PAY-12, superseded by NM-LV-08) with PRD-001 Appendix F's formulas; Nemura implements them and the shared oracle tests them (ADR-N2) | M | P1 |
| NM-PAY-02 | **Inputs collected for the run**: new joiners and leavers, pay changes effective in the month, unpaid leave and encashment (NM-LV-08), approved overtime for Part IV employees (NM-TIM-02), one-off allowances and bonuses as additional wages, and claims paid through payroll if the company chooses that; each input links to its source record | M | P1 |
| NM-PAY-03 | **Payroll calendar**: pay day per pay group, the cut-off for inputs, the latest approval date, salary paid within 7 days after the end of the salary period and overtime within 14 days (Employment Act, Appendix A [verify]); Checks warns when a run is late against these (N.1 #2) | M | P1 |
| NM-PAY-04 | **Payslips to self-service**: published to each person's My pay when the salary is paid, or within 3 working days of it (PRD-001 FR-PAY-08), with a print and download view; an employee who has left keeps access through an expiring link for the record-keeping period (NM-SEC-05) | M | P1 |
| NM-PAY-05 | **Hand-off to the books**: when linked, an approved pay run posts to Fyniq as PRD-001 FR-PPL-04 describes (summary journal by department, no per-person lines) and the salary payments go to Fyniq's payment batch (FR-PAY-11); standalone, it produces a journal file (CSV) and a bank payment file. Either way the run records what the books received (N.1 #6) | M | P1 |
| NM-PAY-06 | **Final pay** for a leaver: the last salary, leave encashment or deduction, notice pay, and the IR21 clearance hold for affected employees (PRD-001 FR-PAY-10) are prepared in one place, linked to offboarding (NM-ONB-03) | M | P1 |

## 10. Self-service and requests

| ID | Requirement | Pri | Phase |
|---|---|---|---|
| NM-ESS-01 | **My home**: big action tiles for what people do most (apply for leave, my payslip, request a letter, claims, update my details, ask a question), the leave balance, the next pay day, open requests and their status, and the next public holiday. Mobile-first; one primary action per screen | M | P1 |
| NM-ESS-02 | **My pay**: payslips by month (view, print, download as PDF), the year's IR8A or AIS form when it is ready, and the pay history the person may see; never the company's figures | M | P1 |
| NM-ESS-03 | **My details**: an employee updates contact details, emergency contacts and address directly; legal name, NRIC or FIN, bank account and residency changes need evidence and HR approval (bank account: NM-PPL-04) | M | P1 |
| NM-ESS-04 | **Requests**: letters (§11), flexible work (NM-LV-10), details changes and general HR questions are requests with a type, a status, a track ("received, with HR, ready"), a due date per type, and the reply; the employee sees where each one is | M | P1 |
| NM-ESS-05 | **Claims link**: when linked to Fyniq, the Claims tile opens Fyniq's My claims (PRD-001 FR-AP-14) with the same sign-in; when standalone or using another expense tool, the tile links there or is hidden | M | P1 |

## 11. Letters and templates

| ID | Requirement | Pri | Phase |
|---|---|---|---|
| NM-LTR-01 | **Letter types** with templates: employment confirmation (for a bank, a landlord or a visa), salary certificate, certificate of employment, service or experience letter, release letter, KETs, offer letter, probation confirmation, promotion or pay-change letter, and the company's own types. Each type sets who may request it, which fields it shows (salary only when the person asks for it), who signs it and how fast it is due | M | P1 |
| NM-LTR-02 | **Templates** use Fyniq's template engine and editor (PRD-001 §8.25, FR-TPL-01…04) with people merge fields (name, job title, start date, salary, pass type, the company's UEN and letterhead); a template change is versioned and needs the HR administrator's approval | M | P1 |
| NM-LTR-03 | **Issue in two steps**: the request fills the template; an HR officer checks it; the authorised signatory signs with a passkey (PRD-001 FR-APR-03 signing); the employee gets it in My requests and by email as an expiring link; the issued letter is kept as evidence (PRD-001 FR-DOC-05) | M | P1 |
| NM-LTR-04 | **Standard letters without waiting**: types the HR administrator marks "issue automatically" (for example an employment confirmation without salary) are generated and signed with the company's pre-approved signature as soon as they are requested, and logged | S | P1 |
| NM-LTR-05 | **Verification**: every issued letter carries a reference and a verification link where a bank or embassy can confirm it is genuine, showing only the letter's own facts; the employee can revoke it | S | P2 |
| NM-LTR-06 | **KETs** are generated from the offer and the people record and issued within 14 days of the start (Employment Act, for employment of 14 days or more [verify]); missing KETs are ✖ on Checks (N.1 #1) | M | P1 |

## 12. Recruitment

| ID | Requirement | Pri | Phase |
|---|---|---|---|
| NM-REC-01 | **Requisitions**: a role, the team, the reason (new or replacement), the salary range, the start date; approved by the Owner or the budget holder. Linked to Fyniq, the requisition shows whether the role is in the plan (PRD-001 §8.14) without showing other financial figures | M | P2 |
| NM-REC-02 | **Job posts**: a careers page and links for job boards; **MyCareersFuture** posting is done by a person (no posting API was found [verify]), with the posting date recorded as evidence; for an Employment Pass hire covered by the Fair Consideration Framework, Nemura counts the 14-day minimum advertising period and blocks the offer letter before it ends unless an exemption is recorded (Appendix A [verify]) | M | P2 |
| NM-REC-03 | **Fair wording**: job posts and interview questions are checked against the Tripartite Guidelines on Fair Employment Practices and, from its commencement, the Workplace Fairness Act: no age, nationality, sex, marital status, pregnancy, caregiving, race, religion, language, disability or mental-health criteria unless a genuine occupational requirement is recorded | M | P2 |
| NM-REC-04 | **Pipeline**: stages (applied, screening, interview, offer, hired, not progressing) on a board, candidate profiles with CV and notes, interview kits with scorecards, and the decision with its reason; every move is audited | M | P2 |
| NM-REC-05 | **Candidate consent and retention**: candidates consent to their data's use for this role (and, separately, for future roles); unsuccessful candidates' data is deleted after the retention period (default 6 months, NM-SEC-05) unless they consented to keep it (N.1 #11) | M | P2 |
| NM-REC-06 | **Offer to hire**: the offer letter comes from a template (NM-LTR-01); on acceptance the candidate becomes a person record with a start date and an onboarding plan (NM-ONB-01), and the Employment Pass or S Pass application date is tracked | M | P2 |

## 13. Performance and KPIs

| ID | Requirement | Pri | Phase |
|---|---|---|---|
| NM-PRF-01 | **Goals**: company, team and personal goals or OKRs with measurable key results, owners, due dates and progress; personal goals may align with a team goal | M | P2 |
| NM-PRF-02 | **KPIs**: role KPIs with targets and their source: entered, or read from Fyniq's non-financial or permitted metrics (PRD-001 §8.13) when linked; a KPI that shows a company financial figure is visible only to people who may see that figure in Fyniq | S | P2 |
| NM-PRF-03 | **Check-ins and feedback**: regular one-to-one notes and feedback, private to the two people and HR unless shared | S | P2 |
| NM-PRF-04 | **Review cycles**: a cycle with dates, the form (self, manager, optional peers), a calibration step for the people team, and the results released to each person; ratings stay restricted | M | P2 |
| NM-PRF-05 | **Pay outcomes**: an increment or bonus recommended in a review becomes an effective-dated pay change or an additional-wage input only after the Payroll administrator prepares it and the Owner approves it (NM-ROLE-04) | M | P2 |

## 14. Onboarding and offboarding

| ID | Requirement | Pri | Phase |
|---|---|---|---|
| NM-ONB-01 | **Onboarding plans** from templates: tasks for HR, the manager, IT and the new joiner, with due dates relative to the start date (KETs within 14 days, bank and CPF details before the first pay run, pass collection, equipment, accounts, introductions) | M | P2 |
| NM-ONB-02 | **Accounts**: linked to Fyniq, a joiner can be invited to Fyniq with the Employee role (My claims) as an onboarding task, by a person with the right to invite; Nemura never grants Fyniq roles itself | S | P2 |
| NM-ONB-03 | **Offboarding**: resignation or termination date, notice period and last day, final pay (NM-PAY-06), leave settlement, IR21 trigger for affected employees (PRD-001 FR-PAY-10), return of equipment, removal of access in Nemura and, linked, a reminder to remove Fyniq access, the exit interview, and the service letter | M | P1 (final pay, IR21, access) / P2 (plans) |

## 15. Time and attendance

| ID | Requirement | Pri | Phase |
|---|---|---|---|
| NM-TIM-01 | **Working patterns**: hours and days per person (full-time, part-time, shifts), used by leave (days to deduct) and by available hours in Fyniq | M | P1 |
| NM-TIM-02 | **Overtime for Part IV employees** (workmen up to S$4,500 and non-workmen up to S$2,600 basic monthly salary [verify]): overtime requested and approved, paid at not less than 1.5 times the hourly basic rate, within the monthly overtime limit, and paid within 14 days of the salary period's end (Appendix A [verify]) | M | P1 |
| NM-TIM-03 | **Clocking and timesheets**: clock in and out on a phone or a kiosk, shift rosters, and timesheets that feed overtime; linked to Fyniq, project time stays in Fyniq (PRD-001 FR-PRJ) | C | P3 |

## 16. Working with Fyniq

| ID | Requirement | Pri | Phase |
|---|---|---|---|
| NM-INT-01 | **Sync**: when linked (NM-ARC-04), Nemura sends the events of Appendix E through its outbox; Fyniq applies them idempotently by source ID and version. Departments and cost centres come **from** Fyniq (PRD-001 FR-EMP-02), so a department's salary account and dimensions stay the accountant's; people and their changes go **to** Fyniq's employee projection | M | P1 |
| NM-INT-02 | **One sign-in, one switch**: a person signed in to either app is signed in to both; the Finance \| People switch keeps their place in each; signing out of one signs out of both (ADR-N3, NM-UX-03) | M | P1 |
| NM-INT-03 | **Pay runs to the ledger**: an approved pay run is sent once, with its approver; Fyniq posts it (PRD-001 FR-PPL-04) and returns the posting reference, which Nemura shows on the run. A refused posting (for example a locked period) comes back with Fyniq's error code and a plain message, and the run waits | M | P1 |
| NM-INT-04 | **Health**: the link shows its state, the last successful sync, events waiting or failed, and a retry; a sync failing for 24 hours is ⚠ on both apps' Checks (N.1 #7) | M | P1 |
| NM-INT-05 | **Minimum data to Fyniq**: only the fields Appendix E lists; never NRIC or FIN, medical, disciplinary or review data; pay reaches Fyniq only as department totals and as payment lines in a salary batch | M | P1 |

## 17. External HR and payroll systems

| ID | Requirement | Pri | Phase |
|---|---|---|---|
| NM-EXT-01 | **Fyniq without Nemura**: a company that already uses an HR or payroll system keeps it; Fyniq's People and HR settings (PRD-001 FR-PPL-01) record which system holds payroll, leave and people records, and Nemura is not loaded for those features | M | P1 |
| NM-EXT-02 | **Connectors** (PRD-001 FR-INT-25…27) bring the books' side in: payroll journals by pay period, salary payment lines for Fyniq's payment batch, the employee projection, and leave taken for available hours. P1 ships a **file import** (CSV or the provider's payroll journal export, with a column mapping) and one Singapore provider connector chosen after its API is verified (Talenox has a developer site; its authentication and endpoints are [verify]); P2 adds a unified HR API adapter (for example Finch, Merge or Kombo) for wider coverage | M | P1 (file, one connector) / P2 (unified API) |
| NM-EXT-03 | **Imported journals are drafts** that a Controller or Accountant approves in Fyniq, because the external system's approval cannot be verified (PRD-001 FR-PPL-05); the import is idempotent per pay period and version, and a re-import shows what changes (PRD-001 FR-INT-27 re-ingestion preview) | M | P1 |
| NM-EXT-04 | **Mixed use**: a company may use Nemura for some features (for example letters and self-service) and an external system for payroll; Nemura then reads pay data it needs for letters (salary) only from its own record, entered or imported, never live from the external system | S | P2 |

## 18. The people assistant

| ID | Requirement | Pri | Phase |
|---|---|---|---|
| NM-AI-01 | **Same rules as Fyniq's AI**: the people assistant follows PRD-001 §9: it reads through tools and proposes; humans decide (golden rule 5); it uses the model routes and data-class policy of §9.9.1 (AI-70…AI-82); people and payroll data are **personal data and payroll** classes, so they go only to in-Singapore processing or a self-hosted model in the organisation's Singapore network by default (AI-75) | M | P1 |
| NM-AI-02 | **Skills baked in** (`agents/people-assistant/SKILL.md`): answer leave, pay-day, payslip and policy questions from the person's own records and the pack and handbook (cited); explain a payslip line; draft a letter, a job post, interview questions, a review summary or a reply to a flexible-work request as a **proposal** (Appendix D) for a person to edit and approve | M | P1 / P2 (hiring, reviews) |
| NM-AI-03 | **Knowledge**: the Singapore pack (Appendix A), the company's handbook and policies and Nemura's user guide are compiled into the knowledge base (PRD-001 §9.11) and cited; statutory rules come from the pack, never from the model's memory; a rule marked [verify] is answered with "to confirm with HR" | M | P1 |
| NM-AI-04 | **Never-list for people work**: the assistant never approves or refuses leave or requests, changes pay, a bank account, a role or a record, issues or signs a letter, moves a candidate, decides a hiring, rating, promotion or termination, or reads another person's restricted data beyond the asking user's rights; red-team tests enforce it (PRD-001 QA-AI-02) | M | P1 |
| NM-AI-05 | **Fairness**: the assistant never screens, ranks or scores candidates or employees, never uses a protected characteristic, and flags such criteria in drafts (NM-REC-03) | M | P2 |

## 19. Experience and design

| ID | Requirement | Pri | Phase |
|---|---|---|---|
| NM-UX-01 | **Nemura's own design system** (`docs/design-system/nemura/MASTER.md`, `tokens.css`): professional and modern, with depth, and the same semantic token names as Fyniq so components behave alike; tokens only; contrast validated in four themes (`make check-tokens`, PRD-001 QA-UI-03); AI content always violet with the AI badge, as in Fyniq | M | P1 |
| NM-UX-02 | **Easy for everyone**: plain words, one primary action per screen, big touch targets (44 px), mobile-first self-service, the five device frames of PRD-001 FR-UX-37, WCAG 2.2 AA, switchable motion (FR-UX-38). Employees have one simple mode; the people team's pages show more | M | P1 |
| NM-UX-03 | **Workspace switch**: a **Finance \| People** switch in the header for people with rights in both Fyniq and Nemura; it changes app, keeps each app's last page, and never changes rights. Fyniq's header carries the same switch (PRD-001 FR-PPL-02) | M | P1 |
| NM-UX-04 | **Help on every screen**: help keys, tooltips, guide pages in `docs/user-guide/nemura/` and a first-run tour for employees and for the people team (PRD-001 §7.9) | M | P1 |
| NM-UX-05 | **Print-ready documents**: payslips and letters have a print and PDF view on the company's letterhead, readable in black and white | M | P1 |

## 20. Security, privacy and records

| ID | Requirement | Pri | Phase |
|---|---|---|---|
| NM-SEC-01 | **Inherited controls**: Nemura applies PRD-001's security, audit and tenancy requirements as written (§11.4 tenancy, AUD-01 audit, SEC-15 logging, secrets in Secret Manager, ASVS level 2) with its own threat model; changes to the AI write path, approvals, pay or bank details, tenancy or the audit trail need a second human reviewer | M | P1 |
| NM-SEC-02 | **Need to know**: pay, NRIC or FIN, bank account, medical, disciplinary, review and candidate data each have a right (Appendix B); masked by default; every read of restricted data is audited; a person always sees their own | M | P1 |
| NM-SEC-03 | **PDPA**: purposes recorded per record type; access and correction requests from employees and candidates handled with a due date; a data inventory entry per type; transfers outside Singapore only after an assessment (PRD-001 CMP-PD-06) | M | P1 |
| NM-SEC-04 | **Records the law requires**: employment records and payslips kept for at least the pack's period (Singapore: 2 years for current employees and after leaving [verify the period after leaving]); KETs and letters kept as evidence | M | P1 |
| NM-SEC-05 | **Retention per record type** in the pack and company policy (for example candidate data 6 months by default, medical certificates 1 year after the leave, employment records as NM-SEC-04 requires); due deletions run as a reviewed job with a report, never silently, and data under a legal hold is kept (N.1 #12) | M | P1 |
| NM-SEC-06 | **Grievances** (Workplace Fairness Act, expected end 2027 [verify]): a confidential grievance channel with an inquiry record, a written outcome and restricted access, ready before the Act commences | S | P2 |

## 21. Deployment and non-functional requirements

| ID | Requirement | Pri | Phase |
|---|---|---|---|
| NM-NFR-01 | **Region and residency**: Google Cloud `asia-southeast1` like Fyniq; local mode offline with the same `make` contract once Nemura has code; scale to zero (PRD-001 ADR-T7) | M | P1 |
| NM-NFR-02 | **Performance**: My home and My pay render in under 1.5 s at the 75th percentile on a mid-range phone on 4G; a pay run for 50 people calculates in under 5 s | S | P1 |
| NM-NFR-03 | **Availability** 99.5% a month for self-service; pay-day jobs retried and alerting; if Fyniq is down, Nemura keeps working and queues the hand-off (outbox) | M | P1 |
| NM-NFR-04 | **Localisation**: English first; dates, numbers and currency from the worksite's pack (Singapore: `en-SG`, S$); templates and packs ready for other languages | S | P1 |

## 22. Phasing

| Phase | Nemura delivers | With Fyniq |
|---|---|---|
| P1 | People records, Singapore pack, leave and flexible-work requests, payroll (PRD-001 FR-PAY), self-service, letters and templates, KETs, final pay and IR21, working patterns and Part IV overtime, the people assistant (questions and letter drafts), standalone sign-in, the Fyniq link with one sign-in and the workspace switch, the file import and one connector for external payroll | Fyniq's P1 |
| P2 | Hiring, performance and KPIs, onboarding and offboarding plans, letter verification, leave liability accrual, unified HR API adapter, grievances | Fyniq's P2 |
| P3 | Time and attendance, the Malaysia pack and several countries | Fyniq's P3 |

## 23. Open questions and recommended defaults

| # | Question | Recommended default | Owner |
|---|---|---|---|
| 1 | Does payroll move to Nemura at P1, or stay in Fyniq until Nemura's P2? | Move at P1: Nemura implements PRD-001 §8.7 (ADR-N2); Fyniq keeps the posting | Founders |
| 2 | Standalone tenants: one Identity Platform tenant per organisation? | Yes; a linked organisation shares Fyniq's tenant | Founders + CTO |
| 3 | Leave year: calendar year or each person's anniversary? | Calendar year, prorated | HR (Sarah Koh) |
| 4 | Leave liability accrual at month end? | Off; decide with the external accountant (Ho & Partners LLP) | Founders + accountant |
| 5 | Letter signatures: passkey signature in Nemura, or an e-signature provider? | Passkey signature with a verification link (NM-LTR-03, NM-LTR-05) | Founders |
| 6 | MyCareersFuture: is there an employer API? | Treat posting as manual with evidence until WSG confirms one | HR |
| 7 | First external payroll connector? | The file import first; then the Singapore provider whose API is verified first | CTO |
| 8 | Is Nemura sold on its own, and how is it priced? | Out of scope for this PRD; decide before P1 build | Founders |
| 9 | Grievance channel before the Workplace Fairness Act commences? | Build in P2 so it is ready well before end 2027 | Founders + DPO |

## Appendix A · Singapore pack facts as found

Checked on 13 Oct 2026. Each row becomes a pack value with its source and effective date; rows
marked [verify] show as "to confirm" until the official page is captured (NM-PACK-02).

| Topic | Rule as found | Source (Appendix F) |
|---|---|---|
| Annual leave | 7 days after the first year of service (prorated after 3 months), +1 day a year of service to 14 days, for employees covered by the Employment Act [verify against MOM] | Secondary guides |
| Sick leave | After 3 months' service: up to 14 days outpatient and 60 days hospitalisation (including the 14); graduated between 3 and 6 months' service (5/15, 8/30, 11/45, 14/60) [verify] | Secondary guides |
| Public holidays | 11 paid public holidays; a holiday on a Sunday moves to the Monday; a holiday on a rest day other than Sunday or a non-working day gives a day off in lieu or a day's pay; working on a holiday earns an extra day's pay, or time off in lieu by agreement for employees outside Part IV [verify] | MOM-based sources |
| 2027 calendar | 11 holidays; Chinese New Year on Sat 6 and Sun 7 Feb 2027 with Mon 8 Feb observed; Hari Raya Puasa Wed 10 Mar; Vesak Day Thu 20 May; Deepavali Thu 28 Oct; Christmas Sat 25 Dec [verify the full list against MOM's gazette] | MOM-based sources (one source disagrees) |
| Childcare leave | 6 days a year for a Singapore Citizen child under 7 (first 3 employer-paid, last 3 government-paid); 2 days extended childcare for a child aged 7–12 (government-paid) [verify] | Secondary guides |
| Unpaid infant care leave | 12 days a year per parent for a Singapore Citizen child under 2, after 3 months' service (raised from 6 days on 1 Jan 2024) [verify] | Secondary guides |
| Maternity leave | 16 weeks Government-Paid Maternity Leave for eligible mothers; employer pays and claims the government-paid portion [verify the split] | MSF, secondary guides |
| Paternity leave | 4 weeks Government-Paid Paternity Leave, mandatory for employers from 1 Apr 2025 [verify] | MSF reports |
| Shared parental leave | 6 weeks for children born 1 Apr 2025–31 Mar 2026; 10 weeks for children born from 1 Apr 2026; default 5 + 5 weeks; reallocation within 4 weeks of birth, later only with the employer's agreement; to be taken within 12 months of birth; employers verify on the Government-Paid Leave portal [verify] | [MSF](https://www.msf.gov.sg/media-room/article/10-weeks-of-shared-parental-leave-from-1-april-2026) |
| Reimbursement cap | Government-paid leave reimbursed up to S$2,500 a week (sources differ on whether the cap is per week or per 4-week block) [verify] | Secondary guides |
| Part IV | Hours, overtime and rest-day rules for workmen earning up to S$4,500 and non-workmen up to S$2,600 basic a month (non-workman threshold from 1 Apr 2019); managers and executives excluded; 44 hours a week, overtime at least 1.5 × the hourly basic rate, one rest day a week [verify the monthly overtime cap] | MOM 2019 guide; secondary |
| Salary timing | Salary within 7 days after the end of the salary period; overtime within 14 days [verify] | Employment Act summaries |
| KETs | Written key employment terms within 14 days of the start, for employment of 14 days or more (from 1 Apr 2016) | Secondary guides citing MOM |
| Itemised payslips | With the salary, or within 3 working days of it (PRD-001 FR-PAY-08) | MOM; PRD-001 |
| Employment records | Keep payslip and employment records for at least 2 years; sources differ on the period for former employees [verify] | Secondary guides; [MOM](https://www.mom.gov.sg/maintenance/employment-practices/employment-records/) |
| Retirement and re-employment | Retirement age 64 and re-employment age 69 from 1 Jul 2026; CPF rate increases for older workers start in 2027, not 2026 [verify the 2027 rates] | Allen & Gledhill, HSF Kramer |
| Flexible work requests | From 1 Dec 2024, employers must consider formal requests and reply in writing within 2 months; a refusal needs a reasonable business ground and a reason | [MOM TG-FWAR](https://www.mom.gov.sg/maintenance/employment-practices/good-work-practices/flexible-work-arrangements) |
| Workplace Fairness Act | Passed in two bills (January and November 2025); expected to commence at the end of 2027; employers must have a grievance-handling process (the 25-employee threshold in the 2024 bill is [verify]); until then the Tripartite Guidelines on Fair Employment Practices apply | HSF Kramer; Singapore Law Watch |
| Fair Consideration Framework | Advertise on MyCareersFuture for at least 14 consecutive days before an Employment Pass application unless exempt; sources differ on the salary exemption (S$22,500 a month most often) and the size exemption (fewer than 10 employees) [verify]; no posting API found | MOM; secondary guides |
| Retrenchment | Mandatory Retrenchment Notifications to MOM within 5 working days, for employers with at least 10 employees [verify: not confirmed in this research] | Not confirmed |

## Appendix B · Permission matrix

Legend: **C** create or prepare · **A** approve · **V** view · **O** own record only · **M** masked · — none.

| Capability | Employee | Line manager | HR officer | HR admin | Payroll admin | Recruiter | Owner | AI |
|---|---|---|---|---|---|---|---|---|
| Own profile, balances, payslips, requests | O | O | O | O | O | O | O | O |
| Directory and org chart | V | V | V | V | V | V | V | V |
| People records (not restricted) | — | V (reports) | C | C/A | V | — | A | V |
| Restricted fields: NRIC/FIN, bank, medical, disciplinary | — | — | M | C/A | M (bank V) | — | V | — |
| Pay, pay history, pay runs | — | — | — | — | C | — | A | — |
| Leave requests | C (own) | A (reports) | A | A | V | — | A | — |
| Letters and templates | C (request) | — | C | A (templates) | — | — | A (sign) | C (draft) |
| Features, packs, policies, roles | — | — | V | C | V | — | A | — |
| Fyniq link and external systems | — | — | — | C | V | — | A (+ second approver) | — |
| Hiring (P2) | — | V (own roles) | V | V | — | C | A (offers) | C (drafts) |
| Performance (P2) | O | C (reports) | V | C | — | — | A | C (drafts) |

## Appendix C · Checks

### N.1 Checks Nemura runs

| # | Area | Condition | Status |
|---|---|---|---|
| 1 | Letters | A person who started 14 or more days ago has no issued KETs (NM-LTR-06) | ✖ |
| 2 | Payroll | A pay run is not approved by its latest approval date, or salary or overtime would be paid later than the pack allows (NM-PAY-03) | ⏰ |
| 3 | People | A work pass expires within 30 days with no renewal or leaving date, or has expired (NM-PPL-03) | ✖ |
| 4 | Leave | A leave balance is below zero for a type that does not allow it, or a leave request has waited more than 3 working days for its approver (NM-LV-02, NM-LV-04) | ⚠ |
| 5 | Requests | A flexible-work request has no written reply within 2 months, or a letter request is past its due date (NM-LV-10, NM-ESS-04) | ⏰ |
| 6 | Payroll | An approved pay run's totals differ from what the books received (Fyniq's posting or the exported journal), or the hand-off has not completed by pay day (NM-PAY-05, NM-INT-03) | ✖ |
| 7 | Link | The Fyniq link has not synced successfully for 24 hours (NM-INT-04) | ⚠ |
| 8 | Packs | Next year's public-holiday calendar is not loaded by 1 Dec, or a pack value marked [verify] is used by a live policy (NM-PACK-02, NM-PACK-05) | ⚠ |
| 9 | Payslips | A payslip was not published within 3 working days of the salary payment (NM-PAY-04) | ✖ |
| 10 | Leave | A government-paid leave reimbursement claim is not filed 30 days before its deadline (NM-LV-07) | ⏰ |
| 11 | Hiring | Unsuccessful candidates' data is past its retention period without consent to keep it (NM-REC-05) | ⚠ |
| 12 | Records | Records past their retention date are waiting for the reviewed deletion job (NM-SEC-05) | ⚠ |

## Appendix D · Task catalogue for the people assistant

Each task is a proposal type in PRD-001 §9.6's framework: strict input schema, tier, limits,
approver roles, 72-hour expiry, off by default.

| Task type | What it proposes | Tier | Approver | Phase |
|---|---|---|---|---|
| `people.draft_letter` | A letter from a template for a request, with the fields filled from the record | 1 | HR officer | P1 |
| `people.draft_fwa_reply` | A written reply to a flexible-work request, with the reason for a refusal | 1 | Line manager or HR officer | P1 |
| `people.draft_job_post` | A job post from a requisition, with fair-wording flags | 1 | Recruiter | P2 |
| `people.draft_interview_kit` | Interview questions and a scorecard for a role | 1 | Recruiter | P2 |
| `people.draft_review_summary` | A summary of a person's goals, check-ins and feedback for their manager | 1 | Line manager | P2 |

## Appendix E · Events and data shared with Fyniq

| Event or call | Direction | Fields | Scope (Fyniq authorisation server) |
|---|---|---|---|
| `people.person.upserted` | Nemura → Fyniq | Person ID, display name, work email, department, cost centre, start and leaving dates, employment status | `people.projection.write` |
| `people.department.changed` | Fyniq → Nemura | Department ID, name, salary account, dimensions (FR-EMP-02) | Nemura reads `org.departments.read` |
| `payroll.run.approved` | Nemura → Fyniq | Run ID and version, period, pay day, approver, department totals by pay item, employer CPF, SDL, SHG, net pay total | `payroll.journal.submit` |
| `payroll.payments.ready` | Nemura → Fyniq | Run ID, one payment line per person (payee name, bank, account, amount), encrypted in transit and at rest, not kept as contacts | `payroll.payments.submit` |
| `payroll.run.posted` | Fyniq → Nemura | Run ID and version, posting reference, status or error code | webhook to Nemura |
| `leave.taken.changed` | Nemura → Fyniq | Person ID, dates, hours (no leave type) | `people.availability.write` |
| `leave.liability.accrued` (P2) | Nemura → Fyniq | Month, total untaken annual leave value by department | `payroll.journal.submit` |

## Appendix F · Sources

Checked on 13 Oct 2026; most are secondary and every fact they support carries [verify] in Appendix A.
MSF, [10 weeks of shared parental leave from 1 April 2026](https://www.msf.gov.sg/media-room/article/10-weeks-of-shared-parental-leave-from-1-april-2026);
[Vistra](https://www.vistra.com/insights/singapores-enhanced-shared-parental-leave-scheme-what-employers-need-know-april-2026)
and [Lockton](https://global.lockton.com/us/en/news-insights/singapore-enhances-paid-family-leave-entitlements-updated) on shared parental leave;
[HSF Kramer](https://www.hsfkramer.com/notes/employment/2025-posts/singapore-workplace-fairness-act-to-take-effect-end-of-2027)
and [Singapore Law Watch](https://www.singaporelawwatch.sg/Headlines/parliament-passes-second-workplace-fairness-bill-paving-way-for-roll-out-in-end-2027) on the Workplace Fairness Act;
[MOM employment records](https://www.mom.gov.sg/maintenance/employment-practices/employment-records/),
[Info-Tech on itemised payslips](https://www.info-tech.com.sg/blog/mom-compliant-itemised-payslip-singapore/);
[Allen & Gledhill](https://www.allenandgledhill.com/sg/publication/articles/32838/retirement-and-re-employment-ages-to-be-raised-to-64-and-69-from-1-july-2026)
and [HSF Kramer](https://www.hsfkramer.com/notes/employment/2026-posts/singapore-changes-to-retirement-and-re-employment-ages-to-take-effect-from-1-july-2026) on retirement ages;
[MOM flexible work arrangements](https://www.mom.gov.sg/maintenance/employment-practices/good-work-practices/flexible-work-arrangements);
[MOM consider all candidates fairly](https://www.mom.gov.sg/passes-and-permits/employment-pass/consider-all-candidates-fairly);
[HCAMag on the 2027 public holidays](https://www.hcamag.com/asia/specialisation/benefits/singapore-reveals-public-holidays-for-2027/579526);
[MOM 2019 guide to Employment Act changes](https://www.MOM.gov.sg/-/media/mom/documents/speeches/2018/guide-to-employment-act-changes-1-april-2019.pdf);
leave entitlement summaries ([Asanify](https://asanify.com/blog/employer-of-record-singapore/leave-policy-singapore-2026-guide/));
government-paid leave claims ([Raffles Corporate Services](https://rafflescorporateservices.com/government-paid-parental-leave-employer-reimbursement-singapore-2026/));
HR software market ([Sleek](https://sleek.com/sg/resources/best-payroll-software-singapore/), [BrioHR](https://briohr.com/blog/best-hr-software-singapore/));
unified HR APIs ([Finch](https://tryfinch.com/blog/best-unified-apis-hris-payroll));
Talenox's API ([Pipedream](https://pipedream.com/apps/talenox), developer site not read);
Malaysia payroll ([Second Talent](https://www.secondtalent.com/resources/malaysia-payroll-benefits-tax/)).

## Revision history

| Revision | Date | Changes |
|---|---|---|
| 0.1 | 13 Oct 2026 | First draft, at the founders' request: Nemura as an optional people platform with its own image and sign-in, single sign-on and a workspace switch with Fyniq, switchable features, country and region packs (Singapore first), people records, leave, payroll (implementing PRD-001 §8.7), self-service, letters, hiring, performance, onboarding, time, external HR systems, the people assistant, its own design system, checks N.1 #1–12 and the assistant's task catalogue. **Second reviewer required** (payroll, personal data, the AI write path, tenancy) |
