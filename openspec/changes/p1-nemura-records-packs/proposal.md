# Proposal

## Why

Nemura has its ground (`p1-nemura-platform`: its own image, database, audit chain, roles, sign-in, web app and privacy
engine) but no people in it. Every later Nemura change (`-leave-time`, `-self-service-letters`, `-payroll`) works on a
person's record and on the facts a country's pack holds: who works where, under what legal classification, the public
holidays, the statutory leave minimums, the retention periods. This change builds the people record, the feature switches
and the packs once, with the rule that no statutory value decides anything until it is verified (NM-PACK-07), and the first
people checks.

Timing (PRD-002 question 12): built after Fyniq's go-live and before the first paid person; none of it is a launch gate.
It needs no payroll data, no pay run and no money.

## What Changes

- **The people record** (NM-PPL-01, 06, 08): a person, with legal and preferred name, contact details, worksite,
  department, job title, line manager and employment type, where job, manager and department changes are effective-dated
  history, a legal classification per person (contract of service or for service, Employment Act coverage and Part IV
  class, residency, retirement and re-employment cohort), and the directory and org chart everyone sees (nothing
  restricted). A person may have no login; a login (`member`) links to at most one person.
- **Restricted fields and the high-risk changes** (NM-PPL-02, 03, 04, 05, 07): NRIC or FIN envelope-encrypted, masked and
  audited on every read; work passes with renewal reminders; the salary bank account change that needs a second person
  and the employee's passkey; dependants for leave; documents per person with a retention rule, stored as evidence.
- **The feature registry** (NM-FEAT-01 to 05): each feature on, off or external, dependencies enforced
  (`NM_FEATURE_REQUIRED`), turning off keeps data and lists what is outstanding, an external system per feature.
- **Packs and worksites** (NM-PACK-01 to 08): Nemura's own pack mechanism (effective-dated, sourced, tested, researched
  then verified then released), the Singapore pack at go-live holding Appendix A's facts, worksites that pick the pack and
  calendar, the next year's calendar, company policy on top checked rule by rule against the statute
  (`NM_POLICY_BELOW_STATUTE`).
- **The people checks** that need only these objects (Appendix C N.1 #3, #8, #12, #13); the rest need leave, letters or
  payroll and stay with those changes.
- **Screens:** `nm-people`, `nm-person`, `nm-features`, `nm-country-pack`, `nm-leave-policy` (the policy form and its
  refusals; the leave types and balances are `p1-nemura-leave-time`).

## Capabilities

### New Capabilities

None.

### Modified Capabilities

None (`skip_specs: true`). It builds requirements already in the main specs: `people/records` (NM-PPL-01 to 08),
`people/features-and-packs` (NM-FEAT-01 to 05, NM-PACK-01 to 08 and one unnumbered) and `people/checks` (N.1 #1 to #15,
of which #3, #8, #12 and #13 are built here). A requirement whose full proof needs an object a later change supplies is
built as far as it can be and says what is left (the coverage table in `tasks.md`).

## Impact

- **Code:** `internal/nemura/{people,features,pack,policy,checks}`; `internal/adapters/postgres/nemura` (the only SQL);
  `db/nemura/migrations/` from 000011; `contracts/openapi/nemura/`; `apps/nemura-web`; one widening of the archtest
  allow-list if the owner agrees (design O1).
- **Data:** the pack files, under Nemura's own directory (design D2), with their sources, retrieval dates and pinned tests;
  the data inventory and the retention schedule gain the new record types (NM-SEC-03, 04).
- **Docs:** user-guide pages and knowledge-base pages for the people screens, the threat model's records section.
- **Needs the owner:** design O1 (how Nemura reads packs) and O2 (who verifies a pack value at go-live). Slice 1 needs neither.
