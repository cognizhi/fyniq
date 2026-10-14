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
