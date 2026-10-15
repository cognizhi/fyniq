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

## Implementation notes (15 Oct 2026)

- **Who wrote what.** A separate Claude Code session wrote the oracle (#16) from §10, §11.3, Appendix B
  and the workbook's `reference.py`, without reading the engine (its PR records this). The engine's
  session wrote the engine side of the harness (#14, #18) and did not change the oracle.
- **The gate** is `TestTheEngineAgreesWithTheOracle` in `make test` (the required check): every golden
  scenario and 60 generated with a fixed seed. `make differential` (in `make ci` and its own CI job)
  searches 1,000 more with Hypothesis and saves a shrunk disagreement as a golden case.
- **Proved on 15 Oct 2026** by draft pull request #19, which planted half-even rounding in `RND-FX`: the
  required `make test` check failed on the gate, the differential job shrank the case to one journal
  (BHD 0.001 at 2025), and the merge was blocked. The pull request was closed unmerged.
- **The oracle's open questions** (test/differential/README.md, "Choices where PRD-001 is silent") are
  not generated, so the gate does not hinge on them; they need a PRD decision before P1.

