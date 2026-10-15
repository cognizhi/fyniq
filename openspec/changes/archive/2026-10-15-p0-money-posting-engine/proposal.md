# Proposal

## Why

Every figure Fyniq shows comes from the posting engine, and no figure may pass through floating point
(golden rule 1). P0 builds the money type and the engine's core so that P1's posting sources plug into a
tested, deterministic heart (PRD-001 §10.1, §10.3, §10.5).

## What Changes

- A `Money` type: an exact decimal (cockroachdb/apd v3) with an ISO currency, no floating-point path,
  no arithmetic across currencies without a conversion, and base amounts computed once (ENG-10, ENG-11,
  ENG-13, ENG-14); input precision bounded at the API edge (ENG-44); decimal strings in JSON (ENG-12).
- Named rounding policies from §10.4, starting with those the P0 posting source uses, each a function
  the engine calls by name.
- The posting engine as a pure function: document in, journal lines out, with no clock, database or
  network inside (ENG-01). The unit of work persists its output in the document's own transaction
  (ENG-02), with entry and line provenance (ENG-03) and one source of truth per figure (ENG-04).
- Validation in the domain layer with stable error codes and plain messages (ENG-22, golden rule 13).
- The first posting source: the manual journal. The workbook's other posting sources are P1 (§18).
- Property tests with `rapid` and mutation tests with `gremlins` scoped to the engine, money and
  rounding packages (QA-ENG-03, QA-ENG-04).

## Capabilities

### New Capabilities

None.

### Modified Capabilities

None (`skip_specs: true`). It builds `ledger/money-and-rounding` (ENG-10…14, ENG-44),
`ledger/posting-engine` (ENG-01…04) and `ledger/validation` (ENG-22), and starts QA-ENG-03 and
QA-ENG-04 in `quality/calculation-correctness`.

## Impact

- `internal/domain/money`, `internal/domain/ledger`, `internal/domain/rounding`.
- Depends on p0-database-tenancy-migrations (unit of work, ledger revision, ENG-46, ENG-47).
- **Second human reviewer required** (AGENTS.md: money and the ledger).
