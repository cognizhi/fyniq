# AI evals

The AI eval suites (PRD-001 §16.2, QA-AI-*, AI-78) live here, one directory per suite, each a `suite.json`: its
agent, version, threshold (the share of cases that must pass) and its cases, synthetic text only, each with one
expectation (`contains`, ignoring case; `equals`, trimmed; or a `matches` pattern). The `evals` Go package embeds
them for the qualification of a model on a connection (`POST /v1/ai/connections/{id}/models/qualify`).

`make evals` (CTL-AI-05) checks each suite judges soundly (its expected answers pass, an empty answer fails), then
replays every model's recorded answers in `cassettes/` against it, offline and with no credential (DEV-02), and writes
`.data/evals/report.json`. A recorded run below its threshold, or a recording that is not whole, fails it.
`FYNIQ_AI_LIVE=1 make evals MODEL=<model id>` runs one model live on the suites' synthetic text and records its
answers for the next replay; never record anything from real organisation data.

Only the `probe` agent's suite ships with p1-ai-providers; each agent's own suites (QA-AI-01…03) arrive with it.
