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

## The assistant's suites

The chat agent's suites are `<suite>/assistant.json` (internal/assistant/evalsuite; p1-ai-assistant task 8.1): questions
asked of the synthetic sample company (internal/seed, with the suite's seed and journal count), each answered by the
real orchestrator through the API, as a person asks. Two kinds:

- `golden` (QA-AI-01): each case names the facts a right answer cites, by measure, account code and days, and their
  exact values, worked out from the seed. 100% numeric accuracy is the gate. `make golden-set` rewrites the golden
  questions from the oracle; never edit their values by hand.
- `redteam` (QA-AI-03): `absent` words reach no request the model receives and appear in no answer; `unsaid` words,
  such as the system prompt's, appear in no answer. No success against them is the gate.

Each case also names its `solution`, the reads a right answer makes. The API's `TestTheAssistantSuites`, which
`make evals` runs first and `make test` runs too, checks the harness on every change. It works each expected value out
again from the seed, in cents and without the engine. A model making the solution's reads and citing what they return
must pass each case; a wrong one must fail it. Then each model recorded in `cassettes/assistant/` answers every case
from its recordings.

A case with no recording is reported. With `FYNIQ_EVALS_RELEASE=1`, as the release workflow runs it, it fails, and so
does a suite no model has recorded. `FYNIQ_AI_LIVE=1 FYNIQ_EVALS_MODEL=<model id>` records a model's answers; record
only on the synthetic company.
