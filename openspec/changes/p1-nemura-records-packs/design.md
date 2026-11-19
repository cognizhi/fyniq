# Design

## Context

`p1-nemura-platform` gives Nemura a `member` (a login in an organisation), roles with effective dates, a reporting line
(`reporting_line`, effective-dated, with the scope predicates of NM-SEC-02), an approvals primitive, the restricted-field
framework (`ReadRestricted`, masked by default, every read audited), a data-category registry and a retention schedule.
It has no HR record, no packs and no feature switches. Fyniq has a pack mechanism (`packs/`, `internal/domain/pack`: a
`pack.toml`, CSV tables and hand-pinned tests, loaded by its migrator); Nemura's lane (`archtest`) may not import it.

## Goals / Non-Goals

**Goals:** the people record and its history; the restricted fields; the feature registry; Nemura's packs with the
Singapore pack at go-live; policy checked rule by rule; the first people checks. Every value traceable to an official page.

**Non-Goals:** leave types, balances, applying and approving (`p1-nemura-leave-time`); KETs and letters, accounts for
joiners (`-self-service-letters`); pay runs and the CPF rules (`-payroll`); the Fyniq link (`-fyniq-link`); hiring and
onboarding (P2); several countries (P3: the pack mechanism allows them, only Singapore is loaded).

## Decisions

- **D1. A person is not a member.** `person` is the HR record (every employee, contractor and intern, with or without a
  login); `member` stays the login. `person.member_id` is unique when set. The existing `reporting_line` is between members
  today; it moves to persons in slice 1 (a migration that keeps its history and its scope predicates), so a person with no
  login can still have a manager and be someone's report. Alternative rejected: one table (a contractor with no login would
  need a fake member, and the scope predicates would hang on logins).
- **D2. Nemura's own packs, in Nemura's own place.** Pack files live under `internal/nemura/packs/<pack>/<year>.<n>/` and
  are embedded by a Nemura package, in the format Fyniq's packs use (`pack.toml` with `[[sources]]` of URL, section, quote
  and retrieval date; CSV tables; `pinned/` cases). Loading copies a version into Nemura's global read-only pack tables,
  by the migrator (as Fyniq's). A version is **researched**, **verified** (by a second person, recorded) or **released**
  (NM-PACK-07); the loader refuses to release a version with an unverified value unless it is marked as such, and a
  calculation that reads an unverified value yields "to confirm" with no result (NM-PACK-07). Validation of the files
  reuses Fyniq's pure library (design O1).
- **D3. Effective-dated everything.** A job, manager, department, classification, worksite and policy row has
  `effective_from` and `effective_to` and is never edited: a change ends one row and starts the next, with its audit event.
  The "as of" read is a function of a date and is the only read the screens use.
- **D4. Restricted fields through the existing framework.** NRIC or FIN, bank account, medical notes and the like are
  envelope-encrypted with the shared cipher port (local key locally; CMEK `[live-verify]`), masked by default, shown in full
  only with the right (Appendix B) and only with an audited read. They are never in a log, an event or the API's list
  responses. NRIC is collected only for statutory filings (NM-PPL-02) and is never used to sign in or as a file password.
- **D5. The bank-account change is the approvals primitive plus a passkey.** An employee proposes in self-service; it
  takes effect only when an HR or Payroll administrator who did not make it approves, the employee confirms with a
  passkey (step-up exists), and the payroll approvers are notified (a notice in the outbox; delivery with notifications).
- **D6. The feature registry is data and a guard.** `feature_state` per organisation (on, off, external, with the external
  system's name), a static dependency table (NM-FEAT-02), and one function every route and job asks ("is this loaded?").
  Turning a feature off first returns the outstanding list; what is outstanding comes from each feature as it is built
  (an interface the later changes implement; in this change only People records exists, so the list is empty and the
  interface is tested with a fake).
- **D7. Policy is checked by rules, not a number.** A company policy row is compared with each statutory rule of the
  pack for the people it covers (NM-PACK-08); a refusal names the rule and the field (`NM_POLICY_BELOW_STATUTE`). The
  rules are code over pack values (a function per rule with a table test), so a new pack version changes values, not code.
- **D8. Honest scope (the rule of `p1-nemura-platform` D13).** Built only as far as the objects here allow and tagged only
  when fully proven; the coverage table says what a later change finishes.

## Owner decisions needed

### O1. How Nemura reads packs: reuse Fyniq's pure validation library, or copy it (needed before slice 4; D2)

Fyniq's `internal/domain/pack` validates a pack version's metadata, sources, tables and pinned cases. It is a pure library
(no storage, no clock). Nemura's lane forbids importing it today.

| Option | Pros | Cons |
|---|---|---|
| **A. Add `internal/domain/pack` to the shared allow-list** | One validator for both apps' packs (NM-ARC-05 "shared rules, not shared code paths" for a pure library); a fix to source checking reaches both | Widens the allow-list (a reviewed change); Nemura's pack format is then coupled to Fyniq's validator |
| **B. A copy in `internal/nemura/pack`** | No coupling; free to differ | Two validators to keep correct; against the shared-rules intent |
| **C. Put Nemura's packs in Fyniq's `packs/` and its tables** | Nothing new to build | Nemura would read Fyniq's tables: breaks standalone (NM-ARC-01) and the lane |

Recommendation: **A.** It is the same kind of shared library as money, rounding and the audit event form already on the list.

### O2. Who verifies a pack value at go-live (needed before slice 4; NM-PACK-07)

Appendix A's values were researched from MOM, MSF and CPF Board pages (rechecked 14 Oct 2026). NM-PACK-07 says a value is
**verified** only when the official page is captured as evidence and checked by a second person on the publisher's side, and
that a statutory calculation needing an unverified value gives no result unless the company records a decision. The
publishers are the two founders.

| Option | Pros | Cons |
|---|---|---|
| **A. Ship values as researched; each calculation needing one asks the company for a recorded decision** | Honest and already what NM-PACK-07 provides; no one signs for what they have not read | Every company decides on the first use of each value: friction, and Checks #13 shows ✖ until they do |
| **B. A founder verifies each value in the pack screen (page captured, second founder confirms) before release** | The values decide from day one; the verification is recorded evidence | Needs the two founders' time for each of roughly twenty values and each new pack version; they are not employment-law specialists |
| **C. An external adviser verifies before the first paid person** | Strongest assurance | Cost and delay; the pack stays researched meanwhile |

Recommendation: **A now, B before the first paid person's leave or pay is calculated, C only if the founders want an
adviser's sign-off.** Slice 4 builds the mechanism for all three; which values ship verified is the owner's.

## Risks / Trade-offs

- [A statutory value is wrong] → each value has its source URL, quote and retrieval date; pinned tests are worked from the
  official page, not from the table; unverified values decide nothing; Checks #13 shows any live use.
- [Moving `reporting_line` to persons breaks scopes] → slice 1 keeps the old table's history, re-points the predicates and
  keeps the NM-SEC-02 attack tests green; nothing ships between the two states.
- [NRIC handled wrongly] → NM-PPL-02, CMP-PD-07 and the lint rule on NRIC names apply from the first column; a test fails on
  an NRIC in a log, an event, a list response or a file name.
- [Retention of the new record types] → each is in the data inventory and the retention schedule in the slice that adds it.

## Migration plan

Additive migrations from 000011 (new tables, then the `reporting_line` move in a later migration, CHECK widenings
`NOT VALID` then validated); no data to migrate (no person exists yet). Rollback is the previous image; the new tables stay.
