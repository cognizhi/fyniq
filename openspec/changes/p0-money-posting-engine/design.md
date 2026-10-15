# Design

## Context

ADR-T1 picks Go partly for its lack of implicit numeric coercion; `apd` v3 gives decimal arithmetic with
explicit contexts. The oracle (p0-python-oracle-differential) will check this engine's output to the
cent, so every rounding must be a named, documented step.

## Goals / Non-Goals

**Goals:**
- No float can reach a money value: the type has no float constructor, and a linter rule forbids
  `float32`/`float64` in money packages.
- The engine is deterministic: the same document and parameters always give the same lines.

**Non-Goals:**
- The P1 posting sources (invoices, bills, payroll, FX revaluation and others).
- Statutory calculators (P1 packs).

## Decisions

- **apd v3 with a fixed context per operation.** Each operation states its precision and rounding mode,
  and rounding to a currency's minor unit happens only inside a named policy. Alternative:
  shopspring/decimal, rejected because its rounding is implicit in places.
- **Pure engine, impure shell.** `ledger.Post(doc, params) ([]Line, error)` takes everything it needs as
  arguments, including the clock's value and the account map. The application service loads them,
  calls the engine and persists the lines through the unit of work in the same transaction.
- **Provenance on every line.** Each line carries the source document, its version, the rule ID that
  produced it and the ledger revision (ENG-03, ENG-46).
- **Errors as values with codes.** Each validation failure carries a stable code, the field and a fix
  (ENG-22); the API maps them to RFC 9457 problems in p0-api-conventions-authz.
- **Audit events.** A posted entry emits `journal_entry.posted` through p0-audit-chain in the same
  transaction (AUD-01).
- **Ports.** None new: the engine is pure; persistence goes through `UnitOfWork`.

## Risks / Trade-offs

- [Go mutation tooling is pre-1.0] → scope gremlins to the three packages and review survivors by hand,
  as QA-ENG-04 says; the run belongs to `make ci`, not `make test`.

## Implementation notes (15 Oct 2026)

- **Packages.** `internal/domain/money` (Money, Currency, Rate; strict parsing; decimal-string
  JSON), `internal/domain/rounding` (the policies, called by name), `internal/domain/ledger` (the
  engine: `PostJournal`, `Reverse`, posting identities, `LEDGER-nn` codes), `internal/domain/journal`
  (the manual journal document and its commands) and `internal/domain/balance` (the balance service).
- **Policies built now:** `RND-FX` (the only one the manual journal uses), `ALLOC-EVEN` with its
  pinned G4-015 case, and `ALLOC-PRORATA`. The statutory policies (`RND-GST-*`, `RND-CPF*`,
  `RND-SDL`), `RND-SHARE` and `RND-MILEAGE` arrive with their P1 sources and packs. apd's
  `RoundHalfUp` rounds the magnitude, so it is half away from zero, as §10.4 defines "half-up";
  pinned cases check negative halves.
- **Cross-currency arithmetic** is refused at run time with `MONEY-04` (Go has no cheap way to make
  it a compile-time error without a type per currency); the only conversion is `rounding.FX`, which
  takes a `Rate` that carries its source (ENG-13).
- **Entries reference their rounding policy implicitly**: every line's base amount is `RND-FX`, and the
  database enforces exactly that formula (`journal_line_base_amount_rnd_fx`). Allocation policies
  will be recorded on the schedules that use them in P1.
- **Documents.** Migration 000005 adds `document` and immutable `document_version` (§11.4). A
  journal is created as a draft at version 1; posting and reversing lock the document first (ENG-47
  step 1), and a `document_guard` trigger allows only draft → posted → reversed. Edits that create
  new versions with reversal and replacement entries (FR-GL-12) come with the P1 journal screens.
- **ENG-02's test hook.** `journal.Service.AfterPosting` runs inside the unit of work after the entry
  and the document change; p0-audit-chain writes its audit event there, and the ENG-02 test makes it
  fail to prove that nothing is saved.
- **ENG-04.** The balance service reads only the materialised balances, which the database proves
  equal to the lines (INV-08). An architecture test allows `UnitOfWork.Balances()` only in the
  balance service, the adapter and the conformance suite, and no adapter query sums journal lines.
- **Golden data.** The sample company (`make seed`: seed 1, 24 journals) is posted through the journal
  service, with the first journal reversed; the trial balance from the balance service matches totals
  computed from the seed's own decimal strings in integer cents, and is pinned in
  `internal/adapters/postgres/testdata/sample-company-trial-balance.golden` for the oracle.
- **Task 3.1 stays open** until p0-python-oracle-differential runs the oracle on the same golden data
  and agrees to the cent.
- **Mutation testing** runs as `make mutation` (part of `make ci`) and its own CI job; reviewed
  survivors are in `docs/quality/mutation-survivors.md`. gremlins 0.6.0 is pinned in `mise.toml`.
