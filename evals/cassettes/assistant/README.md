# The assistant's recordings

The recorded answers of each model the assistant's suites (`evals/<suite>/assistant.json`) were run on, one
`<key>.json` per model request, keyed as `evals/cassettes/` keys them. `TestTheAssistantSuites` replays them. A case
with no recording is reported; with `FYNIQ_EVALS_RELEASE=1` it fails. Record with `FYNIQ_AI_LIVE=1
FYNIQ_EVALS_MODEL=<model id>` on the synthetic sample company only (DEV-08). None are recorded yet
[live-verify: before the first release].
