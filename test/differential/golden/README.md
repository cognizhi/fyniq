# Golden scenarios (QA-ENG-02)

Inputs in the `fyniq-scenario@1` format that both the Go engine (`cmd/scenario-run`) and the oracle
(`oracle/`) post; their results must agree to the cent. Only inputs live here: expected figures
come from the two implementations agreeing, or are worked out by hand in a test, never by running the
engine alone.

| File | What |
|---|---|
| `sample-company-seed-1.json`, `sample-company-seed-2.json` | Generated from the sample-company generator (DEV-04) by `go test ./internal/differential -update`; never edit them by hand. Seed 1 is what `make seed` writes; the first journal is reversed |
| `multi-currency.json` | Hand-built: USD, JPY and KWD lines under `RND-FX` (including a negative half cent), and every reason a journal or reversal is refused. Its figures are worked out by hand in `TestEngineOnTheHandWorkedScenario` |
| `*-shrunk-*.json` | Scenarios a failing differential run shrank to, saved as new golden cases (task 3.1) |
