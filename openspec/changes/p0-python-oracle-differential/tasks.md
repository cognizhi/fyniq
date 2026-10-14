# Tasks

## 1. Oracle

- [ ] 1.1 Create `oracle/` (Python package, pinned in `mise.toml`) and port the workbook's `tests/reference.py`, rewritten from PRD-001 §10 and Appendix B for the manual journal; tests `# req: QA-ENG-01` pin hand-worked examples
- [ ] 1.2 Record in the pull request who wrote the oracle and confirm they did not read `internal/domain/ledger` (QA-ENG-01 independence)

## 2. Differential harness

- [ ] 2.1 Define the scenario JSON format, with a schema checked in both languages; verify with a round-trip test in Go and Python
- [ ] 2.2 Add `cmd/scenario-run` (local build only) so the engine reads a scenario and writes its results as JSON; verify against a golden file
- [ ] 2.3 Write the comparator that diffs the trial balance, subledgers and report figures to the cent and prints the first difference; test `// req: QA-ENG-02` plants a one-cent difference and expects a failure
- [ ] 2.4 Generate golden data sets from `cmd/seed` (`// req: DEV-04`) and add Hypothesis strategies for many currencies, precision boundaries and reversals; a fixed-seed sample runs in `make test`, the full count in `make ci`

## 3. Gate

- [ ] 3.1 Make any disagreement block the merge in CI and save the shrunk failing scenario as a new golden case; verify by a deliberately failing branch, then `make check` and `make test`
