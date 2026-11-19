# Tasks

Each numbered group is one pull request (a "slice"), in this order, and each ends with the gates green: `make check`, `make test`
and, for a slice that touches an image, `make images`. A slice never leaves a test red or a requirement tagged that its tests do
not fully prove. `[live-verify]` marks what only the cloud can show. Slices 1 to 3 need no owner decision; slice 4 needed design O1 and O2, which the
owner decided on 19 Nov 2026 (O1 A, O2 A now and B before the first paid person; CTL-NM-07 tracks the move). Slice 2 and slice 4 are not built yet.

Load before each slice: `fyniq-feature-change` (every slice), `fyniq-accounting-domain` is not needed (no money here),
`fyniq-sg-statutory` (4), `fyniq-security-compliance` (2), `fyniq-api-integrations` (every API operation),
`fyniq-design-system` and `fyniq-knowledge-base` (every screen), `fyniq-release` (a PRD revision).

## 1. The people record and the directory (slice 1)

- [x] 1.1 Migration 000011: `person` (legal and preferred name, work email, employment start, probation end, leaving date and
  reason, `member_id` unique when set), effective-dated `person_job` (title, department, worksite, employment type),
  `person_classification` (contract of service or for service, Employment Act coverage, Part IV class, residency, the date
  the retirement and re-employment cohort is derived from), forced RLS, grants, audit triggers, the "as of" function; goldens
  regenerated. Tests: history is never edited, one open row per person, a leaving date before the start refused.
  `// req: NM-PPL-01, NM-PPL-08`
  (Built, 19 Nov 2026: migration 000011 (`person`, `person_job`, `person_classification` with guards NM120 to NM124), `internal/nemura/people`, `internal/adapters/postgres/nemura/people.go`; `TestAPersonHasEffectiveDatedHistoryAndTheDirectoryShowsWhoWorksHere`. `// req: NM-PPL-01, NM-PPL-06, NM-PPL-08`.)
- [x] 1.2 The reporting line moves to persons (D1): `reporting_line` keeps its rows and the scope predicates now read persons;
  the NM-SEC-02 attack tests stay green and gain a person with no login.
  (Revised, 19 Nov 2026, design D1: the members' reporting line stays on members and the API keeps it in step with the person's manager when both have a login; a manager needs a login to act. Tested in the same test. **Left:** a row-level scope on `person` for a line manager (today the API and its SQL filter decide), with leave approvals.)
- [x] 1.3 API (contract first): list and read persons, create, change job or manager or department (effective-dated), set the
  classification; the directory operation shows names, titles, departments, work emails and the reporting line and nothing
  restricted (NM-PPL-06); `NM_*` codes for each refusal; matrix rows.
  (Built, 19 Nov 2026: `GET /v1/people`, `GET /v1/people/{id}`, `POST /v1/people`, `PUT /v1/people/{id}`, `POST /v1/people/{id}/job`, `POST /v1/people/{id}/classification`, the codes `NM_PERSON_*`, the matrix rows; `TestThePeopleRecordIsReadByTheRightPeopleAndWrittenByHR`.)
- [ ] 1.4 Screens `nm-people` and `nm-person` on the registry (modes, help keys, guide pages `nemura/people.md` and
  `nemura/person.md`, kb pages), the directory and the org chart; axe; the review banner on the HR pages that carry it.
  (Built in part, 19 Nov 2026: `nm-people` (the directory, filtered) and `nm-person` (the record, job history, classification for the people team) with guide pages `nemura/people.md`, `nemura/person.md` and their kb pages, axe. **Left, so unticked:** the add-person and change-job forms, the org chart view and the status filters.)
- [x] 1.5 Verify: `make check`, `make test`, `make openapi`, `make api-check`, `make images`.
  (Done: `make check`, `make openapi`, `make api-check`, the Nemura Go suites in both flavours, the web tests; `make images` not rerun for this slice.)

## 2. Restricted fields, passes, dependants, documents and the bank-account change (slice 2)

- [ ] 2.1 NRIC or FIN, salary-bearing bank account and medical notes as encrypted, masked, audited columns through the cipher
  port (D4); no NRIC in a log, event, list response or file name (a scan test). `// req: NM-PPL-02`
- [ ] 2.2 Work passes with renewal reminders at 90, 60 and 30 days (outbox rows, delivered by the notifier when it exists) and the
  expiry data N.1 #3 reads. `// req: NM-PPL-03`
- [ ] 2.3 The salary bank-account change (D5): proposed, approved by someone else, confirmed by the employee with a passkey,
  payroll approvers noticed. Attack tests: self-approval, no passkey, a stale step-up. `// req: NM-PPL-04`
- [ ] 2.4 Dependants for leave (date of birth or intent to adopt, citizenship, relationship, declaration and evidence),
  effective-dated and restricted. `// req: NM-PPL-07`
- [ ] 2.5 Documents per person with a type, retention rule and viewers, stored as evidence where the type requires it.
  `// req: NM-PPL-05`
- [ ] 2.6 Data inventory, retention schedule and threat model rows for each new record type; the sensitive fields in the
  contract carry the "restricted" marker the contract scan (6.5 of `p1-nemura-platform`) checks.

## 3. The feature registry (slice 3)

- [x] 3.1 Migration: `feature_state` (on, off, external with the system's name), the dependency table, the guard every route and
  job asks, `NM_FEATURE_REQUIRED`; payroll needs the Owner and a second approver (approvals primitive).
  `// req: NM-FEAT-01, NM-FEAT-02`
  (Built, 19 Nov 2026: migration 000012, `internal/nemura/features` and the service, `GET /v1/features`, `PUT /v1/features/{feature}`; `TestTheDependenciesAreEnforcedAndEveryRefusalIsPlain`, `TestAFeatureIsSwitchedWithItsDependenciesAndOpenWorkChecked`, `TestOnlyTheHRAdministratorAndTheOwnerSwitchFeatures`. Payroll cannot be switched on until its two-person approval exists with payroll (`NM_FEATURE_UNAVAILABLE`). `// req: NM-FEAT-01, NM-FEAT-02` only where proven: see the coverage table.)
- [x] 3.2 Off keeps data, lists what is outstanding (an interface the later changes implement; a fake proves it), and what
  must keep running (retention, holds, deadlines) keeps running; external per feature records the system and turns Nemura's
  feature off. `// req: NM-FEAT-03, NM-FEAT-04, NM-FEAT-05`
  (Built, 19 Nov 2026: off keeps every record, open work needs a replacement arrangement for each item (`NM_FEATURE_OBLIGATIONS_OPEN`), proved with a fake `Outstander`; external records the system. **Left:** each feature's own `Outstander`, and the jobs and routes that ask `Switches.Loaded`, as the features are built.)
- [x] 3.3 Screen `nm-features`, API operations, guide and kb pages.
  (Built, 19 Nov 2026: `nm-features`, guide page `nemura/features.md`, its kb page, axe.)

## 4. Packs, worksites, policy and the first checks (slice 4; waits for design O1 and O2)

- [ ] 4.1 Nemura's pack mechanism (D2): files, loader, global read-only tables, the researched, verified and released states, the
  source requirements (URL, section, quote, retrieval date), the refusal of an unverified value in a decision (`to confirm`).
  `// req: NM-PACK-01, NM-PACK-02, NM-PACK-07`
- [ ] 4.2 The Singapore pack `sg-people@2026.1` from PRD-002 Appendix A, each value with its official source fetched on the day
  and hand-pinned tests worked from the page (annual and sick leave, public holidays 2026 and 2027, childcare, infant care,
  maternity, paternity and shared parental leave, Part IV, salary timing, KETs, payslips, records, retirement and
  re-employment, flexible work). A value the page does not settle stays `[verify]`. `// req: NM-PACK-06`
- [ ] 4.3 Worksites (Singapore only) pick the pack and the calendar; next year's calendar and the message for dates beyond it.
  `// req: NM-PACK-03, NM-PACK-05`
- [ ] 4.4 Company policy on top, effective-dated, checked rule by rule (D7) with `NM_POLICY_BELOW_STATUTE` and the "gives more"
  note; screens `nm-country-pack` and `nm-leave-policy`. `// req: NM-PACK-04, NM-PACK-08`
- [ ] 4.5 The checks (Appendix C N.1) built on these objects: #3 (work pass), #8 (calendar by 1 December; a `[verify]` value in a
  live policy), #12 (records past retention, the engine exists) and #13 (a live calculation needs an unverified value with no
  recorded decision), each triggered by a fixture. The others stay with their changes.
- [ ] 4.6 Verify: `make check`, `make test`, `make images`, `make kb-lint`, the controls for the pack and policy gates.

## Coverage

| Requirement | Built in | Status | Left to |
|---|---|---|---|
| NM-PPL-01 People record | 1 | complete | |
| NM-PPL-02 Restricted fields | 2.1 | complete | |
| NM-PPL-03 Work passes | 2.2 | complete | |
| NM-PPL-04 Salary bank account changes | 2.3 | complete (the payroll approvers' notice is queued, delivery with notifications) | |
| NM-PPL-05 Documents per person | 2.5 | complete | KETs and letters as documents: `p1-nemura-self-service-letters` |
| NM-PPL-06 Directory and org chart | 1.3, 1.4 | complete | |
| NM-PPL-07 Dependants for leave | 2.4 | complete | eligibility per scheme: `p1-nemura-leave-time` |
| NM-PPL-08 Legal classification | 1.1 | partial | cohorts and Part IV thresholds applied by leave and hours rules: `p1-nemura-leave-time` |
| NM-FEAT-01 to 05 | 3 | complete except the outstanding lists of features not yet built | each later change implements its outstanding list |
| NM-PACK-01, 02, 06, 07 | 4.1, 4.2 | complete with the values the owner decides ship verified (O2) | |
| NM-PACK-03, 05 | 4.3 | complete (Singapore only) | several countries: P3 |
| NM-PACK-04, 08 | 4.4 | complete for the rules the pack holds | rules about leave types not yet built: `p1-nemura-leave-time` |
| Appendix C N.1 #3, #8, #12, #13 | 4.5 | complete | |
| Appendix C N.1 #1, #2, #4 to #7, #9 to #11, #14, #15 | none | not here: they need letters, payroll, leave, the link or hiring | `p1-nemura-self-service-letters`, `-payroll`, `-leave-time`, `-fyniq-link`, `p2-nemura-people` |
| Screens nm-people, nm-person, nm-features, nm-country-pack, nm-leave-policy | 1.4, 3.3, 4.4 | complete (the leave policy's types: `-leave-time`) | |
