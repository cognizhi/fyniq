# Design

## Context

See proposal.md for why. The engine and the oracle already agree on every shape the generator makes:
the gate passes on `main`. But the generator avoids the shapes whose rules were open
(`test/differential/README.md`, "Choices where PRD-001 is silent"). The comparator also checks only
*which* documents are rejected, never why: the result's `rejected` list carries no reason codes.

Three decisions change behaviour:
- **Earlier-dated reversal:** the oracle posts one today.
- **Manual journal line to share capital:** the oracle posts it, and the scenario format cannot mark
  such an account.
- **Rate without its source:** today a rejected document; under the decision, an invalid scenario.

## Goals / Non-Goals

**Goals:**
- Both sides build every P0 rule of rev 0.24.
- The gate covers every shape that was open.
- No step ever fails the gate on `main`.

**Non-Goals:**
- `RND-RATE` and the rate feed (P1).
- INV-07's ⚠ for manual journals to control accounts (P1).
- FR-GL-12's edits and voids of documents other than the manual journal (P1).
- Any change to the requirement rows, which rev 0.24 already approved.

## Decisions

- **Order: format, then engine, then oracle, with the decided shapes last.** A golden or generated
  scenario that exercises a decided shape fails the gate until both sides follow its rule. So:
  1. **Format.** Change the schema only. No current golden or generated scenario uses `role`, and every
     generated rate carries the source "document", so the gate is unaffected.
  2. **Engine half.** It builds the rules and proves them with its own `// req:` tests. The generator
     still avoids the shapes, so the gate passes.
  3. **Oracle half.** In one pull request: change the oracle's rules, make the generator produce the
     shapes, and add golden cases for them. That pull request's required `make test` and
     `make differential` runs are the first engine-versus-oracle check of the new rules.

  Rejected alternatives:
  - One pull request with both halves: one session would then read both sides (QA-ENG-01).
  - Oracle first: its generator change would fail the gate until the engine lands.
- **Share capital is an account role, not a list of codes.** The scenario gets an optional
  `accounts[].role`, with one value, `share-capital`. The chart of accounts gets a matching mark, held
  wherever FR-COA-03's system accounts are (the engine session chooses the column).
  Alternative: hard-code 3010 and 3020. Rejected because INV-13 names "share-capital accounts", not
  codes, and an organisation may number its chart differently.
- **A rate without a source is an invalid scenario.** The schema enforces it with
  `dependentRequired: {"fx_rate": ["fx_rate_source"]}`. The API assigns the source itself (a typed rate
  is "document", ENG-40), so a scenario lacking one is malformed input, not a rule failure.
  A rate on a base-currency line stays a rule failure, a rejected document:
  - the API receives it as input and refuses it with a field error (ENG-03);
  - a line cannot see the scenario's base currency, so the schema cannot check it.
- **Reversals from P0 without claiming FR-GL-12.**
  - The negation is ENG-14's: tests carry `// req: ENG-14`.
  - The date and at-most-once rule is FR-GL-12's rev 0.24 note, "from P0 for manual journals": tests
    carry `// req: FR-GL-12`.
  - The P1 row itself, edits and voids of every document type, is not claimed. Change 7 did the same
    for FR-INT-06…11.
  - The scenario format has no closed periods, so the oracle checks only the date order. The engine
    also refuses a closed period (INV-03), which is already built.
- **INV-13 in both the domain and the database.**
  - The domain refuses the line (ENG-22, and ENG-24's pointer to INV-13).
  - The posting-source trigger that INV-13 names is the backstop (golden rule 2, ADR-D1).
  - The sources allowed to post to share capital (the register of share issues and opening balances)
    arrive with those sources. In P0 only manual journals post, so every manual journal line to a
    share-capital account is refused.
- **Who writes what.** The oracle session writes groups 1 and 3; the engine session writes group 2.
  The oracle session wrote group 2 from the PRD rows and the scenario format alone, so the engine
  session revises it with `/opsx:update` before applying it.
- **ADRs, ports and audit events.**
  - ADR-D1 (PostgreSQL as the backstop): INV-13's trigger and the account mark live in the database.
  - ADR-T1 (the stack): the engine in Go, the oracle in Python.
  - ADR-T8: the storage port carries the account mark when it reads the chart of accounts. No new
    port or adapter.
  - Audit events: none new, since the mark arrives by migration and seed. The engine session confirms
    against p0-audit-chain whether a refused journal writes an audit event or only returns problem
    details.

## Risks / Trade-offs

- [Both sides misread a decided rule the same way] → each side's tests are worked by hand from the PRD
  rows, and the accountant reviews the posting rules (the statutory review area).
- [Building shows a decided rule is wrong] → stop and change the PRD first (fyniq-openspec, Recipe D).
- [A golden case for a decided shape lands early and fails the gate] → task 2.7 forbids golden cases
  and generator changes for those shapes until group 3.
- [The new generator kinds slow `make test`] → the fixed-seed sample keeps its size: the new kinds
  replace part of the draw instead of adding to it.

## Migration Plan

One migration adds the account mark. Its seed marks 3010 and 3020 in the default chart and in
`cmd/seed`'s sample company. Before the first production deploy, the mark holds only seeded data, so
rolling back means reverting the pull request.
