# Design

## Context

QA-ENG-01 requires the reference ledger to be written from the rules, not translated from the engine,
in a different language. Python is already the oracle's language (ADR-T1). The workbook's
`tests/reference.py` encodes the rules the workbook was tested against.

## Goals / Non-Goals

**Goals:**
- Independence: no shared code, no shared decimal library and no shared rounding implementation with
  the engine.
- A harness that every P1 posting source extends with its own scenarios.

**Non-Goals:**
- Running in production. The oracle is a test tool only.

## Decisions

- **Scenario JSON as the only interface.** The engine reads it through a test-only command
  (`cmd/scenario-run`, built with the `local` tag) and the oracle reads it directly. Each writes its
  results as JSON, and a comparator diffs them to the cent. Alternative: calling the oracle from Go
  through cgo or an embedded interpreter, rejected because it would couple the two.
- **Python `decimal` with explicit contexts.** Each rounding step names the PRD policy it implements, so
  a disagreement points at a policy.
- **Hypothesis for generation.** Shrinking turns a failure into the smallest disagreeing scenario, which
  is saved as a new golden case.
- **Authorship rule enforced by review.** CODEOWNERS on `oracle/` names a different owner from
  `internal/domain/ledger` once a second engineer joins. Until then, the oracle is written in a separate
  agent session that is given only §10, Appendix B and `reference.py`.
- **Audit events / ports.** None: test tooling only.

## Risks / Trade-offs

- [Both implementations misread the same rule] → the other two oracles in §16.1 (workbook parity and the
  hand-pinned statutory figures) and the accountant's review of the posting rules (Area 1 review).
- [Slow CI from thousands of scenarios] → a fixed-seed sample in `make test` and the full count in
  `make ci`.
