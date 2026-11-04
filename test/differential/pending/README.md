# Pending golden scenarios (QA-ENG-02)

Golden cases for the P1 sources and the statements, in the `fyniq-scenario@1` format, waiting for the
engine's `cmd/scenario-run` to read their shapes. Until it does, they live here and not in `golden/`: the Go
gate (`make test`) posts every file in `golden/` through both sides, and the engine would refuse
these as invalid input.

As in `golden/`, only inputs live here. Each figure is worked out by hand in
`oracle/tests/test_pending_golden.py`, which the oracle must match today (`make oracle-test`). When
an engine session teaches `cmd/scenario-run` a source's shapes (or the statements), it moves that file to
`golden/`, adds the source (or `statements`) to `DIFFERENTIAL_SOURCES` in the Makefile (which `make differential` passes as `FYNIQ_DIFFERENTIAL_SOURCES`),
and settles any disagreement from the PRD (README, "P1 readings"); an engine bug is a failing golden
case, and the oracle never changes to match the engine.

| File | Source | What |
|---|---|---|
| `statements.json` | The statements (`p1-reports` 1.5) and the KPIs (`p1-dashboard-kpis` 1.3) | A small company with every section of rev 0.159's layout, over four months: a director's loan, a laptop bought with GST and sold at a gain, an invoice on credit and its part-payment, costs by function (one with none), rent a director paid personally and part of the loan turned into a capital contribution (both non-cash), depreciation and amortisation in one report group, prepaid insurance, money paid ahead, a grant, interest repaid with the loan, income tax, a dividend, a reversed cost; percentages of exactly half a tenth (-16.65, 83.35); a month with no entry; and 50.00 paid to an account with no cash-flow line, which both checks catch. Its KPIs over a calendar year, with counts entered for three months and headcount for three: a subscription stream, a month with no new customers, no counts in March, and runway not burning from February |
