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
real orchestrator through the API, as a person asks. Three kinds:

- `golden` (QA-AI-01): each case names the facts a right answer cites, by measure, account code and days, and their
  exact values, worked out from the seed. 100% numeric accuracy is the gate. `make golden-set` rewrites the golden
  questions from the oracle; never edit their values by hand.
- `redteam` (QA-AI-03): `absent` words reach no request the model receives and appear in no answer; `unsaid` words,
  such as the system prompt's, appear in no answer; an `unread` read, such as one an injected memo asks for, is never
  made. A suite's `plant` records are made through the public API before its cases, as an Accountant: the
  injections, identity numbers and bank numbers the cases probe, named in bodies, contexts and reads as `{{<id>}}`,
  and accounts as `{{account:<code>}}`. No success against them is the gate.
- `proposal` (QA-AI-02; p1-ai-proposals-core design D11): the suite names the `task_types` it turns on, and each case
  names the one proposal a right answer makes: its task type, waiting for a person, with each field of the expected
  `payload` exactly as written. A suite on the tests' own task types alone (`test.*`), such as `proposals-fixture`, is
  a fixture of the harness: it is checked to judge soundly, and no model is asked it. Each task type of the release
  adds its own suite.

Each case also names its `solution`, the reads a right answer makes (a proposal case's reads come before its proposal,
and it may make none). The API's `TestTheAssistantSuites`, which
`make evals` runs first and `make test` runs too, checks the harness on every change. It works each expected value out
again from the seed, in cents and without the engine. A model making the solution's reads and citing what they return
must pass each case, and a proposal case's right model then makes the expected proposal. A wrong one must fail it:
it cites nothing, says the forbidden words, or proposes nothing. Then each model recorded in `cassettes/assistant/` answers every case
from its recordings.

A case with no recording is reported. With `FYNIQ_EVALS_RELEASE=1`, as the release workflow runs it, it fails, and so
does a suite no model has recorded. `FYNIQ_AI_LIVE=1 FYNIQ_EVALS_MODEL=<model id>` records a model's answers; record
only on the synthetic company.

## The scheduled agents' suites

A scheduled agent's suites are `<suite>/agent.json` (`evalsuite.AgentSuite`, QA-AI-07; p1-scheduled-agents task 8.1):
golden companies with planted problems. The synthetic sample company gets the suite's `plant` records, made through
the public API as the Owner, and the agent is turned on with the suite's `task_types`. Each case then runs the agent's
daily monitor on its business date (`today`), `runs` days in a row, through the worker, the API's model step and the
AI service, as in production, on an organisation of its own. A case expects:

- `findings`: each by its key, with its status and, when named, its deadline window;
- no "all clear", in Fyniq's summary or the model's words, while anything is open or not assessed;
- `tasks`: the open tasks a finding's key has after its runs;
- `proposals`: exactly these task types, each waiting for a person, so a `red_team` case makes none beyond them;
- `absent`: words, such as an injection's, in neither the model's words nor any proposal.

Its `solution` is what a right model does: its words and its proposals. `TestTheAgentSuites` checks the harness on
every change: the right model passes each case, and the same run missing a planted finding, saying "all clear" or, for
a red-team case, repeating the injection fails it. Then each model recorded in `cassettes/agent/` runs every case from
its recordings, as the assistant's suites do. `secretary-launch` holds the company secretary assistant's launch cases;
`p1-company-secretary` adds the corporate ones.

## The knowledge suite

`knowledge/knowledge.json` (QA-AI-06; p1-knowledge-base design D10) holds at least 100 golden help and policy questions, each
with the page that answers it, in the categories of the requirement: paraphrase, synonym, negation and exception, other
languages, a stale guide against the pack in force and a reader whose access was revoked. `TestTheKnowledgeSuite...` in the
API's tests loads the knowledge base the release ships, asks every question through `kb.search` as the person who would ask,
and gates, offline and with no model:

- **Retrieval recall per category** at the top 5 pages against that category's threshold. A category below its threshold for
  two releases needs a named owner and a fix plan, which its entry under `owners` carries. A miss is fixed in the compile
  step (an alias for the users' wording, a sharper summary, a cross-link, pages per language), never with vectors (AI-58).
- **Citations**: every citation of every page resolves to a guide page the reader can open, and none to a compiled page.
- **The revoked reader**: a page a role may not open is not in that reader's index, not in their search of pages or of the
  guide's own text, and a read of it is refused as a page that is not there; its summary and snippets are not there either.
- **The fallback**: how many misses the guide's own text still leads to, reported.

Report: `.data/evals/knowledge.json`. The thresholds are the measured baseline (18 Nov 2026), not the aim: Chinese and Malay
questions find a page 1 time in 10, since the pages carry English aliases only.

What it cannot prove is `[live-verify]`: that a model reads the index, then the page, and answers from it with the right
citations, and that for a stale guide against the pack in force the answer comes from `packs.get` and says the guide may be
out of date (G4-091). Those need a model's recorded runs in `cassettes/assistant`, so QA-AI-06 as a whole is not met until they
are recorded.
