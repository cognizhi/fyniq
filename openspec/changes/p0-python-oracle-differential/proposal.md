# Proposal

## Why

Two implementations must agree (golden rule 3). An independent reference ledger, written from the rules
rather than from the engine's code and in a different language, catches errors that one implementation
checking itself cannot (PRD-001 §16.1, QA-ENG-01, QA-ENG-02).

## What Changes

- `oracle/`: a Python package that computes journal lines and the trial balance from the rules in
  PRD-001 §10 and Appendix B, using Python's `decimal` with explicit contexts. It starts from the
  workbook's `tests/reference.py` (in the `zh-sheet-accounting` repository), ported and extended.
- A shared scenario format: JSON documents and parameters that both the engine and the oracle read.
- A differential harness: for every golden data set and thousands of generated scenarios per CI run,
  the engine's and the oracle's trial balance, subledgers and report figures must be equal to the cent.
  Any difference blocks the merge (QA-ENG-02).
- Hypothesis strategies that generate awkward scenarios: many currencies, long journals, extreme
  amounts at the precision boundary, reversals.
- Golden data sets from the sample-company generator (DEV-04, QA-ENG-07).

## Capabilities

### New Capabilities

None.

### Modified Capabilities

None (`skip_specs: true`). It builds QA-ENG-01 and QA-ENG-02 in `quality/calculation-correctness` for
P0's posting source, and the harness every P1 posting source extends.

## Impact

- `oracle/` (Python, pinned by `mise`), `test/differential/`, `make test` and `make ci`.
- No database: the oracle is a pure computation.
- **Process rule:** a different person or agent session from the one that writes the engine writes the
  oracle, from §10 and Appendix B only, and does not read `internal/domain/ledger`. The pull request
  says who wrote it and confirms this.
