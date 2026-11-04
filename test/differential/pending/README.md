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

No case waits here now: `cmd/scenario-run` reads every P1 source and states the statements and the KPIs (4 Nov 2026).
A new source's golden case starts here, as these did.

| File | Source | What |
|---|---|---|
