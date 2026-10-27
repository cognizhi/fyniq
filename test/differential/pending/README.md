# Pending golden scenarios (QA-ENG-02)

Golden cases for the P1 sources, in the `fyniq-scenario@1` format, waiting for the engine's
`cmd/scenario-run` to read their shapes. Until it does, they live here and not in `golden/`: the Go
gate (`make test`) posts every file in `golden/` through both sides, and the engine would refuse
these as invalid input.

As in `golden/`, only inputs live here. Each figure is worked out by hand in
`oracle/tests/test_pending_golden.py`, which the oracle must match today (`make oracle-test`). When
an engine session teaches `cmd/scenario-run` a source's shapes, it moves that source's file to
`golden/`, adds the source to `FYNIQ_DIFFERENTIAL_SOURCES` in the Makefile's `differential` target,
and settles any disagreement from the PRD (README, "P1 readings"); an engine bug is a failing golden
case, and the oracle never changes to match the engine.

| File | Source | What |
|---|---|---|
| `bank.json` | Bank lines and transfers (`p1-bank-import-reconcile` 4.6) | Input GST out and output GST in under `RND-GST-INCL`; a spread over 3 months with one release still to come at `as_of`; a USD line split in two whose `RND-FX` cent goes to 7030; GST on a USD line from its base amount; a same-currency transfer; a Wise SGD-to-USD conversion with a fee; a part to suspense; a part on a money account and a spread on an asset, both refused |
