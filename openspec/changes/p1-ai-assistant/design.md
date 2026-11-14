## Context

- `p1-ai-providers` (archived 14 Nov 2026) built the model layer in the API's and the worker's processes: the
  `ModelProvider` port and its adapters, connections, the data-class policy, routes per agent with the `chat` agent
  already listed, the router (`internal/ai.Router.Run`, streaming, failover, budgets, the AI switch), qualification,
  the activity log of model calls and the `Screening` port (Model Armor adapter, local stub with the red-team corpus).
- `cmd/ai` is a shell. The AI's database role, `fyniq_ai`, holds no grant at all (CMP-AI-01; `TestAttackTheAIRoleCannot
  ReadOrWrite`), and the AI tool registry the archtest `TestTheAIToolsAreReadOnly` checks does not exist yet.
- The API already serves the figures the assistant needs: the statements (`/v1/reports/statements/{kind}`), the
  general ledger, budget against actual, schedules, cash position and projection with its what-if, KPIs, checks,
  statutory packs, ledger entries and every record's read operations, all under RLS and Appendix D.
- `authn.Principal` has an `agent` class: the API audits its calls as the AI on behalf of the person
  (`internal/api/api.go`, AUD-01), and agent-class callers are proposal-only (AI-34).
- The owner's decisions of 14 Nov 2026 (PRD-001 rev 0.277) shape it: the browser reaches the assistant through the API
  with a delegated read token per turn; the orchestrator's model calls go through the API's router (D11 (a) of
  `p1-ai-providers`); each eval suite comes with what it evaluates; the assistant's own rules (AI-P1, AI-P2, AI-P6,
  AI-P7, AI-P8, AI-08, the orchestrator's) are built here.

## Goals / Non-Goals

**Goals:**
- One tool loop, one registry and one set of guardrails that the scheduled agents and the company secretary reuse.
- Every figure shown comes from a typed fact a tool returned in that turn, rendered by platform code.
- The AI service can be compromised without exposing a key, a write or another person's data.

**Non-Goals:**
- Proposals, the `propose` tool, the Prepare lane and "Save as proposal" (`p1-ai-proposals`); the knowledge-base tools
  and how-to answers (`p1-knowledge-base`); MCP (`p1-mcp-first-party`); scheduled agents (`p1-scheduled-agents`).
- A SQL sandbox (AI-03's P2 option), embeddings or vector search (AI-58).
- Nemura's assistant (PRD-002 NM-AI-*), which reuses this loop later.

## Decisions

**D1. Reached through the API (rev 0.277; FR-INT-06, golden rule 4).**
- `/v1/assistant/conversations` (create, list the person's own, read one with its turns, delete their own), `POST
  /v1/assistant/conversations/{id}/turns` (a question and its optional context, FR-UX-17), answered as server-sent
  events (status, text, tool progress, facts, the grounded answer, the end with its usage) and kept as the turn; `POST
  .../turns/{turn_id}/feedback` (QA-AI-05). Each is in the OpenAPI contract and on the never-list for MCP and agent
  clients (§9.6).
- Admission in the API before anything reaches the AI service: the AI switch (`features.ai_assistant`), the person's
  rate limit (D9), the conversation's length (D9); the router then applies budgets and routes per call.
- Alternative: the load balancer's `/ai` route straight to the AI service (the PRD's earlier drawing). Rejected by the
  owner: a second public surface with its own sign-in, CSRF and limits, outside the reviewed contract.

**D2. A delegated read token per turn (AI-01, AI-36, AI-34).**
- The API mints it for the turn: an Ed25519-signed token (issuer and audience the API's own; the key in the secret
  store, rotated) naming the person, the organisation, the turn, the AI tools' scope and an expiry of at most 5 minutes,
  `jti` single-turn. The API verifies it as a principal of class `agent` with the person's roles, so its calls are
  audited as the AI on behalf of the person (AUD-01) and any write is refused (AI-34); the session never leaves the API.
- Alternatives: Hydra's JWT-bearer grant, as the MCP spike does (`internal/mcpdelegation`): a network hop and a client
  registration per turn for a token the API both issues and verifies; forwarding the session: token passthrough,
  refused by AI-36.
- As built (task 1.1): the token is opaque, not signed: `fyai_` and the organisation's ID in the clear followed by 32
  random bytes, of which `ai_grant` (000366) keeps only the SHA-256, with the person, the turn, its issue and its expiry
  (at most 5 minutes) and its end. The API must look the grant up anyway, to end it with its turn, so a signature would
  add a key to keep and rotate and nothing else. `ai_grant_for` resolves it, in the organisation it names, to the person
  and their live roles there (as `session_for` does); `authn.AIGrants` makes it an `agent`-class principal with the
  scope `ai.read` and the turn; the API refuses it anything but a read (`aiReadable`), on top of the AI column; the
  release and local builds route only `fyai_` tokens to it, never at consent. Grants are left out of the tenant export.

**D3. The orchestrator in `cmd/ai`, stateless and keyless (AI-P2, "The orchestrator holds no write credential",
AI-28).**
- `internal/assistant` runs the tool loop: it builds the context (the system prompt from `contracts/ai/prompts/`, the
  conversation's earlier turns as the API sends them, the context chip's record), asks the model through the API, runs
  each tool call through the API with the delegated token, applies the guardrails (D7, D8) and streams events back.
- It has no database access (no DSN in its configuration; `fyniq_ai` keeps no grant) and no provider key: everything it
  reads or calls goes through the API. The API reaches it on an internal address only (Cloud Run internal ingress with
  IAM invoker in the cloud `[live-verify]`; the Compose network locally), and it calls back on the API's internal
  listener (D4).
- Alternative: run the loop inside the API process: rejected, as the API holds write credentials (SEC-03, golden rule 5).
- As built (task 5.1): `internal/assistant` and `cmd/ai`. `POST /turns` takes the turn's delegated token, the question,
  what it is about and the earlier turns, for the API alone (its service token of the class `assistant`: local HMAC, or
  Google's for the api's account in the cloud `[live-verify]`), and streams `status`, `tool`, `text` and `done`, or
  `error` in the API's words. The loop asks `callAIModel` for the `chat` agent with `contracts/ai/prompts/chat@1.md` and
  the registry's tools by name, runs each tool call through `callAITool` (calc it runs itself, on the facts the turn
  read), gives the results back as tool results under their calls, and stops at 8 reads with a plain message (task 5.3
  makes the limits settings). The answer keeps every fact the turn read, the prompt's name and SHA-256 (AI-24), the
  model calls' runs and each tool call's outcome, for the turn's record (task 6.1). What it sends a model and what it
  answers pass the output filter again (AI-20). The AI service's own token to the API is local HMAC, or Google's for the
  internal API's audience as the ai invoker (FYNIQ_GCP's `ai` and `api` entries) `[live-verify]`. The internal tool
  operation now widens the turn's classes by what each read carries (a record or a party's figure may name a person:
  personal data; a statutory value: know-how), so the turn's later calls are routed for them (AI-83). An
  architecture test keeps the database, the wiring, the API, the router and the model connectors out of `cmd/ai`.

**D4. The AI service's calls into the API.**
- An internal listener of the API (its own port, never behind the load balancer) serves two operations for the AI
  service's service identity only, each also carrying the turn's delegated token: `model-call` (agent, classes,
  messages, tools, stream) runs `ai.Router.Run` as the person, so the reservation, the call, the record and the AI
  switch are the router's (D11 (a)); `tool` runs one registry tool as the person (D5). Its contract is
  `contracts/openapi/fyniq-internal.v1.yaml`, generated from the code beside the public one.
- Alternative: the public `/v1` operations for tools too: possible for most, but the internal operation applies the
  registry's limits (D6) in one place, and the model call has no public form.
- As built (task 1.2): Cloud Run gives a container one port, so the internal API is a second huma API on the API's own
  port under `/internal/v1`; Caddy and a Cloud Armor rule refuse `/api/internal/` at the edge. Each call needs the AI
  service's service token (`X-Fyniq-Service-Token`, the `TaskTokens` port's class `ai`: HMAC locally, Google's OIDC
  tokens for an `ai` invoker in the cloud `[live-verify]`; without that invoker every call is refused) and a live turn's
  token. `callAIModel` and `streamAIModel` (server-sent events) run the router for the `chat` agent as the turn's
  person. They route for the union of the classes the AI service names and those the turn's grant has read (000367: the
  grant's `classes` only widen while it is live), and record the named ones, so the AI service can never send what the
  turn read to a step the policy keeps it from (AI-83). The request carries no model: the route chooses it.

**D5. One read-tool registry (AI-49's first part, AI-01, AI-P1, AI-03, AI-68).**
- `contracts/ai/tools/<name>.yaml`: name, description, input JSON Schema (`additionalProperties: false`), output
  shape, limits, scope and version; it generates the model's tool definitions and, later, MCP's. Launch tools:
  `metrics.query` (the semantic layer), `records.get` and `records.search` (allow-listed record types and columns),
  `lineage.explain` (a fact's or a report cell's entries and source documents), `checks.list` and `checks.explain`,
  `packs.get` (statutory values in force on a date, with pack key and version), `calc`. `kb.*` (p1-knowledge-base) and
  `propose` (p1-ai-proposals) are added by those changes. None writes (`TestTheAIToolsAreReadOnly` then checks real
  definitions).
- `calc` (AI-P6) runs in the AI service itself, exact decimals (apd), on facts by ID and constants, returning its result
  as a fact with its input facts' IDs; every other tool reads through the API.
- As built (task 3.1): one `contracts/ai/tools/<name>.json` per tool, in the shape MCP lists a tool (name, title,
  description, `inputSchema`, `annotations`), with its version, where it runs (`api`, or `ai` for `calc`), its scope
  (`ai.read`, the only one) and its limits (at most 50 rows, 5 s and 4,000 tokens a result). JSON rather than YAML: the
  read-only test on main already read JSON, MCP's `tools/list` is JSON, and the release binary needs no YAML parser.
  `contracts/ai` embeds the files; `internal/aitool` loads them and refuses, so the build fails, a tool that is not
  annotated read-only, idempotent and closed to the world, holds another scope, is named for a write, or takes an
  input that is not strict and bounded (every object closed, every string with an enum, a pattern or a length, every
  array with a length, only the JSON Schema words every provider takes, no `$ref`). The model is offered a tool as
  `metrics_query` (providers' tool names take no dots) and the registry finds it again by that name.
  `contracts/ai/tools.lock.json` pins each version's hash, so a changed definition without a new version fails `make
  test`. The internal model call names its tools and the API offers the registry's own definitions; one the AI service
  writes is refused unsent. A contract test keeps `metrics.query`'s input equal to `POST /v1/metrics:query`'s and the
  semantic layer's measures, `records.search`'s kinds equal to the search's, `checks.list`'s statuses equal to a
  check's, and `packs.get`'s packs equal to those in `packs/`. `lineage.explain` takes a fact's ID with the measure,
  months, dimension, member and revision it came with, so the API can rebuild the fact and refuse an ID that does not
  match, with nothing kept between calls.
- As built (task 3.2, first part): `callAITool` (`POST /internal/v1/tool-calls`) checks the input against the tool's
  schema and calls the tool's public read operation in process with the turn's own delegated token, so the person's
  roles, the masking and the AI's read-only gate apply as they do to any client: `metrics.query`, `records.get`,
  `records.search`, `checks.list` and `checks.explain`. A refusal comes back as AI-TOOL-03 with the operation's own words,
  for the model to read. `packs.get` and `lineage.explain` follow in the second part; `calc` runs in the AI service.
  Building it found that the AI column's "Proposal" was read as no view, which kept the assistant from every invoice,
  bill, journal, bank line and claim: the owner decided on 15 Nov 2026 (rev 0.278) that Proposal (and Proposal (draft))
  is a view of those rows within the person's rights and nothing more; a proposal of a named kind (evidence's
  `doc.attach`, a budget's scenario, a timesheet's draft invoice) is that proposal only, so the AI still opens no
  evidence file, a person's own bank account's included (§9.6).
- As built (task 3.2, second part): `packs.get` reads `GET /v1/statutory-values` (`getStatutoryValues`, a new public read
  on "Statutory parameters", API-first): a pack's version in force on a day for the organisation (its active one, else
  the release's, ENG-31) and each table's rows whose dates cover the day, with the sources they cite. Each value is a
  fact naming the pack, the version, the table and the row (`sg-gst.limits.registration_threshold.amount`), its dates
  and its sources' titles (AI-53); a row's first column, a date, a box number and the source are not figures.
  `lineage.explain` (version 2) asks `metrics.query` again with what the fact came with, refuses an ID that is not among
  the answers, and reads the report the fact links to, as the person, within the limits, the fact first: the general
  ledger's lines for an account's fact, the sub-ledger for a customer's or vendor's, the statement or the KPIs otherwise.
  Building it found that a fact's link named the general ledger's account `account=` and the revision `revision=`,
  which the reports do not take (task 2.1's slip); the links now use `account_id` and `as_at_revision`, and a test opens
  every kind of fact's link.
- As built (task 3.3): `aitool.Calc`, which the AI service runs (the API's `callAITool` refuses `calc`): sums,
  differences, products, quotients, percentage changes (from the first, of its size) and shares, in exact decimals (apd,
  60 digits) on the conversation's facts by ID and on plain decimal constants, rounded once by RND-DISPLAY (half away from
  zero, 2 places by default; a calculated figure is shown, never posted). Amounts combine in one currency only, a product
  holds one amount at most, and a constant divided by an amount is refused; a fact the conversation did not read, or one
  without a value, is refused (CALC-01 to CALC-04, each with what to do). The result is a fact (`calc.<op>`) with the IDs
  of its input facts (`inputs`, AI-68), the latest revision among them, and their period when they share one.

**D6. The semantic layer and typed facts (AI-03, AI-68, AI-04).**
- `internal/semantic` defines named measures (revenue, cost of sales, gross profit, operating expenses, net profit,
  net burn, cash, runway in months, receivables and payables outstanding and overdue, GST payable) and dimensions
  (month, quarter, financial year, account, customer, vendor, department, currency), each compiled by platform code to
  the balance service (ENG-04) or the report that already computes it, so a measure equals the statement line it names;
  `POST /v1/metrics:query` exposes it (FR-INT, API-first).
- A fact: ID (`F` + a hash of the measure, dimensions, filters, period and ledger revision), measure, dimensions and
  filters, period or date, value as a decimal string, currency and unit, sign convention, ledger revision (ENG-46) and
  its `lineage.explain` link. Counts, dates and percentages are typed facts too; statutory values carry pack key and
  version.
- Limits per tool call (AI-04): a row cap (50), column allow-lists per record type, a statement timeout (5 s), and a
  token budget per result (a larger result is summarised by the tool, with the facts kept and the rest counted).
- As built (task 2.1): `internal/semantic` and `POST /v1/metrics:query` (Reports, an unqualified view; by customer it also
  needs a view of invoices, by vendor of bills). Organisation measures come from the KPIs' month figures (`kpi.Read`,
  exact) and values (`kpi.Compute`, rounded by RND-KPI, so a KPI fact is the KPI page's); flows sum over the months,
  stocks are at the last month's end, a KPI is one month's. `account_movement` and `account_balance` come from the trial
  balance; receivables by customer and payables by vendor from the sub-ledgers at the last month's end, in the base
  currency; revenue, gross profit, EBITDA, net profit and costs by department from the profit and loss's pivot, with a
  fact for the lines with none. A fact's ID is a hash of what it is and the revision. The query saves nothing, so it is
  exempt from Idempotency-Key and Prefer, and it is the one POST the assistant's token may call.
- As built (task 2.2, with 3.2's first part): `internal/aitool` makes every figure of a read a fact: a money object
  (`amount`, `currency`) is an amount, a whole number a count, decimal text a number; IDs, versions, line numbers and
  codes are not figures. A record's fact names the record (`invoice.total`, member the invoice's ID), the place in it
  (`lines[2].net`), the day it was read, the ledger revision and the operation's path; its ID is a hash of those. A
  result keeps at most its tool's rows of each list (50), then halves them until it fits the tool's token budget (4,000,
  at four characters a token), counting what it left out; one that never fits returns only that count. `records.get`
  keeps an allow-list of fields per kind of record (no contact details, addresses, tax IDs, the people who prepared or
  posted it, its emails, approvals or free-text notes). The tool's time limit (5 s) bounds the call's context, so its
  queries are cancelled with it.

**D7. Grounding and rendering (AI-08, AI-21, AI-69, AI-22).**
- The model answers in a structured output: segments of text and references `{fact: F…}` or `{calc: …}`; it writes no
  digits in text except dates and ordinals the schema allows. Platform code renders each figure from its fact, with
  RND-DISPLAY, and checks the sentence's attribution (measure, entity, period, currency) against the fact; an
  unmatched number or a wrong attribution regenerates the answer once with the failure named, then withholds the figure
  with what could not be established (G4-089, G4-090). A figure from an earlier turn keeps its fact ID or is read again.
- Output is a fixed Markdown subset rendered by the web app (paragraphs, lists, emphasis, tables of facts, links shown
  in full; no images, no HTML, no remote fetch) (AI-69); every AI message carries the violet accent and the AI badge
  (AI-22).
- Alternative: free text checked after the fact by matching numbers: weaker against right-value-wrong-fact (G4-089).
- As built (task 5.2): the model calls ask for a strict structured output (`answer`): segments of `text` (Markdown, no
  figure but a date or an ordinal), `fact` (an ID with the measure, member, first and last day and currency the
  sentence gives it) and `missing` (what could not be established). The assistant checks each fact segment against the
  fact the turn read (G4-089) and each text segment for a stray figure; a failure asks once more, naming what failed,
  and what still fails becomes "(I could not establish this figure)" or "(a figure not established)" (G4-090). Platform
  code renders each figure from its fact, rounded once by RND-DISPLAY: an amount in its currency's decimals with
  thousands separated and a true minus, a percentage and months to one decimal, a ratio to two, days and counts whole.
  The Markdown subset drops images and raw HTML and shows any link but Fyniq's own with its address in full (AI-69).
  The answer records whether it was asked for again, what failed, how much was withheld and the facts it cites. The
  prompt says so as `chat@2`; `chat@1` stays as its history.

**D8. Exclusions, untrusted text and screening (AI-02, AI-20, AI-18, AI-P7, AI-25, CTL-AI-06).**
- The exclusion policy runs in the API on every AI-principal read, before data leaves for the AI service: never-send
  fields (NRIC/FIN, full bank account numbers, secrets, authentication data, full home addresses) are removed or masked
  at the field level by a registry of sensitive fields per record type; payroll needs the new setting
  `ai.payroll_access` (off, dual controlled) and the person's payroll right; an Owner may add exclusions (dual
  controlled) and never remove the never-send list. An output filter in the AI service and the API catches NRIC and
  bank-number shapes in anything bound for the model or the person.
- Free text from outside (bank descriptions, email bodies, document text, counterparties' names) reaches the model as
  quoted `untrusted_text` in tool results, never in the system or user role, and is screened through the `Screening`
  port with prompts and answers; an incomplete screening fails closed (`ports.ErrScreeningIncomplete`).
- CTL-AI-06 runs in `make test` and weekly: planted NRIC, bank numbers and payroll in the sample company must reach no
  recorded model request; its report is evidence.
- As built (task 4.1): `internal/domain/aiexclusion` and a response transformer of the public API (`excludeForAI`,
  after the masking of an M right), so every answer to an AI principal is filtered, the read tools' included, as MCP's
  will be. The never-send fields go by name, or by a name ending in theirs (`client_secret`, `director_nric`): identity
  numbers, full bank account numbers, secrets and authentication data, home addresses, and an individual's address.
  Pay goes unless `ai.payroll_access` (a switch, off, dual controlled, off at once) is on and the person holds a view of
  "Pay runs, employee data" themselves. `ai.exclusions` (a new kind of setting, `fields`: field names separated by
  commas, dual controlled) adds the Owner's fields; the never-send list is code, so no setting takes a field off it. The
  output filter masks, in any text left, an NRIC or FIN whose check letter is right (S****567D) and a bank-number shape,
  9 to 18 digits whole or in groups split by hyphens or spaces (****6789); a decimal, a date, a word, a path, a UUID and
  a reference numbered within a year (2026-00012) are left alone. The AI service's own filter comes with it (task 5.1).
- As built (task 4.2): the API's `Screening` dependency (Model Armor from `FYNIQ_MODEL_ARMOR_TEMPLATE` in a release,
  as the service's account, `[live-verify]`: the template and its role are not provisioned yet; local mode's stub in
  the stack and the tests) screens three things, failing closed: the person's words and the tools' results in each
  internal model call before the router sends it, the whole answer before the call is done (a streamed answer ends in
  an error event instead, and the AI service discards the text it streamed), and each read tool's free text. A tool
  result quotes its free text as `{"untrusted_text": …}` (descriptions, memos, notes, names, references and the like;
  AI-18, AI-25: the organisation's own text too), screened with the names its facts carry; a finding withholds every
  quoted text and name, saying what was found, and keeps the figures. A finding in a model call refuses it
  (AI-SCREEN-01) and sends nothing; a screening that did not finish, or none set up, refuses either (AI-SCREEN-02).

**D9. Denial of wallet and limits (AI-23).**
- Per person: 20 turns a minute and 200 a day (settings, dual controlled); per turn: at most 8 tool calls and the
  route's output cap; per conversation: 40 turns, compacting the oldest into a summary that keeps their facts by ID
  past 20. Token and money budgets are the router's (AI-13, AI-86).
- As built (task 5.3): four settings, each dual controlled: `ai.questions_per_minute` (20), `ai.questions_per_day` (200,
  over the last 24 hours), `ai.reads_per_question` (8, 1 to 20) and `ai.conversation_turns` (40). `askAIConversation`
  admits a turn within them before keeping it (AI-LIMIT-01 and 02, 429; AI-LIMIT-03, 409, asking for a new
  conversation) and hands the AI service the reads it may make, which the tool loop stops at, answering in part. Past
  20 answered turns the oldest are summarised in one message, each question and the facts it cited by ID; the facts the
  conversation's answers cited go to the AI service with the question, so an answer may cite an earlier fact again and
  pass the figures' check (AI-08), and the turn keeps it with its own.

**D10. Prompts and models gated (AI-24, CMP-AI-03).**
- System prompts are versioned files in `contracts/ai/prompts/` with their hash recorded on every turn; a prompt change
  is a reviewed pull request whose `make evals` run must pass, as a route change already needs a qualified model.

**D11. Conversations, turns and the log (AI-12's turns, AI-P8, retention).**
- `ai_conversation`, `ai_turn` (question, context, answer segments, facts cited, status, prompt hash, the run of model
  calls), `ai_tool_call` (tool, version, inputs, an output hash, row counts, duration, outcome) and `ai_feedback`,
  append-only under FORCE RLS, written by the API's role in the turn's unit of work; the activity log lists turns and
  tool calls with their classes and masks (FR-PAY-13). The purge job removes conversations older than
  `retention.ai_conversations_days` (365) and records it (AUD-01).
- Audit events: `ai_conversation.created`, `.deleted`, `ai_turn.answered`, `ai_feedback.given`, `ai_conversation.
  purged`, setting changes for `ai.payroll_access` and the limits.
- As built (task 6.1): migration 000369 keeps the four tables append-only under FORCE RLS (FY341: a conversation is
  only deleted, once; a turn only answered, once; a tool call kept as it ran; feedback only reviewed, once; nothing
  removed but by the purge, which sets `fyniq.ai_purge` for its transaction in task 6.3), written by the API's role,
  read by the worker and the read-only role, none of it by the AI's; each is in the tenant export. The operations,
  under the tag Assistant, are on the AI activity log's row (V own for each member; "—" for the AI, so no AI or MCP
  client reaches them) and refuse anyone but a person signed in: `createAIConversation`, `listAIConversations`,
  `getAIConversation` (the turns, their figures rendered from their facts, the cited facts with their lineage),
  `deleteAIConversation` and `askAIConversation`, which streams `status`, then `answer` with the turn as kept, or
  `error`. Asking admits the turn (the AI switch, refused before a turn is kept), keeps it answering, issues its
  delegated token, hands the AI service the question, what it is about (FR-UX-17) and the last 20 answered turns,
  relays the AI service's status, and keeps the answer with its reads (each with a hash of what the model read), or
  the refusal's code, ending the token whatever happens to the stream. The API reaches the AI service at FYNIQ_AI_URL
  locally (Compose: ai:8080) and FYNIQ_GCP's `ai` entry in the cloud `[live-verify]`.
- As built (task 6.2): a turn keeps the classes of data it read, from its grant as it ends (000370). `listAITurnActivity`
  (`GET /v1/ai/activity/turns`) lists the turns newest first, each member their own and the Owner and the DPO
  everyone's: who asked, what, how it ended and its code, the prompt and its hash, the runs (which `listAIActivity`
  shows by `run`), the tokens, the classes, what grounding did, and each read with its outcome, its counts and the hash
  of what the model read, never its input. Someone else's turn that read payroll is masked (its question withheld) for
  a reader who may not see payroll unmasked, and an unmasked one is audited as `ai_activity.read` (FR-PAY-13, AUD-05).
  Settings › AI › AI activity shows them above the model calls, each with a link to its model calls.

**D12. The panel (FR-UX-17, screen `ai-assistant-panel`).**
- A side panel from the shell (⌘J / Ctrl+J; "Ask about this" on records, report cells and figures), docked, a sheet or
  full screen by device class; the context chip; streaming; citation chips that open the record, the report or the
  entry; Explain on a figure calls `lineage.explain`; Simulate runs the cash projection's what-if, read-only; the
  disabled, budget, excluded-data and tool-failure states; thumbs up or down with a reason; Expert mode shows the
  facts' accounts and query details. Prepare and "Save as proposal" stay hidden until `p1-ai-proposals`.

**D13. Evals (QA-AI-01, QA-AI-03, QA-AI-04, QA-AI-05; rev 0.277).**
- `cmd/evals` gains assistant suites: the golden questions (`evals/golden/`, at least 200 on the sample company, each
  with its expected facts computed from the seed and checked against the oracle's figures) and the red team
  (`evals/redteam/`: injections planted in sample bank lines, bills and documents; exclusion probes; accumulated
  disclosure; misleading requests), run through the real orchestrator against recorded model answers. Release needs
  100% numeric accuracy and 0 red-team successes; a missing recording fails a release run and is reported in replay.
- The harness takes the suites `p1-scheduled-agents` (QA-AI-07), `p1-ai-proposals` (QA-AI-02) and `p1-knowledge-base`
  (QA-AI-06) add.
- Monitoring: per turn, grounding failures, refusals, tool errors, screening flags and cost, as activity-log fields and
  OpenTelemetry counters, summarised on AI usage; approval rates arrive with proposals. Feedback is reviewed by an
  Owner or Administrator before it joins the golden set.

**D14. Local mode (DEV-09, ADR-T4).**
- The AI service runs in the Compose stack; replay answers from `evals/cassettes/`; live runs only with
  `FYNIQ_AI_LIVE=1` and a developer's key, or DEV-09's own-plan adapter, on synthetic data. Model Armor's local stub
  screens. Nothing local is in the release images.

Ports and adapters touched (ADR-T8): `ModelProvider` and `Screening` (used, not changed), a `Secrets` key for D2's
signing key, the storage port's new conversation stores (postgres adapter). ADRs: ADR-T2 (models and residency,
unchanged), ADR-T3 (the delegated token is the API's own, not Hydra's), ADR-T7 (the purge as a scheduled job), ADR-T8.

## Risks / Trade-offs

- [The stream is relayed twice (AI service → API → browser)] → server-sent events end to end with flushing; a turn's
  latency budget is measured locally and `[live-verify]` on Cloud Run.
- [A delegated token leaks from the AI service] → it reads only, through the registry's allow-list, as one person, for
  one turn, for at most 5 minutes, and the AI service is reachable only internally.
- [Structured answers read less naturally] → the renderer joins segments into prose; the golden set's helpfulness
  rating (PRD target ≥ 95%) is tracked with feedback.
- [Golden figures drift from the reports] → each measure is tested equal to its statement line, and the golden set's
  expectations are computed, not typed.
- [No model's answers are recorded offline] → replay reports it; release runs record live on synthetic data first.

## Migration Plan

- Additive migrations; the assistant stays off until the AI switch is on and a route serves `chat`, so a release changes
  nothing for an organisation that has not enabled it.

## Open Questions

- The per-person limits of D9 and the 8-tool cap are first values, settings an Owner can change; the golden set's live
  runs tune them before go-live.
